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
	output, err := parseModelOutputJSON(raw)
	if err != nil {
		return ModelOutput{}, err
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

func parseModelOutputJSON(raw string) (ModelOutput, error) {
	var output ModelOutput
	trimmed := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(trimmed), &output); err == nil {
		return output, nil
	}
	if extracted, ok := extractJSONObject(trimmed); ok {
		if err := json.Unmarshal([]byte(extracted), &output); err == nil {
			return output, nil
		}
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(trimmed[i:]))
		if err := decoder.Decode(&output); err == nil {
			return output, nil
		}
	}
	return ModelOutput{}, fmt.Errorf("parse model output json: invalid JSON object")
}

func extractJSONObject(raw string) (string, bool) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return "", false
	}
	inString := false
	escaped := false
	depth := 0
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], true
			}
		}
	}
	return "", false
}
