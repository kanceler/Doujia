package llm

import "fmt"

func BuildClient(cfg Config) (Client, error) {
	switch cfg.ProviderType {
	case ProviderTypeNoop:
		return NoopClient{}, nil
	case ProviderTypeOpenAICompatible:
		return NewOpenAICompatibleClient(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported llm provider type %q", cfg.ProviderType)
	}
}
