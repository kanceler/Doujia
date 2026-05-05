package llm

import (
	"fmt"

	"doujia/internal/agent/core"
)

func BuildTools(spec core.OpSpec, handlers core.HandlerRegistryLike) ([]Tool, error) {
	tools := make([]Tool, 0, len(spec.AllowedTools))
	for _, allowed := range spec.AllowedTools {
		handler, ok := handlers.Get(allowed.Name)
		if !ok {
			return nil, fmt.Errorf("allowed tool %q is not registered", allowed.Name)
		}
		toolSpec := handler.ToolSpec()
		if allowed.Description != "" {
			toolSpec.Description = allowed.Description
		}
		if len(allowed.Parameters) > 0 {
			toolSpec.Parameters = allowed.Parameters
		}
		tools = append(tools, Tool{
			Name:        toolSpec.Name,
			Description: toolSpec.Description,
			Parameters:  toolSpec.Parameters,
		})
	}
	return tools, nil
}
