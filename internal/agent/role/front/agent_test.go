package front

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"devflow/internal/agent/core"
	frontspec "devflow/internal/agent/spec/front"
)

type stubToolLoop struct {
	request core.ToolLoopRequest
	result  core.AgentResult
	err     error
	called  bool
}

func (s *stubToolLoop) Run(_ context.Context, req core.ToolLoopRequest) (core.AgentResult, error) {
	s.called = true
	s.request = req
	return s.result, s.err
}

func TestAgentRunsWriteCodeThroughToolLoop(t *testing.T) {
	t.Parallel()

	calls := []string{}
	loop := &stubToolLoop{
		result: core.AgentResult{Result: "kok"},
	}
	agent := NewAgent()
	req := core.AgentRunRequest{
		Task:   core.Task{Role: "front", Op: "write_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{OutputDir: t.TempDir()},
		OpSpec: core.OpSpec{Role: "front", Op: "write_code"},
		Prompt: "front prompt",
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"container_git_worktree_prepare": stubHandler{
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					calls = append(calls, "prepare")
					return core.HandlerResponse{Data: map[string]any{"result": "kok"}}, nil
				},
			},
		}},
		LLM:      struct{}{},
		ToolLoop: loop,
	}

	result, err := agent.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if !loop.called {
		t.Fatalf("ToolLoop.Run was not called")
	}
	if loop.request.Task.Role != "front" || loop.request.Task.Op != "write_code" {
		t.Fatalf("ToolLoop request task = %+v, want front/write_code", loop.request.Task)
	}
	calls = append(calls, "tool_loop")
	if len(calls) != 2 || calls[0] != "prepare" || calls[1] != "tool_loop" {
		t.Fatalf("Run() calls = %v, want [prepare tool_loop]", calls)
	}
}

func TestAgentRejectsUnsupportedOp(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "front", Op: "unknown", ExecutionMode: "normal"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || result.Errors[0].Code != "unsupported_front_op" {
		t.Fatalf("Run() errors = %+v, want unsupported_front_op", result.Errors)
	}
}

func TestAgentFailsWhenPrepareHandlerMissing(t *testing.T) {
	t.Parallel()

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:     core.Task{Role: "front", Op: "write_code", ExecutionMode: "normal"},
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{}},
		ToolLoop: &stubToolLoop{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || result.Errors[0].Code != "worktree_prepare_failed" {
		t.Fatalf("Run() errors = %+v, want worktree_prepare_failed", result.Errors)
	}
}

func TestAgentWritesFailureArtifactWhenToolLoopFails(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	spec := frontspec.WriteCodeSpec()
	loop := &stubToolLoop{
		err: fmt.Errorf("frontend build failed"),
	}
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "front", Op: "write_code", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{OutputDir: outputDir},
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"container_git_worktree_prepare": stubHandler{
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					return core.HandlerResponse{Data: map[string]any{"result": "kok"}}, nil
				},
			},
			"artifact_write": stubHandler{
				handleFunc: func(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
					logicalKey, _ := req.Args["logical_key"].(string)
					content, _ := req.Args["content"].(string)
					output, ok := req.OpSpec.FindOutput(logicalKey)
					if !ok {
						return core.HandlerResponse{}, fmt.Errorf("unknown output %q", logicalKey)
					}
					path := filepath.Join(req.Bundle.OutputDir, output.FileName)
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						return core.HandlerResponse{}, err
					}
					return core.HandlerResponse{
						Data: map[string]any{
							"object_type": output.ObjectType,
							"status":      "produced",
							"path":        path,
						},
					}, nil
				},
			},
		}},
		ToolLoop: loop,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKWriteCodeFailure {
		t.Fatalf("Run() outputs = %+v, want write_code_failure", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "write_code_failure.md"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if len(body) == 0 {
		t.Fatal("write_code_failure.md is empty")
	}
}

type stubHandlerRegistry struct {
	handlers map[string]core.Handler
}

func (r stubHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

type stubHandler struct {
	handleFunc func(context.Context, core.HandlerRequest) (core.HandlerResponse, error)
}

func (h stubHandler) Name() string { return "stub" }

func (h stubHandler) Description() string { return "stub" }

func (h stubHandler) ToolSpec() core.ToolSpec { return core.ToolSpec{Name: "stub"} }

func (h stubHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if h.handleFunc == nil {
		return core.HandlerResponse{}, fmt.Errorf("unexpected handle")
	}
	return h.handleFunc(ctx, req)
}
