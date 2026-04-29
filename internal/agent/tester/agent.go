package tester

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

	recipe, recipeErr := GetRecipe(task.Op)
	if recipeErr != nil {
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
	switch recipe.Op {
	case core.TaskOpTestData:
		return a.executeTestData(ctx, task, recipe), nil
	case core.TaskOpTestCode:
		return a.executeTestCode(ctx, task, recipe), nil
	default:
		return a.writeTestArtifact(ctx, task)
	}
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}

func (a *Agent) writeTestArtifact(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	filename := fmt.Sprintf("%s_%s_v1.md", a.agentID, task.Op)
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "test", filename)
	content := fmt.Sprintf("# Test Stub\n\nagent: %s\nop: %s\n", a.agentID, task.Op)
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		a.logStep(fmt.Sprintf("test artifact written: %s", outputURI))
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "test", filename), content)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "TesterAgent", message)
	}
}
