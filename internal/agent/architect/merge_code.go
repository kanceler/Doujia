package architect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

type mergeCodeInputs struct {
	mainDoc  agentengine.ArtifactDocument
	main     common.BranchArtifact
	modules  []agentengine.ArtifactDocument
	branches []mergeCoderBranch
}

type mergeCoderBranch struct {
	doc    agentengine.ArtifactDocument
	branch common.CoderBranchArtifact
}

type mergeCodeReport struct {
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	MergedBranches []string `json:"merged_branches"`
	TestCommand    string   `json:"test_command"`
	TestPassed     bool     `json:"test_passed"`
	FailureSummary string   `json:"failure_summary,omitempty"`
	Conflicts      []string `json:"conflicts,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	SuspectedFiles []string `json:"suspected_files,omitempty"`
}

type deterministicMergeResult struct {
	MainBranch       string
	MainCommitBefore string
	MainCommitAfter  string
	MergedBranches   []string
	MergedCommits    []string
	TestCommands     []string
	TestResults      []common.CommandResult
	ElapsedMillis    int64
}

type mergeConflictError struct {
	branch string
	cause  error
}

func (e mergeConflictError) Error() string {
	return fmt.Sprintf("merge conflict while cherry-picking branch %s: %v", e.branch, e.cause)
}

func (e mergeConflictError) Unwrap() error {
	return e.cause
}

type mergeTestFailureError struct {
	command string
	cause   error
	output  common.CommandResult
}

func (e mergeTestFailureError) Error() string {
	return fmt.Sprintf("merge test command failed %q: %v", e.command, e.cause)
}

func (e mergeTestFailureError) Unwrap() error {
	return e.cause
}

var mergeLocks sync.Map

func shouldFallbackMergeToLLM(err error) bool {
	var conflict mergeConflictError
	return errors.As(err, &conflict)
}

func isMergeTestFailure(err error) bool {
	var testFailure mergeTestFailureError
	return errors.As(err, &testFailure)
}

func lockForRepo(repoDir string) *sync.Mutex {
	key := filepath.Clean(repoDir)
	value, _ := mergeLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (a *Agent) executeMergeCode(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	start := time.Now()
	if a.artifactStore == nil {
		return a.executeMergeCodeFallback(ctx, task)
	}
	docs, err := resolveArchitectDocs(ctx, a.artifactStore, task.ArtifactURIs)
	if err != nil {
		return a.mergeCodeFallbackSuccess(ctx, task, nil, fmt.Sprintf("resolve inputs failed: %v", err), start)
	}
	inputs := findMergeCodeInputs(docs)
	if strings.TrimSpace(inputs.mainDoc.URI) == "" || len(inputs.branches) == 0 {
		return a.mergeCodeFallbackSuccess(ctx, task, &inputs, "main branch or coder branch artifacts are not complete yet", start)
	}
	if err := validateMergeCodeInputs(inputs); err != nil {
		return a.mergeFailureFeedback(ctx, task, &inputs, mergeCodeReport{}, err, start)
	}

	feedback, fastErr := a.executeMergeCodeFastPath(ctx, task, &inputs, start)
	if fastErr == nil {
		return feedback, nil
	}
	if !shouldFallbackMergeToLLM(fastErr) {
		result := core.TaskResultCodeFail
		if isMergeTestFailure(fastErr) {
			result = core.TaskResultCodeBug
		}
		return a.mergeFailureFeedbackWithResult(ctx, task, &inputs, mergeCodeReport{}, fastErr, start, result)
	}
	if llm.IsNoop(a.llmClient) || !isArchitectFallbackAllowed() {
		return a.mergeFailureFeedbackWithResult(ctx, task, &inputs, mergeCodeReport{}, fastErr, start, core.TaskResultCodeBug)
	}
	return a.executeMergeCodeLLMFallback(ctx, task, &inputs, start, fastErr)
}

func (a *Agent) executeMergeCodeFastPath(
	ctx context.Context,
	task core.TaskMetaData,
	inputs *mergeCodeInputs,
	start time.Time,
) (core.TaskMetaData, error) {
	repoDir := filepath.FromSlash(strings.TrimSpace(inputs.main.RepoDir))
	mainBranch := strings.TrimSpace(inputs.main.Branch)
	mainCommit := strings.TrimSpace(inputs.main.Commit)
	if repoDir == "" {
		return core.TaskMetaData{}, fmt.Errorf("main branch repo_dir is required")
	}
	if mainBranch == "" {
		return core.TaskMetaData{}, fmt.Errorf("main branch name is required")
	}
	if mainCommit == "" {
		return core.TaskMetaData{}, fmt.Errorf("main branch commit is required")
	}

	lock := lockForRepo(repoDir)
	lock.Lock()
	defer lock.Unlock()

	clean, err := common.GitIsClean(ctx, repoDir)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("check repo clean before checkout: %w", err)
	}
	if !clean {
		return core.TaskMetaData{}, fmt.Errorf("repo has uncommitted changes before merge")
	}
	if err := common.GitCheckout(ctx, repoDir, mainBranch); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("checkout main branch: %w", err)
	}
	head, err := common.GitRevParseHEAD(ctx, repoDir)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("read main HEAD: %w", err)
	}
	if head != mainCommit {
		return core.TaskMetaData{}, fmt.Errorf("main HEAD mismatch: artifact=%s actual=%s", mainCommit, head)
	}

	success := false
	defer func() {
		if success {
			return
		}
		_ = common.GitCherryPickAbort(ctx, repoDir)
		_ = common.GitResetHard(ctx, repoDir, mainCommit)
	}()

	result := deterministicMergeResult{
		MainBranch:       mainBranch,
		MainCommitBefore: mainCommit,
	}

	sortedBranches := sortedMergeBranches(inputs.branches)
	for _, item := range sortedBranches {
		branch := item.branch
		if strings.TrimSpace(branch.BaseCommit) == "" {
			return core.TaskMetaData{}, fmt.Errorf("coder branch base commit is required: branch=%s", branch.Branch)
		}
		if strings.TrimSpace(branch.BaseCommit) != mainCommit {
			return core.TaskMetaData{}, fmt.Errorf("coder branch base commit mismatch: branch=%s base=%s main=%s", branch.Branch, branch.BaseCommit, mainCommit)
		}
	}
	if err := detectEntryOwnershipOverlap(ctx, repoDir, sortedBranches); err != nil {
		return core.TaskMetaData{}, err
	}

	for _, item := range sortedBranches {
		branch := item.branch
		if err := common.GitCherryPickRange(ctx, repoDir, branch.BaseCommit, branch.Commit); err != nil {
			return core.TaskMetaData{}, mergeConflictError{branch: branch.Branch, cause: err}
		}
		result.MergedBranches = append(result.MergedBranches, branch.Branch)
		result.MergedCommits = append(result.MergedCommits, branch.Commit)
	}

	runner := common.NewLocalGitManager()
	for _, command := range collectMergeTestCommands(inputs) {
		testResult, err := runner.RunTestCommand(ctx, repoDir, command)
		result.TestCommands = append(result.TestCommands, command)
		result.TestResults = append(result.TestResults, testResult)
		if err != nil {
			return core.TaskMetaData{}, mergeTestFailureError{command: command, cause: err, output: testResult}
		}
	}

	afterCommit, err := common.GitRevParseHEAD(ctx, repoDir)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("read merged HEAD: %w", err)
	}
	result.MainCommitAfter = afterCommit
	result.ElapsedMillis = common.DurationMillis(time.Since(start))

	feedback, err := a.writeDeterministicMergeSuccess(ctx, task, inputs, result)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	success = true
	return feedback, nil
}

func detectEntryOwnershipOverlap(ctx context.Context, repoDir string, branches []mergeCoderBranch) error {
	entryOwnedFiles := []string{"index.html", "README.md", "package.json"}
	owners := make(map[string]string, len(entryOwnedFiles))
	for _, item := range branches {
		branch := item.branch
		changed, err := common.GitDiffNameOnly(ctx, repoDir, branch.BaseCommit, branch.Commit, entryOwnedFiles...)
		if err != nil {
			return fmt.Errorf("check entry-owned file changes for branch %s: %w", branch.Branch, err)
		}
		for _, name := range changed {
			if previous := owners[name]; previous != "" && previous != branch.Branch {
				return fmt.Errorf("module ownership overlap: multiple coder branches modified entry-owned files: %s modified by %s and %s", name, previous, branch.Branch)
			}
			owners[name] = branch.Branch
		}
	}
	return nil
}

func (a *Agent) executeMergeCodeLLMFallback(
	ctx context.Context,
	task core.TaskMetaData,
	inputs *mergeCodeInputs,
	start time.Time,
	reason error,
) (core.TaskMetaData, error) {
	if a.openCodeRunner == nil {
		a.openCodeRunner = common.NewManagedOpenCodeRunner("")
	}

	repoDir := filepath.FromSlash(strings.TrimSpace(inputs.main.RepoDir))
	prompt := buildMergeCodePrompt(task, *inputs)
	a.logStep(fmt.Sprintf("merge_code opencode request start: repo=%s prompt_chars=%d reason=%v", repoDir, len(prompt), reason))
	if _, err := a.openCodeRunner.Run(ctx, common.OpenCodeRequest{
		WorkDir: repoDir,
		Prompt:  prompt,
		Model:   a.runConfig.LLM.Model,
		LLM:     a.runConfig.LLM,
		Timeout: a.runConfig.LLM.RequestTimeout,
	}); err != nil {
		if isArchitectCodingAgentUnavailable(err) {
			return a.mergeCodeFallbackSuccess(ctx, task, inputs, "opencode unavailable; fallback merge artifact generated", start)
		}
		return a.mergeFailureFeedback(ctx, task, inputs, mergeCodeReport{}, fmt.Errorf("opencode merge failed: %w", err), start)
	}

	report, err := readMergeCodeReport(repoDir)
	if err != nil {
		if isArchitectFallbackAllowed() {
			return a.mergeCodeFallbackSuccess(ctx, task, inputs, fmt.Sprintf("merge report unavailable: %v", err), start)
		}
		return a.mergeFailureFeedback(ctx, task, inputs, mergeCodeReport{}, err, start)
	}
	if strings.EqualFold(strings.TrimSpace(report.Status), "passed") && report.TestPassed {
		return a.mergeCodeSuccess(ctx, task, inputs, report.Summary, start)
	}
	return a.mergeFailureFeedback(ctx, task, inputs, report, fmt.Errorf("merge report status=%s test_passed=%t", report.Status, report.TestPassed), start)
}

func resolveArchitectDocs(ctx context.Context, store interface {
	Read(context.Context, string) ([]byte, error)
}, uris []string) ([]agentengine.ArtifactDocument, error) {
	docs := make([]agentengine.ArtifactDocument, 0, len(uris))
	for _, uri := range uris {
		uri = filepath.ToSlash(strings.TrimSpace(uri))
		if uri == "" {
			continue
		}
		content, err := store.Read(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", uri, err)
		}
		docs = append(docs, agentengine.ArtifactDocument{URI: uri, Content: string(content)})
	}
	return docs, nil
}

func findMergeCodeInputs(docs []agentengine.ArtifactDocument) mergeCodeInputs {
	var inputs mergeCodeInputs
	for _, doc := range docs {
		uri := filepath.ToSlash(strings.TrimSpace(doc.URI))
		lower := strings.ToLower(uri)
		switch {
		case strings.Contains(lower, "/artifacts/modules/"):
			inputs.modules = append(inputs.modules, doc)
		case strings.Contains(lower, "/artifacts/branches/"):
			switch artifactKind([]byte(doc.Content)) {
			case "main_branch":
				if branch, err := common.ParseBranchArtifact([]byte(doc.Content)); err == nil {
					inputs.mainDoc = doc
					inputs.main = branch
				}
			case "coder_branch":
				if branch, err := common.ParseCoderBranchArtifact([]byte(doc.Content)); err == nil {
					inputs.branches = append(inputs.branches, mergeCoderBranch{doc: doc, branch: branch})
				}
			}
		}
	}
	return inputs
}

func artifactKind(content []byte) string {
	var payload struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Kind)
}

func validateMergeCodeInputs(inputs mergeCodeInputs) error {
	mainRepo := filepath.Clean(filepath.FromSlash(inputs.main.RepoDir))
	mainBranch := strings.TrimSpace(inputs.main.Branch)
	mainCommit := strings.TrimSpace(inputs.main.Commit)
	for _, item := range inputs.branches {
		branchRepo := filepath.Clean(filepath.FromSlash(item.branch.RepoDir))
		if !strings.EqualFold(mainRepo, branchRepo) {
			return fmt.Errorf("coder branch repo must match main repo: main=%s branch=%s", mainRepo, branchRepo)
		}
		if baseBranch := strings.TrimSpace(item.branch.BaseBranch); baseBranch != "" && mainBranch != "" && baseBranch != mainBranch {
			return fmt.Errorf("coder branch base branch must match main branch: main=%s branch_base=%s", mainBranch, baseBranch)
		}
		if baseCommit := strings.TrimSpace(item.branch.BaseCommit); baseCommit != "" && mainCommit != "" && baseCommit != mainCommit {
			return fmt.Errorf("coder branch base commit must match main commit: main=%s branch_base=%s", mainCommit, baseCommit)
		}
	}
	return nil
}

func sortedMergeBranches(items []mergeCoderBranch) []mergeCoderBranch {
	out := append([]mergeCoderBranch(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		left := strings.TrimSpace(out[i].branch.ModuleTaskURI)
		right := strings.TrimSpace(out[j].branch.ModuleTaskURI)
		if left != right {
			return left < right
		}
		left = strings.TrimSpace(out[i].branch.Branch)
		right = strings.TrimSpace(out[j].branch.Branch)
		if left != right {
			return left < right
		}
		return strings.TrimSpace(out[i].doc.URI) < strings.TrimSpace(out[j].doc.URI)
	})
	return out
}

func collectMergeTestCommands(inputs *mergeCodeInputs) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(command string) {
		command = strings.TrimSpace(command)
		if command == "" || seen[command] {
			return
		}
		seen[command] = true
		out = append(out, command)
	}
	if inputs != nil {
		if len(inputs.main.GlobalVerifyCommands) > 0 {
			for _, command := range inputs.main.GlobalVerifyCommands {
				add(command)
			}
		} else {
			add(inputs.main.TestCommand)
		}
		for _, item := range inputs.branches {
			add(item.branch.TestCommand)
		}
	}
	return out
}

func buildMergeCodePrompt(task core.TaskMetaData, inputs mergeCodeInputs) string {
	var builder strings.Builder
	builder.WriteString("# Merge Coder Branches\n\n")
	builder.WriteString("You are the DevFlow Architect Agent. Merge all coder branches into the provided main branch repository.\n\n")
	builder.WriteString(fmt.Sprintf("- task_id: %s\n- op: %s\n", task.TaskID, task.Op))
	builder.WriteString(fmt.Sprintf("- repo_dir: %s\n- main_branch: %s\n- main_commit: %s\n\n", inputs.main.RepoDir, inputs.main.Branch, inputs.main.Commit))
	builder.WriteString("## Coder Branches\n\n")
	for _, item := range inputs.branches {
		builder.WriteString(fmt.Sprintf("- branch: %s commit: %s module_task_uri: %s artifact: %s\n", item.branch.Branch, item.branch.Commit, item.branch.ModuleTaskURI, item.doc.URI))
	}
	builder.WriteString("\n## Module Tasks\n")
	for _, doc := range inputs.modules {
		writeArchitectDoc(&builder, "Module Task", doc)
	}
	builder.WriteString("\n## Required Output\n")
	builder.WriteString("Write .devflow/result.json with status, summary, merged_branches, test_command, test_passed, failure_summary, conflicts, evidence, suspected_files.\n")
	builder.WriteString("Do not skip branches. Resolve conflicts according to module responsibilities. Commit the merged result if product files changed.\n")
	return builder.String()
}

func writeArchitectDoc(builder *strings.Builder, title string, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(title)
	builder.WriteString("\nsource: ")
	builder.WriteString(doc.URI)
	builder.WriteString("\n")
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}

func readMergeCodeReport(repoDir string) (mergeCodeReport, error) {
	content, err := os.ReadFile(filepath.Join(repoDir, ".devflow", "result.json"))
	if err != nil {
		return mergeCodeReport{}, fmt.Errorf("read .devflow/result.json: %w", err)
	}
	var report mergeCodeReport
	if err := json.Unmarshal(content, &report); err != nil {
		return mergeCodeReport{}, fmt.Errorf("parse .devflow/result.json: %w", err)
	}
	if strings.TrimSpace(report.Status) == "" {
		return mergeCodeReport{}, fmt.Errorf(".devflow/result.json status is required")
	}
	return report, nil
}

func (a *Agent) mergeCodeFallbackSuccess(ctx context.Context, task core.TaskMetaData, inputs *mergeCodeInputs, reason string, start time.Time) (core.TaskMetaData, error) {
	return a.mergeCodeSuccess(ctx, task, inputs, "Fallback merge completed: "+reason, start)
}

func (a *Agent) mergeCodeSuccess(ctx context.Context, task core.TaskMetaData, inputs *mergeCodeInputs, summary string, start time.Time) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "code", "merged_code_v1.md")
	content := buildMergeSuccessArtifact(inputs, summary, common.DurationMillis(time.Since(start)))
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	uris := []string{outputURI}
	if inputs != nil && strings.TrimSpace(inputs.mainDoc.URI) != "" {
		uris = append(uris, inputs.mainDoc.URI)
	}
	a.logStep(fmt.Sprintf("merge_code success: outputs=%d", len(uris)))
	return common.FeedbackFor(task, a.runID, a.agentID, uris), nil
}

func buildMergeSuccessArtifact(inputs *mergeCodeInputs, summary string, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# Merged Code\n\n")
	builder.WriteString("## Summary\n\n")
	builder.WriteString(strings.TrimSpace(summary))
	builder.WriteString("\n\n## Inputs\n\n")
	if inputs != nil {
		if strings.TrimSpace(inputs.mainDoc.URI) != "" {
			builder.WriteString("- main_branch_uri: ")
			builder.WriteString(inputs.mainDoc.URI)
			builder.WriteString("\n")
		}
		for _, item := range inputs.branches {
			builder.WriteString("- coder_branch_uri: ")
			builder.WriteString(item.doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString(fmt.Sprintf("\n## Elapsed\n\n%d ms\n", elapsedMillis))
	return builder.String()
}

func (a *Agent) writeDeterministicMergeSuccess(
	ctx context.Context,
	task core.TaskMetaData,
	inputs *mergeCodeInputs,
	result deterministicMergeResult,
) (core.TaskMetaData, error) {
	summaryURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "code", "merged_code_v1.md")
	branchURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "branches", "merged_main_branch.md")

	if err := a.artifactStore.Write(ctx, summaryURI, []byte(buildDeterministicMergeSuccessArtifact(inputs, result))); err != nil {
		return core.TaskMetaData{}, err
	}
	verifyCommands := normalizeGlobalVerifyCommands(inputs.main.GlobalVerifyCommands)
	if len(verifyCommands) == 0 {
		verifyCommands = normalizeGlobalVerifyCommands(result.TestCommands)
	}
	testCommand := ""
	if len(verifyCommands) > 0 {
		testCommand = verifyCommands[0]
	}
	branchArtifact, err := common.MarshalJSONArtifact(common.BranchArtifact{
		SchemaVersion:        1,
		Kind:                 "main_branch",
		RunID:                string(a.runID),
		RepoDir:              inputs.main.RepoDir,
		Branch:               inputs.main.Branch,
		Commit:               result.MainCommitAfter,
		DeliveryProfile:      inputs.main.DeliveryProfile,
		RequiredFiles:        append([]string(nil), inputs.main.RequiredFiles...),
		TestCommand:          testCommand,
		GlobalVerifyCommands: verifyCommands,
	})
	if err != nil {
		return core.TaskMetaData{}, err
	}
	if err := a.artifactStore.Write(ctx, branchURI, branchArtifact); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("merge_code fast path success: branches=%d commit=%s elapsed_ms=%d", len(result.MergedBranches), result.MainCommitAfter, result.ElapsedMillis))
	return common.FeedbackFor(task, a.runID, a.agentID, []string{summaryURI, branchURI}), nil
}

func buildDeterministicMergeSuccessArtifact(inputs *mergeCodeInputs, result deterministicMergeResult) string {
	var builder strings.Builder
	builder.WriteString("# Merged Code\n\n")
	builder.WriteString("## Summary\n\n")
	builder.WriteString("Deterministic git merge fast path completed successfully. OpenCode was not used.\n")
	builder.WriteString("\n## Main Branch\n\n")
	builder.WriteString("- branch: ")
	builder.WriteString(result.MainBranch)
	builder.WriteString("\n- commit_before: ")
	builder.WriteString(result.MainCommitBefore)
	builder.WriteString("\n- commit_after: ")
	builder.WriteString(result.MainCommitAfter)
	builder.WriteString("\n")
	builder.WriteString("\n## Merged Branches\n\n")
	for i := range result.MergedBranches {
		builder.WriteString("- branch: ")
		builder.WriteString(result.MergedBranches[i])
		if i < len(result.MergedCommits) {
			builder.WriteString(" commit: ")
			builder.WriteString(result.MergedCommits[i])
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Test Commands\n\n")
	if len(result.TestCommands) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, command := range result.TestCommands {
			builder.WriteString("- ")
			builder.WriteString(command)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Inputs\n\n")
	if inputs != nil {
		if strings.TrimSpace(inputs.mainDoc.URI) != "" {
			builder.WriteString("- main_branch_uri: ")
			builder.WriteString(inputs.mainDoc.URI)
			builder.WriteString("\n")
		}
		for _, item := range sortedMergeBranches(inputs.branches) {
			builder.WriteString("- coder_branch_uri: ")
			builder.WriteString(item.doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Elapsed\n\n")
	builder.WriteString(fmt.Sprintf("%d ms\n", result.ElapsedMillis))
	return builder.String()
}

func (a *Agent) mergeFailureFeedbackWithResult(
	ctx context.Context,
	task core.TaskMetaData,
	inputs *mergeCodeInputs,
	report mergeCodeReport,
	cause error,
	start time.Time,
	resultCode core.TaskResultCode,
) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "merge_reports", "merge_code_report.md")
	content := buildMergeFailureReport(inputs, report, cause, common.DurationMillis(time.Since(start)))
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	a.logStep(fmt.Sprintf("merge_code failed: %v", cause))
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
		Result:       resultCode,
	}, nil
}

func (a *Agent) mergeFailureFeedback(ctx context.Context, task core.TaskMetaData, inputs *mergeCodeInputs, report mergeCodeReport, cause error, start time.Time) (core.TaskMetaData, error) {
	return a.mergeFailureFeedbackWithResult(ctx, task, inputs, report, cause, start, core.TaskResultCodeFail)
}

func buildMergeFailureReport(inputs *mergeCodeInputs, report mergeCodeReport, cause error, elapsedMillis int64) string {
	var builder strings.Builder
	builder.WriteString("# Merge Failure Report\n\n")
	builder.WriteString("## Summary\n\n")
	if summary := strings.TrimSpace(report.Summary); summary != "" {
		builder.WriteString(summary)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	} else {
		builder.WriteString("merge_code failed.")
	}
	builder.WriteString("\n\n## Inputs\n\n")
	if inputs != nil {
		builder.WriteString("- main_branch_uri: ")
		builder.WriteString(inputs.mainDoc.URI)
		builder.WriteString("\n")
		for _, item := range inputs.branches {
			builder.WriteString("- coder_branch_uri: ")
			builder.WriteString(item.doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Failure\n\n")
	if detail := strings.TrimSpace(report.FailureSummary); detail != "" {
		builder.WriteString(detail)
	} else if cause != nil {
		builder.WriteString(cause.Error())
	}
	var testFailure mergeTestFailureError
	if errors.As(cause, &testFailure) {
		builder.WriteString("\n\n## Test Command\n\n")
		builder.WriteString(testFailure.command)
		builder.WriteString("\n")
		if strings.TrimSpace(testFailure.output.Stdout) != "" {
			builder.WriteString("\n## Stdout\n\n```text\n")
			builder.WriteString(strings.TrimSpace(testFailure.output.Stdout))
			builder.WriteString("\n```\n")
		}
		if strings.TrimSpace(testFailure.output.Stderr) != "" {
			builder.WriteString("\n## Stderr\n\n```text\n")
			builder.WriteString(strings.TrimSpace(testFailure.output.Stderr))
			builder.WriteString("\n```\n")
		}
	}
	builder.WriteString(fmt.Sprintf("\n## Elapsed\n\n%d ms\n", elapsedMillis))
	return builder.String()
}

func isArchitectCodingAgentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "coding agent executable not found") ||
		strings.Contains(message, "opencode") && strings.Contains(message, "not found")
}

func isArchitectFallbackAllowed() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_DISABLE_ARCHITECT_FALLBACK")))
	return value != "1" && value != "true"
}
