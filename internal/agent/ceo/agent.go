package ceo

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"devflow/internal/agent/common"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/runtime"
)

type Agent struct {
	agentID       core.AgentID
	runID         core.RunID
	workspacePath string
	historyMu     sync.Mutex
	taskHistory   []core.AgentTaskHistory
	artifactStore artifact.Store
	llmClient     llm.Client
	logger        logging.RunLogger
}

func NewFactory() runtime.Agent {
	return &Agent{}
}

func (a *Agent) Create(init runtime.AgentInit, deps runtime.AgentDeps) runtime.Agent {
	return &Agent{
		agentID:       init.AgentID,
		runID:         init.RunID,
		workspacePath: init.WorkspacePath,
		taskHistory:   common.CloneTaskHistory(init.TaskHistory),
		artifactStore: deps.ArtifactStore,
		llmClient:     deps.LLMClient,
		logger:        deps.Logger,
	}
}

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (feedback core.TaskMetaData, err error) {
	defer func() {
		a.recordTaskHistory(task, feedback, err)
	}()

	if task.Direction != core.TaskDirectionDispatch {
		return core.TaskMetaData{}, fmt.Errorf("unsupported direction %q: only dispatch can be executed", task.Direction)
	}
	if strings.TrimSpace(string(task.TaskID)) == "" {
		return core.TaskMetaData{}, fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(string(task.AgentID)) == "" {
		return core.TaskMetaData{}, fmt.Errorf("agent_id is required")
	}

	switch task.Op {
	case "ceo_write_requirement", core.TaskOpWritePlan, core.TaskOpRewrite, core.TaskOpReplan:
		return a.executeWriteRequirement(ctx, task)
	case "ceo_review_plan", "ceo_user_confirm", core.TaskOpReviewPlan:
		return a.executeReviewOrConfirm(ctx, task)
	default:
		return core.TaskMetaData{}, fmt.Errorf("unsupported ceo op %q", task.Op)
	}
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}

func (a *Agent) executeWriteRequirement(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "requirement", "requirement_v1.md")
	content := defaultSnakeRequirement()
	if override := strings.TrimSpace(os.Getenv("DEVFLOW_CEO_REQUIREMENT_MD")); override != "" {
		content = normalizeRequirementContent(override)
		a.logStep(fmt.Sprintf("ceo_write_requirement env override used: chars=%d", len(content)))
	} else if a.artifactStore != nil && len(task.ArtifactURIs) > 0 {
		input, err := a.artifactStore.Read(ctx, task.ArtifactURIs[0])
		if err != nil {
			return core.TaskMetaData{}, fmt.Errorf("read CEO write_plan input: %w", err)
		}
		content = string(input)
	} else if !llm.IsNoop(a.llmClient) {
		prompt := buildRequirementPrompt()
		a.logStep(fmt.Sprintf("ceo_write_requirement llm request start: prompt_chars=%d", len(prompt)))
		raw, err := a.llmClient.Complete(ctx, prompt)
		if err != nil {
			a.logStep(fmt.Sprintf("ceo_write_requirement llm request failed, fallback used: %v", err))
		} else if strings.TrimSpace(raw) != "" {
			content = normalizeRequirementContent(raw)
			a.logStep(fmt.Sprintf("ceo_write_requirement llm request success: chars=%d", len(content)))
		}
	}
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}

	output, err := common.WriteAgentOutput(
		a.workspacePath,
		filepath.Join("artifacts", "requirement", "requirement_v1.md"),
		content,
	)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) executeReviewOrConfirm(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil || llm.IsNoop(a.llmClient) || len(task.ArtifactURIs) == 0 {
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
	docs := make([]string, 0, len(task.ArtifactURIs))
	for _, uri := range task.ArtifactURIs {
		content, err := a.artifactStore.Read(ctx, uri)
		if err != nil {
			a.logStep(fmt.Sprintf("%s llm review skipped, read failed: %v", task.Op, err))
			return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
		}
		docs = append(docs, "source: "+uri+"\n"+string(content))
	}
	prompt := buildCEOReviewPrompt(task.Op, docs)
	a.logStep(fmt.Sprintf("%s llm request start: inputs=%d prompt_chars=%d", task.Op, len(task.ArtifactURIs), len(prompt)))
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("%s llm request failed, pass-through used: %v", task.Op, err))
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
	note := strings.TrimSpace(raw)
	if note == "" {
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
	filename := "review_note.md"
	subdir := "review"
	if task.Op == "ceo_user_confirm" {
		filename = "user_confirm.md"
		subdir = "confirm"
	}
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", subdir, filename)
	if err := a.artifactStore.Write(ctx, outputURI, []byte(strings.TrimSpace(note)+"\n")); err != nil {
		return core.TaskMetaData{}, err
	}
	uris := append([]string(nil), task.ArtifactURIs...)
	uris = append(uris, outputURI)
	return common.FeedbackFor(task, a.runID, a.agentID, uris), nil
}

func defaultSnakeRequirement() string {
	lines := []string{
		"# \u9700\u6c42\u6587\u6863",
		"",
		"## \u9879\u76ee\u6982\u8ff0",
		"",
		"\u6211\u9700\u8981\u4e00\u4e2a\u53ef\u4ee5\u76f4\u63a5\u8fd0\u884c\u548c\u4ea4\u4ed8\u7684\u8d2a\u5403\u86c7\u6e38\u620f\u6210\u54c1\uff0c\u6700\u7ec8\u4ea4\u4ed8\u7269\u5fc5\u987b\u5305\u542b\u771f\u5b9e\u6e38\u620f\u4ee3\u7801\uff0c\u800c\u4e0d\u662f\u5360\u4f4d\u6587\u6863\u6216\u4f2a\u4ee3\u7801\u3002",
		"",
		"## \u76ee\u6807",
		"",
		"- \u63d0\u4f9b\u4e00\u4e2a\u53ef\u73a9\u7684\u8d2a\u5403\u86c7\u6e38\u620f\u3002",
		"- \u73a9\u5bb6\u53ef\u4ee5\u5f00\u59cb\u65b0\u6e38\u620f\u3001\u63a7\u5236\u86c7\u79fb\u52a8\u3001\u770b\u5230\u5206\u6570\u53d8\u5316\uff0c\u5e76\u5728\u5931\u8d25\u540e\u91cd\u65b0\u5f00\u59cb\u3002",
		"- \u4ee3\u7801\u7ed3\u6784\u6e05\u6670\uff0c\u65b9\u4fbf\u540e\u7eed\u7ee7\u7eed\u6269\u5c55\u3002",
		"",
		"## \u6838\u5fc3\u529f\u80fd",
		"",
		"- \u4f7f\u7528\u952e\u76d8\u63a7\u5236\u86c7\u7684\u4e0a\u3001\u4e0b\u3001\u5de6\u3001\u53f3\u79fb\u52a8\u3002",
		"- \u5730\u56fe\u4e2d\u968f\u673a\u751f\u6210\u98df\u7269\uff0c\u86c7\u5403\u5230\u98df\u7269\u540e\u8eab\u4f53\u589e\u957f\u3002",
		"- \u5b9e\u65f6\u663e\u793a\u5f53\u524d\u5206\u6570\u3002",
		"- \u649e\u5899\u6216\u649e\u5230\u81ea\u5df1\u65f6\u6e38\u620f\u7ed3\u675f\u3002",
		"- \u6e38\u620f\u7ed3\u675f\u540e\u53ef\u4ee5\u91cd\u65b0\u5f00\u59cb\u3002",
		"",
		"## \u4f53\u9a8c\u8981\u6c42",
		"",
		"- \u6e38\u620f\u542f\u52a8\u6b65\u9aa4\u7b80\u5355\uff0c\u5c3d\u91cf\u51cf\u5c11\u989d\u5916\u4f9d\u8d56\u3002",
		"- \u64cd\u4f5c\u54cd\u5e94\u53ca\u65f6\uff0c\u4e3b\u5faa\u73af\u7a33\u5b9a\u3002",
		"- \u753b\u9762\u53ef\u4ee5\u7b80\u6d01\uff0c\u4f46\u8981\u6e05\u695a\u663e\u793a\u6e38\u620f\u533a\u57df\u3001\u86c7\u3001\u98df\u7269\u548c\u5206\u6570\u3002",
		"",
		"## \u4ea4\u4ed8\u8981\u6c42",
		"",
		"- \u8f93\u51fa\u771f\u5b9e\u53ef\u8fd0\u884c\u7684\u9879\u76ee\u4ee3\u7801\u3002",
		"- \u63d0\u4f9b\u5fc5\u8981\u7684\u8fd0\u884c\u8bf4\u660e\u3002",
		"- \u5c3d\u91cf\u8865\u5145\u57fa\u7840\u6d4b\u8bd5\u6216\u6821\u9a8c\u65b9\u5f0f\uff0c\u5e2e\u52a9\u786e\u8ba4\u6e38\u620f\u80fd\u6b63\u5e38\u8fd0\u884c\u3002",
	}
	return strings.Join(lines, "\n") + "\n"
}

func buildRequirementPrompt() string {
	return "You are the DevFlow CEO. Produce a Chinese Markdown requirement document for building a Snake game. " +
		"Assume this is a test-stage run, so do not ask clarifying questions. " +
		"Output only the final Chinese Markdown requirement document, not JSON."
}

func normalizeRequirementContent(raw string) string {
	content := strings.TrimSpace(raw)
	if !strings.HasPrefix(content, "#") {
		content = "# \u9700\u6c42\u6587\u6863\n\n" + content
	}
	return content + "\n"
}

func buildCEOReviewPrompt(op string, docs []string) string {
	action := "Review the provided documents and decide whether the run can continue."
	if op == "ceo_user_confirm" {
		action = "Act as the user confirmation step and decide whether the run can continue."
	}
	var builder strings.Builder
	builder.WriteString("You are the DevFlow CEO. Output a short Chinese Markdown note.\n")
	builder.WriteString("Task: ")
	builder.WriteString(action)
	builder.WriteString("\n")
	builder.WriteString("Requirements:\n")
	builder.WriteString("- Keep the note concise.\n")
	builder.WriteString("- Explicitly state whether the current documents can move to the next stage.\n")
	builder.WriteString("- Output only Chinese Markdown, not JSON.\n\n")
	for i, doc := range docs {
		builder.WriteString("## Input Document ")
		builder.WriteString(fmt.Sprintf("%d", i+1))
		builder.WriteString("\n\n")
		builder.WriteString(doc)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "CEOAgent", message)
	}
}
