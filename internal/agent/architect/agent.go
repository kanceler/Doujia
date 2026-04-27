package architect

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
	case core.TaskOpWritePlan:
		return a.executeArchitectureFallback(task)
	case core.TaskOpSplitModule, core.TaskOpResplitModule:
		return a.executeSplitModule(ctx, task)
	case core.TaskOpMergeCode:
		return a.executeMergeCode(ctx, task)
	case core.TaskOpTestCode:
		return a.executeGlobalTestCode(ctx, task)
	case "architecture_generation", core.TaskOpRewrite, core.TaskOpReplan:
		return a.executeArchitectureGeneration(ctx, task)
	case "architect_review_proposal":
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}
