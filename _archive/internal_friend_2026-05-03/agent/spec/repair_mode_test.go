package spec_test

import (
	"testing"

	"doujia/internal/agent/core"
	architectspec "doujia/internal/agent/spec/architect"
	coderspec "doujia/internal/agent/spec/coder"
	testerspec "doujia/internal/agent/spec/tester"
)

func TestFirstBatchSpecsRequireRepairInstructionInRepairMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec core.OpSpec
	}{
		{name: "coder.write_code", spec: coderspec.WriteCodeSpec()},
		{name: "tester.test_code", spec: testerspec.TestCodeSpec()},
		{name: "architect.merge_code", spec: architectspec.MergeCodeSpec()},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, ok := tt.spec.ModeInputRules[core.ExecutionModeNormal]; !ok {
				t.Fatalf("%s missing normal mode", tt.name)
			}
			repair, ok := tt.spec.ModeInputRules[core.ExecutionModeRepair]
			if !ok {
				t.Fatalf("%s missing repair mode", tt.name)
			}
			if len(repair.ExtraRequiredInputs) != 1 {
				t.Fatalf("%s repair required inputs len = %d, want 1", tt.name, len(repair.ExtraRequiredInputs))
			}
			if got := repair.ExtraRequiredInputs[0].LogicalKey; got != core.LKRepairInstruction {
				t.Fatalf("%s repair required input = %q, want %q", tt.name, got, core.LKRepairInstruction)
			}
		})
	}
}
