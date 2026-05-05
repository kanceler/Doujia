package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"doujia/internal/agent/core"
)

func TestDefaultRuntimeSupportsFrontWriteCodeReuse(t *testing.T) {
	t.Parallel()

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	bundle := core.AgentInputBundle{
		InputDir:  inputDir,
		OutputDir: outputDir,
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKContainerContext, Path: writeTestFile(t, inputDir, "container_context.json", `{"container_id":"c1"}`)},
			{LogicalKey: core.LKModuleSpec, Path: writeTestFile(t, inputDir, "module_spec.json", `{"module_id":"module01","module_name":"frontend","module_role":"frontend","implementation_role":"front","branch_name":"feature/module01-frontend","worktree_dir":"/workspace/worktrees/module01","owned_paths":["pages/**"],"test_command":"npm test","complexity":"high"}`)},
			{LogicalKey: core.LKCoderTask, Path: writeTestFile(t, inputDir, "coder_task.md", "# Front task")},
			{LogicalKey: core.LKModuleContract, Path: writeTestFile(t, inputDir, "module_contract.md", "# Contract")},
			{LogicalKey: core.LKSeedTests, Path: writeTestFile(t, inputDir, "seed_tests.json", `{"files":[]}`)},
		},
		PreviousOutputs: []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "ver-front-branch",
				Path:              filepath.Join(outputDir, "coder_branch.json"),
				ObjectType:        "json",
				Description:       "reused branch output",
			},
		},
	}

	runtime := NewDefaultRuntime()
	result, err := runtime.RunAgent(context.Background(), core.Task{
		Role:          "front",
		Op:            "write_code",
		ExecutionMode: "reuse",
	}, bundle)
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("RunAgent() result = %q, want %q; errors = %+v", result.Result, "kok", result.Errors)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("RunAgent() outputs len = %d, want 1", len(result.Outputs))
	}
	output := result.Outputs[0]
	if output.LogicalKey != core.LKCoderBranch {
		t.Fatalf("RunAgent() output logical_key = %q, want %q", output.LogicalKey, core.LKCoderBranch)
	}
	if output.Status != "reused" {
		t.Fatalf("RunAgent() output status = %q, want %q", output.Status, "reused")
	}
	if output.ArtifactVersionID != "ver-front-branch" {
		t.Fatalf("RunAgent() output artifact_version_id = %q, want %q", output.ArtifactVersionID, "ver-front-branch")
	}
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
