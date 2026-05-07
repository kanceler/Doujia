package llm

import "context"

type Adapter interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type StreamingAdapter interface {
	Adapter
	StreamChat(ctx context.Context, req ChatRequest, onDelta func(string) error) (ChatResponse, error)
}
