package pm

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

const resultCodeReplan core.TaskResultCode = "replan"

func (a *Agent) executeWritePlan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("pm_write_plan fallback: artifact store is nil")
		return a.executeWritePlanFallback(task)
	}

	recipeTask := task
	if recipeTask.Op == "write_plan" {
		recipeTask.Op = "pm_write_plan"
	}
	env, err := agentengine.ParseTask(recipeTask)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	recipe, err := agentengine.GetRecipe(env.Op)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	if err := agentengine.ValidateTaskInputs(env, recipe); err != nil {
		return core.TaskMetaData{}, err
	}
	docs, err := agentengine.ResolveArtifacts(ctx, a.artifactStore, env.InputArtifacts)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("resolve artifacts: %w", err)
	}
	chunks := agentengine.SelectContext(docs, recipe.MaxContextChars)
	prompt := agentengine.BuildPrompt(env, recipe, chunks)
	a.logStep(fmt.Sprintf("pm_write_plan llm request start: inputs=%d prompt_chars=%d", len(env.InputArtifacts), len(prompt)))

	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_write_plan llm request failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("pm_write_plan llm request failed: %w", err)
		}
		return a.executeWritePlanFallback(task)
	}
	a.logStep(fmt.Sprintf("pm_write_plan llm request success: chars=%d", len(raw)))
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_write_plan parse model output failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("pm_write_plan parse model output failed: %w", err)
		}
		return a.executeWritePlanFallback(task)
	}
	output = addDocumentBasisToModelOutput(output, task.ArtifactURIs)
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, recipe.OutputKind, output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("pm_write_plan artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeReplan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("pm_replan fallback: artifact store is nil")
		return a.executeReplanFallback(task, replanInputs{})
	}

	inputs, err := a.resolveReplanInputs(ctx, task.ArtifactURIs)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	prompt := buildReplanPrompt(task, inputs)
	a.logStep(fmt.Sprintf("pm_replan llm request start: inputs=%d prompt_chars=%d", len(task.ArtifactURIs), len(prompt)))

	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_replan llm request failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("pm_replan llm request failed: %w", err)
		}
		return a.executeReplanFallback(task, inputs)
	}
	a.logStep(fmt.Sprintf("pm_replan llm request success: chars=%d", len(raw)))
	output, err := agentengine.ParseModelOutput(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_replan parse model output failed: %v", err))
		if !llm.IsNoop(a.llmClient) {
			return core.TaskMetaData{}, fmt.Errorf("pm_replan parse model output failed: %w", err)
		}
		return a.executeReplanFallback(task, inputs)
	}
	output = addDocumentBasisToModelOutput(output, replanBasisURIs(task.ArtifactURIs, inputs.previousPlanURI))
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, "prd", output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write replan artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("pm_replan artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeReviewPlan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	docs, err := a.resolveReviewPlanInputs(ctx, task.ArtifactURIs)
	if err != nil {
		return core.TaskMetaData{}, err
	}

	missing := reviewPlanMissingItems(docs.prd, docs.design)
	if len(missing) == 0 {
		return core.TaskMetaData{
			Direction: core.TaskDirectionFeedback,
			RunID:     task.RunID,
			TaskID:    task.TaskID,
			ParentID:  task.ParentID,
			DependsOn: task.DependsOn,
			AgentID:   task.AgentID,
			Op:        task.Op,
			Result:    core.TaskResultCodeOK,
		}, nil
	}

	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "review", "replan_instruction.md")
	content := prependDocumentBasis(buildReviewPlanInstruction(missing), task.ArtifactURIs)
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return core.TaskMetaData{
			Direction:    core.TaskDirectionFeedback,
			RunID:        task.RunID,
			TaskID:       task.TaskID,
			ParentID:     task.ParentID,
			DependsOn:    task.DependsOn,
			AgentID:      task.AgentID,
			Op:           task.Op,
			ArtifactURIs: []string{outputURI},
			Result:       resultCodeReplan,
		}, nil
	}

	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "review", "replan_instruction.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        task.RunID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      task.AgentID,
		Op:           task.Op,
		ArtifactURIs: []string{output},
		Result:       resultCodeReplan,
	}, nil
}

type reviewPlanDocs struct {
	prd    string
	design string
}

type replanInputs struct {
	requirementURI  string
	requirement     string
	reviewURI       string
	review          string
	previousPlanURI string
	previousPlan    string
}

func (a *Agent) resolveReplanInputs(ctx context.Context, uris []string) (replanInputs, error) {
	var inputs replanInputs
	for _, uri := range uris {
		normalized := filepath.ToSlash(strings.TrimSpace(uri))
		if normalized == "" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, normalized)
		if err != nil {
			return replanInputs{}, fmt.Errorf("read artifact %s: %w", normalized, err)
		}
		switch {
		case isRequirementArtifactURI(normalized):
			inputs.requirementURI = normalized
			inputs.requirement = string(content)
		case isReviewArtifactURI(normalized):
			inputs.reviewURI = normalized
			inputs.review = string(content)
		}
	}
	if strings.TrimSpace(inputs.requirement) == "" {
		return replanInputs{}, fmt.Errorf("replan requires requirement artifact")
	}
	if strings.TrimSpace(inputs.review) == "" {
		return replanInputs{}, fmt.Errorf("replan requires review artifact")
	}

	previousPlanURI := a.lastPlanArtifactURI()
	if previousPlanURI == "" {
		return inputs, nil
	}
	content, err := a.artifactStore.Read(ctx, previousPlanURI)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_replan previous plan read skipped: uri=%s err=%v", previousPlanURI, err))
		return inputs, nil
	}
	inputs.previousPlanURI = previousPlanURI
	inputs.previousPlan = string(content)
	return inputs, nil
}

func (a *Agent) lastPlanArtifactURI() string {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	for i := len(a.taskHistory) - 1; i >= 0; i-- {
		item := a.taskHistory[i]
		if item.Status != core.TaskStatusDone {
			continue
		}
		if !isPlanProducingOp(item.Op) {
			continue
		}
		for j := len(item.OutputArtifactURIs) - 1; j >= 0; j-- {
			uri := filepath.ToSlash(strings.TrimSpace(item.OutputArtifactURIs[j]))
			if isPlanArtifactURI(uri) {
				return uri
			}
		}
	}
	return ""
}

func isPlanProducingOp(op string) bool {
	switch strings.TrimSpace(op) {
	case "write_plan", "pm_write_plan", "replan":
		return true
	default:
		return false
	}
}

func isRequirementArtifactURI(uri string) bool {
	return strings.Contains(strings.ToLower(filepath.ToSlash(uri)), "/artifacts/requirement/")
}

func isReviewArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/review/") || strings.Contains(lower, "replan_instruction")
}

func isPlanArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/prd/") || strings.Contains(lower, "/artifacts/plan/")
}

func buildReplanPrompt(task core.TaskMetaData, inputs replanInputs) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are a PM Agent. 当前任务是 replan，需要根据 CEO 原始需求、CEO review 指导书和上一版 PM plan 生成修订后的产品计划。\n\n")
	builder.WriteString("# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\n", task.TaskID))
	builder.WriteString(fmt.Sprintf("agent_id: %s\n", task.AgentID))
	builder.WriteString("op: replan\n\n")

	writePromptDoc(&builder, "CEO 原始需求书", inputs.requirementURI, inputs.requirement)
	writePromptDoc(&builder, "CEO review/replan 指导书", inputs.reviewURI, inputs.review)
	if strings.TrimSpace(inputs.previousPlan) == "" {
		builder.WriteString("## 上一版 PM plan\n\n")
		builder.WriteString("未找到可读取的上一版 PM plan，请直接基于 CEO 原始需求和 review 指导书生成完整新版产品计划。\n\n")
	} else {
		writePromptDoc(&builder, "上一版 PM plan", inputs.previousPlanURI, inputs.previousPlan)
	}

	builder.WriteString("# Instructions\n")
	builder.WriteString("1. CEO 原始需求书是事实源头。\n")
	builder.WriteString("2. CEO review/replan 指导书是本轮必须解决的问题。\n")
	builder.WriteString("3. 上一版 PM plan 仅作为修订基础；如与 CEO 原始需求或 review 冲突，以 CEO 原始需求和 review 为准。\n")
	builder.WriteString("4. 输出必须是一份完整的新产品计划/PRD，不要只输出 diff、补丁或说明。\n")
	builder.WriteString("5. 必须逐条回应 review 指导书中的问题，并把修订落实到后续需求、验收标准或范围约束中。\n")
	builder.WriteString("6. 内容应继续面向架构师消费，覆盖目标、用户场景、功能需求、非功能约束、风险提示和验收标准。\n\n")
	builder.WriteString("# Output JSON Schema\n")
	builder.WriteString(`{"summary":"short summary","artifact_outputs":[{"type":"prd","filename":"plan_v2.md","content":"markdown content"}],"control":[]}`)
	builder.WriteString("\n\n# Constraints\n")
	builder.WriteString("1. Return exactly one JSON object.\n")
	builder.WriteString("2. Do not use markdown code fences.\n")
	builder.WriteString("3. artifact_outputs must include at least one file.\n")
	builder.WriteString("4. filename must not contain path separators.\n")
	return builder.String()
}

func writePromptDoc(builder *strings.Builder, title, uri, content string) {
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n\nsource: ")
	builder.WriteString(uri)
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(content))
	builder.WriteString("\n\n")
}

func replanBasisURIs(inputURIs []string, previousPlanURI string) []string {
	basis := append([]string(nil), inputURIs...)
	previousPlanURI = strings.TrimSpace(previousPlanURI)
	if previousPlanURI == "" {
		return basis
	}
	for _, uri := range basis {
		if filepath.ToSlash(strings.TrimSpace(uri)) == filepath.ToSlash(previousPlanURI) {
			return basis
		}
	}
	return append(basis, previousPlanURI)
}

func (a *Agent) resolveReviewPlanInputs(ctx context.Context, uris []string) (reviewPlanDocs, error) {
	if a.artifactStore == nil {
		return reviewPlanDocs{}, fmt.Errorf("artifact store is required for review_plan")
	}
	var docs reviewPlanDocs
	for _, uri := range uris {
		content, err := a.artifactStore.Read(ctx, uri)
		if err != nil {
			return reviewPlanDocs{}, fmt.Errorf("read artifact %s: %w", uri, err)
		}
		switch agentengine.InferArtifactKind(uri) {
		case "prd":
			docs.prd = string(content)
		case "design":
			docs.design = string(content)
		}
	}
	if strings.TrimSpace(docs.prd) == "" {
		return reviewPlanDocs{}, fmt.Errorf("review_plan requires prd artifact")
	}
	if strings.TrimSpace(docs.design) == "" {
		return reviewPlanDocs{}, fmt.Errorf("review_plan requires design artifact")
	}
	return docs, nil
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

func reviewPlanMissingItems(prd, design string) []string {
	lowerPRD := strings.ToLower(prd)
	lowerDesign := strings.ToLower(design)
	checks := []struct {
		name        string
		prdTerms    []string
		designTerms []string
	}{
		{
			name:        "开始游戏流程",
			prdTerms:    []string{"开始游戏", "start game"},
			designTerms: []string{"开始", "start", "gameloop", "game loop"},
		},
		{
			name:        "用户输入与移动控制",
			prdTerms:    []string{"控制", "移动", "方向键", "input"},
			designTerms: []string{"控制", "移动", "input", "controller"},
		},
		{
			name:        "食物生成与吃食物后的状态变化",
			prdTerms:    []string{"食物", "food"},
			designTerms: []string{"食物", "food"},
		},
		{
			name:        "分数管理与分数展示",
			prdTerms:    []string{"分数", "score"},
			designTerms: []string{"分数", "score"},
		},
		{
			name:        "碰撞检测",
			prdTerms:    []string{"碰撞", "撞墙", "撞到", "collision"},
			designTerms: []string{"碰撞", "撞墙", "撞到", "collision"},
		},
		{
			name:        "游戏结束状态处理",
			prdTerms:    []string{"游戏结束", "结束", "game over", "gameover"},
			designTerms: []string{"游戏结束", "结束", "game over", "gameover"},
		},
	}

	missing := make([]string, 0)
	for _, check := range checks {
		if containsAny(lowerPRD, check.prdTerms) && !containsAny(lowerDesign, check.designTerms) {
			missing = append(missing, check.name)
		}
	}
	if len(missing) == 0 && len(strings.TrimSpace(design)) < 40 {
		return []string{"架构设计过于简略，无法支撑产品需求验收"}
	}
	return missing
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func buildReviewPlanInstruction(missing []string) string {
	var builder strings.Builder
	builder.WriteString("# PM 审阅修改说明\n\n")
	builder.WriteString("## 结论\n\n")
	builder.WriteString("当前架构设计需要重新调整。\n\n")
	builder.WriteString("## 需要修改的地方\n\n")
	for _, item := range missing {
		builder.WriteString("- 请补齐")
		builder.WriteString(item)
		builder.WriteString("。\n")
	}
	builder.WriteString("\n## 修改建议\n\n")
	builder.WriteString("请根据产品书补齐上述内容，并重新提交架构设计。\n")
	return builder.String()
}

func (a *Agent) executeWritePlanFallback(task core.TaskMetaData) (core.TaskMetaData, error) {
	a.logStep("pm_write_plan fallback output used")
	outputKind := "prd"
	if task.Op == "pm_write_plan" {
		outputKind = "plan"
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", outputKind, "plan_v1.md")
	content := prependDocumentBasis("# 产品需求文档\n\n## 说明\n\n当前未通过真实 LLM 生成完整产品细节。\n", task.ArtifactURIs)
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "plan", "plan_v1.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) executeReplanFallback(task core.TaskMetaData, inputs replanInputs) (core.TaskMetaData, error) {
	a.logStep("pm_replan fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "prd", "plan_v2.md")
	content := prependDocumentBasis("# 产品需求文档（Replan）\n\n## 说明\n\n当前未通过真实 LLM 生成完整 replan 产品细节。\n", replanBasisURIs(task.ArtifactURIs, inputs.previousPlanURI))
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(context.Background(), outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "prd", "plan_v2.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "PMAgent", message)
	}
}
