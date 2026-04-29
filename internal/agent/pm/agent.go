package pm

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/runtime"
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

	switch task.Op {
	case core.TaskOpWritePlan:
		return a.executeWritePlan(ctx, task)
	case "pm_write_plan", core.TaskOpRewrite, core.TaskOpReplan:
		if task.Op == core.TaskOpReplan {
			return a.executeReplan(ctx, task)
		}
		return a.executeWritePlan(ctx, task)
	case "pm_review_design", core.TaskOpReviewPlan:
		return a.executeReviewPlan(ctx, task)
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}
