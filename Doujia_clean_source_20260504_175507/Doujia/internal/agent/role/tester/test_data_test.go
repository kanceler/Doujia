package tester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	testerspec "devflow/internal/agent/spec/tester"
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

func TestTesterTestDataReturnsKbugForMissingSeedTestsFiles(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	containerPath := filepath.Join(inputDir, "container_context.json")
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	testerTaskPath := filepath.Join(inputDir, "tester_task.md")
	moduleContractPath := filepath.Join(inputDir, "module_contract.json")
	seedTestsPath := filepath.Join(inputDir, "seed_tests.json")

	if err := os.WriteFile(containerPath, []byte(`{"container_id":"ctr-1","repo_dir":"/workspace/repo"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(container_context): %v", err)
	}
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(testerTaskPath, []byte("# Tester task"), 0o644); err != nil {
		t.Fatalf("WriteFile(tester_task): %v", err)
	}
	if err := os.WriteFile(moduleContractPath, []byte(`{"kind":"module_contract","module_id":"module01"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_contract): %v", err)
	}
	if err := os.WriteFile(seedTestsPath, []byte(`{"kind":"seed_tests","module_id":"module01","test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(seed_tests): %v", err)
	}

	spec := testerspec.TestDataSpec()
	spec.ExpectedOutputs = append(spec.ExpectedOutputs, core.OutputSpec{
		LogicalKey:  core.LKUpstreamArtifactIssue,
		ObjectType:  "markdown",
		FileName:    "upstream_artifact_issue.md",
		Description: "Upstream issue report.",
		Required:    false,
	})

	toolLoopCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "tester", Op: "test_data", ExecutionMode: "normal", AgentID: "tester01"},
		Bundle: core.AgentInputBundle{
			InputDir:  inputDir,
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKContainerContext, Path: containerPath},
				{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
				{LogicalKey: core.LKTesterTask, Path: testerTaskPath},
				{LogicalKey: core.LKModuleContract, Path: moduleContractPath},
				{LogicalKey: core.LKSeedTests, Path: seedTestsPath},
			},
		},
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubHandler{
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
			},
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
	if toolLoopCalled {
		t.Fatal("Run() called tool loop despite invalid seed_tests")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKUpstreamArtifactIssue {
		t.Fatalf("Run() outputs = %+v, want upstream_artifact_issue", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "upstream_artifact_issue.md"))
	if readErr != nil {
		t.Fatalf("ReadFile(upstream_artifact_issue.md) error = %v", readErr)
	}
	if got := string(body); !strings.Contains(got, "seed_tests") || !strings.Contains(got, "tester.test_data") {
		t.Fatalf("upstream_artifact_issue.md = %q, want seed_tests and tester.test_data details", got)
	}
}
