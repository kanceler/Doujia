package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devflow/internal/core"
)

const (
	PinnedCodingAgentEngineVersion = "1.14.27"
)

type OpenCodeRunner interface {
	Run(ctx context.Context, req OpenCodeRequest) (OpenCodeResult, error)
}

type OpenCodeRequest struct {
	WorkDir string
	Prompt  string
	Model   string
	LLM     core.LLMConfig
	Timeout time.Duration
}

type OpenCodeResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

type ManagedOpenCodeRunner struct {
	StartRoot  string
	BinaryPath string
}

func NewManagedOpenCodeRunner(startRoot string) *ManagedOpenCodeRunner {
	return &ManagedOpenCodeRunner{StartRoot: startRoot}
}

func (r *ManagedOpenCodeRunner) Run(ctx context.Context, req OpenCodeRequest) (OpenCodeResult, error) {
	if strings.TrimSpace(req.WorkDir) == "" {
		return OpenCodeResult{}, fmt.Errorf("coding agent workdir is required")
	}
	binary, err := r.resolveBinary()
	if err != nil {
		return OpenCodeResult{}, err
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, binary, openCodeArgs(req)...)
	cmd.Dir = req.WorkDir
	cmd.Env = openCodeEnv(req.LLM)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	result := OpenCodeResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
		Duration: time.Since(start),
	}
	if err != nil {
		return result, fmt.Errorf("coding agent run failed: %w\n%s", err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func openCodeArgs(req OpenCodeRequest) []string {
	args := []string{
		"run",
		"--pure",
		"--format", "json",
		"--dangerously-skip-permissions",
	}
	if model := codingAgentModel(req); model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, req.Prompt)
	return args
}

func codingAgentModel(req OpenCodeRequest) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(req.LLM.Model)
	}
	if model == "" {
		return ""
	}
	if strings.TrimSpace(req.LLM.BaseURL) == "" {
		return model
	}
	return "devflow/" + modelID(model)
}

func modelID(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		return strings.TrimSpace(model[idx+1:])
	}
	return model
}

func (r *ManagedOpenCodeRunner) resolveBinary() (string, error) {
	candidates := make([]string, 0)
	if strings.TrimSpace(r.BinaryPath) != "" {
		candidates = append(candidates, r.BinaryPath)
	}
	if envPath := strings.TrimSpace(os.Getenv("DEVFLOW_CODING_AGENT_PATH")); envPath != "" {
		candidates = append(candidates, envPath)
	}
	if envPath := strings.TrimSpace(os.Getenv("DEVFLOW_OPENCODE_PATH")); envPath != "" {
		candidates = append(candidates, envPath)
	}
	for _, root := range candidateRoots(r.StartRoot) {
		candidates = append(candidates,
			filepath.Join(root, "tools", "coding-agent", "vendor", "windows-x64", "coding-agent.exe"),
			filepath.Join(root, "tools", "opencode", "vendor", "windows-x64", "opencode.exe"),
			filepath.Join(root, "tools", "opencode", "windows-amd64", "opencode.exe"),
			filepath.Join(root, "tools", "opencode", "node_modules", ".bin", windowsOpenCodeBin()),
			filepath.Join(root, "tools", "opencode", "node_modules", ".bin", "opencode"),
			filepath.Join(root, "runtime", "tools", "opencode", "opencode.exe"),
			filepath.Join(root, "runtime", "tools", "opencode", "node_modules", ".bin", windowsOpenCodeBin()),
			filepath.Join(root, "runtime", "tools", "opencode", "node_modules", ".bin", "opencode"),
		)
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	for _, name := range []string{"opencode.exe", "opencode"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("coding agent executable not found; checked DEVFLOW_CODING_AGENT_PATH, project vendor binary, internal engine version %s fallback paths, and PATH", PinnedCodingAgentEngineVersion)
}

func windowsOpenCodeBin() string {
	if runtime.GOOS == "windows" {
		return "opencode.cmd"
	}
	return "opencode"
}

func candidateRoots(start string) []string {
	seen := make(map[string]bool)
	roots := make([]string, 0)
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return
		}
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	add(start)
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}
	for _, root := range append([]string(nil), roots...) {
		current := root
		for i := 0; i < 6; i++ {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			add(parent)
			current = parent
		}
	}
	return roots
}

func openCodeEnv(cfg core.LLMConfig) []string {
	env := os.Environ()
	if strings.TrimSpace(cfg.APIKey) != "" {
		env = append(env, "OPENAI_API_KEY="+cfg.APIKey)
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		env = append(env, "OPENAI_BASE_URL="+codingAgentBaseURL(cfg.BaseURL))
		if content := openCodeConfigContent(cfg); content != "" {
			env = append(env, "OPENCODE_CONFIG_CONTENT="+content)
		}
	}
	env = append(env, "OPENCODE_DISABLE_AUTOUPDATE=1")
	if runtime.GOOS == "windows" {
		env = append(env, "NO_COLOR=1")
	}
	return env
}

func openCodeConfigContent(cfg core.LLMConfig) string {
	baseURL := codingAgentBaseURL(cfg.BaseURL)
	if baseURL == "" {
		return ""
	}
	model := modelID(cfg.Model)
	if model == "" {
		return ""
	}
	content := map[string]any{
		"model": "devflow/" + model,
		"agent": map[string]any{
			"build": map[string]any{
				"tools": map[string]any{
					"skill": false,
					"glob":  false,
				},
			},
		},
		"provider": map[string]any{
			"devflow": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "DevFlow",
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  "{env:OPENAI_API_KEY}",
				},
				"models": map[string]any{
					model: map[string]any{},
				},
			},
		},
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(raw)
}

func codingAgentBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1"
		return parsed.String()
	}
	return baseURL
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
