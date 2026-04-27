package tester

import (
	"fmt"
	"strings"

	"devflow/internal/agentengine"
	"devflow/internal/core"
)

func buildPrompt(task core.TaskMetaData, recipe Recipe, docs []agentengine.ArtifactDocument) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString(recipe.SystemPrompt)
	builder.WriteString("\n\n# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("agent_id: %s\n", task.AgentID))
	builder.WriteString(fmt.Sprintf("op: %s\n", task.Op))
	builder.WriteString("\n# Goal\n")
	builder.WriteString(recipe.Description)
	builder.WriteString("\n\n# Inputs\n")
	for _, doc := range docs {
		builder.WriteString("\n---\n")
		builder.WriteString(fmt.Sprintf("source: %s\n", doc.URI))
		builder.WriteString(doc.Content)
		builder.WriteString("\n")
	}
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	if strings.TrimSpace(recipe.OutputSchema) != "" {
		builder.WriteString("\n\n# Output JSON Schema\n")
		builder.WriteString(recipe.OutputSchema)
		builder.WriteString("\n\n# Constraints\n")
		builder.WriteString("1. Return exactly one JSON object.\n")
		builder.WriteString("2. Do not use markdown code fences.\n")
		builder.WriteString("3. artifact_outputs must include exactly one test_data file.\n")
		builder.WriteString("4. filename must not contain path separators.\n")
	}
	return builder.String()
}
