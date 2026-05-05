package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"doujia/internal/agent/core"
	"doujia/internal/agent/schema"
)

type ContainerCreateHandler struct {
	runner containerRunner
}

type scopedContainerCreateHandler struct {
	bundle core.AgentInputBundle
	runner containerRunner
}

type environmentSpec = schema.EnvironmentSpec

type runtimeContract struct {
	WorkspaceDir string `json:"workspace_dir"`
	RepoDir      string `json:"repo_dir"`
	WorktreesDir string `json:"worktrees_dir"`
	TestRunsDir  string `json:"test_runs_dir"`
	BaseBranch   string `json:"base_branch"`
	BranchPrefix string `json:"branch_prefix"`
}

type containerContext struct {
	SchemaVersion      int    `json:"schema_version"`
	Kind               string `json:"kind"`
	RunID              string `json:"run_id"`
	ContainerID        string `json:"container_id"`
	WorkspaceDir       string `json:"workspace_dir"`
	RepoDir            string `json:"repo_dir"`
	WorktreesDir       string `json:"worktrees_dir"`
	TestRunsDir        string `json:"test_runs_dir"`
	BaseBranch         string `json:"base_branch"`
	BranchPrefix       string `json:"branch_prefix"`
	ActualImage        string `json:"actual_image"`
	Runtime            string `json:"runtime"`
	PackageManager     string `json:"package_manager"`
	DefaultTestCommand string `json:"default_test_command"`
	Status             string `json:"status"`
}

type containerRunner interface {
	CheckDocker(ctx context.Context) error
	Pull(ctx context.Context, image string) error
	Create(ctx context.Context, name string, image string, workspaceDir string) (string, error)
	Exec(ctx context.Context, containerID string, args []string, stdin string) (createContainerCommandResult, error)
}

type createContainerCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type dockerContainerRunner struct{}

func NewContainerCreateHandler() *ContainerCreateHandler {
	return &ContainerCreateHandler{runner: dockerContainerRunner{}}
}

func NewContainerCreateHandlerWithRunner(runner containerRunner) *ContainerCreateHandler {
	if runner == nil {
		runner = dockerContainerRunner{}
	}
	return &ContainerCreateHandler{runner: runner}
}

func (h *ContainerCreateHandler) Name() string {
	return "container_create"
}

func (h *ContainerCreateHandler) Description() string {
	return "根据 environment_spec 和可选 runtime_contract 创建容器上下文"
}

func (h *ContainerCreateHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        "container_create",
		Description: "根据 environment_spec 和可选 runtime_contract 创建容器上下文",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"environment_spec_key"},
			"properties": map[string]any{
				"environment_spec_key": map[string]any{
					"type":        "string",
					"description": "environment_spec 输入产物的 logical_key。",
				},
				"runtime_contract_key": map[string]any{
					"type":        "string",
					"description": "可选的 runtime_contract 输入产物 logical_key。",
				},
			},
		},
	}
}

func (h *ContainerCreateHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleContainerCreate(ctx, req, h.runner)
}

func (h *ContainerCreateHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedContainerCreateHandler{bundle: bundle, runner: h.runner}
}

func (h *scopedContainerCreateHandler) Name() string {
	return "container_create"
}

func (h *scopedContainerCreateHandler) Description() string {
	return "根据 environment_spec 和可选 runtime_contract 创建容器上下文"
}

func (h *scopedContainerCreateHandler) ToolSpec() core.ToolSpec {
	return (&ContainerCreateHandler{runner: h.runner}).ToolSpec()
}

func (h *scopedContainerCreateHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleContainerCreate(ctx, req, h.runner)
}

func handleContainerCreate(ctx context.Context, req core.HandlerRequest, runner containerRunner) (core.HandlerResponse, error) {
	if _, ok := req.Args["path"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("path is controlled by logical_key")
	}
	if runner == nil {
		runner = dockerContainerRunner{}
	}

	inputs, err := resolveContainerInputs(req)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if err := validateContainerInputs(inputs.environment, inputs.runtime); err != nil {
		return core.HandlerResponse{}, err
	}
	if err := runner.CheckDocker(ctx); err != nil {
		return core.HandlerResponse{}, fmt.Errorf("docker_unavailable: %w", err)
	}

	image := chooseContainerImage(inputs.environment)
	if !allowedContainerImage(image) {
		return core.HandlerResponse{}, fmt.Errorf("image_not_allowed: %s", image)
	}
	if err := runner.Pull(ctx, image); err != nil {
		return core.HandlerResponse{}, fmt.Errorf("image_pull_failed: %w", err)
	}

	containerID, err := runner.Create(ctx, createContainerName(req.Task.RunID), image, inputs.runtime.WorkspaceDir)
	if err != nil {
		return core.HandlerResponse{}, fmt.Errorf("container_start_failed: %w", err)
	}
	if err := initializeContainerWorkspace(ctx, runner, containerID, inputs.runtime); err != nil {
		return core.HandlerResponse{}, err
	}
	if err := runEnvironmentChecks(ctx, runner, containerID, inputs.environment.CheckCommands); err != nil {
		return core.HandlerResponse{}, err
	}
	if err := initializeContainerRepo(ctx, runner, containerID, inputs.environment, inputs.runtime); err != nil {
		return core.HandlerResponse{}, err
	}

	contextData := containerContext{
		SchemaVersion:      2,
		Kind:               "container_context",
		RunID:              req.Task.RunID,
		ContainerID:        containerID,
		WorkspaceDir:       inputs.runtime.WorkspaceDir,
		RepoDir:            inputs.runtime.RepoDir,
		WorktreesDir:       inputs.runtime.WorktreesDir,
		TestRunsDir:        inputs.runtime.TestRunsDir,
		BaseBranch:         inputs.runtime.BaseBranch,
		BranchPrefix:       inputs.runtime.BranchPrefix,
		ActualImage:        image,
		Runtime:            inputs.environment.Runtime,
		PackageManager:     inputs.environment.PackageManager,
		DefaultTestCommand: inputs.environment.DefaultTestCommand,
		Status:             "ready",
	}
	content, err := json.MarshalIndent(contextData, "", "  ")
	if err != nil {
		return core.HandlerResponse{}, err
	}

	return core.HandlerResponse{
		Data: map[string]any{
			"content":           string(content),
			"container_context": contextData,
		},
	}, nil
}

type resolvedContainerInputs struct {
	environment environmentSpec
	runtime     runtimeContract
}

func resolveContainerInputs(req core.HandlerRequest) (resolvedContainerInputs, error) {
	var resolved resolvedContainerInputs

	environmentSpecKey, err := requiredStringArg(req.Args, "environment_spec_key")
	if err != nil {
		return resolved, err
	}
	environmentSpecPath, ok := readPathForLogicalKey(req.Bundle, environmentSpecKey)
	if !ok {
		return resolved, fmt.Errorf("artifact_logical_key_not_allowed: %s", environmentSpecKey)
	}
	resolved.environment, err = schema.ReadEnvironmentSpecFile(environmentSpecPath)
	if err != nil {
		return resolved, err
	}

	resolved.runtime = defaultRuntimeContract()
	if runtimeContractKey, ok := req.Args["runtime_contract_key"]; ok {
		key, ok := runtimeContractKey.(string)
		if !ok || key == "" {
			return resolved, fmt.Errorf("arg %q must be a non-empty string", "runtime_contract_key")
		}
		runtimePath, ok := readPathForLogicalKey(req.Bundle, key)
		if !ok {
			return resolved, fmt.Errorf("artifact_logical_key_not_allowed: %s", key)
		}
		resolved.runtime, err = readJSONFile[runtimeContract](runtimePath)
		if err != nil {
			return resolved, err
		}
	}
	return resolved, nil
}

func validateContainerInputs(env environmentSpec, runtime runtimeContract) error {
	required := map[string]string{
		"workspace_dir":        runtime.WorkspaceDir,
		"repo_dir":             runtime.RepoDir,
		"worktrees_dir":        runtime.WorktreesDir,
		"test_runs_dir":        runtime.TestRunsDir,
		"base_branch":          runtime.BaseBranch,
		"runtime":              env.Runtime,
		"image":                schema.ChooseContainerImage(env),
		"default_test_command": env.DefaultTestCommand,
		"package_manager":      env.PackageManager,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing_required_field: %s", field)
		}
	}
	for _, dir := range []string{runtime.WorkspaceDir, runtime.RepoDir, runtime.WorktreesDir, runtime.TestRunsDir} {
		if !isAbsoluteContainerPath(dir) {
			return fmt.Errorf("invalid_create_container_input: container paths must be absolute: %s", dir)
		}
	}
	for _, dir := range []string{runtime.RepoDir, runtime.WorktreesDir, runtime.TestRunsDir} {
		if !pathWithin(runtime.WorkspaceDir, dir) {
			return fmt.Errorf("invalid_create_container_input: %s must be under %s", dir, runtime.WorkspaceDir)
		}
	}
	if err := env.Validate(); err != nil {
		return err
	}
	return nil
}

func chooseContainerImage(env environmentSpec) string {
	return schema.ChooseContainerImage(env)
}

func defaultRuntimeContract() runtimeContract {
	return runtimeContract{
		WorkspaceDir: "/workspace",
		RepoDir:      "/workspace/repo",
		WorktreesDir: "/workspace/worktrees",
		TestRunsDir:  "/workspace/test-runs",
		BaseBranch:   "main",
		BranchPrefix: "feature/",
	}
}

func readJSONFile[T any](path string) (T, error) {
	var value T
	absPath, err := filepath.Abs(path)
	if err != nil {
		return value, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(content, &value); err != nil {
		return value, err
	}
	return value, nil
}

func initializeContainerWorkspace(ctx context.Context, runner containerRunner, containerID string, runtime runtimeContract) error {
	args := []string{"mkdir", "-p", runtime.WorkspaceDir, runtime.RepoDir, runtime.WorktreesDir, runtime.TestRunsDir}
	if _, err := runContainerCommand(ctx, runner, containerID, "workspace_init_failed", args, ""); err != nil {
		return err
	}
	return nil
}

func runEnvironmentChecks(ctx context.Context, runner containerRunner, containerID string, commands []string) error {
	for _, command := range commands {
		args := strings.Fields(command)
		if _, err := runContainerCommand(ctx, runner, containerID, "environment_check_failed", args, ""); err != nil {
			return err
		}
	}
	return nil
}

func initializeContainerRepo(ctx context.Context, runner containerRunner, containerID string, env environmentSpec, runtime runtimeContract) error {
	repoDir := runtime.RepoDir
	if _, err := runContainerCommand(ctx, runner, containerID, "git_init_failed", []string{"git", "-C", repoDir, "init", "-b", runtime.BaseBranch}, ""); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "config", "user.name", "Doujia Architect"},
		{"git", "-C", repoDir, "config", "user.email", "architect@doujia.local"},
	} {
		if _, err := runContainerCommand(ctx, runner, containerID, "git_init_failed", args, ""); err != nil {
			return err
		}
	}

	files := normalizedRepoInitFiles(env.RepoInitFiles)
	for name, content := range files {
		target := path.Join(repoDir, path.Clean(name))
		script := "mkdir -p " + shellQuote(path.Dir(target)) + " && cat > " + shellQuote(target)
		if _, err := runContainerCommand(ctx, runner, containerID, "git_init_failed", []string{"sh", "-c", script}, content); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "add", "-A", "."},
		{"git", "-C", repoDir, "commit", "-m", "chore: initialize generated project workspace"},
	} {
		if _, err := runContainerCommand(ctx, runner, containerID, "initial_commit_failed", args, ""); err != nil {
			return err
		}
	}
	return nil
}

func runContainerCommand(ctx context.Context, runner containerRunner, containerID string, reason string, args []string, stdin string) (createContainerCommandResult, error) {
	result, err := runner.Exec(ctx, containerID, args, stdin)
	if err != nil || result.ExitCode != 0 {
		return result, fmt.Errorf("%s: %s", reason, commandFailureMessage(args, result, err))
	}
	return result, nil
}

func normalizedRepoInitFiles(files map[string]string) map[string]string {
	out := map[string]string{}
	for name, content := range files {
		if safeRepoInitFile(name) {
			out[path.Clean(strings.ReplaceAll(name, "\\", "/"))] = content
		}
	}
	if _, ok := out["README.md"]; !ok {
		out["README.md"] = "# Doujia Generated Project\n"
	}
	return out
}

func commandFailureMessage(args []string, result createContainerCommandResult, err error) string {
	parts := []string{"command=" + strings.Join(args, " ")}
	if err != nil {
		parts = append(parts, "error="+err.Error())
	}
	if strings.TrimSpace(result.Stdout) != "" {
		parts = append(parts, "stdout="+strings.TrimSpace(result.Stdout))
	}
	if strings.TrimSpace(result.Stderr) != "" {
		parts = append(parts, "stderr="+strings.TrimSpace(result.Stderr))
	}
	if result.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit_code=%d", result.ExitCode))
	}
	return strings.Join(parts, "\n")
}

func allowedContainerImage(image string) bool {
	return schema.AllowedContainerImage(image)
}

func allowedCheckCommand(command string) bool {
	return schema.AllowedCheckCommand(command)
}

func isAbsoluteContainerPath(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "\x00")
}

func pathWithin(parent string, child string) bool {
	parent = path.Clean("/" + strings.TrimPrefix(parent, "/"))
	child = path.Clean("/" + strings.TrimPrefix(child, "/"))
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func safeRepoInitFile(name string) bool {
	return schema.SafeRepoInitFile(name)
}

func createContainerName(runID string) string {
	slug := strings.ToLower(strings.TrimSpace(runID))
	re := regexp.MustCompile(`[^a-z0-9_.-]+`)
	slug = re.ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_.-")
	if slug == "" {
		slug = "run"
	}
	return "doujia_run_" + slug
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (dockerContainerRunner) CheckDocker(ctx context.Context) error {
	runCtx, cancel := createContainerCommandContext(ctx)
	defer cancel()
	_, err := runHostCommand(runCtx, "docker", "version", "--format", "{{.Server.Version}}")
	return err
}

func (dockerContainerRunner) Pull(ctx context.Context, image string) error {
	runCtx, cancel := createContainerCommandContext(ctx)
	defer cancel()
	if _, err := runHostCommand(runCtx, "docker", "image", "inspect", image); err == nil {
		return nil
	}
	_, err := runHostCommand(runCtx, "docker", "pull", image)
	return err
}

func (dockerContainerRunner) Create(ctx context.Context, name string, image string, workspaceDir string) (string, error) {
	runCtx, cancel := createContainerCommandContext(ctx)
	defer cancel()
	result, err := runHostCommand(runCtx, "docker", "run", "-d", "--name", name, "-w", workspaceDir, image, "sleep", "infinity")
	if err != nil {
		return "", err
	}
	containerID := strings.TrimSpace(result.Stdout)
	if containerID == "" {
		return "", fmt.Errorf("docker run returned empty container id")
	}
	return containerID, nil
}

func (dockerContainerRunner) Exec(ctx context.Context, containerID string, args []string, stdin string) (createContainerCommandResult, error) {
	runCtx, cancel := createContainerCommandContext(ctx)
	defer cancel()
	dockerArgs := []string{"exec"}
	if stdin != "" {
		dockerArgs = append(dockerArgs, "-i")
	}
	dockerArgs = append(dockerArgs, containerID)
	dockerArgs = append(dockerArgs, args...)
	return runHostCommandWithInput(runCtx, stdin, "docker", dockerArgs...)
}

func createContainerCommandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := 3 * time.Minute
	return context.WithTimeout(ctx, timeout)
}

func runHostCommand(ctx context.Context, name string, args ...string) (createContainerCommandResult, error) {
	return runHostCommandWithInput(ctx, "", name, args...)
}

func runHostCommandWithInput(ctx context.Context, stdin string, name string, args ...string) (createContainerCommandResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := createContainerCommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: commandExitCode(err),
		Duration: time.Since(start),
	}
	if err != nil {
		return result, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
