package front

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func WriteCodeSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "front",
		Op:              "write_code",
		RoleDescription: "Front-end implementation agent that writes module code inside a prepared real Git worktree, verifies it, and records the module branch result.",
		OpDescription:   "Read container_context, module_spec, coder_task, module_contract, and seed_tests. Before the LLM tool loop begins, the runtime must run a fixed preflight step that prepares the real Git branch and worktree using container_context.repo_dir, container_context.base_branch, module_spec.branch_name, and module_spec.worktree_dir. After that preflight succeeds, the agent must read upstream artifacts with artifact_read, inspect existing files with container_read, modify code and tests under module_spec.worktree_dir with container_write, execute module_spec.test_command or other necessary checks with container_run, commit the module changes with container_git_commit, write coder_branch.json with artifact_write, and finish through task_complete. The implementation strategy must follow module_spec.complexity: low complexity should minimize extra exploration after the required reads, while high complexity should spend more effort understanding existing code and boundaries before implementation. Generated and modified files must stay within module_spec.owned_paths or module_spec.runtime_write_paths, and must avoid module_spec.forbidden_paths. coder_branch must record real execution facts including module_id, container_id, repo_dir, base_branch, base_commit, branch, commit, worktree, changed_files, test_command, result, and test_passed.",
		InputBags: []core.InputBagSpec{
			{Name: "module_input", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKContainerContext, Required: true}, {LogicalKey: core.LKModuleSpec, Required: true}, {LogicalKey: core.LKCoderTask, Required: true}, {LogicalKey: core.LKModuleContract, Required: true}, {LogicalKey: core.LKSeedTests, Required: true}}},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "code_bag", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKCoderBranch, Required: true}, {LogicalKey: core.LKWriteCodeFailure}, {LogicalKey: core.LKUpstreamArtifactIssue}}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKContainerContext,
				Description: "Container execution context including container_id, repo_dir, worktrees_dir, test_runs_dir, base_branch, and branch_prefix.",
			},
			{
				LogicalKey:  core.LKModuleSpec,
				Description: "Current module machine-readable spec including module_id, branch_name, worktree_dir, owned_paths, runtime_write_paths, forbidden_paths, test_command, complexity, and implementation_role.",
			},
			{
				LogicalKey:  core.LKCoderTask,
				Description: "Natural-language task instructions for the current front-end implementation stage.",
			},
			{
				LogicalKey:  core.LKModuleContract,
				Description: "Module contract that defines boundaries, responsibilities, and acceptance constraints.",
			},
			{
				LogicalKey:  core.LKSeedTests,
				Description: "Seed tests supplied by upstream stages.",
			},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRepairInstruction,
						Description: "Instructions describing what should be repaired in the current write_code result.",
					},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKCoderBranch,
				ObjectType:  "json",
				FileName:    "coder_branch.json",
				Description: "Module branch result artifact recording the real module branch, commit, worktree, and verification outcome for downstream stages.",
				Required:    true,
			},
			{
				LogicalKey:  core.LKWriteCodeFailure,
				ObjectType:  "markdown",
				FileName:    "write_code_failure.md",
				Description: "Readable failure artifact emitted when front.write_code cannot complete successfully.",
				Required:    false,
			},
			{
				LogicalKey:  core.LKUpstreamArtifactIssue,
				ObjectType:  "markdown",
				FileName:    "upstream_artifact_issue.md",
				Description: "Readable report emitted when an upstream input artifact is malformed or incomplete for front.write_code.",
				Required:    false,
			},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "code_bag", Indexes: speccommon.InputBagIndexes(bundle, "module_input"), Members: speccommon.OutputMembers(result, core.LKCoderBranch, core.LKWriteCodeFailure, core.LKUpstreamArtifactIssue)},
			})
		},
		PreflightHandlers: []string{"container_git_worktree_prepare"},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_read",
				Description: "Read registered input artifact content.",
			},
			{
				Name:        "container_read",
				Description: "Read files inside the current module worktree.",
			},
			{
				Name:        "container_write",
				Description: "Write files inside the current module worktree.",
			},
			{
				Name:        "container_run",
				Description: "Run shell commands inside the current module worktree.",
			},
			{
				Name:        "container_git_commit",
				Description: "Commit changes from the current module worktree and return the resulting commit.",
			},
			{
				Name:        "artifact_write",
				Description: "Write the declared coder_branch or write_code_failure output artifact.",
			},
			{
				Name:        "task_complete",
				Description: "Return the final AgentResult and finish the task.",
			},
		},
		PromptTemplateID: "front.write_code.v1",
	}
}

func DebugWriteCodeSpec() core.OpSpec {
	spec := WriteCodeSpec()
	spec.Op = "debug_write_code"
	spec.OpDescription = "Repair a front-end module branch after tester.test_code returned kbug. Read container_context, module_spec, coder_task, module_contract, seed_tests, the previous coder_branch when present, and the upstream_artifact_issue or module_test_report that explains the failure. Prepare the same real Git worktree, make the smallest necessary UI/code repair, rerun the relevant checks, commit the fixed module changes, and write a fresh coder_branch.json. The new coder_branch must include module_id, container_id, repo_dir, base_branch, base_commit, branch, commit, worktree, changed_files, test_command, result, and test_passed."
	spec.BaseOptionalInputs = append(spec.BaseOptionalInputs,
		core.InputRequirement{
			LogicalKey:  core.LKCoderBranch,
			Description: "Previous coder branch result that failed downstream validation or tests.",
		},
		core.InputRequirement{
			LogicalKey:  core.LKModuleTestReport,
			Description: "Tester module test report describing the failing checks, when available.",
		},
		core.InputRequirement{
			LogicalKey:  core.LKUpstreamArtifactIssue,
			Description: "Readable upstream artifact issue explaining why tester.test_code could not continue safely.",
		},
	)
	return spec
}
