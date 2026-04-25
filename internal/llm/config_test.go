package llm

import "testing"

func TestLoadConfigFromEnvRequiresAPIKeyAndModel(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatalf("LoadConfigFromEnv() error = nil, want non-nil")
	}
}

func TestLoadConfigFromEnvLoadsOpenAICompatibleConfig(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai_compatible")
	t.Setenv("LLM_BASE_URL", "https://example.com/v1/")
	t.Setenv("LLM_API_KEY", "secret")
	t.Setenv("LLM_MODEL", "gpt-test")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://example.com/v1")
	}
	if cfg.ProviderType != ProviderTypeOpenAICompatible {
		t.Fatalf("ProviderType = %q, want %q", cfg.ProviderType, ProviderTypeOpenAICompatible)
	}
	if cfg.APIKey != "secret" {
		t.Fatalf("APIKey = %q, want %q", cfg.APIKey, "secret")
	}
	if cfg.Model != "gpt-test" {
		t.Fatalf("Model = %q, want %q", cfg.Model, "gpt-test")
	}
}

func TestLoadConfigFromEnvLoadsNoopProvider(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "noop")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.ProviderType != ProviderTypeNoop {
		t.Fatalf("ProviderType = %q, want %q", cfg.ProviderType, ProviderTypeNoop)
	}
}
