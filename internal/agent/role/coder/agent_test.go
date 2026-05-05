package coder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	coderspec "devflow/internal/agent/spec/coder"
)

type stubToolLoop struct {
	runFunc func(context.Context, core.ToolLoopRequest) (core.AgentResult, error)
}

func (s stubToolLoop) Run(ctx context.Context, req core.ToolLoopRequest) (core.AgentResult, error) {
	if s.runFunc == nil {
		return core.AgentResult{}, fmt.Errorf("unexpected tool loop call")
	}
	return s.runFunc(ctx, req)
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

func TestCoderWriteCodePreparesWorktreeBeforeToolLoop(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	bundle := writeCodeTestBundle(t, inputDir, outputDir)
	calls := []string{}
	prepareHandler := stubHandler{
		handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
			calls = append(calls, "prepare")
			return core.HandlerResponse{Data: map[string]any{"result": "kok"}}, nil
		},
	}
	toolLoop := stubToolLoop{
		runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			calls = append(calls, "tool_loop")
			return core.AgentResult{Result: "kok"}, nil
		},
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:     core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		Bundle:   bundle,
		OpSpec:   coderspec.WriteCodeSpec(),
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{"container_git_worktree_prepare": prepareHandler}},
		ToolLoop: toolLoop,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(calls) != 2 || calls[0] != "prepare" || calls[1] != "tool_loop" {
		t.Fatalf("Run() calls = %v, want [prepare tool_loop]", calls)
	}
}

func TestCoderWriteCodeFailsWhenPrepareHandlerMissing(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:     core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		Bundle:   writeCodeTestBundle(t, inputDir, outputDir),
		OpSpec:   coderspec.WriteCodeSpec(),
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{}},
		ToolLoop: stubToolLoop{},
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

func TestCoderWriteCodeStopsWhenPrepareFails(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	toolLoopCalled := false
	prepareHandler := stubHandler{
		handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
			return core.HandlerResponse{}, fmt.Errorf("prepare failed")
		},
	}
	toolLoop := stubToolLoop{
		runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			toolLoopCalled = true
			return core.AgentResult{Result: "kok"}, nil
		},
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:     core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		Bundle:   writeCodeTestBundle(t, inputDir, outputDir),
		OpSpec:   coderspec.WriteCodeSpec(),
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{"container_git_worktree_prepare": prepareHandler}},
		ToolLoop: toolLoop,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if toolLoopCalled {
		t.Fatal("Run() called tool loop after prepare failure")
	}
}

func TestCoderWriteCodeWritesFailureArtifactWhenToolLoopFails(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	spec := coderspec.WriteCodeSpec()
	prepareHandler := stubHandler{
		handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
			return core.HandlerResponse{Data: map[string]any{"result": "kok"}}, nil
		},
	}
	writeHandler := stubHandler{
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
					"logical_key": logicalKey,
					"object_type": output.ObjectType,
					"status":      "produced",
					"path":        path,
				},
			}, nil
		},
	}
	toolLoop := stubToolLoop{
		runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			return core.AgentResult{}, fmt.Errorf("changed_files hits forbidden_paths: src/backend/api.js")
		},
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		Bundle: writeCodeTestBundle(t, inputDir, outputDir),
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"container_git_worktree_prepare": prepareHandler,
			"artifact_write":                 writeHandler,
		}},
		ToolLoop: toolLoop,
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
	if got := string(body); got == "" {
		t.Fatal("write_code_failure.md is empty")
	}
}

func TestCoderWriteCodeReturnsKbugForInvalidUpstreamModuleSpecBeforePrepare(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	containerPath := filepath.Join(inputDir, "container_context.json")
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(container_context): %v", err)
	}
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","branch_name":"","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}

	prepareCalled := false
	toolLoopCalled := false
	spec := coderspec.WriteCodeSpec()
	writeHandler := stubHandler{
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
					"logical_key": logicalKey,
					"object_type": output.ObjectType,
					"status":      "produced",
					"path":        path,
				},
			}, nil
		},
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal", AgentID: "coder01"},
		Bundle: core.AgentInputBundle{InputDir: inputDir, OutputDir: outputDir, Inputs: []core.InputArtifact{{LogicalKey: core.LKContainerContext, Path: containerPath}, {LogicalKey: core.LKModuleSpec, Path: moduleSpecPath}}},
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"container_git_worktree_prepare": stubHandler{handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
				prepareCalled = true
				return core.HandlerResponse{}, nil
			}},
			"artifact_write": writeHandler,
		}},
		ToolLoop: stubToolLoop{runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			toolLoopCalled = true
			return core.AgentResult{Result: "kok"}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if prepareCalled {
		t.Fatal("Run() called prepare despite invalid upstream module_spec")
	}
	if toolLoopCalled {
		t.Fatal("Run() called tool loop despite invalid upstream module_spec")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("ReadFile(upstream_artifact_issue.md) error = %v", readErr)
	}
	if got := string(body); got == "" || !containsAll(got, "branch_name", "coder.write_code") {
		t.Fatalf("upstream_artifact_issue.md = %q, want branch_name and coder.write_code details", got)
	}
}

func writeCodeTestBundle(t *testing.T, inputDir, outputDir string) core.AgentInputBundle {
	t.Helper()

	containerPath := filepath.Join(inputDir, "container_context.json")
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","base_branch":"main"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(container_context): %v", err)
	}
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","branch_name":"feature/module01","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}

	return core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKContainerContext, Path: containerPath},
			{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
		},
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
