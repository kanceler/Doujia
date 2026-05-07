package orchestrator

import (
	"strings"
	"testing"

	"devflow/internal/core"
	"devflow/internal/pipeline"
)

func TestOutputBagIDsFromCommitUsesResultSpecificDeclarations(t *testing.T) {
	transition := pipeline.TransitionSpec{
		ID: "test_code",
		OutputBagsByResult: map[string][]pipeline.BagSpec{
			"kok":  {{Name: "tested_module"}},
			"kbug": {{Name: "failure_report"}},
		},
	}
	commit := &core.CommitReceipt{
		Result:       core.TaskResultCodeBug,
		ProducedBags: []core.CommittedBagDef{{Name: "failure_report"}},
	}

	got, err := outputBagIDsFromCommit(transition, core.TaskResultCodeBug, commit, []string{"bag_failure"})
	if err != nil {
		t.Fatalf("outputBagIDsFromCommit() error = %v", err)
	}
	if got["failure_report"] != "bag_failure" {
		t.Fatalf("output bags = %#v, want failure_report mapped to bag_failure", got)
	}
	if got["tested_module"] != "" {
		t.Fatalf("output bags = %#v, did not want tested_module on kbug branch", got)
	}
}

func TestOutputBagIDsFromCommitRejectsBagOutsideResultBranch(t *testing.T) {
	transition := pipeline.TransitionSpec{
		ID: "test_code",
		OutputBagsByResult: map[string][]pipeline.BagSpec{
			"kok":  {{Name: "tested_module"}},
			"kbug": {{Name: "failure_report"}},
		},
	}
	commit := &core.CommitReceipt{
		Result:       core.TaskResultCodeBug,
		ProducedBags: []core.CommittedBagDef{{Name: "tested_module"}},
	}

	_, err := outputBagIDsFromCommit(transition, core.TaskResultCodeBug, commit, []string{"bag_tested"})
	if err == nil {
		t.Fatalf("outputBagIDsFromCommit() error = nil, want undeclared branch output rejected")
	}
	if !strings.Contains(err.Error(), "tested_module") || !strings.Contains(err.Error(), "kbug") {
		t.Fatalf("outputBagIDsFromCommit() error = %v, want tested_module/kbug details", err)
	}
}
