package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/app"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/pipeline"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "doujiagit-archive: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var mode string
	var dbPath string
	var runID string
	var newRunID string
	var refName string
	var targetRefName string
	var archivePath string
	var projectsRoot string
	var pipelineRegistryPath string
	var pipelineID string
	var resume bool
	var includeFiles bool
	flag.StringVar(&mode, "mode", "", "export or import")
	flag.StringVar(&dbPath, "db", "", "SQLite state database path")
	flag.StringVar(&runID, "run", "", "source run id for export")
	flag.StringVar(&newRunID, "new-run", "", "target run id for import")
	flag.StringVar(&refName, "ref", doujiagit.DefaultRefName, "source ref name for export")
	flag.StringVar(&targetRefName, "target-ref", "", "target ref name for import; defaults to archive ref")
	flag.StringVar(&archivePath, "archive", "", "archive zip path")
	flag.StringVar(&projectsRoot, "projects", "", "projects/workspaces root")
	flag.StringVar(&pipelineRegistryPath, "pipeline-registry", "", "JSON pipeline registry path for imported run")
	flag.StringVar(&pipelineID, "pipeline", string(pipeline.PipelineIDPhaseTwo), "pipeline id for imported run")
	flag.BoolVar(&resume, "resume", true, "materialize imported ref into run task state")
	flag.BoolVar(&includeFiles, "include-files", true, "include artifact files on export")
	flag.Parse()

	if dbPath == "" {
		return fmt.Errorf("-db is required")
	}
	if archivePath == "" {
		return fmt.Errorf("-archive is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if mode == "import" {
		return importArchive(ctx, dbPath, archivePath, projectsRoot, newRunID, targetRefName, pipelineID, pipelineRegistryPath, resume)
	}

	db, err := sql.Open("sqlite", filepath.Clean(dbPath))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}
	repository := doujiagit.NewSQLiteRepository(db)
	if err := repository.Migrate(ctx); err != nil {
		return err
	}

	switch mode {
	case "export":
		if runID == "" {
			return fmt.Errorf("-run is required for export")
		}
		if includeFiles && projectsRoot == "" {
			return fmt.Errorf("-projects is required when -include-files=true")
		}
		return doujiagit.WriteRunArchiveZip(ctx, repository, archivePath, doujiagit.ExportOptions{
			RunID:                core.RunID(runID),
			RefName:              refName,
			ProjectsRoot:         projectsRoot,
			IncludeArtifactFiles: includeFiles,
		})
	default:
		return fmt.Errorf("-mode must be export or import")
	}
}

func importArchive(ctx context.Context, dbPath string, archivePath string, projectsRoot string, newRunID string, targetRefName string, pipelineID string, pipelineRegistryPath string, resume bool) error {
	if strings.TrimSpace(newRunID) == "" {
		return fmt.Errorf("-new-run is required for import")
	}
	if strings.TrimSpace(projectsRoot) == "" {
		return fmt.Errorf("-projects is required for import")
	}
	bootstrap, db, err := app.NewSQLiteBootstrapWithOptions(ctx, projectsRoot, dbPath, app.BootstrapOptions{
		PipelineRegistryPath: pipelineRegistryPath,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	importResult, err := doujiagit.ImportRunArchiveZip(ctx, bootstrap.Internals.DoujiaGitRepository, archivePath, doujiagit.ImportOptions{
		NewRunID:      core.RunID(newRunID),
		TargetRefName: targetRefName,
		ProjectsRoot:  projectsRoot,
	})
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	projectDir, err := filepath.Abs(filepath.Join(filepath.Clean(projectsRoot), newRunID))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	run := core.PipelineRun{
		ID:         core.RunID(newRunID),
		PipelineID: core.PipelineID(strings.TrimSpace(pipelineID)),
		Status:     core.RunStatusCreated,
		ProjectDir: projectDir,
		Config: core.RunConfig{
			Delivery: core.DeliveryConfig{
				MaxCoderAgents:         2,
				MaxTesterAgents:        2,
				RequireTesterPerModule: true,
				AllowParallelWork:      true,
				Git: core.GitRunConfig{
					MainBranch: "main",
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if run.PipelineID == "" {
		run.PipelineID = pipeline.PipelineIDPhaseTwo
	}
	if err := bootstrap.Internals.RunRepository.Create(ctx, run); err != nil {
		return err
	}

	var resumeResult any = nil
	if resume {
		result, err := bootstrap.Internals.Orchestrator.ResumeFromRef(ctx, core.RunID(newRunID), importResult.RefName)
		if err != nil {
			return err
		}
		resumeResult = result
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"import": importResult,
		"resume": resumeResult,
	})
}
