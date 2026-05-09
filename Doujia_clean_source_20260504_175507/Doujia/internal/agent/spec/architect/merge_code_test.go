package architect

import (
	"testing"

	"devflow/internal/agent/core"
)

func TestMergeCodeSpecDeclaresOutputBagContract(t *testing.T) {
	t.Parallel()

	spec := MergeCodeSpec()
	if len(spec.OutputBags) != 1 {
		t.Fatalf("MergeCodeSpec().OutputBags len = %d, want 1", len(spec.OutputBags))
	}
	bag := spec.OutputBags[0]
	if bag.Name != "merged_code" {
		t.Fatalf("MergeCodeSpec().OutputBags[0].Name = %q, want merged_code", bag.Name)
	}
	if !bag.Required {
		t.Fatal("MergeCodeSpec().OutputBags[0].Required = false, want true")
	}

	got := map[string]bool{}
	required := map[string]bool{}
	for _, member := range bag.Members {
		got[member.LogicalKey] = member.Required
	}

	wantRequired := map[string]bool{
		core.LKMergedCodeSummary:     true,
		core.LKMergedMainBranch:      true,
		core.LKMergedMainBranchNote:  true,
		core.LKMergeCodeReport:       false,
		core.LKUpstreamArtifactIssue: false,
	}
	for key, want := range wantRequired {
		if _, ok := got[key]; !ok {
			t.Fatalf("MergeCodeSpec().OutputBags[0] missing member %q", key)
		}
		required[key] = got[key]
		if got[key] != want {
			t.Fatalf("MergeCodeSpec().OutputBags[0].Members[%q].Required = %v, want %v", key, got[key], want)
		}
	}
	if len(got) != len(wantRequired) {
		t.Fatalf("MergeCodeSpec().OutputBags[0].Members len = %d, want %d", len(got), len(wantRequired))
	}
}

func TestMergeCodeSpecAcceptsFullDeliveryFrontBackendAliases(t *testing.T) {
	t.Parallel()

	spec := MergeCodeSpec()
	got := map[string]bool{}
	for _, bag := range spec.InputBags {
		got[bag.Name] = bag.Collection
	}

	want := map[string]bool{
		"front_tested_module":   false,
		"backend_tested_module": true,
		"front_code_bag":        false,
		"backend_code_bag":      true,
		"container_context":     false,
		"global_test_input":     false,
	}
	for name, wantCollection := range want {
		gotCollection, ok := got[name]
		if !ok {
			t.Fatalf("MergeCodeSpec().InputBags missing %q; got %+v", name, got)
		}
		if gotCollection != wantCollection {
			t.Fatalf("MergeCodeSpec().InputBags[%q].Collection = %v, want %v", name, gotCollection, wantCollection)
		}
	}
}
