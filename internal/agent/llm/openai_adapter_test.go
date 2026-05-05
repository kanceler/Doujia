package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatCompletionsURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "openai v1 base",
			base: "https://api.openai.com/v1",
			want: "https://api.openai.com/v1/chat/completions",
		},
		{
			name: "ark v3 base",
			base: "https://ark.cn-beijing.volces.com/api/v3",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "full chat completions url",
			base: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "legacy custom host",
			base: "https://example.test/openai",
			want: "https://example.test/openai/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatCompletionsURL(tt.base); got != tt.want {
				t.Fatalf("chatCompletionsURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestOpenAIAdapterRetriesTransientStatus(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v3/chat/completions" {
			t.Fatalf("request path = %q, want /api/v3/chat/completions", got)
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "temporary upstream failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL:    server.URL + "/api/v3",
		APIKey:     "test-key",
		Model:      "test-model",
		RetryDelay: time.Nanosecond,
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("Chat() content = %q, want ok", resp.Message.Content)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("request calls = %d, want 2", got)
	}
}

func TestOpenAIAdapterTracesHTTPAttempts(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "temporary upstream failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	var traces []string
	adapter := &OpenAIAdapter{
		BaseURL:    server.URL + "/api/v3",
		APIKey:     "test-key",
		Model:      "test-model",
		RetryDelay: time.Nanosecond,
		Tracef: func(format string, args ...any) {
			traces = append(traces, fmt.Sprintf(format, args...))
		},
	}

	_, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	joined := strings.Join(traces, "\n")
	for _, want := range []string{"llm_http_start", "llm_http_end", "attempt=1", "attempt=2", "status=502", "status=200", "retry_count=1", "duration="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace output missing %q in:\n%s", want, joined)
		}
	}
}

func TestOpenAIAdapterRetriesRateLimitUsingRetryAfter(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"error":{"code":"RateLimitExceeded.EndpointTPMExceeded"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL:    server.URL + "/api/v3",
		APIKey:     "test-key",
		Model:      "test-model",
		RetryDelay: time.Hour,
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("Chat() content = %q, want ok", resp.Message.Content)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("request calls = %d, want 2", got)
	}
}

func TestOpenAIAdapterParsesSSEChatResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": PING\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[],"created":0,"id":"","model":"test-model","object":"chat.completion.chunk"}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"role":"assistant","content":"hello "}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"world"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL: server.URL + "/v1",
		APIKey:  "test-key",
		Model:   "test-model",
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Fatalf("Chat() role = %q, want assistant", resp.Message.Role)
	}
	if resp.Message.Content != "hello world" {
		t.Fatalf("Chat() content = %q, want hello world", resp.Message.Content)
	}
}

func TestOpenAIAdapterUsesResponsesAPIForText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/responses" {
			t.Fatalf("request path = %q, want /v1/responses", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Model:    "test-model",
		APIStyle: "responses",
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Role != "assistant" {
		t.Fatalf("Chat() role = %q, want assistant", resp.Message.Role)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("Chat() content = %q, want ok", resp.Message.Content)
	}
}

func TestOpenAIAdapterUsesResponsesAPIForToolCalls(t *testing.T) {
	t.Parallel()

	var sawTool bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/responses" {
			t.Fatalf("request path = %q, want /v1/responses", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v, want one tool", body["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok || tool["type"] != "function" || tool["name"] != "noop_tool" {
			t.Fatalf("tool = %#v, want Responses function tool", tools[0])
		}
		sawTool = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"function_call","call_id":"call_1","name":"noop_tool","arguments":"{\"ok\":true}"}]}`))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Model:    "test-model",
		APIStyle: "responses",
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
		Tools: []Tool{{
			Name:       "noop_tool",
			Parameters: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !sawTool {
		t.Fatal("server did not see Responses tool shape")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "noop_tool" {
		t.Fatalf("ToolCall = %+v, want call_1 noop_tool", resp.ToolCalls[0])
	}
	if got := resp.ToolCalls[0].Args["ok"]; got != true {
		t.Fatalf("ToolCall args ok = %#v, want true", got)
	}
}

func TestOpenAIAdapterResponsesMovesSystemMessagesToInstructions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["instructions"] != "System prompt." {
			t.Fatalf("instructions = %#v, want system prompt", body["instructions"])
		}
		input, ok := body["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("input = %#v, want one fallback input item", body["input"])
		}
		item, ok := input[0].(map[string]any)
		if !ok || item["role"] != "user" || item["content"] == "" {
			t.Fatalf("input[0] = %#v, want user fallback item", input[0])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Model:    "test-model",
		APIStyle: "responses",
	}
	resp, err := adapter.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "system", Content: "System prompt."}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("Chat() content = %q, want ok", resp.Message.Content)
	}
}

func TestNewOpenAIAdapterFromEnvParsesRetrySettings(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_MODEL", "test-model")
	t.Setenv("OPENAI_MAX_RETRIES", "7")
	t.Setenv("OPENAI_RETRY_DELAY", "2s")
	t.Setenv("OPENAI_RETRY_MAX_DELAY", "1m")
	t.Setenv("OPENAI_API_STYLE", "responses")

	adapter := NewOpenAIAdapterFromEnv()

	if adapter.APIStyle != "responses" {
		t.Fatalf("APIStyle = %q, want responses", adapter.APIStyle)
	}
	if adapter.MaxRetries != 7 {
		t.Fatalf("MaxRetries = %d, want 7", adapter.MaxRetries)
	}
	if adapter.RetryDelay != 2*time.Second {
		t.Fatalf("RetryDelay = %s, want 2s", adapter.RetryDelay)
	}
	if adapter.RetryMaxDelay != time.Minute {
		t.Fatalf("RetryMaxDelay = %s, want 1m", adapter.RetryMaxDelay)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
