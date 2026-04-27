package coder

import (
	"context"
	"encoding/json"
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

type openCodeExecutionReport struct {
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changed_files"`
	TestCommand  string   `json:"test_command"`
	TestPassed   bool     `json:"test_passed"`
}

func (a *Agent) executeWriteCode(ctx context.Context, task core.TaskMetaData, recipe Recipe) (core.TaskMetaData, error) {
	start := time.Now()
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("coder artifact store is nil")
	}
	if a.openCodeRunner == nil {
		a.openCodeRunner = common.NewManagedOpenCodeRunner("")
	}
	if a.gitManager == nil {
		a.gitManager = common.NewLocalGitManager()
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("resolve artifacts: %w", err), start), nil
	}
	moduleDoc, branchDoc, err := findWriteCodeInputs(docs, recipe)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	branchInfo, err := common.ParseBranchArtifact([]byte(branchDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}

	moduleName := moduleNameFromURI(moduleDoc.URI)
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
		return a.failureFeedback(ctx, task, fmt.Errorf("create coder worktree: %w", err), start), nil
	}

	prompt := buildPrompt(task, recipe, moduleDoc, branchDoc)
	a.logStep(fmt.Sprintf("write_code coding agent request start: worktree=%s prompt_chars=%d", worktree.Path, len(prompt)))
	codingAgentResult, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: worktree.Path,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	})
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("coding agent run: %w", err), start), nil
	}

	report, err := readOpenCodeReport(worktree.Path)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	hasChanges, err := a.gitManager.HasChanges(ctx, worktree.Path)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("check code changes: %w", err), start), nil
	}
	if !hasChanges {
		return a.failureFeedback(ctx, task, fmt.Errorf("coding agent completed but produced no code changes"), start), nil
	}

	testCommand := strings.TrimSpace(branchInfo.TestCommand)
	if testCommand == "" {
		testCommand = strings.TrimSpace(report.TestCommand)
	}
	if testCommand == "" {
		testCommand = "go test ./..."
	}
	testResult, err := a.gitManager.RunTestCommand(ctx, worktree.Path, testCommand)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("test command failed: %w", err), start), nil
	}
	commitResult, err := a.gitManager.CommitAll(ctx, worktree.Path, fmt.Sprintf("%s: write_code %s", a.agentID, moduleName))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("commit coder changes: %w", err), start), nil
	}

	summary := strings.TrimSpace(report.Summary)
	if summary == "" {
		summary = "Coder write_code completed."
	}
	output := common.CoderBranchArtifact{
		SchemaVersion:            1,
		Kind:                     "coder_branch",
		RepoDir:                  branchInfo.RepoDir,
		BaseBranch:               branchInfo.Branch,
		BaseCommit:               branchInfo.Commit,
		Branch:                   worktree.Branch,
		Commit:                   commitResult.Commit,
		Worktree:                 filepath.ToSlash(worktree.Path),
		ModuleTaskURI:            moduleDoc.URI,
		Summary:                  summary,
		ChangedFiles:             report.ChangedFiles,
		TestCommand:              testCommand,
		CodingAgentElapsedMillis: common.DurationMillis(codingAgentResult.Duration),
		TestElapsedMillis:        common.DurationMillis(testResult.Duration),
		CommitElapsedMillis:      common.DurationMillis(commitResult.Duration),
		TotalElapsedMillis:       common.DurationMillis(time.Since(start)),
	}
	content, err := common.MarshalJSONArtifact(output)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", recipe.OutputKind, moduleName+"_branch.md")
	if err := a.artifactStore.Write(ctx, outputURI, content); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("write_code success: branch=%s commit=%s elapsed_ms=%d", output.Branch, output.Commit, output.TotalElapsedMillis))
	return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
}

func findWriteCodeInputs(docs []agentengine.ArtifactDocument, recipe Recipe) (agentengine.ArtifactDocument, agentengine.ArtifactDocument, error) {
	var moduleDoc agentengine.ArtifactDocument
	var branchDoc agentengine.ArtifactDocument
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.ToLower(doc.URI))
		switch {
		case strings.Contains(uri, "/artifacts/modules/"):
			moduleDoc = doc
		case strings.Contains(uri, "/artifacts/branches/"):
			branchDoc = doc
		}
	}
	if strings.TrimSpace(moduleDoc.URI) == "" {
		return moduleDoc, branchDoc, fmt.Errorf("%s requires an artifact URI containing /artifacts/modules/", recipe.Op)
	}
	if strings.TrimSpace(branchDoc.URI) == "" {
		return moduleDoc, branchDoc, fmt.Errorf("%s requires an artifact URI containing /artifacts/branches/", recipe.Op)
	}
	return moduleDoc, branchDoc, nil
}

func moduleNameFromURI(uri string) string {
	base := path.Base(filepath.ToSlash(uri))
	ext := path.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func readOpenCodeReport(worktree string) (openCodeExecutionReport, error) {
	content, err := os.ReadFile(filepath.Join(worktree, ".devflow", "result.json"))
	if err != nil {
		return openCodeExecutionReport{}, fmt.Errorf("read .devflow/result.json: %w", err)
	}
	var report openCodeExecutionReport
	if err := json.Unmarshal(content, &report); err != nil {
		return openCodeExecutionReport{}, fmt.Errorf("parse .devflow/result.json: %w", err)
	}
	if strings.TrimSpace(report.Status) == "" {
		return openCodeExecutionReport{}, fmt.Errorf(".devflow/result.json status is required")
	}
	return report, nil
}

func (a *Agent) failureFeedback(ctx context.Context, task core.TaskMetaData, cause error, start time.Time) core.TaskMetaData {
	content, _ := common.MarshalJSONArtifact(map[string]any{
		"schema_version":      1,
		"kind":                "coder_failure",
		"op":                  task.Op,
		"error":               cause.Error(),
		"total_elapsed_ms":    common.DurationMillis(time.Since(start)),
		"input_artifact_uris": task.ArtifactURIs,
	})
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "failures", "write_code_failure.md")
	outputs := []string{}
	if a.artifactStore != nil && content != nil {
		if err := a.artifactStore.Write(ctx, outputURI, content); err == nil {
			outputs = append(outputs, outputURI)
		}
	}
	a.logStep(fmt.Sprintf("write_code failed: %v", cause))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: outputs,
		Result:       core.TaskResultCodeFail,
	}
}
