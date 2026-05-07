package tester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	"devflow/internal/agent/executor"
	testerspec "devflow/internal/agent/spec/tester"
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

func writeTestCodeArtifactHandler(t *testing.T) core.Handler {
	t.Helper()
	return stubHandler{
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
					"object_type":  output.ObjectType,
					"status":       "produced",
					"path":         path,
					"artifact_uri": "projects/run/agents/tester01/artifacts/test_code/" + output.FileName,
				},
			}, nil
		},
	}
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
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"tests/a_test.js":"test('a', () => {});\n"},"test_command":"npm test"}`), 0o644); err != nil {
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
					"object_type":  output.ObjectType,
					"status":       "produced",
					"path":         path,
					"artifact_uri": "projects/run/agents/tester01/artifacts/test_code/" + output.FileName,
				},
			}, nil
		},
	}
	bundle := core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
			{LogicalKey: core.LKCoderBranch, Path: coderBranchPath},
			{LogicalKey: core.LKFullTestFiles, Path: fullTestFilesPath},
		},
	}

	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal", AgentID: "tester01"},
		Bundle: bundle,
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
	result.ProducedBags = spec.ResolveProducedBags(core.Task{Role: "tester", Op: "test_code"}, bundle, result)
	if len(result.ProducedBags) != 1 || result.ProducedBags[0].Name != "failure_report" {
		t.Fatalf("ResolveProducedBags() = %+v, want failure_report", result.ProducedBags)
	}
	if err := executor.ValidateOutputs(core.Task{Role: "tester", Op: "test_code"}, bundle, spec, result); err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
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

func TestTesterTestCodeAcceptsLegacySuccessCoderBranchResult(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	coderBranchPath := filepath.Join(inputDir, "coder_branch.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(coderBranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","worktree":"/workspace/worktrees/module01","result":"success","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"tests/a_test.js":"test('a', () => {});\n"},"test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	containerRunCalled := false
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
		OpSpec: testerspec.TestCodeSpec(),
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": writeTestCodeArtifactHandler(t),
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					containerRunCalled = true
					return core.HandlerResponse{Data: map[string]any{
						"exit_code":   0,
						"stdout":      "ok\n",
						"stderr":      "",
						"duration_ms": int64(12),
					}}, nil
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
	if !containerRunCalled {
		t.Fatal("Run() did not call container_run")
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKModuleTestReport {
		t.Fatalf("Run() outputs = %+v, want module_test_report", result.Outputs)
	}
}

func TestTesterTestCodeReturnsKbugForFailedModuleTests(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	coderBranchPath := filepath.Join(inputDir, "coder_branch.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(coderBranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"abc123","worktree":"/workspace/worktrees/module01","result":"kok","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"tests/a_test.js":"test('a', () => {});\n"},"test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	spec := testerspec.TestCodeSpec()
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
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":   1,
							"stdout":      "running module tests",
							"stderr":      "assertion failed",
							"duration_ms": int64(12),
						},
					}, nil
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
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != core.LKModuleTestReport {
		t.Fatalf("Run() outputs = %+v, want module_test_report", result.Outputs)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "module_test_report.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(module_test_report.json) error = %v", readErr)
	}
	report := string(body)
	for _, want := range []string{`"result": "kbug"`, `"test_passed": false`, "assertion failed"} {
		if !strings.Contains(report, want) {
			t.Fatalf("module_test_report.json = %q, want %q", report, want)
		}
	}
}

func TestTesterTestCodePrefersLatestDuplicateCoderBranch(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	oldCoderBranchPath := filepath.Join(inputDir, "coder_branch_old.json")
	newCoderBranchPath := filepath.Join(inputDir, "coder_branch_new.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(oldCoderBranchPath, []byte(`{"module_id":"module01","branch":"feature/module01","commit":"","worktree":"/workspace/worktrees/module01","result":"kok","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch_old): %v", err)
	}
	if err := os.WriteFile(newCoderBranchPath, []byte(`{"module_id":"module01","branch":"feature/module01-debug","commit":"fixed123","worktree":"/workspace/worktrees/module01","result":"kok","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch_new): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"tests/a_test.js":"test('a', () => {});\n"},"test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	spec := testerspec.TestCodeSpec()
	containerRunCalled := false
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task: core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal", AgentID: "tester01"},
		Bundle: core.AgentInputBundle{
			InputDir:  inputDir,
			OutputDir: outputDir,
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
				{LogicalKey: core.LKCoderBranch, Path: oldCoderBranchPath},
				{LogicalKey: core.LKFullTestFiles, Path: fullTestFilesPath},
				{LogicalKey: core.LKCoderBranch, Path: newCoderBranchPath},
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
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					containerRunCalled = true
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":   0,
							"stdout":      "ok",
							"stderr":      "",
							"duration_ms": int64(5),
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
	if !containerRunCalled {
		t.Fatal("Run() did not call container_run; likely read stale coder_branch")
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "module_test_report.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(module_test_report.json) error = %v", readErr)
	}
	if !strings.Contains(string(body), "fixed123") {
		t.Fatalf("module_test_report.json = %q, want latest coder_branch commit", string(body))
	}
}

func TestTesterTestCodePrefersCoderBranchTestCommandOverStaleFullTestFiles(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	coderBranchPath := filepath.Join(inputDir, "coder_branch.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module02","module_name":"backend","implementation_role":"coder","worktree_dir":"/workspace/worktrees/module02","owned_paths":["server/**"],"test_command":"npm run build","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(coderBranchPath, []byte(`{"module_id":"module02","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","result":"kok","test_command":"cd server && npm run build","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"server/tests/module02/smoke.test.js":"test('a', () => {});\n"},"test_command":"npm run build"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	spec := testerspec.TestCodeSpec()
	var gotCommand string
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
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
					gotCommand, _ = req.Args["command"].(string)
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":   0,
							"stdout":      "ok",
							"stderr":      "",
							"duration_ms": int64(10),
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
	if gotCommand != "cd server && npm run build" {
		t.Fatalf("container_run command = %q, want repaired coder_branch command", gotCommand)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "module_test_report.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(module_test_report.json) error = %v", readErr)
	}
	if !strings.Contains(string(body), `"test_command": "cd server \u0026\u0026 npm run build"`) {
		t.Fatalf("module_test_report.json = %q, want repaired coder_branch command", string(body))
	}
}

func TestTesterTestCodeNormalizesBackendBareNPMTestCommand(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	moduleSpecPath := filepath.Join(inputDir, "module_spec.json")
	coderBranchPath := filepath.Join(inputDir, "coder_branch.json")
	fullTestFilesPath := filepath.Join(inputDir, "full_test_files.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module02","module_name":"backend","module_role":"backend","implementation_role":"coder","worktree_dir":"/workspace/worktrees/module02","owned_paths":["server/**"],"runtime_write_paths":["server/package.json","server/tests/module02/**"],"test_command":"npm test","complexity":"high"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(module_spec): %v", err)
	}
	if err := os.WriteFile(coderBranchPath, []byte(`{"module_id":"module02","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","result":"kok","test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile(coder_branch): %v", err)
	}
	if err := os.WriteFile(fullTestFilesPath, []byte(`{"kind":"full_test_files","files":{"server/tests/module02/smoke.test.js":"test('a', () => {});\n"},"test_command":"npm test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(full_test_files): %v", err)
	}

	spec := testerspec.TestCodeSpec()
	var gotCommand string
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
			"artifact_write": writeTestCodeArtifactHandler(t),
			"container_run": stubHandler{
				name: "container_run",
				handleFunc: func(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
					gotCommand, _ = req.Args["command"].(string)
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":   0,
							"stdout":      "ok",
							"stderr":      "",
							"duration_ms": int64(10),
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
	if gotCommand != "cd server && npm test" {
		t.Fatalf("container_run command = %q, want backend server command", gotCommand)
	}
	body, readErr := os.ReadFile(filepath.Join(outputDir, "module_test_report.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(module_test_report.json) error = %v", readErr)
	}
	if !strings.Contains(string(body), `"test_command": "cd server \u0026\u0026 npm test"`) {
		t.Fatalf("module_test_report.json = %q, want normalized backend command", string(body))
	}
}
