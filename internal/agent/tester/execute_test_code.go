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
)

type testCodeInputs struct {
	moduleTask agentengine.ArtifactDocument
	branch     agentengine.ArtifactDocument
	testData   agentengine.ArtifactDocument
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
	branchInfo, err := common.ParseBranchArtifact([]byte(inputs.branch.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, err)
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

	prompt := buildTestCodePrompt(task, recipe, inputs, branchInfo)
	a.logStep(fmt.Sprintf("test_code opencode request start: worktree=%s prompt_chars=%d", worktree.Path, len(prompt)))
	if _, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: worktree.Path,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	}); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("opencode test run: %w", err))
	}

	report, err := readTestCodeReport(worktree.Path)
	if err != nil {
		return a.failureFeedback(ctx, task, err)
	}
	if report.TestPassed {
		a.logStep("test_code success: opencode reported tests passed")
		return common.FeedbackFor(task, a.runID, a.agentID, nil)
	}
	return a.testCodeFailureReport(ctx, task, inputs, report)
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
		case strings.Contains(lower, "/artifacts/test_data/"):
			inputs.testData = doc
		}
	}
	if strings.TrimSpace(inputs.moduleTask.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/modules/", recipe.Op)
	}
	if strings.TrimSpace(inputs.branch.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/branches/", recipe.Op)
	}
	if strings.TrimSpace(inputs.testData.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/test_data/", recipe.Op)
	}
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

func buildTestCodePrompt(task core.TaskMetaData, recipe Recipe, inputs testCodeInputs, branchInfo common.BranchArtifact) string {
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
	writePromptDoc(&builder, "Unit Test Data", inputs.testData)
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	builder.WriteString("\n\n")
	builder.WriteString("- Execute the tests yourself in this worktree.\n")
	builder.WriteString("- You may add temporary test files when needed.\n")
	builder.WriteString("- Do not modify product code except test files needed for verification.\n")
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

func (a *Agent) testCodeFailureReport(ctx context.Context, task core.TaskMetaData, inputs testCodeInputs, report testCodeExecutionReport) core.TaskMetaData {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_reports", testCodeReportFilename(task.AgentID))
	content := buildTestCodeFailureReport(inputs, report)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("write test failure report: %w", err))
	}
	a.logStep(fmt.Sprintf("test_code failed: %s", report.Summary))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{outputURI},
		Result:       core.TaskResultCodeFail,
	}
}

func testCodeReportFilename(agentID core.AgentID) string {
	name := sanitizeFilePart(string(agentID))
	if name == "" {
		name = "tester"
	}
	return name + "_test_report.md"
}

func buildTestCodeFailureReport(inputs testCodeInputs, report testCodeExecutionReport) string {
	var builder strings.Builder
	builder.WriteString("# Test Failure Report\n\n")
	builder.WriteString("## Summary\n\n")
	writeValueOrFallback(&builder, report.Summary, "OpenCode reported test failure.")
	builder.WriteString("\n\n## Failure Summary\n\n")
	writeValueOrFallback(&builder, report.FailureSummary, "No detailed failure summary was provided.")
	builder.WriteString("\n\n## Artifact Inputs\n\n")
	builder.WriteString("- module_task_uri: ")
	builder.WriteString(inputs.moduleTask.URI)
	builder.WriteString("\n- branch_uri: ")
	builder.WriteString(inputs.branch.URI)
	builder.WriteString("\n- test_data_uri: ")
	builder.WriteString(inputs.testData.URI)
	builder.WriteString("\n\n## Test Command\n\n")
	writeValueOrFallback(&builder, report.TestCommand, "not reported")
	builder.WriteString("\n\n## Reproduction Steps\n\n")
	writeListOrFallback(&builder, report.ReproductionSteps, "No reproduction steps were reported.")
	builder.WriteString("\n\n## Evidence\n\n")
	writeListOrFallback(&builder, report.Evidence, "No evidence was reported.")
	builder.WriteString("\n\n## Suspected Files\n\n")
	writeListOrFallback(&builder, report.SuspectedFiles, "No suspected files were reported.")
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
