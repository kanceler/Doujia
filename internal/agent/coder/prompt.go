package coder

import (
	"fmt"
	"strings"

	"devflow/internal/agent/common"
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
	builder.WriteString("6. test_command must be one executable shell command string. Do not include prose, Markdown, explanations, or phrases like \"smoke test covering\" in test_command.\n")
	builder.WriteString("7. Put test explanations in summary, not in test_command.\n")
	builder.WriteString("8. Example report: {\"status\":\"completed\",\"summary\":\"Implemented and verified CLI todo flow.\",\"changed_files\":[\"todo.py\"],\"test_command\":\"python -m compileall todo_app todo.py; python todo.py list\",\"test_passed\":true}\n")
	return builder.String()
}

func writeDoc(builder *strings.Builder, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(fmt.Sprintf("source: %s\n", doc.URI))
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}

func buildDebugPrompt(task core.TaskMetaData, recipe Recipe, inputs debugInputs, branchInfo common.CoderBranchArtifact, testCommand string) string {
	var builder strings.Builder
	builder.WriteString("# Debug Existing Coder Branch\n\n")
	builder.WriteString("# Role\n")
	builder.WriteString(recipe.SystemPrompt)
	builder.WriteString("\n\n# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("agent_id: %s\n", task.AgentID))
	builder.WriteString(fmt.Sprintf("op: %s\n", task.Op))
	builder.WriteString("\n# Goal\n")
	builder.WriteString(recipe.Description)
	builder.WriteString("\n\n# Branch Under Repair\n")
	builder.WriteString(fmt.Sprintf("- repo_dir: %s\n", branchInfo.RepoDir))
	builder.WriteString(fmt.Sprintf("- branch: %s\n", branchInfo.Branch))
	builder.WriteString(fmt.Sprintf("- commit: %s\n", branchInfo.Commit))
	builder.WriteString(fmt.Sprintf("- worktree: %s\n", branchInfo.Worktree))
	builder.WriteString(fmt.Sprintf("- suggested_test_command: %s\n", testCommand))
	builder.WriteString("\n# Inputs\n")
	writeNamedDoc(&builder, "Module Task", inputs.moduleDoc)
	writeNamedDoc(&builder, "Coder Branch Artifact", inputs.branchDoc)
	writeNamedDoc(&builder, "Test Failure Report", inputs.failureDoc)
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	builder.WriteString("\n\n# Runtime Contract\n")
	builder.WriteString("1. You are already inside the existing coder branch worktree.\n")
	builder.WriteString("2. Do not run git checkout, git merge, or git commit.\n")
	builder.WriteString("3. Do not modify the main branch worktree.\n")
	builder.WriteString("4. Reproduce the failure when possible, then fix the root cause.\n")
	builder.WriteString("5. Write a JSON execution report to .devflow/result.json.\n")
	builder.WriteString("6. The report must include status, summary, changed_files, test_command, and test_passed.\n")
	builder.WriteString("7. test_command must be one executable shell command string. Do not include prose, Markdown, explanations, or phrases like \"smoke test covering\" in test_command.\n")
	builder.WriteString("8. Put test explanations in summary, not in test_command.\n")
	return builder.String()
}

func writeNamedDoc(builder *strings.Builder, title string, doc agentengine.ArtifactDocument) {
	builder.WriteString("\n---\n")
	builder.WriteString(title)
	builder.WriteString("\nsource: ")
	builder.WriteString(doc.URI)
	builder.WriteString("\n")
	builder.WriteString(doc.Content)
	builder.WriteString("\n")
}
