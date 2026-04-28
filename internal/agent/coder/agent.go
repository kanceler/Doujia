package coder

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
	runRoot        string
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
		runRoot:        init.RunRoot,
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
	case core.TaskOpWriteCode:
		return a.executeWriteCode(ctx, task, recipe)
	case core.TaskOpDebug:
		return a.executeDebug(ctx, task, recipe)
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}

func (a *Agent) recordTaskHistory(task core.TaskMetaData, feedback core.TaskMetaData, err error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()

	a.taskHistory = append(a.taskHistory, common.NewTaskHistoryItem(task, feedback, a.agentID, err))
}

func (a *Agent) writeCodeArtifact(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	filename := fmt.Sprintf("%s_code_v1.md", a.agentID)
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "code", filename)
	content := fmt.Sprintf("# Code Stub\n\nagent: %s\nop: %s\n", a.agentID, task.Op)
	if a.artifactStore != nil {
		if err := a.artifactStore.Write(ctx, outputURI, []byte(content)); err != nil {
			return core.TaskMetaData{}, err
		}
		a.logStep(fmt.Sprintf("code artifact written: %s", outputURI))
		return common.FeedbackFor(task, a.runID, a.agentID, []string{outputURI}), nil
	}
	output, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "code", filename), content)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return common.FeedbackFor(task, a.runID, a.agentID, []string{output}), nil
}

func (a *Agent) logStep(message string) {
	if a.logger != nil {
		_ = a.logger.Log(a.runID, "CoderAgent", message)
	}
}
