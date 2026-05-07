package pipeline

import (
	"strings"
	"testing"
)

func TestValidateRegistrySpecRequiresDeliveryExposeForSignatureOutputBag(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "child",
  "pipeline_defs": [{
    "schema_version": "devflow.pipeline/v0.4",
    "pipeline_id": "child",
    "kind": "entry",
    "signature": {
      "output_bags": [{"name": "child_out"}]
    },
    "start_state": "start",
    "delivery_state": "done",
    "states": [
      {"id": "start", "kind": "start", "proof": {"type": "external"}},
      {
        "id": "done",
        "kind": "delivery",
        "proof": {"type": "external"},
        "exposes": {"bags": [{"name": "other_out"}]}
      }
    ],
    "transitions": []
  }]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want delivery return mismatch")
	}
	if !strings.Contains(err.Error(), "delivery_state.exposes.bags") || !strings.Contains(err.Error(), "signature.output_bags") || !strings.Contains(err.Error(), "child_out") {
		t.Fatalf("ParseRegistrySpec() error = %v, want delivery/signature child_out mismatch", err)
	}
}

func TestValidateRegistrySpecRejectsCallOutputBagNotDeclaredByCalledPipeline(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "parent",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "child",
      "kind": "subpipeline",
      "signature": {
        "output_bags": [{"name": "child_out"}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "external"},
          "exposes": {"bags": [{"name": "child_out"}]}
        }
      ],
      "transitions": []
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "parent",
      "kind": "entry",
      "signature": {
        "output_bags": [{"name": "parent_out", "indexed_by": ["module_key"]}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}, "next": {"type": "all", "transitions": ["call_child"]}},
        {"id": "done", "kind": "delivery", "proof": {"type": "transition_result", "transition": "call_child"}}
      ],
      "transitions": [{
        "id": "call_child",
        "kind": "call",
        "pipeline_id": "child",
        "from_state": "start",
        "to_state": "done",
        "mode": "single",
        "output_bags": [{"name": "wrong_out"}]
      }]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want undeclared call return")
	}
	if !strings.Contains(err.Error(), "transition.output_bags") || !strings.Contains(err.Error(), "wrong_out") || !strings.Contains(err.Error(), "child_out") {
		t.Fatalf("ParseRegistrySpec() error = %v, want call output bag mismatch", err)
	}
}

func TestParseRegistrySpecAllowsCallOutputBagRenameFromReturn(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "parent",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "child",
      "kind": "subpipeline",
      "signature": {
        "output_bags": [{"name": "child_out", "indexed_by": ["module_key"]}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "external"},
          "exposes": {"bags": [{"name": "child_out", "indexed_by": ["module_key"]}]}
        }
      ],
      "transitions": []
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "parent",
      "kind": "entry",
      "signature": {
        "output_bags": [{"name": "parent_out", "indexed_by": ["module_key"]}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}, "next": {"type": "all", "transitions": ["call_child"]}},
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "call_child"},
          "exposes": {"bags": [{"name": "parent_out", "from_transition": "call_child", "indexed_by": ["module_key"]}]}
        }
      ],
      "transitions": [{
        "id": "call_child",
        "kind": "call",
        "pipeline_id": "child",
        "from_state": "start",
        "to_state": "done",
        "mode": "single",
        "output_bags": [{"name": "parent_out", "from_return": "child_out", "indexed_by": ["module_key"]}]
      }]
    }
  ]
}`)
	spec, err := ParseRegistrySpec(raw)
	if err != nil {
		t.Fatalf("ParseRegistrySpec() error = %v", err)
	}
	parent, ok := spec.Pipeline("parent")
	if !ok {
		t.Fatalf("parent pipeline not found")
	}
	if got := parent.Transitions[0].OutputBags[0].FromReturn; got != "child_out" {
		t.Fatalf("from_return = %q, want child_out", got)
	}
}
