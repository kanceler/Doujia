package tester

import (
	"context"
	"devflow/internal/agent/common"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/runtime"
	"fmt"
	"path"
	"path/filepath"
)

type Agent struct {
	agentID       core.AgentID
	runID         core.RunID
	workspacePath string
	artifactStore artifact.Store
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
		artifactStore: deps.ArtifactStore,
		logger:        deps.Logger,
	}
}

func (a *Agent) Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case core.TaskOpTestData, core.TaskOpTestCode:
		return a.writeTestArtifact(ctx, task)
	default:
		return common.FeedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
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
