package tester

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"devflow/internal/core"
	"devflow/internal/llm"
)

type memoryArtifactStore struct {
	reads  map[string]string
	writes map[string]string
}

func (s *memoryArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	content, ok := s.reads[uri]
	if !ok {
		return nil, fmt.Errorf("missing read artifact %s", uri)
	}
	return []byte(content), nil
}

func (s *memoryArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[uri] = string(content)
	return nil
}

type fakeLLMClient struct {
	response string
	prompt   string
	err      error
}

func (c *fakeLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	c.prompt = prompt
	return c.response, c.err
}

func TestExecuteTestDataGeneratesFallbackFromTaskMetadataArtifacts(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	store := &memoryArtifactStore{
		reads: map[string]string{
			testerTaskURI: "# Tester Task\n\n## Pairing Metadata\n\n- paired_coder_agent: coder_01\n- module_task_uri: " + moduleTaskURI + "\n",
			moduleTaskURI: "# Coder Task\n\n## Pairing Metadata\n\n- module_id: engine\n- paired_tester_agent: tester_01\n",
		},
	}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("tester_01"),
		artifactStore: store,
		llmClient:     llm.NoopClient{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     core.RunID("run1"),
		TaskID:    core.TaskID("task_test_data_01"),
		AgentID:   core.AgentID("tester_01"),
		Op:        "test_data",
		ArtifactURIs: []string{
			testerTaskURI,
			moduleTaskURI,
		},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(feedback.ArtifactURIs), 1; got != want {
		t.Fatalf("artifact uri count = %d, want %d", got, want)
	}
	outputURI := feedback.ArtifactURIs[0]
	if !strings.Contains(outputURI, "/artifacts/test_data/tester_01_test_data.md") {
		t.Fatalf("output uri = %s, want tester test_data artifact", outputURI)
	}
	output := store.writes[outputURI]
	for _, want := range []string{
		"# Test Data",
		testerTaskURI,
		moduleTaskURI,
		"paired_coder_agent: coder_01",
		"Unit Test Data",
		"Boundary And Error Data",
		"Acceptance Matrix",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("fallback output missing %q:\n%s", want, output)
		}
	}
}

func TestExecuteTestDataUsesModelOutput(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	moduleTaskURI := "projects/run1/agents/architect01/artifacts/modules/coder_01_task.md"
	store := &memoryArtifactStore{
		reads: map[string]string{
			testerTaskURI: "# Tester Task\n",
			moduleTaskURI: "# Coder Task\n",
		},
	}
	model := &fakeLLMClient{
		response: `{"summary":"generated","artifact_outputs":[{"type":"test_data","filename":"ignored.md","content":"# Model Test Data\n\ncustom model cases"}]}`,
	}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("tester_01"),
		artifactStore: store,
		llmClient:     model,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_test_data_01"),
		AgentID:      core.AgentID("tester_01"),
		Op:           "test_data",
		ArtifactURIs: []string{testerTaskURI, moduleTaskURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := feedback.ArtifactURIs[0], "projects/run1/agents/tester_01/artifacts/test_data/tester_01_test_data.md"; got != want {
		t.Fatalf("output uri = %s, want %s", got, want)
	}
	if !strings.Contains(store.writes[feedback.ArtifactURIs[0]], "custom model cases") {
		t.Fatalf("model output was not written:\n%s", store.writes[feedback.ArtifactURIs[0]])
	}
	if !strings.Contains(model.prompt, "# Output JSON Schema") {
		t.Fatalf("prompt missing output schema:\n%s", model.prompt)
	}
}

func TestExecuteTestDataFailsWithoutPairedModuleTask(t *testing.T) {
	testerTaskURI := "projects/run1/agents/architect01/artifacts/tests/tester_01_task.md"
	store := &memoryArtifactStore{
		reads: map[string]string{
			testerTaskURI: "# Tester Task\n",
		},
	}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("tester_01"),
		artifactStore: store,
		llmClient:     llm.NoopClient{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        core.RunID("run1"),
		TaskID:       core.TaskID("task_test_data_01"),
		AgentID:      core.AgentID("tester_01"),
		Op:           "test_data",
		ArtifactURIs: []string{testerTaskURI},
	})
	if err != nil {
		t.Fatalf("execute test_data: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if len(feedback.ArtifactURIs) != 1 {
		t.Fatalf("failure artifact count = %d, want 1", len(feedback.ArtifactURIs))
	}
	if !strings.Contains(store.writes[feedback.ArtifactURIs[0]], "requires an artifact URI containing /artifacts/modules/") {
		t.Fatalf("failure output missing module requirement:\n%s", store.writes[feedback.ArtifactURIs[0]])
	}
}
