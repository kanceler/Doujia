package pm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	"devflow/internal/agent/handler"
	"devflow/internal/agent/llm"
	pmspec "devflow/internal/agent/spec/pm"
)

type reviewTestHandlerRegistry struct {
	handler core.Handler
}

func (r reviewTestHandlerRegistry) Get(name string) (core.Handler, bool) {
	if r.handler == nil || r.handler.Name() != name {
		return nil, false
	}
	if scoped, ok := r.handler.(core.ScopedHandler); ok {
		return scoped.WithScope(core.AgentInputBundle{}), true
	}
	return r.handler, true
}

type stubReviewLLM struct {
	content string
}

func (s stubReviewLLM) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: s.content,
		},
	}, nil
}

func TestPMAgentReviewPlanReturnsKokWithoutOutputsOnPass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	architecturePath := filepath.Join(dir, "architecture_v1.md")
	environmentPath := filepath.Join(dir, "environment_spec.json")
	if err := os.WriteFile(architecturePath, []byte(`# Architecture Plan

## Project Positioning
This project turns a PM plan into a deliverable web application with clear page and module boundaries.

## Technology Direction
Use Node for the backend runtime and keep the implementation lightweight.

## Runtime
The code runs in one repository and supports local development plus container execution.
`), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}
	if err := os.WriteFile(environmentPath, []byte(`{
  "runtime": "node",
  "image": "node:20-bookworm",
  "package_manager": "npm",
  "system_packages": ["git", "curl", "ca-certificates"],
  "check_commands": ["node --version", "npm --version", "git --version"],
  "repo_init_files": {
    "README.md": "# Project\n"
  },
  "setup_commands": [],
  "default_test_command": "npm test"
}`), 0o644); err != nil {
		t.Fatalf("write environment spec: %v", err)
	}

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "pm", Op: "review_plan", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir: dir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKArchitecturePlan, Path: architecturePath},
				{LogicalKey: core.LKEnvironmentSpec, Path: environmentPath},
			},
		},
		OpSpec: pmspec.ReviewPlanSpec(),
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

func TestPMAgentReviewPlanWritesFailureReportOnFail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	architecturePath := filepath.Join(dir, "architecture_v1.md")
	environmentPath := filepath.Join(dir, "environment_spec.json")
	if err := os.WriteFile(architecturePath, []byte("bad"), 0o644); err != nil {
		t.Fatalf("write architecture plan: %v", err)
	}
	if err := os.WriteFile(environmentPath, []byte(`{"runtime":"node"}`), 0o644); err != nil {
		t.Fatalf("write environment spec: %v", err)
	}

	bundle := core.AgentInputBundle{
		OutputDir: dir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKArchitecturePlan, Path: architecturePath},
			{LogicalKey: core.LKEnvironmentSpec, Path: environmentPath},
		},
	}
	writeHandler := handler.NewArtifactWriteHandler().WithScope(bundle)

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "pm", Op: "review_plan", ExecutionMode: "normal"},
		Bundle: bundle,
		OpSpec: pmspec.ReviewPlanSpec(),
		Handlers: reviewTestHandlerRegistry{
			handler: writeHandler,
		},
		LLM: stubReviewLLM{
			content: "# Review Failed\n\n- architecture_plan is too short.\n- environment_spec is missing required fields.\n",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("Run() outputs len = %d, want 1", len(result.Outputs))
	}
	if result.Outputs[0].LogicalKey != core.LKReviewPlanFailure {
		t.Fatalf("Run() output logical_key = %q, want %q", result.Outputs[0].LogicalKey, core.LKReviewPlanFailure)
	}
	body, err := os.ReadFile(result.Outputs[0].Path)
	if err != nil {
		t.Fatalf("read failure report: %v", err)
	}
	if !strings.Contains(string(body), "Review Failed") {
		t.Fatalf("failure report = %q, want LLM review content", string(body))
	}
}
