package coder

import (
	"context"
	"encoding/json"
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
	"devflow/internal/llm"
)

type openCodeExecutionReport struct {
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changed_files"`
	TestCommand  string   `json:"test_command"`
	TestPassed   bool     `json:"test_passed"`
}

type debugInputs struct {
	moduleDoc    agentengine.ArtifactDocument
	branchDoc    agentengine.ArtifactDocument
	contractDoc  agentengine.ArtifactDocument
	seedDoc      agentengine.ArtifactDocument
	fullTestsDoc agentengine.ArtifactDocument
	failureDoc   agentengine.ArtifactDocument
}

type writeCodeInputs struct {
	moduleDoc   agentengine.ArtifactDocument
	branchDoc   agentengine.ArtifactDocument
	contractDoc agentengine.ArtifactDocument
	seedDoc     agentengine.ArtifactDocument
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
	inputs, err := findWriteCodeInputs(docs, recipe)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	branchInfo, err := common.ParseBranchArtifact([]byte(inputs.branchDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	seedBundle, err := common.ParseTestFileBundle([]byte(inputs.seedDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse seed_tests.json: %w", err), start), nil
	}
	if err := common.ValidateTestFileBundle(seedBundle, "seed_tests"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate seed_tests.json: %w", err), start), nil
	}
	if llm.IsNoop(a.llmClient) {
		fallbackWorktree := common.Worktree{
			RepoDir: branchInfo.RepoDir,
			Branch:  branchInfo.Branch,
			Path:    a.workspacePath,
		}
		return a.writeCodeFallbackBranch(ctx, task, inputs.moduleDoc, branchInfo, fallbackWorktree, start)
	}

	moduleName := moduleNameFromURI(inputs.moduleDoc.URI)
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
		if isCodingAgentFallbackAllowed() {
			fallbackWorktree := common.Worktree{
				RepoDir: branchInfo.RepoDir,
				Branch:  branchInfo.Branch,
				Path:    a.workspacePath,
			}
			return a.writeCodeFallbackBranch(ctx, task, inputs.moduleDoc, branchInfo, fallbackWorktree, start)
		}
		return a.failureFeedback(ctx, task, fmt.Errorf("create coder worktree: %w", err), start), nil
	}
	if _, err := common.MaterializeTestFiles(worktree.Path, seedBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("materialize seed tests: %w", err), start), nil
	}

	brief, err := a.prepareImplementationBrief(ctx, worktree.Path, inputs, branchInfo, seedBundle)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("prepare implementation brief: %w", err), start), nil
	}
	prompt := buildBriefPrompt(task, recipe, brief.Content)
	a.logStep(fmt.Sprintf("write_code coding agent request start: worktree=%s prompt_chars=%d", worktree.Path, len(prompt)))
	codingAgentResult, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: worktree.Path,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	})
	if err != nil {
		if isCodingAgentUnavailable(err) {
			return a.writeCodeFallbackBranch(ctx, task, inputs.moduleDoc, branchInfo, worktree, start)
		}
		return a.failureFeedback(ctx, task, fmt.Errorf("coding agent run: %w", err), start), nil
	}

	hasChanges, err := a.gitManager.HasChanges(ctx, worktree.Path)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("check code changes: %w", err), start), nil
	}
	report, err := readOpenCodeReport(worktree.Path)
	if err != nil {
		report, err = synthesizeOpenCodeReportIfNeeded(inputs.moduleDoc.URI, common.BundleTestCommand(seedBundle), hasChanges, codingAgentResult, err)
		if err != nil {
			return a.failureFeedback(ctx, task, err, start), nil
		}
		a.logStep("write_code missing .devflow/result.json; using synthetic execution report")
	}
	if !hasChanges {
		if isCodingAgentFallbackAllowed() {
			return a.writeCodeFallbackBranch(ctx, task, inputs.moduleDoc, branchInfo, worktree, start)
		}
		return a.failureFeedback(ctx, task, fmt.Errorf("coding agent completed but produced no code changes"), start), nil
	}

	testCommand := writeCodeTestCommand(seedBundle, inputs.contractDoc.Content, branchInfo, report)
	if err := validateTestCommand(testCommand); err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	if _, err := common.MaterializeTestFiles(worktree.Path, seedBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("rewrite seed tests before host test: %w", err), start), nil
	}
	testResult, err := a.gitManager.RunTestCommand(ctx, worktree.Path, testCommand)
	if err != nil {
		a.logStep(fmt.Sprintf("write_code initial test failed: command=%q exit=%d", testCommand, testResult.ExitCode))
		failureDoc := a.buildAndWriteTestFailureArtifact(ctx, inputs.moduleDoc.URI, "host_test_failure", testCommand, testResult, err, report)
		repairedReport, repairedCommand, repairedResult, repairErr := a.retryWriteCodeAfterTestFailure(ctx, task, inputs, branchInfo, worktree, seedBundle, report, testCommand, testResult, err, failureDoc)
		if repairErr != nil {
			if !isRetryableWriteCodeFailure(err) || !isRetryableWriteCodeFailure(repairErr) {
				return a.failureFeedbackWithArtifacts(ctx, task, fmt.Errorf("test command failed: %w; automatic repair failed: %v", err, repairErr), start, artifactDocURIs(failureDoc)), nil
			}
			retryBranchDoc, branchErr := a.writeRetryableBranchArtifact(ctx, inputs.moduleDoc, branchInfo, worktree, report, testCommand)
			if branchErr != nil {
				return a.failureFeedbackWithArtifacts(ctx, task, fmt.Errorf("test command failed: %w; automatic repair failed: %v; retry branch artifact error: %v", err, repairErr, branchErr), start, artifactDocURIs(failureDoc)), nil
			}
			outputs := append(artifactDocURIs(retryBranchDoc), artifactDocURIs(failureDoc)...)
			return a.bugFeedbackWithArtifacts(ctx, task, fmt.Errorf("test command failed: %w; automatic repair failed: %v", err, repairErr), start, outputs), nil
		}
		report = repairedReport
		testCommand = repairedCommand
		testResult = repairedResult
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
		ModuleTaskURI:            inputs.moduleDoc.URI,
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

func (a *Agent) executeDebug(ctx context.Context, task core.TaskMetaData, recipe Recipe) (core.TaskMetaData, error) {
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
	inputs, err := findDebugInputs(docs, recipe)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	branchInfo, err := common.ParseCoderBranchArtifact([]byte(inputs.branchDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	seedBundle, err := common.ParseTestFileBundle([]byte(inputs.seedDoc.Content))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("parse seed_tests.json: %w", err), start), nil
	}
	if err := common.ValidateTestFileBundle(seedBundle, "seed_tests"); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("validate seed_tests.json: %w", err), start), nil
	}
	var fullBundle common.TestFileBundle
	if strings.TrimSpace(inputs.fullTestsDoc.URI) != "" {
		fullBundle, err = common.ParseTestFileBundle([]byte(inputs.fullTestsDoc.Content))
		if err != nil {
			return a.failureFeedback(ctx, task, fmt.Errorf("parse full_test_files.json: %w", err), start), nil
		}
		if err := common.ValidateTestFileBundle(fullBundle, "full_test_files"); err != nil {
			return a.failureFeedback(ctx, task, fmt.Errorf("validate full_test_files.json: %w", err), start), nil
		}
	}
	worktreePath := filepath.FromSlash(strings.TrimSpace(branchInfo.Worktree))
	if worktreePath == "" {
		return a.failureFeedback(ctx, task, fmt.Errorf("debug requires coder_branch artifact worktree"), start), nil
	}
	if _, err := common.MaterializeTestFiles(worktreePath, seedBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("materialize seed tests for debug: %w", err), start), nil
	}
	if strings.TrimSpace(inputs.fullTestsDoc.URI) != "" {
		if _, err := common.MaterializeTestFiles(worktreePath, fullBundle); err != nil {
			return a.failureFeedback(ctx, task, fmt.Errorf("materialize full tests for debug: %w", err), start), nil
		}
	}

	testCommand := debugTestCommand(inputs.failureDoc.Content, branchInfo, openCodeExecutionReport{})
	brief, err := a.prepareDebugBrief(ctx, worktreePath, inputs, branchInfo, testCommand)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("prepare debug brief: %w", err), start), nil
	}
	prompt := buildDebugBriefPrompt(task, recipe, brief.Content)
	a.logStep(fmt.Sprintf("debug coding agent request start: worktree=%s prompt_chars=%d", worktreePath, len(prompt)))
	codingAgentResult, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: worktreePath,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	})
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("debug coding agent run: %w", err), start), nil
	}

	report, err := readOpenCodeReport(worktreePath)
	if err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	testCommand = debugTestCommand(inputs.failureDoc.Content, branchInfo, report)
	if err := validateTestCommand(testCommand); err != nil {
		return a.failureFeedback(ctx, task, err, start), nil
	}
	if _, err := common.MaterializeTestFiles(worktreePath, seedBundle); err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("rewrite seed tests before debug host test: %w", err), start), nil
	}
	if strings.TrimSpace(inputs.fullTestsDoc.URI) != "" {
		if _, err := common.MaterializeTestFiles(worktreePath, fullBundle); err != nil {
			return a.failureFeedback(ctx, task, fmt.Errorf("rewrite full tests before debug host test: %w", err), start), nil
		}
	}
	testResult, err := a.gitManager.RunTestCommand(ctx, worktreePath, testCommand)
	if err != nil {
		failureDoc := a.buildAndWriteTestFailureArtifact(ctx, inputs.moduleDoc.URI, "debug_test_failure", testCommand, testResult, err, report)
		return a.failureFeedbackWithArtifacts(ctx, task, fmt.Errorf("test command failed: %w", err), start, artifactDocURIs(failureDoc)), nil
	}
	hasChanges, err := a.gitManager.HasChanges(ctx, worktreePath)
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("check debug code changes: %w", err), start), nil
	}
	if !hasChanges {
		a.logStep(fmt.Sprintf("debug success without code changes: branch=%s elapsed_ms=%d", branchInfo.Branch, common.DurationMillis(time.Since(start))))
		return common.FeedbackFor(task, a.runID, a.agentID, []string{inputs.branchDoc.URI}), nil
	}

	moduleName := moduleNameFromURI(inputs.moduleDoc.URI)
	commitResult, err := a.gitManager.CommitAll(ctx, worktreePath, fmt.Sprintf("%s: debug %s", a.agentID, moduleName))
	if err != nil {
		return a.failureFeedback(ctx, task, fmt.Errorf("commit debug changes: %w", err), start), nil
	}

	summary := strings.TrimSpace(report.Summary)
	if summary == "" {
		summary = "Coder debug completed."
	}
	output := common.CoderBranchArtifact{
		SchemaVersion:            1,
		Kind:                     "coder_branch",
		RepoDir:                  branchInfo.RepoDir,
		BaseBranch:               branchInfo.BaseBranch,
		BaseCommit:               branchInfo.BaseCommit,
		Branch:                   branchInfo.Branch,
		Commit:                   commitResult.Commit,
		Worktree:                 filepath.ToSlash(worktreePath),
		ModuleTaskURI:            inputs.moduleDoc.URI,
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
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", recipe.OutputKind, moduleName+"_debug_branch.md")
	if err := a.artifactStore.Write(ctx, outputURI, content); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("debug success: branch=%s commit=%s elapsed_ms=%d", output.Branch, output.Commit, output.TotalElapsedMillis))
	return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
}

func (a *Agent) retryWriteCodeAfterTestFailure(
	ctx context.Context,
	task core.TaskMetaData,
	writeInputs writeCodeInputs,
	branchInfo common.BranchArtifact,
	worktree common.Worktree,
	seedBundle common.TestFileBundle,
	report openCodeExecutionReport,
	testCommand string,
	testResult common.CommandResult,
	testErr error,
	failureDoc agentengine.ArtifactDocument,
) (openCodeExecutionReport, string, common.CommandResult, error) {
	debugRecipe, err := GetRecipe(core.TaskOpDebug)
	if err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, err
	}
	syntheticBranch, err := common.MarshalJSONArtifact(common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       branchInfo.RepoDir,
		BaseBranch:    branchInfo.Branch,
		BaseCommit:    branchInfo.Commit,
		Branch:        worktree.Branch,
		Commit:        branchInfo.Commit,
		Worktree:      filepath.ToSlash(worktree.Path),
		ModuleTaskURI: writeInputs.moduleDoc.URI,
		Summary:       strings.TrimSpace(report.Summary),
		ChangedFiles:  append([]string(nil), report.ChangedFiles...),
		TestCommand:   testCommand,
	})
	if err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("marshal synthetic coder branch artifact: %w", err)
	}

	if strings.TrimSpace(failureDoc.URI) == "" {
		failureDoc = agentengine.ArtifactDocument{
			URI:     "runtime://coder/test_reports/host_test_failure.md",
			Kind:    "test_report",
			Content: buildHostTestFailureReport(testCommand, testResult, testErr, report),
		}
	}
	inputs := debugInputs{
		moduleDoc: writeInputs.moduleDoc,
		branchDoc: agentengine.ArtifactDocument{
			URI:     "runtime://coder/branches/current_branch.md",
			Kind:    "coder_branch",
			Content: string(syntheticBranch),
		},
		contractDoc: writeInputs.contractDoc,
		seedDoc:     writeInputs.seedDoc,
		failureDoc:  failureDoc,
	}

	debugBranchInfo := common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       branchInfo.RepoDir,
		BaseBranch:    branchInfo.Branch,
		BaseCommit:    branchInfo.Commit,
		Branch:        worktree.Branch,
		Commit:        branchInfo.Commit,
		Worktree:      filepath.ToSlash(worktree.Path),
		ModuleTaskURI: writeInputs.moduleDoc.URI,
		TestCommand:   testCommand,
		Summary:       strings.TrimSpace(report.Summary),
		ChangedFiles:  append([]string(nil), report.ChangedFiles...),
	}
	brief, err := a.prepareDebugBrief(ctx, worktree.Path, inputs, debugBranchInfo, testCommand)
	if err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("prepare automatic repair debug brief: %w", err)
	}
	prompt := buildDebugBriefPrompt(task, debugRecipe, brief.Content)
	a.logStep(fmt.Sprintf("write_code automatic repair start: worktree=%s prompt_chars=%d", worktree.Path, len(prompt)))
	repairedRun, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: worktree.Path,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	})
	if err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("automatic repair coding agent run: %w", err)
	}

	hasChanges, hasChangesErr := a.gitManager.HasChanges(ctx, worktree.Path)
	if hasChangesErr != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("check code changes after automatic repair: %w", hasChangesErr)
	}
	repairedReport, err := readOpenCodeReport(worktree.Path)
	if err != nil {
		repairedReport, err = synthesizeOpenCodeReportIfNeeded(writeInputs.moduleDoc.URI, testCommand, hasChanges, repairedRun, err)
		if err != nil {
			return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("read automatic repair report: %w", err)
		}
		a.logStep("write_code automatic repair missing .devflow/result.json; using synthetic execution report")
	}
	repairedCommand := strings.TrimSpace(testCommand)
	if repairedCommand == "" {
		repairedCommand = common.BundleTestCommand(seedBundle)
	}
	if repairedCommand == "" {
		repairedCommand = strings.TrimSpace(branchInfo.TestCommand)
	}
	if err := validateTestCommand(repairedCommand); err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, err
	}
	if _, err := common.MaterializeTestFiles(worktree.Path, seedBundle); err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("rewrite seed tests before repaired test: %w", err)
	}
	repairedResult, err := a.gitManager.RunTestCommand(ctx, worktree.Path, repairedCommand)
	if err != nil {
		return openCodeExecutionReport{}, "", common.CommandResult{}, fmt.Errorf("repaired test command failed: %w", err)
	}
	a.logStep(fmt.Sprintf("write_code automatic repair succeeded: command=%q exit=%d", repairedCommand, repairedResult.ExitCode))
	return repairedReport, repairedCommand, repairedResult, nil
}

func findWriteCodeInputs(docs []agentengine.ArtifactDocument, recipe Recipe) (writeCodeInputs, error) {
	var inputs writeCodeInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.ToLower(doc.URI))
		switch {
		case strings.Contains(uri, "/artifacts/modules/"):
			inputs.moduleDoc = doc
		case strings.Contains(uri, "/artifacts/branches/"):
			inputs.branchDoc = doc
		case strings.Contains(uri, "/artifacts/contracts/"):
			inputs.contractDoc = doc
		case strings.Contains(uri, "/artifacts/seed_tests/"):
			inputs.seedDoc = doc
		}
	}
	if strings.TrimSpace(inputs.moduleDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/modules/", recipe.Op)
	}
	if strings.TrimSpace(inputs.branchDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/branches/", recipe.Op)
	}
	if strings.TrimSpace(inputs.contractDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/contracts/", recipe.Op)
	}
	if strings.TrimSpace(inputs.seedDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/seed_tests/", recipe.Op)
	}
	return inputs, nil
}

func findDebugInputs(docs []agentengine.ArtifactDocument, recipe Recipe) (debugInputs, error) {
	var inputs debugInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.ToLower(doc.URI))
		switch {
		case strings.Contains(uri, "/artifacts/modules/"):
			inputs.moduleDoc = doc
		case strings.Contains(uri, "/artifacts/branches/"):
			inputs.branchDoc = doc
		case strings.Contains(uri, "/artifacts/contracts/"):
			inputs.contractDoc = doc
		case strings.Contains(uri, "/artifacts/seed_tests/"):
			inputs.seedDoc = doc
		case strings.Contains(uri, "/artifacts/test_data/") && path.Base(doc.URI) == "full_test_files.json":
			inputs.fullTestsDoc = doc
		case strings.Contains(uri, "/artifacts/test_reports/"):
			inputs.failureDoc = doc
		}
	}
	if strings.TrimSpace(inputs.moduleDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/modules/", recipe.Op)
	}
	if strings.TrimSpace(inputs.branchDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/branches/", recipe.Op)
	}
	if strings.TrimSpace(inputs.contractDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/contracts/", recipe.Op)
	}
	if strings.TrimSpace(inputs.seedDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/seed_tests/", recipe.Op)
	}
	if strings.TrimSpace(inputs.failureDoc.URI) == "" {
		return inputs, fmt.Errorf("%s requires an artifact URI containing /artifacts/test_reports/", recipe.Op)
	}
	return inputs, nil
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

func synthesizeOpenCodeReportIfNeeded(moduleURI string, suggestedCommand string, hasChanges bool, run common.OpenCodeResult, reportErr error) (openCodeExecutionReport, error) {
	if strictDeliveryEnabled() {
		return openCodeExecutionReport{}, reportErr
	}
	if !hasChanges || !errors.Is(reportErr, os.ErrNotExist) {
		return openCodeExecutionReport{}, reportErr
	}
	moduleName := moduleNameFromURI(moduleURI)
	if strings.TrimSpace(moduleName) == "" {
		moduleName = "module"
	}
	summary := fmt.Sprintf("Coding agent completed with code changes for %s but did not write .devflow/result.json. Using a synthesized execution report.", moduleName)
	if stderr := strings.TrimSpace(run.Stderr); stderr != "" {
		summary += " stderr: " + truncateForSummary(stderr, 280)
	} else if stdout := strings.TrimSpace(run.Stdout); stdout != "" {
		summary += " stdout: " + truncateForSummary(stdout, 280)
	}
	return openCodeExecutionReport{
		Status:       "completed",
		Summary:      summary,
		ChangedFiles: nil,
		TestCommand:  strings.TrimSpace(suggestedCommand),
		TestPassed:   false,
	}, nil
}

func truncateForSummary(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "..."
}

func isRetryableWriteCodeFailure(err error) bool {
	if err == nil {
		return true
	}
	lower := strings.ToLower(err.Error())
	nonRetryableHints := []string{
		"required executable",
		"not found in path",
		"commandnotfoundexception",
	}
	for _, hint := range nonRetryableHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	return true
}

func debugTestCommand(failureReport string, branchInfo common.CoderBranchArtifact, report openCodeExecutionReport) string {
	if command := testCommandFromFailureReport(failureReport); command != "" {
		return command
	}
	if command := strings.TrimSpace(branchInfo.TestCommand); command != "" {
		return command
	}
	if command := strings.TrimSpace(report.TestCommand); command != "" {
		return command
	}
	return "go test ./..."
}

func testCommandFromFailureReport(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !strings.EqualFold(strings.TrimSpace(line), "## Test Command") {
			continue
		}
		for _, candidate := range lines[i+1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if strings.HasPrefix(candidate, "#") {
				return ""
			}
			candidate = strings.Trim(candidate, "`")
			if strings.EqualFold(candidate, "not reported") {
				return ""
			}
			return candidate
		}
	}
	return ""
}

func writeCodeTestCommand(seedBundle common.TestFileBundle, contractContent string, branchInfo common.BranchArtifact, report openCodeExecutionReport) string {
	if command := common.BundleTestCommand(seedBundle); command != "" {
		return command
	}
	if command := officialSeedTestCommand(contractContent); command != "" {
		return command
	}
	if command := strings.TrimSpace(branchInfo.TestCommand); command != "" {
		return command
	}
	return strings.TrimSpace(report.TestCommand)
}

func officialSeedTestCommand(content string) string {
	var contract struct {
		OfficialSeedTestCommand string `json:"official_seed_test_command"`
	}
	if err := json.Unmarshal([]byte(content), &contract); err != nil {
		return ""
	}
	return strings.TrimSpace(contract.OfficialSeedTestCommand)
}

func buildHostTestFailureReport(command string, result common.CommandResult, runErr error, report openCodeExecutionReport) string {
	var builder strings.Builder
	builder.WriteString("# Host Test Failure Report\n\n")
	builder.WriteString("## Test Command\n\n")
	builder.WriteString(strings.TrimSpace(command))
	builder.WriteString("\n\n## Runner Error\n\n")
	if runErr != nil {
		builder.WriteString(strings.TrimSpace(runErr.Error()))
	} else {
		builder.WriteString("not reported")
	}
	builder.WriteString("\n\n## Exit Code\n\n")
	builder.WriteString(fmt.Sprintf("%d", result.ExitCode))
	builder.WriteString("\n\n## Stdout\n\n```\n")
	builder.WriteString(strings.TrimSpace(result.Stdout))
	builder.WriteString("\n```\n\n## Stderr\n\n```\n")
	builder.WriteString(strings.TrimSpace(result.Stderr))
	builder.WriteString("\n```\n\n## Previous Coder Summary\n\n")
	if strings.TrimSpace(report.Summary) == "" {
		builder.WriteString("not reported")
	} else {
		builder.WriteString(strings.TrimSpace(report.Summary))
	}
	builder.WriteString("\n")
	return builder.String()
}

func (a *Agent) buildAndWriteTestFailureArtifact(
	ctx context.Context,
	moduleURI string,
	suffix string,
	command string,
	result common.CommandResult,
	runErr error,
	report openCodeExecutionReport,
) agentengine.ArtifactDocument {
	doc := agentengine.ArtifactDocument{
		URI:     "runtime://coder/test_reports/host_test_failure.md",
		Kind:    "test_report",
		Content: buildHostTestFailureReport(command, result, runErr, report),
	}
	moduleName := moduleNameFromURI(moduleURI)
	if moduleName == "" {
		moduleName = "module"
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_reports", moduleName+"_"+suffix+".md")
	doc.URI = outputURI
	if a.artifactStore == nil {
		return doc
	}
	if err := a.artifactStore.Write(ctx, outputURI, []byte(doc.Content)); err != nil {
		a.logStep(fmt.Sprintf("write test failure artifact failed: %v", err))
		doc.URI = "runtime://coder/test_reports/host_test_failure.md"
		return doc
	}
	a.logStep(fmt.Sprintf("test failure artifact written: %s", outputURI))
	return doc
}

func validateTestCommand(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("test_command is empty")
	}
	lower := strings.ToLower(command)
	proseHints := []string{
		" smoke test covering ",
		" covering ",
		" manual test ",
		" validates ",
		" verifies ",
		" using ",
		" and corrupt ",
	}
	for _, hint := range proseHints {
		if strings.Contains(lower, hint) {
			return fmt.Errorf("invalid test_command %q: must be executable shell command only; put explanations in summary/evidence", command)
		}
	}
	return nil
}

func (a *Agent) failureFeedback(ctx context.Context, task core.TaskMetaData, cause error, start time.Time) core.TaskMetaData {
	return a.feedbackWithArtifacts(ctx, task, cause, start, nil, core.TaskResultCodeFail)
}

func (a *Agent) failureFeedbackWithArtifacts(ctx context.Context, task core.TaskMetaData, cause error, start time.Time, extraOutputs []string) core.TaskMetaData {
	return a.feedbackWithArtifacts(ctx, task, cause, start, extraOutputs, core.TaskResultCodeFail)
}

func (a *Agent) bugFeedbackWithArtifacts(ctx context.Context, task core.TaskMetaData, cause error, start time.Time, extraOutputs []string) core.TaskMetaData {
	return a.feedbackWithArtifacts(ctx, task, cause, start, extraOutputs, core.TaskResultCodeBug)
}

func (a *Agent) feedbackWithArtifacts(ctx context.Context, task core.TaskMetaData, cause error, start time.Time, extraOutputs []string, result core.TaskResultCode) core.TaskMetaData {
	content, _ := common.MarshalJSONArtifact(map[string]any{
		"schema_version":      1,
		"kind":                "coder_failure",
		"op":                  task.Op,
		"error":               cause.Error(),
		"total_elapsed_ms":    common.DurationMillis(time.Since(start)),
		"input_artifact_uris": task.ArtifactURIs,
	})
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "failures", "write_code_failure.md")
	outputs := append([]string(nil), nonEmptyStrings(extraOutputs)...)
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
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: outputs,
		Result:       result,
	}
}

func artifactDocURIs(docs ...agentengine.ArtifactDocument) []string {
	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		if uri := strings.TrimSpace(doc.URI); uri != "" {
			out = append(out, uri)
		}
	}
	return out
}

func nonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (a *Agent) writeRetryableBranchArtifact(
	ctx context.Context,
	moduleDoc agentengine.ArtifactDocument,
	branchInfo common.BranchArtifact,
	worktree common.Worktree,
	report openCodeExecutionReport,
	testCommand string,
) (agentengine.ArtifactDocument, error) {
	moduleName := moduleNameFromURI(moduleDoc.URI)
	if moduleName == "" {
		moduleName = "module"
	}
	artifact := common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       branchInfo.RepoDir,
		BaseBranch:    branchInfo.Branch,
		BaseCommit:    branchInfo.Commit,
		Branch:        worktree.Branch,
		Commit:        branchInfo.Commit,
		Worktree:      filepath.ToSlash(worktree.Path),
		ModuleTaskURI: moduleDoc.URI,
		Summary:       strings.TrimSpace(report.Summary),
		ChangedFiles:  append([]string(nil), report.ChangedFiles...),
		TestCommand:   strings.TrimSpace(testCommand),
	}
	content, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		return agentengine.ArtifactDocument{}, fmt.Errorf("marshal retryable coder branch artifact: %w", err)
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "branches", moduleName+"_failed_branch.md")
	if a.artifactStore == nil {
		return agentengine.ArtifactDocument{
			URI:     "runtime://coder/branches/failed_branch.md",
			Kind:    "coder_branch",
			Content: string(content),
		}, nil
	}
	if err := a.artifactStore.Write(ctx, outputURI, content); err != nil {
		return agentengine.ArtifactDocument{}, fmt.Errorf("write retryable coder branch artifact: %w", err)
	}
	return agentengine.ArtifactDocument{
		URI:     outputURI,
		Kind:    "coder_branch",
		Content: string(content),
	}, nil
}

func (a *Agent) writeCodeFallbackBranch(ctx context.Context, task core.TaskMetaData, moduleDoc agentengine.ArtifactDocument, branchInfo common.BranchArtifact, worktree common.Worktree, start time.Time) (core.TaskMetaData, error) {
	moduleName := moduleNameFromURI(moduleDoc.URI)
	legacyFeedback, err := a.writeCodeArtifact(ctx, task)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	output := common.CoderBranchArtifact{
		SchemaVersion:            1,
		Kind:                     "coder_branch",
		RepoDir:                  branchInfo.RepoDir,
		BaseBranch:               branchInfo.Branch,
		BaseCommit:               branchInfo.Commit,
		Branch:                   worktree.Branch,
		Commit:                   branchInfo.Commit,
		Worktree:                 filepath.ToSlash(worktree.Path),
		ModuleTaskURI:            moduleDoc.URI,
		Summary:                  "Fallback coder branch artifact generated because OpenCode was unavailable or produced no product change.",
		ChangedFiles:             nil,
		TestCommand:              strings.TrimSpace(branchInfo.TestCommand),
		CodingAgentElapsedMillis: 0,
		TestElapsedMillis:        0,
		CommitElapsedMillis:      0,
		TotalElapsedMillis:       common.DurationMillis(time.Since(start)),
	}
	content, err := common.MarshalJSONArtifact(output)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "branches", moduleName+"_branch.md")
	if err := a.artifactStore.Write(ctx, outputURI, content); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("write_code fallback branch artifact written: %s", outputURI))
	uris := append([]string{outputURI}, legacyFeedback.ArtifactURIs...)
	return common.FeedbackFor(task, a.runID, a.agentID, uris), nil
}

func isCodingAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "coding agent executable not found") ||
		strings.Contains(message, "opencode") && strings.Contains(message, "not found")
}

func isCodingAgentFallbackAllowed() bool {
	if strictDeliveryEnabled() {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_DISABLE_CODER_FALLBACK")))
	return value != "1" && value != "true"
}

func strictDeliveryEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_STRICT_DELIVERY")))
	return value == "1" || value == "true"
}
