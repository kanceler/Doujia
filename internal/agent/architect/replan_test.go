package architect

import (
	"context"
	"strings"
	"testing"

	"devflow/internal/core"
)

type architectReplanStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *architectReplanStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *architectReplanStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

type architectReplanLLM struct {
	prompt string
}

func (c *architectReplanLLM) Complete(_ context.Context, prompt string) (string, error) {
	c.prompt = prompt
	return `{"summary":"replanned architecture","artifact_outputs":[{"type":"design","filename":"architecture_v2.md","content":"# 新架构设计\n\n已根据 review 修订。"}],"control":[]}`, nil
}

func TestExecuteReplanUsesPRDReviewAndPreviousDesign(t *testing.T) {
	prdURI := "projects/run1/agents/pm01/artifacts/prd/plan_v2.md"
	reviewURI := "projects/run1/agents/pm01/artifacts/review/replan_instruction.md"
	previousDesignURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	store := &architectReplanStore{
		files: map[string]string{
			prdURI:            "# 产品计划\n\n需要支持暂停和恢复。",
			reviewURI:         "# Review\n\n请补充暂停状态机和恢复流程。",
			previousDesignURI: "# 旧架构设计\n\n已有 GameLoop 和 Renderer 模块。",
		},
	}
	llmClient := &architectReplanLLM{}
	agent := &Agent{
		agentID:       "architect01",
		runID:         "run1",
		artifactStore: store,
		llmClient:     llmClient,
		taskHistory: []core.AgentTaskHistory{
			{
				RunID:              "run1",
				TaskID:             "task_architecture_generation",
				AgentID:            "architect01",
				Op:                 "architecture_generation",
				Status:             core.TaskStatusDone,
				OutputArtifactURIs: []string{previousDesignURI},
			},
		},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "architect01",
		Op:           "replan",
		ArtifactURIs: []string{prdURI, reviewURI},
	}

	feedback, err := agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	outputURI := "projects/run1/agents/architect01/artifacts/design/architecture_v2.md"
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("feedback artifact count = %d, want %d", got, want)
	}
	if feedback.ArtifactURIs[0] != outputURI {
		t.Fatalf("feedback artifact = %q, want %q", feedback.ArtifactURIs[0], outputURI)
	}
	if got := store.writes[outputURI]; !strings.Contains(got, "# 新架构设计") {
		t.Fatalf("written output missing model content: %q", got)
	}
	for _, want := range []string{
		"需要支持暂停和恢复",
		"请补充暂停状态机和恢复流程",
		"已有 GameLoop 和 Renderer 模块",
		"输出必须是一份完整的新架构设计书",
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
	prdURI := "projects/run1/agents/pm01/artifacts/prd/plan_v2.md"
	agent := &Agent{
		agentID: "architect01",
		runID:   "run1",
		artifactStore: &architectReplanStore{
			files: map[string]string{prdURI: "# 产品计划"},
		},
	}
	task := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "architect01",
		Op:           "replan",
		ArtifactURIs: []string{prdURI},
	}

	if _, err := agent.Execute(context.Background(), task); err == nil {
		t.Fatal("Execute returned nil error, want missing review error")
	}
}
