package agentengine

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type ArtifactDocument struct {
	URI     string
	Kind    string
	Content string
	Chunks  []ContextChunk
}

type ContextChunk struct {
	SourceURI string
	Title     string
	Content   string
}

func SplitMarkdown(sourceURI string, raw string) []ContextChunk {
	lines := strings.Split(raw, "\n")
	chunks := make([]ContextChunk, 0)
	currentTitle := "document"
	currentContent := make([]string, 0)
	flush := func() {
		content := strings.TrimSpace(strings.Join(currentContent, "\n"))
		if content == "" {
			return
		}
		chunks = append(chunks, ContextChunk{SourceURI: sourceURI, Title: currentTitle, Content: content})
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			flush()
			currentTitle = strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
			if currentTitle == "" {
				currentTitle = "document"
			}
			currentContent = nil
			continue
		}
		currentContent = append(currentContent, line)
	}
	flush()
	if len(chunks) == 0 && strings.TrimSpace(raw) != "" {
		return []ContextChunk{{SourceURI: sourceURI, Title: "document", Content: strings.TrimSpace(raw)}}
	}
	return chunks
}

func SelectContext(docs []ArtifactDocument, maxChars int) []ContextChunk {
	chunks := make([]ContextChunk, 0)
	used := 0
	for _, doc := range docs {
		for _, chunk := range doc.Chunks {
			cost := len(chunk.Title) + len(chunk.Content)
			if maxChars > 0 && used > 0 && used+cost > maxChars {
				return chunks
			}
			chunks = append(chunks, chunk)
			used += cost
		}
	}
	return chunks
}

func BuildPrompt(task TaskEnvelope, recipe Recipe, chunks []ContextChunk) string {
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
	for _, chunk := range chunks {
		builder.WriteString("\n---\n")
		builder.WriteString(fmt.Sprintf("source: %s\n", chunk.SourceURI))
		builder.WriteString(fmt.Sprintf("section: %s\n", chunk.Title))
		builder.WriteString(chunk.Content)
		builder.WriteString("\n")
	}
	builder.WriteString("\n# Instructions\n")
	builder.WriteString(recipe.UserInstruction)
	builder.WriteString("\n\n# Output JSON Schema\n")
	builder.WriteString(recipe.OutputSchema)
	builder.WriteString("\n\n# Constraints\n")
	builder.WriteString("1. Return exactly one JSON object.\n")
	builder.WriteString("2. Do not use markdown code fences.\n")
	builder.WriteString("3. artifact_outputs must include at least one file.\n")
	builder.WriteString("4. filename must not contain path separators.\n")
	return builder.String()
}

func ParseModelOutput(raw string) (ModelOutput, error) {
	var output ModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return ModelOutput{}, fmt.Errorf("parse model output json: %w", err)
	}
	if strings.TrimSpace(output.Summary) == "" {
		return ModelOutput{}, fmt.Errorf("model output summary is required")
	}
	if len(output.ArtifactOutputs) == 0 {
		return ModelOutput{}, fmt.Errorf("model output artifact_outputs is required")
	}
	for i, file := range output.ArtifactOutputs {
		if strings.TrimSpace(file.Filename) == "" {
			return ModelOutput{}, fmt.Errorf("artifact_outputs[%d].filename is required", i)
		}
		if file.Filename != path.Base(filepath.ToSlash(file.Filename)) {
			return ModelOutput{}, fmt.Errorf("artifact_outputs[%d].filename must not contain path separators", i)
		}
	}
	return output, nil
}
