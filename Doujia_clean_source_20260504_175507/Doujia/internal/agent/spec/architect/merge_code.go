package architect

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func MergeCodeSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "merge_code",
		RoleDescription: "Architect agent that verifies tested coder branches, performs the real repository merge, and records the merged main branch result.",
		OpDescription:   "Read container_context and module_specs, then load each <module_id>_coder_branch and <module_id>_module_test_report input. Only when all module test reports pass and all coder branches share the same base_branch and base_commit may the agent call container_git_cherry_pick to apply commits into container_context.repo_dir. The agent must always emit merged_code_v1.md, merged_main_branch.json, and merged_main_branch.md, and should emit merge_code_report.md when the merge is blocked or fails.",
		InputBags: []core.InputBagSpec{
			{Name: "container_context", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
			}},
			{Name: "front_tested_module", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKModuleTestReport, Required: true},
			}},
			{Name: "backend_tested_module", Required: true, Collection: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKModuleTestReport, Required: true},
			}},
			{Name: "front_code_bag", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKCoderBranch, Required: true},
			}},
			{Name: "backend_code_bag", Required: true, Collection: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKCoderBranch, Required: true},
			}},
			{Name: "tested_module", Collection: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKModuleTestReport, Required: true},
			}},
			{Name: "code_bag", Collection: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKCoderBranch, Required: true},
			}},
			{Name: "global_test_input"},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKContainerContext, Description: "Container execution context including container_id, repo_dir, and base_branch."},
			{LogicalKey: core.LKModuleSpecs, Description: "Combined module specification list for the modules to merge."},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{LogicalKey: core.LKRepairInstruction, Description: "Instructions describing what should be repaired in the merge outputs."},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKMergedCodeSummary, ObjectType: "markdown", FileName: "merged_code_v1.md", Description: "Human-readable summary of the merge result.", Required: true},
			{LogicalKey: core.LKMergedMainBranch, ObjectType: "json", FileName: "merged_main_branch.json", Description: "Machine-readable merged main branch result for downstream stages.", Required: true},
			{LogicalKey: core.LKMergedMainBranchNote, ObjectType: "markdown", FileName: "merged_main_branch.md", Description: "Human-readable merged main branch note.", Required: true},
			{LogicalKey: core.LKMergeCodeReport, ObjectType: "markdown", FileName: "merge_code_report.md", Description: "Readable failure report when the merge is blocked or fails.", Required: false},
			{LogicalKey: core.LKUpstreamArtifactIssue, ObjectType: "markdown", FileName: "upstream_artifact_issue.md", Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for architect.merge_code.", Required: false},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "merged_code", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKMergedCodeSummary, Required: true},
				{LogicalKey: core.LKMergedMainBranch, Required: true},
				{LogicalKey: core.LKMergedMainBranchNote, Required: true},
				{LogicalKey: core.LKMergeCodeReport, Required: false},
				{LogicalKey: core.LKUpstreamArtifactIssue, Required: false},
			}},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "merged_code", Members: speccommon.OutputMembers(result, core.LKMergedCodeSummary, core.LKMergedMainBranch, core.LKMergedMainBranchNote, core.LKMergeCodeReport, core.LKUpstreamArtifactIssue)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_read", Description: "Read registered input artifact content."},
			{Name: "artifact_write", Description: "Write declared merge output artifacts."},
			{Name: "container_git_cherry_pick", Description: "Cherry-pick tested module commits into container_context.repo_dir and return structured merge data."},
		},
		PromptTemplateID: "architect.merge_code.v1",
	}
}
