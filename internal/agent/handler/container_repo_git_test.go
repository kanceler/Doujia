package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
)

type scriptedContainerRunner struct {
	execFunc func(ctx context.Context, containerID string, args []string, stdin string) (createContainerCommandResult, error)
}

func (s scriptedContainerRunner) CheckDocker(context.Context) error { return nil }

func (s scriptedContainerRunner) Pull(context.Context, string) error { return nil }

func (s scriptedContainerRunner) Create(context.Context, string, string, string) (string, error) {
	return "", fmt.Errorf("unexpected create")
}

func (s scriptedContainerRunner) Exec(ctx context.Context, containerID string, args []string, stdin string) (createContainerCommandResult, error) {
	if s.execFunc == nil {
		return createContainerCommandResult{}, fmt.Errorf("unexpected exec")
	}
	return s.execFunc(ctx, containerID, args, stdin)
}

func TestContainerGitWorktreePrepareRejectsMissingBranchName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"pages/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	_, err := handleContainerGitWorktreePrepare(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
	}, scriptedContainerRunner{})
	if err == nil || !strings.Contains(err.Error(), "module_spec.branch_name is required") {
		t.Fatalf("handleContainerGitWorktreePrepare() error = %v, want missing branch_name", err)
	}
}

func TestContainerGitWorktreePrepareReturnsPreparedWhenWorktreeAlreadyMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"pages/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, containerID string, args []string, _ string) (createContainerCommandResult, error) {
			if containerID != "ctr-1" {
				t.Fatalf("Exec() containerID = %q, want ctr-1", containerID)
			}
			command := strings.Join(args, " ")
			for _, want := range []string{
				"rev-parse --git-dir",
				"rev-parse --verify \"$base_branch^{commit}\"",
				"show-ref --verify --quiet \"refs/heads/$branch_name\"",
				"git -C \"$worktree_dir\" rev-parse --is-inside-work-tree",
				"git -C \"$worktree_dir\" rev-parse --abbrev-ref HEAD",
			} {
				if !strings.Contains(command, want) {
					t.Fatalf("command = %q, want substring %q", command, want)
				}
			}
			return createContainerCommandResult{Stdout: "already_prepared\n"}, nil
		},
	}

	resp, err := handleContainerGitWorktreePrepare(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitWorktreePrepare() error = %v", err)
	}
	if got := resp.Data["status"]; got != "already_prepared" {
		t.Fatalf("handleContainerGitWorktreePrepare() status = %v, want already_prepared", got)
	}
}

func TestContainerGitWorktreePrepareCreatesBranchAndWorktree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"pages/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			for _, want := range []string{
				"mkdir \"$lock_dir\"",
				"git -C \"$repo_dir\" branch -- \"$branch_name\" \"$base_branch\"",
				"git -C \"$repo_dir\" worktree add -- \"$worktree_dir\" \"$branch_name\"",
			} {
				if !strings.Contains(command, want) {
					t.Fatalf("command = %q, want substring %q", command, want)
				}
			}
			return createContainerCommandResult{Stdout: "created\n"}, nil
		},
	}

	resp, err := handleContainerGitWorktreePrepare(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitWorktreePrepare() error = %v", err)
	}
	if got := resp.Data["status"]; got != "created" {
		t.Fatalf("handleContainerGitWorktreePrepare() status = %v, want created", got)
	}
}

func TestContainerGitCommitRejectsForbiddenChangedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"src/frontend/**", "src/pages/**", "src/components/**"},
		"forbidden_paths":     []string{"src/backend/**", "src/api/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			switch {
			case strings.Contains(command, "git status --porcelain"):
				return createContainerCommandResult{
					Stdout: "M src/backend/api.js\n",
				}, nil
			case strings.Contains(command, "git add -A ."):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git commit -m"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git rev-parse HEAD"):
				return createContainerCommandResult{Stdout: "abc123\n"}, nil
			default:
				return createContainerCommandResult{}, nil
			}
		},
	}

	_, err := handleContainerGitCommit(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{
			"message": "feat: update frontend",
		},
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "forbidden_paths") {
		t.Fatalf("handleContainerGitCommit() error = %v, want forbidden_paths rejection", err)
	}
}

func TestContainerGitCommitCommitsOwnedChangedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"src/frontend/**", "src/pages/**", "src/components/**"},
		"forbidden_paths":     []string{"src/backend/**", "src/api/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	statusSeen := false
	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			switch {
			case strings.Contains(command, "git status --porcelain"):
				statusSeen = true
				return createContainerCommandResult{
					Stdout: "M src/frontend/App.jsx\nA src/frontend/components/Header.jsx\n",
				}, nil
			case strings.Contains(command, "rev-parse") && strings.Contains(command, "main") && !strings.Contains(command, "HEAD"):
				return createContainerCommandResult{Stdout: "abc123\n"}, nil
			case strings.Contains(command, "git add -A ."):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git commit -m"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git rev-parse HEAD"):
				return createContainerCommandResult{Stdout: "abc123\n"}, nil
			case strings.Contains(command, "git diff-tree"):
				return createContainerCommandResult{Stdout: "src/frontend/App.jsx\nsrc/frontend/components/Header.jsx\n"}, nil
			default:
				return createContainerCommandResult{}, nil
			}
		},
	}

	resp, err := handleContainerGitCommit(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{
			"message": "feat: update frontend",
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitCommit() error = %v", err)
	}
	if !statusSeen {
		t.Fatal("handleContainerGitCommit() did not inspect changed files before commit")
	}
	if got := resp.Data["commit"]; got != "abc123" {
		t.Fatalf("handleContainerGitCommit() commit = %v, want abc123", got)
	}
	if got := resp.Data["base_commit"]; got != "abc123" {
		t.Fatalf("handleContainerGitCommit() base_commit = %v, want abc123", got)
	}
	if got := resp.Data["branch"]; got != "feature/module01" {
		t.Fatalf("handleContainerGitCommit() branch = %v, want feature/module01", got)
	}
	if got := resp.Data["test_command"]; got != "npm test" {
		t.Fatalf("handleContainerGitCommit() test_command = %v, want npm test", got)
	}
}

func TestContainerGitCommitRejectsWhenChangedFileMissingFromCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"src/frontend/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			switch {
			case strings.Contains(command, "git status --porcelain"):
				return createContainerCommandResult{Stdout: "M src/frontend/App.jsx\nA src/frontend/Header.jsx\n"}, nil
			case strings.Contains(command, "rev-parse") && strings.Contains(command, "main") && !strings.Contains(command, "HEAD"):
				return createContainerCommandResult{Stdout: "base123\n"}, nil
			case strings.Contains(command, "git add -A ."):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git commit -m"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git rev-parse HEAD"):
				return createContainerCommandResult{Stdout: "commit123\n"}, nil
			case strings.Contains(command, "git diff-tree"):
				return createContainerCommandResult{Stdout: "src/frontend/App.jsx\n"}, nil
			default:
				return createContainerCommandResult{}, nil
			}
		},
	}

	_, err := handleContainerGitCommit(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{"message": "feat: update frontend"},
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "not present in commit") {
		t.Fatalf("handleContainerGitCommit() error = %v, want missing committed file rejection", err)
	}
}

func TestContainerGitCommitMaterializesChangedDirectoryWithGitkeep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module02",
		"module_name":         "backend-core",
		"module_role":         "backend",
		"implementation_role": "coder",
		"branch_name":         "feature/module02",
		"worktree_dir":        "/workspace/worktrees/module02",
		"owned_paths":         []string{"data/**", "server/**"},
		"test_command":        "npm test",
		"complexity":          "high",
	})

	var commands []string
	statusCalls := 0
	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			commands = append(commands, command)
			switch {
			case strings.Contains(command, "git status --porcelain"):
				statusCalls++
				if statusCalls == 1 {
					return createContainerCommandResult{Stdout: "?? data/\n"}, nil
				}
				return createContainerCommandResult{Stdout: "?? data/.gitkeep\n"}, nil
			case strings.Contains(command, ".gitkeep"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "rev-parse") && strings.Contains(command, "main") && !strings.Contains(command, "HEAD"):
				return createContainerCommandResult{Stdout: "base123\n"}, nil
			case strings.Contains(command, "git add -A ."):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git commit -m"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git rev-parse HEAD"):
				return createContainerCommandResult{Stdout: "commit123\n"}, nil
			case strings.Contains(command, "git diff-tree"):
				return createContainerCommandResult{Stdout: "data/.gitkeep\n"}, nil
			default:
				return createContainerCommandResult{}, nil
			}
		},
	}

	resp, err := handleContainerGitCommit(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{"message": "feat: add data dir"},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitCommit() error = %v", err)
	}
	if statusCalls != 2 {
		t.Fatalf("git status calls = %d, want 2 after gitkeep materialization", statusCalls)
	}
	if !containsCommand(commands, "data/.gitkeep") {
		t.Fatalf("commands = %#v, want data/.gitkeep materialization", commands)
	}
	changedFiles, ok := resp.Data["changed_files"].([]string)
	if !ok {
		t.Fatalf("changed_files = %#v, want []string", resp.Data["changed_files"])
	}
	if len(changedFiles) != 1 || changedFiles[0] != "data/.gitkeep" {
		t.Fatalf("changed_files = %#v, want data/.gitkeep", changedFiles)
	}
}

func TestContainerWriteAllowsRuntimeWritePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module03",
		"module_name":         "backend-records",
		"module_role":         "backend",
		"implementation_role": "coder",
		"branch_name":         "feature/module03",
		"worktree_dir":        "/workspace/worktrees/module03",
		"owned_paths":         []string{"server/routes/feedings.js"},
		"runtime_write_paths": []string{"server/tests/module03/**"},
		"forbidden_paths":     []string{"miniprogram/**"},
		"test_command":        "cd server && npm test",
		"complexity":          "high",
	})

	wrote := false
	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, stdin string) (createContainerCommandResult, error) {
			wrote = true
			command := strings.Join(args, " ")
			if !strings.Contains(command, "server/tests/module03/smoke.test.js") {
				t.Fatalf("command = %q, want runtime_write_paths target", command)
			}
			if stdin != "test('smoke', () => {});\n" {
				t.Fatalf("stdin = %q, want test content", stdin)
			}
			return createContainerCommandResult{}, nil
		},
	}

	resp, err := handleContainerWrite(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{
			"relative_path": "server/tests/module03/smoke.test.js",
			"content":       "test('smoke', () => {});\n",
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerWrite() error = %v", err)
	}
	if !wrote {
		t.Fatal("handleContainerWrite() did not attempt to write runtime_write_paths file")
	}
	if got := resp.Data["status"]; got != "written" {
		t.Fatalf("handleContainerWrite() status = %v, want written", got)
	}
}

func TestContainerReadRootListingOnlyShowsWritableScope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module01",
		"module_name":         "frontend",
		"module_role":         "frontend",
		"implementation_role": "front",
		"branch_name":         "feature/module01",
		"worktree_dir":        "/workspace/worktrees/module01",
		"owned_paths":         []string{"miniprogram/**"},
		"runtime_write_paths": []string{"miniprogram/tests/**"},
		"forbidden_paths":     []string{"server/**", "README.md"},
		"test_command":        "echo frontend",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, _ []string, _ string) (createContainerCommandResult, error) {
			return createContainerCommandResult{
				Stdout: "./README.md\n./miniprogram/app.js\n./miniprogram/tests/frontend.smoke.md\n./server/package.json\n",
			}, nil
		},
	}

	resp, err := handleContainerRead(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{
			"relative_path": ".",
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerRead() error = %v", err)
	}
	content, _ := resp.Data["content"].(string)
	if strings.Contains(content, "README.md") || strings.Contains(content, "server/package.json") {
		t.Fatalf("handleContainerRead(.) content = %q, want non-writable files filtered out", content)
	}
	for _, want := range []string{"./miniprogram/app.js", "./miniprogram/tests/frontend.smoke.md"} {
		if !strings.Contains(content, want) {
			t.Fatalf("handleContainerRead(.) content = %q, want %q", content, want)
		}
	}
}

func TestContainerGitCommitAllowsRuntimeWritePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module02",
		"module_name":         "backend-core",
		"module_role":         "backend",
		"implementation_role": "coder",
		"branch_name":         "feature/module02",
		"worktree_dir":        "/workspace/worktrees/module02",
		"owned_paths":         []string{"server/app.js", "server/db/**", "server/utils/**", "server/routes/pets.js"},
		"runtime_write_paths": []string{"server/package.json", "server/package-lock.json", "server/tests/module02/**"},
		"forbidden_paths":     []string{"miniprogram/**"},
		"test_command":        "cd server && npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			switch {
			case strings.Contains(command, "git status --porcelain"):
				return createContainerCommandResult{
					Stdout: "M server/package.json\nA server/tests/module02/smoke.test.js\n",
				}, nil
			case strings.Contains(command, "git add -A ."):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git commit -m"):
				return createContainerCommandResult{}, nil
			case strings.Contains(command, "git rev-parse HEAD"):
				return createContainerCommandResult{Stdout: "abc123\n"}, nil
			case strings.Contains(command, "git diff-tree"):
				return createContainerCommandResult{Stdout: "server/package.json\nserver/tests/module02/smoke.test.js\n"}, nil
			default:
				return createContainerCommandResult{}, nil
			}
		},
	}

	resp, err := handleContainerGitCommit(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
		Args: map[string]any{
			"message": "feat: setup backend core",
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitCommit() error = %v", err)
	}
	if got := resp.Data["commit"]; got != "abc123" {
		t.Fatalf("handleContainerGitCommit() commit = %v, want abc123", got)
	}
}

func TestContainerGitWorktreePrepareScaffoldsSharedProjectFilesBeforeBranch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	containerPath := filepath.Join(dir, "container_context.json")
	modulePath := filepath.Join(dir, "module_spec.json")

	writeJSONFile(t, containerPath, map[string]any{
		"container_id": "ctr-1",
		"repo_dir":     "/workspace/repo",
		"base_branch":  "main",
	})
	writeJSONFile(t, modulePath, map[string]any{
		"module_id":           "module02",
		"module_name":         "backend-core",
		"module_role":         "backend",
		"implementation_role": "coder",
		"branch_name":         "feature/module02",
		"worktree_dir":        "/workspace/worktrees/module02",
		"owned_paths":         []string{"server/app.js", "server/db/**", "server/utils/**", "server/routes/pets.js"},
		"runtime_write_paths": []string{"server/package.json", "server/tests/module02/**"},
		"test_command":        "cd server && npm test",
		"complexity":          "high",
	})

	runner := scriptedContainerRunner{
		execFunc: func(_ context.Context, _ string, args []string, _ string) (createContainerCommandResult, error) {
			command := strings.Join(args, " ")
			for _, want := range []string{
				"server/package.json",
				"server/app.js",
				"server/db/database.js",
				"git -C \"$repo_dir\" commit -m \"chore: scaffold shared project skeleton\"",
				"git -C \"$repo_dir\" branch -- \"$branch_name\" \"$base_branch\"",
			} {
				if !strings.Contains(command, want) {
					t.Fatalf("command = %q, want substring %q", command, want)
				}
			}
			return createContainerCommandResult{Stdout: "created\n"}, nil
		},
	}

	resp, err := handleContainerGitWorktreePrepare(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: modulePath},
			},
		},
	}, runner)
	if err != nil {
		t.Fatalf("handleContainerGitWorktreePrepare() error = %v", err)
	}
	if got := resp.Data["status"]; got != "created" {
		t.Fatalf("handleContainerGitWorktreePrepare() status = %v, want created", got)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func containsCommand(commands []string, want string) bool {
	for _, command := range commands {
		if strings.Contains(command, want) {
			return true
		}
	}
	return false
}
