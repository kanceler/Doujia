package runtime

import (
	"context"
	"devflow/internal/core"
	"path/filepath"
)

type PMAgent struct {
	agentID       core.AgentID
	runID         core.RunID
	workspacePath string
}

func NewPMAgentFactory() Agent {
	return &PMAgent{}
}

func (a *PMAgent) Create(init AgentInit) Agent {
	return &PMAgent{
		agentID:       init.AgentID,
		runID:         init.RunID,
		workspacePath: init.WorkspacePath,
	}
}

func (a *PMAgent) Execute(_ context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case "0":
		if err := appendLineToArtifacts(task.ArtifactURIs, "PM reviewed the input artifact."); err != nil {
			return core.TaskMetaData{}, err
		}
		output, err := writeAgentOutput(
			a.workspacePath,
			filepath.Join("artifacts", "plan", "plan_v1.md"),
			"# Plan\n\nPM plan draft is ready.\n",
		)
		if err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, []string{output}), nil
	case "1":
		if err := appendLineToArtifacts(task.ArtifactURIs, "PM approved the proposal."); err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}
