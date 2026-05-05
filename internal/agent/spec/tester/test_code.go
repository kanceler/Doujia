package tester

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func TestCodeSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "tester",
		Op:              "test_code",
		RoleDescription: "Tester agent that writes complete module test files into the current worktree, runs real test commands, and produces a module test report.",
		OpDescription:   "Read container_context, module_spec, coder_branch, and full_test_files. Write the complete test files into the current module worktree, execute the real test command, and produce module_test_report.json from the observed stdout, stderr, exit code, and duration. All container file operations must stay within module_spec.worktree_dir, and the agent must not invent undeclared paths or logical keys.",
		InputBags: []core.InputBagSpec{
			{Name: "module_input", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
				{LogicalKey: core.LKModuleSpec, Required: true},
			}},
			{Name: "code_bag", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKCoderBranch, Required: true},
			}},
			{Name: "test_data_bag", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKFullTestFiles, Required: true},
			}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKContainerContext, Description: "Container execution context including container_id and repo_dir."},
			{LogicalKey: core.LKModuleSpec, Description: "Current module spec including module_id, test_run_dir, test_command, tester, and owned_paths."},
			{LogicalKey: core.LKCoderBranch, Description: "Latest coder branch result including branch, commit, worktree, test_command, and test_passed."},
			{LogicalKey: core.LKFullTestFiles, Description: "Full module test file bundle including files and test_command for the current module."},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Instructions describing what should be repaired in the current module_test_report."},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKModuleTestReport, ObjectType: "json", FileName: "module_test_report.json", Description: "Module test report containing real execution facts such as result, test_passed, tested_branch, tested_commit, stdout, stderr, exit_code, duration_ms, and summary.", Required: true},
			{LogicalKey: core.LKUpstreamArtifactIssue, ObjectType: "markdown", FileName: "upstream_artifact_issue.md", Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for tester.test_code.", Required: false},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "tested_module", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKModuleTestReport, Required: true},
				{LogicalKey: core.LKUpstreamArtifactIssue, Required: false},
			}},
			{Name: "failure_report", Required: false, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKModuleTestReport, Required: false},
				{LogicalKey: core.LKUpstreamArtifactIssue, Required: false},
			}},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			name := "tested_module"
			if result.Result == string(appcore.TaskResultCodeBug) {
				name = "failure_report"
			}
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: name, Indexes: speccommon.InputBagIndexes(bundle, "module_input"), Members: speccommon.OutputMembers(result, core.LKModuleTestReport, core.LKUpstreamArtifactIssue)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_read", Description: "Read registered input artifact content."},
			{Name: "artifact_write", Description: "Write declared output artifacts."},
			{Name: "container_write", Description: "Write test files into the current module worktree."},
			{Name: "container_run", Description: "Run the current module test command inside the worktree."},
		},
		PromptTemplateID: "tester.test_code.v1",
	}
}
