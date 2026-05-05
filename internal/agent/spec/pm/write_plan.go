package pm

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func WritePlanSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "pm",
		Op:              "write_plan",
		RoleDescription: "产品经理智能体，负责将用户需求整理为简洁、清晰、可交付的产品方案。",
		OpDescription:   "读取 requirement，生成面向用户阅读的简体中文产品计划文档 plan_v1.md。计划内容应控制在约 200 字，聚焦项目目标、核心功能、用户价值和交付范围。除非需求中明确提出，否则不要添加里程碑、时间线、排期或预估工作量等章节。",
		InputBags: []core.InputBagSpec{
			{Name: "requirement", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKRequirement, Required: true}}},
		},
		BaseRequiredInputs: []core.InputRequirement{
			{
				LogicalKey:  core.LKRequirement,
				Description: "原始用户需求文档，或上游整理后的标准化需求。",
			},
		},
		BaseOptionalInputs: []core.InputRequirement{},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"repair": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRepairInstruction,
						Description: "修复说明，描述 plan_v1.md 中需要调整或补充的内容。",
					},
				},
			},
			"rewrite": {
				ExtraRequiredInputs: []core.InputRequirement{
					{
						LogicalKey:  core.LKRewriteInstruction,
						Description: "重写说明，描述 plan_v1.md 应如何改写。",
					},
				},
			},
			"reuse": {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKPMPlan,
				ObjectType:  "markdown",
				FileName:    "plan_v1.md",
				Description: "面向用户阅读的产品计划文档，对应输出文件 plan_v1.md。",
				Required:    true,
			},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "product_plan", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKPMPlan, Required: true}}},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "product_plan", Members: speccommon.OutputMembers(result, core.LKPMPlan)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_read",
				Description: "读取输入产物内容。",
			},
			{
				Name:        "artifact_write",
				Description: "写入输出产物内容。",
			},
			{
				Name:        "task_complete",
				Description: "返回最终 AgentResult 并结束当前任务。",
			},
		},
		PromptTemplateID: "pm.write_plan.v1",
	}
}
