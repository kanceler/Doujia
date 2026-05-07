package llm

import (
	"fmt"

	"devflow/internal/agent/core"
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
		toolSpec.Parameters = sanitizeToolParametersForLLM(toolSpec.Name, toolSpec.Parameters)
		tools = append(tools, Tool{
			Name:        toolSpec.Name,
			Description: toolSpec.Description,
			Parameters:  toolSpec.Parameters,
		})
	}
	return tools, nil
}

func sanitizeToolParametersForLLM(name string, parameters map[string]any) map[string]any {
	if name != "artifact_write" || len(parameters) == 0 {
		return parameters
	}
	copied := copyStringAnyMap(parameters)
	properties, ok := copied["properties"].(map[string]any)
	if !ok {
		return copied
	}
	properties = copyStringAnyMap(properties)
	delete(properties, "content_base64")
	copied["properties"] = properties
	return copied
}

func copyStringAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
