package tester

import (
	"context"
	"testing"

	"devflow/internal/core"
)

func TestExecuteAppendsTaskHistory(t *testing.T) {
	agent := &Agent{
		agentID:     "tester01",
		runID:       "run1",
		taskHistory: []core.AgentTaskHistory{{TaskID: "existing"}},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task1",
		AgentID:      "tester01",
		Op:           "unsupported_tester_op",
		ArtifactURIs: []string{"projects/run1/agents/architect01/artifacts/modules/module_01.md"},
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
	if last.Status != core.TaskStatusFailed {
		t.Fatalf("history status = %s, want %s", last.Status, core.TaskStatusFailed)
	}
	if last.InputArtifactURIs[0] != task.ArtifactURIs[0] {
		t.Fatalf("input artifact = %q, want %q", last.InputArtifactURIs[0], task.ArtifactURIs[0])
	}
	if got, want := len(last.OutputArtifactURIs), len(feedback.ArtifactURIs); got != want {
		t.Fatalf("output artifact count = %d, want %d", got, want)
	}
}
