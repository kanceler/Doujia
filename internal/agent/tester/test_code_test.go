package tester

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

type fakeOpenCodeRunner struct {
	report testCodeExecutionReport
	prompt string
	err    error
}

func (r *fakeOpenCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
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

type fakeGitManager struct {
	worktreePath string
	request      common.CreateCoderWorktreeRequest
	err          error
}

func (m *fakeGitManager) CreateCoderWorktree(_ context.Context, req common.CreateCoderWorktreeRequest) (common.Worktree, error) {
	m.request = req
	if m.err != nil {
		return common.Worktree{}, m.err
	}
	return common.Worktree{RepoDir: req.RepoDir, Branch: "tester/worktree", Path: m.worktreePath}, nil
}

func (m *fakeGitManager) HasChanges(context.Context, string) (bool, error) {
	return false, nil
}

func (m *fakeGitManager) RunTestCommand(context.Context, string, string) (common.CommandResult, error) {
	return common.CommandResult{}, nil
}

func (m *fakeGitManager) CommitAll(context.Context, string, string) (common.CommitResult, error) {
	return common.CommitResult{}, nil
}

func TestExecuteTestCodeReturnsOKWhenOpenCodeReportsPassed(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	testDataURI := "projects/run1/agents/tester_01/artifacts/test_data/tester_01_test_data.md"
	worktreePath := t.TempDir()
	store := &memoryArtifactStore{
		reads: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", "go test ./..."),
			testDataURI:   "# Test Data\n\n- happy path\n",
		},
	}
	runner := &fakeOpenCodeRunner{report: testCodeExecutionReport{
		Status:      "passed",
		Summary:     "all test data passed",
		TestCommand: "go test ./...",
		TestPassed:  true,
	}}
	git := &fakeGitManager{worktreePath: worktreePath}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("tester_01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     git,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_test_code_01"),
		AgentID:      core.AgentID("tester_01"),
		Op:           "test_code",
		ArtifactURIs: []string{moduleTaskURI, branchURI, testDataURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if len(feedback.ArtifactURIs) != 0 {
		t.Fatalf("passed test_code artifact uris = %v, want empty", feedback.ArtifactURIs)
	}
	if git.request.RepoDir != "C:/repo/project" || git.request.BaseCommit != "abc123" {
		t.Fatalf("worktree request = %+v, want repo and coder commit", git.request)
	}
	for _, want := range []string{"# Test Code Verification", moduleTaskURI, branchURI, testDataURI, "Do not commit"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
}

func TestExecuteTestCodeReturnsKFailAndReportWhenOpenCodeReportsFailure(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	testDataURI := "projects/run1/agents/tester_01/artifacts/test_data/tester_01_test_data.md"
	store := &memoryArtifactStore{
		reads: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", ""),
			testDataURI:   "# Test Data\n\n- boundary case\n",
		},
	}
	runner := &fakeOpenCodeRunner{report: testCodeExecutionReport{
		Status:            "failed",
		Summary:           "boundary case failed",
		TestCommand:       "go test ./...",
		TestPassed:        false,
		FailureSummary:    "score was not updated",
		ReproductionSteps: []string{"run go test ./..."},
		Evidence:          []string{"expected 2 got 1"},
		SuspectedFiles:    []string{"score.go"},
	}}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("tester_01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
		gitManager:     &fakeGitManager{worktreePath: t.TempDir()},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_test_code_01"),
		AgentID:      core.AgentID("tester_01"),
		Op:           "test_code",
		ArtifactURIs: []string{moduleTaskURI, branchURI, testDataURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("failure artifact count = %d, want %d", got, want)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	for _, want := range []string{"# Test Failure Report", "boundary case failed", "score was not updated", "expected 2 got 1", moduleTaskURI, testDataURI} {
		if !strings.Contains(report, want) {
			t.Fatalf("failure report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteTestCodeFailsWithoutTestDataArtifact(t *testing.T) {
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	store := &memoryArtifactStore{
		reads: map[string]string{
			moduleTaskURI: "# Coder Task\n",
			branchURI:     coderBranchArtifactJSON(t, "C:/repo/project", "coder/01", "abc123", ""),
		},
	}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("tester_01"),
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_test_code_01"),
		AgentID:      core.AgentID("tester_01"),
		Op:           "test_code",
		ArtifactURIs: []string{moduleTaskURI, branchURI},
	})
	if err != nil {
		t.Fatalf("execute test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if !strings.Contains(store.writes[feedback.ArtifactURIs[0]], "requires an artifact URI containing /artifacts/test_data/") {
		t.Fatalf("failure output missing test_data requirement:\n%s", store.writes[feedback.ArtifactURIs[0]])
	}
}

func coderBranchArtifactJSON(t *testing.T, repoDir, branch, commit, testCommand string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(common.CoderBranchArtifact{
		SchemaVersion: 1,
		Kind:          "coder_branch",
		RepoDir:       repoDir,
		Branch:        branch,
		Commit:        commit,
		TestCommand:   testCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var _ common.OpenCodeRunner = (*fakeOpenCodeRunner)(nil)
var _ common.GitManager = (*fakeGitManager)(nil)
