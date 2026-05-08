package pipeline

import (
	"strings"
	"testing"
)

func TestLoadRegistrySpecFullDeliveryJSON(t *testing.T) {
	path := fullDeliveryRegistryPath()
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	if spec.SchemaVersion != RegistrySchemaVersionV04 {
		t.Fatalf("schema version = %q, want %q", spec.SchemaVersion, RegistrySchemaVersionV04)
	}
	if spec.EntryPipelineID != "pipeline_full_delivery" {
		t.Fatalf("entry pipeline = %q, want pipeline_full_delivery", spec.EntryPipelineID)
	}
	if got, want := len(spec.PipelineDefs), 13; got != want {
		t.Fatalf("pipeline defs = %d, want %d", got, want)
	}
	entry, ok := spec.Pipeline("pipeline_full_delivery")
	if !ok {
		t.Fatalf("entry pipeline not found")
	}
	if entry.StartState != "delivery_start" || entry.DeliveryState != "delivery_done" {
		t.Fatalf("entry start/delivery = %s/%s", entry.StartState, entry.DeliveryState)
	}
	if !hasTransition(entry, "run_backend_module_group", "call") {
		t.Fatalf("entry pipeline should contain run_backend_module_group call transition")
	}
	if !hasTransition(entry, "architect_create_container", "task") {
		t.Fatalf("entry pipeline should contain architect_create_container task transition")
	}
	modulePipeline, ok := spec.Pipeline("pipeline_module")
	if !ok {
		t.Fatalf("pipeline_module not found")
	}
	if modulePipeline.StartState != "module_input_ready" || modulePipeline.DeliveryState != "module_tested" {
		t.Fatalf("pipeline_module start/delivery = %s/%s", modulePipeline.StartState, modulePipeline.DeliveryState)
	}
}

func TestFullDeliveryMergeCodeCarriesRequiredContext(t *testing.T) {
	path := fullDeliveryRegistryPath()
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	mergePipeline, ok := spec.Pipeline("pipeline_merge_code")
	if !ok {
		t.Fatalf("pipeline_merge_code not found")
	}
	if !hasBag(mergePipeline.Signature.InputBags, "container_context") {
		t.Fatalf("pipeline_merge_code signature input bags = %+v, want container_context", mergePipeline.Signature.InputBags)
	}
	if !hasBag(mergePipeline.Signature.InputBags, "global_test_input") {
		t.Fatalf("pipeline_merge_code signature input bags = %+v, want global_test_input", mergePipeline.Signature.InputBags)
	}
	if !hasBag(mergePipeline.Signature.InputBags, "backend_code_bag") {
		t.Fatalf("pipeline_merge_code signature input bags = %+v, want backend_code_bag", mergePipeline.Signature.InputBags)
	}
	mergeTransition, ok := transitionByID(mergePipeline, "merge_code")
	if !ok {
		t.Fatalf("pipeline_merge_code.merge_code transition not found")
	}
	if !hasBag(mergeTransition.InputBags, "container_context") {
		t.Fatalf("pipeline_merge_code.merge_code input bags = %+v, want container_context", mergeTransition.InputBags)
	}
	if !hasBag(mergeTransition.InputBags, "global_test_input") {
		t.Fatalf("pipeline_merge_code.merge_code input bags = %+v, want global_test_input", mergeTransition.InputBags)
	}
	if !hasBag(mergeTransition.InputBags, "backend_code_bag") {
		t.Fatalf("pipeline_merge_code.merge_code input bags = %+v, want backend_code_bag", mergeTransition.InputBags)
	}

	entry, ok := spec.Pipeline("pipeline_full_delivery")
	if !ok {
		t.Fatalf("pipeline_full_delivery not found")
	}
	rootMerge, ok := transitionByID(entry, "merge_code")
	if !ok {
		t.Fatalf("pipeline_full_delivery.merge_code transition not found")
	}
	if rootMerge.Bindings == nil {
		t.Fatalf("pipeline_full_delivery.merge_code bindings = nil, want container_context binding")
	}
	if _, ok := rootMerge.Bindings.InputBags["container_context"]; !ok {
		t.Fatalf("pipeline_full_delivery.merge_code input bag bindings = %+v, want container_context", rootMerge.Bindings.InputBags)
	}
	if _, ok := rootMerge.Bindings.InputBags["global_test_input"]; !ok {
		t.Fatalf("pipeline_full_delivery.merge_code input bag bindings = %+v, want global_test_input", rootMerge.Bindings.InputBags)
	}
	if _, ok := rootMerge.Bindings.InputBags["backend_code_bag"]; !ok {
		t.Fatalf("pipeline_full_delivery.merge_code input bag bindings = %+v, want backend_code_bag", rootMerge.Bindings.InputBags)
	}
}

func TestValidateRegistrySpecRejectsMissingCallBinding(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "sub",
      "kind": "subpipeline",
      "signature": {
        "params": ["module_key"],
        "input_bags": [{"name": "module_input"}],
        "output_bags": [{"name": "code_bag"}]
      },
      "start_state": "module_input_ready",
      "delivery_state": "code_written",
      "states": [
        {
          "id": "module_input_ready",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["write_code"]}
        },
        {
          "id": "code_written",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "write_code"}
        }
      ],
      "transitions": [
        {
          "id": "write_code",
          "kind": "task",
          "from_state": "module_input_ready",
          "to_state": "code_written",
          "agent": {"role": "coder", "alias": "${agent_alias}"},
          "op": "write_code",
          "input_bags": [{"name": "module_input"}],
          "output_bags": [{"name": "code_bag"}]
        }
      ]
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {
          "id": "start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["call_sub"]}
        },
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "call_sub"}
        }
      ],
      "transitions": [
        {
          "id": "call_sub",
          "kind": "call",
          "pipeline_id": "sub",
          "from_state": "start",
          "to_state": "done",
          "mode": "single",
          "bindings": {
            "input_bags": {"module_input": "bag_01"}
          }
        }
      ]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want missing param binding")
	}
	if !strings.Contains(err.Error(), "bindings.params.module_key") {
		t.Fatalf("error = %v, want missing module_key binding", err)
	}
}

func TestValidateRegistrySpecRejectsUnknownInheritedAgentAlias(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "sub",
      "kind": "subpipeline",
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {
          "id": "start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["write_code"]}
        },
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "write_code"}
        }
      ],
      "transitions": [
        {
          "id": "write_code",
          "kind": "task",
          "from_state": "start",
          "to_state": "done",
          "agent": {"role": "coder", "alias": "coder"},
          "op": "write_code"
        }
      ]
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "architect", "role": "architect", "default_agent_id": "architect01"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {
          "id": "start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["call_sub"]}
        },
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "call_sub"}
        }
      ],
      "transitions": [
        {
          "id": "call_sub",
          "kind": "call",
          "pipeline_id": "sub",
          "from_state": "start",
          "to_state": "done",
          "mode": "single"
        }
      ]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want unknown inherited agent alias")
	}
	if !strings.Contains(err.Error(), `task.agent.alias "coder"`) {
		t.Fatalf("error = %v, want unknown coder alias", err)
	}
}

func TestParseRegistrySpecUsesNameOnlyNamespaceBags(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "ceo", "role": "ceo", "default_agent_id": "ceo"}
        ],
        "bags": [
          {"name": "requirement"},
          {"name": "product_plan"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {
          "id": "start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["write_requirement"]}
        },
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "write_requirement"}
        }
      ],
      "transitions": [
        {
          "id": "write_requirement",
          "kind": "task",
          "from_state": "start",
          "to_state": "done",
          "agent": {"role": "ceo", "alias": "ceo"},
          "op": "write_plan",
          "output_bags": [
            {"name": "requirement"},
            {"name": "product_plan"}
          ]
        }
      ]
    }
  ]
}`)
	spec, err := ParseRegistrySpec(raw)
	if err != nil {
		t.Fatalf("ParseRegistrySpec() error = %v", err)
	}
	entry, ok := spec.Pipeline("entry")
	if !ok {
		t.Fatalf("entry pipeline not found")
	}
	if got, want := entry.Namespace.Bags[0].Name, "requirement"; got != want {
		t.Fatalf("namespace.bags[0].name = %q, want %q", got, want)
	}
	if got, want := entry.Namespace.Bags[1].Name, "product_plan"; got != want {
		t.Fatalf("namespace.bags[1].name = %q, want %q", got, want)
	}
	if got, want := entry.Transitions[0].OutputBags[0].Name, "requirement"; got != want {
		t.Fatalf("transition output bag name = %q, want %q", got, want)
	}
}

func TestValidateRegistrySpecAllowsNamespaceBagWithNameOnly(t *testing.T) {
	spec := RegistrySpec{
		SchemaVersion:   RegistrySchemaVersionV04,
		RegistryID:      "test_registry",
		EntryPipelineID: "entry",
		PipelineDefs: []PipelineDefSpec{
			{
				SchemaVersion: PipelineSchemaVersionV04,
				PipelineID:    "entry",
				Kind:          "entry",
				Namespace: NamespaceSpec{
					Agents: []SignatureAgentSpec{{Name: "ceo", Role: "ceo"}},
					Bags:   []BagSpec{{Name: "requirement"}},
				},
				StartState:    "start",
				DeliveryState: "done",
				States: []StateSpec{
					{ID: "start", Kind: "start", Proof: ProofSpec{Type: "external"}},
					{ID: "done", Kind: "delivery", Proof: ProofSpec{Type: "external"}},
				},
				Transitions: []TransitionSpec{},
			},
		},
	}

	if err := ValidateRegistrySpec(spec); err != nil {
		t.Fatalf("ValidateRegistrySpec() error = %v, want name-only namespace bag accepted", err)
	}
}

func TestParseRegistrySpecRejectsLegacyBagAliasOnlyBag(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "ceo", "role": "ceo", "default_agent_id": "ceo"}
        ],
        "bags": [
          {"bag_role": "requirement"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "external"}}
      ],
      "transitions": []
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want bag_role-only bag rejected")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("ParseRegistrySpec() error = %v, want name is required", err)
	}
}

func TestValidateRegistrySpecRejectsReferenceToUnknownNamespaceBag(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "ceo", "role": "ceo", "default_agent_id": "ceo"}
        ],
        "bags": [
          {"name": "requirement"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {
          "id": "start",
          "kind": "start",
          "proof": {"type": "external"},
          "next": {"type": "all", "transitions": ["write_requirement"]}
        },
        {
          "id": "done",
          "kind": "delivery",
          "proof": {"type": "transition_result", "transition": "write_requirement"}
        }
      ],
      "transitions": [
        {
          "id": "write_requirement",
          "kind": "task",
          "from_state": "start",
          "to_state": "done",
          "agent": {"role": "ceo", "alias": "ceo"},
          "op": "write_plan",
          "output_bags": [
            {"name": "product_plan"}
          ]
        }
      ]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want unknown namespace bag")
	}
	if !strings.Contains(err.Error(), "product_plan") {
		t.Fatalf("error = %v, want product_plan reference failure", err)
	}
}

func TestParseRegistrySpecSupportsOutputBagsByResult(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "tester", "role": "tester", "default_agent_id": "tester"}
        ],
        "bags": [
          {"name": "module_input"},
          {"name": "tested_module"},
          {"name": "failure_report"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "transition_result", "transition": "test_code"}}
      ],
      "transitions": [
        {
          "id": "test_code",
          "kind": "task",
          "from_state": "start",
          "to_state": "done",
          "agent": {"role": "tester", "alias": "tester"},
          "op": "test_code",
          "input_bags": [{"name": "module_input"}],
          "output_bags_by_result": {
            "kok": [{"name": "tested_module", "required": true}],
            "kbug": [{"name": "failure_report", "required": true}]
          }
        }
      ]
    }
  ]
}`)
	spec, err := ParseRegistrySpec(raw)
	if err != nil {
		t.Fatalf("ParseRegistrySpec() error = %v", err)
	}
	entry, ok := spec.Pipeline("entry")
	if !ok {
		t.Fatalf("entry pipeline not found")
	}
	got := entry.Transitions[0].OutputBagsByResult
	if got["kok"][0].Name != "tested_module" {
		t.Fatalf("kok output bag = %#v, want tested_module", got["kok"])
	}
	if got["kbug"][0].Name != "failure_report" {
		t.Fatalf("kbug output bag = %#v, want failure_report", got["kbug"])
	}
}

func TestParseRegistrySpecRejectsUnknownOutputBagByResultReference(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "entry",
      "kind": "entry",
      "namespace": {
        "agents": [
          {"name": "tester", "role": "tester", "default_agent_id": "tester"}
        ],
        "bags": [
          {"name": "module_input"},
          {"name": "tested_module"}
        ]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "transition_result", "transition": "test_code"}}
      ],
      "transitions": [
        {
          "id": "test_code",
          "kind": "task",
          "from_state": "start",
          "to_state": "done",
          "agent": {"role": "tester", "alias": "tester"},
          "op": "test_code",
          "input_bags": [{"name": "module_input"}],
          "output_bags_by_result": {
            "kbug": [{"name": "failure_report", "required": true}]
          }
        }
      ]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want unknown output_bags_by_result reference")
	}
	if !strings.Contains(err.Error(), "output_bags_by_result") || !strings.Contains(err.Error(), "failure_report") {
		t.Fatalf("ParseRegistrySpec() error = %v, want output_bags_by_result failure_report reference", err)
	}
}

func hasTransition(def PipelineDefSpec, id string, kind string) bool {
	for _, transition := range def.Transitions {
		if transition.ID == id && transition.Kind == kind {
			return true
		}
	}
	return false
}

func transitionByID(def PipelineDefSpec, id string) (TransitionSpec, bool) {
	for _, transition := range def.Transitions {
		if transition.ID == id {
			return transition, true
		}
	}
	return TransitionSpec{}, false
}

func hasBag(bags []BagSpec, name string) bool {
	for _, bag := range bags {
		if bag.Name == name {
			return true
		}
	}
	return false
}
