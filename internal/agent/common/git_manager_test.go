package common

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"devflow/internal/core"
)

func TestShellCommandName(t *testing.T) {
	t.Run("plain command", func(t *testing.T) {
		if got, want := shellCommandName("node --test test/app.test.js"), "node"; got != want {
			t.Fatalf("shellCommandName() = %q, want %q", got, want)
		}
	})

	t.Run("quoted command", func(t *testing.T) {
		if got, want := shellCommandName(`"C:\Program Files\nodejs\node.exe" --test`), `C:\Program Files\nodejs\node.exe`; got != want {
			t.Fatalf("shellCommandName() = %q, want %q", got, want)
		}
	})
}

func TestParseBranchArtifactGlobalVerifyCommands(t *testing.T) {
	artifact, err := ParseBranchArtifact([]byte(`{
	  "schema_version": 1,
	  "kind": "main_branch",
	  "repo_dir": "D:/repo",
	  "branch": "main",
	  "commit": "abc123",
	  "delivery_profile": "frontend_web",
	  "required_files": ["index.html", "README.md", "src/"],
	  "test_command": "npm test",
	  "global_verify_commands": ["npm test", "npm run build"]
	}`))
	if err != nil {
		t.Fatalf("ParseBranchArtifact() error = %v", err)
	}
	if artifact.TestCommand != "npm test" {
		t.Fatalf("TestCommand = %q, want npm test", artifact.TestCommand)
	}
	if got, want := strings.Join(artifact.GlobalVerifyCommands, ","), "npm test,npm run build"; got != want {
		t.Fatalf("GlobalVerifyCommands = %v, want %s", artifact.GlobalVerifyCommands, want)
	}
	if artifact.DeliveryProfile != "frontend_web" {
		t.Fatalf("DeliveryProfile = %q, want frontend_web", artifact.DeliveryProfile)
	}
	if got, want := strings.Join(artifact.RequiredFiles, ","), "index.html,README.md,src/"; got != want {
		t.Fatalf("RequiredFiles = %v, want %s", artifact.RequiredFiles, want)
	}
}

func TestRunTestCommandRequiresCommand(t *testing.T) {
	result, err := NewLocalGitManager().RunTestCommand(context.Background(), t.TempDir(), " ")
	if err == nil {
		t.Fatal("RunTestCommand() error = nil, want error")
	}
	if result.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1", result.ExitCode)
	}
	if !strings.Contains(err.Error(), "test command is required") {
		t.Fatalf("error = %q, want required command hint", err.Error())
	}
}

func TestWindowsTestShellCommandUsesCmdForChainedCommands(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows shell selection only")
	}
	command := "node --test && node test/module01.seed.test.js"
	cmd := testShellCommand(context.Background(), command)
	if got := strings.ToLower(filepath.Base(cmd.Path)); got != "cmd.exe" {
		t.Fatalf("shell = %q, want cmd.exe for && command", cmd.Path)
	}
	if len(cmd.Args) < 3 || !strings.EqualFold(cmd.Args[1], "/C") || cmd.Args[2] != command {
		t.Fatalf("args = %#v, want cmd.exe /C %q", cmd.Args, command)
	}
}

func TestCommandRuntimeEnvPrependsConfiguredNodePaths(t *testing.T) {
	t.Setenv("PATH", strings.Join([]string{"C:\\Windows\\System32", "C:\\Tools"}, string(os.PathListSeparator)))
	t.Setenv("DEVFLOW_EXTRA_PATH", strings.Join([]string{"D:\\Extra\\Bin", "E:\\Shared\\Bin"}, string(os.PathListSeparator)))

	nodeDir := t.TempDir()
	nodePath := filepath.Join(nodeDir, "node.exe")
	if err := os.WriteFile(nodePath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(node.exe) error = %v", err)
	}
	t.Setenv("DEVFLOW_NODE_PATH", nodePath)

	env := commandRuntimeEnv("node --test test/engine-services.test.js")
	var pathValue string
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			pathValue = strings.TrimPrefix(item, "PATH=")
			break
		}
	}
	if pathValue == "" {
		t.Fatalf("PATH not found in env: %v", env)
	}
	parts := filepath.SplitList(pathValue)
	wantPrefix := []string{nodeDir, "D:\\Extra\\Bin", "E:\\Shared\\Bin"}
	for i, want := range wantPrefix {
		if i >= len(parts) || parts[i] != want {
			t.Fatalf("PATH prefix = %v, want %v", parts[:min(len(parts), len(wantPrefix))], wantPrefix)
		}
	}
}

func TestWrapMissingExecutableErrorAddsNodeHint(t *testing.T) {
	result := CommandResult{
		ExitCode: 1,
		Stderr: `node : The term 'node' is not recognized as the name of a cmdlet, function, script file, or operable program.
FullyQualifiedErrorId : CommandNotFoundException`,
	}
	err := wrapMissingExecutableError("node --test test/engine-services.test.js", result, fmt.Errorf("exit status 1"))
	if err == nil {
		t.Fatal("wrapMissingExecutableError() returned nil")
	}
	got := err.Error()
	for _, want := range []string{"required executable \"node\" was not found in PATH", "DEVFLOW_NODE_PATH", "DEVFLOW_EXTRA_PATH"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error = %q, want substring %q", got, want)
		}
	}
}

func TestWrapMissingExecutableErrorDetectsUnixNotFound(t *testing.T) {
	result := CommandResult{
		ExitCode: 127,
		Stderr:   "/bin/sh: 1: pytest: not found",
	}
	err := wrapMissingExecutableError("pytest", result, fmt.Errorf("exit status 127"))
	if err == nil {
		t.Fatal("wrapMissingExecutableError() returned nil")
	}
	if !strings.Contains(err.Error(), `required executable "pytest" was not found in PATH`) {
		t.Fatalf("error = %q, want missing executable hint", err.Error())
	}
}

func TestCreateCoderWorktreeReusesExistingBranchAndPath(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "app.txt", "base\n")
	baseCommit := commitAllForTest(t, repoDir, "base")

	workspacePath := t.TempDir()
	manager := NewLocalGitManager()
	req := CreateCoderWorktreeRequest{
		RepoDir:       repoDir,
		BaseBranch:    "main",
		BaseCommit:    baseCommit,
		RunID:         core.RunID("run1"),
		AgentID:       core.AgentID("tester01"),
		TaskID:        core.TaskID("task_06_tester01_test_code"),
		ModuleName:    "coder01",
		WorkspacePath: workspacePath,
	}

	worktree, err := manager.CreateCoderWorktree(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateCoderWorktree() first error = %v", err)
	}
	writeFileForTest(t, worktree.Path, "scratch.tmp", "temp\n")
	writeFileForTest(t, worktree.Path, "app.txt", "dirty\n")

	reused, err := manager.CreateCoderWorktree(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateCoderWorktree() reuse error = %v", err)
	}
	if reused.Path != worktree.Path {
		t.Fatalf("reused path = %q, want %q", reused.Path, worktree.Path)
	}
	content, err := os.ReadFile(filepath.Join(reused.Path, "app.txt"))
	if err != nil {
		t.Fatalf("ReadFile(app.txt) error = %v", err)
	}
	if got := strings.ReplaceAll(string(content), "\r\n", "\n"); got != "base\n" {
		t.Fatalf("app.txt after reuse = %q, want %q", got, "base\n")
	}
	if _, err := os.Stat(filepath.Join(reused.Path, "scratch.tmp")); !os.IsNotExist(err) {
		t.Fatalf("scratch.tmp still exists after reuse, stat err = %v", err)
	}
	head := runGitForTest(t, reused.Path, "rev-parse", "HEAD")
	if head != baseCommit {
		t.Fatalf("HEAD after reuse = %q, want %q", head, baseCommit)
	}
}

func TestCreateCoderWorktreeReattachesExistingBranchWhenPathWasRemoved(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "app.txt", "base\n")
	baseCommit := commitAllForTest(t, repoDir, "base")

	workspacePath := t.TempDir()
	manager := NewLocalGitManager()
	req := CreateCoderWorktreeRequest{
		RepoDir:       repoDir,
		BaseBranch:    "main",
		BaseCommit:    baseCommit,
		RunID:         core.RunID("run1"),
		AgentID:       core.AgentID("tester01"),
		TaskID:        core.TaskID("task_06_tester01_test_code"),
		ModuleName:    "coder01",
		WorkspacePath: workspacePath,
	}

	worktree, err := manager.CreateCoderWorktree(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateCoderWorktree() first error = %v", err)
	}
	if _, err := runGit(context.Background(), repoDir, "worktree", "remove", "--force", worktree.Path); err != nil {
		t.Fatalf("git worktree remove error = %v", err)
	}

	reattached, err := manager.CreateCoderWorktree(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateCoderWorktree() reattach error = %v", err)
	}
	if reattached.Path != worktree.Path {
		t.Fatalf("reattached path = %q, want %q", reattached.Path, worktree.Path)
	}
	head := runGitForTest(t, reattached.Path, "rev-parse", "HEAD")
	if head != baseCommit {
		t.Fatalf("HEAD after reattach = %q, want %q", head, baseCommit)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
