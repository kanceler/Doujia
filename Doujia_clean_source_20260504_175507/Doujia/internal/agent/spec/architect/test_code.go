package architect

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func TestCodeSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "test_code",
		RoleDescription: "Architect agent that runs final global acceptance commands against the merged branch and produces delivery artifacts.",
		OpDescription:   "Read container_context, merged_main_branch, global_test_data, global_acceptance_tests, and global_test_commands. Execute the real global acceptance commands in the merged repository and produce global_test_report.json and delivery_guide.md. Command execution failure is a normal test failure path, not an upstream issue.",
		InputBags: []core.InputBagSpec{
			{Name: "global_test_code_input", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKContainerContext, Required: true}, {LogicalKey: core.LKMergedMainBranch, Required: true}, {LogicalKey: core.LKGlobalTestData, Required: true}, {LogicalKey: core.LKGlobalAcceptanceTests, Required: true}, {LogicalKey: core.LKGlobalTestCommands, Required: true}}},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "global_test_report", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKGlobalTestReport, Required: true}, {LogicalKey: core.LKDeliveryGuide, Required: true}, {LogicalKey: core.LKUpstreamArtifactIssue}}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKContainerContext, Description: "Container execution context including container_id, repo_dir, runtime, and package_manager."},
			{LogicalKey: core.LKMergedMainBranch, Description: "Merged main branch artifact including merged_commit and result."},
			{LogicalKey: core.LKGlobalTestData, Description: "Human-readable global test-data summary."},
			{LogicalKey: core.LKGlobalAcceptanceTests, Description: "Machine-readable global acceptance scenarios."},
			{LogicalKey: core.LKGlobalTestCommands, Description: "Global test command templates to execute."},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Instructions describing what should be repaired in the final global test report or delivery guide."},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKGlobalTestReport, ObjectType: "json", FileName: "global_test_report.json", Description: "Final global test report containing result, test_passed, tested_branch, tested_commit, commands, and summary.", Required: true},
			{LogicalKey: core.LKDeliveryGuide, ObjectType: "markdown", FileName: "delivery_guide.md", Description: "User-facing delivery guide including test results and rerun instructions.", Required: true},
			{LogicalKey: core.LKUpstreamArtifactIssue, ObjectType: "markdown", FileName: "upstream_artifact_issue.md", Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for architect.test_code.", Required: false},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "global_test_report", Members: speccommon.OutputMembers(result, core.LKGlobalTestReport, core.LKDeliveryGuide, core.LKUpstreamArtifactIssue)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_read", Description: "Read registered input artifact content."},
			{Name: "artifact_write", Description: "Write declared output artifacts."},
			{Name: "container_exec", Description: "Execute one command inside container_context.repo_dir and return stdout, stderr, exit_code, and duration."},
		},
		PromptTemplateID: "architect.test_code.v1",
	}
}
