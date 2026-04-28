package architect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

type architectTestCodeInputs struct {
	design  agentengine.ArtifactDocument
	mainDoc agentengine.ArtifactDocument
	main    common.BranchArtifact
	merged  agentengine.ArtifactDocument
}

type architectTestCodeReport struct {
	Status            string   `json:"status"`
	Summary           string   `json:"summary"`
	TestCommand       string   `json:"test_command"`
	TestPassed        bool     `json:"test_passed"`
	Fixed             bool     `json:"fixed"`
	ChangedFiles      []string `json:"changed_files,omitempty"`
	FailureSummary    string   `json:"failure_summary,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	SuspectedFiles    []string `json:"suspected_files,omitempty"`
	ReproductionSteps []string `json:"reproduction_steps,omitempty"`
}

func (a *Agent) executeGlobalTestCode(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	start := time.Now()
	if a.artifactStore == nil {
		return core.TaskMetaData{
			Direction:    core.TaskDirectionFeedback,
			RunID:        a.runID,
			TaskID:       task.TaskID,
			ParentID:     task.ParentID,
			DependsOn:    task.DependsOn,
			DependsOnIDs: task.DependsOnIDs,
			AgentID:      a.agentID,
			Op:           task.Op,
			Result:       core.TaskResultCodeFail,
		}, nil
	}
	if a.gitManager == nil {
		a.gitManager = common.NewLocalGitManager()
	}
	docs, err := resolveArchitectDocs(ctx, a.artifactStore, task.ArtifactURIs)
	if err != nil {
		return a.globalTestSystemFailure(ctx, task, nil, architectTestCodeReport{}, fmt.Errorf("resolve inputs failed: %w", err), start)
	}
	inputs := findArchitectTestCodeInputs(docs)
	if strings.TrimSpace(inputs.mainDoc.URI) == "" {
		return a.globalTestSystemFailure(ctx, task, &inputs, architectTestCodeReport{}, fmt.Errorf("main branch artifact missing"), start)
	}
	repoDir := filepath.FromSlash(strings.TrimSpace(inputs.main.RepoDir))
	if err := validateGlobalTestRepo(repoDir); err != nil {
		return a.globalTestSystemFailure(ctx, task, &inputs, architectTestCodeReport{}, err, start)
	}
	if err := validateFrontendWebDelivery(repoDir, inputs.main); err != nil {
		return a.globalTestSystemFailure(ctx, task, &inputs, architectTestCodeReport{}, err, start)
	}
	commands := globalVerifyCommands(inputs)
	if len(commands) == 0 {
		return a.globalTestSystemFailure(ctx, task, &inputs, architectTestCodeReport{}, fmt.Errorf("global_verify_commands or test_command is required"), start)
	}

	testCtx, cancel := context.WithTimeout(ctx, globalTestTimeout(a.runConfig))
	defer cancel()
	evidence := make([]string, 0, len(commands))
	for _, command := range commands {
		a.logStep(fmt.Sprintf("test_code command start: repo=%s command=%q", repoDir, command))
		result, err := a.gitManager.RunTestCommand(testCtx, repoDir, command)
		if err == nil && result.ExitCode == 0 {
			evidence = append(evidence, fmt.Sprintf("%s passed in %d ms", command, common.DurationMillis(result.Duration)))
			continue
		}
		code := classifyGlobalTestCommandFailure(testCtx, result, err)
		report := architectTestCodeReport{
			Status:            "failed",
			Summary:           fmt.Sprintf("global verification command failed: %s", command),
			TestCommand:       command,
			TestPassed:        false,
			FailureSummary:    strings.TrimSpace(result.Stdout + "\n" + result.Stderr + "\n" + errorString(err)),
			Evidence:          evidence,
			ReproductionSteps: []string{command},
		}
		cause := fmt.Errorf("global verification command %q failed with exit code %d", command, result.ExitCode)
		if err != nil {
			cause = fmt.Errorf("global verification command %q failed: %w", command, err)
		}
		return a.globalTestFailureFeedbackWithResult(ctx, task, &inputs, report, cause, start, code)
	}
	if err := validateFrontendWebDelivery(repoDir, inputs.main); err != nil {
		return a.globalTestSystemFailure(ctx, task, &inputs, architectTestCodeReport{}, err, start)
	}

	return a.globalTestSuccess(ctx, task, &inputs, fmt.Sprintf("Global verification passed: %s", strings.Join(commands, "; ")), start)
}

func globalVerifyCommands(inputs architectTestCodeInputs) []string {
	commands := normalizeGlobalVerifyCommands(inputs.main.GlobalVerifyCommands)
	if len(commands) > 0 {
		return commands
	}

	if command := strings.TrimSpace(inputs.main.TestCommand); command != "" {
		return []string{command}
	}

	return nil
}

func validateGlobalTestRepo(repoDir string) error {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return fmt.Errorf("global test requires repo_dir")
	}

	info, err := os.Stat(repoDir)
	if err != nil {
		return fmt.Errorf("global test repo_dir unavailable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("global test repo_dir is not a directory: %s", repoDir)
	}
	return nil
}

func validateFrontendWebDelivery(repoDir string, artifact common.BranchArtifact) error {
	if !strings.EqualFold(strings.TrimSpace(artifact.DeliveryProfile), defaultDeliveryProfile) {
		return nil
	}
	requiredFiles := artifact.RequiredFiles
	if len(requiredFiles) == 0 {
		requiredFiles = []string{"index.html", "README.md", "src/"}
	}
	for _, required := range requiredFiles {
		if err := validateFrontendWebRequiredPath(repoDir, required); err != nil {
			return err
		}
	}
	content, err := os.ReadFile(filepath.Join(repoDir, "index.html"))
	if err != nil {
		return fmt.Errorf("frontend_web delivery missing index.html")
	}
	index := strings.ToLower(string(content))
	if !strings.Contains(index, "script") && !strings.Contains(index, "module") && !strings.Contains(index, "src/") {
		return fmt.Errorf("frontend_web delivery index.html does not load src application code")
	}
	return nil
}

func validateFrontendWebRequiredPath(repoDir string, required string) error {
	required = filepath.ToSlash(strings.TrimSpace(required))
	if required == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(required, "/")
	target := filepath.Join(repoDir, filepath.FromSlash(trimmed))
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("frontend_web delivery missing %s", required)
	}
	if strings.HasSuffix(required, "/") || required == "src" {
		if !info.IsDir() {
			return fmt.Errorf("frontend_web delivery missing %s", required)
		}
		return nil
	}
	if info.IsDir() {
		return fmt.Errorf("frontend_web delivery missing %s", required)
	}
	return nil
}

func globalTestTimeout(config core.RunConfig) time.Duration {
	if config.Delivery.GlobalTestTimeoutSeconds > 0 {
		return time.Duration(config.Delivery.GlobalTestTimeoutSeconds) * time.Second
	}
	return 60 * time.Second
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func classifyGlobalTestCommandFailure(ctx context.Context, result common.CommandResult, err error) core.TaskResultCode {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return core.TaskResultCodeBug
	}

	output := strings.ToLower(result.Stdout + "\n" + result.Stderr + "\n" + errorString(err))
	if strings.Contains(output, "executable file not found") ||
		strings.Contains(output, "command not found") ||
		strings.Contains(output, "not recognized as an internal or external command") ||
		strings.Contains(output, "no such file or directory") {
		return core.TaskResultCodeFail
	}

	if result.ExitCode != 0 {
		return core.TaskResultCodeBug
	}

	if err != nil {
		return core.TaskResultCodeFail
	}

	return core.TaskResultCodeBug
}

func findArchitectTestCodeInputs(docs []agentengine.ArtifactDocument) architectTestCodeInputs {
	var inputs architectTestCodeInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.TrimSpace(doc.URI))
		lower := strings.ToLower(uri)
		switch {
		case agentengine.InferArtifactKind(uri) == "design":
			inputs.design = doc
		case strings.Contains(lower, "/artifacts/code/"):
			inputs.merged = doc
		case strings.Contains(lower, "/artifacts/branches/") && artifactKind([]byte(doc.Content)) == "main_branch":
			if branch, err := common.ParseBranchArtifact([]byte(doc.Content)); err == nil {
				if strings.Contains(lower, "/merged_main_branch.md") || strings.TrimSpace(inputs.mainDoc.URI) == "" || !strings.Contains(strings.ToLower(filepath.ToSlash(inputs.mainDoc.URI)), "/merged_main_branch.md") {
					inputs.mainDoc = doc
					inputs.main = branch
				}
			}
		}
	}
	return inputs
}

func (a *Agent) globalTestSuccess(ctx context.Context, task core.TaskMetaData, inputs *architectTestCodeInputs, summary string, start time.Time) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test", "global_test_report_v1.md")
	content := buildGlobalTestSuccessArtifact(inputs, summary, common.DurationMillis(time.Since(start)))
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep("test_code success: global report written")
	return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
}

func buildGlobalTestSuccessArtifact(inputs *architectTestCodeInputs, summary string, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# Global Test Report\n\n")
	builder.WriteString("## Summary\n\n")
	builder.WriteString(strings.TrimSpace(summary))
	builder.WriteString("\n\n## Inputs\n\n")
	if inputs != nil {
		if strings.TrimSpace(inputs.design.URI) != "" {
			builder.WriteString("- architecture_uri: ")
			builder.WriteString(inputs.design.URI)
			builder.WriteString("\n")
		}
		if strings.TrimSpace(inputs.mainDoc.URI) != "" {
			builder.WriteString("- main_branch_uri: ")
			builder.WriteString(inputs.mainDoc.URI)
			builder.WriteString("\n")
		}
		if strings.TrimSpace(inputs.merged.URI) != "" {
			builder.WriteString("- merged_code_uri: ")
			builder.WriteString(inputs.merged.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString(fmt.Sprintf("\n## Elapsed\n\n%d ms\n", elapsedMillis))
	return builder.String()
}

func (a *Agent) globalTestSystemFailure(ctx context.Context, task core.TaskMetaData, inputs *architectTestCodeInputs, report architectTestCodeReport, cause error, start time.Time) (core.TaskMetaData, error) {
	return a.globalTestFailureFeedbackWithResult(ctx, task, inputs, report, cause, start, core.TaskResultCodeFail)
}

func (a *Agent) globalTestBugFailure(ctx context.Context, task core.TaskMetaData, inputs *architectTestCodeInputs, report architectTestCodeReport, cause error, start time.Time) (core.TaskMetaData, error) {
	return a.globalTestFailureFeedbackWithResult(ctx, task, inputs, report, cause, start, core.TaskResultCodeBug)
}

func (a *Agent) globalTestFailureFeedbackWithResult(ctx context.Context, task core.TaskMetaData, inputs *architectTestCodeInputs, report architectTestCodeReport, cause error, start time.Time, result core.TaskResultCode) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_reports", "architect_test_code_report.md")
	content := buildGlobalTestFailureReport(inputs, report, cause, common.DurationMillis(time.Since(start)))
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("test_code failed: %v", cause))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{outputURI},
		Result:       result,
	}, nil
}

func buildGlobalTestFailureReport(inputs *architectTestCodeInputs, report architectTestCodeReport, cause error, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# Global Test Failure Report\n\n")
	builder.WriteString("## Summary\n\n")
	if summary := strings.TrimSpace(report.Summary); summary != "" {
		builder.WriteString(summary)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("global test failed.")
	}
	builder.WriteString("\n\n## Inputs\n\n")
	if inputs != nil {
		builder.WriteString("- architecture_uri: ")
		builder.WriteString(inputs.design.URI)
		builder.WriteString("\n- main_branch_uri: ")
		builder.WriteString(inputs.mainDoc.URI)
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Failure\n\n")
	if detail := strings.TrimSpace(report.FailureSummary); detail != "" {
		builder.WriteString(detail)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	}
	builder.WriteString(fmt.Sprintf("\n\n## Elapsed\n\n%d ms\n", elapsedMillis))
	return builder.String()
}
