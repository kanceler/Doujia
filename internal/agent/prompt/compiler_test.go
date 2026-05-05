package prompt

import (
	"strings"
	"testing"

	"devflow/internal/agent/core"
)

func TestCompileUsesLogicalMetadataWithoutExposingPathsAsPrimaryInput(t *testing.T) {
	t.Parallel()

	text := Compile(
		core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal"},
		core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{
					LogicalKey:        "module_spec",
					Path:              "/tmp/module_spec.json",
					ArtifactVersionID: "ver-input",
					LogicalArtifactID: "la-input",
					ObjectType:        "json",
					Description:       "input description",
				},
			},
			PreviousOutputs: []core.PreviousOutputRef{
				{
					LogicalKey:        "module_test_report",
					Path:              "/tmp/module_test_report.json",
					ArtifactVersionID: "ver-previous",
					LogicalArtifactID: "la-previous",
					ObjectType:        "json",
					Description:       "previous description",
				},
			},
		},
		core.OpSpec{
			Role:            "tester",
			Op:              "test_code",
			RoleDescription: "role description",
			OpDescription:   "op description",
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "module_test_report", ObjectType: "json", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_read", Description: "read"},
				{Name: "artifact_write", Description: "write"},
			},
		},
	)

	for _, want := range []string{"module_spec", "module_test_report", "ver-input", "ver-previous", "la-input", "la-previous", "object_type"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in prompt:\n%s", want, text)
		}
	}
	if strings.Contains(text, "/tmp/module_spec.json") || strings.Contains(text, "/tmp/module_test_report.json") {
		t.Fatalf("Compile() exposed artifact paths in prompt:\n%s", text)
	}
}

func TestCompileIncludesArchitectEnvironmentSpecContract(t *testing.T) {
	t.Parallel()

	text := Compile(
		core.Task{Role: "architect", Op: "write_plan", ExecutionMode: "normal"},
		core.AgentInputBundle{},
		core.OpSpec{
			Role:            "architect",
			Op:              "write_plan",
			RoleDescription: "role description",
			OpDescription:   "must output environment_spec with check_commands and default_test_command",
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "environment_spec", ObjectType: "json", Required: true},
			},
		},
	)

	for _, want := range []string{"check_commands", "default_test_command"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in prompt:\n%s", want, text)
		}
	}
}

func TestCompileIncludesStrictJSONOutputContracts(t *testing.T) {
	t.Parallel()

	text := Compile(
		core.Task{Role: "architect", Op: "merge_code", ExecutionMode: "normal"},
		core.AgentInputBundle{},
		core.OpSpec{
			Role:            "architect",
			Op:              "merge_code",
			RoleDescription: "role description",
			OpDescription:   "op description",
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: core.LKMergedMainBranch, ObjectType: "json", FileName: "merged_main_branch.json", Required: true},
				{LogicalKey: core.LKFullTestFiles, ObjectType: "json", FileName: "full_test_files.json", Required: true},
			},
		},
	)

	for _, want := range []string{
		"STRICT JSON OUTPUT CONTRACTS",
		"Do not wrap JSON in Markdown",
		core.LKMergedMainBranch,
		"merged_commit",
		core.LKFullTestFiles,
		"test_command",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in prompt:\n%s", want, text)
		}
	}
}

func TestCompileIncludesRepairModeRules(t *testing.T) {
	t.Parallel()

	text := Compile(
		core.Task{Role: "tester", Op: "test_code", ExecutionMode: core.ExecutionModeRepair},
		core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKRepairInstruction, ArtifactVersionID: "ver-repair"},
			},
			PreviousOutputs: []core.PreviousOutputRef{
				{LogicalKey: core.LKModuleTestReport, ArtifactVersionID: "ver-previous-report"},
			},
		},
		core.OpSpec{
			Role:            "tester",
			Op:              "test_code",
			RoleDescription: "role description",
			OpDescription:   "op description",
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: core.LKModuleTestReport, ObjectType: "json", Required: true},
			},
		},
	)

	for _, want := range []string{
		"execution_mode: repair",
		"必须先读取 repair_instruction",
		"只修复 repair_instruction 指定的问题",
		"status = reused",
		"当 result = kok 时",
		"不要伪造必需输出",
		core.LKModuleTestReport,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Compile() missing %q in repair prompt:\n%s", want, text)
		}
	}
}
