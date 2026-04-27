package coder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/core"
)

type debugMemoryArtifactStore struct {
	reads  map[string]string
	writes map[string]string
}

func (s *debugMemoryArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.reads[uri]), nil
}

func (s *debugMemoryArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[uri] = string(content)
	return nil
}

type debugFakeOpenCodeRunner struct {
	report  openCodeExecutionReport
	workDir string
	prompt  string
	err     error
}

func (r *debugFakeOpenCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
	r.workDir = req.WorkDir
	r.prompt = req.Prompt
	if r.err != nil {
		return common.OpenCodeResult{ExitCode: 1}, r.err
	}
	if err := os.MkdirAll(filepath.Join(req.WorkDir, ".devflow"), 0o755); err != nil {
		return common.OpenCodeResult{}, err
	}
	raw, err := json.Marshal(r.report)
	if err != nil {
		return common.OpenCodeResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, ".devflow", "result.json"), raw, 0o644); err != nil {
		return common.OpenCodeResult{}, err
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

type debugFakeGitManager struct {
	hasChanges        bool
	testCommand       string
	commitMessage     string
	commitCalled      bool
	createWorktreeHit bool
}

func (m *debugFakeGitManager) CreateCoderWorktree(context.Context, common.CreateCoderWorktreeRequest) (common.Worktree, error) {
	m.createWorktreeHit = true
	return common.Worktree{}, nil
}

func (m *debugFakeGitManager) HasChanges(context.Context, string) (bool, error) {
	return m.hasChanges, nil
}

func (m *debugFakeGitManager) RunTestCommand(_ context.Context, _ string, command string) (common.CommandResult, error) {
	m.testCommand = command
	return common.CommandResult{Duration: time.Millisecond}, nil
}

func (m *debugFakeGitManager) CommitAll(_ context.Context, _ string, message string) (common.CommitResult, error) {
	m.commitCalled = true
	m.commitMessage = message
	return common.CommitResult{Commit: "fix456", Duration: time.Millisecond}, nil
}

func TestExecuteDebugRepairsExistingCoderBranchAndWritesUpdatedBranchArtifact(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	reportURI := "projects/run1/agents/tester_01/artifacts/test_reports/tester_01_test_report.md"
	worktreePath := t.TempDir()
	store := &debugMemoryArtifactStore{
		reads: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI: debugCoderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       "C:/repo/project",
				BaseBranch:    "main",
				BaseCommit:    "base123",
				Branch:        "devflow/run1/coder_01/task/module",
				Commit:        "coder123",
				Worktree:      filepath.ToSlash(worktreePath),
				ModuleTaskURI: moduleTaskURI,
				TestCommand:   "go test ./...",
			}),
			reportURI: "# Test Failure Report\n\n## Test Command\n\ngo test ./internal/foo\n\n## Failure Summary\n\nexpected score update failed\n",
		},
	}
	runner := &debugFakeOpenCodeRunner{report: openCodeExecutionReport{
		Status:       "fixed",
		Summary:      "fixed score update",
		ChangedFiles: []string{"internal/foo/score.go"},
		TestCommand:  "go test ./internal/foo",
		TestPassed:   true,
	}}
	git := &debugFakeGitManager{hasChanges: true}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("coder_01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_debug_01"),
		AgentID:      core.AgentID("coder_01"),
		Op:           "debug",
		ArtifactURIs: []string{moduleTaskURI, branchURI, reportURI},
	})
	if err != nil {
		t.Fatalf("execute debug: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := runner.workDir, worktreePath; got != want {
		t.Fatalf("opencode workdir = %q, want original worktree %q", got, want)
	}
	if git.createWorktreeHit {
		t.Fatal("debug should repair the existing coder branch worktree, not create a new worktree")
	}
	if got, want := git.testCommand, "go test ./internal/foo"; got != want {
		t.Fatalf("test command = %q, want %q", got, want)
	}
	if !git.commitCalled || !strings.Contains(git.commitMessage, "debug") {
		t.Fatalf("commit message = %q, want debug commit", git.commitMessage)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("debug output artifact count = %d, want %d", got, want)
	}
	if feedback.ArtifactURIs[0] == branchURI {
		t.Fatal("debug with code changes should write an updated branch artifact")
	}
	var output common.CoderBranchArtifact
	if err := json.Unmarshal([]byte(store.writes[feedback.ArtifactURIs[0]]), &output); err != nil {
		t.Fatalf("parse debug branch artifact: %v", err)
	}
	if output.Branch != "devflow/run1/coder_01/task/module" || output.Commit != "fix456" {
		t.Fatalf("output branch/commit = %s/%s, want original branch with new commit", output.Branch, output.Commit)
	}
	if output.Worktree != filepath.ToSlash(worktreePath) {
		t.Fatalf("output worktree = %q, want %q", output.Worktree, filepath.ToSlash(worktreePath))
	}
	if output.TestCommand != "go test ./internal/foo" {
		t.Fatalf("output test command = %q, want failure report command", output.TestCommand)
	}
}

func TestExecuteDebugReturnsOriginalBranchWhenNoCodeChangesAndTestsPass(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	reportURI := "projects/run1/agents/tester_01/artifacts/test_reports/tester_01_test_report.md"
	worktreePath := t.TempDir()
	store := &debugMemoryArtifactStore{
		reads: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI: debugCoderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       "C:/repo/project",
				Branch:        "devflow/run1/coder_01/task/module",
				Commit:        "coder123",
				Worktree:      filepath.ToSlash(worktreePath),
				ModuleTaskURI: moduleTaskURI,
				TestCommand:   "go test ./...",
			}),
			reportURI: "# Test Failure Report\n\n## Test Command\n\ngo test ./...\n",
		},
	}
	runner := &debugFakeOpenCodeRunner{report: openCodeExecutionReport{
		Status:      "passed",
		Summary:     "failure no longer reproduces",
		TestCommand: "go test ./...",
		TestPassed:  true,
	}}
	git := &debugFakeGitManager{hasChanges: false}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("coder_01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_debug_01"),
		AgentID:      core.AgentID("coder_01"),
		Op:           "debug",
		ArtifactURIs: []string{moduleTaskURI, branchURI, reportURI},
	})
	if err != nil {
		t.Fatalf("execute debug: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := strings.Join(feedback.ArtifactURIs, "|"), branchURI; got != want {
		t.Fatalf("debug output artifacts = %v, want original branch artifact", feedback.ArtifactURIs)
	}
	if git.commitCalled {
		t.Fatal("debug should not commit when no code changes were produced")
	}
	for _, want := range []string{"# Debug Existing Coder Branch", moduleTaskURI, branchURI, reportURI, "Do not switch branches"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
}

func debugCoderBranchArtifactJSON(t *testing.T, artifact common.CoderBranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var _ common.OpenCodeRunner = (*debugFakeOpenCodeRunner)(nil)
var _ common.GitManager = (*debugFakeGitManager)(nil)
