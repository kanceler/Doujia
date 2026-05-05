package handler

import (
	"context"
	"strings"
	"testing"

	"devflow/internal/agent/core"
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
	if _, ok := resp.Data["outputs"]; !ok {
		t.Fatal("Handle() outputs missing, want empty outputs defaulted")
	}
}

func TestTaskCompleteAllowsMissingOutputs(t *testing.T) {
	t.Parallel()

	resp, err := NewTaskCompleteHandler().Handle(context.Background(), core.HandlerRequest{
		Args: map[string]any{
			"result": "kok",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if _, ok := resp.Data["outputs"]; !ok {
		t.Fatal("Handle() outputs missing, want empty outputs defaulted")
	}
}

func TestTaskCompleteAcceptsRecoverResultCodes(t *testing.T) {
	t.Parallel()

	for _, result := range []string{"kbug", "krewrite", "kreplan", "kcontrol_invalid"} {
		result := result
		t.Run(result, func(t *testing.T) {
			t.Parallel()

			resp, err := NewTaskCompleteHandler().Handle(context.Background(), core.HandlerRequest{
				Args: map[string]any{
					"result":  result,
					"outputs": []any{},
				},
			})
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if got := resp.Data["result"]; got != result {
				t.Fatalf("Handle() result = %v, want %s", got, result)
			}
		})
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
	if !strings.Contains(err.Error(), "one of kok, kfail, kbug, krewrite, kreplan, or kcontrol_invalid") {
		t.Fatalf("Handle() error = %v, want supported result-code rejection", err)
	}
}
