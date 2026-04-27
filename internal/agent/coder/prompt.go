package coder

import (
	"fmt"
	"strings"

	"devflow/internal/agentengine"
	"devflow/internal/core"
)

func buildPrompt(task core.TaskMetaData, recipe Recipe, moduleDoc agentengine.ArtifactDocument, branchDoc agentengine.ArtifactDocument) string {
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
	writeDoc(&builder, moduleDoc)
	writeDoc(&builder, branchDoc)
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	builder.WriteString("\n\n# Runtime Contract\n")
	builder.WriteString("1. You are already inside the coder git worktree.\n")
	builder.WriteString("2. Do not run git checkout, git merge, or git commit.\n")
	builder.WriteString("3. Do not modify the main branch worktree.\n")
	builder.WriteString("4. Write a JSON execution report to .devflow/result.json.\n")
	builder.WriteString("5. The report must include status, summary, changed_files, test_command, and test_passed.\n")
	return builder.String()
}

func writeDoc(builder *strings.Builder, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(fmt.Sprintf("source: %s\n", doc.URI))
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}
