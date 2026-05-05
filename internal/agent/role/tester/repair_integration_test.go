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

type repairTestAgentRegistry struct {
	agent core.Agent
}

func (r repairTestAgentRegistry) Get(role string) (core.Agent, bool) {
	if r.agent == nil || r.agent.Role() != role {
		return nil, false
	}
	return r.agent, true
}

type repairTestOpRegistry struct {
	spec core.OpSpec
}

func (r repairTestOpRegistry) Get(role, op string) (core.OpSpec, bool) {
	if r.spec.Role != role || r.spec.Op != op {
		return core.OpSpec{}, false
	}
	return r.spec, true
}

func (r repairTestOpRegistry) GetByID(opID string) (core.OpSpec, bool) {
	if r.spec.Role+"."+r.spec.Op != opID {
		return core.OpSpec{}, false
	}
	return r.spec, true
}

type recordingTesterAgent struct {
	called bool
}

func (a *recordingTesterAgent) Role() string {
	return "tester"
}

func (a *recordingTesterAgent) Run(context.Context, core.AgentRunRequest) (core.AgentResult, error) {
	a.called = true
	return core.AgentResult{Result: "kok"}, nil
}

func TestTesterTestCodeRepairRequiresRepairInstruction(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	bundle := writeTestCodeRepairBundle(t, inputDir, outputDir, false, false)
	agent := &recordingTesterAgent{}

	result, err := executor.NewRuntime(
		repairTestAgentRegistry{agent: agent},
		repairTestOpRegistry{spec: testerspec.TestCodeSpec()},
		stubHandlerRegistry{handlers: map[string]core.Handler{}},
	).RunAgent(context.Background(), core.Task{
		Role:          "tester",
		Op:            "test_code",
		ExecutionMode: core.ExecutionModeRepair,
	}, bundle)
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("RunAgent() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, core.LKRepairInstruction) {
		t.Fatalf("RunAgent() errors = %+v, want missing repair_instruction", result.Errors)
	}
	if agent.called {
		t.Fatal("RunAgent() called tester agent despite missing repair_instruction")
	}
}

func TestTesterTestCodeRepairRerunsDeterministicFlowAndProducesFreshReport(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	bundle := writeTestCodeRepairBundle(t, inputDir, outputDir, true, true)
	spec := testerspec.TestCodeSpec()

	containerRunCalls := 0
	containerWriteCalled := false
	artifactWriteCalls := 0
	result, err := NewAgent().Run(context.Background(), core.AgentRunRequest{
		Task:   core.Task{Role: "tester", Op: "test_code", ExecutionMode: core.ExecutionModeRepair, AgentID: "tester01"},
		Bundle: bundle,
		OpSpec: spec,
		Handlers: stubHandlerRegistry{handlers: map[string]core.Handler{
			"artifact_write": stubHandler{
				name: "artifact_write",
				handleFunc: func(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
					artifactWriteCalls++
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
					containerRunCalls++
					command, _ := req.Args["command"].(string)
					if command != "npm test --repair" {
						return core.HandlerResponse{}, fmt.Errorf("unexpected command %q", command)
					}
					return core.HandlerResponse{
						Data: map[string]any{
							"exit_code":    0,
							"stdout":       "repair rerun ok",
							"stderr":       "",
							"duration_ms":  int64(42),
							"container_id": "ctr-1",
						},
					}, nil
				},
			},
			"container_write": stubHandler{
				name: "container_write",
				handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
					containerWriteCalled = true
					return core.HandlerResponse{}, fmt.Errorf("container_write should not be called")
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
	if containerRunCalls != 1 {
		t.Fatalf("Run() container_run calls = %d, want 1", containerRunCalls)
	}
	if artifactWriteCalls != 1 {
		t.Fatalf("Run() artifact_write calls = %d, want 1", artifactWriteCalls)
	}
	if containerWriteCalled {
		t.Fatal("Run() called container_write, want deterministic test execution without coder code edits")
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("Run() outputs len = %d, want 1", len(result.Outputs))
	}
	if result.Outputs[0].LogicalKey != core.LKModuleTestReport {
		t.Fatalf("Run() output logical_key = %q, want module_test_report", result.Outputs[0].LogicalKey)
	}
	if result.Outputs[0].Status != "produced" {
		t.Fatalf("Run() output status = %q, want produced", result.Outputs[0].Status)
	}

	reportBody, readErr := os.ReadFile(filepath.Join(outputDir, "module_test_report.json"))
	if readErr != nil {
		t.Fatalf("ReadFile(module_test_report.json) error = %v", readErr)
	}
	report := string(reportBody)
	for _, want := range []string{"repair rerun ok", `"tested_commit": "abc123"`, `"test_command": "npm test --repair"`} {
		if !strings.Contains(report, want) {
			t.Fatalf("module_test_report.json = %q, want %q", report, want)
		}
	}
	if strings.Contains(report, "stale previous failure") {
		t.Fatalf("module_test_report.json = %q, want fresh report instead of previous_outputs reuse", report)
	}
}

func TestTesterTestCodeRepairAllowsReusedOutputWithoutPathWhenArtifactVersionIDExists(t *testing.T) {
	t.Parallel()

	err := executor.ValidateOutputs(
		core.Task{Role: "tester", Op: "test_code", ExecutionMode: core.ExecutionModeRepair},
		core.AgentInputBundle{OutputDir: t.TempDir()},
		testerspec.TestCodeSpec(),
		core.AgentResult{
			Result: "kok",
			Outputs: []core.AgentOutput{
				{
					LogicalKey:        core.LKModuleTestReport,
					ObjectType:        "json",
					Status:            "reused",
					ArtifactVersionID: "av_xxx",
					ArtifactURI:       "projects/run_x/agents/tester01/artifacts/test_reports/module_test_report.json",
				},
			},
			ProducedBags: []core.ProducedBagManifest{
				{
					Name: "tested_module",
					Members: []core.ProducedBagMember{
						{LogicalKey: core.LKModuleTestReport, ArtifactVersionID: "av_xxx"},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestTesterTestCodeRepairRejectsReusedOutputWithoutArtifactVersionID(t *testing.T) {
	t.Parallel()

	err := executor.ValidateOutputs(
		core.Task{Role: "tester", Op: "test_code", ExecutionMode: core.ExecutionModeRepair},
		core.AgentInputBundle{OutputDir: t.TempDir()},
		testerspec.TestCodeSpec(),
		core.AgentResult{
			Result: "kok",
			Outputs: []core.AgentOutput{
				{
					LogicalKey:  core.LKModuleTestReport,
					ObjectType:  "json",
					Status:      "reused",
					ArtifactURI: "projects/run_x/agents/tester01/artifacts/test_reports/module_test_report.json",
				},
			},
		},
	)
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want missing artifact_version_id")
	}
	if !strings.Contains(err.Error(), "artifact_version_id") {
		t.Fatalf("ValidateOutputs() error = %v, want artifact_version_id", err)
	}
}

func writeTestCodeRepairBundle(t *testing.T, inputDir, outputDir string, includeRepairInstruction bool, includePreviousOutputs bool) core.AgentInputBundle {
	t.Helper()

	containerPath := writeRepairTestFile(t, inputDir, "container_context.json", `{"container_id":"ctr-1","repo_dir":"/workspace/repo"}`)
	moduleSpecPath := writeRepairTestFile(t, inputDir, "module_spec.json", `{"module_id":"module01","module_name":"frontend","implementation_role":"front","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`)
	coderBranchPath := writeRepairTestFile(t, inputDir, "coder_branch.json", `{"module_id":"module01","branch":"feature/module01","commit":"abc123","worktree":"/workspace/worktrees/module01","result":"kok","test_command":"npm test","test_passed":true}`)
	fullTestFilesPath := writeRepairTestFile(t, inputDir, "full_test_files.json", `{"kind":"full_test_files","files":[{"path":"tests/module01.test.js","content":"test('repair', () => {});\n"}],"test_command":"npm test --repair"}`)

	inputs := []core.InputArtifact{
		{LogicalKey: core.LKContainerContext, Path: containerPath, ArtifactVersionID: "av_container_context"},
		{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath, ArtifactVersionID: "av_module_spec"},
		{LogicalKey: core.LKCoderBranch, Path: coderBranchPath, ArtifactVersionID: "av_coder_branch"},
		{LogicalKey: core.LKFullTestFiles, Path: fullTestFilesPath, ArtifactVersionID: "av_full_test_files"},
	}
	if includeRepairInstruction {
		repairPath := writeRepairTestFile(t, inputDir, "repair_instruction.md", "# Repair Instruction\nRerun tests against the latest branch and regenerate the report.\n")
		inputs = append(inputs, core.InputArtifact{
			LogicalKey:        core.LKRepairInstruction,
			Path:              repairPath,
			ArtifactVersionID: "av_repair_instruction",
		})
	}

	bundle := core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs:    inputs,
		Bags: []core.AgentInputBag{
			{Name: "module_input", BagID: "bag_module_input", Indexes: map[string]string{"module_key": "module01"}, ArtifactVersionIDs: []string{"av_container_context", "av_module_spec"}},
			{Name: "code_bag", BagID: "bag_code", ArtifactVersionIDs: []string{"av_coder_branch"}},
			{Name: "test_data_bag", BagID: "bag_test_data", ArtifactVersionIDs: []string{"av_full_test_files"}},
		},
	}
	if includePreviousOutputs {
		bundle.PreviousOutputs = []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKModuleTestReport,
				ArtifactVersionID: "av_previous_report",
				ArtifactURI:       "projects/run_x/agents/tester01/artifacts/test_reports/module_test_report.json",
				Path:              writeRepairTestFile(t, inputDir, "previous_module_test_report.json", `{"result":"kfail","summary":"stale previous failure"}`),
				Description:       "previous failed test report for reference",
			},
		}
	}
	return bundle
}

func writeRepairTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
