package front

import core "devflow/internal/agent/core"

func UserPreviewConfirmSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "front",
		Op:              "user_preview_confirm",
		RoleDescription: "Front-end preview confirmation agent for the current tested frontend module.",
		OpDescription:   "Confirm the preview_edit result. The built-in real-smoke implementation auto-approves with kok so unattended full-delivery smoke can continue; interactive review can replace this op later.",
		InputBags: []core.InputBagSpec{
			{Name: "module_input", Required: true},
			{Name: "preview_edit_bag", Required: true},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal":  {},
			"repair":  {},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs:  []core.OutputSpec{},
		AllowedTools:     []core.ToolSpec{},
		PromptTemplateID: "front.user_preview_confirm.v1",
	}
}
