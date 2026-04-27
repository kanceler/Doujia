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

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "PMAgent", message)
	}
}
