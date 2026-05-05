package tester

import (
	"testing"

	core "devflow/internal/agent/core"
	appcore "devflow/internal/core"
)

func TestTestCodeProducedBagInheritsModuleInputIndexes(t *testing.T) {
	spec := TestCodeSpec()
	result := core.AgentResult{
		Result: string(appcore.TaskResultCodeOK),
		Outputs: []core.AgentOutput{{
			LogicalKey:  core.LKModuleTestReport,
			Status:      "produced",
			ArtifactURI: "projects/run/agents/tester01/artifacts/test_code/module_test_report.json",
		}},
	}

	bags := spec.ResolveProducedBags(core.Task{Role: "tester", Op: "test_code"}, core.AgentInputBundle{
		Bags: []core.AgentInputBag{{
			Name:    "module_input",
			BagID:   "bag_module01",
			Indexes: map[string]string{"module_key": "module01"},
		}},
	}, result)

	if got, want := len(bags), 1; got != want {
		t.Fatalf("produced bags = %#v, want %d item", bags, want)
	}
	if got := bags[0].Name; got != "tested_module" {
		t.Fatalf("produced bag name = %q, want tested_module", got)
	}
	if got := bags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("produced bag indexes = %#v, want module_key module01", bags[0].Indexes)
	}
}
