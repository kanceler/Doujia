package pipeline

import (
	"strings"
	"testing"
)

func TestValidateRegistrySpecRejectsExportedHandlerReferenceToMissingHandler(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [{
    "schema_version": "devflow.pipeline/v0.4",
    "pipeline_id": "entry",
    "kind": "entry",
    "namespace": {
      "agents": [{"name": "coder", "role": "coder", "default_agent_id": "coder01"}],
      "bags": [{"name": "code_bag"}]
    },
    "signature": {
      "exported_handlers": [{
        "name": "debug_code",
        "handler": "missing_debug",
        "handles": ["kbug"],
        "replaces": [{"name": "code_bag"}]
      }]
    },
    "start_state": "start",
    "delivery_state": "done",
    "states": [
      {"id": "start", "kind": "start", "proof": {"type": "external"}},
      {"id": "done", "kind": "delivery", "proof": {"type": "external"}}
    ],
    "transitions": []
  }]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want missing exported handler reference")
	}
	if !strings.Contains(err.Error(), "signature.exported_handlers") || !strings.Contains(err.Error(), "missing_debug") {
		t.Fatalf("ParseRegistrySpec() error = %v, want exported handler reference error", err)
	}
}

func TestValidateRegistrySpecRejectsCallOutputHandlerNotExportedByChild(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "parent",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "child",
      "kind": "subpipeline",
      "namespace": {
        "agents": [{"name": "coder", "role": "coder", "default_agent_id": "coder01"}],
        "bags": [{"name": "code_bag"}]
      },
      "signature": {
        "output_bags": [{"name": "code_bag"}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "external"}, "exposes": {"bags": [{"name": "code_bag"}]}}
      ],
      "transitions": []
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "parent",
      "kind": "entry",
      "namespace": {
        "bags": [{"name": "code_bag"}]
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
        "output_bags": [{"name": "code_bag"}],
        "output_handlers": [{"name": "debug_code", "from_handler": "debug_code"}]
      }]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want unexported output handler rejected")
	}
	if !strings.Contains(err.Error(), "output_handlers") || !strings.Contains(err.Error(), "debug_code") {
		t.Fatalf("ParseRegistrySpec() error = %v, want output handler export error", err)
	}
}

func TestValidateRegistrySpecRejectsUnhandledChildThrows(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "parent",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "child",
      "kind": "subpipeline",
      "namespace": {
        "bags": [{"name": "code_bag"}, {"name": "failure_report", "indexed_by": ["module_key"]}]
      },
      "signature": {
        "output_bags": [{"name": "code_bag"}],
        "throws": [{
          "result": "kbug",
          "bags": [{"name": "failure_report", "required": true, "indexed_by": ["module_key"]}]
        }]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "external"}, "exposes": {"bags": [{"name": "code_bag"}]}}
      ],
      "transitions": []
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "parent",
      "kind": "entry",
      "namespace": {"bags": [{"name": "code_bag"}]},
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
        "output_bags": [{"name": "code_bag"}]
      }]
    }
  ]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want unhandled child throws rejected")
	}
	if !strings.Contains(err.Error(), "throws") || !strings.Contains(err.Error(), "kbug") {
		t.Fatalf("ParseRegistrySpec() error = %v, want throws handling error", err)
	}
}

func TestValidateRegistrySpecAllowsParentRethrowOfChildThrows(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "parent",
  "pipeline_defs": [
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "child",
      "kind": "subpipeline",
      "namespace": {
        "bags": [{"name": "code_bag"}, {"name": "failure_report", "indexed_by": ["module_key"]}]
      },
      "signature": {
        "output_bags": [{"name": "code_bag"}],
        "throws": [{"result": "kbug", "bags": [{"name": "failure_report", "indexed_by": ["module_key"]}]}]
      },
      "start_state": "start",
      "delivery_state": "done",
      "states": [
        {"id": "start", "kind": "start", "proof": {"type": "external"}},
        {"id": "done", "kind": "delivery", "proof": {"type": "external"}, "exposes": {"bags": [{"name": "code_bag"}]}}
      ],
      "transitions": []
    },
    {
      "schema_version": "devflow.pipeline/v0.4",
      "pipeline_id": "parent",
      "kind": "entry",
      "namespace": {"bags": [{"name": "code_bag"}, {"name": "failure_report", "indexed_by": ["module_key"]}]},
      "signature": {
        "throws": [{"result": "kbug", "bags": [{"name": "failure_report", "indexed_by": ["module_key"]}]}]
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
        "output_bags": [{"name": "code_bag"}]
      }]
    }
  ]
}`)
	if _, err := ParseRegistrySpec(raw); err != nil {
		t.Fatalf("ParseRegistrySpec() error = %v, want parent rethrow accepted", err)
	}
}

func TestValidateRegistrySpecRejectsHandlerTransitionWithoutMaxAttempts(t *testing.T) {
	raw := []byte(`{
  "schema_version": "devflow.pipeline.registry/v0.4",
  "registry_id": "test_registry",
  "entry_pipeline_id": "entry",
  "pipeline_defs": [{
    "schema_version": "devflow.pipeline/v0.4",
    "pipeline_id": "entry",
    "kind": "entry",
    "namespace": {
      "agents": [{"name": "coder", "role": "coder", "default_agent_id": "coder01"}],
      "bags": [{"name": "failure_report"}, {"name": "code_bag"}]
    },
    "handlers": [{
      "name": "debug_code",
      "handles": ["kbug"],
      "transition": "debug_code",
      "input_bags": [{"name": "failure_report", "source": "exception"}],
      "replace_bags": [{"name": "code_bag", "from_transition": "debug_code"}],
      "resume": {"type": "retry_failed_transition"}
    }],
    "start_state": "start",
    "delivery_state": "done",
    "states": [
      {"id": "start", "kind": "start", "proof": {"type": "external"}, "next": {"type": "all", "transitions": ["debug_code"]}},
      {"id": "done", "kind": "delivery", "proof": {"type": "transition_result", "transition": "debug_code"}}
    ],
    "transitions": [{
      "id": "debug_code",
      "kind": "task",
      "from_state": "start",
      "to_state": "done",
      "agent": {"role": "coder", "alias": "coder"},
      "op": "write_code",
      "output_bags": [{"name": "code_bag"}]
    }]
  }]
}`)
	_, err := ParseRegistrySpec(raw)
	if err == nil {
		t.Fatalf("ParseRegistrySpec() error = nil, want handler max_attempts rejection")
	}
	if !strings.Contains(err.Error(), "limits.max_attempts") || !strings.Contains(err.Error(), "debug_code") {
		t.Fatalf("ParseRegistrySpec() error = %v, want handler max_attempts error", err)
	}
}
