package architect

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

type architectReplanInputs struct {
	prdURI            string
	prd               string
	reviewURI         string
	review            string
	previousDesignURI string
	previousDesign    string
}

func (a *Agent) executeReplan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return a.executeReplanFallback(task, architectReplanInputs{})
	}
	inputs, err := a.resolveReplanInputs(ctx, task.ArtifactURIs)
	if err != nil {
		a.logStep(fmt.Sprintf("architect_replan fallback: %v", err))
		return a.executeReplanFallback(task, inputs)
	}
	if llm.IsNoop(a.llmClient) {
		return a.executeReplanFallback(task, inputs)
	}

	prompt := buildArchitectReplanPrompt(task, inputs)
	a.logStep(fmt.Sprintf("architect_replan llm request start: prompt_chars=%d", len(prompt)))
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("architect_replan llm request failed: %w", err)
	}
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("architect_replan parse model output failed: %w", err)
	}
	output = addDocumentBasisToModelOutput(output, replanBasisURIs(task.ArtifactURIs, inputs.previousDesignURI))
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, "design", output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write replan artifact outputs: %w", err)
	}
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) resolveReplanInputs(ctx context.Context, uris []string) (architectReplanInputs, error) {
	var inputs architectReplanInputs
	for _, uri := range uris {
		normalized := filepath.ToSlash(strings.TrimSpace(uri))
		if normalized == "" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, normalized)
		if err != nil {
			return inputs, fmt.Errorf("read artifact %s: %w", normalized, err)
		}
		switch {
		case isPRDArtifactURI(normalized):
			inputs.prdURI = normalized
			inputs.prd = string(content)
		case isReviewArtifactURI(normalized):
			inputs.reviewURI = normalized
			inputs.review = string(content)
		case isDesignArtifactURI(normalized):
			inputs.previousDesignURI = normalized
			inputs.previousDesign = string(content)
		}
	}
	if strings.TrimSpace(inputs.previousDesignURI) == "" {
		if previous := a.lastDesignArtifactURI(); previous != "" {
			if content, err := a.artifactStore.Read(ctx, previous); err == nil {
				inputs.previousDesignURI = previous
				inputs.previousDesign = string(content)
			}
		}
	}
	if strings.TrimSpace(inputs.prd) == "" {
		return inputs, fmt.Errorf("replan requires prd artifact")
	}
	if strings.TrimSpace(inputs.review) == "" {
		return inputs, fmt.Errorf("replan requires review artifact")
	}
	return inputs, nil
}

func (a *Agent) lastDesignArtifactURI() string {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	for i := len(a.taskHistory) - 1; i >= 0; i-- {
		item := a.taskHistory[i]
		if item.Status != core.TaskStatusDone || !isDesignProducingOp(item.Op) {
			continue
		}
		for j := len(item.OutputArtifactURIs) - 1; j >= 0; j-- {
			uri := filepath.ToSlash(strings.TrimSpace(item.OutputArtifactURIs[j]))
			if isDesignArtifactURI(uri) {
				return uri
			}
		}
	}
	return ""
}

func isDesignProducingOp(op string) bool {
	switch strings.TrimSpace(op) {
	case core.TaskOpWritePlan, "architecture_generation", core.TaskOpRewrite, core.TaskOpReplan:
		return true
	default:
		return false
	}
}

func isPRDArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/prd/") || strings.Contains(lower, "/artifacts/plan/")
}

func isReviewArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/review/") || strings.Contains(lower, "replan_instruction")
}

func isDesignArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/design/") || strings.Contains(lower, "/artifacts/architecture/")
}

func buildArchitectReplanPrompt(task core.TaskMetaData, inputs architectReplanInputs) string {
	var builder strings.Builder
	builder.WriteString("# Architect Replan\n\n")
	builder.WriteString("You are the DevFlow Architect Agent. Revise the technical design from the latest PRD, review instruction, and previous design.\n\n")
	builder.WriteString(fmt.Sprintf("- task_id: %s\n- agent_id: %s\n- op: %s\n\n", task.TaskID, task.AgentID, task.Op))
	writeReplanPromptDoc(&builder, "Latest PRD", inputs.prdURI, inputs.prd)
	writeReplanPromptDoc(&builder, "Review Instruction", inputs.reviewURI, inputs.review)
	writeReplanPromptDoc(&builder, "Previous Design", inputs.previousDesignURI, inputs.previousDesign)
	builder.WriteString("## Instructions\n\n")
	builder.WriteString("- Produce a complete replacement architecture document, not a diff.\n")
	builder.WriteString("- Resolve every review instruction explicitly.\n")
	builder.WriteString("- Include technical goals, runtime model, module boundaries, state/data model, key flows, interfaces, error handling, testability notes, and module split guidance.\n\n")
	builder.WriteString("## Output JSON Schema\n\n")
	builder.WriteString(`{"summary":"short summary","artifact_outputs":[{"type":"design","filename":"architecture_v2.md","content":"markdown content"}],"control":[]}`)
	return builder.String()
}

func writeReplanPromptDoc(builder *strings.Builder, title, uri, content string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n\nsource: ")
	builder.WriteString(strings.TrimSpace(uri))
	builder.WriteString("\n\n")
	if strings.TrimSpace(content) == "" {
		builder.WriteString("Not provided.")
	} else {
		builder.WriteString(strings.TrimSpace(content))
	}
	builder.WriteString("\n\n")
}

func replanBasisURIs(inputURIs []string, previousDesignURI string) []string {
	basis := append([]string(nil), inputURIs...)
	previousDesignURI = filepath.ToSlash(strings.TrimSpace(previousDesignURI))
	if previousDesignURI == "" {
		return basis
	}
	for _, uri := range basis {
		if filepath.ToSlash(strings.TrimSpace(uri)) == previousDesignURI {
			return basis
		}
	}
	return append(basis, previousDesignURI)
}

func (a *Agent) executeReplanFallback(task core.TaskMetaData, inputs architectReplanInputs) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "design", "architecture_v2.md")
	content := prependDocumentBasis("# Architecture Replan\n\nFallback architecture replan output is ready.\n", replanBasisURIs(task.ArtifactURIs, inputs.previousDesignURI))
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "design", "architecture_v2.md"), content)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func addDocumentBasisToModelOutput(output agentengine.ModelOutput, artifactURIs []string) agentengine.ModelOutput {
	for i := range output.ArtifactOutputs {
		output.ArtifactOutputs[i].Content = prependDocumentBasis(output.ArtifactOutputs[i].Content, artifactURIs)
	}
	return output
}

func prependDocumentBasis(content string, artifactURIs []string) string {
	if strings.HasPrefix(strings.TrimLeft(content, "\r\n\t "), "# Document Basis") {
		return content
	}
	var builder strings.Builder
	builder.WriteString("# Document Basis\n\n")
	items := normalizedBasisItems(artifactURIs)
	if len(items) == 0 {
		builder.WriteString("No explicit input artifact was provided.\n\n")
	} else {
		for _, item := range items {
			builder.WriteString("- ")
			builder.WriteString(item)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(strings.TrimLeft(content, "\r\n"))
	return builder.String()
}

func normalizedBasisItems(artifactURIs []string) []string {
	items := make([]string, 0, len(artifactURIs))
	for _, uri := range artifactURIs {
		uri = strings.TrimSpace(filepath.ToSlash(uri))
		if uri == "" {
			continue
		}
		kind := agentengine.InferArtifactKind(uri)
		if kind == "unknown" {
			items = append(items, uri)
		} else {
			items = append(items, kind+": "+uri)
		}
	}
	return items
}
