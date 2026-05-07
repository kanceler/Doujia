package architect

import (
	"os"
	"path/filepath"
	"testing"

	"devflow/internal/agent/core"
)

func TestResolveSplitModuleOutputsDefaultsToOneBackendModule(t *testing.T) {
	t.Parallel()

	outputs, err := SplitModuleSpec().ResolveExpectedOutputs(core.AgentInputBundle{})
	if err != nil {
		t.Fatalf("ResolveExpectedOutputs() error = %v", err)
	}
	if len(outputs) != 11 {
		t.Fatalf("ResolveExpectedOutputs() len = %d, want 11", len(outputs))
	}
	if _, found := findOutput(outputs, core.ModuleSpecKey("module02")); !found {
		t.Fatal("ResolveExpectedOutputs() missing module02_spec")
	}
	if _, found := findOutput(outputs, core.ModuleSpecKey("module03")); found {
		t.Fatal("ResolveExpectedOutputs() unexpectedly included module03_spec")
	}
}

func TestResolveSplitModuleOutputsExpandsBackendModules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "run_delivery_config.json")
	if err := os.WriteFile(configPath, []byte(`{"backend_module_count":2}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	outputs, err := SplitModuleSpec().ResolveExpectedOutputs(core.AgentInputBundle{
		Inputs: []core.InputArtifact{
			{LogicalKey: core.LKRunDeliveryConfig, Path: configPath},
		},
	})
	if err != nil {
		t.Fatalf("ResolveExpectedOutputs() error = %v", err)
	}
	if len(outputs) != 16 {
		t.Fatalf("ResolveExpectedOutputs() len = %d, want 16", len(outputs))
	}
	for _, key := range []string{
		core.ModuleSpecKey("module03"),
		core.ModuleCoderTaskKey("module03"),
		core.ModuleTesterTaskKey("module03"),
		core.ModuleContractKey("module03"),
		core.ModuleSeedTestsKey("module03"),
	} {
		if _, found := findOutput(outputs, key); !found {
			t.Fatalf("ResolveExpectedOutputs() missing %s", key)
		}
	}
}

func findOutput(outputs []core.OutputSpec, logicalKey string) (core.OutputSpec, bool) {
	for _, output := range outputs {
		if output.LogicalKey == logicalKey {
			return output, true
		}
	}
	return core.OutputSpec{}, false
}
