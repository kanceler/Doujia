package ceo

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func WritePlanSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "ceo",
		Op:              "write_plan",
		RoleDescription: "CEO 会话智能体，负责将用户目标整理成可直接进入交付链路的需求文档。",
		OpDescription:   "当输入中已有 requirement 时，优先沿用并标准化该需求文档；否则生成一份简体中文 Markdown 需求文档 requirement_v1.md，内容应明确项目目标、核心功能、体验要求和交付要求。",
		OutputBags: []core.OutputBagSpec{
			{Name: "requirement", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKRequirement, Required: true}}},
		},
		BaseRequiredInputs: []core.InputRequirement{},
		BaseOptionalInputs: []core.InputRequirement{
			{LogicalKey: core.LKRequirement, Description: "用户或上游已经提供的 requirement，可直接整理后沿用。"},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeNormal:  {},
			core.ExecutionModeRewrite: {},
			core.ExecutionModeReuse:   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKRequirement,
				ObjectType:  "markdown",
				FileName:    "requirement_v1.md",
				Description: "CEO 产出的需求文档。",
				Required:    true,
			},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "requirement", Members: speccommon.OutputMembers(result, core.LKRequirement)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_write", Description: "写出需求文档 requirement_v1.md。"},
		},
		PromptTemplateID: "ceo.write_plan.v1",
	}
}

func ReviewPlanSpec() core.OpSpec {
	return core.OpSpec{
		Role:               "ceo",
		Op:                 "review_plan",
		RoleDescription:    "CEO 会话智能体，负责对上游计划类产物做轻量确认，决定流程是否继续。",
		OpDescription:      "读取上游传入的计划或审阅材料后做轻量确认。正常情况下直接通过，返回 result=kok，不写任何输出文件。",
		BaseRequiredInputs: []core.InputRequirement{},
		BaseOptionalInputs: []core.InputRequirement{
			{LogicalKey: core.LKPMPlan, Description: "PM 产出的产品计划。"},
			{LogicalKey: core.LKArchitecturePlan, Description: "架构方案。"},
			{LogicalKey: core.LKRequirement, Description: "原始 requirement。"},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			core.ExecutionModeNormal: {},
			core.ExecutionModeReuse:  {},
		},
		ExpectedOutputs: []core.OutputSpec{},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return nil
		},
		AllowedTools:     []core.ToolSpec{},
		PromptTemplateID: "ceo.review_plan.v1",
	}
}
