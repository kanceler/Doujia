package architect

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

func (a *Agent) executeArchitectureGeneration(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("architecture_generation fallback: artifact store is nil")
		return a.executeArchitectureFallback(task)
	}

	env, err := agentengine.ParseTask(task)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	recipe := architectWritePlanRecipe(env.Op)
	if err := agentengine.ValidateTaskInputs(env, recipe); err != nil {
		return core.TaskMetaData{}, err
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("resolve artifacts: %w", err)
	}
	chunks := agentengine.SelectContext(docs, recipe.MaxContextChars)
	prompt := agentengine.BuildPrompt(env, recipe, chunks)
	a.logStep(fmt.Sprintf("architecture_generation llm request start: inputs=%d prompt_chars=%d", len(env.InputArtifacts), len(prompt)))

	if a.llmClient == nil {
		a.logStep("architecture_generation fallback: llm client is nil")
		return a.executeArchitectureFallback(task)
	}
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("architecture_generation llm request failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architecture_generation llm request failed: %w", err)
		}
		return a.executeArchitectureFallback(task)
	}
	a.logStep(fmt.Sprintf("architecture_generation llm request success: chars=%d", len(raw)))
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("architecture_generation parse model output failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architecture_generation parse model output failed: %w", err)
		}
		return a.executeArchitectureFallback(task)
	}
	output = addDocumentBasisToModelOutput(output, task.ArtifactURIs)
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, recipe.OutputKind, output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("architecture_generation artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeArchitectureFallback(task core.TaskMetaData) (core.TaskMetaData, error) {
	a.logStep("architecture_generation fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "design", "architecture_v1.md")
	content := prependDocumentBasis("# 架构设计\n\n## 说明\n\n当前未通过真实 LLM 生成完整架构细节。\n", task.ArtifactURIs)
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "design", "architecture_v1.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func architectWritePlanRecipe(op string) agentengine.Recipe {
	return agentengine.Recipe{
		Op:                 op,
		Description:        "Generate a technical architecture design from PM PRD artifacts.",
		OutputKind:         "design",
		RequiredInputKinds: []string{"prd"},
		SystemPrompt:       "You are an Architect Agent. Your job is to convert product requirements into a technical architecture design. Do not rewrite user stories or produce another PRD.",
		UserInstruction: strings.Join([]string{
			"Produce a concise but concrete architecture document for engineers and testers.",
			"Focus on technical structure, not product narration.",
			"Must include: technical goals, runtime model, module boundaries, core state/data model, key flows, module interface contracts, boundary/error handling, testability concerns, and module-splitting suggestions.",
			"For each module, describe responsibility, inputs, outputs, and dependencies.",
			"Do not invent technology choices that are not implied by the input. If a choice is unknown, state it as undecided.",
		}, " "),
		OutputSchema:    `{"summary":"short summary","artifact_outputs":[{"type":"design","filename":"architecture_v1.md","content":"markdown content"}],"control":[]}`,
		MaxContextChars: 12000,
	}
}

func addDocumentBasisToModelOutput(output agentengine.ModelOutput, artifactURIs []string) agentengine.ModelOutput {
	for i := range output.ArtifactOutputs {
		output.ArtifactOutputs[i].Content = prependDocumentBasis(output.ArtifactOutputs[i].Content, artifactURIs)
	}
	return output
}

func prependDocumentBasis(content string, artifactURIs []string) string {
	if strings.HasPrefix(strings.TrimLeft(content, "\r\n\t "), "# 文档依据") {
		return content
	}
	var builder strings.Builder
	builder.WriteString("# 文档依据\n\n")
	items := normalizedBasisItems(artifactURIs)
	if len(items) == 0 {
		builder.WriteString("未提供明确输入依据。\n\n")
	} else {
		builder.WriteString("本文档基于以下输入生成：\n\n")
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
			continue
		}
		items = append(items, kind+": "+uri)
	}
	return items
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "ArchitectAgent", message)
	}
}
