package front

import (
	core "devflow/internal/agent/core"
	speccommon "devflow/internal/agent/spec/common"
	appcore "devflow/internal/core"
)

func PreviewEditSpec() core.OpSpec {
	return core.OpSpec{
		Role:            "front",
		Op:              "preview_edit",
		RoleDescription: "Front-end preview agent that materializes a preview review manifest for the current tested frontend module.",
		OpDescription:   "Read module_input, code_bag, and tested_module, then write a preview_edit.json manifest that records the current module preview state. This built-in deterministic implementation is intentionally minimal so the full-delivery real smoke can advance to user preview confirmation.",
		InputBags: []core.InputBagSpec{
			{Name: "module_input", Required: true},
			{Name: "code_bag", Required: true},
			{Name: "tested_module", Required: true},
		},
		OutputBags: []core.OutputBagSpec{
			{Name: "preview_edit_bag", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: core.LKPreviewEdit, Required: true}}},
		},
		ModeInputRules: map[string]core.ModeInputRule{
			"normal":  {},
			"repair":  {},
			"rewrite": {},
			"reuse":   {},
		},
		ExpectedOutputs: []core.OutputSpec{
			{
				LogicalKey:  core.LKPreviewEdit,
				ObjectType:  "json",
				ContentType: "application/json; charset=utf-8",
				Encoding:    "utf-8",
				FileName:    "preview_edit.json",
				Description: "Preview edit manifest for the tested frontend module.",
				Required:    true,
			},
		},
		ProducedBagsResolver: func(task core.Task, bundle core.AgentInputBundle, result core.AgentResult) []appcore.ProducedBagManifest {
			return speccommon.NonEmptyProducedBags([]appcore.ProducedBagManifest{
				{Name: "preview_edit_bag", Indexes: speccommon.InputBagIndexes(bundle, "module_input"), Members: speccommon.OutputMembers(result, core.LKPreviewEdit)},
			})
		},
		AllowedTools: []core.ToolSpec{
			{
				Name:        "artifact_write",
				Description: "Write the preview_edit manifest artifact.",
			},
		},
		PromptTemplateID: "front.preview_edit.v1",
	}
}
