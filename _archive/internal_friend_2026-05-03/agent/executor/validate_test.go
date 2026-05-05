package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
)

func TestValidateOutputsAllowsDeclaredFailureOutputForKfail(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
			{
				LogicalKey: core.LKWriteCodeFailure,
				ObjectType: "markdown",
				FileName:   "write_code_failure.md",
				Required:   false,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "write_code_failure.md")
	if err := os.WriteFile(path, []byte("# fail"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kfail",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKWriteCodeFailure,
				ObjectType: "markdown",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestValidateOutputsRejectsUnexpectedFailureOutputForKfail(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "unexpected.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kfail",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: "unexpected_failure",
				ObjectType: "markdown",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want unexpected output error")
	}
}

func TestValidateInputsRepairRequiresRepairInstruction(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKModuleSpec},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeNormal: {},
			core.ExecutionModeRepair: {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction},
				},
			},
		},
	}
	moduleSpecPath := filepath.Join(t.TempDir(), "module_spec.json")
	if err := os.WriteFile(moduleSpecPath, []byte(`{"module_id":"module01"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := core.AgentInputBundle{
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKModuleSpec, Path: moduleSpecPath},
		},
	}

	err := ValidateInputs(core.Task{ExecutionMode: core.ExecutionModeRepair}, bundle, spec)
	if err == nil {
		t.Fatal("ValidateInputs() error = nil, want missing repair_instruction")
	}
	if !strings.Contains(err.Error(), core.LKRepairInstruction) {
		t.Fatalf("ValidateInputs() error = %v, want repair_instruction", err)
	}

	repairPath := filepath.Join(t.TempDir(), "repair_instruction.md")
	if err := os.WriteFile(repairPath, []byte("# Repair"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle.Inputs = append(bundle.Inputs, core.InputArtifact{LogicalKey: core.LKRepairInstruction, Path: repairPath})
	if err := ValidateInputs(core.Task{ExecutionMode: core.ExecutionModeRepair}, bundle, spec); err != nil {
		t.Fatalf("ValidateInputs() error = %v, want nil", err)
	}
}

func TestBuildReuseResultPreservesArtifactURI(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
		},
	}

	result, err := BuildReuseResult(core.Task{}, core.AgentInputBundle{
		PreviousOutputs: []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "ver-front-branch",
				ArtifactURI:       "projects/run_x/agents/front01/artifacts/write_code/coder_branch.json",
				Path:              filepath.Join(t.TempDir(), "coder_branch.json"),
			},
		},
	}, spec)
	if err != nil {
		t.Fatalf("BuildReuseResult() error = %v", err)
	}
	if got := result.Outputs[0].ArtifactURI; got != "projects/run_x/agents/front01/artifacts/write_code/coder_branch.json" {
		t.Fatalf("BuildReuseResult() artifact_uri = %q, want preserved value", got)
	}
}

func TestValidateOutputsAllowsReusedOutputWithoutPath(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
		},
	}

	err := ValidateOutputs(core.Task{ExecutionMode: core.ExecutionModeRepair}, core.AgentInputBundle{OutputDir: t.TempDir()}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:        core.LKCoderBranch,
				ObjectType:        "json",
				Status:            "reused",
				ArtifactVersionID: "av_coder_branch",
				ArtifactURI:       "projects/run_x/agents/coder01/artifacts/write_code/coder_branch.json",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestValidateOutputsRejectsReusedOutputWithWhitespaceArtifactVersionID(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
		},
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: t.TempDir()}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:        core.LKCoderBranch,
				ObjectType:        "json",
				Status:            "reused",
				ArtifactVersionID: "   ",
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want empty artifact_version_id error")
	}
	if !strings.Contains(err.Error(), "artifact_version_id") {
		t.Fatalf("ValidateOutputs() error = %v, want artifact_version_id", err)
	}
}

func TestBuildReuseResultRejectsWhitespaceArtifactVersionID(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
		},
	}

	_, err := BuildReuseResult(core.Task{}, core.AgentInputBundle{
		PreviousOutputs: []core.PreviousOutputRef{
			{
				LogicalKey:        core.LKCoderBranch,
				ArtifactVersionID: "\t ",
			},
		},
	}, spec)
	if err == nil {
		t.Fatal("BuildReuseResult() error = nil, want empty artifact_version_id error")
	}
	if !strings.Contains(err.Error(), "artifact_version_id") {
		t.Fatalf("BuildReuseResult() error = %v, want artifact_version_id", err)
	}
}

func TestValidateOutputsAllowsFailureOutputsWithoutRequiredArtifacts(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				FileName:   "coder_branch.json",
				Required:   true,
			},
			{
				LogicalKey: core.LKWriteCodeFailure,
				ObjectType: "markdown",
				FileName:   "write_code_failure.md",
				Required:   false,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "write_code_failure.md")
	if err := os.WriteFile(path, []byte("# fail"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kbug",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKWriteCodeFailure,
				ObjectType: "markdown",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestValidateOutputsRequiresArtifactURIWhenOutputURIBaseIsSet(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKUpstreamArtifactIssue,
				ObjectType: "markdown",
				FileName:   "upstream_artifact_issue.md",
				Required:   false,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "upstream_artifact_issue.md")
	if err := os.WriteFile(path, []byte("# issue"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{
		OutputDir:     dir,
		OutputURIBase: "projects/run01/agents/coder01/artifacts/failures",
	}, spec, core.AgentResult{
		Result: "kbug",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKUpstreamArtifactIssue,
				ObjectType: "markdown",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want missing artifact_uri error")
	}
}

func TestValidateOutputsAllowsEmptyArtifactURIWhenOutputURIBaseIsUnset(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKUpstreamArtifactIssue,
				ObjectType: "markdown",
				FileName:   "upstream_artifact_issue.md",
				Required:   false,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "upstream_artifact_issue.md")
	if err := os.WriteFile(path, []byte("# issue"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{
		OutputDir: dir,
	}, spec, core.AgentResult{
		Result: "kbug",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKUpstreamArtifactIssue,
				ObjectType: "markdown",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}
