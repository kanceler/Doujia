package architect

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

func TestExecuteMergeCodeFastPathMergesBranchesWithoutOpenCode(t *testing.T) {
	repoDir := initArchitectGitRepo(t)
	writeRepoFile(t, repoDir, "README.md", "base\n")
	base := commitRepoAll(t, repoDir, "base")

	branchA := createCoderBranchFromBase(t, repoDir, "feature-a", base, func() {
		writeRepoFile(t, repoDir, "module_a.txt", "A1\n")
		commitRepoAll(t, repoDir, "feature a")
	})
	branchB := createCoderBranchFromBase(t, repoDir, "feature-b", base, func() {
		writeRepoFile(t, repoDir, "module_b.txt", "B1\n")
		commitRepoAll(t, repoDir, "feature b")
	})
	mustCheckoutBranch(t, repoDir, "main")

	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	branchAURI := "projects/run1/agents/coder01/artifacts/branches/feature_a.md"
	branchBURI := "projects/run1/agents/coder02/artifacts/branches/feature_b.md"
	store := &testArtifactStore{
		files: map[string]string{
			mainURI: mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			branchAURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       filepath.ToSlash(repoDir),
				BaseBranch:    "main",
				BaseCommit:    base,
				Branch:        "feature-a",
				Commit:        branchA,
				ModuleTaskURI: "projects/run1/agents/coder01/artifacts/modules/module_a.md",
				TestCommand:   "Write-Output merge-a",
			}),
			branchBURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       filepath.ToSlash(repoDir),
				BaseBranch:    "main",
				BaseCommit:    base,
				Branch:        "feature-b",
				Commit:        branchB,
				ModuleTaskURI: "projects/run1/agents/coder02/artifacts/modules/module_b.md",
				TestCommand:   "Write-Output merge-b",
			}),
		},
	}
	runner := &mergeRunner{err: errors.New("OpenCode should not be called")}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, branchAURI, branchBURI},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if runner.workDir != "" {
		t.Fatalf("OpenCode runner was called with workdir %q", runner.workDir)
	}
	for _, name := range []string{"module_a.txt", "module_b.txt"} {
		if _, err := os.Stat(filepath.Join(repoDir, name)); err != nil {
			t.Fatalf("expected %s in merged main: %v", name, err)
		}
	}
	summaryURI := "projects/run1/agents/architect01/artifacts/code/merged_code_v1.md"
	branchURI := "projects/run1/agents/architect01/artifacts/branches/merged_main_branch.md"
	if _, ok := store.writes[summaryURI]; !ok {
		t.Fatalf("summary artifact %s not written", summaryURI)
	}
	branchArtifact := store.writes[branchURI]
	if !strings.Contains(branchArtifact, `"kind": "main_branch"`) {
		t.Fatalf("merged main branch artifact missing kind:\n%s", branchArtifact)
	}
	head := gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
	if !strings.Contains(branchArtifact, head) {
		t.Fatalf("merged main branch artifact missing head %s:\n%s", head, branchArtifact)
	}
}

func TestMergeCodeDetectsEntryOwnershipOverlapBeforeLLMFallback(t *testing.T) {
	repoDir := initArchitectGitRepo(t)
	writeRepoFile(t, repoDir, "README.md", "base\n")
	writeRepoFile(t, repoDir, "index.html", "<main>base</main>\n")
	writeRepoFile(t, repoDir, "package.json", "{}\n")
	base := commitRepoAll(t, repoDir, "base")

	leftHead := createCoderBranchFromBase(t, repoDir, "entry-left", base, func() {
		writeRepoFile(t, repoDir, "README.md", "left owns entry docs\n")
		commitRepoAll(t, repoDir, "left entry change")
	})
	rightHead := createCoderBranchFromBase(t, repoDir, "entry-right", base, func() {
		writeRepoFile(t, repoDir, "README.md", "right also owns entry docs\n")
		commitRepoAll(t, repoDir, "right entry change")
	})
	mustCheckoutBranch(t, repoDir, "main")

	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	leftURI := "projects/run1/agents/coder01/artifacts/branches/left.md"
	rightURI := "projects/run1/agents/coder02/artifacts/branches/right.md"
	store := &testArtifactStore{
		files: map[string]string{
			mainURI: mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			leftURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "entry-left", Commit: leftHead, ModuleTaskURI: "projects/run1/modules/left.md",
			}),
			rightURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "entry-right", Commit: rightHead, ModuleTaskURI: "projects/run1/modules/right.md",
			}),
		},
	}
	runner := &mergeRunner{err: errors.New("OpenCode should not be called")}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		llmClient:      &testLLM{},
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, leftURI, rightURI},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if runner.workDir != "" {
		t.Fatalf("OpenCode runner was called with workdir %q", runner.workDir)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	if !strings.Contains(report, "module ownership overlap: multiple coder branches modified entry-owned files") {
		t.Fatalf("failure report missing entry ownership overlap:\n%s", report)
	}
	head := gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
	if head != base {
		t.Fatalf("HEAD after overlap precheck = %s, want %s", head, base)
	}
	status := gitOutputForArchitectTest(t, repoDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("working tree not clean after overlap precheck:\n%s", status)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git", "CHERRY_PICK_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("CHERRY_PICK_HEAD present after overlap precheck, stat err = %v", err)
	}
}

func TestWriteDeterministicMergeSuccessPreservesFrontendWebDeliveryContract(t *testing.T) {
	store := &testArtifactStore{}
	agent := &Agent{runID: "run1", agentID: "architect01", artifactStore: store}
	_, err := agent.writeDeterministicMergeSuccess(context.Background(), core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     "run1",
		TaskID:    "task_merge",
		AgentID:   "architect01",
		Op:        core.TaskOpMergeCode,
	}, &mergeCodeInputs{
		main: common.BranchArtifact{
			Kind:                 "main_branch",
			RepoDir:              "D:/repo/project",
			Branch:               "main",
			Commit:               "base123",
			DeliveryProfile:      defaultDeliveryProfile,
			RequiredFiles:        []string{"index.html", "README.md", "src/"},
			GlobalVerifyCommands: []string{"node --test", "node test/module01.seed.test.js"},
		},
	}, deterministicMergeResult{
		MainBranch:       "main",
		MainCommitBefore: "base123",
		MainCommitAfter:  "merged456",
		TestCommands:     []string{"node --test", "node test/module01.seed.test.js"},
	})
	if err != nil {
		t.Fatalf("writeDeterministicMergeSuccess() error = %v", err)
	}
	branchArtifact := store.writes["projects/run1/agents/architect01/artifacts/branches/merged_main_branch.md"]
	for _, want := range []string{`"delivery_profile": "frontend_web"`, `"required_files": [`, `"src/"`, `"global_verify_commands": [`, `"test_command": "node --test"`} {
		if !strings.Contains(branchArtifact, want) {
			t.Fatalf("merged main branch artifact missing %q:\n%s", want, branchArtifact)
		}
	}
}

func TestExecuteMergeCodeFastPathConflictRollsBackAndDoesNotLeaveCherryPickState(t *testing.T) {
	t.Setenv("DEVFLOW_DISABLE_ARCHITECT_FALLBACK", "1")

	repoDir := initArchitectGitRepo(t)
	writeRepoFile(t, repoDir, "shared.txt", "line\n")
	base := commitRepoAll(t, repoDir, "base")

	leftHead := createCoderBranchFromBase(t, repoDir, "left", base, func() {
		writeRepoFile(t, repoDir, "shared.txt", "left\n")
		commitRepoAll(t, repoDir, "left change")
	})
	rightHead := createCoderBranchFromBase(t, repoDir, "right", base, func() {
		writeRepoFile(t, repoDir, "shared.txt", "right\n")
		commitRepoAll(t, repoDir, "right change")
	})
	mustCheckoutBranch(t, repoDir, "main")

	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	leftURI := "projects/run1/agents/coder01/artifacts/branches/left.md"
	rightURI := "projects/run1/agents/coder02/artifacts/branches/right.md"
	store := &testArtifactStore{
		files: map[string]string{
			mainURI: mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			leftURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "left", Commit: leftHead, ModuleTaskURI: "projects/run1/modules/left.md",
			}),
			rightURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "right", Commit: rightHead, ModuleTaskURI: "projects/run1/modules/right.md",
			}),
		},
	}
	agent := &Agent{runID: "run1", agentID: "architect01", artifactStore: store}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, leftURI, rightURI},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if feedback.Result == core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want non-ok", feedback.Result)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git", "CHERRY_PICK_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("CHERRY_PICK_HEAD still present, stat err = %v", err)
	}
	head := gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
	if head != base {
		t.Fatalf("HEAD after rollback = %s, want %s", head, base)
	}
	status := gitOutputForArchitectTest(t, repoDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("working tree not clean after rollback:\n%s", status)
	}
}

func TestExecuteMergeCodeFastPathCherryPicksAllCommitsAfterBase(t *testing.T) {
	repoDir := initArchitectGitRepo(t)
	writeRepoFile(t, repoDir, "README.md", "base\n")
	base := commitRepoAll(t, repoDir, "base")

	head := createCoderBranchFromBase(t, repoDir, "multi", base, func() {
		writeRepoFile(t, repoDir, "commit_a.txt", "A\n")
		commitRepoAll(t, repoDir, "commit a")
		writeRepoFile(t, repoDir, "commit_b.txt", "B\n")
		commitRepoAll(t, repoDir, "commit b")
	})
	mustCheckoutBranch(t, repoDir, "main")

	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	branchURI := "projects/run1/agents/coder01/artifacts/branches/multi.md"
	store := &testArtifactStore{
		files: map[string]string{
			mainURI: mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			branchURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "multi", Commit: head, ModuleTaskURI: "projects/run1/modules/multi.md",
			}),
		},
	}
	agent := &Agent{runID: "run1", agentID: "architect01", artifactStore: store}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, branchURI},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	for _, name := range []string{"commit_a.txt", "commit_b.txt"} {
		if _, err := os.Stat(filepath.Join(repoDir, name)); err != nil {
			t.Fatalf("expected %s after merge: %v", name, err)
		}
	}
}

func TestExecuteMergeCodeFastPathTestFailureRollsBackMain(t *testing.T) {
	repoDir := initArchitectGitRepo(t)
	writeRepoFile(t, repoDir, "README.md", "base\n")
	base := commitRepoAll(t, repoDir, "base")

	head := createCoderBranchFromBase(t, repoDir, "failing", base, func() {
		writeRepoFile(t, repoDir, "module.txt", "change\n")
		commitRepoAll(t, repoDir, "module change")
	})
	mustCheckoutBranch(t, repoDir, "main")

	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	branchURI := "projects/run1/agents/coder01/artifacts/branches/failing.md"
	store := &testArtifactStore{
		files: map[string]string{
			mainURI: mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			branchURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1, Kind: "coder_branch", RepoDir: filepath.ToSlash(repoDir), BaseBranch: "main", BaseCommit: base, Branch: "failing", Commit: head, ModuleTaskURI: "projects/run1/modules/failing.md", TestCommand: "exit 1",
			}),
		},
	}
	agent := &Agent{runID: "run1", agentID: "architect01", artifactStore: store}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, branchURI},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if feedback.Result != core.TaskResultCodeBug {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeBug)
	}
	headAfter := gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
	if headAfter != base {
		t.Fatalf("HEAD after test failure rollback = %s, want %s", headAfter, base)
	}
	status := gitOutputForArchitectTest(t, repoDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("working tree not clean after test failure rollback:\n%s", status)
	}
	reportURI := "projects/run1/agents/architect01/artifacts/merge_reports/merge_code_report.md"
	if report := store.writes[reportURI]; !strings.Contains(report, "exit 1") {
		t.Fatalf("failure report missing failing command:\n%s", report)
	}
}

func TestFindArchitectTestCodeInputsPrefersMergedMainBranchArtifact(t *testing.T) {
	baseDoc := agentengine.ArtifactDocument{
		URI:     "projects/run1/agents/architect01/artifacts/branches/main_branch.md",
		Content: mainBranchArtifactJSONForTest(t, "D:/repo", "main", "base123"),
	}
	mergedDoc := agentengine.ArtifactDocument{
		URI:     "projects/run1/agents/architect01/artifacts/branches/merged_main_branch.md",
		Content: mainBranchArtifactJSONForTest(t, "D:/repo", "main", "merged456"),
	}

	inputs := findArchitectTestCodeInputs([]agentengine.ArtifactDocument{baseDoc, mergedDoc})
	if got, want := inputs.main.Commit, "merged456"; got != want {
		t.Fatalf("selected main commit = %q, want %q", got, want)
	}
	if got, want := inputs.mainDoc.URI, mergedDoc.URI; got != want {
		t.Fatalf("selected main URI = %q, want %q", got, want)
	}
}

func initArchitectGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	gitOutputForArchitectTest(t, repoDir, "init", "-b", "main")
	gitOutputForArchitectTest(t, repoDir, "config", "user.name", "DevFlow Test")
	gitOutputForArchitectTest(t, repoDir, "config", "user.email", "devflow@example.local")
	return repoDir
}

func gitOutputForArchitectTest(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	cmdResult, err := runArchitectGit(repoDir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(cmdResult)
}

func runArchitectGit(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func writeRepoFile(t *testing.T, repoDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
}

func commitRepoAll(t *testing.T, repoDir, message string) string {
	t.Helper()
	gitOutputForArchitectTest(t, repoDir, "add", "-A")
	gitOutputForArchitectTest(t, repoDir, "commit", "-m", message)
	return gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
}

func createCoderBranchFromBase(t *testing.T, repoDir, branch, base string, fn func()) string {
	t.Helper()
	gitOutputForArchitectTest(t, repoDir, "checkout", "-b", branch, base)
	fn()
	return gitOutputForArchitectTest(t, repoDir, "rev-parse", "HEAD")
}

func mustCheckoutBranch(t *testing.T, repoDir, branch string) {
	t.Helper()
	gitOutputForArchitectTest(t, repoDir, "checkout", branch)
}
