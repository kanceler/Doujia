package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/app"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
)

const defaultRequirementText = "\u5e2e\u6211\u5199\u4e00\u4e2a\u5355\u673a\u7248\u672c\u7684\u8d2a\u5403\u86c7\u6e38\u620f\u5427"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "doujiagit-demo-phase-two: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var dbPath string
	var projectsRoot string
	var runID string
	var outputPath string
	var pipelineRegistryPath string
	var pipelineID string
	var agentMode string
	var agentPluginRoots string
	var requirementText string
	var timeout time.Duration
	maxCoderAgents := 2
	maxTesterAgents := 2
	allowParallel := true
	var reset bool
	flag.StringVar(&dbPath, "db", filepath.Join("runtime", "doujiagit-demo", "state.db"), "SQLite state database path")
	flag.StringVar(&projectsRoot, "projects", filepath.Join("runtime", "doujiagit-demo", "workspaces"), "projects/workspaces root")
	flag.StringVar(&runID, "run", "run_doujiagit_demo_phase_two", "run id")
	flag.StringVar(&outputPath, "out", "", "optional JSON output path; stdout when empty")
	flag.StringVar(&pipelineRegistryPath, "pipeline-registry", "", "optional PipelineSpec JSON registry path")
	flag.StringVar(&pipelineID, "pipeline", "", "pipeline id; defaults to phase_two_delivery_flow or the JSON registry entry pipeline")
	flag.StringVar(&agentMode, "agent-mode", "", "agent mode: empty/default/protocolmock or real")
	flag.StringVar(&agentPluginRoots, "agent-plugin-roots", "", "optional real-agent plugin root directories separated by the OS path-list separator")
	flag.StringVar(&requirementText, "requirement", defaultRequirementText, "external requirement text used to seed ceo_write_requirement")
	flag.DurationVar(&timeout, "timeout", 45*time.Second, "maximum time to wait for the demo run")
	flag.IntVar(&maxCoderAgents, "max-coder-agents", 2, "maximum coder agents to use for the demo run")
	flag.IntVar(&maxTesterAgents, "max-tester-agents", 2, "maximum tester agents to use for the demo run")
	flag.BoolVar(&allowParallel, "parallel", true, "allow parallel module work in the demo run")
	flag.BoolVar(&reset, "reset", false, "remove the demo database and run workspace before running")
	flag.Parse()

	dbPath = filepath.Clean(dbPath)
	projectsRoot = filepath.Clean(projectsRoot)
	pipelineRegistryPath = strings.TrimSpace(pipelineRegistryPath)
	if err := validateDemoRunID(runID); err != nil {
		return err
	}
	if err := validateRealAgentEnv(agentMode, os.Getenv); err != nil {
		return err
	}
	selectedPipelineID, err := resolveDemoPipelineID(pipelineRegistryPath, pipelineID)
	if err != nil {
		return err
	}
	runConfig, err := demoRunConfig(demoRunOptions{
		MaxCoderAgents:  maxCoderAgents,
		MaxTesterAgents: maxTesterAgents,
		AllowParallel:   allowParallel,
	})
	if err != nil {
		return err
	}
	runDir := filepath.Join(projectsRoot, runID)
	if reset {
		if err := removeSQLiteDBFiles(dbPath); err != nil {
			return err
		}
		if err := os.RemoveAll(runDir); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	bootstrap, db, err := app.NewSQLiteBootstrapWithOptions(ctx, projectsRoot, dbPath, app.BootstrapOptions{
		PipelineRegistryPath: pipelineRegistryPath,
		AgentMode:            agentMode,
		AgentPluginRoots:     filepath.SplitList(agentPluginRoots),
		TaskWorkerCount:      demoTaskWorkerCount(runConfig.Delivery),
	})
	if err != nil {
		return err
	}
	defer db.Close()

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, core.RunID(runID), selectedPipelineID, runConfig); err != nil {
		return err
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, core.RunID(runID)); err != nil {
		return err
	}
	feedback, err := seedRequirementFeedback(ctx, bootstrap, core.RunID(runID), runDir, requirementText)
	if err != nil {
		return err
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		return err
	}
	if err := waitForRunStatus(ctx, bootstrap, core.RunID(runID), core.RunStatusCompleted); err != nil {
		return err
	}

	graph, err := doujiagit.BuildRunGraph(ctx, bootstrap.Internals.DoujiaGitRepository, core.RunID(runID), doujiagit.DefaultRefName)
	if err != nil {
		return err
	}
	return writeJSON(outputPath, graph)
}

func resolveDemoPipelineID(registryPath string, pipelineID string) (core.PipelineID, error) {
	pipelineID = strings.TrimSpace(pipelineID)
	if pipelineID != "" {
		return core.PipelineID(pipelineID), nil
	}
	if strings.TrimSpace(registryPath) == "" {
		return pipeline.PipelineIDPhaseTwo, nil
	}
	spec, err := pipeline.LoadRegistrySpec(filepath.Clean(registryPath))
	if err != nil {
		return "", err
	}
	if spec.EntryPipelineID == "" {
		return "", fmt.Errorf("pipeline registry %s has empty entry_pipeline_id", registryPath)
	}
	return spec.EntryPipelineID, nil
}

func validateDemoRunID(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("-run is required")
	}
	if runID == "." || runID == ".." || strings.ContainsAny(runID, `/\`) {
		return fmt.Errorf("-run must be a simple run id, got %q", runID)
	}
	return nil
}

func validateRealAgentEnv(agentMode string, getenv func(string) string) error {
	mode := strings.ToLower(strings.TrimSpace(agentMode))
	if mode != "real" && mode != "llm" {
		return nil
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	required := []string{"OPENAI_API_KEY", "OPENAI_MODEL", "OPENAI_BASE_URL"}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("-agent-mode %s requires %s; set them before running or use -agent-mode protocolmock for the stable offline baseline", mode, strings.Join(missing, ", "))
	}
	return nil
}

func removeSQLiteDBFiles(dbPath string) error {
	info, err := os.Stat(dbPath)
	if err == nil && info.IsDir() {
		return fmt.Errorf("refusing to remove directory passed as -db: %s", dbPath)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

type demoRunOptions struct {
	MaxCoderAgents  int
	MaxTesterAgents int
	AllowParallel   bool
}

func demoRunConfig(options ...demoRunOptions) (core.RunConfig, error) {
	opts := demoRunOptions{
		MaxCoderAgents:  2,
		MaxTesterAgents: 2,
		AllowParallel:   true,
	}
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.MaxCoderAgents <= 0 {
		return core.RunConfig{}, fmt.Errorf("-max-coder-agents must be greater than zero")
	}
	if opts.MaxTesterAgents <= 0 {
		return core.RunConfig{}, fmt.Errorf("-max-tester-agents must be greater than zero")
	}
	return core.RunConfig{
		Delivery: core.DeliveryConfig{
			MaxCoderAgents:         opts.MaxCoderAgents,
			MaxTesterAgents:        opts.MaxTesterAgents,
			RequireTesterPerModule: true,
			AllowParallelWork:      opts.AllowParallel,
			Git: core.GitRunConfig{
				MainBranch: "main",
			},
		},
	}, nil
}

func demoTaskWorkerCount(config core.DeliveryConfig) int {
	if !config.AllowParallelWork && config.MaxCoderAgents == 1 && config.MaxTesterAgents == 1 {
		return 1
	}
	return 4
}

func seedRequirementFeedback(ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, runDir string, requirementText string) (core.TaskMetaData, error) {
	requirementText = strings.TrimSpace(requirementText)
	if requirementText == "" {
		requirementText = defaultRequirementText
	}
	outputRef := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "ceo", "artifacts", "requirement", "requirement_v1.md"))
	store := &artifact.ScopedLocalStore{
		RunRoot:       runDir,
		WorkspaceRoot: filepath.Join(runDir, "agents", "ceo"),
	}
	if err := store.Write(ctx, outputRef, []byte(requirementText+"\n")); err != nil {
		return core.TaskMetaData{}, err
	}
	committer := runtime.NewDoujiaGitOutputCommitter(bootstrap.Internals.DoujiaGitRepository, store)
	feedback, err := runtime.CommitAgentFeedbackOutputs(ctx, committer, runID, "ceo", core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	}, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "ceo_write_requirement",
		AgentID:      "ceo",
		Op:           core.TaskOpWritePlan,
		ArtifactURIs: []string{outputRef},
		Result:       core.TaskResultCodeOK,
		Outputs: []core.AgentOutput{
			{LogicalKey: "requirement", ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: "requirement"},
				},
			},
		},
	})
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return feedback, nil
}

func waitForRunStatus(ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, want core.RunStatus) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
		if err == nil && run.Status == want {
			return nil
		}
		if err == nil && run.Status == core.RunStatusFailed {
			return fmt.Errorf("run %s failed before reaching %s", runID, want)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for run %s status %s: %w", runID, want, ctx.Err())
		case <-ticker.C:
		}
	}
}

func writeJSON(outputPath string, payload any) error {
	var out *os.File
	if outputPath == "" {
		out = os.Stdout
	} else {
		if err := os.MkdirAll(filepath.Dir(filepath.Clean(outputPath)), 0o755); err != nil {
			return err
		}
		file, err := os.Create(outputPath)
		if err != nil {
			return err
		}
		defer file.Close()
		out = file
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
