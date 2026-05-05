package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"doujia/internal/agent/core"
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
			"required":             []any{"result", "outputs"},
			"properties": map[string]any{
				"result": map[string]any{
					"type":        "string",
					"description": "Final task result. Use kok for success and kfail for failure.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Short human-readable completion summary.",
				},
				"outputs": map[string]any{
					"type":        "array",
					"description": "Declared outputs, including logical_key, object_type, status, and optional path or artifact_version_id.",
					"items": map[string]any{
						"type": "object",
					},
				},
				"errors": map[string]any{
					"type":        "array",
					"description": "Optional final errors. Only use this when result is kfail.",
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
	if result != "kok" && result != "kfail" {
		return core.HandlerResponse{}, fmt.Errorf("result must be kok or kfail")
	}

	rawOutputs, ok := req.Args["outputs"]
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("missing %q arg", "outputs")
	}
	if _, ok := rawOutputs.([]any); !ok {
		if _, ok := rawOutputs.([]map[string]any); !ok {
			return core.HandlerResponse{}, fmt.Errorf("arg %q must be an array", "outputs")
		}
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
