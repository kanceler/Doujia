package orchestrator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

func TestMergeChildInstanceHandlersRequiresExplicitOutputHandlers(t *testing.T) {
	childDef := pipeline.PipelineDefSpec{
		Signature: pipeline.SignatureSpec{
			ExportedHandlers: []pipeline.HandlerExportSpec{{
				Name:     "debug_code",
				Handler:  "debug_code",
				Handles:  []string{"kbug"},
				Replaces: []pipeline.BagSpec{{Name: "code_bag", IndexedBy: []string{"module_key"}}},
			}},
		},
	}
	child := core.PipelineInstance{
		ID:     "child01",
		Params: map[string]string{"module_key": "module01"},
		OutputBagIDs: map[string]string{
			"code_bag[module_key=module01]": "bag_code_v1",
		},
		OutputBagIDLists: map[string][]string{
			"code_bag[module_key=module01]": {"bag_code_v1"},
		},
	}

	parent := mergeChildInstanceHandlers(core.PipelineInstance{}, pipeline.TransitionSpec{}, child, childDef)
	if len(parent.HandlerBindings) != 0 {
		t.Fatalf("handler bindings = %#v, want none without explicit output_handlers", parent.HandlerBindings)
	}

	parent = mergeChildInstanceHandlers(core.PipelineInstance{}, pipeline.TransitionSpec{
		OutputHandlers: []pipeline.HandlerBindingSpec{{Name: "debug_code", FromHandler: "debug_code", IndexedBy: []string{"module_key"}}},
	}, child, childDef)
	if got := len(parent.HandlerBindings); got != 1 {
		t.Fatalf("handler bindings = %d, want 1", got)
	}
	binding := parent.HandlerBindings[0]
	if binding.Name != "debug_code" || binding.OwnerInstanceID != "child01" || binding.Indexes["module_key"] != "module01" {
		t.Fatalf("binding = %#v, want debug_code owned by child01/module01", binding)
	}
	if got := binding.Replaces[0].BagID; got != "bag_code_v1" {
		t.Fatalf("binding replaces bag = %q, want bag_code_v1", got)
	}
}

func TestSelectHandlerBindingsForExceptionChoosesMatchingFailedInputBags(t *testing.T) {
	parent := core.PipelineInstance{
		HandlerBindings: []core.HandlerBindingRef{
			{
				Name:            "debug_code",
				OwnerInstanceID: "child1",
				Handles:         []string{"kbug"},
				Replaces:        []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_child1", Indexes: map[string]string{"module_key": "module01"}}},
				Indexes:         map[string]string{"module_key": "module01"},
			},
			{
				Name:            "debug_code",
				OwnerInstanceID: "child2",
				Handles:         []string{"kbug"},
				Replaces:        []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_child2", Indexes: map[string]string{"module_key": "module02"}}},
				Indexes:         map[string]string{"module_key": "module02"},
			},
		},
	}
	frame := core.ExceptionFrame{
		Result:          core.TaskResultCodeBug,
		FailedInputBags: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_child2", Indexes: map[string]string{"module_key": "module02"}}},
	}

	selected := selectHandlerBindingsForException(parent, frame)
	if got := len(selected); got != 1 {
		t.Fatalf("selected = %#v, want exactly child2", selected)
	}
	if selected[0].OwnerInstanceID != "child2" {
		t.Fatalf("selected owner = %s, want child2", selected[0].OwnerInstanceID)
	}
}

func TestSelectHandlerBindingsForExceptionKeepsParallelMatchesTogether(t *testing.T) {
	parent := core.PipelineInstance{
		HandlerBindings: []core.HandlerBindingRef{
			{
				Name:            "debug_code",
				OwnerInstanceID: "child1",
				Handles:         []string{"kbug"},
				Replaces:        []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_child1"}},
			},
			{
				Name:            "debug_code",
				OwnerInstanceID: "child2",
				Handles:         []string{"kbug"},
				Replaces:        []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_child2"}},
			},
		},
	}
	frame := core.ExceptionFrame{
		Result: core.TaskResultCodeBug,
		FailedInputBags: []core.BagBindingRef{
			{Name: "code_bag", BagID: "bag_code_child1"},
			{Name: "code_bag", BagID: "bag_code_child2"},
		},
	}

	selected := selectHandlerBindingsForException(parent, frame)
	if got := len(selected); got != 2 {
		t.Fatalf("selected = %#v, want both parallel matching handlers", selected)
	}
}

func TestPipelineBugBubblesToParentReceivedHandler(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	definitions, err := pipeline.NewJSONRegistry(pipeline.RegistrySpec{
		SchemaVersion:   pipeline.RegistrySchemaVersionV04,
		RegistryID:      "test_registry",
		EntryPipelineID: "parent",
		PipelineDefs: []pipeline.PipelineDefSpec{
			{
				SchemaVersion: pipeline.PipelineSchemaVersionV04,
				PipelineID:    "owner",
				Kind:          "subpipeline",
				Namespace: pipeline.NamespaceSpec{
					Agents: []pipeline.SignatureAgentSpec{{Name: "coder", Role: core.AgentRoleCoder, DefaultAgentID: "coder01"}},
					Bags:   []pipeline.BagSpec{{Name: "module_input"}, {Name: "code_bag"}, {Name: "failure_report"}},
				},
				Signature: pipeline.SignatureSpec{
					OutputBags: []pipeline.BagSpec{{Name: "code_bag"}},
					ExportedHandlers: []pipeline.HandlerExportSpec{{
						Name:     "debug_code",
						Handler:  "debug_code",
						Handles:  []string{"kbug"},
						Replaces: []pipeline.BagSpec{{Name: "code_bag"}},
					}},
				},
				Handlers: []pipeline.PipelineHandlerSpec{{
					Name:        "debug_code",
					Handles:     []string{"kbug"},
					Transition:  "debug_code",
					InputBags:   []pipeline.HandlerInputBagSpec{{Name: "failure_report", Source: "exception"}, {Name: "module_input", Source: "owner_input"}, {Name: "code_bag", Source: "owner_output"}},
					ReplaceBags: []pipeline.BagSpec{{Name: "code_bag", FromTransition: "debug_code"}},
					Resume:      pipeline.ResumePolicySpec{Type: "retry_failed_transition"},
				}},
				StartState:    "start",
				DeliveryState: "done",
				States: []pipeline.StateSpec{
					{ID: "start", Kind: "start", Proof: pipeline.ProofSpec{Type: "external"}, Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"debug_code"}}},
					{ID: "done", Kind: "delivery", Proof: pipeline.ProofSpec{Type: "transition_result", Transition: "debug_code"}, Exposes: pipeline.ExposesSpec{Bags: []pipeline.BagSpec{{Name: "code_bag"}}}},
				},
				Transitions: []pipeline.TransitionSpec{{
					ID:         "debug_code",
					Kind:       "task",
					FromState:  "start",
					ToState:    "done",
					Agent:      &pipeline.AgentSpec{Role: core.AgentRoleCoder, Alias: "coder"},
					Op:         "debug_write_code",
					InputBags:  []pipeline.BagSpec{{Name: "module_input"}, {Name: "code_bag"}, {Name: "failure_report"}},
					OutputBags: []pipeline.BagSpec{{Name: "code_bag"}},
					Limits:     pipeline.LimitsSpec{MaxAttempts: 3},
				}},
			},
			{
				SchemaVersion: pipeline.PipelineSchemaVersionV04,
				PipelineID:    "checker",
				Kind:          "subpipeline",
				Namespace: pipeline.NamespaceSpec{
					Agents: []pipeline.SignatureAgentSpec{{Name: "tester", Role: core.AgentRoleTester, DefaultAgentID: "tester01"}},
					Bags:   []pipeline.BagSpec{{Name: "code_bag"}, {Name: "failure_report"}},
				},
				Signature: pipeline.SignatureSpec{
					InputBags: []pipeline.BagSpec{{Name: "code_bag"}},
					Throws:    []pipeline.ExceptionSpec{{Result: "kbug", Bags: []pipeline.BagSpec{{Name: "failure_report"}}}},
				},
				StartState:    "start",
				DeliveryState: "done",
				States: []pipeline.StateSpec{
					{ID: "start", Kind: "start", Proof: pipeline.ProofSpec{Type: "external"}, Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"test_code"}}},
					{ID: "done", Kind: "delivery", Proof: pipeline.ProofSpec{Type: "transition_result", Transition: "test_code"}},
				},
				Transitions: []pipeline.TransitionSpec{{
					ID:        "test_code",
					Kind:      "task",
					FromState: "start",
					ToState:   "done",
					Agent:     &pipeline.AgentSpec{Role: core.AgentRoleTester, Alias: "tester"},
					Op:        core.TaskOpTestCode,
					InputBags: []pipeline.BagSpec{{Name: "code_bag"}},
					OutputBagsByResult: map[string][]pipeline.BagSpec{
						"kbug": {{Name: "failure_report"}},
					},
				}},
			},
			{
				SchemaVersion: pipeline.PipelineSchemaVersionV04,
				PipelineID:    "parent",
				Kind:          "entry",
				Namespace: pipeline.NamespaceSpec{
					Agents: []pipeline.SignatureAgentSpec{{Name: "coder", Role: core.AgentRoleCoder, DefaultAgentID: "coder01"}},
					Bags:   []pipeline.BagSpec{{Name: "module_input"}, {Name: "code_bag"}, {Name: "failure_report"}},
				},
				Handlers: []pipeline.PipelineHandlerSpec{
					{
						Name:        "debug_code",
						Handles:     []string{"kbug"},
						Transition:  "debug_code",
						InputBags:   []pipeline.HandlerInputBagSpec{{Name: "failure_report", Source: "exception"}},
						ReplaceBags: []pipeline.BagSpec{{Name: "code_bag"}},
						Resume:      pipeline.ResumePolicySpec{Type: "retry_failed_transition"},
					},
				},
				ExceptionHandlers: map[string]pipeline.ExceptionHandlerPolicySpec{
					"kbug": {Select: "nearest_matching", Join: "all_success", OnUnhandled: "fail_run", Resume: pipeline.ResumePolicySpec{Type: "retry_failed_transition"}},
				},
				StartState:    "start",
				DeliveryState: "done",
				States: []pipeline.StateSpec{
					{ID: "start", Kind: "start", Proof: pipeline.ProofSpec{Type: "external"}, Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"call_owner", "call_checker"}}},
					{ID: "done", Kind: "delivery", Proof: pipeline.ProofSpec{Type: "external"}},
				},
				Transitions: []pipeline.TransitionSpec{
					{ID: "call_owner", Kind: "call", PipelineID: "owner", FromState: "start", ToState: "done", Mode: "single", OutputBags: []pipeline.BagSpec{{Name: "code_bag"}}, OutputHandlers: []pipeline.HandlerBindingSpec{{Name: "debug_code", FromHandler: "debug_code"}}},
					{ID: "call_checker", Kind: "call", PipelineID: "checker", FromState: "start", ToState: "done", Mode: "single", Bindings: &pipeline.BindingsSpec{InputBags: map[string]any{"code_bag": map[string]any{"name": "code_bag"}}}},
					{ID: "debug_code", Kind: "task", FromState: "start", ToState: "done", Agent: &pipeline.AgentSpec{Role: core.AgentRoleCoder, Alias: "coder"}, Op: "debug_write_code", Limits: pipeline.LimitsSpec{MaxAttempts: 3}, OutputBags: []pipeline.BagSpec{{Name: "code_bag"}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	service := NewService(pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "parent"}), runRepo, taskRepo, noopProvisioner{}, recordingDispatchSink{}, nil, nil)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	if err := runRepo.Create(ctx, core.PipelineRun{ID: "run_bubble", PipelineID: "parent", Status: core.RunStatusRunning, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	parentID := core.PipelineInstanceID("parent")
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:              parentID,
		RunID:           "run_bubble",
		PipelineID:      "parent",
		Status:          core.PipelineInstanceStatusRunning,
		HandlerBindings: []core.HandlerBindingRef{{Name: "debug_code", OwnerInstanceID: "owner01", Handles: []string{"kbug"}, Replaces: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}}}},
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("Create(parent instance) error = %v", err)
	}
	ownerParent := parentID
	moduleVersion := createTestArtifactVersion(t, ctx, doujiaGitRepo, "run_bubble", "architect01", "module")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{BagID: "bag_module", RunID: "run_bubble", ArtifactVersionIDs: []string{moduleVersion}, CreatedAt: now}); err != nil {
		t.Fatalf("CreateBag(module) error = %v", err)
	}
	codeVersionV1 := createTestArtifactVersion(t, ctx, doujiaGitRepo, "run_bubble", "coder01", "code_v1")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{BagID: "bag_code_v1", RunID: "run_bubble", ArtifactVersionIDs: []string{codeVersionV1}, CreatedAt: now}); err != nil {
		t.Fatalf("CreateBag(code v1) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:                 "owner01",
		RunID:              "run_bubble",
		PipelineID:         "owner",
		ParentID:           &ownerParent,
		ParentTransitionID: "call_owner",
		Status:             core.PipelineInstanceStatusCompleted,
		AgentBindings:      map[string]core.AgentID{"coder": "coder01"},
		InputBagIDs:        map[string]string{"module_input": "bag_module"},
		OutputBagIDs:       map[string]string{"code_bag": "bag_code_v1"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(owner instance) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:                 "checker01",
		RunID:              "run_bubble",
		PipelineID:         "checker",
		ParentID:           &ownerParent,
		ParentTransitionID: "call_checker",
		Status:             core.PipelineInstanceStatusRunning,
		AgentBindings:      map[string]core.AgentID{"tester": "tester01"},
		InputBagIDs:        map[string]string{"code_bag": "bag_code_v1"},
		InputBagIDLists:    map[string][]string{"code_bag": {"bag_code_v1"}},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(checker instance) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:                 "checker01_test_code",
		RunID:              "run_bubble",
		PipelineInstanceID: "checker01",
		StageID:            "test_code",
		AgentRole:          core.AgentRoleTester,
		AgentID:            "tester01",
		Op:                 core.TaskOpTestCode,
		Status:             core.TaskStatusDispatched,
		InputBagIDs:        []string{"bag_code_v1"},
		InputBags:          []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(test task) error = %v", err)
	}

	failureVersion := createTestArtifactVersion(t, ctx, doujiaGitRepo, "run_bubble", "tester01", "failure")
	err = service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     "run_bubble",
		TaskID:    "checker01_test_code",
		AgentID:   "tester01",
		Op:        core.TaskOpTestCode,
		InputBags: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}},
		Result:    core.TaskResultCodeBug,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeBug, ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion}}}},
	})
	if err != nil {
		t.Fatalf("OnFeedback(kbug) error = %v", err)
	}
	debugTask, err := taskRepo.Get(ctx, "run_bubble", "owner01_debug_code")
	if err != nil {
		t.Fatalf("Get(debug task) error = %v", err)
	}
	if debugTask.ExecutionMode != core.ExecutionModeRepair || debugTask.ExceptionFrameID == "" {
		t.Fatalf("debug task = %#v, want repair task with exception frame", debugTask)
	}
	if got := debugTask.InputBagIDs; !sameStringSet(got, []string{"bag_module", "bag_code_v1"}) {
		t.Fatalf("debug input bags = %v, want owner input + output", got)
	}

	codeVersion := createTestArtifactVersion(t, ctx, doujiaGitRepo, "run_bubble", "coder01", "code_v2")
	err = service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     "run_bubble",
		TaskID:    "owner01_debug_code",
		AgentID:   "coder01",
		Op:        "debug_write_code",
		Result:    core.TaskResultCodeOK,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{codeVersion}}}},
	})
	if err != nil {
		t.Fatalf("OnFeedback(debug ok) error = %v", err)
	}
	owner, err := instanceRepo.Get(ctx, "run_bubble", "owner01")
	if err != nil {
		t.Fatalf("Get(owner) error = %v", err)
	}
	if owner.OutputBagIDs["code_bag"] == "bag_code_v1" || owner.OutputBagIDs["code_bag"] == "" {
		t.Fatalf("owner code_bag = %#v, want replaced with new code bag", owner.OutputBagIDs)
	}
	retryTask, err := taskRepo.Get(ctx, "run_bubble", "checker01_test_code")
	if err != nil {
		t.Fatalf("Get(retry task) error = %v", err)
	}
	if retryTask.Status != core.TaskStatusDispatched {
		t.Fatalf("retry task status = %s, want dispatched", retryTask.Status)
	}
	if containsString(retryTask.InputBagIDs, "bag_code_v1") {
		t.Fatalf("retry input bags = %v, old code bag should have been replaced", retryTask.InputBagIDs)
	}
	if got, want := retryTask.InputBagIDs, []string{owner.OutputBagIDs["code_bag"]}; !sameStringSet(got, want) {
		t.Fatalf("retry input bags = %v, want only replacement bag %s", got, owner.OutputBagIDs["code_bag"])
	}
	if len(retryTask.InputBags) != 1 || retryTask.InputBags[0].BagID != owner.OutputBagIDs["code_bag"] {
		t.Fatalf("retry input bag bindings = %+v, want replacement code bag %s", retryTask.InputBags, owner.OutputBagIDs["code_bag"])
	}
	snapshots, err := doujiaGitRepo.ListSnapshotsByRun(ctx, "run_bubble")
	if err != nil {
		t.Fatalf("ListSnapshotsByRun() error = %v", err)
	}
	var failedSnapshot, repairSnapshot doujiagit.TaskSnapshot
	for _, snapshot := range snapshots {
		switch snapshot.TaskID {
		case "checker01_test_code":
			failedSnapshot = snapshot
		case "owner01_debug_code":
			repairSnapshot = snapshot
		}
	}
	if failedSnapshot.SnapshotID == "" || failedSnapshot.Result != core.TaskResultCodeBug {
		t.Fatalf("failed snapshot = %+v, want checker kbug history", failedSnapshot)
	}
	if repairSnapshot.SnapshotID == "" {
		t.Fatalf("repair snapshot not found in %+v", snapshots)
	}
	if repairSnapshot.BranchKind != doujiagit.DecisionKindRepair ||
		repairSnapshot.RecoverFromSnapshotID != failedSnapshot.SnapshotID ||
		!reflect.DeepEqual(repairSnapshot.FailureReportBagIDs, failedSnapshot.OutputBagIDs) ||
		!reflect.DeepEqual(repairSnapshot.PreviousOutputBagIDs, []string{"bag_code_v1"}) ||
		repairSnapshot.RepairTargetTransitionID != "debug_code" ||
		repairSnapshot.RepairTargetTaskID != "owner01_debug_code" {
		t.Fatalf("repair snapshot metadata = %+v, failed = %+v", repairSnapshot, failedSnapshot)
	}
	decision, err := doujiaGitRepo.GetSnapshotProcessingDecision(ctx, "run_bubble", doujiagit.DefaultRefName, repairSnapshot.SnapshotID)
	if err != nil {
		t.Fatalf("GetSnapshotProcessingDecision(repair) error = %v", err)
	}
	if decision.DecisionKind != doujiagit.DecisionKindRepair ||
		decision.FailedSnapshotID != failedSnapshot.SnapshotID ||
		!reflect.DeepEqual(decision.FailureReportBagIDs, failedSnapshot.OutputBagIDs) ||
		!reflect.DeepEqual(decision.PreviousOutputBagIDs, []string{"bag_code_v1"}) ||
		decision.RepairTargetTransitionID != "debug_code" ||
		decision.RepairTargetTaskID != "owner01_debug_code" {
		t.Fatalf("repair processing decision = %+v", decision)
	}
}

type noopProvisioner struct{}

func (noopProvisioner) EnsureAgent(context.Context, runtime.EnsureAgentRequest) (runtime.EnsureAgentResult, error) {
	return runtime.EnsureAgentResult{}, nil
}

type recordingDispatchSink struct{}

func (recordingDispatchSink) Dispatch(context.Context, core.TaskMetaData) error {
	return nil
}

func createTestArtifactVersion(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID, agentID core.AgentID, suffix string) string {
	t.Helper()
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte(string(runID) + ":" + string(agentID) + ":" + suffix)),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/" + string(agentID) + "/" + suffix,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertObject(%s) error = %v", suffix, err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  string(agentID),
		LogicalKey: suffix,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact(%s) error = %v", suffix, err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion(%s) error = %v", suffix, err)
	}
	return version.ArtifactVersionID
}
