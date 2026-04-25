package pm

import (
	"context"
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
	taskHistory   []core.TaskMetaData
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

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case "pm_write_plan":
		return a.executeWritePlan(ctx, task)
	case "pm_review_design":
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}
