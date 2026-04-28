package architect

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
	agentID        core.AgentID
	runID          core.RunID
	workspacePath  string
	runConfig      core.RunConfig
	historyMu      sync.Mutex
	taskHistory    []core.AgentTaskHistory
	artifactStore  artifact.Store
	llmClient      llm.Client
	logger         logging.RunLogger
	openCodeRunner common.OpenCodeRunner
	gitManager     common.GitManager
}

func NewFactory() runtime.Agent {
	return &Agent{}
}

func (a *Agent) Create(init runtime.AgentInit, deps runtime.AgentDeps) runtime.Agent {
	return &Agent{
		agentID:        init.AgentID,
		runID:          init.RunID,
		workspacePath:  init.WorkspacePath,
		runConfig:      init.RunConfig,
		taskHistory:    common.CloneTaskHistory(init.TaskHistory),
		artifactStore:  deps.ArtifactStore,
		llmClient:      deps.LLMClient,
		logger:         deps.Logger,
		openCodeRunner: common.NewManagedOpenCodeRunner(init.RunRoot),
		gitManager:     common.NewLocalGitManager(),
	}
}

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (feedback core.TaskMetaData, err error) {
	defer func() {
		a.recordTaskHistory(task, feedback, err)
	}()

	switch task.Op {
	case core.TaskOpWritePlan:
		return a.executeArchitectureGeneration(ctx, task)
	case core.TaskOpSplitModule, core.TaskOpResplitModule:
		return a.executeSplitModule(ctx, task)
	case core.TaskOpMergeCode:
		return a.executeMergeCode(ctx, task)
	case core.TaskOpTestCode:
		return a.executeGlobalTestCode(ctx, task)
	case core.TaskOpTestData:
		return a.executeTestData(ctx, task)
	case core.TaskOpReplan:
		return a.executeReplan(ctx, task)
	case "architecture_generation", core.TaskOpRewrite:
		return a.executeArchitectureGeneration(ctx, task)
	case "architect_review_proposal":
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}
