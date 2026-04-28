package tester

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

type testCodeInputs struct {
	moduleTask   agentengine.ArtifactDocument
	branch       agentengine.ArtifactDocument
	contractDoc  agentengine.ArtifactDocument
	seedDoc      agentengine.ArtifactDocument
	fullTestsDoc agentengine.ArtifactDocument
}

type testCodeExecutionReport struct {
	Status            string   `json:"status"`
	Summary           string   `json:"summary"`
	TestCommand       string   `json:"test_command"`
	TestPassed        bool     `json:"test_passed"`
	FailureSummary    string   `json:"failure_summary,omitempty"`
	ReproductionSteps []string `json:"reproduction_steps,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	SuspectedFiles    []string `json:"suspected_files,omitempty"`
}

func (a *Agent) executeTestCode(ctx context.Context, task core.TaskMetaData, recipe Recipe) core.TaskMetaData {
	if a.artifactStore == nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("tester artifact store is nil"))
	}
	if a.openCodeRunner == nil {
		a.openCodeRunner = common.NewManagedOpenCodeRunner("")
	}
	if a.gitManager == nil {
		a.gitManager = common.NewLocalGitManager()
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("resolve artifacts: %w", err))
	}
	inputs, err := findTestCodeInputs(env.InputArtifacts, docs, recipe)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	if strings.TrimSpace(inputs.moduleTask.URI) == "" {
		inputs, err = a.fillModuleTaskFromCoderBranch(ctx, inputs)
		if err != nil {
			return a.failureFeedback(ctx, task, err)
		}
	}
	branchInfo, err := common.ParseCoderBranchArtifact([]byte(inputs.branch.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	seedBundle, err := common.ParseTestFileBundle([]byte(inputs.seedDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse seed_tests.json: %w", err))
	}
	if err := common.ValidateTestFileBundle(seedBundle, "seed_tests"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate seed_tests.json: %w", err))
	}
	fullBundle, err := common.ParseTestFileBundle([]byte(inputs.fullTestsDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse full_test_files.json: %w", err))
	}
	if err := common.ValidateTestFileBundle(fullBundle, "full_test_files"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate full_test_files.json: %w", err))
	}
	if llm.IsNoop(a.llmClient) && isTesterFallbackAllowed() {
		a.logStep("test_code demo fallback success: noop llm client and strict delivery disabled")
		return a.testCodeFallbackSuccess(ctx, task, inputs)
	}

	moduleName := moduleNameFromArtifactURI(inputs.moduleTask.URI)
	worktree, err := a.gitManager.CreateCoderWorktree(ctx, common.CreateCoderWorktreeRequest{
		RepoDir:       branchInfo.RepoDir,
		BaseBranch:    branchInfo.Branch,
		BaseCommit:    branchInfo.Commit,
		RunID:         a.runID,
		AgentID:       a.agentID,
		TaskID:        task.TaskID,
		ModuleName:    moduleName,
		WorkspacePath: a.workspacePath,
	})
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("create tester worktree: %w", err))
	}

	if _, err := common.MaterializeTestFiles(worktree.Path, seedBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("materialize seed tests: %w", err))
	}
	if _, err := common.MaterializeTestFiles(worktree.Path, fullBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("materialize full tests: %w", err))
	}
	seedCommand := common.BundleTestCommand(seedBundle)
	if strings.TrimSpace(seedCommand) == "" {
		return a.failureFeedback(ctx, task, fmt.Errorf("seed_tests test_command is empty"))
	}
	seedResult, err := a.gitManager.RunTestCommand(ctx, worktree.Path, seedCommand)
	if err != nil {
		if isTesterRuntimeFailure(err) {
			return a.failureFeedback(ctx, task, err)
		}
		return a.testCodeFailureReport(ctx, task, inputs, "seed_tests", seedCommand, seedResult, err)
	}
	fullCommand := common.BundleTestCommand(fullBundle)
	if strings.TrimSpace(fullCommand) == "" {
		return a.failureFeedback(ctx, task, fmt.Errorf("full_test_files test_command is empty"))
	}
	fullResult, err := a.gitManager.RunTestCommand(ctx, worktree.Path, fullCommand)
	if err != nil {
		if isTesterRuntimeFailure(err) {
			return a.failureFeedback(ctx, task, err)
		}
		return a.testCodeFailureReport(ctx, task, inputs, "full_tests", fullCommand, fullResult, err)
	}
	a.logStep("test_code success: host seed and full tests passed")
	return a.testCodeFallbackSuccess(ctx, task, inputs)
}

func findTestCodeInputs(refs []agentengine.ArtifactRef, docs []agentengine.ArtifactDocument, recipe Recipe) (testCodeInputs, error) {
	docsByURI := make(map[string]agentengine.ArtifactDocument, len(docs))
	for _, doc := range docs {
		docsByURI[filepath.ToSlash(doc.URI)] = doc
	}

	var inputs testCodeInputs
	for _, ref := range refs {
		uri := filepath.ToSlash(strings.TrimSpace(ref.URI))
		lower := strings.ToLower(uri)
		doc := docsByURI[uri]
		switch {
		case strings.Contains(lower, "/artifacts/modules/"):
			inputs.moduleTask = doc
		case strings.Contains(lower, "/artifacts/branches/"):
			inputs.branch = doc
		case strings.Contains(lower, "/artifacts/contracts/"):
			inputs.contractDoc = doc
		case strings.Contains(lower, "/artifacts/seed_tests/"):
			inputs.seedDoc = doc
		case strings.Contains(lower, "/artifacts/test_data/") && path.Base(uri) == "full_test_files.json":
			inputs.fullTestsDoc = doc
		}
	}
	if strings.TrimSpace(inputs.branch.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/branches/", recipe.Op)
	}
	if strings.TrimSpace(inputs.contractDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/contracts/", recipe.Op)
	}
	if strings.TrimSpace(inputs.seedDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/seed_tests/", recipe.Op)
	}
	if strings.TrimSpace(inputs.fullTestsDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires full_test_files.json in /artifacts/test_data/", recipe.Op)
	}
	return inputs, nil
}

func (a *Agent) fillModuleTaskFromCoderBranch(ctx context.Context, inputs testCodeInputs) (testCodeInputs, error) {
	branchInfo, err := common.ParseCoderBranchArtifact([]byte(inputs.branch.Content))
	if err != nil {
		return inputs, fmt.Errorf("test_code requires module task URI or coder branch artifact with module_task_uri: %w", err)
	}
	moduleURI := strings.TrimSpace(branchInfo.ModuleTaskURI)
	if moduleURI == "" {
		return inputs, fmt.Errorf("coder branch artifact module_task_uri is required")
	}
	content, err := a.artifactStore.Read(ctx, moduleURI)
	if err != nil {
		return inputs, fmt.Errorf("read module task %s: %w", moduleURI, err)
	}
	inputs.moduleTask = agentengine.ArtifactDocument{URI: moduleURI, Content: string(content)}
	return inputs, nil
}

func readTestCodeReport(worktree string) (testCodeExecutionReport, error) {
	content, err := os.ReadFile(filepath.Join(worktree, ".devflow", "result.json"))
	if err != nil {
		return testCodeExecutionReport{}, fmt.Errorf("read .devflow/result.json: %w", err)
	}
	var report testCodeExecutionReport
	if err := json.Unmarshal(content, &report); err != nil {
		return testCodeExecutionReport{}, fmt.Errorf("parse .devflow/result.json: %w", err)
	}
	if strings.TrimSpace(report.Status) == "" {
		return testCodeExecutionReport{}, fmt.Errorf(".devflow/result.json status is required")
	}
	return report, nil
}

func buildTestCodePrompt(task core.TaskMetaData, recipe Recipe, inputs testCodeInputs, branchInfo common.CoderBranchArtifact) string {
	testCommand := strings.TrimSpace(branchInfo.TestCommand)
	if testCommand == "" {
		testCommand = "go test ./..."
	}

	var builder strings.Builder
	builder.WriteString("# Test Code Verification\n\n")
	builder.WriteString("# Role\n")
	builder.WriteString(recipe.SystemPrompt)
	builder.WriteString("\n\n# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("agent_id: %s\n", task.AgentID))
	builder.WriteString(fmt.Sprintf("op: %s\n", task.Op))
	builder.WriteString("\n# Branch Under Test\n")
	builder.WriteString(fmt.Sprintf("- repo_dir: %s\n", branchInfo.RepoDir))
	builder.WriteString(fmt.Sprintf("- branch: %s\n", branchInfo.Branch))
	builder.WriteString(fmt.Sprintf("- commit: %s\n", branchInfo.Commit))
	builder.WriteString(fmt.Sprintf("- suggested_test_command: %s\n", testCommand))
	builder.WriteString("\n# Inputs\n")
	writePromptDoc(&builder, "Module Task", inputs.moduleTask)
	writePromptDoc(&builder, "Coder Branch Artifact", inputs.branch)
	writePromptDoc(&builder, "Module Contract", inputs.contractDoc)
	writePromptDoc(&builder, "Seed Tests", inputs.seedDoc)
	writePromptDoc(&builder, "Full Test Files", inputs.fullTestsDoc)
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	builder.WriteString("\n\n")
	builder.WriteString("- Execute the tests yourself in this worktree.\n")
	builder.WriteString("- You may add temporary test files when needed.\n")
	builder.WriteString("- Do not modify product code except test files needed for verification.\n")
	builder.WriteString("- If module_contract.delivery_profile is frontend_web, verify index.html, README.md, src/, and that index.html loads the app code.\n")
	builder.WriteString("- Do not commit, merge, push, or switch branches.\n")
	builder.WriteString("- Do not commit any changes.\n")
	builder.WriteString("\n# Mandatory Completion Contract\n")
	builder.WriteString("- Before finishing, create the .devflow directory if it does not exist.\n")
	builder.WriteString("- Before finishing, write .devflow/result.json exactly once with the schema below.\n")
	builder.WriteString("- This report is mandatory even when tests pass and even when no code changes are needed.\n")
	builder.WriteString("- If you cannot run tests, still write .devflow/result.json with status=\"failed\", test_passed=false, and a failure_summary.\n")
	builder.WriteString("- test_command must be executable shell syntax only; put explanations in summary or evidence.\n")
	builder.WriteString("\n# .devflow/result.json schema\n")
	builder.WriteString(`{"status":"passed|failed","summary":"short summary","test_command":"command executed","test_passed":true,"failure_summary":"","reproduction_steps":[],"evidence":[],"suspected_files":[]}`)
	builder.WriteString("\n")
	return builder.String()
}

func writePromptDoc(builder *strings.Builder, title string, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(title)
	builder.WriteString("\nsource: ")
	builder.WriteString(doc.URI)
	builder.WriteString("\n")
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}

func (a *Agent) testCodeFailureReport(ctx context.Context, task core.TaskMetaData, inputs testCodeInputs, failedPhase string, command string, result common.CommandResult, runErr error) core.TaskMetaData {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_reports", testCodeReportFilename(task.AgentID))
	content := buildTestCodeFailureReport(inputs, failedPhase, command, result, runErr)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("write test failure report: %w", err))
	}
	a.logStep(fmt.Sprintf("test_code failed: phase=%s command=%q", failedPhase, command))
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
		Result:       core.TaskResultCodeBug,
	}
}

func (a *Agent) testCodeFallbackSuccess(ctx context.Context, task core.TaskMetaData, inputs testCodeInputs) core.TaskMetaData {
	legacyFeedback, err := a.writeTestArtifact(ctx, task)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	a.logStep(fmt.Sprintf("test_code success artifact written: module=%s full_tests=%s", inputs.moduleTask.URI, inputs.fullTestsDoc.URI))
	return common.FeedbackFor(task, a.runID, a.agentID, legacyFeedback.ArtifactURIs)
}

func isTesterCodingAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "coding agent executable not found") ||
		strings.Contains(message, "opencode") && strings.Contains(message, "not found")
}

func isTesterFallbackAllowed() bool {
	if strictDeliveryEnabled() {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_DISABLE_TESTER_FALLBACK")))
	return value != "1" && value != "true"
}

func strictDeliveryEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_STRICT_DELIVERY")))
	return value == "1" || value == "true"
}

func isTesterRuntimeFailure(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "required executable") ||
		strings.Contains(lower, "not found in path") ||
		strings.Contains(lower, "commandnotfoundexception")
}

func testCodeReportFilename(agentID core.AgentID) string {
	name := sanitizeFilePart(string(agentID))
	if name == "" {
		name = "tester"
	}
	return name + "_test_report.md"
}

func buildTestCodeFailureReport(inputs testCodeInputs, failedPhase string, command string, result common.CommandResult, runErr error) string {
	var builder strings.Builder
	builder.WriteString("# Test Failure Report\n\n")
	builder.WriteString("## Artifact Inputs\n\n")
	builder.WriteString("- module_task_uri: ")
	builder.WriteString(inputs.moduleTask.URI)
	builder.WriteString("\n- branch_uri: ")
	builder.WriteString(inputs.branch.URI)
	builder.WriteString("\n- contract_uri: ")
	builder.WriteString(inputs.contractDoc.URI)
	builder.WriteString("\n- seed_tests_uri: ")
	builder.WriteString(inputs.seedDoc.URI)
	builder.WriteString("\n- full_test_files_uri: ")
	builder.WriteString(inputs.fullTestsDoc.URI)
	builder.WriteString("\n- failed_phase: ")
	builder.WriteString(failedPhase)
	builder.WriteString("\n\n## Test Command\n\n")
	writeValueOrFallback(&builder, command, "not reported")
	builder.WriteString("\n\n## Exit Code\n\n")
	builder.WriteString(fmt.Sprintf("%d", result.ExitCode))
	builder.WriteString("\n\n## Runner Error\n\n")
	if runErr != nil {
		builder.WriteString(strings.TrimSpace(runErr.Error()))
	} else {
		builder.WriteString("not reported")
	}
	builder.WriteString("\n\n## Stdout\n\n```\n")
	builder.WriteString(strings.TrimSpace(result.Stdout))
	builder.WriteString("\n```\n\n## Stderr\n\n```\n")
	builder.WriteString(strings.TrimSpace(result.Stderr))
	builder.WriteString("\n```\n")
	builder.WriteString("\n\n## Reproduction Steps\n\n")
	builder.WriteString("- Checkout or open the coder branch worktree from the branch artifact.\n")
	builder.WriteString("- Materialize seed_tests.json and full_test_files.json into the worktree.\n")
	builder.WriteString("- Run `")
	builder.WriteString(strings.TrimSpace(command))
	builder.WriteString("`.\n")
	builder.WriteString("\n")
	return builder.String()
}

func writeValueOrFallback(builder *strings.Builder, value, fallback string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	builder.WriteString(value)
}

func writeListOrFallback(builder *strings.Builder, items []string, fallback string) {
	wrote := false
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
		wrote = true
	}
	if !wrote {
		builder.WriteString("- ")
		builder.WriteString(fallback)
		builder.WriteString("\n")
	}
}
