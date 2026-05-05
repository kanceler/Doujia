package coder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
	"doujia/internal/agent/executor"
	"doujia/internal/agent/prompt"
	coderspec "doujia/internal/agent/spec/coder"
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

func TestCoderWriteCodeRepairMissingInstructionReturnsKfailBeforeExecution(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	bundle := writeCodeRepairBundle(t, inputDir, outputDir, false, false)

	prepareCalled := false
	toolLoopCalled := false
	runtime := executor.NewRuntime(
		repairTestAgentRegistry{agent: NewAgent()},
		repairTestOpRegistry{spec: coderspec.WriteCodeSpec()},
		stubHandlerRegistry{handlers: map[string]core.Handler{
			"container_git_worktree_prepare": stubHandler{handleFunc: func(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
				prepareCalled = true
				return core.HandlerResponse{}, fmt.Errorf("should not be called")
			}},
		}},
	).WithToolLoop(stubToolLoop{
		runFunc: func(context.Context, core.ToolLoopRequest) (core.AgentResult, error) {
			toolLoopCalled = true
			return core.AgentResult{Result: "kok"}, nil
		},
	})

	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "coder",
		Op:            "write_code",
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
	if prepareCalled {
		t.Fatal("RunAgent() called prepare handler despite missing repair_instruction")
	}
	if toolLoopCalled {
		t.Fatal("RunAgent() called tool loop despite missing repair_instruction")
	}
}

func TestCoderWriteCodeRepairPromptIncludesRepairInstructions(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	bundle := writeCodeRepairBundle(t, inputDir, outputDir, true, true)

	text := prompt.Compile(
		core.Task{Role: "coder", Op: "write_code", ExecutionMode: core.ExecutionModeRepair},
		bundle,
		coderspec.WriteCodeSpec(),
	)

	for _, want := range []string{
		"execution_mode: repair",
		"当前执行模式：repair",
		"必须先读取 repair_instruction",
		"不要无理由重新生成所有产物",
		"如果提供 previous_outputs，请先判断哪些输出可以复用",
		"status = reused",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in prompt:\n%s", want, text)
		}
	}
}

func TestCoderWriteCodeRepairAllowsReusedCoderBranchWithoutPath(t *testing.T) {
	t.Parallel()

	err := executor.ValidateOutputs(
		core.Task{Role: "coder", Op: "write_code", ExecutionMode: core.ExecutionModeRepair},
		core.AgentInputBundle{OutputDir: t.TempDir()},
		coderspec.WriteCodeSpec(),
		core.AgentResult{
			Result: "kok",
			Outputs: []core.AgentOutput{
				{
					LogicalKey:        core.LKCoderBranch,
					ObjectType:        "json",
					Status:            "reused",
					ArtifactVersionID: "av_xxx",
					ArtifactURI:       "projects/run_x/agents/coder01/artifacts/branches/coder_branch.json",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestCoderWriteCodeRepairRejectsReusedCoderBranchWithoutArtifactVersionID(t *testing.T) {
	t.Parallel()

	err := executor.ValidateOutputs(
		core.Task{Role: "coder", Op: "write_code", ExecutionMode: core.ExecutionModeRepair},
		core.AgentInputBundle{OutputDir: t.TempDir()},
		coderspec.WriteCodeSpec(),
		core.AgentResult{
			Result: "kok",
			Outputs: []core.AgentOutput{
				{
					LogicalKey:  core.LKCoderBranch,
					ObjectType:  "json",
					Status:      "reused",
					ArtifactURI: "projects/run_x/agents/coder01/artifacts/branches/coder_branch.json",
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

func writeCodeRepairBundle(t *testing.T, inputDir, outputDir string, includeRepairInstruction bool, includePreviousOutputs bool) core.AgentInputBundle {
	t.Helper()

	containerPath := writeRepairFile(t, inputDir, "container_context.json", `{"container_id":"ctr-1","repo_dir":"/workspace/repo","worktrees_dir":"/workspace/worktrees","base_branch":"main"}`)
	moduleSpecPath := writeRepairFile(t, inputDir, "module_spec.json", `{"module_id":"module01","module_name":"frontend","module_role":"frontend","implementation_role":"coder","branch_name":"feature/module01","worktree_dir":"/workspace/worktrees/module01","owned_paths":["server/**"],"test_command":"npm test","complexity":"high"}`)
	coderTaskPath := writeRepairFile(t, inputDir, "coder_task.md", "# Coder Task\nRepair the module branch.\n")
	moduleContractPath := writeRepairFile(t, inputDir, "module_contract.md", "# Module Contract\nKeep the API shape stable.\n")
	seedTestsPath := writeRepairFile(t, inputDir, "seed_tests.json", `{"files":[{"path":"server/module01.test.js","content":"test('seed', () => {});\n"}]}`)

	inputs := []core.InputArtifact{
		{LogicalKey: core.LKContainerContext, Path: containerPath},
		{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
		{LogicalKey: core.LKCoderTask, Path: coderTaskPath},
		{LogicalKey: core.LKModuleContract, Path: moduleContractPath},
		{LogicalKey: core.LKSeedTests, Path: seedTestsPath},
	}
	if includeRepairInstruction {
		repairPath := writeRepairFile(t, inputDir, "repair_instruction.md", "# Repair Instruction\nOnly fix the failing module branch output.\n")
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
	}
	if includePreviousOutputs {
		bundle.PreviousOutputs = []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "av_previous_coder_branch",
				ArtifactURI:       "projects/run_x/agents/coder01/artifacts/branches/coder_branch.json",
				Description:       "previous coder branch output that may be reused",
			},
		}
	}
	return bundle
}

func writeRepairFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
