package tester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
	testerspec "doujia/internal/agent/spec/tester"
)

type stubHandlerRegistry struct {
	handlers map[string]core.Handler
}

func (r stubHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

type stubHandler struct {
	name       string
	handleFunc func(context.Context, core.HandlerRequest) (core.HandlerResponse, error)
}

func (h stubHandler) Name() string { return h.name }

func (h stubHandler) Description() string { return h.name }

func (h stubHandler) ToolSpec() core.ToolSpec { return core.ToolSpec{Name: h.name} }

func (h stubHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if h.handleFunc == nil {
		return core.HandlerResponse{}, fmt.Errorf("unexpected handle for %s", h.name)
	}
	return h.handleFunc(ctx, req)
}

func TestTesterTestCodeReturnsKbugForInvalidUpstreamCoderBranch(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	coderBranchPath := filepath.Join(inputDir, "coder_branch.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(coderBranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"","result":"kok","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":["tests/a_test.js"],"test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	containerRunCalled := false
	spec := testerspec.TestCodeSpec()
	writeHandler := stubHandler{
		name: "artifact_write",
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
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal", AgentID: "tester01"},
		Bundle: core.AgentInputBundle{
			InputDir:  inputDir,
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
				{LogicalKey: core.LKCoderBranch, Path: coderBranchPath},
				{LogicalKey: core.LKFullTestFiles, Path: fullTestFilesPath},
			},
		},
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": writeHandler,
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					containerRunCalled = true
					return core.HandlerResponse{}, nil
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kbug" {
		t.Fatalf("Run() result = %q, want kbug", result.Result)
	}
	if containerRunCalled {
		t.Fatal("Run() called container_run despite invalid upstream coder_branch")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("ReadFile(upstream_artifact_issue.md) error = %v", readErr)
	}
	got := string(body)
	if got == "" || !strings.Contains(got, "commit") || !strings.Contains(got, "tester.test_code") {
		t.Fatalf("upstream_artifact_issue.md = %q, want commit and tester.test_code details", got)
	}
}
