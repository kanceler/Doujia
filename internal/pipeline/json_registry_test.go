package pipeline

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLoadJSONRegistryLooksUpDefinitions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	registry, err := LoadJSONRegistry(path)
	if err != nil {
		t.Fatalf("LoadJSONRegistry() error = %v", err)
	}

	entry, err := registry.Entry(ctx)
	if err != nil {
		t.Fatalf("Entry() error = %v", err)
	}
	if entry.PipelineID != "pipeline_full_delivery" {
		t.Fatalf("entry pipeline id = %q, want pipeline_full_delivery", entry.PipelineID)
	}

	codePipeline, err := registry.GetDef(ctx, "pipeline_write_code")
	if err != nil {
		t.Fatalf("GetDef(pipeline_write_code) error = %v", err)
	}
	if codePipeline.Signature.Params[0] != "module_key" {
		t.Fatalf("first param = %q, want module_key", codePipeline.Signature.Params[0])
	}

	_, err = registry.GetDef(ctx, "missing_pipeline")
	if err == nil {
		t.Fatalf("GetDef(missing_pipeline) error = nil, want error")
	}
}

func TestNewLegacyRegistryFromSpecCompilesLinearTaskPipeline(t *testing.T) {
	ctx := context.Background()
	spec, err := ParseRegistrySpec([]byte(linearRegistryJSON))
	if err != nil {
		t.Fatalf("ParseRegistrySpec() error = %v", err)
	}
	registry, err := NewLegacyRegistryFromSpec(spec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}

	legacy, err := registry.Get(ctx, "linear_entry")
	if err != nil {
		t.Fatalf("Get(linear_entry) error = %v", err)
	}
	if legacy.ID != "linear_entry" || legacy.Name != "Linear Entry" {
		t.Fatalf("legacy identity = %s/%s, want linear_entry/Linear Entry", legacy.ID, legacy.Name)
	}
	if got, want := len(legacy.Stages), 2; got != want {
		t.Fatalf("stage count = %d, want %d", got, want)
	}
	first := legacy.Stages[0]
	if first.ID != "ceo_write_requirement" || first.AgentRole != "ceo" || first.AgentAlias != "ceo" || first.Op != "write_plan" || !first.External {
		t.Fatalf("first stage = %+v, want external ceo write_plan", first)
	}
	second := legacy.Stages[1]
	if second.ID != "pm_write_plan" || second.AgentRole != "pm" || second.AgentAlias != "pm01" || second.Op != "write_plan" {
		t.Fatalf("second stage = %+v, want pm write_plan", second)
	}
	if len(second.DependsOnIDs) != 1 || second.DependsOnIDs[0] != "ceo_write_requirement" {
		t.Fatalf("second dependencies = %v, want [ceo_write_requirement]", second.DependsOnIDs)
	}
}

func TestLegacyAdapterCompilesFullDeliveryTaskPrefix(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	registry, err := NewLegacyRegistryFromSpec(spec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(full delivery) error = %v", err)
	}
	legacy, err := registry.Get(ctx, "pipeline_full_delivery")
	if err != nil {
		t.Fatalf("Get(pipeline_full_delivery) error = %v", err)
	}
	wantStages := []string{
		"ceo_write_requirement",
		"pm_write_plan",
		"ceo_review_product_plan",
		"architect_write_plan",
		"pm_review_architecture",
		"architect_create_container",
		"split_module",
	}
	if got, want := len(legacy.Stages), len(wantStages); got != want {
		t.Fatalf("stage count = %d, want %d", got, want)
	}
	for i, want := range wantStages {
		if string(legacy.Stages[i].ID) != want {
			t.Fatalf("stage[%d] = %s, want %s", i, legacy.Stages[i].ID, want)
		}
	}
	if !legacy.Stages[0].External {
		t.Fatalf("first stage should be external")
	}
	if got := legacy.Stages[len(legacy.Stages)-1].Op; got != "split_module" {
		t.Fatalf("last op = %s, want split_module", got)
	}
}

const linearRegistryJSON = `{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "linear_registry",
  "entry_pipeline_id": "linear_entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "linear_entry",
      "kind": "entry",
      "name": "Linear Entry",
      "namespace": {
        "agents": [
          {"name": "ceo", "role": "ceo", "default_agent_id": "ceo"},
          {"name": "pm01", "role": "pm", "default_agent_id": "pm01"}
        ]
      },
      "start_state": "delivery_start",
      "delivery_state": "product_plan_ready",
      "states": [
        {
          "id": "delivery_start",
          "kind": "start",
          "name": "delivery started",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["ceo_write_requirement"]}
        },
        {
          "id": "requirement_ready",
          "kind": "state",
          "name": "CEO 交付需求包",
          "proof": {"type": "transition_result", "transition": "ceo_write_requirement"},
          "next": {"type": "all", "transitions": ["pm_write_plan"]}
        },
        {
          "id": "product_plan_ready",
          "kind": "delivery",
          "name": "PM 交付产品计划包",
          "proof": {"type": "transition_result", "transition": "pm_write_plan"}
        }
      ],
      "transitions": [
        {
          "id": "ceo_write_requirement",
          "kind": "task",
          "from_state": "delivery_start",
          "to_state": "requirement_ready",
          "agent": {"role": "ceo", "alias": "ceo"},
          "op": "write_plan",
          "output_bags": [{"name": "requirement", "required": true}]
        },
        {
          "id": "pm_write_plan",
          "kind": "task",
          "from_state": "requirement_ready",
          "to_state": "product_plan_ready",
          "agent": {"role": "pm", "alias": "pm01"},
          "op": "write_plan",
          "input_bags": [{"name": "requirement", "from_state": "requirement_ready"}],
          "output_bags": [{"name": "product_plan", "required": true}]
        }
      ]
    }
  ]
}`
