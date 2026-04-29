package common

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"devflow/internal/core"
)

func TestOpenCodeArgsOmitsPromptForFakeOpenCode(t *testing.T) {
	t.Setenv("DEVFLOW_FAKE_OPENCODE", "1")
	prompt := strings.Repeat("long prompt ", 5000)

	args, err := openCodeArgs(OpenCodeRequest{WorkDir: t.TempDir(), Prompt: prompt, Model: "gpt-5.5"})
	if err != nil {
		t.Fatalf("openCodeArgs returned error: %v", err)
	}

	for _, arg := range args {
		if strings.Contains(arg, "long prompt") {
			t.Fatalf("fake opencode args included prompt; args length=%d", len(strings.Join(args, " ")))
		}
	}
	if len(strings.Join(args, " ")) > 1000 {
		t.Fatalf("fake opencode args are unexpectedly long: %d", len(strings.Join(args, " ")))
	}
}

func TestOpenCodeArgsWritesPromptToFileInsteadOfCommandLine(t *testing.T) {
	t.Setenv("DEVFLOW_FAKE_OPENCODE", "")

	workDir := t.TempDir()
	prompt := strings.Repeat("very long prompt ", 5000)

	args, err := openCodeArgs(OpenCodeRequest{
		WorkDir: workDir,
		Prompt:  prompt,
		Model:   "gpt-5.5",
	})
	if err != nil {
		t.Fatalf("openCodeArgs returned error: %v", err)
	}

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "very long prompt") {
		t.Fatalf("command line still contains raw prompt; chars=%d", len(joined))
	}
	if len(joined) > 30000 {
		t.Fatalf("command line too long: %d", len(joined))
	}

	promptDir := filepath.Join(workDir, ".devflow", "opencode", "prompts")
	entries, err := os.ReadDir(promptDir)
	if err != nil {
		t.Fatalf("ReadDir prompt dir returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("prompt file count = %d, want 1", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(promptDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile prompt returned error: %v", err)
	}
	if string(content) != prompt {
		t.Fatalf("prompt file content mismatch")
	}
}

func TestResolveBinaryFindsVendorCodingAgentUnderTools(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "tools", "coding-agent", "vendor", "windows-x64", "coding-agent.exe")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	runner := NewManagedOpenCodeRunner(root)
	got, err := runner.resolveBinary()
	if err != nil {
		t.Fatalf("resolveBinary returned error: %v", err)
	}
	if got != binaryPath {
		t.Fatalf("resolveBinary = %q, want %q", got, binaryPath)
	}
}

func TestResolveBinaryPrefersExplicitEnvPath(t *testing.T) {
	root := t.TempDir()
	envBinary := filepath.Join(root, "custom", "coding-agent.exe")
	if err := os.MkdirAll(filepath.Dir(envBinary), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(envBinary, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("DEVFLOW_CODING_AGENT_PATH", envBinary)

	runner := NewManagedOpenCodeRunner(root)
	got, err := runner.resolveBinary()
	if err != nil {
		t.Fatalf("resolveBinary returned error: %v", err)
	}
	if got != envBinary {
		t.Fatalf("resolveBinary = %q, want env binary %q", got, envBinary)
	}
}

func TestOpenCodeEnvIncludesCompatibleBaseURLAndConfigContent(t *testing.T) {
	cfg := core.LLMConfig{
		APIKey:  "test-key",
		BaseURL: "https://example.test",
		Model:   "gpt-5.5",
	}

	workDir := t.TempDir()
	env, err := openCodeEnv(OpenCodeRequest{WorkDir: workDir, LLM: cfg})
	if err != nil {
		t.Fatalf("openCodeEnv returned error: %v", err)
	}
	joined := strings.Join(env, "\n")

	if !strings.Contains(joined, "OPENAI_API_KEY=test-key") {
		t.Fatalf("env missing OPENAI_API_KEY:\n%s", joined)
	}
	if !strings.Contains(joined, "OPENAI_BASE_URL=https://example.test/v1") {
		t.Fatalf("env missing normalized OPENAI_BASE_URL:\n%s", joined)
	}
	if !strings.Contains(joined, "OPENCODE_CONFIG_CONTENT=") {
		t.Fatalf("env missing OPENCODE_CONFIG_CONTENT:\n%s", joined)
	}
	if !strings.Contains(joined, `"model":"devflow/gpt-5.5"`) {
		t.Fatalf("env missing devflow model mapping:\n%s", joined)
	}
	if runtime.GOOS == "windows" && !strings.Contains(joined, "NO_COLOR=1") {
		t.Fatalf("env missing NO_COLOR on windows:\n%s", joined)
	}
	wantDataHome := "XDG_DATA_HOME=" + filepath.Join(workDir, ".devflow", "opencode", "data")
	if !strings.Contains(joined, wantDataHome) {
		t.Fatalf("env missing isolated XDG_DATA_HOME %q:\n%s", wantDataHome, joined)
	}
}

func TestCodingAgentBaseURLAppendsV1OnlyForHostRoot(t *testing.T) {
	if got := codingAgentBaseURL("https://example.test"); got != "https://example.test/v1" {
		t.Fatalf("codingAgentBaseURL root = %q, want %q", got, "https://example.test/v1")
	}
	if got := codingAgentBaseURL("https://example.test/custom"); got != "https://example.test/custom" {
		t.Fatalf("codingAgentBaseURL custom path = %q, want unchanged", got)
	}
}
