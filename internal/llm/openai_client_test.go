package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleClientRetriesTransientStatus(t *testing.T) {
	originalDelay := chatCompletionRetryDelay
	chatCompletionRetryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { chatCompletionRetryDelay = originalDelay })

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary upstream error", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleClient(Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})
	got, err := client.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("Complete = %q, want ok", got)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestOpenAICompatibleClientDoesNotRetryQuotaExhausted(t *testing.T) {
	originalDelay := chatCompletionRetryDelay
	chatCompletionRetryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { chatCompletionRetryDelay = originalDelay })

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"API_KEY_QUOTA_EXHAUSTED","message":"quota exhausted"}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleClient(Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})
	_, err := client.Complete(context.Background(), "hello")
	if err == nil {
		t.Fatal("Complete error = nil, want quota error")
	}
	if !strings.Contains(err.Error(), "API_KEY_QUOTA_EXHAUSTED") {
		t.Fatalf("Complete error = %v, want quota exhausted", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
