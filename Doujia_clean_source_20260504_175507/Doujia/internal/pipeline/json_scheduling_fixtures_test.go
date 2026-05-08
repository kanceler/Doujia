package pipeline

import (
	"path/filepath"
	"testing"

	"devflow/internal/core"
)

func TestSchedulingLinearJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(filepath.Join("..", "orchestrator", "testdata", "pipelines", "scheduling_linear.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(linear) error = %v", err)
	}
	if spec.EntryPipelineID != "scheduling_linear" {
		t.Fatalf("entry pipeline = %s, want scheduling_linear", spec.EntryPipelineID)
	}
	def, ok := spec.Pipeline("scheduling_linear")
	if !ok {
		t.Fatalf("scheduling_linear definition missing")
	}
	if len(def.Transitions) != 2 {
		t.Fatalf("transition count = %d, want 2", len(def.Transitions))
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(linear) error = %v", err)
	}
	legacy, err := NewLegacyRegistryFromSpec(spec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(linear) error = %v", err)
	}
	compiled, err := legacy.Get(t.Context(), "scheduling_linear")
	if err != nil {
		t.Fatalf("legacy.Get(scheduling_linear) error = %v", err)
	}
	if got, want := len(compiled.Stages), 2; got != want {
		t.Fatalf("compiled stages = %d, want %d", got, want)
	}
}

func TestSchedulingParallelMergeJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(filepath.Join("..", "orchestrator", "testdata", "pipelines", "scheduling_parallel_merge.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(parallel merge) error = %v", err)
	}
	if spec.EntryPipelineID != "parallel_merge" {
		t.Fatalf("entry pipeline = %s, want parallel_merge", spec.EntryPipelineID)
	}
	def, ok := spec.Pipeline("parallel_merge")
	if !ok {
		t.Fatalf("parallel_merge definition missing")
	}
	if len(def.Transitions) != 3 {
		t.Fatalf("transition count = %d, want 3", len(def.Transitions))
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(parallel merge) error = %v", err)
	}
	if _, err := NewLegacyRegistryFromSpec(spec); err == nil {
		t.Fatalf("NewLegacyRegistryFromSpec(parallel merge) error = nil, want non-linear graph outside legacy adapter")
	}
}

func TestSchedulingCallReturnJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(filepath.Join("..", "orchestrator", "testdata", "pipelines", "scheduling_call_return.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(call return) error = %v", err)
	}
	if spec.EntryPipelineID != "parent_call_return" {
		t.Fatalf("entry pipeline = %s, want parent_call_return", spec.EntryPipelineID)
	}
	if _, ok := spec.Pipeline("child_write_code"); !ok {
		t.Fatalf("child_write_code definition missing")
	}
	parent, ok := spec.Pipeline("parent_call_return")
	if !ok {
		t.Fatalf("parent_call_return definition missing")
	}
	if len(parent.Transitions) != 2 {
		t.Fatalf("parent transition count = %d, want 2", len(parent.Transitions))
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(call return) error = %v", err)
	}
	if _, err := NewLegacyRegistryFromSpec(spec); err == nil {
		t.Fatalf("NewLegacyRegistryFromSpec(call return) error = nil, want call graph outside legacy adapter")
	}
}

func TestSchedulingRecoverRepairJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(filepath.Join("..", "orchestrator", "testdata", "pipelines", "scheduling_recover_repair.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(recover repair) error = %v", err)
	}
	if spec.EntryPipelineID != "parent_recover_repair" {
		t.Fatalf("entry pipeline = %s, want parent_recover_repair", spec.EntryPipelineID)
	}
	for _, id := range []string{"owner_repairable_code", "checker_throws_bug", "parent_recover_repair"} {
		if _, ok := spec.Pipeline(core.PipelineID(id)); !ok {
			t.Fatalf("%s definition missing", id)
		}
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(recover repair) error = %v", err)
	}
	if _, err := NewLegacyRegistryFromSpec(spec); err == nil {
		t.Fatalf("NewLegacyRegistryFromSpec(recover repair) error = nil, want call/recover graph outside legacy adapter")
	}
}

func TestSchedulingPartialRepairSiblingJSONFixtureLoads(t *testing.T) {
	spec, err := LoadRegistrySpec(filepath.Join("..", "orchestrator", "testdata", "pipelines", "scheduling_partial_repair_sibling.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(partial repair sibling) error = %v", err)
	}
	if spec.EntryPipelineID != "parent_partial_repair" {
		t.Fatalf("entry pipeline = %s, want parent_partial_repair", spec.EntryPipelineID)
	}
	for _, id := range []string{"owner_repairable_code", "test_data_ready", "checker_uses_code_and_data", "parent_partial_repair"} {
		if _, ok := spec.Pipeline(core.PipelineID(id)); !ok {
			t.Fatalf("%s definition missing", id)
		}
	}
	if _, err := NewJSONRegistry(spec); err != nil {
		t.Fatalf("NewJSONRegistry(partial repair sibling) error = %v", err)
	}
	if _, err := NewLegacyRegistryFromSpec(spec); err == nil {
		t.Fatalf("NewLegacyRegistryFromSpec(partial repair sibling) error = nil, want call/recover graph outside legacy adapter")
	}
}
