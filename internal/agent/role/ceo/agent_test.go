package ceo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	ceospec "devflow/internal/agent/spec/ceo"
)

type testHandlerRegistry struct {
	handlers map[string]core.Handler
}

func (r testHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

type testHandler struct {
	name       string
	handleFunc func(context.Context, core.HandlerRequest) (core.HandlerResponse, error)
}

func (h testHandler) Name() string { return h.name }
func (h testHandler) Description() string { return h.name }
func (h testHandler) ToolSpec() core.ToolSpec { return core.ToolSpec{Name: h.name} }
func (h testHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if h.handleFunc == nil {
		return core.HandlerResponse{}, fmt.Errorf("unexpected handle for %s", h.name)
	}
	return h.handleFunc(ctx, req)
}

func TestCEOWritePlanUsesFallbackRequirementWhenInputMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	spec := ceospec.WritePlanSpec()
	agent := NewAgent()

	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "ceo", Op: "write_plan", OpID: "ceo.write_plan"},
		Bundle: core.AgentInputBundle{OutputDir: outputDir},
		OpSpec: spec,
		Handlers: testHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": testHandler{
				name: "artifact_write",
				handleFunc: func(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
					content, _ := req.Args["content"].(string)
					path := filepath.Join(req.Bundle.OutputDir, "requirement_v1.md")
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						return core.HandlerResponse{}, err
					}
					return core.HandlerResponse{
						Data: map[string]any{
							"object_type": "markdown",
							"status":      "produced",
							"path":        path,
							"artifact_uri": "projects/run/agents/ceo/artifacts/requirement/requirement_v1.md",
						},
					}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("Run() outputs len = %d, want 1", len(result.Outputs))
	}
	if result.Outputs[0].LogicalKey != core.LKRequirement {
		t.Fatalf("Run() logical_key = %q, want requirement", result.Outputs[0].LogicalKey)
	}
	body, err := os.ReadFile(filepath.Join(outputDir, "requirement_v1.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); !strings.Contains(got, "贪吃蛇") {
		t.Fatalf("requirement_v1.md = %q, want fallback requirement content", got)
	}
}

func TestCEOReviewPlanPassesThroughWithoutOutputs(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "ceo", Op: "review_plan", OpID: "ceo.review_plan"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("Run() outputs len = %d, want 0", len(result.Outputs))
	}
}
