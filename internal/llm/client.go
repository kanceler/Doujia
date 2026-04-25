package llm

import (
	"context"
	"fmt"
)

type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

type NoopClient struct{}

func (NoopClient) Complete(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("llm client is not configured")
}

func IsNoop(client Client) bool {
	if client == nil {
		return true
	}
	switch client.(type) {
	case NoopClient, *NoopClient:
		return true
	default:
		return false
	}
}
