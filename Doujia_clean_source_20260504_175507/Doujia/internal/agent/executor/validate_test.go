package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
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

func TestValidateOutputsChecksDeclaredContentMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "preview", "index.html")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  "preview_html",
				ObjectType:  "html",
				ContentType: "text/html; charset=utf-8",
				Encoding:    "utf-8",
				FileName:    "preview/index.html",
				Required:    true,
			},
		},
	}
	bundle := core.AgentInputBundle{OutputDir: dir, OutputURIBase: "projects/run/agents/front/artifacts/preview"}

	err := ValidateOutputs(core.Task{}, bundle, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  "preview_html",
				ObjectType:  "html",
				ContentType: "text/html; charset=utf-8",
				Encoding:    "utf-8",
				Status:      "produced",
				Path:        path,
				ArtifactURI: "projects/run/agents/front/artifacts/preview/preview/index.html",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}

	err = ValidateOutputs(core.Task{}, bundle, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  "preview_html",
				ObjectType:  "html",
				ContentType: "text/plain",
				Encoding:    "utf-8",
				Status:      "produced",
				Path:        path,
				ArtifactURI: "projects/run/agents/front/artifacts/preview/preview/index.html",
			},
		},
	})
	if err == nil {
		t.Fatalf("ValidateOutputs() error = nil, want content_type mismatch")
	}
	if !strings.Contains(err.Error(), "content_type") {
		t.Fatalf("ValidateOutputs() error = %v, want content_type mismatch", err)
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

func TestValidateOutputsRejectsCoderBranchMissingResultAndBaseCommit(t *testing.T) {
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
	path := filepath.Join(dir, "coder_branch.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module02","container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","changed_files":["server/core/gameState.js"],"test_command":"npm test","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want missing field error")
	}
	if !strings.Contains(err.Error(), "base_commit") && !strings.Contains(err.Error(), "result") {
		t.Fatalf("ValidateOutputs() error = %v, want base_commit/result detail", err)
	}
}

func TestValidateOutputsAllowsWellFormedCoderBranch(t *testing.T) {
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
	path := filepath.Join(dir, "coder_branch.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module02","container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main","base_commit":"def456","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","changed_files":["server/core/gameState.js"],"test_command":"npm test","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				Status:     "produced",
				Path:       path,
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestValidateOutputsRejectsBOMJSONArtifact(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKGlobalTestReport,
				ObjectType: "json",
				FileName:   "global_test_report.json",
				Required:   true,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "global_test_report.json")
	body := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"kind":"global_test_report","result":"kok","test_passed":true,"tested_commit":"abc123"}`)...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: core.LKGlobalTestReport, ObjectType: "json", Status: "produced", Path: path},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want BOM rejection")
	}
	if !strings.Contains(err.Error(), "BOM") {
		t.Fatalf("ValidateOutputs() error = %v, want BOM detail", err)
	}
}

func TestValidateOutputsRejectsGlobalTestReportMissingTestedCommit(t *testing.T) {
	t.Parallel()

	spec := core.OpSpec{
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey: core.LKGlobalTestReport,
				ObjectType: "json",
				FileName:   "global_test_report.json",
				Required:   true,
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "global_test_report.json")
	if err := os.WriteFile(path, []byte(`{"kind":"global_test_report","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: core.LKGlobalTestReport, ObjectType: "json", Status: "produced", Path: path},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want tested_commit rejection")
	}
	if !strings.Contains(err.Error(), "tested_commit") {
		t.Fatalf("ValidateOutputs() error = %v, want tested_commit detail", err)
	}
}

func TestValidateOutputsAcceptsDeclaredOutputBagContract(t *testing.T) {
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
		OutputBags: []core.OutputBagSpec{
			{
				Name:     "code_bag",
				Required: true,
				Members: []core.BagMemberRequirement{
					{LogicalKey: core.LKCoderBranch, Required: true},
					{LogicalKey: core.LKWriteCodeFailure, Required: false},
				},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "coder_branch.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module02","container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main","base_commit":"def456","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","changed_files":["server/core/gameState.js"],"test_command":"npm test","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				Status:     "produced",
				Path:       path,
			},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "code_bag",
				Members: []core.ProducedBagMember{
					{LogicalKey: core.LKCoderBranch},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateOutputs() error = %v", err)
	}
}

func TestValidateOutputsRejectsUnknownProducedBag(t *testing.T) {
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
		OutputBags: []core.OutputBagSpec{
			{
				Name:     "code_bag",
				Required: true,
				Members: []core.BagMemberRequirement{
					{LogicalKey: core.LKCoderBranch, Required: true},
				},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "coder_branch.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module02","container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main","base_commit":"def456","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","changed_files":["server/core/gameState.js"],"test_command":"npm test","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				Status:     "produced",
				Path:       path,
			},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "unexpected_bag",
				Members: []core.ProducedBagMember{
					{LogicalKey: core.LKCoderBranch},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want unknown produced bag error")
	}
	if !strings.Contains(err.Error(), "unexpected_bag") {
		t.Fatalf("ValidateOutputs() error = %v, want unexpected_bag detail", err)
	}
}

func TestValidateOutputsRejectsMissingRequiredBagMember(t *testing.T) {
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
		OutputBags: []core.OutputBagSpec{
			{
				Name:     "code_bag",
				Required: true,
				Members: []core.BagMemberRequirement{
					{LogicalKey: core.LKCoderBranch, Required: true},
					{LogicalKey: core.LKWriteCodeFailure, Required: true},
				},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "coder_branch.json")
	if err := os.WriteFile(path, []byte(`{"module_id":"module02","container_id":"ctr-1","repo_dir":"/workspace/repo","base_branch":"main","base_commit":"def456","branch":"feature/module02","commit":"abc123","worktree":"/workspace/worktrees/module02","changed_files":["server/core/gameState.js"],"test_command":"npm test","result":"kok","test_passed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateOutputs(core.Task{}, core.AgentInputBundle{OutputDir: dir}, spec, core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{
				LogicalKey: core.LKCoderBranch,
				ObjectType: "json",
				Status:     "produced",
				Path:       path,
			},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "code_bag",
				Members: []core.ProducedBagMember{
					{LogicalKey: core.LKCoderBranch},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("ValidateOutputs() error = nil, want missing required bag member error")
	}
	if !strings.Contains(err.Error(), core.LKWriteCodeFailure) {
		t.Fatalf("ValidateOutputs() error = %v, want write_code_failure detail", err)
	}
}
