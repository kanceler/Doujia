package handler

import (
	"context"
	"strings"
	"testing"

	"doujia/internal/agent/core"
)

func TestTaskCompleteAcceptsKok(t *testing.T) {
	t.Parallel()

	resp, err := NewTaskCompleteHandler().Handle(context.Background(), core.HandlerRequest{
		Args: map[string]any{
			"result":  "kok",
			"outputs": []any{},
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := resp.Data["result"]; got != "kok" {
		t.Fatalf("Handle() result = %v, want kok", got)
	}
}

func TestTaskCompleteRejectsLegacyOK(t *testing.T) {
	t.Parallel()

	_, err := NewTaskCompleteHandler().Handle(context.Background(), core.HandlerRequest{
		Args: map[string]any{
			"result":  "ok",
			"outputs": []any{},
		},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "kok or kfail") {
		t.Fatalf("Handle() error = %v, want kok/kfail rejection", err)
	}
}
