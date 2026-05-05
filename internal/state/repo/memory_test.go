package repo

import (
	"context"
	"testing"
	"time"

	"devflow/internal/core"
)

func TestMemoryPipelineInstanceRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryPipelineInstanceRepository()
	now := time.Now().UTC()
	parentID := core.PipelineInstanceID("root")
	instance := PipelineInstanceRecord{
		ID:                 "module01",
		RunID:              "run_memory",
		PipelineID:         "pipeline_module",
		ParentID:           &parentID,
		ParentTransitionID: "test_all_modules",
		InstanceKey:        "module01",
		Status:             core.PipelineInstanceStatusCreated,
		Params:             map[string]string{"module_key": "module01"},
		AgentBindings: map[string]core.AgentID{
			"coder":  "coder01",
			"tester": "tester01",
		},
		InputBagIDs: map[string]string{"module_input": "bag_module01"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := repository.Create(ctx, instance); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	instance.Status = core.PipelineInstanceStatusRunning
	instance.OutputBagIDs = map[string]string{"tested_module": "bag_tested_module01"}
	if err := repository.Update(ctx, instance); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repository.Get(ctx, instance.RunID, instance.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != core.PipelineInstanceStatusRunning || got.Params["module_key"] != "module01" {
		t.Fatalf("instance = %#v, want running module01", got)
	}
	if got.AgentBindings["tester"] != "tester01" || got.InputBagIDs["module_input"] != "bag_module01" {
		t.Fatalf("instance bindings = %#v bags=%#v", got.AgentBindings, got.InputBagIDs)
	}
	if got.OutputBagIDs["tested_module"] != "bag_tested_module01" {
		t.Fatalf("output bags = %#v, want tested_module", got.OutputBagIDs)
	}

	got.Params["module_key"] = "mutated"
	again, err := repository.Get(ctx, instance.RunID, instance.ID)
	if err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	if again.Params["module_key"] != "module01" {
		t.Fatalf("repository returned mutable params map")
	}

	children, err := repository.ListChildren(ctx, instance.RunID, parentID)
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(children) != 1 || children[0].ID != instance.ID {
		t.Fatalf("children = %#v, want module01", children)
	}
}
