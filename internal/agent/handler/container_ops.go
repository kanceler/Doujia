package handler

import (
	"context"
	"fmt"
	"path"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
)

type containerModuleSpec = schema.ModuleSpec

type ContainerReadHandler struct {
	runner containerRunner
}

type ContainerWriteHandler struct {
	runner containerRunner
}

type ContainerRunHandler struct {
	runner containerRunner
}

type ContainerExecHandler struct {
	runner containerRunner
}

type ContainerGitCommitHandler struct {
	runner containerRunner
}

type scopedContainerOpsHandler struct {
	kind   string
	bundle core.AgentInputBundle
	runner containerRunner
}

func NewContainerReadHandler() *ContainerReadHandler {
	return &ContainerReadHandler{runner: dockerContainerRunner{}}
}

func NewContainerReadHandlerWithRunner(runner containerRunner) *ContainerReadHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerReadHandler{runner: runner}
}

func NewContainerWriteHandler() *ContainerWriteHandler {
	return &ContainerWriteHandler{runner: dockerContainerRunner{}}
}

func NewContainerWriteHandlerWithRunner(runner containerRunner) *ContainerWriteHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerWriteHandler{runner: runner}
}

func NewContainerRunHandler() *ContainerRunHandler {
	return &ContainerRunHandler{runner: dockerContainerRunner{}}
}

func NewContainerRunHandlerWithRunner(runner containerRunner) *ContainerRunHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerRunHandler{runner: runner}
}

func NewContainerExecHandler() *ContainerExecHandler {
	return &ContainerExecHandler{runner: dockerContainerRunner{}}
}

func NewContainerExecHandlerWithRunner(runner containerRunner) *ContainerExecHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerExecHandler{runner: runner}
}

func NewContainerGitCommitHandler() *ContainerGitCommitHandler {
	return &ContainerGitCommitHandler{runner: dockerContainerRunner{}}
}

func NewContainerGitCommitHandlerWithRunner(runner containerRunner) *ContainerGitCommitHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerGitCommitHandler{runner: runner}
}

func (h *ContainerReadHandler) Name() string { return "container_read" }
func (h *ContainerReadHandler) Description() string {
	return "在容器内读取当前模块工作区中的文件"
}
func (h *ContainerReadHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"relative_path"},
			"properties": map[string]any{
				"relative_path": map[string]any{"type": "string", "description": "Relative file path inside the module worktree."},
			},
		},
	}
}
func (h *ContainerReadHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerRead(ctx, req, h.runner)
}
func (h *ContainerReadHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerOpsHandler{kind: h.Name(), bundle: bundle, runner: h.runner}
}

func (h *ContainerWriteHandler) Name() string { return "container_write" }
func (h *ContainerWriteHandler) Description() string {
	return "在容器内向当前模块工作区写入文件"
}
func (h *ContainerWriteHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"relative_path", "content"},
			"properties": map[string]any{
				"relative_path": map[string]any{"type": "string", "description": "模块工作区内的相对文件路径。"},
				"content":       map[string]any{"type": "string", "description": "要写入的完整文件内容。"},
			},
		},
	}
}
func (h *ContainerWriteHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerWrite(ctx, req, h.runner)
}
func (h *ContainerWriteHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerOpsHandler{kind: h.Name(), bundle: bundle, runner: h.runner}
}

func (h *ContainerRunHandler) Name() string { return "container_run" }
func (h *ContainerRunHandler) Description() string {
	return "在容器内的当前模块工作区执行一条命令"
}
func (h *ContainerRunHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"command"},
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要在模块工作区内执行的 shell 命令。"},
			},
		},
	}
}
func (h *ContainerRunHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerRun(ctx, req, h.runner)
}
func (h *ContainerRunHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerOpsHandler{kind: h.Name(), bundle: bundle, runner: h.runner}
}

func (h *ContainerExecHandler) Name() string { return "container_exec" }
func (h *ContainerExecHandler) Description() string {
	return "在容器内的仓库根目录执行一条命令"
}
func (h *ContainerExecHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"command"},
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要在仓库根目录执行的 shell 命令。"},
			},
		},
	}
}
func (h *ContainerExecHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerExec(ctx, req, h.runner)
}
func (h *ContainerExecHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerOpsHandler{kind: h.Name(), bundle: bundle, runner: h.runner}
}

func (h *ContainerGitCommitHandler) Name() string { return "container_git_commit" }
func (h *ContainerGitCommitHandler) Description() string {
	return "在容器内提交当前模块工作区的代码改动并返回 HEAD"
}
func (h *ContainerGitCommitHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        h.Name(),
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"message"},
			"properties": map[string]any{
				"message": map[string]any{"type": "string", "description": "Git 提交信息。"},
			},
		},
	}
}
func (h *ContainerGitCommitHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerGitCommit(ctx, req, h.runner)
}
func (h *ContainerGitCommitHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerOpsHandler{kind: h.Name(), bundle: bundle, runner: h.runner}
}

func (h *scopedContainerOpsHandler) Name() string { return h.kind }
func (h *scopedContainerOpsHandler) Description() string {
	switch h.kind {
	case "container_read":
		return "在容器内读取当前模块工作区中的文件"
	case "container_write":
		return "在容器内向当前模块工作区写入文件"
	case "container_run":
		return "在容器内的当前模块工作区执行一条命令"
	case "container_exec":
		return "在容器内的仓库根目录执行一条命令"
	default:
		return "在容器内提交当前模块工作区的代码改动并返回 HEAD"
	}
}
func (h *scopedContainerOpsHandler) ToolSpec() core.ToolSpec {
	switch h.kind {
	case "container_read":
		return NewContainerReadHandlerWithRunner(h.runner).ToolSpec()
	case "container_write":
		return NewContainerWriteHandlerWithRunner(h.runner).ToolSpec()
	case "container_run":
		return NewContainerRunHandlerWithRunner(h.runner).ToolSpec()
	case "container_exec":
		return NewContainerExecHandlerWithRunner(h.runner).ToolSpec()
	default:
		return NewContainerGitCommitHandlerWithRunner(h.runner).ToolSpec()
	}
}
func (h *scopedContainerOpsHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	switch h.kind {
	case "container_read":
		return handleContainerRead(ctx, req, h.runner)
	case "container_write":
		return handleContainerWrite(ctx, req, h.runner)
	case "container_run":
		return handleContainerRun(ctx, req, h.runner)
	case "container_exec":
		return handleContainerExec(ctx, req, h.runner)
	default:
		return handleContainerGitCommit(ctx, req, h.runner)
	}
}

type resolvedContainerModule struct {
	container containerContext
	spec      containerModuleSpec
}

type resolvedContainerRepo struct {
	container containerContext
}

func resolveContainerModule(req core.HandlerRequest) (resolvedContainerModule, error) {
	var resolved resolvedContainerModule
	containerPath, ok := readPathForLogicalKey(req.Bundle, "container_context")
	if !ok {
		return resolved, fmt.Errorf("artifact_logical_key_not_allowed: %s", "container_context")
	}
	modulePath, ok := readPathForLogicalKey(req.Bundle, "module_spec")
	if !ok {
		return resolved, fmt.Errorf("artifact_logical_key_not_allowed: %s", "module_spec")
	}
	container, err := readJSONFile[containerContext](containerPath)
	if err != nil {
		return resolved, err
	}
	spec, err := schema.ReadModuleSpecFile(modulePath)
	if err != nil {
		return resolved, err
	}
	if strings.TrimSpace(container.ContainerID) == "" {
		return resolved, fmt.Errorf("container_context.container_id is required")
	}
	if err := spec.Validate(); err != nil {
		return resolved, fmt.Errorf("invalid module_spec: %w", err)
	}
	resolved.container = container
	resolved.spec = spec
	return resolved, nil
}

func resolveContainerRepo(req core.HandlerRequest) (resolvedContainerRepo, error) {
	var resolved resolvedContainerRepo
	containerPath, ok := readPathForLogicalKey(req.Bundle, "container_context")
	if !ok {
		return resolved, fmt.Errorf("artifact_logical_key_not_allowed: %s", "container_context")
	}
	container, err := readJSONFile[containerContext](containerPath)
	if err != nil {
		return resolved, err
	}
	if strings.TrimSpace(container.ContainerID) == "" {
		return resolved, fmt.Errorf("container_context.container_id is required")
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		return resolved, fmt.Errorf("container_context.repo_dir is required")
	}
	resolved.container = container
	return resolved, nil
}

func handleContainerRead(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	relativePath, err := requiredStringArg(req.Args, "relative_path")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	resolved, err := resolveContainerModule(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	normalized := strings.TrimSpace(strings.ReplaceAll(relativePath, "\\", "/"))
	if normalized == "." {
		command := "cd " + shellQuote(resolved.spec.WorktreeDir) + " && find . -maxdepth 3 -type f | sort"
		result, execErr := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", command}, "")
		if execErr != nil || result.ExitCode != 0 {
			return core.HandlerResponse{}, fmt.Errorf("container_read_failed: %s", commandFailureMessage([]string{"sh", "-lc", command}, result, execErr))
		}
		content := filterContainerRootListing(result.Stdout, resolved.spec)
		return core.HandlerResponse{Data: map[string]any{
			"relative_path": ".",
			"content":       content,
			"container_id":  resolved.container.ContainerID,
		}}, nil
	}
	normalized, err = normalizeContainerRelativePath(relativePath)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	target := path.Join(resolved.spec.WorktreeDir, normalized)
	result, err := runner.Exec(ctx, resolved.container.ContainerID, []string{"cat", target}, "")
	if err != nil || result.ExitCode != 0 {
		if strings.Contains(strings.ToLower(result.Stderr), "no such file or directory") {
			return core.HandlerResponse{Data: map[string]any{
				"relative_path": normalized,
				"content":       "",
				"exists":        false,
				"container_id":  resolved.container.ContainerID,
			}}, nil
		}
		return core.HandlerResponse{}, fmt.Errorf("container_read_failed: %s", commandFailureMessage([]string{"cat", target}, result, err))
	}
	return core.HandlerResponse{Data: map[string]any{
		"relative_path": normalized,
		"content":       result.Stdout,
		"exists":        true,
		"container_id":  resolved.container.ContainerID,
	}}, nil
}

func handleContainerWrite(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	relativePath, err := requiredStringArg(req.Args, "relative_path")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	content, err := requiredStringArg(req.Args, "content")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	resolved, err := resolveContainerModule(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	normalized, err := normalizeContainerRelativePath(relativePath)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if runtimeErr := validateContainerWritablePath(normalized, resolved.spec); runtimeErr != nil {
		return core.HandlerResponse{}, runtimeErr
	}
	target := path.Join(resolved.spec.WorktreeDir, normalized)
	parent := path.Dir(target)
	command := "mkdir -p " + shellQuote(parent) + " && cat > " + shellQuote(target)
	result, err := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", command}, content)
	if err != nil || result.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_write_failed: %s", commandFailureMessage([]string{"sh", "-lc", command}, result, err))
	}
	return core.HandlerResponse{Data: map[string]any{
		"relative_path": normalized,
		"status":        "written",
		"container_id":  resolved.container.ContainerID,
	}}, nil
}

func handleContainerRun(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	command, err := requiredStringArg(req.Args, "command")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	resolved, err := resolveContainerModule(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	script := "cd " + shellQuote(resolved.spec.WorktreeDir) + " && " + command
	result, err := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", script}, "")
	if shouldFailContainerRun(err, result) {
		return core.HandlerResponse{}, fmt.Errorf("container_run_failed: %s", commandFailureMessage([]string{"sh", "-lc", script}, result, err))
	}
	return core.HandlerResponse{Data: map[string]any{
		"exit_code":    result.ExitCode,
		"stdout":       result.Stdout,
		"stderr":       result.Stderr,
		"duration_ms":  result.Duration.Milliseconds(),
		"container_id": resolved.container.ContainerID,
	}}, nil
}

func handleContainerExec(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	command, err := requiredStringArg(req.Args, "command")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	resolved, err := resolveContainerRepo(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	script := "cd " + shellQuote(resolved.container.RepoDir) + " && " + command
	result, err := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", script}, "")
	if shouldFailContainerRun(err, result) {
		return core.HandlerResponse{}, fmt.Errorf("container_exec_failed: %s", commandFailureMessage([]string{"sh", "-lc", script}, result, err))
	}
	return core.HandlerResponse{Data: map[string]any{
		"exit_code":    result.ExitCode,
		"stdout":       result.Stdout,
		"stderr":       result.Stderr,
		"duration_ms":  result.Duration.Milliseconds(),
		"container_id": resolved.container.ContainerID,
		"cwd":          resolved.container.RepoDir,
	}}, nil
}

func shouldFailContainerRun(err error, result createContainerCommandResult) bool {
	if err == nil {
		return false
	}
	if result.ExitCode <= 0 {
		return true
	}
	text := strings.ToLower(err.Error() + "\n" + result.Stderr)
	for _, marker := range []string{
		"error response from daemon",
		"no such container",
		"container is not running",
		"is not running",
		"cannot exec in a stopped state",
		"unable to upgrade connection",
		"context deadline exceeded",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func handleContainerGitCommit(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	message, err := requiredStringArg(req.Args, "message")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	resolved, err := resolveContainerModule(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	statusCommand := "cd " + shellQuote(resolved.spec.WorktreeDir) + " && git status --porcelain"
	statusResult, execErr := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", statusCommand}, "")
	if execErr != nil || statusResult.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %s", commandFailureMessage([]string{"sh", "-lc", statusCommand}, statusResult, execErr))
	}
	changedFiles, err := collectChangedFiles(statusResult.Stdout)
	if err != nil {
		return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %w", err)
	}
	changedFiles, err = materializeChangedDirectories(ctx, runner, resolved.container.ContainerID, resolved.spec.WorktreeDir, changedFiles)
	if err != nil {
		return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %w", err)
	}
	if err := validateChangedFiles(changedFiles, resolved.spec); err != nil {
		return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %w", err)
	}
	baseCommitCommand := "git -C " + shellQuote(resolved.container.RepoDir) + " rev-parse " + shellQuote(resolved.container.BaseBranch)
	baseCommitResult, execErr := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", baseCommitCommand}, "")
	if execErr != nil || baseCommitResult.ExitCode != 0 {
		return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %s", commandFailureMessage([]string{"sh", "-lc", baseCommitCommand}, baseCommitResult, execErr))
	}
	commands := []string{
		"cd " + shellQuote(resolved.spec.WorktreeDir) + " && git add -A .",
		"cd " + shellQuote(resolved.spec.WorktreeDir) + " && git commit -m " + shellQuote(message),
		"cd " + shellQuote(resolved.spec.WorktreeDir) + " && git rev-parse HEAD",
	}
	for i, command := range commands {
		result, execErr := runner.Exec(ctx, resolved.container.ContainerID, []string{"sh", "-lc", command}, "")
		if execErr != nil || result.ExitCode != 0 {
			return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %s", commandFailureMessage([]string{"sh", "-lc", command}, result, execErr))
		}
		if i == len(commands)-1 {
			commitID := strings.TrimSpace(result.Stdout)
			if err := verifyChangedFilesCommitted(ctx, runner, resolved.container.ContainerID, resolved.spec.WorktreeDir, commitID, changedFiles); err != nil {
				return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: %w", err)
			}
			return core.HandlerResponse{Data: map[string]any{
				"module_id":     resolved.spec.ModuleID,
				"branch":        resolved.spec.BranchName,
				"worktree":      resolved.spec.WorktreeDir,
				"repo_dir":      resolved.container.RepoDir,
				"base_branch":   resolved.container.BaseBranch,
				"base_commit":   strings.TrimSpace(baseCommitResult.Stdout),
				"test_command":  resolved.spec.TestCommand,
				"commit":        commitID,
				"changed_files": changedFiles,
				"container_id":  resolved.container.ContainerID,
			}}, nil
		}
	}
	return core.HandlerResponse{}, fmt.Errorf("container_git_commit_failed: missing commit result")
}

func verifyChangedFilesCommitted(ctx context.Context, runner containerRunner, containerID string, worktreeDir string, commitID string, changedFiles []string) error {
	commitID = strings.TrimSpace(commitID)
	if commitID == "" {
		return fmt.Errorf("commit id is empty")
	}
	command := "cd " + shellQuote(worktreeDir) + " && git diff-tree --no-commit-id --name-only -r " + shellQuote(commitID)
	result, execErr := runner.Exec(ctx, containerID, []string{"sh", "-lc", command}, "")
	if execErr != nil || result.ExitCode != 0 {
		return fmt.Errorf("verify changed files in commit %q: %s", commitID, commandFailureMessage([]string{"sh", "-lc", command}, result, execErr))
	}
	committed, err := collectCommittedFiles(result.Stdout)
	if err != nil {
		return fmt.Errorf("verify changed files in commit %q: %w", commitID, err)
	}
	for _, file := range changedFiles {
		file = strings.TrimSpace(strings.ReplaceAll(file, "\\", "/"))
		if file == "" {
			continue
		}
		if strings.HasSuffix(file, "/") {
			file = strings.TrimRight(file, "/") + "/.gitkeep"
		}
		if !committed[file] {
			return fmt.Errorf("changed file %q is not present in commit %q", file, commitID)
		}
	}
	return nil
}

func collectCommittedFiles(stdout string) (map[string]bool, error) {
	out := map[string]bool{}
	lines := strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		normalized, err := normalizeContainerRelativePath(line)
		if err != nil {
			return nil, fmt.Errorf("invalid committed file %q: %w", line, err)
		}
		out[normalized] = true
	}
	return out, nil
}

func materializeChangedDirectories(ctx context.Context, runner containerRunner, containerID string, worktreeDir string, changedFiles []string) ([]string, error) {
	dirs := changedDirectories(changedFiles)
	if len(dirs) == 0 {
		return changedFiles, nil
	}
	for _, dir := range dirs {
		gitkeep := path.Join(worktreeDir, dir, ".gitkeep")
		command := "mkdir -p " + shellQuote(path.Dir(gitkeep)) + " && : > " + shellQuote(gitkeep)
		result, execErr := runner.Exec(ctx, containerID, []string{"sh", "-lc", command}, "")
		if execErr != nil || result.ExitCode != 0 {
			return nil, fmt.Errorf("materialize empty directory %q: %s", dir, commandFailureMessage([]string{"sh", "-lc", command}, result, execErr))
		}
	}
	statusCommand := "cd " + shellQuote(worktreeDir) + " && git status --porcelain"
	statusResult, execErr := runner.Exec(ctx, containerID, []string{"sh", "-lc", statusCommand}, "")
	if execErr != nil || statusResult.ExitCode != 0 {
		return nil, fmt.Errorf("refresh changed files after .gitkeep: %s", commandFailureMessage([]string{"sh", "-lc", statusCommand}, statusResult, execErr))
	}
	return collectChangedFiles(statusResult.Stdout)
}

func changedDirectories(files []string) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	for _, file := range files {
		file = strings.TrimSpace(strings.ReplaceAll(file, "\\", "/"))
		if !strings.HasSuffix(file, "/") {
			continue
		}
		dir := strings.Trim(file, "/")
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

func collectChangedFiles(stdout string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	files := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 || !strings.Contains(trimmed, " ") {
			return nil, fmt.Errorf("invalid git status line: %q", line)
		}
		filePart := strings.TrimSpace(trimmed[strings.Index(trimmed, " ")+1:])
		if idx := strings.LastIndex(filePart, " -> "); idx >= 0 {
			filePart = strings.TrimSpace(filePart[idx+4:])
		}
		isDir := strings.HasSuffix(strings.ReplaceAll(filePart, "\\", "/"), "/")
		normalized, err := normalizeContainerRelativePath(filePart)
		if err != nil {
			return nil, fmt.Errorf("invalid changed file %q: %w", filePart, err)
		}
		if isDir && !strings.HasSuffix(normalized, "/") {
			normalized += "/"
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		files = append(files, normalized)
	}
	return files, nil
}

func validateChangedFiles(files []string, spec containerModuleSpec) error {
	for _, file := range files {
		if matchesModulePattern(file, spec.ForbiddenPaths) {
			return fmt.Errorf("changed_files hit forbidden_paths: %s", file)
		}
		if allowed := runtimeWritablePatterns(spec); len(allowed) > 0 && !matchesModulePattern(file, allowed) {
			return fmt.Errorf("changed_files outside owned_paths/runtime_write_paths: %s", file)
		}
	}
	return nil
}

func normalizeContainerRelativePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return "", fmt.Errorf("relative_path is required")
	}
	if strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("relative_path must not be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("relative_path escapes worktree")
	}
	return strings.TrimPrefix(cleaned, "./"), nil
}

func validateContainerWritablePath(relativePath string, spec containerModuleSpec) error {
	if matchesModulePattern(relativePath, spec.ForbiddenPaths) {
		return fmt.Errorf("relative_path hits forbidden_paths: %s", relativePath)
	}
	if allowed := runtimeWritablePatterns(spec); len(allowed) > 0 && !matchesModulePattern(relativePath, allowed) {
		return fmt.Errorf("relative_path is outside owned_paths/runtime_write_paths: %s", relativePath)
	}
	return nil
}

func runtimeWritablePatterns(spec containerModuleSpec) []string {
	patterns := make([]string, 0, len(spec.OwnedPaths)+len(spec.RuntimeWritePaths))
	patterns = append(patterns, spec.OwnedPaths...)
	patterns = append(patterns, spec.RuntimeWritePaths...)
	return patterns
}

func filterContainerRootListing(stdout string, spec containerModuleSpec) string {
	allowed := runtimeWritablePatterns(spec)
	if len(allowed) == 0 {
		return stdout
	}

	lines := strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized := strings.TrimPrefix(trimmed, "./")
		if matchesModulePattern(normalized, allowed) {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, "\n") + "\n"
}

func matchesModulePattern(file string, patterns []string) bool {
	file = strings.TrimPrefix(path.Clean(strings.ReplaceAll(file, "\\", "/")), "./")
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if file == prefix || strings.HasPrefix(file, prefix+"/") {
				return true
			}
			continue
		}
		if file == pattern {
			return true
		}
	}
	return false
}
