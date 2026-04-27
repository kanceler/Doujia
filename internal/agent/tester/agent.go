package tester

import (
	"context"
	"sync"

	"devflow/internal/agent/common"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/llm"
	"devflow/internal/logging"
	"devflow/internal/runtime"
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

	recipe, err := GetRecipe(task.Op)
	if err != nil {
		return a.failureFeedback(ctx, task, err), nil
	}
	if recipe.Op == "test_data" {
		return a.executeTestData(ctx, task, recipe), nil
	}
	if recipe.Op == "test_code" {
		return a.executeTestCode(ctx, task, recipe), nil
	}
	return a.failureFeedback(ctx, task, errNotImplemented(recipe.Op)), nil
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "TesterAgent", message)
	}
}
