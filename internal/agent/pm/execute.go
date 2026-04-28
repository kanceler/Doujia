package pm

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

func (a *Agent) executeWritePlan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		a.logStep("pm_write_plan fallback: artifact store is nil")
		return a.executeWritePlanFallback(task)
	}
	parsedTask := task
	if parsedTask.Op == core.TaskOpWritePlan {
		parsedTask.Op = "pm_write_plan"
	}

	env, err := agentengine.ParseTask(parsedTask)
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
	uris, err := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, recipe.OutputKind, output)
	if err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write artifact outputs: %w", err)
	}
	a.logStep(fmt.Sprintf("pm_write_plan artifact write success: outputs=%d", len(uris)))
	return agentengine.BuildFeedback(task, uris, output), nil
}

func (a *Agent) executeWritePlanFallback(task core.TaskMetaData) (core.TaskMetaData, error) {
	a.logStep("pm_write_plan fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "plan", "plan_v1.md")
	content := "# Plan\n\nPM plan draft is ready.\n"
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

func (a *Agent) executeReviewPlan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}

	prdURI, prd, designURI, design, err := a.resolveReviewPlanDocs(ctx, task.ArtifactURIs)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_review_plan fallback pass-through: %v", err))
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
	if approved, note, handled := a.reviewPlanWithModel(ctx, task, prdURI, prd, designURI, design); handled {
		if approved {
			uris := append([]string(nil), task.ArtifactURIs...)
			if strings.TrimSpace(note) != "" {
				outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "review", "review_plan_note.md")
				if err := a.artifactStore.Write(ctx, outputURI, []byte(strings.TrimSpace(note)+"\n")); err != nil {
					return core.TaskMetaData{}, err
				}
				uris = append(uris, outputURI)
			}
			return common.FeedbackFor(task, a.runID, a.agentID, uris), nil
		}
		outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "review", "replan_instruction.md")
		if err := a.artifactStore.Write(ctx, outputURI, []byte(strings.TrimSpace(note)+"\n")); err != nil {
			return core.TaskMetaData{}, err
		}
		return core.TaskMetaData{
			Direction:    core.TaskDirectionFeedback,
			RunID:        task.RunID,
			TaskID:       task.TaskID,
			ParentID:     task.ParentID,
			DependsOn:    task.DependsOn,
			DependsOnIDs: task.DependsOnIDs,
			AgentID:      a.agentID,
			Op:           task.Op,
			ArtifactURIs: []string{outputURI},
			Result:       core.TaskResultCodeReplan,
		}, nil
	}

	missing := reviewPlanMissingItems(prd, design)
	if len(missing) == 0 {
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}

	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "review", "replan_instruction.md")
	content := buildReviewPlanInstruction(missing, task.ArtifactURIs)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
		return core.TaskMetaData{}, err
	}
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        task.RunID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{outputURI},
		Result:       core.TaskResultCodeReplan,
	}, nil
}

func (a *Agent) executeReplan(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return a.executeReplanFallback(task, "", "")
	}
	requirementURI, requirement, reviewURI, review, err := a.resolveReplanDocs(ctx, task.ArtifactURIs)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_replan fallback with partial inputs: %v", err))
	}
	previousPlanURI := a.lastPlanArtifactURI()
	previousPlan := ""
	if previousPlanURI != "" {
		if content, readErr := a.artifactStore.Read(ctx, previousPlanURI); readErr == nil {
			previousPlan = string(content)
		} else {
			a.logStep(fmt.Sprintf("pm_replan previous plan read skipped: uri=%s err=%v", previousPlanURI, readErr))
		}
	}

	if !llm.IsNoop(a.llmClient) && strings.TrimSpace(requirement) != "" && strings.TrimSpace(review) != "" {
		prompt := buildReplanPrompt(task, requirementURI, requirement, reviewURI, review, previousPlanURI, previousPlan)
		a.logStep(fmt.Sprintf("pm_replan llm request start: prompt_chars=%d", len(prompt)))
		raw, llmErr := a.llmClient.Complete(ctx, prompt)
		if llmErr != nil {
			return core.TaskMetaData{}, fmt.Errorf("pm_replan llm request failed: %w", llmErr)
		}
		output, parseErr := agentengine.ParseModelOutput(raw)
		if parseErr != nil {
			return core.TaskMetaData{}, fmt.Errorf("pm_replan parse model output failed: %w", parseErr)
		}
		uris, writeErr := agentengine.WriteArtifacts(ctx, a.artifactStore, a.runID, a.agentID, "prd", output)
		if writeErr != nil {
			return core.TaskMetaData{}, fmt.Errorf("write replan artifact outputs: %w", writeErr)
		}
		return agentengine.BuildFeedback(task, uris, output), nil
	}

	return a.executeReplanFallback(task, previousPlanURI, previousPlan)
}

func (a *Agent) resolveReviewPlanDocs(ctx context.Context, uris []string) (string, string, string, string, error) {
	var prdURI string
	var prd string
	var designURI string
	var design string
	for _, uri := range uris {
		kind := agentengine.InferArtifactKind(uri)
		if kind != "prd" && kind != "design" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, uri)
		if err != nil {
			return "", "", "", "", fmt.Errorf("read artifact %s: %w", uri, err)
		}
		switch kind {
		case "prd":
			prdURI = uri
			prd = string(content)
		case "design":
			designURI = uri
			design = string(content)
		}
	}
	if strings.TrimSpace(prd) == "" {
		prdURI = a.lastPlanArtifactURI()
		if prdURI != "" {
			if content, err := a.artifactStore.Read(ctx, prdURI); err == nil {
				prd = string(content)
			}
		}
	}
	if strings.TrimSpace(prd) == "" || strings.TrimSpace(design) == "" {
		return "", "", "", "", fmt.Errorf("review_plan requires both prd and design artifacts")
	}
	return prdURI, prd, designURI, design, nil
}

func (a *Agent) resolveReplanDocs(ctx context.Context, uris []string) (string, string, string, string, error) {
	var requirementURI, requirement string
	var reviewURI, review string
	for _, uri := range uris {
		normalized := filepath.ToSlash(strings.TrimSpace(uri))
		if normalized == "" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, normalized)
		if err != nil {
			return "", "", "", "", fmt.Errorf("read artifact %s: %w", normalized, err)
		}
		lower := strings.ToLower(normalized)
		switch {
		case strings.Contains(lower, "/artifacts/requirement/"):
			requirementURI = normalized
			requirement = string(content)
		case strings.Contains(lower, "/artifacts/review/") || strings.Contains(lower, "replan_instruction"):
			reviewURI = normalized
			review = string(content)
		}
	}
	if strings.TrimSpace(requirement) == "" {
		return requirementURI, requirement, reviewURI, review, fmt.Errorf("replan requires requirement artifact")
	}
	if strings.TrimSpace(review) == "" {
		return requirementURI, requirement, reviewURI, review, fmt.Errorf("replan requires review artifact")
	}
	return requirementURI, requirement, reviewURI, review, nil
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
	case core.TaskOpWritePlan, "pm_write_plan", core.TaskOpRewrite, core.TaskOpReplan:
		return true
	default:
		return false
	}
}

func isPlanArtifactURI(uri string) bool {
	lower := strings.ToLower(filepath.ToSlash(uri))
	return strings.Contains(lower, "/artifacts/prd/") || strings.Contains(lower, "/artifacts/plan/")
}

func reviewPlanMissingItems(prd, design string) []string {
	if len(strings.TrimSpace(design)) < 40 {
		return []string{"architecture design is too short to support PM acceptance review"}
	}
	checks := []struct {
		name        string
		prdTerms    []string
		designTerms []string
	}{
		{name: "acceptance criteria", prdTerms: []string{"acceptance", "验收"}, designTerms: []string{"acceptance", "验收", "test"}},
		{name: "API or interface design", prdTerms: []string{"api", "接口"}, designTerms: []string{"api", "接口", "contract"}},
		{name: "error handling", prdTerms: []string{"error", "错误", "异常"}, designTerms: []string{"error", "错误", "异常", "fallback"}},
		{name: "data model", prdTerms: []string{"data", "数据"}, designTerms: []string{"data", "数据", "model", "schema"}},
	}
	lowerPRD := strings.ToLower(prd)
	lowerDesign := strings.ToLower(design)
	missing := make([]string, 0)
	for _, check := range checks {
		if containsAny(lowerPRD, check.prdTerms) && !containsAny(lowerDesign, check.designTerms) {
			missing = append(missing, check.name)
		}
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

func buildReviewPlanInstruction(missing []string, basis []string) string {
	var builder strings.Builder
	builder.WriteString("# PM Review Replan Instruction\n\n")
	builder.WriteString("## Result\n\n")
	builder.WriteString("The current architecture design needs rework before delivery can continue.\n\n")
	builder.WriteString("## Missing Or Weak Items\n\n")
	for _, item := range missing {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Input Artifacts\n\n")
	for _, uri := range basis {
		builder.WriteString("- ")
		builder.WriteString(uri)
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Instruction\n\n")
	builder.WriteString("Please revise the upstream plan or design so the missing items are explicitly covered.\n")
	return builder.String()
}

func (a *Agent) reviewPlanWithModel(ctx context.Context, task core.TaskMetaData, prdURI, prd, designURI, design string) (bool, string, bool) {
	if llm.IsNoop(a.llmClient) {
		return false, "", false
	}
	prompt := buildReviewPlanPrompt(task, prdURI, prd, designURI, design)
	a.logStep(fmt.Sprintf("pm_review_plan llm request start: prompt_chars=%d", len(prompt)))
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("pm_review_plan llm request failed, heuristic fallback used: %v", err))
		return false, "", false
	}
	var decision struct {
		Approved    bool     `json:"approved"`
		Summary     string   `json:"summary"`
		Missing     []string `json:"missing_items"`
		ReviewNote  string   `json:"review_note"`
		Instruction string   `json:"replan_instruction"`
	}
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		a.logStep(fmt.Sprintf("pm_review_plan parse model output failed, heuristic fallback used: %v", err))
		return false, "", false
	}
	if decision.Approved {
		note := strings.TrimSpace(decision.ReviewNote)
		if note == "" {
			note = "# PM Review Note\n\n" + strings.TrimSpace(decision.Summary)
		}
		return true, note, true
	}
	instruction := strings.TrimSpace(decision.Instruction)
	if instruction == "" {
		instruction = buildReviewPlanInstruction(nonEmptyItems(decision.Missing), []string{prdURI, designURI})
	}
	return false, instruction, true
}

func buildReviewPlanPrompt(task core.TaskMetaData, prdURI, prd, designURI, design string) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are a PM Agent. Review the architecture design against the PRD and decide whether delivery can continue.\n\n")
	builder.WriteString("# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\nagent_id: %s\nop: %s\n\n", task.TaskID, task.AgentID, task.Op))
	writePromptDoc(&builder, "PRD", prdURI, prd)
	writePromptDoc(&builder, "Architecture Design", designURI, design)
	builder.WriteString("# Output JSON Schema\n")
	builder.WriteString(`{"approved":true,"summary":"short summary","missing_items":[],"review_note":"markdown note when approved","replan_instruction":"markdown instruction when rejected"}`)
	builder.WriteString("\n\nReturn exactly one JSON object. Do not use markdown code fences.\n")
	return builder.String()
}

func nonEmptyItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func buildReplanPrompt(task core.TaskMetaData, requirementURI, requirement, reviewURI, review, previousPlanURI, previousPlan string) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are a PM Agent. Rewrite a complete product plan according to the requirement, review instruction, and previous plan.\n\n")
	builder.WriteString("# Task\n")
	builder.WriteString(fmt.Sprintf("task_id: %s\nagent_id: %s\nop: replan\n\n", task.TaskID, task.AgentID))
	writePromptDoc(&builder, "Requirement", requirementURI, requirement)
	writePromptDoc(&builder, "Review Instruction", reviewURI, review)
	if strings.TrimSpace(previousPlan) != "" {
		writePromptDoc(&builder, "Previous Plan", previousPlanURI, previousPlan)
	}
	builder.WriteString("# Output JSON Schema\n")
	builder.WriteString(`{"summary":"short summary","artifact_outputs":[{"type":"prd","filename":"plan_v2.md","content":"markdown content"}],"control":[]}`)
	builder.WriteString("\n\nReturn exactly one JSON object. Do not use markdown code fences.\n")
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

func (a *Agent) executeReplanFallback(task core.TaskMetaData, previousPlanURI, previousPlan string) (core.TaskMetaData, error) {
	a.logStep("pm_replan fallback output used")
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "prd", "plan_v2.md")
	var builder strings.Builder
	builder.WriteString("# Product Plan Replan\n\n")
	builder.WriteString("PM replan draft is ready.\n\n")
	if strings.TrimSpace(previousPlanURI) != "" {
		builder.WriteString("## Previous Plan\n\n")
		builder.WriteString("source: ")
		builder.WriteString(previousPlanURI)
		builder.WriteString("\n\n")
		if strings.TrimSpace(previousPlan) != "" {
			builder.WriteString(previousPlan)
			builder.WriteString("\n")
		}
	}
	content := builder.String()
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
