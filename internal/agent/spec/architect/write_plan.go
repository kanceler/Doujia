package architect

import (
	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func WritePlanSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "architect",
		Op:              "write_plan",
		RoleDescription: "架构师智能体，负责将产品计划转换为架构设计产物。",
		OpDescription:   "根据 PM 计划生成面向用户阅读的架构方案 architecture_v1.md，以及机器可读的运行环境说明 environment_spec.json。架构方案应聚焦技术组成、页面与模块结构、内容与数据组织方式、运行约束等内容。默认假设容器导出已经是既定交付方式。除非需求中明确提出，否则不要在 architecture_v1.md 中加入任何部署相关内容，包括部署目标、部署方案、部署计划、交付模式讨论、容器导出讨论、托管讨论、静态导出讨论、服务器选型或任何部署相关标题；默认容器导出这一事实只体现在 environment_spec.json 中。\n\n" + schema.EnvironmentSpecPromptContract(),
		InputBags: []core.InputBagSpec{
			{Name: "product_plan", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKPMPlan, Required: true}}},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "architecture", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKArchitecturePlan, Required: true}, {LogicalKey: core.LKEnvironmentSpec, Required: true}}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKPMPlan,
				Description: "PM 产出的产品计划，说明项目目标、核心功能、用户价值和交付范围。",
			},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKRequirement,
				Description: "原始用户需求，用于补充项目上下文。",
			},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRepairInstruction,
						Description: "修复说明，描述上一版架构产物中需要修正的内容。",
					},
				},
			},
			"rewrite": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRewriteInstruction,
						Description: "重写说明，描述上一版输出应如何改写。",
					},
				},
			},
			"reuse": {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKArchitecturePlan,
				ObjectType:  "markdown",
				FileName:    "architecture_v1.md",
				Description: "面向用户阅读的架构方案，覆盖定位、技术组成、运行环境和交付结果。",
				Required:    true,
			},
			{
				LogicalKey:  core.LKEnvironmentSpec,
				ObjectType:  "json",
				FileName:    "environment_spec.json",
				Description: "机器可读的运行环境说明，包含 runtime、image、package_manager、初始化提示和默认测试命令等信息。",
				Required:    true,
			},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "architecture", Members: speccommon.OutputMembers(result, core.LKArchitecturePlan, core.LKEnvironmentSpec)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_read",
				Description: "读取已登记的输入产物内容。",
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
		PromptTemplateID: "architect.write_plan.v1",
	}
}
