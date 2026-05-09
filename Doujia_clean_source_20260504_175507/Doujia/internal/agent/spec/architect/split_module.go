package architect

import (
	"fmt"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func SplitModuleSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "split_module",
		RoleDescription: "Architect agent that splits the architecture plan into a fixed frontend module plus configurable backend delivery artifacts for coder and tester stages.",
		OpDescription:   "Read architecture_plan and container_context, then use artifact_read and artifact_write to generate module specs, coder tasks, tester tasks, module contracts, and seed tests. By default, create one frontend module and one backend module. If run_delivery_config is provided, use backend_module_count to decide how many backend modules to create. module01 must always own all frontend UI work; module02 and later modules must only own backend work. Each module_spec must include a complexity field whose value is either low or high. After all required outputs are written, call task_complete with the final AgentResult.\n\n" + schema.ModuleSpecPromptContract(),
		InputBags: []core.InputBagSpec{
			{Name: "architecture", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKArchitecturePlan, Required: true},
			}},
			{Name: "container_context", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
			}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKArchitecturePlan,
				Description: "Architecture plan that defines the project structure, delivery boundaries, and module responsibilities.",
			},
			{
				LogicalKey:  core.LKContainerContext,
				Description: "Container execution context that provides repo_dir, worktrees_dir, test_runs_dir, base_branch, and branch_prefix.",
			},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKEnvironmentSpec,
				Description: "Optional runtime environment specification for runtime and default test command context.",
			},
			{
				LogicalKey:  core.LKRunDeliveryConfig,
				Description: "Optional delivery configuration. Currently supports backend_module_count to control backend module count while module01 remains the frontend module.",
			},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRepairInstruction,
						Description: "Instructions describing which split_module outputs need repair.",
					},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{LogicalKey: core.LKModuleSpecs, ObjectType: "json", FileName: "module_specs/module_specs.json", Description: "Combined module specs for the fixed frontend module plus all backend modules, including a complexity field for each module.", Required: true},
		},
		ExpectedOutputsResolver: resolveSplitModuleOutputs,
		OutputBags: []core.OutputBagSpec{
			{Name: "front_module_input", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
			}},
			{Name: "backend_module_input", Required: true, Collection: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
			}},
			{Name: "global_test_input", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
				{LogicalKey: core.LKArchitecturePlan, Required: true},
				{LogicalKey: core.LKModuleSpecs, Required: true},
			}},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			moduleIDs := moduleIDsFromSplitModuleOutputs(result.Outputs)
			bags := make([]appcore.ProducedBagManifest, 0, len(moduleIDs)+1)
			for _, moduleID := range moduleIDs {
				members := append(
					speccommon.InputMembers(bundle, core.LKContainerContext),
					speccommon.OutputMembers(
						result,
						core.ModuleSpecKey(moduleID),
						core.ModuleCoderTaskKey(moduleID),
						core.ModuleTesterTaskKey(moduleID),
						core.ModuleContractKey(moduleID),
						core.ModuleSeedTestsKey(moduleID),
					)...,
				)
				bagName := "backend_module_input"
				if moduleID == "module01" {
					bagName = "front_module_input"
				}
				bags = append(bags, appcore.ProducedBagManifest{
					Name:    bagName,
					Indexes: map[string]string{"module_key": moduleID},
					Members: members,
				})
			}
			bags = append(bags, appcore.ProducedBagManifest{
				Name: "global_test_input",
				Members: append(
					speccommon.InputMembers(bundle, core.LKContainerContext, core.LKArchitecturePlan, core.LKEnvironmentSpec),
					speccommon.OutputMembers(result, core.LKModuleSpecs)...,
				),
			})
			return speccommon.NonEmptyProducedBags(bags)
		},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_read",
				Description: "Read registered input artifact content.",
			},
			{
				Name:        "artifact_write",
				Description: "Write declared output artifacts.",
			},
			{
				Name:        "task_complete",
				Description: "Finish the task by returning the final AgentResult payload.",
			},
		},
		PromptTemplateID: "architect.split_module.v1",
	}
}

func resolveSplitModuleOutputs(bundle core.AgentInputBundle) ([]core.OutputSpec, error) {
	config := schema.RunDeliveryConfig{BackendModuleCount: 1}
	for _, input := range bundle.Inputs {
		if input.LogicalKey != core.LKRunDeliveryConfig || input.Path == "" {
			continue
		}
		loaded, err := schema.ReadRunDeliveryConfigFile(input.Path)
		if err != nil {
			return nil, err
		}
		config = loaded
		break
	}

	outputs := []core.OutputSpec{
		{LogicalKey: core.LKModuleSpecs, ObjectType: "json", FileName: "module_specs/module_specs.json", Description: "Combined module specs for the fixed frontend module plus all backend modules, including a complexity field for each module.", Required: true},
	}
	totalModules := 1 + config.BackendModuleCount
	for i := 1; i <= totalModules; i++ {
		moduleID := fmt.Sprintf("module%02d", i)
		roleLabel := "Backend"
		if i == 1 {
			roleLabel = "Frontend"
		}
		outputs = append(outputs,
			core.OutputSpec{LogicalKey: core.ModuleSpecKey(moduleID), ObjectType: "json", FileName: fmt.Sprintf("module_specs/%s_spec.json", moduleID), Description: roleLabel + " module machine-readable spec. Must include complexity with value low or high.", Required: true},
			core.OutputSpec{LogicalKey: core.ModuleCoderTaskKey(moduleID), ObjectType: "markdown", FileName: fmt.Sprintf("modules/%s/coder_task.md", moduleID), Description: roleLabel + " module coder task instructions.", Required: true},
			core.OutputSpec{LogicalKey: core.ModuleTesterTaskKey(moduleID), ObjectType: "markdown", FileName: fmt.Sprintf("modules/%s/tester_task.md", moduleID), Description: roleLabel + " module tester task instructions.", Required: true},
			core.OutputSpec{LogicalKey: core.ModuleContractKey(moduleID), ObjectType: "json", FileName: fmt.Sprintf("contracts/%s_contract.json", moduleID), Description: roleLabel + " module contract definition.", Required: true},
			core.OutputSpec{LogicalKey: core.ModuleSeedTestsKey(moduleID), ObjectType: "json", FileName: fmt.Sprintf("seed_tests/%s_seed_tests.json", moduleID), Description: roleLabel + " module seed tests.", Required: true},
		)
	}
	return outputs, nil
}

func moduleIDsFromSplitModuleOutputs(outputs []core.AgentOutput) []string {
	seen := make(map[string]bool)
	moduleIDs := make([]string, 0)
	for _, output := range outputs {
		if !strings.HasPrefix(output.LogicalKey, "module") || !strings.HasSuffix(output.LogicalKey, "_spec") {
			continue
		}
		moduleID := strings.TrimSuffix(output.LogicalKey, "_spec")
		if seen[moduleID] {
			continue
		}
		seen[moduleID] = true
		moduleIDs = append(moduleIDs, moduleID)
	}
	sortStrings(moduleIDs)
	return moduleIDs
}

func sortStrings(items []string) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}
