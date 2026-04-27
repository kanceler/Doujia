package pm

import (
	"context"
	"strings"
	"testing"

	"devflow/internal/core"
)

type replanMemoryStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *replanMemoryStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *replanMemoryStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

type replanLLM struct {
	prompt string
}

func (c *replanLLM) Complete(_ context.Context, prompt string) (string, error) {
	c.prompt = prompt
	return `{"summary":"replanned","artifact_outputs":[{"type":"prd","filename":"plan_v2.md","content":"# 新产品计划\n\n已根据 CEO review 修订。"}],"control":[]}`, nil
}

func TestExecuteReplanUsesRequirementReviewAndPreviousPlan(t *testing.T) {
	requirementURI := "projects/run1/agents/ceo/artifacts/requirement/requirement_v1.md"
	reviewURI := "projects/run1/agents/ceo/artifacts/review/replan_instruction.md"
	previousPlanURI := "projects/run1/agents/pm01/artifacts/prd/plan_v1.md"
	store := &replanMemoryStore{
		files: map[string]string{
			requirementURI:  "# 用户需求书\n\n做一个支持暂停的贪吃蛇。",
			reviewURI:       "# CEO Review\n\n请补充暂停后的恢复验收标准。",
			previousPlanURI: "# 旧产品计划\n\n已有开始游戏和计分要求。",
		},
	}
	llmClient := &replanLLM{}
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
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "pm01",
		Op:           "replan",
		ArtifactURIs: []string{requirementURI, reviewURI},
	}

	feedback, err := agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("feedback artifact count = %d, want %d", got, want)
	}
	outputURI := "projects/run1/agents/pm01/artifacts/prd/plan_v2.md"
	if feedback.ArtifactURIs[0] != outputURI {
		t.Fatalf("feedback artifact = %q, want %q", feedback.ArtifactURIs[0], outputURI)
	}
	if got := store.writes[outputURI]; !strings.Contains(got, "# 新产品计划") {
		t.Fatalf("written output missing model content: %q", got)
	}
	for _, want := range []string{
		"做一个支持暂停的贪吃蛇",
		"请补充暂停后的恢复验收标准",
		"已有开始游戏和计分要求",
		"输出必须是一份完整的新产品计划",
	} {
		if !strings.Contains(llmClient.prompt, want) {
			t.Fatalf("prompt missing %q\nprompt:\n%s", want, llmClient.prompt)
		}
	}
	if got, want := len(agent.taskHistory), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
	if last := agent.taskHistory[1]; last.Op != "replan" || len(last.OutputArtifactURIs) != 1 || last.OutputArtifactURIs[0] != outputURI {
		t.Fatalf("last history = %+v, want replan output %s", last, outputURI)
	}
}

func TestExecuteReplanRequiresReviewArtifact(t *testing.T) {
	requirementURI := "projects/run1/agents/ceo/artifacts/requirement/requirement_v1.md"
	agent := &Agent{
		agentID: "pm01",
		runID:   "run1",
		artifactStore: &replanMemoryStore{
			files: map[string]string{requirementURI: "# 用户需求书"},
		},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "pm01",
		Op:           "replan",
		ArtifactURIs: []string{requirementURI},
	}

	if _, err := agent.Execute(context.Background(), task); err == nil {
		t.Fatal("Execute returned nil error, want missing review error")
	}
}
