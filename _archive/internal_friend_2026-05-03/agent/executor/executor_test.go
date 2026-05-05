package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
)

type testAgentRegistry struct {
	agent  core.Agent
	agents map[string]core.Agent
}

func (r testAgentRegistry) Get(role string) (core.Agent, bool) {
	if r.agents != nil {
		agent, ok := r.agents[role]
		return agent, ok
	}
	if r.agent == nil || r.agent.Role() != role {
		return nil, false
	}
	return r.agent, true
}

type testOpRegistry struct {
	spec  core.OpSpec
	specs map[string]core.OpSpec
}

func (r testOpRegistry) Get(role, op string) (core.OpSpec, bool) {
	if r.specs != nil {
		spec, ok := r.specs[role+"."+op]
		return spec, ok
	}
	if r.spec.Role != role || r.spec.Op != op {
		return core.OpSpec{}, false
	}
	return r.spec, true
}

type testAgent struct {
	role   string
	result core.AgentResult
	err    error
}

func (a testAgent) Role() string {
	return a.role
}

func (a testAgent) Run(_ context.Context, _ core.AgentRunRequest) (core.AgentResult, error) {
	return a.result, a.err
}

type recordingAgent struct {
	role   string
	result core.AgentResult
	err    error
	calls  []core.AgentRunRequest
}

func (a *recordingAgent) Role() string {
	return a.role
}

func (a *recordingAgent) Run(_ context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	a.calls = append(a.calls, req)
	return a.result, a.err
}

type testHandlerRegistry struct {
	handlers map[string]core.Handler
}

func (r testHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}

type testHandler struct {
	name string
}

func (h testHandler) Name() string { return h.name }

func (h testHandler) Description() string { return h.name }

func (h testHandler) ToolSpec() core.ToolSpec { return core.ToolSpec{Name: h.name} }

func (h testHandler) Handle(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
	return core.HandlerResponse{Data: map[string]any{"name": h.name}}, nil
}

func TestExecutePreservesFailureOutputs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	reportPath := filepath.Join(outputDir, "module_test_report.json")
	if err := os.WriteFile(reportPath, []byte(`{"result":"kfail"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	spec := core.OpSpec{
		Role: "tester",
		Op:   "test_code",
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: "module_test_report",
				ObjectType: "json",
				FileName:   "module_test_report.json",
				Required:   true,
			},
		},
	}
	agent := testAgent{
		role: "tester",
		result: core.AgentResult{
			Result:  "kfail",
			Message: "tester test_code failed",
			Outputs: []core.AgentOutput{
				{
					LogicalKey: "module_test_report",
					ObjectType: "json",
					Status:     "produced",
					Path:       reportPath,
				},
			},
			Errors: []core.AgentError{
				{Code: "test_code_failed", Message: "module tests failed"},
			},
		},
	}
	runtime := NewRuntime(testAgentRegistry{agent: agent}, testOpRegistry{spec: spec}, nil)

	result, err := runtime.Execute(context.Background(), core.Task{
		Role:          "tester",
		Op:            "test_code",
		ExecutionMode: "normal",
	}, core.AgentInputBundle{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Execute() result = %q, want %q", result.Result, "kfail")
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("Execute() outputs len = %d, want 1", len(result.Outputs))
	}
	if result.Outputs[0].LogicalKey != "module_test_report" {
		t.Fatalf("Execute() output logical_key = %q, want %q", result.Outputs[0].LogicalKey, "module_test_report")
	}
}

func TestRunAgentReturnsAgentFailureOutputs(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	reportPath := filepath.Join(outputDir, "module_test_report.json")
	if err := os.WriteFile(reportPath, []byte(`{"result":"kfail"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	spec := core.OpSpec{
		Role: "tester",
		Op:   "test_code",
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: "module_test_report",
				ObjectType: "json",
				FileName:   "module_test_report.json",
				Required:   true,
			},
		},
	}
	agent := testAgent{
		role: "tester",
		result: core.AgentResult{
			Result: "kfail",
			Outputs: []core.AgentOutput{
				{
					LogicalKey: "module_test_report",
					ObjectType: "json",
					Status:     "produced",
					Path:       reportPath,
				},
			},
		},
	}
	runtime := NewRuntime(testAgentRegistry{agent: agent}, testOpRegistry{spec: spec}, nil)

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "tester",
		Op:            "test_code",
		ExecutionMode: "normal",
	}, core.AgentInputBundle{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("RunAgent() result = %q, want %q", result.Result, "kfail")
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("RunAgent() outputs len = %d, want 1", len(result.Outputs))
	}
}

func TestRunAgentDefaultsEmptyExecutionModeToNormal(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	reportPath := filepath.Join(outputDir, "module_test_report.json")
	if err := os.WriteFile(reportPath, []byte(`{"result":"kok"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	spec := core.OpSpec{
		Role: "tester",
		Op:   "test_code",
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeNormal: {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: "module_test_report",
				ObjectType: "json",
				FileName:   "module_test_report.json",
				Required:   true,
			},
		},
	}
	agent := &recordingAgent{
		role: "tester",
		result: core.AgentResult{
			Result: "kok",
			Outputs: []core.AgentOutput{
				{
					LogicalKey: "module_test_report",
					ObjectType: "json",
					Status:     "produced",
					Path:       reportPath,
				},
			},
		},
	}
	runtime := NewRuntime(testAgentRegistry{agent: agent}, testOpRegistry{spec: spec}, nil)

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role: "tester",
		Op:   "test_code",
	}, core.AgentInputBundle{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want kok", result.Result)
	}
	if len(agent.calls) != 1 {
		t.Fatalf("agent calls = %d, want 1", len(agent.calls))
	}
	if got := agent.calls[0].Task.ExecutionMode; got != core.ExecutionModeNormal {
		t.Fatalf("effective execution_mode = %q, want %q", got, core.ExecutionModeNormal)
	}
	if !strings.Contains(agent.calls[0].Prompt, "execution_mode: normal") {
		t.Fatalf("prompt = %q, want normal execution_mode", agent.calls[0].Prompt)
	}
}

func TestRunAgentRepairRequiresRepairInstruction(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		Role: "tester",
		Op:   "test_code",
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeNormal: {},
			core.ExecutionModeRepair: {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction},
				},
			},
		},
	}
	agent := &recordingAgent{role: "tester", result: core.AgentResult{Result: "kok"}}
	runtime := NewRuntime(testAgentRegistry{agent: agent}, testOpRegistry{spec: spec}, nil)

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "tester",
		Op:            "test_code",
		ExecutionMode: core.ExecutionModeRepair,
	}, core.AgentInputBundle{OutputDir: t.TempDir()})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("RunAgent() result = %q, want kfail", result.Result)
	}
	if len(agent.calls) != 0 {
		t.Fatalf("agent calls = %d, want 0 when repair input is missing", len(agent.calls))
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, core.LKRepairInstruction) {
		t.Fatalf("RunAgent() errors = %+v, want missing repair_instruction", result.Errors)
	}
}

func TestRunAgentRoutesWriteCodeToImplementationRole(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	modulePath := filepath.Join(inputDir, "module_spec.json")
	if err := os.WriteFile(modulePath, []byte(`{
  "module_id": "module01",
  "module_name": "frontend",
  "module_role": "frontend",
  "implementation_role": "front",
  "branch_name": "feature/module01-frontend",
  "worktree_dir": "/workspace/worktrees/module01",
  "owned_paths": ["pages/**"],
  "test_command": "npm test",
  "complexity": "high"
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	frontAgent := &recordingAgent{role: "front", result: core.AgentResult{Result: "kok"}}
	coderAgent := &recordingAgent{role: "coder", result: core.AgentResult{Result: "kok"}}
	runtime := NewRuntime(
		testAgentRegistry{agents: map[string]core.Agent{"front": frontAgent, "coder": coderAgent}},
		testOpRegistry{specs: map[string]core.OpSpec{
			"front.write_code": {
				Role:               "front",
				Op:                 "write_code",
				RoleDescription:    "front-role-description",
				ModeInputRules:     map[string]core.ModeInputRule{"normal": {}},
				BaseRequiredInputs: []core.InputRequirement{{LogicalKey: core.LKModuleSpec}},
			},
			"coder.write_code": {
				Role:               "coder",
				Op:                 "write_code",
				RoleDescription:    "coder-role-description",
				ModeInputRules:     map[string]core.ModeInputRule{"normal": {}},
				BaseRequiredInputs: []core.InputRequirement{{LogicalKey: core.LKModuleSpec}},
			},
		}},
		nil,
	)

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "coder",
		Op:            "write_code",
		ExecutionMode: "normal",
	}, core.AgentInputBundle{
		InputDir: inputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKModuleSpec, Path: modulePath},
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want kok", result.Result)
	}
	if len(frontAgent.calls) != 1 {
		t.Fatalf("front agent calls = %d, want 1", len(frontAgent.calls))
	}
	if len(coderAgent.calls) != 0 {
		t.Fatalf("coder agent calls = %d, want 0", len(coderAgent.calls))
	}
	if frontAgent.calls[0].Task.Role != "front" {
		t.Fatalf("effective task role = %q, want front", frontAgent.calls[0].Task.Role)
	}
	if !strings.Contains(frontAgent.calls[0].Prompt, "front-role-description") {
		t.Fatalf("prompt = %q, want front role description", frontAgent.calls[0].Prompt)
	}
}

func TestRunAgentKeepsCoderWhenImplementationRoleIsCoder(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	modulePath := filepath.Join(inputDir, "module_spec.json")
	if err := os.WriteFile(modulePath, []byte(`{
  "module_id": "module02",
  "module_name": "backend",
  "module_role": "backend",
  "implementation_role": "coder",
  "branch_name": "feature/module02-backend",
  "worktree_dir": "/workspace/worktrees/module02",
  "owned_paths": ["server/**"],
  "test_command": "npm test",
  "complexity": "high"
}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	frontAgent := &recordingAgent{role: "front", result: core.AgentResult{Result: "kok"}}
	coderAgent := &recordingAgent{role: "coder", result: core.AgentResult{Result: "kok"}}
	runtime := NewRuntime(
		testAgentRegistry{agents: map[string]core.Agent{"front": frontAgent, "coder": coderAgent}},
		testOpRegistry{specs: map[string]core.OpSpec{
			"front.write_code": {
				Role:               "front",
				Op:                 "write_code",
				RoleDescription:    "front-role-description",
				ModeInputRules:     map[string]core.ModeInputRule{"normal": {}},
				BaseRequiredInputs: []core.InputRequirement{{LogicalKey: core.LKModuleSpec}},
			},
			"coder.write_code": {
				Role:               "coder",
				Op:                 "write_code",
				RoleDescription:    "coder-role-description",
				ModeInputRules:     map[string]core.ModeInputRule{"normal": {}},
				BaseRequiredInputs: []core.InputRequirement{{LogicalKey: core.LKModuleSpec}},
			},
		}},
		nil,
	)

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "coder",
		Op:            "write_code",
		ExecutionMode: "normal",
	}, core.AgentInputBundle{
		InputDir: inputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKModuleSpec, Path: modulePath},
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want kok", result.Result)
	}
	if len(coderAgent.calls) != 1 {
		t.Fatalf("coder agent calls = %d, want 1", len(coderAgent.calls))
	}
	if len(frontAgent.calls) != 0 {
		t.Fatalf("front agent calls = %d, want 0", len(frontAgent.calls))
	}
	if coderAgent.calls[0].Task.Role != "coder" {
		t.Fatalf("effective task role = %q, want coder", coderAgent.calls[0].Task.Role)
	}
	if !strings.Contains(coderAgent.calls[0].Prompt, "coder-role-description") {
		t.Fatalf("prompt = %q, want coder role description", coderAgent.calls[0].Prompt)
	}
}

func TestScopedHandlerRegistryAllowsInternalPreflightHandlerOutsideAllowedTools(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_write"},
		},
	}
	registry := scopedHandlerRegistry{
		base: testHandlerRegistry{
			handlers: map[string]core.Handler{
				"artifact_write":                 testHandler{name: "artifact_write"},
				"container_git_worktree_prepare": testHandler{name: "container_git_worktree_prepare"},
				"container_exec":                 testHandler{name: "container_exec"},
			},
		},
		spec:    spec,
		allowed: allowedToolNames(spec),
	}

	if _, ok := registry.Get("container_git_worktree_prepare"); !ok {
		t.Fatal("Get(container_git_worktree_prepare) = false, want true for internal preflight handler")
	}
	if _, ok := registry.Get("container_exec"); ok {
		t.Fatal("Get(container_exec) = true, want false for non-internal unlisted handler")
	}
}
