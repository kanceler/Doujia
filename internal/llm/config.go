package llm

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.openai.com/v1"
const DefaultRequestTimeout = 60 * time.Second

type ProviderType string

const (
	ProviderTypeOpenAICompatible ProviderType = "openai_compatible"
	ProviderTypeNoop             ProviderType = "noop"
)

type Config struct {
	ProviderType   ProviderType
	BaseURL        string
	APIKey         string
	Model          string
	RequestTimeout time.Duration
}

func LoadConfigFromEnv() (Config, error) {
	timeout, err := loadRequestTimeoutFromEnv()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ProviderType:   ProviderType(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))),
		BaseURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("LLM_BASE_URL")), "/"),
		APIKey:         strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		Model:          strings.TrimSpace(os.Getenv("LLM_MODEL")),
		RequestTimeout: timeout,
	}
	if cfg.ProviderType == "" {
		cfg.ProviderType = ProviderTypeOpenAICompatible
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}

	switch cfg.ProviderType {
	case ProviderTypeNoop:
		return cfg, nil
	case ProviderTypeOpenAICompatible:
		if cfg.APIKey == "" {
			return Config{}, fmt.Errorf("LLM_API_KEY is required")
		}
		if cfg.Model == "" {
			return Config{}, fmt.Errorf("LLM_MODEL is required")
		}
		return cfg, nil
	default:
		return Config{}, fmt.Errorf("unsupported LLM_PROVIDER %q", cfg.ProviderType)
	}
}

func LoadOptionalConfigFromEnv() (Config, bool, error) {
	provider := strings.TrimSpace(os.Getenv("LLM_PROVIDER"))
	hasAPIKey := strings.TrimSpace(os.Getenv("LLM_API_KEY")) != ""
	hasModel := strings.TrimSpace(os.Getenv("LLM_MODEL")) != ""
	if provider == "" && !hasAPIKey && !hasModel {
		return Config{}, false, nil
	}
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func loadRequestTimeoutFromEnv() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("LLM_TIMEOUT_SECONDS"))
	if raw == "" {
		return DefaultRequestTimeout, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid LLM_TIMEOUT_SECONDS %q", raw)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("LLM_TIMEOUT_SECONDS must be zero or greater")
	}
	if seconds == 0 {
		return 0, nil
	}
	return time.Duration(seconds) * time.Second, nil
}
