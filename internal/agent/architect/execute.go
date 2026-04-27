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

func (a *Agent) executeReplan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("architect_replan fallback: artifact store is nil")
		return a.executeReplanFallback(task, architectReplanInputs{})
	}

	inputs, err := a.resolveReplanInputs(ctx, task.ArtifactURIs)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	prompt := buildReplanPrompt(task, inputs)
	a.logStep(fmt.Sprintf("architect_replan llm request start: inputs=%d prompt_chars=%d", len(task.ArtifactURIs), len(prompt)))

	if a.llmClient == nil {
		a.logStep("architect_replan fallback: llm client is nil")
		return a.executeReplanFallback(task, inputs)
	}
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("architect_replan llm request failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architect_replan llm request failed: %w", err)
		}
		return a.executeReplanFallback(task, inputs)
	}
	a.logStep(fmt.Sprintf("architect_replan llm request success: chars=%d", len(raw)))
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("architect_replan parse model output failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("architect_replan parse model output failed: %w", err)
		}
		return a.executeReplanFallback(task, inputs)
	}
	output = addDocumentBasisToModelOutput(output, replanBasisURIs(task.ArtifactURIs, inputs.previousDesignURI))
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, "design", output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write replan artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("architect_replan artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeTestData(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("architect artifact store is nil")
	}
	docs := make([]agentengine.ArtifactDocument, 0, len(task.ArtifactURIs))
	for _, uri := range task.ArtifactURIs {
		uri = filepath.ToSlash(strings.TrimSpace(uri))
		if uri == "" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, uri)
		if err != nil {
			return core.TaskMetaData{}, fmt.Errorf("read artifact %s: %w", uri, err)
		}
		docs = append(docs, agentengine.ArtifactDocument{
			URI:     uri,
			Kind:    agentengine.InferArtifactKind(uri),
			Content: string(content),
		})
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test_data", "architect_test_data.md")
	content := prependDocumentBasis(buildArchitectTestDataDocument(docs), task.ArtifactURIs)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write architect test_data artifact: %w", err)
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
}

func buildArchitectTestDataDocument(docs []agentengine.ArtifactDocument) string {
	var builder strings.Builder
	builder.WriteString("# Architect Test Data\n\n")
	builder.WriteString("## Purpose\n\n")
	builder.WriteString("Architecture-level verification data generated from the current design and split-module outputs.\n\n")
	builder.WriteString("## Source Artifacts\n\n")
	if len(docs) == 0 {
		builder.WriteString("- No source artifacts were provided.\n")
	} else {
		for _, doc := range docs {
			builder.WriteString("- ")
			builder.WriteString(doc.URI)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Architecture Verification Matrix\n\n")
	builder.WriteString("| Area | Data To Check | Expected Result |\n")
	builder.WriteString("| --- | --- | --- |\n")
	builder.WriteString("| module_boundaries | module task ownership and dependencies | responsibilities do not overlap unexpectedly |\n")
	builder.WriteString("| integration_flow | main branch plus coder branches | modules can be merged in declared order |\n")
	builder.WriteString("| state_contracts | shared state and interface notes | implementation preserves architecture contracts |\n")
	builder.WriteString("| failure_paths | missing input, invalid state, failed tests | reports are explicit and actionable |\n")
	return builder.String()
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

type architectReplanInputs struct {
	prdURI            string
	prd               string
	reviewURI         string
	review            string
	previousDesignURI string
	previousDesign    string
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
			return architectReplanInputs{}, fmt.Errorf("read artifact %s: %w", normalized, err)
		}
		switch {
		case isPRDArtifactURI(normalized):
			inputs.prdURI = normalized
			inputs.prd = string(content)
		case isReviewArtifactURI(normalized):
			inputs.reviewURI = normalized
			inputs.review = string(content)
		}
	}
	if strings.TrimSpace(inputs.prd) == "" {
		return architectReplanInputs{}, fmt.Errorf("replan requires prd artifact")
	}
	if strings.TrimSpace(inputs.review) == "" {
		return architectReplanInputs{}, fmt.Errorf("replan requires review artifact")
	}

	previousDesignURI := a.lastDesignArtifactURI()
	if previousDesignURI == "" {
		return inputs, nil
	}
	content, err := a.artifactStore.Read(ctx, previousDesignURI)
	if err != nil {
		a.logStep(fmt.Sprintf("architect_replan previous design read skipped: uri=%s err=%v", previousDesignURI, err))
		return inputs, nil
	}
	inputs.previousDesignURI = previousDesignURI
	inputs.previousDesign = string(content)
	return inputs, nil
}

func (a *Agent) lastDesignArtifactURI() string {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	for i := len(a.taskHistory) - 1; i >= 0; i-- {
		item := a.taskHistory[i]
		if item.Status != core.TaskStatusDone {
			continue
		}
		if !isDesignProducingOp(item.Op) {
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
	case "write_plan", "architecture_generation", "replan":
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

func buildReplanPrompt(task core.TaskMetaData, inputs architectReplanInputs) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are an Architect Agent. 当前任务是 replan，需要根据 PM 最新 PRD、review 指导书和上一版架构设计书生成修订后的架构设计。\n\n")
	builder.WriteString("# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("agent_id: %s\n", task.AgentID))
	builder.WriteString("op: replan\n\n")

	writeReplanPromptDoc(&builder, "PM 最新 PRD", inputs.prdURI, inputs.prd)
	writeReplanPromptDoc(&builder, "review/replan 指导书", inputs.reviewURI, inputs.review)
	if strings.TrimSpace(inputs.previousDesign) == "" {
		builder.WriteString("## 上一版架构设计书\n\n")
		builder.WriteString("未找到可读取的上一版架构设计书，请直接基于 PM 最新 PRD 和 review 指导书生成完整新版架构设计。\n\n")
	} else {
		writeReplanPromptDoc(&builder, "上一版架构设计书", inputs.previousDesignURI, inputs.previousDesign)
	}

	builder.WriteString("# Instructions\n")
	builder.WriteString("1. PM 最新 PRD 是产品事实源头。\n")
	builder.WriteString("2. review/replan 指导书是本轮必须解决的问题。\n")
	builder.WriteString("3. 上一版架构设计书仅作为修订基础；如与 PM PRD 或 review 冲突，以 PM PRD 和 review 为准。\n")
	builder.WriteString("4. 输出必须是一份完整的新架构设计书，不要只输出 diff、补丁或说明。\n")
	builder.WriteString("5. 必须逐条回应 review 指导书中的问题，并把修订落实到模块边界、数据模型、关键流程、接口契约或测试性关注点中。\n")
	builder.WriteString("6. 不要重写 PRD，不要扩写产品叙事，专注技术结构和工程可执行性。\n")
	builder.WriteString("7. 必须覆盖技术目标、运行时模型、模块边界、核心状态/数据模型、关键流程、接口契约、边界/错误处理、测试性关注点和模块拆分建议。\n\n")
	builder.WriteString("# Output JSON Schema\n")
	builder.WriteString(`{"summary":"short summary","artifact_outputs":[{"type":"design","filename":"architecture_v2.md","content":"markdown content"}],"control":[]}`)
	builder.WriteString("\n\n# Constraints\n")
	builder.WriteString("1. Return exactly one JSON object.\n")
	builder.WriteString("2. Do not use markdown code fences.\n")
	builder.WriteString("3. artifact_outputs must include at least one file.\n")
	builder.WriteString("4. filename must not contain path separators.\n")
	return builder.String()
}

func writeReplanPromptDoc(builder *strings.Builder, title, uri, content string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n\nsource: ")
	builder.WriteString(uri)
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(content))
	builder.WriteString("\n\n")
}

func replanBasisURIs(inputURIs []string, previousDesignURI string) []string {
	basis := append([]string(nil), inputURIs...)
	previousDesignURI = strings.TrimSpace(previousDesignURI)
	if previousDesignURI == "" {
		return basis
	}
	for _, uri := range basis {
		if filepath.ToSlash(strings.TrimSpace(uri)) == filepath.ToSlash(previousDesignURI) {
			return basis
		}
	}
	return append(basis, previousDesignURI)
}

func (a *Agent) executeReplanFallback(task core.TaskMetaData, inputs architectReplanInputs) (core.TaskMetaData, error) {
	a.logStep("architect_replan fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "design", "architecture_v2.md")
	content := prependDocumentBasis("# 架构设计（Replan）\n\n## 说明\n\n当前未通过真实 LLM 生成完整 replan 架构细节。\n", replanBasisURIs(task.ArtifactURIs, inputs.previousDesignURI))
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "design", "architecture_v2.md"),
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
