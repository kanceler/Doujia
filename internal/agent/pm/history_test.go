package pm

import (
	"context"
	"testing"

	"devflow/internal/core"
)

func TestExecuteAppendsTaskHistory(t *testing.T) {
	agent := &Agent{
		agentID:     "pm01",
		runID:       "run1",
		taskHistory: []core.AgentTaskHistory{{TaskID: "existing"}},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task1",
		AgentID:      "pm01",
		Op:           "pm_review_design",
		ArtifactURIs: []string{"projects/run1/agents/architect01/artifacts/design/architecture_v1.md"},
	}

	feedback, err := agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if got, want := len(agent.taskHistory), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
	last := agent.taskHistory[1]
	if last.RunID != task.RunID || last.TaskID != task.TaskID || last.AgentID != agent.agentID || last.Op != task.Op {
		t.Fatalf("history identity = %+v, want run/task/agent/op from executed task", last)
	}
	if last.Status != core.TaskStatusDone {
		t.Fatalf("history status = %s, want %s", last.Status, core.TaskStatusDone)
	}
	if got, want := len(last.InputArtifactURIs), len(task.ArtifactURIs); got != want {
		t.Fatalf("input artifact count = %d, want %d", got, want)
	}
	if last.InputArtifactURIs[0] != task.ArtifactURIs[0] {
		t.Fatalf("input artifact = %q, want %q", last.InputArtifactURIs[0], task.ArtifactURIs[0])
	}
	if got, want := len(last.OutputArtifactURIs), len(feedback.ArtifactURIs); got != want {
		t.Fatalf("output artifact count = %d, want %d", got, want)
	}
	if last.OutputArtifactURIs[0] != feedback.ArtifactURIs[0] {
		t.Fatalf("output artifact = %q, want %q", last.OutputArtifactURIs[0], feedback.ArtifactURIs[0])
	}
}
