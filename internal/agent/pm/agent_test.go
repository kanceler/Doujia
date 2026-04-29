package pm

import (
	"context"
	"strings"
	"testing"

	"devflow/internal/core"
)

type testArtifactStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *testArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *testArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

type testLLM struct {
	prompt string
}

func (c *testLLM) Complete(_ context.Context, prompt string) (string, error) {
	c.prompt = prompt
	return `{"summary":"replanned","artifact_outputs":[{"type":"prd","filename":"plan_v2.md","content":"# Product Plan v2\n\nUpdated according to review.\n"}],"control":[]}`, nil
}

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
	if len(feedback.ArtifactURIs) > 0 && last.OutputArtifactURIs[0] != feedback.ArtifactURIs[0] {
		t.Fatalf("output artifact = %q, want %q", last.OutputArtifactURIs[0], feedback.ArtifactURIs[0])
	}
}

func TestExecuteReplanUsesRequirementReviewAndPreviousPlan(t *testing.T) {
	requirementURI := "projects/run1/agents/ceo/artifacts/requirement/requirement_v1.md"
	reviewURI := "projects/run1/agents/ceo/artifacts/review/replan_instruction.md"
	previousPlanURI := "projects/run1/agents/pm01/artifacts/prd/plan_v1.md"
	store := &testArtifactStore{
		files: map[string]string{
			requirementURI:  "# Requirement\n\nAdd pause and resume support.\n",
			reviewURI:       "# CEO Review\n\nClarify recovery acceptance criteria.\n",
			previousPlanURI: "# Product Plan v1\n\nExisting start game and scoring flow.\n",
		},
	}
	llmClient := &testLLM{}
	agent := &Agent{
		agentID:       "pm01",
		runID:         "run1",
		artifactStore: store,
		llmClient:     llmClient,
		taskHistory: []core.AgentTaskHistory{
			{
				RunID:              "run1",
				TaskID:             "task_write_plan",
				AgentID:            "pm01",
				Op:                 "pm_write_plan",
				Status:             core.TaskStatusDone,
				OutputArtifactURIs: []string{previousPlanURI},
			},
		},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "pm01",
		Op:           core.TaskOpReplan,
		ArtifactURIs: []string{requirementURI, reviewURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	outputURI := "projects/run1/agents/pm01/artifacts/prd/plan_v2.md"
	if got := feedback.ArtifactURIs[0]; got != outputURI {
		t.Fatalf("feedback artifact = %q, want %q", got, outputURI)
	}
	if got := store.writes[outputURI]; !strings.Contains(got, "# Product Plan v2") {
		t.Fatalf("written output missing model content:\n%s", got)
	}
	for _, want := range []string{
		"Add pause and resume support.",
		"Clarify recovery acceptance criteria.",
		"Existing start game and scoring flow.",
	} {
		if !strings.Contains(llmClient.prompt, want) {
			t.Fatalf("prompt missing %q\nprompt:\n%s", want, llmClient.prompt)
		}
	}
	if got, want := len(agent.taskHistory), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
}

func TestExecuteReviewPlanRejectsWhenDesignMissesPRDItems(t *testing.T) {
	prdURI := "projects/run1/agents/pm01/artifacts/prd/plan_v1.md"
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	store := &testArtifactStore{
		files: map[string]string{
			prdURI: `# Product Plan

Acceptance criteria must be explicit.
API design is required.
Error handling matters.
Data model must be defined.
`,
			designURI: "# Architecture\n\nVery short design without the required detail.\n",
		},
	}
	agent := &Agent{
		agentID:       "pm01",
		runID:         "run1",
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_review",
		AgentID:      "pm01",
		Op:           core.TaskOpReviewPlan,
		ArtifactURIs: []string{prdURI, designURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeReplan {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeReplan)
	}
	outputURI := "projects/run1/agents/pm01/artifacts/review/replan_instruction.md"
	if got := feedback.ArtifactURIs[0]; got != outputURI {
		t.Fatalf("feedback artifact = %q, want %q", got, outputURI)
	}
	report := store.writes[outputURI]
	for _, want := range []string{
		"# PM Review Replan Instruction",
		"acceptance criteria",
		"API or interface design",
		"error handling",
		"data model",
		prdURI,
		designURI,
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("review report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteReviewPlanPassesThroughWhenArtifactsAreIncomplete(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nOnly design is available.\n",
		},
	}
	agent := &Agent{
		agentID:       "pm01",
		runID:         "run1",
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_review",
		AgentID:      "pm01",
		Op:           core.TaskOpReviewPlan,
		ArtifactURIs: []string{designURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got := strings.Join(feedback.ArtifactURIs, "|"); got != designURI {
		t.Fatalf("pass-through artifacts = %v, want original artifact", feedback.ArtifactURIs)
	}
}
