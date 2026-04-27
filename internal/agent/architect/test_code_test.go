package architect

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

type architectTestFakeOpenCodeRunner struct {
	report      architectTestCodeExecutionReport
	workDir     string
	prompt      string
	err         error
	afterReport func(workDir string) error
}

func (r *architectTestFakeOpenCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
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
	if r.afterReport != nil {
		if err := r.afterReport(req.WorkDir); err != nil {
			return common.OpenCodeResult{}, err
		}
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

func TestExecuteArchitectTestCodeRunsOCInMainRepoAndReturnsOKWithoutArtifacts(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := initArchitectTestGitRepo(t)
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			designURI: "# 架构设计\n\n## 核心流程\n\n输入、调度、渲染。\n",
			mainURI:   mainBranchArtifactJSON(t, repoDir, "main", "base123"),
		},
	}
	runner := &architectTestFakeOpenCodeRunner{report: architectTestCodeExecutionReport{
		Status:       "passed",
		Summary:      "根据架构书自测通过",
		TestCommand:  "go test ./...",
		TestPassed:   true,
		Fixed:        true,
		ChangedFiles: []string{"engine.go", "engine_test.go"},
		TestsAdded:   []string{"engine_test.go"},
	}}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("architect01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_arch_test_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "test_code",
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("execute architect test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if len(feedback.ArtifactURIs) != 0 {
		t.Fatalf("success artifact uris = %v, want empty", feedback.ArtifactURIs)
	}
	if got, want := runner.workDir, filepath.FromSlash(repoDir); got != want {
		t.Fatalf("opencode workdir = %q, want main repo %q", got, want)
	}
	for _, want := range []string{"# 架构师级代码测试与修复", "自行设计测试", designURI, mainURI, "修复后必须重新运行测试"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
}

func TestExecuteArchitectTestCodeFailsWhenFixedChangesRemainUncommitted(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := initArchitectTestGitRepo(t)
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			designURI: "# 架构设计\n\n## 核心流程\n\n代码必须符合架构。\n",
			mainURI:   mainBranchArtifactJSON(t, repoDir, "main", "base123"),
		},
	}
	runner := &architectTestFakeOpenCodeRunner{report: architectTestCodeExecutionReport{
		Status:       "passed",
		Summary:      "测试已通过但忘记提交",
		TestCommand:  "go test ./...",
		TestPassed:   true,
		Fixed:        true,
		ChangedFiles: []string{"engine.go"},
	}}
	runner.afterReport = func(workDir string) error {
		return os.WriteFile(filepath.Join(workDir, "engine.go"), []byte("package main\n"), 0o644)
	}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("architect01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_arch_test_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "test_code",
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("execute architect test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	for _, want := range []string{"# 架构师测试失败报告", "OC 报告已修复并测试通过，但仓库仍存在未提交代码改动", "engine.go"} {
		if !strings.Contains(report, want) {
			t.Fatalf("problem document missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteArchitectTestCodeIgnoresPytestRuntimeCacheAfterCommittedFix(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := initArchitectTestGitRepo(t)
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			designURI: "# 架构设计\n\n## 核心流程\n\n提交代码后允许测试生成运行时缓存。\n",
			mainURI:   mainBranchArtifactJSON(t, repoDir, "main", "base123"),
		},
	}
	runner := &architectTestFakeOpenCodeRunner{report: architectTestCodeExecutionReport{
		Status:       "passed",
		Summary:      "测试通过并已提交修复",
		TestCommand:  "pytest -q",
		TestPassed:   true,
		Fixed:        true,
		ChangedFiles: []string{"todo_cli/repository.py", "tests/test_service.py"},
	}}
	runner.afterReport = func(workDir string) error {
		if err := os.MkdirAll(filepath.Join(workDir, "todo_cli"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(workDir, "todo_cli", "repository.py"), []byte("print('fixed')\n"), 0o644); err != nil {
			return err
		}
		runGitForTest(t, workDir, "add", "todo_cli/repository.py")
		runGitForTest(t, workDir, "commit", "-m", "fix: acceptance")
		cacheDir := filepath.Join(workDir, "tests", "__pycache__")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(cacheDir, "test_service.cpython-312.pyc"), []byte("pyc"), 0o644)
	}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("architect01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_arch_test_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "test_code",
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("execute architect test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if len(feedback.ArtifactURIs) != 0 {
		t.Fatalf("success artifact uris = %v, want empty", feedback.ArtifactURIs)
	}
}

func TestExecuteArchitectTestCodeReturnsKFailWithProblemDocumentWhenOCReportsFailure(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := filepath.ToSlash(t.TempDir())
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			designURI: "# 架构设计\n\n## 接口契约\n\nScoreService 必须更新分数。\n",
			mainURI:   mainBranchArtifactJSON(t, repoDir, "main", "base123"),
		},
	}
	runner := &architectTestFakeOpenCodeRunner{report: architectTestCodeExecutionReport{
		Status:            "failed",
		Summary:           "架构验收测试失败",
		TestCommand:       "go test ./...",
		TestPassed:        false,
		Fixed:             false,
		FailureSummary:    "ScoreService 没有按架构书更新分数。",
		TestsAdded:        []string{"score_service_test.go"},
		Evidence:          []string{"expected score 2 got 1"},
		SuspectedFiles:    []string{"score_service.go"},
		ReproductionSteps: []string{"运行 go test ./..."},
	}}
	agent := &Agent{
		runID:          core.RunID("run1"),
		agentID:        core.AgentID("architect01"),
		workspacePath:  t.TempDir(),
		artifactStore:  store,
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_arch_test_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "test_code",
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("execute architect test_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("failure artifact count = %d, want %d", got, want)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	for _, want := range []string{"# 架构师测试失败报告", "ScoreService 没有按架构书更新分数", designURI, repoDir, "expected score 2 got 1", "score_service.go"} {
		if !strings.Contains(report, want) {
			t.Fatalf("problem document missing %q:\n%s", want, report)
		}
	}
}

func initArchitectTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	runGitForTest(t, repoDir, "init")
	runGitForTest(t, repoDir, "config", "user.name", "Architect Test")
	runGitForTest(t, repoDir, "config", "user.email", "architect-test@example.local")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# test repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, repoDir, "add", ".")
	runGitForTest(t, repoDir, "commit", "-m", "initial")
	return filepath.ToSlash(repoDir)
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := runGit(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

var _ common.OpenCodeRunner = (*architectTestFakeOpenCodeRunner)(nil)
