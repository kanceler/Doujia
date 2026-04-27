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

type mergeMemoryArtifactStore struct {
	reads    map[string]string
	writes   map[string]string
	writeErr error
}

func (s *mergeMemoryArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.reads[uri]), nil
}

func (s *mergeMemoryArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[uri] = string(content)
	return nil
}

type mergeFakeOpenCodeRunner struct {
	report  mergeCodeExecutionReport
	workDir string
	prompt  string
	err     error
}

func (r *mergeFakeOpenCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
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

func TestExecuteMergeCodeUsesSplitModuleMainRepoAndReturnsOKWithNoArtifacts(t *testing.T) {
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	repoDir := filepath.ToSlash(t.TempDir())
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			mainURI:   mainBranchArtifactJSON(t, repoDir, "main", "base123"),
			moduleURI: "# 程序员任务书：Engine\n\n## 模块目标\n\n实现引擎。\n",
			branchURI: coderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       repoDir,
				BaseBranch:    "main",
				BaseCommit:    "base123",
				Branch:        "devflow/run1/coder_01/task/engine",
				Commit:        "coder123",
				ModuleTaskURI: moduleURI,
				TestCommand:   "go test ./...",
			}),
		},
	}
	runner := &mergeFakeOpenCodeRunner{report: mergeCodeExecutionReport{
		Status:         "passed",
		Summary:        "合并成功",
		MergedBranches: []string{"devflow/run1/coder_01/task/engine"},
		TestCommand:    "go test ./...",
		TestPassed:     true,
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
		TaskID:       core.TaskID("task_merge_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "merge_code",
		ArtifactURIs: []string{mainURI, moduleURI, branchURI},
	})
	if err != nil {
		t.Fatalf("execute merge_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if len(feedback.ArtifactURIs) != 0 {
		t.Fatalf("merge_code success artifact uris = %v, want empty", feedback.ArtifactURIs)
	}
	if got, want := runner.workDir, filepath.FromSlash(repoDir); got != want {
		t.Fatalf("opencode workdir = %q, want main repo %q", got, want)
	}
	for _, want := range []string{"# 合并程序员分支", "你是 DevFlow 架构师 Agent", moduleURI, branchURI, "逐个合并"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
}

func TestExecuteMergeCodeRejectsCoderBranchFromDifferentRepo(t *testing.T) {
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	mainRepo := filepath.ToSlash(t.TempDir())
	otherRepo := filepath.ToSlash(t.TempDir())
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			mainURI:   mainBranchArtifactJSON(t, mainRepo, "main", "base123"),
			moduleURI: "# 程序员任务书：Engine\n",
			branchURI: coderBranchArtifactJSON(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       otherRepo,
				BaseBranch:    "main",
				BaseCommit:    "base123",
				Branch:        "devflow/run1/coder_01/task/engine",
				Commit:        "coder123",
				ModuleTaskURI: moduleURI,
			}),
		},
	}
	runner := &mergeFakeOpenCodeRunner{}
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
		TaskID:       core.TaskID("task_merge_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "merge_code",
		ArtifactURIs: []string{mainURI, moduleURI, branchURI},
	})
	if err != nil {
		t.Fatalf("execute merge_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if runner.workDir != "" {
		t.Fatalf("opencode should not run for mismatched repo, got workdir %q", runner.workDir)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("failure artifact count = %d, want %d", got, want)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	for _, want := range []string{"# 合并失败报告", "程序员分支仓库地址必须和主分支仓库地址一致", mainRepo, otherRepo} {
		if !strings.Contains(report, want) {
			t.Fatalf("failure report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteMergeCodeSurfacesFailureReportWriteError(t *testing.T) {
	store := &mergeMemoryArtifactStore{writeErr: os.ErrPermission}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("architect01"),
		workspacePath: t.TempDir(),
		artifactStore: store,
	}

	_, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     core.RunID("run1"),
		TaskID:    core.TaskID("task_merge_01"),
		AgentID:   core.AgentID("architect01"),
		Op:        "merge_code",
	})
	if err == nil {
		t.Fatal("expected merge_code to surface failure report write error")
	}
	if !strings.Contains(err.Error(), "写入合并失败报告失败") {
		t.Fatalf("error = %v, want Chinese failure report write context", err)
	}
}

func TestExecuteMergeCodeReportsBranchParseErrorInChinese(t *testing.T) {
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	branchURI := "projects/run1/agents/coder_01/artifacts/branches/coder_01_branch.md"
	store := &mergeMemoryArtifactStore{
		reads: map[string]string{
			mainURI:   mainBranchArtifactJSON(t, filepath.ToSlash(t.TempDir()), "main", "base123"),
			moduleURI: "# 程序员任务书：Engine\n",
			branchURI: "{invalid json",
		},
	}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("architect01"),
		workspacePath: t.TempDir(),
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_merge_01"),
		AgentID:      core.AgentID("architect01"),
		Op:           "merge_code",
		ArtifactURIs: []string{mainURI, moduleURI, branchURI},
	})
	if err != nil {
		t.Fatalf("execute merge_code: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	if !strings.Contains(report, "解析分支产物失败") {
		t.Fatalf("failure report missing Chinese parse context:\n%s", report)
	}
	if strings.Contains(report, "invalid character") {
		t.Fatalf("failure report leaked English JSON parser error:\n%s", report)
	}
}

func mainBranchArtifactJSON(t *testing.T, repoDir, branch, commit string) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(common.BranchArtifact{
		SchemaVersion: 1,
		Kind:          "main_branch",
		RepoDir:       repoDir,
		Branch:        branch,
		Commit:        commit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func coderBranchArtifactJSON(t *testing.T, artifact common.CoderBranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var _ common.OpenCodeRunner = (*mergeFakeOpenCodeRunner)(nil)
