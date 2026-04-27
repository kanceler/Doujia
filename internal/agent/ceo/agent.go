package ceo

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/runtime"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
	case "write_plan", "ceo_write_requirement":
		return a.executeWriteRequirement(ctx, task)
	case "review_plan", "ceo_review_plan", "user_confirm", "ceo_user_confirm":
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
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
	content := "# Requirement\n\n我需要制作一个贪吃蛇软件\n"
	if a.artifactStore != nil && len(task.ArtifactURIs) > 0 {
		input, err := a.artifactStore.Read(ctx, task.ArtifactURIs[0])
		if err != nil {
			return core.TaskMetaData{}, fmt.Errorf("read CEO write_plan input: %w", err)
		}
		content = string(input)
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
