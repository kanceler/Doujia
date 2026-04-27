package architect

import (
	"context"
	"strings"
	"testing"

	"devflow/internal/core"
)

type memoryArtifactStore struct {
	writes map[string]string
}

func (s *memoryArtifactStore) Read(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (s *memoryArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = map[string]string{}
	}
	s.writes[uri] = string(content)
	return nil
}

func TestParseRunSplitConfigRejectsMismatchedCoderTesterCounts(t *testing.T) {
	_, err := parseRunSplitConfig(`{
		"main_branch": "main",
		"coder_agents": 2,
		"tester_agents": 1,
		"max_modules": 2
	}`)
	if err == nil {
		t.Fatal("expected mismatched coder/tester counts to be rejected")
	}
	if !strings.Contains(err.Error(), "tester_agents must equal coder_agents") {
		t.Fatalf("expected equality error, got %v", err)
	}
}

func TestNormalizeSplitPlanPairsCoderAndTesterByIndex(t *testing.T) {
	cfg := runSplitConfig{
		MainBranch:   "main",
		CoderAgents:  2,
		TesterAgents: 2,
		MaxModules:   2,
		AgentNamePrefix: map[string]string{
			"coder":  "coder",
			"tester": "tester",
		},
	}
	plan := normalizeSplitPlan(splitModulePlan{
		Modules: []splitModuleDefinition{
			{ID: "engine", Name: "Engine", Goal: "Implement engine"},
			{ID: "renderer", Name: "Renderer", Goal: "Implement renderer"},
		},
	}, cfg)

	if got, want := len(plan.CoderTasks), 2; got != want {
		t.Fatalf("coder task count = %d, want %d", got, want)
	}
	if got, want := len(plan.TesterTasks), 2; got != want {
		t.Fatalf("tester task count = %d, want %d", got, want)
	}
	pairs := []struct {
		coder  splitAgentTaskDocument
		tester splitAgentTaskDocument
		module string
	}{
		{plan.CoderTasks[0], plan.TesterTasks[0], "engine"},
		{plan.CoderTasks[1], plan.TesterTasks[1], "renderer"},
	}
	for i, pair := range pairs {
		index := i + 1
		if got, want := pair.coder.AgentName, "coder_0"+string(rune('0'+index)); got != want {
			t.Fatalf("coder task %d agent = %s, want %s", index, got, want)
		}
		if got, want := pair.tester.AgentName, "tester_0"+string(rune('0'+index)); got != want {
			t.Fatalf("tester task %d agent = %s, want %s", index, got, want)
		}
		if pair.coder.ModuleID != pair.module {
			t.Fatalf("coder task %d module = %s, want %s", index, pair.coder.ModuleID, pair.module)
		}
		if pair.tester.ModuleID != pair.module {
			t.Fatalf("tester task %d module = %s, want %s", index, pair.tester.ModuleID, pair.module)
		}
		if len(pair.tester.ModuleIDs) != 0 {
			t.Fatalf("tester task %d ModuleIDs = %v, want empty for one-to-one pairing", index, pair.tester.ModuleIDs)
		}
	}
}

func TestBuildSplitControlsPassesPairedCoderTaskToTesterWithoutMainBranch(t *testing.T) {
	plan := splitModulePlan{
		CoderTasks: []splitAgentTaskDocument{
			{AgentName: "coder_01", ModuleID: "engine"},
		},
		TesterTasks: []splitAgentTaskDocument{
			{AgentName: "tester_01", ModuleID: "engine"},
		},
	}
	taskURIs := map[string]string{
		"coder_01":  "projects/run/agents/architect/artifacts/modules/coder_01_task.md",
		"tester_01": "projects/run/agents/architect/artifacts/tests/tester_01_task.md",
	}
	mainBranchURI := "projects/run/agents/architect/artifacts/branches/main_branch.md"

	controls, err := buildSplitControls(plan, taskURIs, mainBranchURI)
	if err != nil {
		t.Fatalf("build controls: %v", err)
	}

	var testerControl core.Control
	for _, control := range controls {
		if control.Type == core.ControlTypeNewTester {
			testerControl = control
			break
		}
	}
	if testerControl.AgentName != "tester_01" {
		t.Fatalf("tester control agent = %q, want tester_01", testerControl.AgentName)
	}
	wantURIs := []string{taskURIs["tester_01"], taskURIs["coder_01"]}
	if got := strings.Join(testerControl.ArtifactURIs, "|"); got != strings.Join(wantURIs, "|") {
		t.Fatalf("tester artifact uris = %v, want %v", testerControl.ArtifactURIs, wantURIs)
	}
}

func TestWriteSplitTaskArtifactsEmbedsPairingMetadata(t *testing.T) {
	store := &memoryArtifactStore{}
	agent := &Agent{
		runID:         core.RunID("run1"),
		agentID:       core.AgentID("architect01"),
		artifactStore: store,
	}
	inputs := splitModuleInputs{
		basisURIs: []string{
			"projects/run1/agents/architect01/artifacts/design/architecture_v1.md",
			"projects/run1/agents/architect01/artifacts/config/run_config.md",
		},
	}
	plan := splitModulePlan{
		CoderTasks: []splitAgentTaskDocument{
			{AgentName: "coder_01", ModuleID: "engine", Content: "# Coder\n"},
		},
		TesterTasks: []splitAgentTaskDocument{
			{AgentName: "tester_01", ModuleID: "engine", Content: "# Tester\n"},
		},
	}

	taskURIs, err := agent.writeSplitTaskArtifacts(context.Background(), inputs, plan, "projects/run1/agents/architect01/artifacts/branches/main_branch.md")
	if err != nil {
		t.Fatalf("write split artifacts: %v", err)
	}

	coderContent := store.writes[taskURIs["coder_01"]]
	if !strings.Contains(coderContent, "- paired_tester_agent: tester_01") {
		t.Fatalf("coder artifact missing paired tester metadata:\n%s", coderContent)
	}

	testerContent := store.writes[taskURIs["tester_01"]]
	if !strings.Contains(testerContent, "- paired_coder_agent: coder_01") {
		t.Fatalf("tester artifact missing paired coder metadata:\n%s", testerContent)
	}
	if !strings.Contains(testerContent, "- module_task_uri: "+taskURIs["coder_01"]) {
		t.Fatalf("tester artifact missing paired module task uri:\n%s", testerContent)
	}
}
