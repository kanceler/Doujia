package ceo

import (
	"context"
	"testing"

	"devflow/internal/core"
)

type memoryStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *memoryStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *memoryStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

func TestExecuteAppendsTaskHistory(t *testing.T) {
	agent := &Agent{
		agentID:     "ceo",
		runID:       "run1",
		taskHistory: []core.AgentTaskHistory{{TaskID: "existing"}},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task1",
		AgentID:      "ceo",
		Op:           "ceo_review_plan",
		ArtifactURIs: []string{"projects/run1/agents/pm01/artifacts/prd/plan_v1.md"},
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
	if last.InputArtifactURIs[0] != task.ArtifactURIs[0] {
		t.Fatalf("input artifact = %q, want %q", last.InputArtifactURIs[0], task.ArtifactURIs[0])
	}
	if last.OutputArtifactURIs[0] != feedback.ArtifactURIs[0] {
		t.Fatalf("output artifact = %q, want %q", last.OutputArtifactURIs[0], feedback.ArtifactURIs[0])
	}
}

func TestExecuteWriteRequirementCopiesInputArtifact(t *testing.T) {
	store := &memoryStore{
		files: map[string]string{
			"projects/run1/agents/user/artifacts/requirement/input.md": "# Requirement\n\nKeep original content.\n",
		},
	}
	agent := &Agent{
		agentID:       "ceo",
		runID:         "run1",
		artifactStore: store,
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_write_plan",
		AgentID:      "ceo",
		Op:           core.TaskOpWritePlan,
		ArtifactURIs: []string{"projects/run1/agents/user/artifacts/requirement/input.md"},
	}

	feedback, err := agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	outputURI := "projects/run1/agents/ceo/artifacts/requirement/requirement_v1.md"
	if got := feedback.ArtifactURIs[0]; got != outputURI {
		t.Fatalf("feedback output = %q, want %q", got, outputURI)
	}
	if got := store.writes[outputURI]; got != store.files[task.ArtifactURIs[0]] {
		t.Fatalf("written content = %q, want copied input content %q", got, store.files[task.ArtifactURIs[0]])
	}
}
