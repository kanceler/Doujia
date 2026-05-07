package pm

import "devflow/internal/agent/core"

func ReviewPlanSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "pm",
		Op:              "review_plan",
		RoleDescription: "产品经理智能体，负责对架构师 write_plan 结果做宽松审阅，判断产物是否达到正常可流转状态。",
		OpDescription:   "读取 architecture_plan 和 environment_spec，对 architect.write_plan 的结果做宽松审阅。只要架构方案看起来是正常输出、environment_spec 结构有效，就通过。通过时返回 result=kok 且不要写任何输出文件。只有在明显不合格时才返回 kfail，并写出 review_plan_failure.md 简要说明失败原因。",
		BaseRequiredInputs: []core.InputRequirement{
			{LogicalKey: core.LKArchitecturePlan, Description: "架构师生成的 architecture_v1.md。"},
			{LogicalKey: core.LKEnvironmentSpec, Description: "架构师生成的 environment_spec.json。"},
		},
		BaseOptionalInputs: []core.InputRequirement{
			{LogicalKey: core.LKPMPlan, Description: "上游 PM 计划，可用于辅助理解上下文。"},
			{LogicalKey: core.LKRequirement, Description: "原始用户需求，可用于辅助理解上下文。"},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal": {},
			"reuse":  {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKReviewPlanFailure,
				ObjectType:  "markdown",
				FileName:    "review_plan_failure.md",
				Description: "当审阅未通过时产出的失败说明 Markdown。",
				Required:    false,
			},
		},
		AllowedTools: []core.ToolSpec{
			{Name: "artifact_write", Description: "在审阅失败时写出 review_plan_failure.md。"},
		},
		PromptTemplateID: "pm.review_plan.v1",
	}
}
