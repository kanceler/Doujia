package runtime

import (
	"context"
	"devflow/internal/core"
	"path/filepath"
)

type ArchitectAgent struct {
	agentID       core.AgentID
	runID         core.RunID
	workspacePath string
}

func NewArchitectAgentFactory() Agent {
	return &ArchitectAgent{}
}

func (a *ArchitectAgent) Create(init AgentInit) Agent {
	return &ArchitectAgent{
		agentID:       init.AgentID,
		runID:         init.RunID,
		workspacePath: init.WorkspacePath,
	}
}

func (a *ArchitectAgent) Execute(_ context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case "0":
		if err := appendLineToArtifacts(task.ArtifactURIs, "Architect reviewed the input artifact."); err != nil {
			return core.TaskMetaData{}, err
		}
		output, err := writeAgentOutput(
			a.workspacePath,
			filepath.Join("artifacts", "architecture", "architecture_v1.md"),
			"# Architecture\n\nArchitect design draft is ready.\n",
		)
		if err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, []string{output}), nil
	case "1":
		if err := appendLineToArtifacts(task.ArtifactURIs, "Architect approved the proposal."); err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}
