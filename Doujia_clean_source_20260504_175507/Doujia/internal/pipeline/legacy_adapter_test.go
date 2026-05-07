package pipeline

import (
	"strings"
	"testing"

	"devflow/internal/core"
)

func TestCompileLegacyPipelineDefRejectsDynamicTailInStrictMode(t *testing.T) {
	def := PipelineDefSpec{
		PipelineID:    "delivery",
		Name:          "Delivery",
		StartState:    "start",
		DeliveryState: "done",
		SchemaVersion: PipelineSchemaVersionV04,
		Namespace:     NamespaceSpec{Agents: []SignatureAgentSpec{{Name: "pm", Role: core.AgentRolePM}}},
		States:        []StateSpec{{ID: "start", Next: &NextSpec{Type: "all", Transitions: []string{"write_plan"}}}, {ID: "after_plan", Next: &NextSpec{Type: "all", Transitions: []string{"call_modules"}}}, {ID: "done"}},
		Transitions:   []TransitionSpec{{ID: "write_plan", Kind: "task", FromState: "start", ToState: "after_plan", Agent: &AgentSpec{Role: core.AgentRolePM, Alias: "pm"}, Op: "write_plan"}, {ID: "call_modules", Kind: "call", FromState: "after_plan", ToState: "done", PipelineID: "module", Mode: "single"}},
		Signature:     SignatureSpec{},
	}

	if _, err := CompileLegacyPipelineDef(def); err == nil || !strings.Contains(err.Error(), "unsupported first kind") {
		t.Fatalf("CompileLegacyPipelineDef() error = %v, want strict dynamic-tail rejection", err)
	}
	got, err := CompileLegacyPipelineDefPrefix(def)
	if err != nil {
		t.Fatalf("CompileLegacyPipelineDefPrefix() error = %v", err)
	}
	if len(got.Stages) != 1 || got.Stages[0].ID != "write_plan" {
		t.Fatalf("prefix stages = %+v, want only write_plan", got.Stages)
	}
}

func TestCompileLegacyPipelineDefNormalizesNamespaceBagAliases(t *testing.T) {
	def := PipelineDefSpec{
		PipelineID:    "delivery",
		Name:          "Delivery",
		StartState:    "start",
		DeliveryState: "done",
		SchemaVersion: PipelineSchemaVersionV04,
		Namespace: NamespaceSpec{
			Agents: []SignatureAgentSpec{{Name: "pm", Role: core.AgentRolePM}},
			Bags:   []BagSpec{{Name: "requirement"}},
		},
		States: []StateSpec{
			{ID: "start", Next: &NextSpec{Type: "all", Transitions: []string{"write_plan"}}},
			{ID: "done"},
		},
		Transitions: []TransitionSpec{
			{
				ID:        "write_plan",
				Kind:      "task",
				FromState: "start",
				ToState:   "done",
				Agent:     &AgentSpec{Role: core.AgentRolePM, Alias: "pm"},
				Op:        "write_plan",
				OutputBags: []BagSpec{
					{Name: "requirement"},
				},
			},
		},
	}

	got, err := CompileLegacyPipelineDef(def)
	if err != nil {
		t.Fatalf("CompileLegacyPipelineDef() error = %v", err)
	}
	if len(got.Stages) != 1 || got.Stages[0].ID != "write_plan" {
		t.Fatalf("stages = %+v, want write_plan", got.Stages)
	}
}

func TestCompileLegacyPipelineDefPreservesTaskBagDeclarations(t *testing.T) {
	def := PipelineDefSpec{
		PipelineID:    "delivery",
		Name:          "Delivery",
		StartState:    "start",
		DeliveryState: "done",
		SchemaVersion: PipelineSchemaVersionV04,
		Namespace:     NamespaceSpec{Agents: []SignatureAgentSpec{{Name: "pm", Role: core.AgentRolePM}}},
		States: []StateSpec{
			{ID: "start", Next: &NextSpec{Type: "all", Transitions: []string{"write_plan"}}},
			{ID: "done"},
		},
		Transitions: []TransitionSpec{
			{
				ID:        "write_plan",
				Kind:      "task",
				FromState: "start",
				ToState:   "done",
				Agent:     &AgentSpec{Role: core.AgentRolePM, Alias: "pm"},
				Op:        "write_plan",
				InputBags:  []BagSpec{{Name: "requirement"}},
				OutputBags: []BagSpec{{Name: "product_plan"}},
			},
		},
	}

	got, err := CompileLegacyPipelineDef(def)
	if err != nil {
		t.Fatalf("CompileLegacyPipelineDef() error = %v", err)
	}
	if len(got.Stages) != 1 {
		t.Fatalf("stages = %+v, want one stage", got.Stages)
	}
	if got.Stages[0].InputBags[0].Name != "requirement" {
		t.Fatalf("stage input bags = %+v, want requirement", got.Stages[0].InputBags)
	}
	if got.Stages[0].OutputBags[0].Name != "product_plan" {
		t.Fatalf("stage output bags = %+v, want product_plan", got.Stages[0].OutputBags)
	}
}
