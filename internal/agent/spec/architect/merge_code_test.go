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
