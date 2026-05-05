package architect

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func CreateContainerSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "create_container",
		RoleDescription: "架构师智能体，负责根据环境输入准备可运行的容器上下文。",
		OpDescription:   "根据 environment_spec.json 和可选的 runtime_contract 创建或模拟生成 container_context.json；当 runtime_contract 缺失时，默认使用 /workspace 目录布局。",
		InputBags: []core.InputBagSpec{
			{Name: "architecture", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKEnvironmentSpec, Required: true},
				{LogicalKey: core.LKArchitecturePlan, Required: false},
			}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKEnvironmentSpec,
				Description: "项目的运行环境说明。",
			},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKRuntimeContract,
				Description: "可选的运行时目录契约覆盖项。",
			},
			{
				LogicalKey:  core.LKArchitecturePlan,
				Description: "可选的架构方案，用于补充上下文。",
			},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRepairInstruction,
						Description: "修复说明，描述上一版容器准备结果应如何修正。",
					},
				},
			},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKContainerContext,
				ObjectType:  "json",
				FileName:    "container_context.json",
				Description: "容器运行上下文，包含标识信息、工作目录、运行时信息和就绪状态。",
				Required:    true,
			},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "container_context", Required: true, Members: []core.BagMemberRequirement{
				{LogicalKey: core.LKContainerContext, Required: true},
				{LogicalKey: core.LKArchitecturePlan, Required: false},
				{LogicalKey: core.LKEnvironmentSpec, Required: false},
			}},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			members := append(
				speccommon.InputMembers(bundle, core.LKArchitecturePlan, core.LKEnvironmentSpec),
				speccommon.OutputMembers(result, core.LKContainerContext)...,
			)
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "container_context", Members: members},
			})
		},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_read",
				Description: "读取已登记的输入产物内容。",
			},
			{
				Name:        "container_create",
				Description: "根据 environment_spec 和可选的 runtime_contract 构建 container_context 内容。",
			},
			{
				Name:        "artifact_write",
				Description: "写入已声明的输出产物。",
			},
			{
				Name:        "task_complete",
				Description: "返回最终 AgentResult 并结束当前任务。",
			},
		},
		PromptTemplateID: "architect.create_container.v1",
	}
}
