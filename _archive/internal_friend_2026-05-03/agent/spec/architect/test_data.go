package architect

import (
	"doujia/internal/agent/core"
	"doujia/internal/agent/schema"
)

func TestDataSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "test_data",
		RoleDescription: "Architect agent that generates global acceptance test data and command templates from validated upstream artifacts.",
		OpDescription:   "Read container_context, module_specs, architecture_plan, and environment_spec before producing any output. If merged_main_branch is provided, read it too. Produce exactly global_test_data.md, global_acceptance_tests.json, and global_test_commands.json. Do not invent merge facts when merged_main_branch is absent. Prefer module_specs.global_test_command over environment_spec.default_test_command when building command templates.\n\n" + schema.GlobalTestArtifactsPromptContract(),
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKContainerContext, Description: "Container execution context including container_id and repo_dir."},
			{LogicalKey: core.LKModuleSpecs, Description: "Full module spec list, including optional global_test_command."},
			{LogicalKey: core.LKArchitecturePlan, Description: "Architecture plan describing system-wide acceptance goals."},
			{LogicalKey: core.LKEnvironmentSpec, Description: "Environment spec including runtime and default_test_command."},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{LogicalKey: core.LKMergedMainBranch, Description: "Optional merged main branch artifact used to enrich target commit information."},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Instructions describing what should be repaired in the global test-data artifacts."},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKGlobalTestData, ObjectType: "markdown", FileName: "global_test_data.md", Description: "Human-readable summary of the global acceptance-test data.", Required: true},
			{LogicalKey: core.LKGlobalAcceptanceTests, ObjectType: "json", FileName: "global_acceptance_tests.json", Description: "Machine-readable global acceptance scenarios.", Required: true},
			{LogicalKey: core.LKGlobalTestCommands, ObjectType: "json", FileName: "global_test_commands.json", Description: "Global test command template for architect.test_code.", Required: true},
			{LogicalKey: core.LKUpstreamArtifactIssue, ObjectType: "markdown", FileName: "upstream_artifact_issue.md", Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for architect.test_data.", Required: false},
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_read", Description: "Read registered input artifact content."},
			{Name: "artifact_write", Description: "Write declared output artifact content."},
			{Name: "task_complete", Description: "Return the final AgentResult and finish the current task."},
		},
		PromptTemplateID: "architect.test_data.v1",
	}
}
