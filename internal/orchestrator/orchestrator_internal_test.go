package orchestrator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"devflow/internal/core"
	"devflow/internal/pipeline"
	"devflow/internal/state/repo"
)

func TestForwardedInputBagsUseNames(t *testing.T) {
	got := bagIDsForSpecs([]pipeline.BagSpec{
		{Name: "architecture"},
	}, []string{"bag_architecture"})

	if got["architecture"] != "bag_architecture" {
		t.Fatalf("bagIDsForSpecs() = %#v, want architecture role forwarded", got)
	}
}

func TestShouldForwardInputBagsForNoOutputReview(t *testing.T) {
	task := core.Task{InputBagIDs: []string{"bag_architecture"}}
	feedback := core.TaskMetaData{Result: core.TaskResultCodeOK}

	if !shouldForwardInputBags(task, feedback, core.TaskStatusDone) {
		t.Fatal("shouldForwardInputBags() = false, want true for successful no-output review")
	}
}

func TestCommitOutputBagIDsByNameUsesProducedBags(t *testing.T) {
	got := commitOutputBagIDsByName(&core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "module_input", ArtifactVersionIDs: []string{"version:module01"}},
		},
	}, []string{"bag_module01"})

	if got["module_input"] != "bag_module01" {
		t.Fatalf("commitOutputBagIDsByName() = %#v, want module_input mapped", got)
	}
}

func TestCommitOutputBagIDsByNameIndexesCollectionBags(t *testing.T) {
	got := commitOutputBagIDsByName(&core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "module_input", Indexes: map[string]string{"module_key": "module01"}, ArtifactVersionIDs: []string{"version:module01"}},
			{Name: "module_input", Indexes: map[string]string{"module_key": "module02"}, ArtifactVersionIDs: []string{"version:module02"}},
		},
	}, []string{"bag_module01", "bag_module02"})

	if got["module_input[module_key=module01]"] != "bag_module01" {
		t.Fatalf("commitOutputBagIDsByName() = %#v, want indexed module01 key mapped", got)
	}
	if got["module_input[module_key=module02]"] != "bag_module02" {
		t.Fatalf("commitOutputBagIDsByName() = %#v, want indexed module02 key mapped", got)
	}
}

func TestOutputBagIDsFromCommitKeepsIndexedCollectionKeys(t *testing.T) {
	got, err := outputBagIDsFromCommit(pipeline.TransitionSpec{
		ID: "write_code",
		OutputBags: []pipeline.BagSpec{{
			Name:      "code_bag",
			IndexedBy: []string{"module_key"},
		}},
	}, core.TaskResultCodeOK, &core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "code_bag", Indexes: map[string]string{"module_key": "module01"}, ArtifactVersionIDs: []string{"version:module01"}},
		},
	}, []string{"bag_code_module01"})
	if err != nil {
		t.Fatalf("outputBagIDsFromCommit() error = %v", err)
	}
	if got["code_bag[module_key=module01]"] != "bag_code_module01" {
		t.Fatalf("outputBagIDsFromCommit() = %#v, want indexed code_bag key", got)
	}
	if got["code_bag"] != "bag_code_module01" {
		t.Fatalf("outputBagIDsFromCommit() = %#v, want canonical code_bag fallback key", got)
	}
}

func TestOutputBagIDListsFromCommitPreservesCollectionMembers(t *testing.T) {
	got, err := outputBagIDListsFromCommit(pipeline.TransitionSpec{
		ID: "test_all_modules",
		OutputBags: []pipeline.BagSpec{{
			Name:       "tested_module",
			Collection: true,
			IndexedBy:  []string{"module_key"},
		}},
	}, core.TaskResultCodeOK, &core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "tested_module", Indexes: map[string]string{"module_key": "module01"}, ArtifactVersionIDs: []string{"version:module01"}},
			{Name: "tested_module", Indexes: map[string]string{"module_key": "module02"}, ArtifactVersionIDs: []string{"version:module02"}},
		},
	}, []string{"bag_tested_module01", "bag_tested_module02"}, core.PipelineInstance{})
	if err != nil {
		t.Fatalf("outputBagIDListsFromCommit() error = %v", err)
	}
	if !reflect.DeepEqual(got["tested_module"], []string{"bag_tested_module01", "bag_tested_module02"}) {
		t.Fatalf("output bag lists = %#v, want both tested_module bags", got)
	}
	if !reflect.DeepEqual(got["tested_module[module_key=module01]"], []string{"bag_tested_module01"}) {
		t.Fatalf("output bag lists = %#v, want indexed module01 bag", got)
	}
	if !reflect.DeepEqual(got["tested_module[module_key=module02]"], []string{"bag_tested_module02"}) {
		t.Fatalf("output bag lists = %#v, want indexed module02 bag", got)
	}
}

func TestOutputBagIDListsFromCommitRejectsDuplicateNonCollectionReturn(t *testing.T) {
	_, err := outputBagIDListsFromCommit(pipeline.TransitionSpec{
		ID: "write_plan",
		OutputBags: []pipeline.BagSpec{{
			Name: "product_plan",
		}},
	}, core.TaskResultCodeOK, &core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "product_plan", ArtifactVersionIDs: []string{"version:plan1"}},
			{Name: "product_plan", ArtifactVersionIDs: []string{"version:plan2"}},
		},
	}, []string{"bag_plan1", "bag_plan2"}, core.PipelineInstance{})
	if err == nil {
		t.Fatal("outputBagIDListsFromCommit() error = nil, want duplicate non-collection rejection")
	}
}

func TestBindControlInputBagsFromProducedBagsUsesModuleKeyIndex(t *testing.T) {
	control := core.Control{
		Params:    map[string]string{"module_key": "module01"},
		InputBags: map[string]string{"module_input": "module_input"},
	}
	got := bindControlInputBagsFromProducedBags(control, &core.CommitReceipt{
		ProducedBags: []core.CommittedBagDef{
			{Name: "module_input", Indexes: map[string]string{"module_key": "module01"}, ArtifactVersionIDs: []string{"version:module01"}},
			{Name: "module_input", Indexes: map[string]string{"module_key": "module02"}, ArtifactVersionIDs: []string{"version:module02"}},
		},
	}, []string{"bag_module01", "bag_module02"})

	if got.InputBags["module_input"] != "bag_module01" {
		t.Fatalf("bound control input bags = %#v, want module01 bag selected by module_key", got.InputBags)
	}
}

func TestBuildTaskFromPipelineTransitionSetsNamedInputBagBindings(t *testing.T) {
	task, err := buildTaskFromPipelineTransition("run_named", core.PipelineInstance{
		ID: "instance_module01",
		AgentBindings: map[string]core.AgentID{
			"coder": "coder01",
		},
		InputBagIDs: map[string]string{
			"module_input": "bag_module01",
		},
		InputBagIDLists: map[string][]string{
			"module_input": {"bag_module01"},
		},
	}, pipeline.TransitionSpec{
		ID: "write_code",
		Agent: &pipeline.AgentSpec{
			Role:  core.AgentRoleCoder,
			Alias: "coder",
		},
		Op:        core.TaskOpWriteCode,
		InputBags: []pipeline.BagSpec{{Name: "module_input", IndexedBy: []string{"module_key"}}},
	})
	if err != nil {
		t.Fatalf("buildTaskFromPipelineTransition() error = %v", err)
	}
	if got, want := len(task.InputBags), 1; got != want {
		t.Fatalf("task input bags = %#v, want %d named binding", task.InputBags, want)
	}
	if got := task.InputBags[0].Name; got != "module_input" {
		t.Fatalf("task input bag name = %q, want module_input", got)
	}
	if got := task.InputBags[0].BagID; got != "bag_module01" {
		t.Fatalf("task input bag id = %q, want bag_module01", got)
	}
}

func TestEnterStartStateAppliesIndexedExposesBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_start_exposes"
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "entry"}),
		repo.NewMemoryRunRepository(),
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	service.SetPipelineInstanceRepository(instanceRepo)

	run := core.PipelineRun{ID: runID, PipelineID: "entry"}
	instance := core.PipelineInstance{
		ID:         "root_test_all_modules_module01",
		RunID:      runID,
		PipelineID: "pipeline_write_code",
		Status:     core.PipelineInstanceStatusRunning,
		Params:     map[string]string{"module_key": "module01"},
		AgentBindings: map[string]core.AgentID{
			"coder": "coder01",
		},
		InputBagIDs: map[string]string{
			"module_input": "bag_module01",
		},
		InputBagIDLists: map[string][]string{
			"module_input": {"bag_module01"},
		},
	}
	if err := instanceRepo.Create(ctx, instance); err != nil {
		t.Fatalf("Create(instance) error = %v", err)
	}

	def := pipeline.PipelineDefSpec{
		PipelineID: "pipeline_write_code",
		StartState: "module_input_ready",
		States: []pipeline.StateSpec{{
			ID:   "module_input_ready",
			Kind: "start",
			Proof: pipeline.ProofSpec{
				Type: "external",
			},
			Exposes: pipeline.ExposesSpec{Bags: []pipeline.BagSpec{{
				Name:      "module_input",
				IndexedBy: []string{"module_key"},
			}}},
			Next: &pipeline.NextSpec{
				Type:        "all",
				Transitions: []string{"write_code"},
			},
		}},
		Transitions: []pipeline.TransitionSpec{{
			ID:        "write_code",
			Kind:      "task",
			FromState: "module_input_ready",
			ToState:   "code_written",
			Agent:     &pipeline.AgentSpec{Role: core.AgentRoleCoder, Alias: "coder"},
			Op:        core.TaskOpWriteCode,
			InputBags: []pipeline.BagSpec{{Name: "module_input"}},
		}},
	}

	if err := service.enterPipelineState(ctx, run, instance, def, "module_input_ready"); err != nil {
		t.Fatalf("enterPipelineState() error = %v", err)
	}
	task, err := taskRepo.Get(ctx, runID, "root_test_all_modules_module01_write_code")
	if err != nil {
		t.Fatalf("Get(write_code task) error = %v", err)
	}
	if got := task.InputBags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("dispatched input bags = %#v, want module_key module01", task.InputBags)
	}
	if len(dispatcher.dispatched) != 1 || dispatcher.dispatched[0].InputBags[0].Indexes["module_key"] != "module01" {
		t.Fatalf("dispatch payloads = %#v, want indexed module_input", dispatcher.dispatched)
	}
}

func TestBuildPipelineInstanceFromSingleCallPreservesIndexedCollectionBindings(t *testing.T) {
	parent := core.PipelineInstance{
		ID: "root",
		OutputBagIDs: map[string]string{
			"tested_module":                      "bag_tested_module02",
			"tested_module[module_key=module01]": "bag_tested_module01",
			"tested_module[module_key=module02]": "bag_tested_module02",
		},
		OutputBagIDLists: map[string][]string{
			"tested_module":                      {"bag_tested_module01", "bag_tested_module02"},
			"tested_module[module_key=module01]": {"bag_tested_module01"},
			"tested_module[module_key=module02]": {"bag_tested_module02"},
		},
	}

	child, err := buildPipelineInstanceFromSingleCall("run_single_call", parent, pipeline.TransitionSpec{
		ID:         "merge_code",
		Kind:       "call",
		PipelineID: "pipeline_merge_code",
		Mode:       "single",
		Bindings: &pipeline.BindingsSpec{InputBags: map[string]any{
			"tested_module": map[string]any{
				"name":       "tested_module",
				"collection": true,
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildPipelineInstanceFromSingleCall() error = %v", err)
	}
	if got := child.InputBagIDLists["tested_module[module_key=module01]"]; !reflect.DeepEqual(got, []string{"bag_tested_module01"}) {
		t.Fatalf("child input bag lists = %#v, want module01 indexed binding", child.InputBagIDLists)
	}
	if got := child.InputBagIDLists["tested_module[module_key=module02]"]; !reflect.DeepEqual(got, []string{"bag_tested_module02"}) {
		t.Fatalf("child input bag lists = %#v, want module02 indexed binding", child.InputBagIDLists)
	}
}

type internalRecordingDispatcher struct {
	dispatched []core.TaskMetaData
}

func (d *internalRecordingDispatcher) Dispatch(_ context.Context, task core.TaskMetaData) error {
	d.dispatched = append(d.dispatched, task)
	return nil
}

func TestResolveInputBagBindingObjectAcceptsCanonicalName(t *testing.T) {
	got, err := resolveInputBagBinding(core.PipelineInstance{
		ID: "parent",
		OutputBagIDs: map[string]string{
			"module_input": "bag_module01",
		},
	}, map[string]any{"name": "module_input"})
	if err != nil {
		t.Fatalf("resolveInputBagBinding() error = %v", err)
	}
	if got != "bag_module01" {
		t.Fatalf("resolveInputBagBinding() = %q, want bag_module01", got)
	}
}

func TestResolveInputBagBindingObjectPrefersModuleKeyIndexedBag(t *testing.T) {
	got, err := resolveInputBagBinding(core.PipelineInstance{
		ID:     "module01",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"code_bag":                      "bag_code_module02",
			"code_bag[module_key=module01]": "bag_code_module01",
			"code_bag[module_key=module02]": "bag_code_module02",
		},
		OutputBagIDLists: map[string][]string{
			"code_bag":                      {"bag_code_module01", "bag_code_module02"},
			"code_bag[module_key=module01]": {"bag_code_module01"},
			"code_bag[module_key=module02]": {"bag_code_module02"},
		},
	}, map[string]any{"name": "code_bag"})
	if err != nil {
		t.Fatalf("resolveInputBagBinding() error = %v", err)
	}
	if got != "bag_code_module01" {
		t.Fatalf("resolveInputBagBinding() = %q, want module01 indexed bag", got)
	}
}

func TestMergeChildInstanceOutputsPreservesIndexedOutputKeysForTaskBindings(t *testing.T) {
	parent := core.PipelineInstance{
		ID: "root",
	}
	child := core.PipelineInstance{
		ID:     "root_test_all_modules_module01",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"tested_module":                      "bag_tested_module01",
			"tested_module[module_key=module01]": "bag_tested_module01",
		},
		OutputBagIDLists: map[string][]string{
			"tested_module":                      {"bag_tested_module01"},
			"tested_module[module_key=module01]": {"bag_tested_module01"},
		},
	}
	parent = mergeChildInstanceOutputs(parent, pipeline.TransitionSpec{
		OutputBags: []pipeline.BagSpec{{
			Name:      "tested_module",
			IndexedBy: []string{"module_key"},
		}},
	}, child)

	task, err := buildTaskFromPipelineTransition("run_indexed_child_output", core.PipelineInstance{
		ID: "root",
		AgentBindings: map[string]core.AgentID{
			"architect": "architect01",
		},
		OutputBagIDs:     parent.OutputBagIDs,
		OutputBagIDLists: parent.OutputBagIDLists,
	}, pipeline.TransitionSpec{
		ID: "merge_code",
		Agent: &pipeline.AgentSpec{
			Role:  core.AgentRoleArchitect,
			Alias: "architect",
		},
		Op: core.TaskOpMergeCode,
		InputBags: []pipeline.BagSpec{{
			Name:       "tested_module",
			Collection: true,
		}},
	})
	if err != nil {
		t.Fatalf("buildTaskFromPipelineTransition() error = %v", err)
	}
	if got, want := len(task.InputBags), 1; got != want {
		t.Fatalf("task input bags = %#v, want %d item", task.InputBags, want)
	}
	if got := task.InputBags[0].BagID; got != "bag_tested_module01" {
		t.Fatalf("task input bag id = %q, want bag_tested_module01", got)
	}
	if got := task.InputBags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("task input bag indexes = %#v, want module_key module01", task.InputBags[0].Indexes)
	}
}

func TestMergeChildInstanceOutputsIndexesUnindexedChildReturnFromParams(t *testing.T) {
	parent := core.PipelineInstance{ID: "root"}
	child := core.PipelineInstance{
		ID:     "root_test_all_modules_module01",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"tested_module": "bag_tested_module01",
		},
		OutputBagIDLists: map[string][]string{
			"tested_module": {"bag_tested_module01"},
		},
	}

	parent = mergeChildInstanceOutputs(parent, pipeline.TransitionSpec{
		OutputBags: []pipeline.BagSpec{{
			Name:      "tested_module",
			IndexedBy: []string{"module_key"},
		}},
	}, child)

	if got := parent.OutputBagIDs["tested_module[module_key=module01]"]; got != "bag_tested_module01" {
		t.Fatalf("parent output bags = %#v, want indexed tested_module from child params", parent.OutputBagIDs)
	}

	task, err := buildTaskFromPipelineTransition("run_unindexed_child_output", core.PipelineInstance{
		ID: "root",
		AgentBindings: map[string]core.AgentID{
			"architect": "architect01",
		},
		OutputBagIDs:     parent.OutputBagIDs,
		OutputBagIDLists: parent.OutputBagIDLists,
	}, pipeline.TransitionSpec{
		ID:    "merge_code",
		Agent: &pipeline.AgentSpec{Role: core.AgentRoleArchitect, Alias: "architect"},
		Op:    core.TaskOpMergeCode,
		InputBags: []pipeline.BagSpec{{
			Name:       "tested_module",
			Collection: true,
		}},
	})
	if err != nil {
		t.Fatalf("buildTaskFromPipelineTransition() error = %v", err)
	}
	if got, want := len(task.InputBags), 1; got != want {
		t.Fatalf("task input bags = %#v, want %d item", task.InputBags, want)
	}
	if got := task.InputBags[0].Indexes["module_key"]; got != "module01" {
		t.Fatalf("task input bag indexes = %#v, want module_key module01", task.InputBags)
	}
}

func TestMergeChildInstanceOutputsPreservesTwoIndexedChildReturns(t *testing.T) {
	parent := core.PipelineInstance{ID: "root"}
	transition := pipeline.TransitionSpec{
		OutputBags: []pipeline.BagSpec{{
			Name:       "tested_module",
			Collection: true,
			IndexedBy:  []string{"module_key"},
		}},
	}

	for _, child := range []core.PipelineInstance{
		{
			ID:     "root_test_all_modules_module01",
			Params: map[string]string{"module_key": "module01"},
			OutputBagIDs: map[string]string{
				"tested_module":                      "bag_tested_module01",
				"tested_module[module_key=module01]": "bag_tested_module01",
			},
			OutputBagIDLists: map[string][]string{
				"tested_module":                      {"bag_tested_module01"},
				"tested_module[module_key=module01]": {"bag_tested_module01"},
			},
		},
		{
			ID:     "root_test_all_modules_module02",
			Params: map[string]string{"module_key": "module02"},
			OutputBagIDs: map[string]string{
				"tested_module":                      "bag_tested_module02",
				"tested_module[module_key=module02]": "bag_tested_module02",
			},
			OutputBagIDLists: map[string][]string{
				"tested_module":                      {"bag_tested_module02"},
				"tested_module[module_key=module02]": {"bag_tested_module02"},
			},
		},
	} {
		parent = mergeChildInstanceOutputs(parent, transition, child)
	}

	task, err := buildTaskFromPipelineTransition("run_indexed_child_outputs", core.PipelineInstance{
		ID: "root",
		AgentBindings: map[string]core.AgentID{
			"architect": "architect01",
		},
		OutputBagIDs:     parent.OutputBagIDs,
		OutputBagIDLists: parent.OutputBagIDLists,
	}, pipeline.TransitionSpec{
		ID:    "merge_code",
		Agent: &pipeline.AgentSpec{Role: core.AgentRoleArchitect, Alias: "architect"},
		Op:    core.TaskOpMergeCode,
		InputBags: []pipeline.BagSpec{{
			Name:       "tested_module",
			Collection: true,
		}},
	})
	if err != nil {
		t.Fatalf("buildTaskFromPipelineTransition() error = %v", err)
	}
	if got, want := len(task.InputBags), 2; got != want {
		t.Fatalf("task input bags = %#v, want %d indexed items", task.InputBags, want)
	}
	gotIndexes := map[string]string{}
	for _, binding := range task.InputBags {
		gotIndexes[binding.BagID] = binding.Indexes["module_key"]
	}
	if gotIndexes["bag_tested_module01"] != "module01" || gotIndexes["bag_tested_module02"] != "module02" {
		t.Fatalf("task input bag indexes = %#v, want module01/module02", task.InputBags)
	}
}

func TestMergeChildInstanceOutputsCanRenameReturnedBag(t *testing.T) {
	parent := mergeChildInstanceOutputs(core.PipelineInstance{ID: "root"}, pipeline.TransitionSpec{
		OutputBags: []pipeline.BagSpec{{
			Name:       "parent_out",
			FromReturn: "child_out",
			IndexedBy:  []string{"module_key"},
		}},
	}, core.PipelineInstance{
		ID:     "child",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"child_out":                      "bag_child_out",
			"child_out[module_key=module01]": "bag_child_out",
		},
		OutputBagIDLists: map[string][]string{
			"child_out":                      {"bag_child_out"},
			"child_out[module_key=module01]": {"bag_child_out"},
		},
	})

	if got := parent.OutputBagIDs["parent_out[module_key=module01]"]; got != "bag_child_out" {
		t.Fatalf("parent output bags = %#v, want renamed indexed return", parent.OutputBagIDs)
	}
	if got := parent.OutputBagIDs["child_out"]; got != "" {
		t.Fatalf("parent output bags = %#v, child return name should not leak into parent", parent.OutputBagIDs)
	}
}

func TestApplyStateExposesAddsIndexedTransitionOutputKeys(t *testing.T) {
	instance := core.PipelineInstance{
		ID:     "root_test_all_modules_module01",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"tested_module": "bag_tested_module01",
		},
		OutputBagIDLists: map[string][]string{
			"tested_module": {"bag_tested_module01"},
		},
	}

	got := applyStateExposes(instance, pipeline.StateSpec{
		ID:   "module_tested",
		Kind: "delivery",
		Exposes: pipeline.ExposesSpec{Bags: []pipeline.BagSpec{{
			Name:           "tested_module",
			FromTransition: "test_code",
			IndexedBy:      []string{"module_key"},
		}}},
	})

	if got.OutputBagIDs["tested_module[module_key=module01]"] != "bag_tested_module01" {
		t.Fatalf("output bags = %#v, want indexed tested_module key", got.OutputBagIDs)
	}
	if values := got.OutputBagIDLists["tested_module[module_key=module01]"]; !reflect.DeepEqual(values, []string{"bag_tested_module01"}) {
		t.Fatalf("output bag lists = %#v, want indexed tested_module list", got.OutputBagIDLists)
	}
}

func TestAdvanceReadyTasksDispatchesOneAtATimeWhenParallelWorkDisabled(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_low_concurrency"
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &lowConcurrencyRecordingDispatcher{}
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "pipeline_low_concurrency"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_low_concurrency",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config: core.RunConfig{Delivery: core.DeliveryConfig{
			MaxCoderAgents:    1,
			MaxTesterAgents:   1,
			AllowParallelWork: false,
			Git:               core.GitRunConfig{MainBranch: "main"},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	parentID := core.TaskID("parent")
	for _, task := range []core.Task{
		{ID: parentID, RunID: runID, Status: core.TaskStatusDone, OutputArtifactRefs: []core.ArtifactRef{"projects/run/parent.md"}, CreatedAt: now, UpdatedAt: now},
		{ID: "child_a", RunID: runID, StageID: "write_code", AgentRole: core.AgentRoleCoder, AgentID: "coder01", Op: core.TaskOpWriteCode, ParentID: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusPending, CreatedAt: now, UpdatedAt: now},
		{ID: "child_b", RunID: runID, StageID: "test_data", AgentRole: core.AgentRoleTester, AgentID: "tester01", Op: core.TaskOpTestData, ParentID: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusPending, CreatedAt: now, UpdatedAt: now},
	} {
		if err := taskRepo.Create(ctx, task); err != nil {
			t.Fatalf("Create(task %s) error = %v", task.ID, err)
		}
	}

	if err := service.advanceReadyTasks(ctx, run); err != nil {
		t.Fatalf("advanceReadyTasks() error = %v", err)
	}
	if got := len(dispatcher.dispatched); got != 1 {
		t.Fatalf("dispatched tasks = %d, want 1 in low concurrency mode", got)
	}
	childA, err := taskRepo.Get(ctx, runID, "child_a")
	if err != nil {
		t.Fatalf("Get(child_a) error = %v", err)
	}
	childB, err := taskRepo.Get(ctx, runID, "child_b")
	if err != nil {
		t.Fatalf("Get(child_b) error = %v", err)
	}
	statuses := map[core.TaskStatus]int{childA.Status: 1, childB.Status: 1}
	if statuses[core.TaskStatusDispatched] != 1 || statuses[core.TaskStatusPending] != 1 {
		t.Fatalf("child statuses = %s/%s, want one dispatched and one pending", childA.Status, childB.Status)
	}
}

type lowConcurrencyRecordingDispatcher struct {
	dispatched []core.TaskMetaData
}

func (d *lowConcurrencyRecordingDispatcher) Dispatch(_ context.Context, task core.TaskMetaData) error {
	d.dispatched = append(d.dispatched, task)
	return nil
}
