package handler

import (
	"context"
	"fmt"
	"strings"

	"devflow/internal/agent/core"
)

type ContainerGitCherryPickHandler struct {
	runner containerRunner
}

type ContainerGitWorktreePrepareHandler struct {
	runner containerRunner
}

type scopedContainerGitCherryPickHandler struct {
	bundle core.AgentInputBundle
	runner containerRunner
}

type scopedContainerGitWorktreePrepareHandler struct {
	bundle core.AgentInputBundle
	runner containerRunner
}

func NewContainerGitCherryPickHandler() *ContainerGitCherryPickHandler {
	return &ContainerGitCherryPickHandler{runner: dockerContainerRunner{}}
}

func NewContainerGitWorktreePrepareHandler() *ContainerGitWorktreePrepareHandler {
	return &ContainerGitWorktreePrepareHandler{runner: dockerContainerRunner{}}
}

func NewContainerGitCherryPickHandlerWithRunner(runner containerRunner) *ContainerGitCherryPickHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerGitCherryPickHandler{runner: runner}
}

func NewContainerGitWorktreePrepareHandlerWithRunner(runner containerRunner) *ContainerGitWorktreePrepareHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerGitWorktreePrepareHandler{runner: runner}
}

func (h *ContainerGitCherryPickHandler) Name() string {
	return "container_git_cherry_pick"
}

func (h *ContainerGitWorktreePrepareHandler) Name() string {
	return "container_git_worktree_prepare"
}

func (h *ContainerGitCherryPickHandler) Description() string {
	return "Cherry-pick tested module commits onto container_context.repo_dir and return structured merge data."
}

func (h *ContainerGitWorktreePrepareHandler) Description() string {
	return "Prepare the module branch and worktree inside container_context.repo_dir before coder.write_code enters the tool loop."
}

func (h *ContainerGitCherryPickHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"commits"},
			"properties": map[string]any{
				"commits": map[string]any{
					"type":        "array",
					"description": "Git commit SHAs to cherry-pick onto container_context.repo_dir in order.",
					"items":       map[string]any{"type": "string"},
				},
				"base_branch": map[string]any{
					"type":        "string",
					"description": "Optional branch to checkout before cherry-picking. Defaults to container_context.base_branch.",
				},
			},
		},
	}
}

func (h *ContainerGitWorktreePrepareHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		},
	}
}

func (h *ContainerGitCherryPickHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerGitCherryPick(ctx, req, h.runner)
}

func (h *ContainerGitWorktreePrepareHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerGitWorktreePrepare(ctx, req, h.runner)
}

func (h *ContainerGitCherryPickHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerGitCherryPickHandler{bundle: bundle, runner: h.runner}
}

func (h *ContainerGitWorktreePrepareHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerGitWorktreePrepareHandler{bundle: bundle, runner: h.runner}
}

func (h *scopedContainerGitCherryPickHandler) Name() string {
	return "container_git_cherry_pick"
}

func (h *scopedContainerGitWorktreePrepareHandler) Name() string {
	return "container_git_worktree_prepare"
}

func (h *scopedContainerGitCherryPickHandler) Description() string {
	return "Cherry-pick tested module commits onto container_context.repo_dir and return structured merge data."
}

func (h *scopedContainerGitWorktreePrepareHandler) Description() string {
	return "Prepare the module branch and worktree inside container_context.repo_dir before coder.write_code enters the tool loop."
}

func (h *scopedContainerGitCherryPickHandler) ToolSpec() core.ToolSpec {
	return NewContainerGitCherryPickHandlerWithRunner(h.runner).ToolSpec()
}

func (h *scopedContainerGitWorktreePrepareHandler) ToolSpec() core.ToolSpec {
	return NewContainerGitWorktreePrepareHandlerWithRunner(h.runner).ToolSpec()
}

func (h *scopedContainerGitCherryPickHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleContainerGitCherryPick(ctx, req, h.runner)
}

func (h *scopedContainerGitWorktreePrepareHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleContainerGitWorktreePrepare(ctx, req, h.runner)
}

func handleContainerGitCherryPick(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	container, err := resolveContainerContext(req.Bundle)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	commits, err := stringSliceParam(req.Args, "commits")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if len(commits) == 0 {
		return core.HandlerResponse{}, fmt.Errorf("param %q must not be empty", "commits")
	}
	baseBranch := strings.TrimSpace(container.BaseBranch)
	if arg, ok := req.Args["base_branch"]; ok {
		baseBranch, err = requiredStringArg(map[string]any{"base_branch": arg}, "base_branch")
		if err != nil {
			return core.HandlerResponse{}, err
		}
	}
	if strings.TrimSpace(baseBranch) == "" {
		return core.HandlerResponse{}, fmt.Errorf("container_context.base_branch is required")
	}
	if strings.TrimSpace(container.ContainerID) == "" {
		return core.HandlerResponse{}, fmt.Errorf("container_context.container_id is required")
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		return core.HandlerResponse{}, fmt.Errorf("container_context.repo_dir is required")
	}

	checkoutCommand := "git -C " + shellQuote(container.RepoDir) + " checkout " + shellQuote(baseBranch)
	checkoutResult, execErr := runner.Exec(ctx, container.ContainerID, []string{"sh", "-lc", checkoutCommand}, "")
	if execErr != nil || checkoutResult.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_git_cherry_pick_failed: %s", commandFailureMessage([]string{"sh", "-lc", checkoutCommand}, checkoutResult, execErr))
	}

	applied := make([]string, 0, len(commits))
	for _, commit := range commits {
		commit = strings.TrimSpace(commit)
		if commit == "" {
			return core.HandlerResponse{}, fmt.Errorf("commits must contain only non-empty strings")
		}
		command := "git -C " + shellQuote(container.RepoDir) + " cherry-pick " + shellQuote(commit)
		result, execErr := runner.Exec(ctx, container.ContainerID, []string{"sh", "-lc", command}, "")
		if execErr != nil || result.ExitCode != 0 {
			abortCommand := "git -C " + shellQuote(container.RepoDir) + " cherry-pick --abort"
			abortResult, _ := runner.Exec(ctx, container.ContainerID, []string{"sh", "-lc", abortCommand}, "")
			return core.HandlerResponse{
				Data: map[string]any{
					"result":          "conflict",
					"base_branch":     baseBranch,
					"applied_commits": applied,
					"failed_commit":   commit,
					"stdout":          result.Stdout,
					"stderr":          result.Stderr,
					"abort_stdout":    abortResult.Stdout,
					"abort_stderr":    abortResult.Stderr,
					"container_id":    container.ContainerID,
					"repo_dir":        container.RepoDir,
				},
			}, nil
		}
		applied = append(applied, commit)
	}

	headCommand := "git -C " + shellQuote(container.RepoDir) + " rev-parse HEAD"
	headResult, execErr := runner.Exec(ctx, container.ContainerID, []string{"sh", "-lc", headCommand}, "")
	if execErr != nil || headResult.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_git_cherry_pick_failed: %s", commandFailureMessage([]string{"sh", "-lc", headCommand}, headResult, execErr))
	}

	return core.HandlerResponse{
		Data: map[string]any{
			"result":          "kok",
			"base_branch":     baseBranch,
			"applied_commits": applied,
			"merged_commit":   strings.TrimSpace(headResult.Stdout),
			"stdout":          headResult.Stdout,
			"container_id":    container.ContainerID,
			"repo_dir":        container.RepoDir,
		},
	}, nil
}

func handleContainerGitWorktreePrepare(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	resolved, err := resolveContainerModule(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	baseBranch := strings.TrimSpace(resolved.container.BaseBranch)
	branchName := strings.TrimSpace(resolved.spec.BranchName)
	worktreeDir := strings.TrimSpace(resolved.spec.WorktreeDir)

	if baseBranch == "" {
		return core.HandlerResponse{}, fmt.Errorf("container_context.base_branch is required")
	}
	if branchName == "" {
		return core.HandlerResponse{}, fmt.Errorf("module_spec.branch_name is required")
	}
	if worktreeDir == "" {
		return core.HandlerResponse{}, fmt.Errorf("module_spec.worktree_dir is required")
	}

	scriptLines := []string{
		"set -eu",
		"repo_dir=" + shellQuote(resolved.container.RepoDir),
		"base_branch=" + shellQuote(baseBranch),
		"branch_name=" + shellQuote(branchName),
		"worktree_dir=" + shellQuote(worktreeDir),
		"lock_dir=\"$repo_dir/.git/doujia-worktree.lock\"",
		"i=0",
		"while ! mkdir \"$lock_dir\" 2>/dev/null; do i=$((i+1)); [ \"$i\" -lt 60 ] || { echo lock_timeout >&2; exit 97; }; sleep 1; done",
		"trap 'rmdir \"$lock_dir\"' EXIT",
		"git -C \"$repo_dir\" rev-parse --git-dir >/dev/null",
		"git -C \"$repo_dir\" rev-parse --verify \"$base_branch^{commit}\" >/dev/null",
	}
	scriptLines = append(scriptLines, sharedScaffoldScriptLines(resolved.spec)...)
	scriptLines = append(scriptLines,
		"if ! git -C \"$repo_dir\" show-ref --verify --quiet \"refs/heads/$branch_name\"; then git -C \"$repo_dir\" branch -- \"$branch_name\" \"$base_branch\"; fi",
		"if [ -d \"$worktree_dir\" ]; then",
		"  git -C \"$worktree_dir\" rev-parse --is-inside-work-tree >/dev/null 2>&1",
		"  current_branch=$(git -C \"$worktree_dir\" rev-parse --abbrev-ref HEAD)",
		"  [ \"$current_branch\" = \"$branch_name\" ] || { echo branch_mismatch:$current_branch >&2; exit 98; }",
		"  printf 'already_prepared\\n'",
		"  exit 0",
		"fi",
		"mkdir -p \"$(dirname \"$worktree_dir\")\"",
		"git -C \"$repo_dir\" worktree add -- \"$worktree_dir\" \"$branch_name\"",
		"printf 'created\\n'",
	)
	script := strings.Join(scriptLines, "\n")

	result, execErr := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", script}, "")
	if execErr != nil || result.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_git_worktree_prepare_failed: %s", commandFailureMessage([]string{"sh", "-lc", script}, result, execErr))
	}

	return core.HandlerResponse{
		Data: map[string]any{
			"result":       "kok",
			"status":       strings.TrimSpace(result.Stdout),
			"base_branch":  baseBranch,
			"branch_name":  branchName,
			"worktree_dir": worktreeDir,
			"repo_dir":     resolved.container.RepoDir,
			"container_id": resolved.container.ContainerID,
		},
	}, nil
}

func sharedScaffoldScriptLines(spec containerModuleSpec) []string {
	lines := []string{
		"write_if_missing() {",
		"  target=\"$1\"",
		"  if [ -e \"$target\" ]; then return 0; fi",
		"  mkdir -p \"$(dirname \"$target\")\"",
		"  cat > \"$target\"",
		"}",
	}

	switch strings.TrimSpace(spec.ModuleRole) {
	case "frontend":
		lines = append(lines,
			`write_if_missing "$repo_dir/miniprogram/app.js" <<'EOF'
App({});
EOF`,
			`write_if_missing "$repo_dir/miniprogram/app.json" <<'EOF'
{
  "pages": [],
  "window": {
    "navigationBarTitleText": "Doujia"
  }
}
EOF`,
			`write_if_missing "$repo_dir/miniprogram/app.wxss" <<'EOF'
page {
  background: #f7f7f7;
}
EOF`,
		)
	default:
		lines = append(lines,
			`write_if_missing "$repo_dir/server/package.json" <<'EOF'
{
  "name": "doujia-server",
  "private": true,
  "type": "commonjs",
  "scripts": {
    "start": "node app.js",
    "test": "jest --runInBand"
  },
  "dependencies": {
    "better-sqlite3": "^11.0.0",
    "express": "^4.19.2"
  },
  "devDependencies": {
    "jest": "^29.7.0",
    "supertest": "^7.0.0"
  }
}
EOF`,
			`write_if_missing "$repo_dir/server/app.js" <<'EOF'
module.exports = {};
EOF`,
			`write_if_missing "$repo_dir/server/db/database.js" <<'EOF'
module.exports = {};
EOF`,
			`write_if_missing "$repo_dir/server/db/schema.sql" <<'EOF'
-- schema placeholder
EOF`,
			`write_if_missing "$repo_dir/server/utils/validator.js" <<'EOF'
module.exports = {};
EOF`,
		)
	}

	lines = append(lines,
		`if [ -n "$(git -C "$repo_dir" status --porcelain)" ]; then
  git -C "$repo_dir" add -A .
  git -C "$repo_dir" commit -m "chore: scaffold shared project skeleton" >/dev/null
fi`,
	)
	return lines
}

func resolveContainerContext(bundle core.AgentInputBundle) (containerContext, error) {
	containerPath, ok := readPathForLogicalKey(bundle, "container_context")
	if !ok {
		return containerContext{}, fmt.Errorf("artifact_logical_key_not_allowed: %s", "container_context")
	}
	return readJSONFile[containerContext](containerPath)
}
