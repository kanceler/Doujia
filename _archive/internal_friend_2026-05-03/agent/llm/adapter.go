package llm

import "context"

type Adapter interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
