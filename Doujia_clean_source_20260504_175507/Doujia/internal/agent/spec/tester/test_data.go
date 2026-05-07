package tester

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func TestDataSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "tester",
		Op:              "test_data",
		RoleDescription: "Tester agent that expands module test data from validated upstream artifacts.",
		OpDescription:   "Read container_context, module_spec, tester_task, module_contract, and seed_tests. Produce expanded_test_data.md, boundary_tests.json, and full_test_files.json. Reuse seed_tests test_command and files first, then add only the minimum necessary boundary coverage.",
		InputBags: []core.InputBagSpec{
			{Name: "module_input", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKContainerContext, Required: true}, {LogicalKey: core.LKModuleSpec, Required: true}, {LogicalKey: core.LKTesterTask, Required: true}, {LogicalKey: core.LKModuleContract, Required: true}, {LogicalKey: core.LKSeedTests, Required: true}}},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "test_data_bag", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKExpandedTestData, Required: true}, {LogicalKey: core.LKBoundaryTests, Required: true}, {LogicalKey: core.LKFullTestFiles, Required: true}, {LogicalKey: core.LKUpstreamArtifactIssue}}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKContainerContext, Description: "Container execution context including container_id and repo_dir."},
			{LogicalKey: core.LKModuleSpec, Description: "Current module spec including module_id, tester, test_run_dir, test_command, and owned_paths."},
			{LogicalKey: core.LKTesterTask, Description: "Natural-language tester task for the current module."},
			{LogicalKey: core.LKModuleContract, Description: "Current module contract defining boundaries and interfaces."},
			{LogicalKey: core.LKSeedTests, Description: "Seed test artifact including reusable test files and test_command."},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Instructions describing what should be repaired in the test-data artifacts."},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKExpandedTestData, ObjectType: "markdown", FileName: "expanded_test_data.md", Description: "Human-readable summary of expanded test data and added coverage.", Required: true},
			{LogicalKey: core.LKBoundaryTests, ObjectType: "json", FileName: "boundary_tests.json", Description: "Machine-readable list of boundary test scenarios.", Required: true},
			{LogicalKey: core.LKFullTestFiles, ObjectType: "json", FileName: "full_test_files.json", Description: "Full executable test-file bundle for tester.test_code.", Required: true},
			{LogicalKey: core.LKUpstreamArtifactIssue, ObjectType: "markdown", FileName: "upstream_artifact_issue.md", Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for tester.test_data.", Required: false},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "test_data_bag", Indexes: speccommon.InputBagIndexes(bundle, "module_input"), Members: speccommon.OutputMembers(result, core.LKExpandedTestData, core.LKBoundaryTests, core.LKFullTestFiles, core.LKUpstreamArtifactIssue)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_read", Description: "Read registered input artifact content."},
			{Name: "artifact_write", Description: "Write declared output artifact content."},
			{Name: "task_complete", Description: "Return the final AgentResult and finish the current task."},
		},
		PromptTemplateID: "tester.test_data.v1",
	}
}
