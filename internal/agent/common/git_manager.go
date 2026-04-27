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
	worktreeDir := filepath.Join(req.WorkspacePath, "worktrees", taskSlug+"_"+moduleSlug)
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return Worktree{}, err
	}

	if _, err := runGit(ctx, req.RepoDir, "worktree", "add", "-b", branch, worktreeDir, baseRef); err != nil {
		return Worktree{}, err
	}
	return Worktree{RepoDir: req.RepoDir, Branch: branch, Path: worktreeDir}, nil
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
		command = "go test ./..."
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", command)
	cmd.Dir = worktree
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
		return result, fmt.Errorf("run test command %q: %w", command, err)
	}
	return result, nil
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
