package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/core"
)

func TestLoadRegistrySpecFullDeliveryJSON(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
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
	if got, want := len(spec.PipelineDefs), 16; got != want {
		t.Fatalf("pipeline defs = %d, want %d", got, want)
	}
	entry, ok := spec.Pipeline("pipeline_full_delivery")
	if !ok {
		t.Fatalf("entry pipeline not found")
	}
	if entry.StartState != "delivery_start" || entry.DeliveryState != "delivery_done" {
		t.Fatalf("entry start/delivery = %s/%s", entry.StartState, entry.DeliveryState)
	}
	if !hasTransition(entry, "global_test_code", "call") {
		t.Fatalf("entry pipeline should contain global_test_code call transition")
	}
	if !hasTransition(entry, "create_container", "call") {
		t.Fatalf("entry pipeline should contain create_container call transition")
	}
	modulePipeline, ok := spec.Pipeline("pipeline_backend_module")
	if !ok {
		t.Fatalf("pipeline_backend_module not found")
	}
	if modulePipeline.StartState != "module_input_ready" || modulePipeline.DeliveryState != "module_tested" {
		t.Fatalf("pipeline_backend_module start/delivery = %s/%s", modulePipeline.StartState, modulePipeline.DeliveryState)
	}
}

func TestFullDeliveryMergeCodeCarriesRequiredContext(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
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
	if !hasBag(mergePipeline.Signature.InputBags, "code_bag") {
		t.Fatalf("pipeline_merge_code signature input bags = %+v, want code_bag", mergePipeline.Signature.InputBags)
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
	if !hasBag(mergeTransition.InputBags, "code_bag") {
		t.Fatalf("pipeline_merge_code.merge_code input bags = %+v, want code_bag", mergeTransition.InputBags)
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
	if _, ok := rootMerge.Bindings.InputBags["code_bag"]; !ok {
		t.Fatalf("pipeline_full_delivery.merge_code input bag bindings = %+v, want code_bag", rootMerge.Bindings.InputBags)
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

func TestFullDeliveryV2DefinesRequiredPipelines(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}

	required := []string{
		"pipeline_create_container",
		"pipeline_split_modules",
		"pipeline_backend_module",
		"pipeline_front_module_slot",
		"pipeline_front_module",
		"pipeline_global_test_data",
		"pipeline_merge_code",
		"pipeline_global_test_code",
		"pipeline_delivery_review",
		"pipeline_full_delivery",
	}

	for _, id := range required {
		if _, ok := spec.Pipeline(core.PipelineID(id)); !ok {
			t.Fatalf("required pipeline %q not found", id)
		}
	}
}

func requirePipeline(t *testing.T, spec RegistrySpec, id core.PipelineID) PipelineDefSpec {
	t.Helper()
	def, ok := spec.Pipeline(id)
	if !ok {
		t.Fatalf("pipeline %q not found", id)
	}
	return def
}

func requireState(t *testing.T, def PipelineDefSpec, id string) StateSpec {
	t.Helper()
	for _, state := range def.States {
		if state.ID == id {
			return state
		}
	}
	t.Fatalf("pipeline %q state %q not found", def.PipelineID, id)
	return StateSpec{}
}

func requireTransition(t *testing.T, def PipelineDefSpec, id string) TransitionSpec {
	t.Helper()
	for _, transition := range def.Transitions {
		if transition.ID == id {
			return transition
		}
	}
	t.Fatalf("pipeline %q transition %q not found", def.PipelineID, id)
	return TransitionSpec{}
}

func TestFullDeliveryV2MainFlowDependencies(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	entry := requirePipeline(t, spec, "pipeline_full_delivery")

	launchParallel := requireTransition(t, entry, "architect_launch_parallel_work")
	if launchParallel.Kind != "task" {
		t.Fatalf("architect_launch_parallel_work.kind = %q, want task", launchParallel.Kind)
	}
	if launchParallel.Op == "" {
		t.Fatalf("architect_launch_parallel_work.op is empty, want fanout op")
	}

	createContainer := requireTransition(t, entry, "create_container")
	if createContainer.Kind != "call" {
		t.Fatalf("create_container.kind = %q, want call", createContainer.Kind)
	}
	if createContainer.PipelineID != "pipeline_create_container" {
		t.Fatalf("create_container.pipeline_id = %q, want pipeline_create_container", createContainer.PipelineID)
	}

	splitModules := requireTransition(t, entry, "split_modules")
	if splitModules.Kind != "call" {
		t.Fatalf("split_modules.kind = %q, want call", splitModules.Kind)
	}
	if splitModules.PipelineID != "pipeline_split_modules" {
		t.Fatalf("split_modules.pipeline_id = %q, want pipeline_split_modules", splitModules.PipelineID)
	}

	developmentReady := requireState(t, entry, "development_ready")
	if developmentReady.Kind != "aggregate" || developmentReady.Proof.Mode != "all" {
		t.Fatalf("development_ready = %+v, want aggregate all", developmentReady)
	}
	if !containsString(developmentReady.Proof.States, "container_ready") {
		t.Fatalf("development_ready states = %v, want container_ready", developmentReady.Proof.States)
	}
	if !containsString(developmentReady.Proof.States, "modules_split") {
		t.Fatalf("development_ready states = %v, want modules_split", developmentReady.Proof.States)
	}

	frontSlot := requireTransition(t, entry, "run_front_module_slot")
	if frontSlot.Kind != "call" {
		t.Fatalf("run_front_module_slot.kind = %q, want call", frontSlot.Kind)
	}
	if frontSlot.PipelineID != "pipeline_front_module_slot" {
		t.Fatalf("run_front_module_slot.pipeline_id = %q, want pipeline_front_module_slot", frontSlot.PipelineID)
	}

	backendModules := requireTransition(t, entry, "run_backend_modules")
	if backendModules.Kind != "call" {
		t.Fatalf("run_backend_modules.kind = %q, want call", backendModules.Kind)
	}
	if backendModules.PipelineID != "pipeline_backend_module" {
		t.Fatalf("run_backend_modules.pipeline_id = %q, want pipeline_backend_module", backendModules.PipelineID)
	}

	globalTestData := requireTransition(t, entry, "write_global_test_data")
	if globalTestData.Kind != "call" {
		t.Fatalf("write_global_test_data.kind = %q, want call", globalTestData.Kind)
	}
	if globalTestData.PipelineID != "pipeline_global_test_data" {
		t.Fatalf("write_global_test_data.pipeline_id = %q, want pipeline_global_test_data", globalTestData.PipelineID)
	}

	modulesDone := requireState(t, entry, "modules_done")
	if modulesDone.Kind != "aggregate" || modulesDone.Proof.Mode != "all" {
		t.Fatalf("modules_done = %+v, want aggregate all", modulesDone)
	}
	if !containsString(modulesDone.Proof.States, "front_module_slot_done") {
		t.Fatalf("modules_done states = %v, want front_module_slot_done", modulesDone.Proof.States)
	}
	if !containsString(modulesDone.Proof.States, "backend_modules_done") {
		t.Fatalf("modules_done states = %v, want backend_modules_done", modulesDone.Proof.States)
	}

	mergeCode := requireTransition(t, entry, "merge_code")
	if mergeCode.FromState != "modules_done" {
		t.Fatalf("merge_code.from_state = %q, want modules_done", mergeCode.FromState)
	}
	if mergeCode.PipelineID != "pipeline_merge_code" {
		t.Fatalf("merge_code.pipeline_id = %q, want pipeline_merge_code", mergeCode.PipelineID)
	}

	globalTestReady := requireState(t, entry, "global_test_ready")
	if globalTestReady.Kind != "aggregate" || globalTestReady.Proof.Mode != "all" {
		t.Fatalf("global_test_ready = %+v, want aggregate all", globalTestReady)
	}
	if !containsString(globalTestReady.Proof.States, "code_merged") {
		t.Fatalf("global_test_ready states = %v, want code_merged", globalTestReady.Proof.States)
	}
	if !containsString(globalTestReady.Proof.States, "global_test_data_ready") {
		t.Fatalf("global_test_ready states = %v, want global_test_data_ready", globalTestReady.Proof.States)
	}
}

func TestBackendModuleDeclaresRecoverableCodeHandler(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	backend := requirePipeline(t, spec, "pipeline_backend_module")

	if len(backend.Signature.Throws) == 0 {
		t.Fatalf("pipeline_backend_module should declare throws for recoverable module bugs")
	}

	foundThrow := false
	for _, thrown := range backend.Signature.Throws {
		if thrown.Result == "kbug" && hasBag(thrown.Bags, "failure_report") {
			foundThrow = true
			break
		}
	}
	if !foundThrow {
		t.Fatalf("throws = %+v, want kbug with failure_report", backend.Signature.Throws)
	}

	if len(backend.Signature.ExportedHandlers) == 0 {
		t.Fatalf("pipeline_backend_module should export debug handler capability")
	}

	foundExport := false
	for _, exported := range backend.Signature.ExportedHandlers {
		if exported.Name == "debug_code" && containsString(exported.Handles, "kbug") {
			foundExport = true
			break
		}
	}
	if !foundExport {
		t.Fatalf("exported handlers = %+v, want debug_code handles kbug", backend.Signature.ExportedHandlers)
	}

	debug := requireTransition(t, backend, "coder_debug_code")
	if debug.Kind != "task" {
		t.Fatalf("coder_debug_code.kind = %q, want task", debug.Kind)
	}
	if debug.Limits.MaxAttempts <= 0 {
		t.Fatalf("coder_debug_code.limits.max_attempts = %d, want > 0", debug.Limits.MaxAttempts)
	}
}

func TestFrontModulePreviewHotUpdateIsProgrammerJudged(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	front := requirePipeline(t, spec, "pipeline_front_module")

	review := requireTransition(t, front, "user_preview_review")
	if review.Kind != "task" {
		t.Fatalf("user_preview_review = %+v, want task", review)
	}
	if review.Agent == nil || review.Agent.Role != core.AgentRoleCEO {
		t.Fatalf("user_preview_review agent = %+v, want ceo/user-facing session role", review.Agent)
	}

	hotUpdate := requireTransition(t, front, "front_hot_update")
	if hotUpdate.Kind != "task" {
		t.Fatalf("front_hot_update = %+v, want task", hotUpdate)
	}
	if hotUpdate.Agent == nil || hotUpdate.Agent.Role != "front" {
		t.Fatalf("front_hot_update agent = %+v, want front programmer", hotUpdate.Agent)
	}
	if hotUpdate.Limits.MaxAttempts <= 0 {
		t.Fatalf("front_hot_update limits = %+v, want max_attempts", hotUpdate.Limits)
	}

	feedbackState := requireState(t, front, "preview_reviewed")
	if feedbackState.Next == nil || feedbackState.Next.Type != "by_result" {
		t.Fatalf("preview_reviewed next = %+v, want by_result", feedbackState.Next)
	}
	if _, ok := feedbackState.Next.Cases[string(core.TaskResultCodeRewrite)]; !ok {
		t.Fatalf("preview_reviewed cases = %+v, want krewrite", feedbackState.Next.Cases)
	}
	if _, ok := feedbackState.Next.Cases[string(core.TaskResultCodeBug)]; !ok {
		t.Fatalf("preview_reviewed cases = %+v, want kbug", feedbackState.Next.Cases)
	}
}

func TestGlobalTestUsesArchitectTriageInsteadOfGlobalDebugCode(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "pipeline_full_delivery.spec.json")
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	global := requirePipeline(t, spec, "pipeline_global_test_code")

	triage := requireTransition(t, global, "architect_triage_global_test_failure")
	if triage.Kind != "task" {
		t.Fatalf("architect_triage_global_test_failure = %+v, want task", triage)
	}
	if triage.Agent == nil || triage.Agent.Role != core.AgentRoleArchitect {
		t.Fatalf("triage agent = %+v, want architect", triage.Agent)
	}
	if triage.Limits.MaxAttempts <= 0 {
		t.Fatalf("triage limits = %+v, want max_attempts", triage.Limits)
	}
	for _, transition := range global.Transitions {
		if transition.ID == "global_debug_code" || transition.ID == "debug_global_code" {
			t.Fatalf("global debug transition should not be used; use architect_triage_global_test_failure")
		}
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
