package real

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentbootstrap "devflow/internal/agent/bootstrap"
	agentcore "devflow/internal/agent/core"
	"devflow/internal/core"
	"devflow/internal/runtime"
)

func TestCloneProducedBagsCopiesResolvedProducedBags(t *testing.T) {
	defs := cloneProducedBags([]core.ProducedBagManifest{
		{
			Name:    "module_input",
			Indexes: map[string]string{"module_key": "module01"},
			Members: []core.ProducedBagMember{
				{LogicalKey: agentcore.ModuleSpecKey("module01"), OutputRef: "projects/run/agents/architect01/artifacts/split/module01_spec.json"},
			},
		},
	})

	if len(defs) != 1 {
		t.Fatalf("cloneProducedBags() len = %d, want 1", len(defs))
	}
	if got := defs[0].Name; got != "module_input" {
		t.Fatalf("Name = %q, want module_input", got)
	}
	if got := defs[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("Indexes = %#v, want module_key module01", defs[0].Indexes)
	}
}

func TestResultControlsDoesNotInferSplitModuleControls(t *testing.T) {
	controls := resultControls(agentcore.AgentResult{
		Result: "kok",
		Outputs: []agentcore.AgentOutput{
			{LogicalKey: agentcore.ModuleSpecKey("module01"), Status: "produced"},
		},
	})

	if len(controls) != 0 {
		t.Fatalf("resultControls() = %#v, want no inferred controls", controls)
	}
}

func TestInputsFromArtifactURIsRecognizesRunDeliveryConfig(t *testing.T) {
	inputs := inputsFromArtifactURIs("D:\\run", []string{"projects/run/system/run_delivery_config.json"})
	if len(inputs) != 1 {
		t.Fatalf("inputs len = %d, want 1", len(inputs))
	}
	if inputs[0].LogicalKey != agentcore.LKRunDeliveryConfig {
		t.Fatalf("logical key = %q, want %q", inputs[0].LogicalKey, agentcore.LKRunDeliveryConfig)
	}
}

func TestAliasModuleScopedInputsAddsGenericModuleKeys(t *testing.T) {
	inputs := aliasModuleScopedInputs([]agentcore.InputArtifact{
		{LogicalKey: agentcore.ModuleSpecKey("module01"), Path: "module01_spec.json", ArtifactVersionID: "version:spec"},
		{LogicalKey: agentcore.ModuleCoderTaskKey("module01"), Path: "coder_task.md", ArtifactVersionID: "version:coder"},
		{LogicalKey: agentcore.ModuleTesterTaskKey("module01"), Path: "tester_task.md", ArtifactVersionID: "version:tester"},
		{LogicalKey: agentcore.ModuleContractKey("module01"), Path: "contract.json", ArtifactVersionID: "version:contract"},
		{LogicalKey: agentcore.ModuleSeedTestsKey("module01"), Path: "seed_tests.json", ArtifactVersionID: "version:seed"},
	})

	for _, want := range []string{
		agentcore.LKModuleSpec,
		agentcore.LKCoderTask,
		agentcore.LKTesterTask,
		agentcore.LKModuleContract,
		agentcore.LKSeedTests,
	} {
		if !hasLogicalKey(inputs, want) {
			t.Fatalf("aliasModuleScopedInputs() missing logical key %q in %+v", want, inputs)
		}
	}
}

func TestFactoryWithRuntimeOptionsSurfacesPluginLoadError(t *testing.T) {
	badRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(badRoot, "broken_pack"), 0o755); err != nil {
		t.Fatalf("mkdir broken pack: %v", err)
	}

	agent := NewFactoryWithRuntimeOptions(core.AgentRolePM, agentbootstrap.RuntimeOptions{
		PluginRoots: []string{badRoot},
	}).Create(runtime.AgentInit{}, runtime.AgentDeps{})

	_, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want plugin load error")
	}
	if !strings.Contains(err.Error(), "plugin.json") {
		t.Fatalf("Execute() error = %q, want missing plugin.json detail", err)
	}
}

func TestFactoryUsesRunLLMConfigForRuntimeOptions(t *testing.T) {
	agent := NewFactoryWithRuntimeOptions(core.AgentRolePM, agentbootstrap.RuntimeOptions{}).Create(runtime.AgentInit{
		RunID:   "run_real_llm_config",
		AgentID: "pm01",
		RunConfig: core.RunConfig{
			LLM: core.LLMRunConfig{
				Provider: "openai",
				Model:    "gpt-5.5",
				APIKey:   "run-key",
				BaseURL:  "https://api.example.test/v1",
				APIStyle: "chat_completions",
			},
		},
	}, runtime.AgentDeps{})

	realAgent, ok := agent.(*Agent)
	if !ok {
		t.Fatalf("Create() = %T, want *Agent", agent)
	}
	if realAgent.runtimeOptions.LLMConfig.Model != "gpt-5.5" {
		t.Fatalf("runtime LLM model = %q, want gpt-5.5", realAgent.runtimeOptions.LLMConfig.Model)
	}
	if realAgent.runtimeOptions.LLMConfig.APIKey != "run-key" {
		t.Fatalf("runtime LLM API key = %q, want run-key", realAgent.runtimeOptions.LLMConfig.APIKey)
	}
	if realAgent.runtimeOptions.LLMConfig.BaseURL != "https://api.example.test/v1" {
		t.Fatalf("runtime LLM base URL = %q, want run base URL", realAgent.runtimeOptions.LLMConfig.BaseURL)
	}
	if realAgent.runtimeOptions.LLMConfig.APIStyle != "chat_completions" {
		t.Fatalf("runtime LLM API style = %q, want chat_completions", realAgent.runtimeOptions.LLMConfig.APIStyle)
	}
}

func hasLogicalKey(inputs []agentcore.InputArtifact, want string) bool {
	for _, input := range inputs {
		if input.LogicalKey == want {
			return true
		}
	}
	return false
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasProducedBagMember(items []core.ProducedBagMember, logicalKey string, artifactVersionID string) bool {
	for _, item := range items {
		if item.LogicalKey != logicalKey {
			continue
		}
		if artifactVersionID == "" || item.ArtifactVersionID == artifactVersionID {
			return true
		}
	}
	return false
}
