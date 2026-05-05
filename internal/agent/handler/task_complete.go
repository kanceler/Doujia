package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"devflow/internal/agent/core"
	appcore "devflow/internal/core"
)

type TaskCompleteHandler struct{}

func NewTaskCompleteHandler() *TaskCompleteHandler {
	return &TaskCompleteHandler{}
}

func (h *TaskCompleteHandler) Name() string {
	return "task_complete"
}

func (h *TaskCompleteHandler) Description() string {
	return "Return the final AgentResult and end the current tool-loop task."
}

func (h *TaskCompleteHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        "task_complete",
		Description: h.Description(),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"result"},
			"properties": map[string]any{
				"result": map[string]any{
					"type":        "string",
					"description": "Final task result. Use kok for success; kfail, kbug, krewrite, kreplan, or kcontrol_invalid for non-success outcomes.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Short human-readable completion summary.",
				},
				"outputs": map[string]any{
					"type":        "array",
					"description": "Optional declared outputs, including logical_key, object_type, status, and optional path or artifact_version_id.",
					"items": map[string]any{
						"type": "object",
					},
				},
				"errors": map[string]any{
					"type":        "array",
					"description": "Optional final errors. Use this when result is not kok.",
					"items": map[string]any{
						"type": "object",
					},
				},
				"control": map[string]any{
					"type":        "array",
					"description": "Optional runtime controls such as start_pipeline.",
					"items": map[string]any{
						"type": "object",
					},
				},
				"controls": map[string]any{
					"type":        "array",
					"description": "Optional plural alias for control.",
					"items": map[string]any{
						"type": "object",
					},
				},
			},
		},
	}
}

func (h *TaskCompleteHandler) Handle(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	result, err := requiredStringArg(req.Args, "result")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if !isSupportedTaskResult(result) {
		return core.HandlerResponse{}, fmt.Errorf("result must be one of kok, kfail, kbug, krewrite, kreplan, or kcontrol_invalid")
	}

	if rawOutputs, ok := req.Args["outputs"]; ok {
		if _, ok := rawOutputs.([]any); !ok {
			if _, ok := rawOutputs.([]map[string]any); !ok {
				return core.HandlerResponse{}, fmt.Errorf("arg %q must be an array", "outputs")
			}
		}
	} else {
		req.Args["outputs"] = []any{}
	}

	payload, err := json.Marshal(req.Args)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return core.HandlerResponse{}, err
	}
	return core.HandlerResponse{Data: decoded}, nil
}

func isSupportedTaskResult(result string) bool {
	switch appcore.TaskResultCode(result) {
	case appcore.TaskResultCodeOK,
		appcore.TaskResultCodeFail,
		appcore.TaskResultCodeBug,
		appcore.TaskResultCodeRewrite,
		appcore.TaskResultCodeReplan,
		appcore.TaskResultCodeControlInvalid:
		return true
	default:
		return false
	}
}
