package pipeline

import (
	"path/filepath"
	"testing"

	"devflow/internal/core"
)

func TestNewFullDeliveryJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(fullDeliveryRegistryPath())
	if err != nil {
		t.Fatalf("LoadRegistrySpec(new full delivery) error = %v", err)
	}
	if spec.EntryPipelineID != "pipeline_full_delivery" {
		t.Fatalf("entry pipeline = %s, want pipeline_full_delivery", spec.EntryPipelineID)
	}
	for _, id := range []string{
		"pipeline_full_delivery",
		"pipeline_write_code",
		"pipeline_write_test_data",
		"pipeline_test_code",
		"pipeline_module",
		"pipeline_backend_module_group",
		"pipeline_front_write_code",
		"pipeline_front_test_code",
		"pipeline_front_preview_review",
		"pipeline_front_module",
		"pipeline_global_test_data",
		"pipeline_merge_code",
		"pipeline_global_test_code",
	} {
		if _, ok := spec.Pipeline(core.PipelineID(id)); !ok {
			t.Fatalf("%s definition missing", id)
		}
	}
	testCode, ok := spec.Pipeline("pipeline_test_code")
	if !ok {
		t.Fatalf("pipeline_test_code definition missing")
	}
	if len(testCode.Signature.ExportedHandlers) == 0 {
		t.Fatalf("pipeline_test_code exported handlers missing")
	}
	module, ok := spec.Pipeline("pipeline_module")
	if !ok {
		t.Fatalf("pipeline_module definition missing")
	}
	if _, ok := stateByIDForTest(module.States, "module_test_input_ready"); !ok {
		t.Fatalf("pipeline_module module_test_input_ready aggregate missing")
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(new full delivery) error = %v", err)
	}
	legacy, err := NewLegacyRegistryFromSpec(spec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(new full delivery) error = %v", err)
	}
	compiled, err := legacy.Get(t.Context(), "pipeline_full_delivery")
	if err != nil {
		t.Fatalf("legacy.Get(pipeline_full_delivery) error = %v", err)
	}
	if len(compiled.Stages) == 0 {
		t.Fatalf("compiled legacy stages = 0, want top-level task prefix")
	}
}

func fullDeliveryRegistryPath() string {
	return filepath.Join("..", "orchestrator", "testdata", "full_delivery", "pipeline_full_delivery.spec.json")
}

func stateByIDForTest(states []StateSpec, id string) (StateSpec, bool) {
	for _, state := range states {
		if state.ID == id {
			return state, true
		}
	}
	return StateSpec{}, false
}
