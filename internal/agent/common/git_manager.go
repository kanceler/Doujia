package common

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devflow/internal/core"
)

type GitManager interface {
	CreateCoderWorktree(ctx context.Context, req CreateCoderWorktreeRequest) (Worktree, error)
	HasChanges(ctx context.Context, worktree string) (bool, error)
	RunTestCommand(ctx context.Context, worktree string, command string) (CommandResult, error)
	CommitAll(ctx context.Context, worktree string, message string) (CommitResult, error)
}

type CreateCoderWorktreeRequest struct {
	RepoDir       string
	BaseBranch    string
	BaseCommit    string
	RunID         core.RunID
	AgentID       core.AgentID
	TaskID        core.TaskID
	ModuleName    string
	WorkspacePath string
}

type Worktree struct {
	RepoDir string
	Branch  string
	Path    string
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type CommitResult struct {
	Commit   string
	Duration time.Duration
}

type LocalGitManager struct{}

func NewLocalGitManager() *LocalGitManager {
	return &LocalGitManager{}
}

func (m *LocalGitManager) CreateCoderWorktree(ctx context.Context, req CreateCoderWorktreeRequest) (Worktree, error) {
	if strings.TrimSpace(req.RepoDir) == "" {
		return Worktree{}, fmt.Errorf("repo dir is required")
	}
	if strings.TrimSpace(req.WorkspacePath) == "" {
		return Worktree{}, fmt.Errorf("workspace path is required")
	}
	repoDir, err := filepath.Abs(filepath.Clean(req.RepoDir))
	if err != nil {
		return Worktree{}, fmt.Errorf("resolve repo dir: %w", err)
	}
	workspacePath, err := filepath.Abs(filepath.Clean(req.WorkspacePath))
	if err != nil {
		return Worktree{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	baseRef := strings.TrimSpace(req.BaseCommit)
	if baseRef == "" {
		baseRef = strings.TrimSpace(req.BaseBranch)
	}
	if baseRef == "" {
		return Worktree{}, fmt.Errorf("base commit or branch is required")
	}

	moduleSlug := safeSegment(req.ModuleName)
	if moduleSlug == "" {
		moduleSlug = "module"
	}
	taskSlug := safeSegment(string(req.TaskID))
	if taskSlug == "" {
		taskSlug = "task"
	}
	branch := coderBranchName(req)
	worktreeDir := filepath.Join(workspacePath, "worktrees", taskSlug+"_"+moduleSlug)
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return Worktree{}, err
	}

	if _, err := runGit(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir, baseRef); err != nil {
		reused, reuseErr := reuseExistingWorktree(ctx, repoDir, worktreeDir, branch, baseRef, err)
		if reuseErr != nil {
			return Worktree{}, reuseErr
		}
		if !reused {
			return Worktree{}, err
		}
	}
	return Worktree{RepoDir: repoDir, Branch: branch, Path: worktreeDir}, nil
}

func reuseExistingWorktree(ctx context.Context, repoDir string, worktreeDir string, branch string, baseRef string, createErr error) (bool, error) {
	if !isAlreadyExistsWorktreeError(createErr) {
		return false, nil
	}
	if _, err := os.Stat(worktreeDir); err == nil {
		if err := resetReusableWorktree(ctx, worktreeDir, baseRef); err != nil {
			return false, err
		}
		return true, nil
	}
	if _, err := runGit(ctx, repoDir, "worktree", "add", worktreeDir, branch); err != nil {
		return false, err
	}
	if err := resetReusableWorktree(ctx, worktreeDir, baseRef); err != nil {
		return false, err
	}
	return true, nil
}

func resetReusableWorktree(ctx context.Context, worktreeDir string, baseRef string) error {
	if _, err := runGit(ctx, worktreeDir, "reset", "--hard", baseRef); err != nil {
		return err
	}
	if _, err := runGit(ctx, worktreeDir, "clean", "-fd"); err != nil {
		return err
	}
	return nil
}

func isAlreadyExistsWorktreeError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "already exists")
}

func coderBranchName(req CreateCoderWorktreeRequest) string {
	runSlug := safeSegment(string(req.RunID))
	agentSlug := safeSegment(string(req.AgentID))
	taskSlug := safeSegment(string(req.TaskID))
	moduleSlug := safeSegment(req.ModuleName)
	if taskSlug == "" {
		taskSlug = "task"
	}
	if moduleSlug == "" {
		moduleSlug = "module"
	}
	branch := strings.Join([]string{
		"devflow",
		runSlug,
		agentSlug,
		taskSlug,
		moduleSlug,
	}, "/")
	if len(branch) <= 100 {
		return branch
	}
	return strings.Join([]string{
		"devflow",
		shortBranchSegment(runSlug, 24),
		shortBranchSegment(agentSlug, 18),
		shortBranchSegment(taskSlug+"-"+moduleSlug, 36),
	}, "/")
}

func (m *LocalGitManager) HasChanges(ctx context.Context, worktree string) (bool, error) {
	result, err := runGit(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		pathPart := line
		if len(line) > 3 {
			pathPart = line[3:]
		}
		pathPart = filepath.ToSlash(strings.TrimSpace(pathPart))
		if strings.HasPrefix(pathPart, ".devflow/") || pathPart == ".devflow" {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (m *LocalGitManager) RunTestCommand(ctx context.Context, worktree string, command string) (CommandResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandResult{ExitCode: -1}, fmt.Errorf("test command is required")
	}
	start := time.Now()
	cmd := testShellCommand(ctx, command)
	cmd.Dir = worktree
	cmd.Env = commandRuntimeEnv(command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
		Duration: time.Since(start),
	}
	if err != nil {
		return result, wrapMissingExecutableError(command, result, err)
	}
	return result, nil
}

func testShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if strings.Contains(command, "&&") || strings.Contains(command, "||") {
			return exec.CommandContext(ctx, "cmd.exe", "/C", command)
		}
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", command)
		}
		return exec.CommandContext(ctx, "cmd.exe", "/C", command)
	}

	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func commandRuntimeEnv(command string) []string {
	env := os.Environ()
	extraPaths := make([]string, 0, 2)
	if nodePath := strings.TrimSpace(os.Getenv("DEVFLOW_NODE_PATH")); strings.EqualFold(shellCommandName(command), "node") && nodePath != "" {
		if info, err := os.Stat(nodePath); err == nil {
			if info.IsDir() {
				extraPaths = append(extraPaths, nodePath)
			} else {
				extraPaths = append(extraPaths, filepath.Dir(nodePath))
			}
		} else {
			extraPaths = append(extraPaths, filepath.Dir(nodePath))
		}
	}
	if extraPath := strings.TrimSpace(os.Getenv("DEVFLOW_EXTRA_PATH")); extraPath != "" {
		for _, item := range filepath.SplitList(extraPath) {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				extraPaths = append(extraPaths, trimmed)
			}
		}
	}
	if len(extraPaths) == 0 {
		return env
	}
	return prependPathEnv(env, extraPaths)
}

func prependPathEnv(env []string, extraPaths []string) []string {
	if len(extraPaths) == 0 {
		return env
	}
	pathKey := "PATH"
	currentPath := os.Getenv(pathKey)
	out := make([]string, 0, len(env)+1)
	found := false
	prefix := strings.Join(extraPaths, string(os.PathListSeparator))
	for _, item := range env {
		if len(item) >= len(pathKey)+1 && strings.EqualFold(item[:len(pathKey)], pathKey) && item[len(pathKey)] == '=' {
			found = true
			currentPath = item[len(pathKey)+1:]
			out = append(out, pathKey+"="+prefix+string(os.PathListSeparator)+currentPath)
			continue
		}
		out = append(out, item)
	}
	if !found {
		out = append(out, pathKey+"="+prefix+string(os.PathListSeparator)+currentPath)
	}
	return out
}

func shellCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '"' || command[0] == '\'' {
		quote := command[0]
		command = command[1:]
		if idx := strings.IndexByte(command, quote); idx >= 0 {
			return strings.TrimSpace(command[:idx])
		}
		return strings.TrimSpace(command)
	}
	for i, r := range command {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return strings.TrimSpace(command[:i])
		}
	}
	return command
}

func wrapMissingExecutableError(command string, result CommandResult, runErr error) error {
	stderr := strings.ToLower(result.Stderr)
	name := shellCommandName(command)
	if name != "" && (strings.Contains(stderr, "commandnotfoundexception") ||
		strings.Contains(stderr, "is not recognized as the name of a cmdlet") ||
		strings.Contains(stderr, "command not found") ||
		strings.Contains(stderr, "not found") ||
		strings.Contains(stderr, "executable file not found")) {
		hint := "install the required runtime or expose it in PATH"
		if strings.EqualFold(name, "node") {
			hint = "install Node.js or set DEVFLOW_NODE_PATH / DEVFLOW_EXTRA_PATH so the DevFlow process can find node.exe"
		}
		return fmt.Errorf("run test command %q: required executable %q was not found in PATH; %s: %w", command, name, hint, runErr)
	}
	return fmt.Errorf("run test command %q: %w", command, runErr)
}

func (m *LocalGitManager) CommitAll(ctx context.Context, worktree string, message string) (CommitResult, error) {
	start := time.Now()
	if _, err := runGit(ctx, worktree, "add", "-A", "."); err != nil {
		return CommitResult{}, err
	}
	if _, err := os.Stat(filepath.Join(worktree, ".devflow")); err == nil {
		_, _ = runGit(ctx, worktree, "reset", "-q", "--", ".devflow")
	}
	if _, err := runGit(ctx, worktree, "diff", "--cached", "--quiet"); err == nil {
		return CommitResult{}, fmt.Errorf("no staged code changes to commit")
	}
	if strings.TrimSpace(message) == "" {
		message = "devflow coder changes"
	}
	if _, err := runGit(ctx, worktree, "-c", "user.name=DevFlow Coder", "-c", "user.email=devflow@example.local", "commit", "-m", message); err != nil {
		return CommitResult{}, err
	}
	head, err := runGit(ctx, worktree, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Commit: strings.TrimSpace(head.Stdout), Duration: time.Since(start)}, nil
}

func runGit(ctx context.Context, dir string, args ...string) (CommandResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
		Duration: time.Since(start),
	}
	if err != nil {
		return result, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func safeSegment(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.'
		if ok {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-.")
}

func shortBranchSegment(value string, max int) string {
	value = safeSegment(value)
	if value == "" || len(value) <= max {
		return value
	}
	sum := sha1.Sum([]byte(value))
	suffix := hex.EncodeToString(sum[:])[:8]
	if max <= len(suffix)+1 {
		return suffix[:max]
	}
	headLen := max - len(suffix) - 1
	return strings.Trim(value[:headLen], "-.") + "-" + suffix
}

func DurationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	millis := duration.Milliseconds()
	if millis == 0 {
		return 1
	}
	return millis
}
