package pm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"devflow/internal/agent/core"
	"devflow/internal/agent/llm"
	rolecommon "devflow/internal/agent/role/common"
	"devflow/internal/agent/schema"
)

type PMAgent struct{}

func NewAgent() *PMAgent {
	return &PMAgent{}
}

func (a *PMAgent) Role() string {
	return "pm"
}

func (a *PMAgent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	return rolecommon.DispatchByOpID(ctx, req, map[string]rolecommon.OpHandler{
		"pm.write_plan": func(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
			if req.ToolLoop == nil {
				return agentFail("missing_tool_loop", "pm agent requires ToolLoop"), nil
			}
			return req.ToolLoop.Run(ctx, core.ToolLoopRequest{
				Task:     req.Task,
				Bundle:   req.Bundle,
				OpSpec:   req.OpSpec,
				Prompt:   req.Prompt,
				Handlers: req.Handlers,
				LLM:      req.LLM,
			})
		},
		"pm.review_plan": a.runReviewPlan,
	}, "unsupported_op")
}

func (a *PMAgent) runReviewPlan(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	architectureContent, err := readBundleArtifactContent(req.Bundle, core.LKArchitecturePlan)
	if err != nil {
		return core.AgentResult{}, err
	}
	environmentPath, err := readBundleArtifactPath(req.Bundle, core.LKEnvironmentSpec)
	if err != nil {
		return core.AgentResult{}, err
	}

	issues := reviewArchitecturePlan(architectureContent)
	issues = append(issues, reviewEnvironmentSpec(environmentPath)...)
	if len(issues) == 0 {
		return core.AgentResult{
			Result:  "kok",
			Message: "pm review_plan completed",
			Outputs: []core.AgentOutput{},
		}, nil
	}

	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("artifact_write handler is required")
	}
	report := generateReviewFailureMarkdown(ctx, req.LLM, issues)
	resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": core.LKReviewPlanFailure,
			"content":     report,
		},
	})
	if err != nil {
		return core.AgentResult{}, err
	}

	return core.AgentResult{
		Result:  "kfail",
		Message: "pm review_plan failed",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  core.LKReviewPlanFailure,
				ObjectType:  stringValue(resp.Data["object_type"]),
				ContentType: stringValue(resp.Data["content_type"]),
				Encoding:    stringValue(resp.Data["encoding"]),
				Status:      stringValue(resp.Data["status"]),
				Path:        stringValue(resp.Data["path"]),
				ArtifactURI: stringValue(resp.Data["artifact_uri"]),
			},
		},
		Errors: []core.AgentError{
			{Code: "review_plan_failed", Message: "architect write_plan output did not pass pm review; see review_plan_failure"},
		},
	}, nil
}

func reviewArchitecturePlan(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{"architecture_plan is empty."}
	}

	headings := 0
	for _, line := range strings.Split(strings.ReplaceAll(trimmed, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headings++
		}
	}

	issues := []string{}
	if headings < 2 {
		issues = append(issues, "architecture_plan should include at least two Markdown headings.")
	}
	if utf8.RuneCountInString(trimmed) < 80 {
		issues = append(issues, "architecture_plan is too short to look like a normal architect write_plan output.")
	}
	return issues
}

func reviewEnvironmentSpec(path string) []string {
	spec, err := schema.ReadEnvironmentSpecFile(path)
	if err != nil {
		return []string{"environment_spec is unreadable: " + err.Error()}
	}
	if err := spec.Validate(); err != nil {
		return []string{"environment_spec is invalid: " + err.Error()}
	}
	return nil
}

func generateReviewFailureMarkdown(ctx context.Context, llmClient core.LLMClientLike, issues []string) string {
	adapter, ok := llmClient.(llm.Adapter)
	if ok && adapter != nil {
		prompt := "请根据下面的问题，写一份简短的 Markdown 审阅失败说明。语气平和，重点说明为什么 architect.write_plan 当前不适合继续流转。只输出 Markdown 正文，不要代码块。\n\n问题列表：\n- " + strings.Join(issues, "\n- ")
		resp, err := adapter.Chat(ctx, llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
		})
		if err == nil && strings.TrimSpace(resp.Message.Content) != "" {
			return resp.Message.Content
		}
	}

	return "# Review Failed\n\n- " + strings.Join(issues, "\n- ") + "\n"
}

func readBundleArtifactContent(bundle core.AgentInputBundle, logicalKey string) (string, error) {
	path, err := readBundleArtifactPath(bundle, logicalKey)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func readBundleArtifactPath(bundle core.AgentInputBundle, logicalKey string) (string, error) {
	for i := len(bundle.Inputs) - 1; i >= 0; i-- {
		input := bundle.Inputs[i]
		if input.LogicalKey == logicalKey && input.Path != "" {
			return resolveBundleArtifactPath(bundle, input.Path), nil
		}
	}
	for i := len(bundle.PreviousOutputs) - 1; i >= 0; i-- {
		previous := bundle.PreviousOutputs[i]
		if previous.LogicalKey == logicalKey && previous.Path != "" {
			return resolveBundleArtifactPath(bundle, previous.Path), nil
		}
	}
	return "", fmt.Errorf("missing artifact %q", logicalKey)
}

func resolveBundleArtifactPath(bundle core.AgentInputBundle, rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" || filepath.IsAbs(rawPath) {
		return filepath.Clean(rawPath)
	}
	candidates := []string{rawPath}
	if bundle.InputDir != "" {
		candidates = append(candidates, filepath.Join(bundle.InputDir, filepath.FromSlash(rawPath)))
	}
	if runRoot := runRootFromBundle(bundle); runRoot != "" {
		candidates = append(candidates, filepath.Join(runRoot, filepath.FromSlash(projectArtifactPath(rawPath))))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate)
		}
	}
	return filepath.Clean(rawPath)
}

func runRootFromBundle(bundle core.AgentInputBundle) string {
	for _, dir := range []string{bundle.InputDir, bundle.OutputDir} {
		if root := runRootFromAgentDir(dir); root != "" {
			return root
		}
	}
	return ""
}

func runRootFromAgentDir(dir string) string {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "agents" && i > 0 {
			return filepath.FromSlash(strings.Join(parts[:i], "/"))
		}
	}
	return ""
}

func projectArtifactPath(rawPath string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(rawPath))
	parts := strings.Split(normalized, "/")
	if len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" {
		return strings.Join(parts[2:], "/")
	}
	return normalized
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func buildPlan(requirementContent string) string {
	summary := summarizeRequirement(requirementContent, 1200)
	return fmt.Sprintf(`# PM Plan v1

## Requirement Summary
%s

## Scope
- Turn the requirement into a clear first-version product plan.
- Identify the primary pages and user-facing capabilities needed for delivery.
- Keep implementation details for downstream architecture and coding stages.

## Pages / Features
- Home or entry page that explains the product purpose.
- Core content/list page for browsing main information.
- Detail page for reading or inspecting one item.
- About or supporting page when the requirement calls for personal or contextual information.

## Non-goals / Follow-up
- No real LLM integration in this runtime milestone.
- No persistence into DoujiaGit or artifact version storage yet.
- Follow-up agents can refine architecture, code tasks, and test data after this plan.
`, summary)
}

func summarizeRequirement(content string, maxRunes int) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "Requirement file is empty."
	}
	if utf8.RuneCountInString(trimmed) <= maxRunes {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxRunes]) + "\n\n(truncated)"
}

func agentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
