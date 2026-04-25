package runtime

import (
	"context"
	"devflow/internal/core"
	"path/filepath"
)

type CEOAgent struct {
	agentID       core.AgentID
	runID         core.RunID
	workspacePath string
}

func NewCEOAgentFactory() Agent {
	return &CEOAgent{}
}

func (a *CEOAgent) Create(init AgentInit) Agent {
	return &CEOAgent{
		agentID:       init.AgentID,
		runID:         init.RunID,
		workspacePath: init.WorkspacePath,
	}
}

func (a *CEOAgent) Execute(_ context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	switch task.Op {
	case "0", "ceo_write_requirement":
		output, err := writeAgentOutput(
			a.workspacePath,
			filepath.Join("artifacts", "requirement", "requirement_v1.md"),
			"# Requirement\n\nCEO requirement draft is ready.\n",
		)
		if err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, []string{output}), nil
	case "1", "ceo_review_plan", "ceo_user_confirm":
		if err := appendLineToArtifacts(task.ArtifactURIs, "CEO approved."); err != nil {
			return core.TaskMetaData{}, err
		}
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	default:
		return feedbackFor(task, a.runID, a.agentID, append([]string(nil), task.ArtifactURIs...)), nil
	}
}
