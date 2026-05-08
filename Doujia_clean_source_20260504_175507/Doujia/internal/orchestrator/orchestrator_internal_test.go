package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/pipeline"
	"devflow/internal/state/repo"
)

func TestPipelineJSONLinearSchedulesFromSyntheticBags(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "pipelines", "scheduling_linear.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(linear) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(linear) error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(linear) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_json_linear")
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "scheduling_linear",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	codeBagVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v1")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "write_code",
		AgentID:   "coder01",
		Op:        core.TaskOpWriteCode,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{
				Name:               "code_bag",
				ArtifactVersionIDs: []string{codeBagVersion},
			}},
		},
	}); err != nil {
		t.Fatalf("write_code feedback error = %v", err)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("dispatch count = %d, want test_code dispatch", len(dispatcher.dispatched))
	}
	dispatch := dispatcher.dispatched[0]
	if dispatch.TaskID != "test_code" || dispatch.Op != core.TaskOpTestCode {
		t.Fatalf("dispatch = %+v, want test_code", dispatch)
	}
	if dispatch.SourceSnapshotID == "" || dispatch.SourceFrontierSnapshotID == "" || dispatch.SourceRefName != doujiagit.DefaultRefName {
		t.Fatalf("dispatch provenance = %+v, want committed DoujiaGit source", dispatch)
	}
	if len(dispatch.InputBags) != 1 || dispatch.InputBags[0].Name != "code_bag" {
		t.Fatalf("dispatch input bags = %+v, want code_bag", dispatch.InputBags)
	}
	decision, err := doujiaGitRepo.GetSnapshotProcessingDecision(ctx, runID, doujiagit.DefaultRefName, dispatch.SourceSnapshotID)
	if err != nil {
		t.Fatalf("GetSnapshotProcessingDecision() error = %v", err)
	}
	if decision.Status != doujiagit.SnapshotProcessingStatusAdvanced || !reflect.DeepEqual(decision.ProducedTaskIDs, []string{"test_code"}) {
		t.Fatalf("decision = %+v, want advanced producing test_code", decision)
	}
}

func TestPipelineJSONParallelMergeWaitsForBothSyntheticBranches(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "pipelines", "scheduling_parallel_merge.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(parallel merge) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(parallel merge) error = %v", err)
	}
	def, err := definitions.GetDef(ctx, "parallel_merge")
	if err != nil {
		t.Fatalf("GetDef(parallel_merge) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "parallel_merge"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_json_parallel_merge")
	now := time.Now().UTC()
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "parallel_merge",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	moduleInputBag := createTestBagInternal(t, ctx, doujiaGitRepo, runID, "architect01", "module_input_v1")
	instance := core.PipelineInstance{
		ID:          "root",
		RunID:       runID,
		PipelineID:  "parallel_merge",
		Status:      core.PipelineInstanceStatusCreated,
		InputBagIDs: map[string]string{"module_input": moduleInputBag},
		AgentBindings: map[string]core.AgentID{
			"coder":     "coder01",
			"tester":    "tester01",
			"architect": "architect01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := instanceRepo.Create(ctx, instance); err != nil {
		t.Fatalf("Create(instance) error = %v", err)
	}
	if err := service.startPipelineInstance(ctx, run, instance); err != nil {
		t.Fatalf("startPipelineInstance() error = %v", err)
	}
	if len(dispatcher.dispatched) != 2 {
		t.Fatalf("initial dispatch count = %d, want write_code and write_test_data", len(dispatcher.dispatched))
	}
	if !hasDispatchedTask(dispatcher.dispatched, "root_write_code", core.TaskOpWriteCode) ||
		!hasDispatchedTask(dispatcher.dispatched, "root_write_test_data", core.TaskOpTestData) {
		t.Fatalf("initial dispatches = %+v, want write_code and write_test_data", dispatcher.dispatched)
	}

	codeVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v1")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "root_write_code",
		AgentID:   "coder01",
		Op:        core.TaskOpWriteCode,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{
				Name:               "code_bag",
				Indexes:            map[string]string{"module_key": "module01"},
				ArtifactVersionIDs: []string{codeVersion},
			}},
		},
	}); err != nil {
		t.Fatalf("write_code feedback error = %v", err)
	}
	if hasDispatchedTask(dispatcher.dispatched, "root_merge_context", core.TaskOpMergeCode) {
		t.Fatalf("dispatches = %+v, merge should wait for test_data branch", dispatcher.dispatched)
	}

	testDataVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "tester01", "test_data_v1")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "root_write_test_data",
		AgentID:   "tester01",
		Op:        core.TaskOpTestData,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{
				Name:               "test_data",
				Indexes:            map[string]string{"module_key": "module01"},
				ArtifactVersionIDs: []string{testDataVersion},
			}},
		},
	}); err != nil {
		t.Fatalf("write_test_data feedback error = %v", err)
	}
	if !hasDispatchedTask(dispatcher.dispatched, "root_merge_context", core.TaskOpMergeCode) {
		t.Fatalf("dispatches = %+v, want merge_context after both branches", dispatcher.dispatched)
	}
	mergeTask, err := taskRepo.Get(ctx, runID, "root_merge_context")
	if err != nil {
		t.Fatalf("Get(merge task) error = %v", err)
	}
	if len(mergeTask.InputBags) != 2 {
		t.Fatalf("merge input bags = %+v, want code_bag and test_data", mergeTask.InputBags)
	}
	if _, ok := findTransition(def, "merge_context"); !ok {
		t.Fatalf("merge_context transition missing from parsed JSON def")
	}
}

func TestPipelineJSONCallReturnDispatchesParentContinuationFromSyntheticChildBag(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "pipelines", "scheduling_call_return.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(call return) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(call return) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "parent_call_return"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_json_call_return")
	now := time.Now().UTC()
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "parent_call_return",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	moduleInputBag := createTestBagInternal(t, ctx, doujiaGitRepo, runID, "architect01", "module_input_for_call")
	parent := core.PipelineInstance{
		ID:          "root",
		RunID:       runID,
		PipelineID:  "parent_call_return",
		Status:      core.PipelineInstanceStatusCreated,
		InputBagIDs: map[string]string{"module_input": moduleInputBag},
		AgentBindings: map[string]core.AgentID{
			"coder":  "coder01",
			"tester": "tester01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := instanceRepo.Create(ctx, parent); err != nil {
		t.Fatalf("Create(parent instance) error = %v", err)
	}
	if err := service.startPipelineInstance(ctx, run, parent); err != nil {
		t.Fatalf("startPipelineInstance(parent) error = %v", err)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("initial dispatch count = %d, want child write_code", len(dispatcher.dispatched))
	}
	childDispatch := dispatcher.dispatched[0]
	if childDispatch.Op != core.TaskOpWriteCode {
		t.Fatalf("initial dispatch = %+v, want child write_code", childDispatch)
	}
	childTask, err := taskRepo.Get(ctx, runID, childDispatch.TaskID)
	if err != nil {
		t.Fatalf("Get(child task) error = %v", err)
	}
	if childTask.PipelineInstanceID == "" || childTask.PipelineInstanceID == "root" {
		t.Fatalf("child task pipeline instance = %q, want child instance", childTask.PipelineInstanceID)
	}

	codeVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "child_code_v1")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    childTask.ID,
		AgentID:   "coder01",
		Op:        core.TaskOpWriteCode,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{
				Name:               "code_bag",
				ArtifactVersionIDs: []string{codeVersion},
			}},
		},
	}); err != nil {
		t.Fatalf("child write_code feedback error = %v", err)
	}
	if !hasDispatchedTask(dispatcher.dispatched, "root_test_code", core.TaskOpTestCode) {
		t.Fatalf("dispatches = %+v, want parent test_code after child return", dispatcher.dispatched)
	}
	testTask, err := taskRepo.Get(ctx, runID, "root_test_code")
	if err != nil {
		t.Fatalf("Get(parent test task) error = %v", err)
	}
	if testTask.PipelineInstanceID != "root" {
		t.Fatalf("test task pipeline instance = %q, want root", testTask.PipelineInstanceID)
	}
	if len(testTask.InputBags) != 1 || testTask.InputBags[0].Name != "code_bag" {
		t.Fatalf("test task input bags = %+v, want returned code_bag", testTask.InputBags)
	}
	child, err := instanceRepo.Get(ctx, runID, childTask.PipelineInstanceID)
	if err != nil {
		t.Fatalf("Get(child instance) error = %v", err)
	}
	if child.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("child status = %s, want completed", child.Status)
	}
}

func TestFullDeliveryJSONDispatchesThreeWayFanoutAfterSplit(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "full_delivery", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(full delivery) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(full delivery) error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(full delivery) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_full_delivery_three_way_fanout")
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	requirementVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "ceo", "requirement")
	productPlanVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "pm01", "product_plan")
	architectureVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "architecture")
	containerContextVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "container_context")
	frontModuleVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "front_module_input")
	backendAPIVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "backend_api_module_input")
	backendStoreVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "backend_store_module_input")
	globalTestInputVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_input")

	feedbacks := []core.TaskMetaData{
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     runID,
			TaskID:    "ceo_write_requirement",
			AgentID:   "ceo",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "requirement",
				ArtifactVersionIDs: []string{requirementVersionID},
			}}},
		},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     runID,
			TaskID:    "pm_write_product_plan",
			AgentID:   "pm01",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "product_plan",
				ArtifactVersionIDs: []string{productPlanVersionID},
			}}},
		},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "ceo_review_product_plan", AgentID: "ceo", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     runID,
			TaskID:    "architect_write_architecture",
			AgentID:   "architect01",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "architecture",
				ArtifactVersionIDs: []string{architectureVersionID},
			}}},
		},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "pm_review_architecture", AgentID: "pm01", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     runID,
			TaskID:    "architect_create_container",
			AgentID:   "architect01",
			Op:        core.TaskOpCreateContainer,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "container_context",
				ArtifactVersionIDs: []string{containerContextVersionID},
			}}},
		},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     runID,
			TaskID:    "architect_split_modules",
			AgentID:   "architect01",
			Op:        core.TaskOpSplitModule,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{
				{
					Name:               "front_module_input",
					Indexes:            map[string]string{"module_key": "front"},
					ArtifactVersionIDs: []string{frontModuleVersionID},
				},
				{
					Name:               "backend_module_input",
					Indexes:            map[string]string{"module_key": "backend_api"},
					ArtifactVersionIDs: []string{backendAPIVersionID},
				},
				{
					Name:               "backend_module_input",
					Indexes:            map[string]string{"module_key": "backend_store"},
					ArtifactVersionIDs: []string{backendStoreVersionID},
				},
				{
					Name:               "global_test_input",
					ArtifactVersionIDs: []string{globalTestInputVersionID},
				},
			}},
		},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("OnFeedback(%s) error = %v", feedback.TaskID, err)
		}
	}

	instances, err := instanceRepo.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(instances) error = %v", err)
	}
	for _, pipelineID := range []core.PipelineID{
		"pipeline_front_module",
		"pipeline_backend_module_group",
		"pipeline_global_test_data",
	} {
		if !hasPipelineInstanceForDefinition(instances, pipelineID) {
			t.Fatalf("instances = %+v, want child pipeline %s", instances, pipelineID)
		}
	}
	if !hasDispatchedOp(dispatcher.dispatched, core.TaskOpWriteCode) ||
		!hasDispatchedOp(dispatcher.dispatched, core.TaskOpTestData) {
		t.Fatalf("dispatches = %+v, want child write_code and test_data work after split", dispatcher.dispatched)
	}
}

func TestLegacyTerminalSnapshotProjectionExposesRootInputBags(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "full_delivery", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(full delivery) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(full delivery) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "pipeline_full_delivery"}),
		runRepo,
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_legacy_terminal_projection")
	now := time.Now().UTC()
	run := core.PipelineRun{ID: runID, PipelineID: "pipeline_full_delivery", Status: core.RunStatusRunning, CreatedAt: now, UpdatedAt: now}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	root, err := service.ensureRootPipelineInstance(ctx, run)
	if err != nil {
		t.Fatalf("ensureRootPipelineInstance() error = %v", err)
	}
	frontVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "front_module_input")
	backendVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "backend_module_input")
	globalVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_input")
	commit := &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{
		{Name: "front_module_input", Indexes: map[string]string{"module_key": "front"}, ArtifactVersionIDs: []string{frontVersionID}},
		{Name: "backend_module_input", Indexes: map[string]string{"module_key": "backend_api"}, ArtifactVersionIDs: []string{backendVersionID}},
		{Name: "global_test_input", ArtifactVersionIDs: []string{globalVersionID}},
	}}
	task := core.Task{ID: "architect_split_modules", RunID: runID, StageID: "architect_split_modules", AgentRole: core.AgentRoleArchitect, AgentID: "architect01", Op: core.TaskOpSplitModule}
	fact, err := service.commitFeedbackFact(ctx, task, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Op:        task.Op,
		Result:    core.TaskResultCodeOK,
		Commit:    commit,
	}, doujiagit.RefMoveModeAdvance)
	if err != nil {
		t.Fatalf("commitFeedbackFact() error = %v", err)
	}
	snapshots, err := doujiaGitRepo.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSnapshotsByRun() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, want one split snapshot", snapshots)
	}
	if _, err := service.advanceRootPipelineFromLegacyTerminalSnapshot(ctx, run, snapshots[0], fact); err != nil {
		latest, _ := instanceRepo.Get(ctx, runID, root.ID)
		t.Fatalf("advanceRootPipelineFromLegacyTerminalSnapshot() error = %v; input bags = %+v; output bags = %+v; input lists = %+v; output lists = %+v", err, latest.InputBagIDs, latest.OutputBagIDs, latest.InputBagIDLists, latest.OutputBagIDLists)
	}
	latest, err := instanceRepo.Get(ctx, runID, root.ID)
	if err != nil {
		t.Fatalf("Get(root) error = %v", err)
	}
	if latest.InputBagIDs["front_module_input"] == "" {
		t.Fatalf("root input bags = %+v, output bags = %+v, want front_module_input mirrored", latest.InputBagIDs, latest.OutputBagIDs)
	}
}

func TestFullDeliveryJSONBackendGroupReturnsOnlyAfterAllModuleChildrenComplete(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_backend_group_return")
	h.advanceToSplit(t)

	backendGroup := h.childInstanceByParentAndPipeline(t, "root", "pipeline_backend_module_group")
	moduleChildren := h.childInstancesByParentAndPipeline(t, backendGroup.ID, "pipeline_module")
	if len(moduleChildren) != 2 {
		t.Fatalf("backend module children = %+v, want two pipeline_module instances", moduleChildren)
	}
	sort.Slice(moduleChildren, func(i, j int) bool {
		return moduleChildren[i].InstanceKey < moduleChildren[j].InstanceKey
	})

	h.completeInstance(t, moduleChildren[0], map[string][]string{
		"tested_module[module_key=backend_api]": {"bag_backend_api_tested"},
		"code_bag[module_key=backend_api]":      {"bag_backend_api_code"},
		"test_data_bag[module_key=backend_api]": {"bag_backend_api_test_data"},
	})

	backendGroup = h.instance(t, backendGroup.ID)
	if backendGroup.Status == core.PipelineInstanceStatusCompleted {
		t.Fatalf("backend group status = %s, want not completed after one module return", backendGroup.Status)
	}
	root := h.instance(t, "root")
	if got := len(root.OutputBagIDLists["backend_tested_module"]); got != 0 {
		t.Fatalf("root backend outputs = %+v, want no backend return before second module", root.OutputBagIDLists)
	}
	if hasDispatchedOp(h.dispatcher.dispatched, core.TaskOpMergeCode) {
		t.Fatalf("dispatches = %+v, merge_code should not dispatch before all root branches return", h.dispatcher.dispatched)
	}

	h.completeInstance(t, moduleChildren[1], map[string][]string{
		"tested_module[module_key=backend_store]": {"bag_backend_store_tested"},
		"code_bag[module_key=backend_store]":      {"bag_backend_store_code"},
		"test_data_bag[module_key=backend_store]": {"bag_backend_store_test_data"},
	})

	backendGroup = h.instance(t, backendGroup.ID)
	if backendGroup.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("backend group status = %s, want completed after both module returns", backendGroup.Status)
	}
	root = h.instance(t, "root")
	if root.OutputBagIDs["backend_tested_module[module_key=backend_api]"] != "bag_backend_api_tested" ||
		root.OutputBagIDs["backend_tested_module[module_key=backend_store]"] != "bag_backend_store_tested" {
		t.Fatalf("root backend outputs = %+v, want indexed backend tested modules on parent root", root.OutputBagIDs)
	}
	if hasDispatchedOp(h.dispatcher.dispatched, core.TaskOpMergeCode) {
		t.Fatalf("dispatches = %+v, merge_code should still wait for front and global-test-data returns", h.dispatcher.dispatched)
	}
}

func TestFullDeliveryJSONDispatchesMergeCodeAfterThreeBranchReturns(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_merge_dispatch")
	h.advanceToSplit(t)

	front := h.childInstanceByParentAndPipeline(t, "root", "pipeline_front_module")
	globalTestData := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_data")

	h.completeInstance(t, front, map[string][]string{
		"tested_module[module_key=front]":   {"bag_front_tested"},
		"code_bag[module_key=front]":        {"bag_front_code"},
		"preview_edit_bag[module_key=front]": {"bag_front_preview"},
	})
	h.completeInstance(t, globalTestData, map[string][]string{
		"global_test_data": {"bag_global_test_data"},
	})
	if hasDispatchedOp(h.dispatcher.dispatched, core.TaskOpMergeCode) {
		t.Fatalf("dispatches = %+v, merge_code should wait for backend group return", h.dispatcher.dispatched)
	}

	h.completeBackendGroup(t)

	if !hasDispatchedOp(h.dispatcher.dispatched, core.TaskOpMergeCode) {
		root := h.instance(t, "root")
		t.Fatalf("dispatches = %+v, root outputs = %+v, root inputs = %+v, want merge_code after three branch returns", h.dispatcher.dispatched, root.OutputBagIDs, root.InputBagIDLists)
	}

	mergeTask := h.taskByOp(t, core.TaskOpMergeCode)
	gotBagNames := countTaskInputBagNames(mergeTask.InputBags)
	wantBagNames := map[string]int{
		"front_tested_module": 1,
		"backend_tested_module": 2,
		"front_code_bag": 1,
		"backend_code_bag": 2,
		"container_context": 1,
		"global_test_input": 1,
	}
	if !reflect.DeepEqual(gotBagNames, wantBagNames) {
		t.Fatalf("merge task input bag names = %+v, want %+v; raw input bags = %+v", gotBagNames, wantBagNames, mergeTask.InputBags)
	}
}

func TestFullDeliveryJSONDispatchesGlobalTestCodeAfterMergeReturn(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_global_test_dispatch")
	h.advanceToMergeDispatch(t)

	merge := h.childInstanceByParentAndPipeline(t, "root", "pipeline_merge_code")
	h.completeInstance(t, merge, map[string][]string{
		"merged_code": {"bag_merged_code"},
	})

	if !hasDispatchedOp(h.dispatcher.dispatched, core.TaskOpTestCode) {
		root := h.instance(t, "root")
		t.Fatalf("dispatches = %+v, root outputs = %+v, want global_test_code dispatch after merge return", h.dispatcher.dispatched, root.OutputBagIDs)
	}

	globalTest := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_code")
	if globalTest.InputBagIDs["merged_code"] != "bag_merged_code" {
		t.Fatalf("global test child input bags = %+v, want merged_code bag_merged_code", globalTest.InputBagIDs)
	}
	globalTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	gotBagNames := countTaskInputBagNames(globalTask.InputBags)
	wantBagNames := map[string]int{
		"merged_code":      1,
		"global_test_data": 1,
		"container_context": 1,
	}
	if !reflect.DeepEqual(gotBagNames, wantBagNames) {
		t.Fatalf("global test task input bag names = %+v, want %+v; raw input bags = %+v", gotBagNames, wantBagNames, globalTask.InputBags)
	}
}

func TestFullDeliveryJSONAwaitsAcceptanceAfterGlobalTestCodeReturn(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_awaiting_acceptance")
	h.advanceToMergeDispatch(t)

	merge := h.childInstanceByParentAndPipeline(t, "root", "pipeline_merge_code")
	h.completeInstance(t, merge, map[string][]string{
		"merged_code": {"bag_merged_code"},
	})

	globalTest := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_code")
	h.completeInstance(t, globalTest, map[string][]string{
		"global_test_report": {"bag_global_test_report"},
		"merged_code":        {"bag_merged_code"},
	})

	root := h.instance(t, "root")
	if root.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("root status = %s, want completed after global test return", root.Status)
	}
	if root.OutputBagIDs["global_test_report"] != "bag_global_test_report" {
		t.Fatalf("root output bags = %+v, want global_test_report propagated", root.OutputBagIDs)
	}
	run := h.runState(t)
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want awaiting_acceptance", run.Status)
	}
}

func TestFullDeliveryJSONGlobalTestKbugDispatchesDebugAndRetries(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_global_test_recover")
	h.advanceToMergeDispatch(t)

	merge := h.childInstanceByParentAndPipeline(t, "root", "pipeline_merge_code")
	h.completeInstance(t, merge, map[string][]string{
		"merged_code": {"bag_merged_code_v1"},
	})

	globalTest := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_code")
	globalTestTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	failureVersion := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "global_test_failure")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    globalTestTask.ID,
		AgentID:   "architect01",
		Op:        core.TaskOpTestCode,
		Result:    core.TaskResultCodeBug,
		InputBags: append([]core.BagBindingRef(nil), globalTestTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeBug, ProducedBags: []core.CommittedBagDef{{
			Name:               "failure_report",
			ArtifactVersionIDs: []string{failureVersion},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(global_test_code kbug) error = %v", err)
	}

	debugTask := h.taskByPipelineAndOp(t, globalTest.ID, "debug_global_code")
	if debugTask.Status != core.TaskStatusDispatched {
		t.Fatalf("debug task status = %s, want dispatched", debugTask.Status)
	}
	gotDebugBags := countTaskInputBagNames(debugTask.InputBags)
	wantDebugBags := map[string]int{
		"merged_code":      1,
		"global_test_data": 1,
		"container_context": 1,
		"failure_report":   1,
	}
	if !reflect.DeepEqual(gotDebugBags, wantDebugBags) {
		t.Fatalf("debug task input bag names = %+v, want %+v; raw input bags = %+v", gotDebugBags, wantDebugBags, debugTask.InputBags)
	}

	mergedCodeV2 := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "merged_code_v2")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    debugTask.ID,
		AgentID:   "architect01",
		Op:        "debug_global_code",
		Result:    core.TaskResultCodeOK,
		InputBags: append([]core.BagBindingRef(nil), debugTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
			Name:               "merged_code",
			ArtifactVersionIDs: []string{mergedCodeV2},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(debug_global_code ok) error = %v", err)
	}

	retryTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	if retryTask.Status != core.TaskStatusDispatched {
		t.Fatalf("retry task status = %s, want dispatched", retryTask.Status)
	}
	gotRetryBags := countTaskInputBagNames(retryTask.InputBags)
	wantRetryBags := map[string]int{
		"merged_code":      1,
		"global_test_data": 1,
		"container_context": 1,
	}
	if !reflect.DeepEqual(gotRetryBags, wantRetryBags) {
		t.Fatalf("retry task input bag names = %+v, want %+v; raw input bags = %+v", gotRetryBags, wantRetryBags, retryTask.InputBags)
	}
	if containsString(retryTask.InputBagIDs, "bag_merged_code_v1") {
		t.Fatalf("retry input bag ids = %v, old merged_code should have been replaced", retryTask.InputBagIDs)
	}
}

func TestFullDeliveryJSONGlobalTestRecoverStillCompletesDelivery(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_global_test_recover_delivery")
	h.advanceToMergeDispatch(t)

	merge := h.childInstanceByParentAndPipeline(t, "root", "pipeline_merge_code")
	h.completeInstance(t, merge, map[string][]string{
		"merged_code": {"bag_merged_code_v1"},
	})

	globalTest := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_code")
	globalTestTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	failureVersion := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "global_test_failure")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    globalTestTask.ID,
		AgentID:   "architect01",
		Op:        core.TaskOpTestCode,
		Result:    core.TaskResultCodeBug,
		InputBags: append([]core.BagBindingRef(nil), globalTestTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeBug, ProducedBags: []core.CommittedBagDef{{
			Name:               "failure_report",
			ArtifactVersionIDs: []string{failureVersion},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(global_test_code kbug) error = %v", err)
	}

	debugTask := h.taskByPipelineAndOp(t, globalTest.ID, "debug_global_code")
	mergedCodeV2 := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "merged_code_v2")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    debugTask.ID,
		AgentID:   "architect01",
		Op:        "debug_global_code",
		Result:    core.TaskResultCodeOK,
		InputBags: append([]core.BagBindingRef(nil), debugTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
			Name:               "merged_code",
			ArtifactVersionIDs: []string{mergedCodeV2},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(debug_global_code ok) error = %v", err)
	}

	retryTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	reportVersion := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "global_test_report")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    retryTask.ID,
		AgentID:   "architect01",
		Op:        core.TaskOpTestCode,
		Result:    core.TaskResultCodeOK,
		InputBags: append([]core.BagBindingRef(nil), retryTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
			Name:               "global_test_report",
			ArtifactVersionIDs: []string{reportVersion},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(global_test_code retry ok) error = %v", err)
	}

	root := h.instance(t, "root")
	if root.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("root status = %s, want completed after recover retry success", root.Status)
	}
	if root.OutputBagIDs["global_test_report"] == "" {
		t.Fatalf("root output bags = %+v, want global_test_report after recover retry success", root.OutputBagIDs)
	}
	run := h.runState(t)
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want awaiting_acceptance after recover retry success", run.Status)
	}
}

func TestFullDeliveryJSONRootOutputHandlerRepairsDeliveredGlobalTestChild(t *testing.T) {
	h := newFullDeliveryJSONHarness(t, "run_full_delivery_root_output_handler_repair")
	h.advanceToMergeDispatch(t)

	merge := h.childInstanceByParentAndPipeline(t, "root", "pipeline_merge_code")
	h.completeInstance(t, merge, map[string][]string{
		"merged_code": {"bag_merged_code_v1"},
	})

	globalTest := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_code")
	h.completeInstance(t, globalTest, map[string][]string{
		"global_test_report": {"bag_global_test_report_v1"},
		"merged_code":        {"bag_merged_code_v1"},
	})

	root := h.instance(t, "root")
	binding, ok := findHandlerBindingByName(root.HandlerBindings, "debug_global_code")
	if !ok {
		t.Fatalf("root handler bindings = %+v, want debug_global_code export from global_test_code child", root.HandlerBindings)
	}

	failureVersion := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "acceptance", "global_test_failure")
	const failureBagID = "bag_acceptance_failure"
	if err := h.doujiaGit.CreateBag(h.ctx, doujiagit.ArtifactBag{
		BagID:              failureBagID,
		RunID:              h.runID,
		ArtifactVersionIDs: []string{failureVersion},
		CreatedAt:          time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateBag(%s) error = %v", failureBagID, err)
	}

	frame := core.ExceptionFrame{
		ID:                       "frame_global_acceptance_bug",
		RunID:                    h.runID,
		Result:                   core.TaskResultCodeBug,
		OriginPipelineInstanceID: globalTest.ID,
		ResumeTransitionID:       "global_test_code",
		FailedInputBags:          []core.BagBindingRef{{Name: "merged_code", BagID: "bag_merged_code_v1"}},
		FailureBags:              []core.BagBindingRef{{Name: "failure_report", BagID: failureBagID}},
		SelectedHandlers:         []core.HandlerBindingRef{binding},
		CreatedAt:                time.Now().UTC(),
	}
	root.ExceptionFrames = append(root.ExceptionFrames, frame)
	root.UpdatedAt = time.Now().UTC()
	if err := h.instanceRepo.Update(h.ctx, root); err != nil {
		t.Fatalf("Update(root with exception frame) error = %v", err)
	}

	if err := h.service.dispatchRepairHandler(h.ctx, h.run, binding, frame); err != nil {
		t.Fatalf("dispatchRepairHandler() error = %v", err)
	}

	debugTask := h.taskByPipelineAndOp(t, globalTest.ID, "debug_global_code")
	gotDebugBags := countTaskInputBagNames(debugTask.InputBags)
	wantDebugBags := map[string]int{
		"merged_code":      1,
		"global_test_data": 1,
		"container_context": 1,
		"failure_report":   1,
	}
	if !reflect.DeepEqual(gotDebugBags, wantDebugBags) {
		t.Fatalf("debug task input bag names = %+v, want %+v; raw input bags = %+v", gotDebugBags, wantDebugBags, debugTask.InputBags)
	}

	mergedCodeV2 := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "merged_code_v2")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    debugTask.ID,
		AgentID:   "architect01",
		Op:        "debug_global_code",
		Result:    core.TaskResultCodeOK,
		InputBags: append([]core.BagBindingRef(nil), debugTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
			Name:               "merged_code",
			ArtifactVersionIDs: []string{mergedCodeV2},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(debug_global_code ok) error = %v", err)
	}

	retryTask := h.taskByPipelineAndOp(t, globalTest.ID, core.TaskOpTestCode)
	if containsString(retryTask.InputBagIDs, "bag_merged_code_v1") {
		t.Fatalf("retry input bag ids = %v, old merged_code should have been replaced", retryTask.InputBagIDs)
	}

	reportVersion := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "global_test_report_v2")
	if err := h.service.OnFeedback(h.ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     h.runID,
		TaskID:    retryTask.ID,
		AgentID:   "architect01",
		Op:        core.TaskOpTestCode,
		Result:    core.TaskResultCodeOK,
		InputBags: append([]core.BagBindingRef(nil), retryTask.InputBags...),
		Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
			Name:               "global_test_report",
			ArtifactVersionIDs: []string{reportVersion},
		}}},
	}); err != nil {
		t.Fatalf("OnFeedback(global_test_code retry ok) error = %v", err)
	}

	root = h.instance(t, "root")
	if root.OutputBagIDs["global_test_report"] == "bag_global_test_report_v1" {
		t.Fatalf("root output bags = %+v, want latest global_test_report propagated after output-handler repair", root.OutputBagIDs)
	}
}

func TestPipelineJSONRecoverRepairPreservesFailedSnapshotAndRetriesWithReplacementBag(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "pipelines", "scheduling_recover_repair.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(recover repair) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(recover repair) error = %v", err)
	}
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "parent_recover_repair"}), runRepo, taskRepo, noopProvisioner{}, dispatcher, nil, nil)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_json_recover_repair")
	if err := runRepo.Create(ctx, core.PipelineRun{ID: runID, PipelineID: "parent_recover_repair", Status: core.RunStatusRunning, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	parentID := core.PipelineInstanceID("parent")
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:              parentID,
		RunID:           runID,
		PipelineID:      "parent_recover_repair",
		Status:          core.PipelineInstanceStatusRunning,
		HandlerBindings: []core.HandlerBindingRef{{Name: "debug_code", OwnerInstanceID: "owner01", Handles: []string{"kbug"}, Replaces: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}}}},
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("Create(parent instance) error = %v", err)
	}
	ownerParent := parentID
	moduleVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "module")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{BagID: "bag_module", RunID: runID, ArtifactVersionIDs: []string{moduleVersion}, CreatedAt: now}); err != nil {
		t.Fatalf("CreateBag(module) error = %v", err)
	}
	codeVersionV1 := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v1")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{BagID: "bag_code_v1", RunID: runID, ArtifactVersionIDs: []string{codeVersionV1}, CreatedAt: now}); err != nil {
		t.Fatalf("CreateBag(code v1) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:                 "owner01",
		RunID:              runID,
		PipelineID:         "owner_repairable_code",
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
		RunID:              runID,
		PipelineID:         "checker_throws_bug",
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
		RunID:              runID,
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

	failureVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "tester01", "failure")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "checker01_test_code",
		AgentID:   "tester01",
		Op:        core.TaskOpTestCode,
		InputBags: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}},
		Result:    core.TaskResultCodeBug,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeBug, ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion}}}},
	}); err != nil {
		t.Fatalf("OnFeedback(kbug) error = %v", err)
	}
	debugTask, err := taskRepo.Get(ctx, runID, "owner01_debug_code")
	if err != nil {
		t.Fatalf("Get(debug task) error = %v", err)
	}
	if debugTask.ExecutionMode != core.ExecutionModeRepair || debugTask.ExceptionFrameID == "" {
		t.Fatalf("debug task = %#v, want repair task with exception frame", debugTask)
	}
	if got := debugTask.InputBagIDs; !sameStringSet(got, []string{"bag_module", "bag_code_v1"}) {
		t.Fatalf("debug input bags = %v, want owner input + previous code", got)
	}

	codeVersionV2 := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v2")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "owner01_debug_code",
		AgentID:   "coder01",
		Op:        "debug_write_code",
		Result:    core.TaskResultCodeOK,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{codeVersionV2}}}},
	}); err != nil {
		t.Fatalf("OnFeedback(debug ok) error = %v", err)
	}
	owner, err := instanceRepo.Get(ctx, runID, "owner01")
	if err != nil {
		t.Fatalf("Get(owner) error = %v", err)
	}
	if owner.OutputBagIDs["code_bag"] == "bag_code_v1" || owner.OutputBagIDs["code_bag"] == "" {
		t.Fatalf("owner code_bag = %#v, want replacement bag", owner.OutputBagIDs)
	}
	retryTask, err := taskRepo.Get(ctx, runID, "checker01_test_code")
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

	snapshots, err := doujiaGitRepo.ListSnapshotsByRun(ctx, runID)
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
		t.Fatalf("failed snapshot = %+v, want immutable kbug history", failedSnapshot)
	}
	if repairSnapshot.SnapshotID == "" || repairSnapshot.BranchKind != doujiagit.DecisionKindRepair {
		t.Fatalf("repair snapshot = %+v, want repair branch", repairSnapshot)
	}
	if repairSnapshot.RecoverFromSnapshotID != failedSnapshot.SnapshotID ||
		!reflect.DeepEqual(repairSnapshot.FailureReportBagIDs, failedSnapshot.OutputBagIDs) ||
		!reflect.DeepEqual(repairSnapshot.PreviousOutputBagIDs, []string{"bag_code_v1"}) ||
		repairSnapshot.RepairTargetTransitionID != "debug_code" ||
		repairSnapshot.RepairTargetTaskID != "owner01_debug_code" {
		t.Fatalf("repair metadata = %+v, failed = %+v", repairSnapshot, failedSnapshot)
	}
}

func TestPipelineJSONPartialRepairPreservesReusableSiblingBag(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "pipelines", "scheduling_partial_repair_sibling.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(partial repair sibling) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(partial repair sibling) error = %v", err)
	}
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "parent_partial_repair"}), runRepo, taskRepo, noopProvisioner{}, dispatcher, nil, nil)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_json_partial_repair_sibling")
	if err := runRepo.Create(ctx, core.PipelineRun{ID: runID, PipelineID: "parent_partial_repair", Status: core.RunStatusRunning, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	parentID := core.PipelineInstanceID("parent")
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:               parentID,
		RunID:            runID,
		PipelineID:       "parent_partial_repair",
		Status:           core.PipelineInstanceStatusRunning,
		OutputBagIDs:     map[string]string{"code_bag": "bag_code_v1", "test_data": "bag_test_data_v1"},
		OutputBagIDLists: map[string][]string{"code_bag": {"bag_code_v1"}, "test_data": {"bag_test_data_v1"}},
		HandlerBindings:  []core.HandlerBindingRef{{Name: "debug_code", OwnerInstanceID: "owner01", Handles: []string{"kbug"}, Replaces: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}}}},
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("Create(parent instance) error = %v", err)
	}
	ownerParent := parentID
	moduleVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "architect01", "module")
	codeVersionV1 := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v1")
	testDataVersionV1 := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "tester01", "test_data_v1")
	for _, bag := range []doujiagit.ArtifactBag{
		{BagID: "bag_module", RunID: runID, ArtifactVersionIDs: []string{moduleVersion}, CreatedAt: now},
		{BagID: "bag_code_v1", RunID: runID, ArtifactVersionIDs: []string{codeVersionV1}, CreatedAt: now},
		{BagID: "bag_test_data_v1", RunID: runID, ArtifactVersionIDs: []string{testDataVersionV1}, CreatedAt: now},
	} {
		if err := doujiaGitRepo.CreateBag(ctx, bag); err != nil {
			t.Fatalf("CreateBag(%s) error = %v", bag.BagID, err)
		}
	}
	if err := doujiaGitRepo.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:         "snapshot:test_data_v1",
		RunID:              runID,
		TaskID:             "testdata01_write_test_data",
		LogicalSnapshotID:  "test_data:module01",
		SnapshotVersionID:  "snapshot:test_data_v1:v1",
		SnapshotVersionNo:  1,
		ArrivalKind:        doujiagit.RefMoveModeAdvance,
		PipelineInstanceID: "testdata01",
		TransitionID:       "write_test_data",
		AgentRole:          core.AgentRoleTester,
		AgentID:            "tester01",
		Op:                 core.TaskOpTestData,
		Result:             core.TaskResultCodeOK,
		OutputBagIDs:       []string{"bag_test_data_v1"},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateSnapshot(test data) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:                 "owner01",
		RunID:              runID,
		PipelineID:         "owner_repairable_code",
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
		ID:                 "testdata01",
		RunID:              runID,
		PipelineID:         "test_data_ready",
		ParentID:           &ownerParent,
		ParentTransitionID: "call_test_data",
		Status:             core.PipelineInstanceStatusCompleted,
		AgentBindings:      map[string]core.AgentID{"tester": "tester01"},
		InputBagIDs:        map[string]string{"module_input": "bag_module"},
		OutputBagIDs:       map[string]string{"test_data": "bag_test_data_v1"},
		OutputBagIDLists:   map[string][]string{"test_data": {"bag_test_data_v1"}},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(test data instance) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:                 "checker01",
		RunID:              runID,
		PipelineID:         "checker_uses_code_and_data",
		ParentID:           &ownerParent,
		ParentTransitionID: "call_checker",
		Status:             core.PipelineInstanceStatusRunning,
		AgentBindings:      map[string]core.AgentID{"tester": "tester01"},
		InputBagIDs:        map[string]string{"code_bag": "bag_code_v1", "test_data": "bag_test_data_v1"},
		InputBagIDLists:    map[string][]string{"code_bag": {"bag_code_v1"}, "test_data": {"bag_test_data_v1"}},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(checker instance) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:                 "checker01_test_code",
		RunID:              runID,
		PipelineInstanceID: "checker01",
		StageID:            "test_code",
		AgentRole:          core.AgentRoleTester,
		AgentID:            "tester01",
		Op:                 core.TaskOpTestCode,
		Status:             core.TaskStatusDispatched,
		InputBagIDs:        []string{"bag_code_v1", "bag_test_data_v1"},
		InputBags:          []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}, {Name: "test_data", BagID: "bag_test_data_v1"}},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(test task) error = %v", err)
	}

	failureVersion := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "tester01", "failure")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "checker01_test_code",
		AgentID:   "tester01",
		Op:        core.TaskOpTestCode,
		InputBags: []core.BagBindingRef{{Name: "code_bag", BagID: "bag_code_v1"}, {Name: "test_data", BagID: "bag_test_data_v1"}},
		Result:    core.TaskResultCodeBug,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeBug, ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion}}}},
	}); err != nil {
		t.Fatalf("OnFeedback(kbug) error = %v", err)
	}
	debugTask, err := taskRepo.Get(ctx, runID, "owner01_debug_code")
	if err != nil {
		t.Fatalf("Get(debug task) error = %v", err)
	}
	if debugTask.ExecutionMode != core.ExecutionModeRepair || debugTask.ExceptionFrameID == "" {
		t.Fatalf("debug task = %#v, want repair task with exception frame", debugTask)
	}

	codeVersionV2 := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "coder01", "code_v2")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "owner01_debug_code",
		AgentID:   "coder01",
		Op:        "debug_write_code",
		Result:    core.TaskResultCodeOK,
		Commit:    &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{codeVersionV2}}}},
	}); err != nil {
		t.Fatalf("OnFeedback(debug ok) error = %v", err)
	}
	owner, err := instanceRepo.Get(ctx, runID, "owner01")
	if err != nil {
		t.Fatalf("Get(owner) error = %v", err)
	}
	retryTask, err := taskRepo.Get(ctx, runID, "checker01_test_code")
	if err != nil {
		t.Fatalf("Get(retry task) error = %v", err)
	}
	if retryTask.Status != core.TaskStatusDispatched {
		t.Fatalf("retry task status = %s, want dispatched", retryTask.Status)
	}
	if containsString(retryTask.InputBagIDs, "bag_code_v1") {
		t.Fatalf("retry input bags = %v, old code bag should have been replaced", retryTask.InputBagIDs)
	}
	if got, want := retryTask.InputBagIDs, []string{owner.OutputBagIDs["code_bag"], "bag_test_data_v1"}; !sameStringSet(got, want) {
		t.Fatalf("retry input bags = %v, want repaired code plus reusable test data %v", got, want)
	}
	if len(retryTask.InputBags) != 2 {
		t.Fatalf("retry input bindings = %+v, want code_bag and reusable test_data", retryTask.InputBags)
	}

	snapshots, err := doujiaGitRepo.ListSnapshotsByRun(ctx, runID)
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
		t.Fatalf("failed snapshot = %+v, want immutable kbug history", failedSnapshot)
	}
	if repairSnapshot.SnapshotID == "" || repairSnapshot.BranchKind != doujiagit.DecisionKindRepair {
		t.Fatalf("repair snapshot = %+v, want repair branch", repairSnapshot)
	}
	if !containsString(repairSnapshot.ReusableSnapshotIDs, "snapshot:test_data_v1") {
		t.Fatalf("repair reusable snapshots = %v, want reusable sibling test data snapshot", repairSnapshot.ReusableSnapshotIDs)
	}
}

func TestRequireDoujiaGitRejectsMissingRepository(t *testing.T) {
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "requires_doujiagit"}),
		repo.NewMemoryRunRepository(),
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)

	_, err := service.requireDoujiaGit()
	if !errors.Is(err, ErrDoujiaGitRequired) {
		t.Fatalf("requireDoujiaGit() error = %v, want ErrDoujiaGitRequired", err)
	}
}

func TestRequireDoujiaGitReturnsConfiguredRepository(t *testing.T) {
	repository := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "requires_doujiagit"}),
		repo.NewMemoryRunRepository(),
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(repository)

	got, err := service.requireDoujiaGit()
	if err != nil {
		t.Fatalf("requireDoujiaGit() error = %v", err)
	}
	if got != repository {
		t.Fatalf("requireDoujiaGit() returned %T, want configured repository", got)
	}
}

func TestAdvanceByFactsRequiresDoujiaGitRepository(t *testing.T) {
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "requires_doujiagit"}),
		repo.NewMemoryRunRepository(),
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)

	err := service.advanceByFacts(context.Background(), core.PipelineRun{ID: "run_requires_doujiagit", PipelineID: "requires_doujiagit"})
	if !errors.Is(err, ErrDoujiaGitRequired) {
		t.Fatalf("advanceByFacts() error = %v, want ErrDoujiaGitRequired", err)
	}
}

func TestRunFactProjectionReportsUnconsumedActiveMember(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_fact_projection_unconsumed"
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "fact_projection"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	run := core.PipelineRun{ID: runID, PipelineID: "fact_projection", Status: core.RunStatusRunning, CreatedAt: now, UpdatedAt: now}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	snapshot := doujiagit.TaskSnapshot{
		SnapshotID:        "snapshot:projection:one",
		RunID:             runID,
		TaskID:            "task_01",
		SnapshotVersionID: "snapshot:projection:one:v1",
		SnapshotVersionNo: 1,
		ArrivalKind:       doujiagit.RefMoveModeAdvance,
		Result:            core.TaskResultCodeOK,
		CreatedAt:         now,
	}
	if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:projection:one",
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshot.SnapshotID},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if _, _, err := doujiaGitRepo.MoveRef(ctx, doujiagit.MoveRefRequest{
		Ref: doujiagit.Ref{
			RefName:                   doujiagit.DefaultRefName,
			RunID:                     runID,
			FrontierSnapshotID:        "frontier:projection:one",
			FrontierMemberSnapshotIDs: []string{snapshot.SnapshotID},
			UpdatedAt:                 now,
		},
		Event: doujiagit.RefMoveEvent{
			EventID:               "event:projection:one",
			RunID:                 runID,
			RefName:               doujiagit.DefaultRefName,
			ToFrontierSnapshotIDs: []string{"frontier:projection:one"},
			Mode:                  doujiagit.RefMoveModeAdvance,
			CreatedAt:             now,
		},
	}); err != nil {
		t.Fatalf("MoveRef() error = %v", err)
	}

	projection, err := service.buildRunFactProjection(ctx, run)
	if err != nil {
		t.Fatalf("buildRunFactProjection() error = %v", err)
	}
	if projection.FrontierSnapshotID != "frontier:projection:one" {
		t.Fatalf("FrontierSnapshotID = %q, want frontier:projection:one", projection.FrontierSnapshotID)
	}
	if !reflect.DeepEqual(projection.ActiveSnapshotIDs, []string{snapshot.SnapshotID}) {
		t.Fatalf("ActiveSnapshotIDs = %#v, want active snapshot", projection.ActiveSnapshotIDs)
	}
	if !reflect.DeepEqual(projection.UnconsumedSnapshotIDs, []string{snapshot.SnapshotID}) {
		t.Fatalf("UnconsumedSnapshotIDs = %#v, want unconsumed snapshot", projection.UnconsumedSnapshotIDs)
	}
	if !projection.HasAdvanceableWork {
		t.Fatal("HasAdvanceableWork = false, want true")
	}
	if projection.LegacyProjectionIsStale {
		t.Fatal("LegacyProjectionIsStale = true, want false while legacy run is running")
	}
}

func TestRunFactProjectionDoesNotTreatConsumedOnlyAsFailure(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_fact_projection_consumed"
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "fact_projection"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	run := core.PipelineRun{ID: runID, PipelineID: "fact_projection", Status: core.RunStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	snapshot := doujiagit.TaskSnapshot{
		SnapshotID:        "snapshot:projection:consumed",
		RunID:             runID,
		TaskID:            "task_01",
		SnapshotVersionID: "snapshot:projection:consumed:v1",
		SnapshotVersionNo: 1,
		ArrivalKind:       doujiagit.RefMoveModeAdvance,
		Result:            core.TaskResultCodeOK,
		CreatedAt:         now,
	}
	if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:projection:consumed",
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshot.SnapshotID},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if _, _, err := doujiaGitRepo.MoveRef(ctx, doujiagit.MoveRefRequest{
		Ref: doujiagit.Ref{
			RefName:                   doujiagit.DefaultRefName,
			RunID:                     runID,
			FrontierSnapshotID:        "frontier:projection:consumed",
			FrontierMemberSnapshotIDs: []string{snapshot.SnapshotID},
			UpdatedAt:                 now,
		},
		Event: doujiagit.RefMoveEvent{
			EventID:               "event:projection:consumed",
			RunID:                 runID,
			RefName:               doujiagit.DefaultRefName,
			ToFrontierSnapshotIDs: []string{"frontier:projection:consumed"},
			Mode:                  doujiagit.RefMoveModeAdvance,
			CreatedAt:             now,
		},
	}); err != nil {
		t.Fatalf("MoveRef() error = %v", err)
	}
	if err := doujiaGitRepo.CreateSnapshotProcessingDecision(ctx, doujiagit.SnapshotProcessingDecision{
		RunID:      runID,
		RefName:    doujiagit.DefaultRefName,
		SnapshotID: snapshot.SnapshotID,
		Status:     doujiagit.SnapshotProcessingStatusAdvanced,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateSnapshotProcessingDecision() error = %v", err)
	}

	projection, err := service.buildRunFactProjection(ctx, run)
	if err != nil {
		t.Fatalf("buildRunFactProjection() error = %v", err)
	}
	if len(projection.UnconsumedSnapshotIDs) != 0 {
		t.Fatalf("UnconsumedSnapshotIDs = %#v, want none", projection.UnconsumedSnapshotIDs)
	}
	if projection.HasAdvanceableWork {
		t.Fatal("HasAdvanceableWork = true, want false for consumed-only frontier")
	}
	if len(projection.FailedSnapshotIDs) != 0 {
		t.Fatalf("FailedSnapshotIDs = %#v, want none because no-work is not failure", projection.FailedSnapshotIDs)
	}
	if !projection.LegacyProjectionIsStale {
		t.Fatal("LegacyProjectionIsStale = false, want true because facts do not prove failure")
	}
}

func TestSyncLegacyStatusProjectionClearsStaleFailureWhenFactsDoNotFail(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_projection_sync"
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "projection_sync"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	run := core.PipelineRun{ID: runID, PipelineID: "projection_sync", Status: core.RunStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	projection := runFactProjection{
		RunID:                   runID,
		LegacyRunStatus:         core.RunStatusFailed,
		LegacyProjectionIsStale: true,
	}

	if err := service.syncLegacyStatusProjection(ctx, run, projection); err != nil {
		t.Fatalf("syncLegacyStatusProjection() error = %v", err)
	}
	got, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if got.Status != core.RunStatusRunning {
		t.Fatalf("run status = %s, want running projection", got.Status)
	}
}

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

func TestCommitFeedbackFactReturnsCommittedContextBeforeTaskStateUpdate(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "fact_context"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_fact_context"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "fact_context",
		Status:     core.RunStatusRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	task := core.Task{
		ID:        "task_01",
		RunID:     runID,
		StageID:   "task_01",
		AgentRole: core.AgentRoleCEO,
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("Create(task) error = %v", err)
	}
	versionID := createInternalDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "ceo", "requirement")
	feedback := normalizeCommitFeedback(core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Op:        task.Op,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{
				Name:               "requirement",
				ArtifactVersionIDs: []string{versionID},
			}},
		},
	})

	fact, err := service.commitFeedbackFact(ctx, task, feedback, doujiagit.RefMoveModeAdvance)
	if err != nil {
		t.Fatalf("commitFeedbackFact() error = %v", err)
	}
	if !fact.Committed {
		t.Fatalf("fact.Committed = false, want true")
	}
	if fact.SnapshotID == "" || fact.SnapshotVersionID == "" || fact.FrontierSnapshotID == "" {
		t.Fatalf("committed fact context missing IDs: %#v", fact)
	}
	if fact.SnapshotVersionID != fact.SnapshotID+":v1" {
		t.Fatalf("SnapshotVersionID = %q, want snapshot v1", fact.SnapshotVersionID)
	}
	if fact.RefName != doujiagit.DefaultRefName {
		t.Fatalf("RefName = %q, want %q", fact.RefName, doujiagit.DefaultRefName)
	}
	if len(fact.OutputBagIDs) != 1 {
		t.Fatalf("OutputBagIDs = %#v, want one committed bag", fact.OutputBagIDs)
	}
	if _, err := doujiaGitRepo.GetSnapshot(ctx, fact.SnapshotID); err != nil {
		t.Fatalf("GetSnapshot(%q) error = %v", fact.SnapshotID, err)
	}
	ref, err := doujiaGitRepo.GetRef(ctx, runID, fact.RefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if ref.FrontierSnapshotID != fact.FrontierSnapshotID {
		t.Fatalf("ref frontier snapshot = %q, want %q", ref.FrontierSnapshotID, fact.FrontierSnapshotID)
	}
	storedBeforeUpdate, err := taskRepo.Get(ctx, runID, task.ID)
	if err != nil {
		t.Fatalf("Get(task before update) error = %v", err)
	}
	if storedBeforeUpdate.Status != core.TaskStatusDispatched {
		t.Fatalf("task status before update = %s, want dispatched", storedBeforeUpdate.Status)
	}

	updated, err := service.updateTaskStateFromFeedback(ctx, task, feedback, core.TaskStatusDone, fact)
	if err != nil {
		t.Fatalf("updateTaskStateFromFeedback() error = %v", err)
	}
	if updated.Status != core.TaskStatusDone {
		t.Fatalf("updated status = %s, want done", updated.Status)
	}
	if !reflect.DeepEqual(updated.OutputBagIDs, fact.OutputBagIDs) {
		t.Fatalf("updated output bag IDs = %#v, want %#v", updated.OutputBagIDs, fact.OutputBagIDs)
	}
}

func TestCommitFeedbackFactRequiresRepositoryAndCommitsNoReceiptSnapshot(t *testing.T) {
	ctx := context.Background()
	task := core.Task{
		ID:        "task_01",
		RunID:     "run_no_fact",
		StageID:   "task_01",
		AgentRole: core.AgentRoleCEO,
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
		Status:    core.TaskStatusDispatched,
	}
	feedback := core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     task.RunID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Op:        task.Op,
		Result:    core.TaskResultCodeOK,
	}

	nilRepoService := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "nil_repo"}),
		repo.NewMemoryRunRepository(),
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	fact, err := nilRepoService.commitFeedbackFact(ctx, task, feedback, doujiagit.RefMoveModeAdvance)
	if !errors.Is(err, ErrDoujiaGitRequired) {
		t.Fatalf("commitFeedbackFact(nil repo) error = %v, want ErrDoujiaGitRequired", err)
	}

	repository := doujiagit.NewMemoryRepository()
	noCommitService := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "no_commit"}),
		repo.NewMemoryRunRepository(),
		repo.NewMemoryTaskRepository(),
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	noCommitService.SetDoujiaGitRepository(repository)
	fact, err = noCommitService.commitFeedbackFact(ctx, task, feedback, doujiagit.RefMoveModeAdvance)
	if err != nil {
		t.Fatalf("commitFeedbackFact(no commit) error = %v", err)
	}
	if !fact.Committed {
		t.Fatalf("no commit fact = %+v, want committed zero-bag snapshot", fact)
	}
	if len(fact.OutputBagIDs) != 0 {
		t.Fatalf("no commit output bags = %#v, want none", fact.OutputBagIDs)
	}
	snapshot, err := repository.GetSnapshot(ctx, fact.SnapshotID)
	if err != nil {
		t.Fatalf("GetSnapshot(%q) error = %v", fact.SnapshotID, err)
	}
	if snapshot.Result != core.TaskResultCodeOK || len(snapshot.OutputBagIDs) != 0 {
		t.Fatalf("snapshot = %+v, want ok zero-output snapshot", snapshot)
	}
}

func TestActiveRefMembersUsesFrontierMemberSnapshotIDs(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "active_ref_members"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	runID := core.RunID("run_active_ref_members")
	now := time.Now().UTC()
	snapshot := doujiagit.TaskSnapshot{
		SnapshotID:        "snapshot:one",
		RunID:             runID,
		TaskID:            "task_01",
		SnapshotVersionID: "snapshot:one:v1",
		SnapshotVersionNo: 1,
		ArrivalKind:       doujiagit.RefMoveModeAdvance,
		AgentRole:         core.AgentRoleCEO,
		AgentID:           "ceo",
		Op:                core.TaskOpWritePlan,
		Result:            core.TaskResultCodeOK,
		CreatedAt:         now,
	}
	if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:one",
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshot.SnapshotID},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if _, _, err := doujiaGitRepo.MoveRef(ctx, doujiagit.MoveRefRequest{
		Ref: doujiagit.Ref{
			RefName:                   doujiagit.DefaultRefName,
			RunID:                     runID,
			FrontierSnapshotID:        "frontier:one",
			FrontierMemberSnapshotIDs: []string{snapshot.SnapshotID},
			UpdatedAt:                 now,
		},
		Event: doujiagit.RefMoveEvent{
			EventID:               "event:one",
			RunID:                 runID,
			RefName:               doujiagit.DefaultRefName,
			ToFrontierSnapshotIDs: []string{"frontier:one"},
			Mode:                  doujiagit.RefMoveModeAdvance,
			CreatedAt:             now,
		},
	}); err != nil {
		t.Fatalf("MoveRef() error = %v", err)
	}

	members, err := service.activeRefMembers(ctx, runID)
	if err != nil {
		t.Fatalf("activeRefMembers() error = %v", err)
	}
	if len(members) != 1 || members[0].Snapshot.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("members = %+v, want snapshot:one", members)
	}
	if members[0].Ref.RefName != doujiagit.DefaultRefName {
		t.Fatalf("member ref = %+v, want main ref", members[0].Ref)
	}
	consumed, err := service.snapshotAlreadyConsumed(ctx, runID, doujiagit.DefaultRefName, snapshot.SnapshotID)
	if err != nil {
		t.Fatalf("snapshotAlreadyConsumed(before) error = %v", err)
	}
	if consumed {
		t.Fatal("snapshotAlreadyConsumed(before) = true, want false")
	}
	if err := doujiaGitRepo.CreateSnapshotProcessingDecision(ctx, doujiagit.SnapshotProcessingDecision{
		RunID:      runID,
		RefName:    doujiagit.DefaultRefName,
		SnapshotID: snapshot.SnapshotID,
		Status:     doujiagit.SnapshotProcessingStatusAdvanced,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateSnapshotProcessingDecision() error = %v", err)
	}
	consumed, err = service.snapshotAlreadyConsumed(ctx, runID, doujiagit.DefaultRefName, snapshot.SnapshotID)
	if err != nil {
		t.Fatalf("snapshotAlreadyConsumed(after) error = %v", err)
	}
	if !consumed {
		t.Fatal("snapshotAlreadyConsumed(after) = false, want true")
	}
}

func TestAdvanceActiveRefNoWorkDoesNotFailRun(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "active_ref_no_work"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_active_ref_no_work")
	now := time.Now().UTC()
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "active_ref_no_work",
		Status:     core.RunStatusRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.advanceActiveRef(ctx, run); err != nil {
		t.Fatalf("advanceActiveRef() error = %v", err)
	}
	got, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if got.Status != core.RunStatusRunning {
		t.Fatalf("run status = %s, want running", got.Status)
	}
}

func TestNewFrontierMembersAppliesSetReplacement(t *testing.T) {
	cases := []struct {
		name     string
		current  []string
		consumed []string
		produced []string
		want     []string
	}{
		{name: "single advance", current: []string{"a"}, consumed: []string{"a"}, produced: []string{"b"}, want: []string{"b"}},
		{name: "fanout", current: []string{"a"}, consumed: []string{"a"}, produced: []string{"b", "c"}, want: []string{"b", "c"}},
		{name: "sibling advance", current: []string{"a", "b"}, consumed: []string{"a"}, produced: []string{"c"}, want: []string{"b", "c"}},
		{name: "merge", current: []string{"a", "b", "c"}, consumed: []string{"a", "b", "c"}, produced: []string{"m"}, want: []string{"m"}},
		{name: "normalizes duplicates", current: []string{"a", "b", "b"}, consumed: []string{"a", " "}, produced: []string{"c", "c"}, want: []string{"b", "c"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := newFrontierMembers(tt.current, tt.consumed, tt.produced)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("newFrontierMembers() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReplaceActiveFrontierValidatesConsumedAndProducedMembers(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "frontier_replace"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_frontier_replace")
	now := time.Now().UTC()
	for _, snapshot := range []doujiagit.TaskSnapshot{
		{SnapshotID: "snapshot:a", RunID: runID, TaskID: "task_a", Result: core.TaskResultCodeOK, CreatedAt: now},
		{SnapshotID: "snapshot:b", RunID: runID, TaskID: "task_b", Result: core.TaskResultCodeOK, CreatedAt: now.Add(time.Millisecond)},
	} {
		if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CreateSnapshot(%s) error = %v", snapshot.SnapshotID, err)
		}
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:a",
		RunID:              runID,
		TaskSnapshotIDs:    []string{"snapshot:a"},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:                   doujiagit.DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        "frontier:a",
		FrontierMemberSnapshotIDs: []string{"snapshot:a"},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	if _, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ConsumedSnapshotIDs: []string{"snapshot:missing"},
		ProducedSnapshotIDs: []string{"snapshot:b"},
		Mode:                doujiagit.RefMoveModeAdvance,
	}); err == nil {
		t.Fatal("replaceActiveFrontier(consumed missing) error = nil, want validation error")
	}
	if _, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ConsumedSnapshotIDs: []string{"snapshot:a"},
		ProducedSnapshotIDs: []string{"snapshot:missing"},
		Mode:                doujiagit.RefMoveModeAdvance,
	}); err == nil {
		t.Fatal("replaceActiveFrontier(produced missing) error = nil, want validation error")
	}
	result, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ConsumedSnapshotIDs: []string{"snapshot:a"},
		ProducedSnapshotIDs: []string{"snapshot:b"},
		Mode:                doujiagit.RefMoveModeAdvance,
		Reason:              "unit test replacement",
	})
	if err != nil {
		t.Fatalf("replaceActiveFrontier() error = %v", err)
	}
	if result.ToFrontierSnapshotID == "" || !reflect.DeepEqual(result.NewMemberSnapshotIDs, []string{"snapshot:b"}) {
		t.Fatalf("replacement result = %+v, want frontier with snapshot:b", result)
	}
	ref, err := doujiaGitRepo.GetRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if ref.FrontierSnapshotID != result.ToFrontierSnapshotID || !reflect.DeepEqual(ref.FrontierMemberSnapshotIDs, []string{"snapshot:b"}) {
		t.Fatalf("ref after replacement = %+v, result = %+v", ref, result)
	}
}

func TestReplaceActiveFrontierRejectsDuplicateLogicalSnapshotVersionsForAdvance(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "frontier_logical_versions"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_frontier_logical_versions")
	now := time.Now().UTC()
	for _, snapshot := range []doujiagit.TaskSnapshot{
		{
			SnapshotID:        "snapshot:code:v1",
			RunID:             runID,
			TaskID:            "task_code_v1",
			LogicalSnapshotID: "logical:write_code",
			SnapshotVersionID: "snapshot:code:v1:version",
			SnapshotVersionNo: 1,
			Result:            core.TaskResultCodeOK,
			CreatedAt:         now,
		},
		{
			SnapshotID:        "snapshot:code:v2",
			RunID:             runID,
			TaskID:            "task_code_v2",
			LogicalSnapshotID: "logical:write_code",
			SnapshotVersionID: "snapshot:code:v2:version",
			SnapshotVersionNo: 2,
			Result:            core.TaskResultCodeOK,
			CreatedAt:         now.Add(time.Millisecond),
		},
	} {
		if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CreateSnapshot(%s) error = %v", snapshot.SnapshotID, err)
		}
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:code:v1",
		RunID:              runID,
		TaskSnapshotIDs:    []string{"snapshot:code:v1"},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:                   doujiagit.DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        "frontier:code:v1",
		FrontierMemberSnapshotIDs: []string{"snapshot:code:v1"},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	if _, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ProducedSnapshotIDs: []string{"snapshot:code:v2"},
		Mode:                doujiagit.RefMoveModeAdvance,
	}); err == nil {
		t.Fatal("replaceActiveFrontier(duplicate logical version) error = nil, want validation error")
	}
	result, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ConsumedSnapshotIDs: []string{"snapshot:code:v1"},
		ProducedSnapshotIDs: []string{"snapshot:code:v2"},
		Mode:                doujiagit.RefMoveModeAdvance,
	})
	if err != nil {
		t.Fatalf("replaceActiveFrontier(replace version) error = %v", err)
	}
	if !reflect.DeepEqual(result.NewMemberSnapshotIDs, []string{"snapshot:code:v2"}) {
		t.Fatalf("replacement result members = %#v, want v2 only", result.NewMemberSnapshotIDs)
	}
}

func TestReplaceActiveFrontierCallReturnPreservesSiblingMembers(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "frontier_call_return"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_frontier_call_return")
	now := time.Now().UTC()
	for _, snapshot := range []doujiagit.TaskSnapshot{
		{SnapshotID: "snapshot:child_done", RunID: runID, TaskID: "child_done", LogicalSnapshotID: "child:done", Result: core.TaskResultCodeOK, CreatedAt: now},
		{SnapshotID: "snapshot:sibling_running", RunID: runID, TaskID: "sibling_running", LogicalSnapshotID: "sibling:running", Result: core.TaskResultCodeOK, CreatedAt: now.Add(time.Millisecond)},
		{SnapshotID: "snapshot:parent_returned", RunID: runID, TaskID: "parent_returned", LogicalSnapshotID: "parent:return:call_owner", ArrivalKind: doujiagit.DecisionKindCallReturn, Result: core.TaskResultCodeOK, CreatedAt: now.Add(2 * time.Millisecond)},
	} {
		if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CreateSnapshot(%s) error = %v", snapshot.SnapshotID, err)
		}
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:children",
		RunID:              runID,
		TaskSnapshotIDs:    []string{"snapshot:child_done", "snapshot:sibling_running"},
		CreatedByMode:      doujiagit.DecisionKindFanout,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:                   doujiagit.DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        "frontier:children",
		FrontierMemberSnapshotIDs: []string{"snapshot:child_done", "snapshot:sibling_running"},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	result, err := service.replaceActiveFrontier(ctx, frontierReplacement{
		RunID:               runID,
		ConsumedSnapshotIDs: []string{"snapshot:child_done"},
		ProducedSnapshotIDs: []string{"snapshot:parent_returned"},
		Mode:                doujiagit.DecisionKindCallReturn,
		Reason:              "child returned to parent slot",
	})
	if err != nil {
		t.Fatalf("replaceActiveFrontier(call-return) error = %v", err)
	}
	if !sameStringSet(result.NewMemberSnapshotIDs, []string{"snapshot:parent_returned", "snapshot:sibling_running"}) {
		t.Fatalf("call-return members = %#v, want parent return plus sibling", result.NewMemberSnapshotIDs)
	}
}

func TestCommitFeedbackFactPreservesExistingSiblingFrontierMembers(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "commit_preserve_sibling"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_commit_preserve_sibling")
	now := time.Now().UTC()
	sibling := doujiagit.TaskSnapshot{SnapshotID: "snapshot:sibling", RunID: runID, TaskID: "task_sibling", Result: core.TaskResultCodeOK, CreatedAt: now}
	if err := doujiaGitRepo.CreateSnapshot(ctx, sibling); err != nil {
		t.Fatalf("CreateSnapshot(sibling) error = %v", err)
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:sibling",
		RunID:              runID,
		TaskSnapshotIDs:    []string{sibling.SnapshotID},
		CreatedByMode:      doujiagit.DecisionKindFanout,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:                   doujiagit.DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        "frontier:sibling",
		FrontierMemberSnapshotIDs: []string{sibling.SnapshotID},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	versionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "worker", "done")
	task := core.Task{
		ID:        "task_done",
		RunID:     runID,
		StageID:   "done",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "worker",
		Op:        "done_op",
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}
	fact, err := service.commitFeedbackFact(ctx, task, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Op:        task.Op,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "done", ArtifactVersionIDs: []string{versionID}}},
		},
	}, doujiagit.RefMoveModeAdvance)
	if err != nil {
		t.Fatalf("commitFeedbackFact() error = %v", err)
	}
	ref, err := doujiaGitRepo.GetRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if !sameStringSet(ref.FrontierMemberSnapshotIDs, []string{sibling.SnapshotID, fact.SnapshotID}) {
		t.Fatalf("frontier members = %#v, want sibling plus committed snapshot %s", ref.FrontierMemberSnapshotIDs, fact.SnapshotID)
	}
}

func TestCommitFeedbackFactReplacesInputProducerWhenTaskEvolvesSnapshot(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "commit_replace_source"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		&internalRecordingDispatcher{},
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	runID := core.RunID("run_commit_replace_source")
	now := time.Now().UTC()
	versionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "worker", "source")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{BagID: "bag_source", RunID: runID, ArtifactVersionIDs: []string{versionID}, CreatedAt: now}); err != nil {
		t.Fatalf("CreateBag(source) error = %v", err)
	}
	source := doujiagit.TaskSnapshot{SnapshotID: "snapshot:source", RunID: runID, TaskID: "task_source", Result: core.TaskResultCodeOK, OutputBagIDs: []string{"bag_source"}, CreatedAt: now}
	sibling := doujiagit.TaskSnapshot{SnapshotID: "snapshot:sibling", RunID: runID, TaskID: "task_sibling", Result: core.TaskResultCodeOK, CreatedAt: now.Add(time.Millisecond)}
	for _, snapshot := range []doujiagit.TaskSnapshot{source, sibling} {
		if err := doujiaGitRepo.CreateSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CreateSnapshot(%s) error = %v", snapshot.SnapshotID, err)
		}
	}
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: "frontier:source",
		RunID:              runID,
		TaskSnapshotIDs:    []string{source.SnapshotID, sibling.SnapshotID},
		CreatedByMode:      doujiagit.DecisionKindFanout,
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:                   doujiagit.DefaultRefName,
		RunID:                     runID,
		FrontierSnapshotID:        "frontier:source",
		FrontierMemberSnapshotIDs: []string{source.SnapshotID, sibling.SnapshotID},
		UpdatedAt:                 now,
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	nextVersionID := createTestArtifactVersionInternal(t, ctx, doujiaGitRepo, runID, "worker", "next")
	task := core.Task{
		ID:          "task_next",
		RunID:       runID,
		StageID:     "next",
		AgentRole:   core.AgentRoleArchitect,
		AgentID:     "worker",
		Op:          "next_op",
		Status:      core.TaskStatusDispatched,
		InputBagIDs: []string{"bag_source"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	fact, err := service.commitFeedbackFact(ctx, task, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		Op:          task.Op,
		InputBagIDs: task.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "next", ArtifactVersionIDs: []string{nextVersionID}}},
		},
	}, doujiagit.RefMoveModeAdvance)
	if err != nil {
		t.Fatalf("commitFeedbackFact() error = %v", err)
	}
	ref, err := doujiaGitRepo.GetRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if !sameStringSet(ref.FrontierMemberSnapshotIDs, []string{sibling.SnapshotID, fact.SnapshotID}) {
		t.Fatalf("frontier members = %#v, want source replaced with committed snapshot and sibling preserved", ref.FrontierMemberSnapshotIDs)
	}
}

func createTestArtifactVersionInternal(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID, namespace string, key string) string {
	t.Helper()
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte(string(runID) + ":" + namespace + ":" + key)),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/" + namespace + "/" + key,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  namespace,
		LogicalKey: key,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	return version.ArtifactVersionID
}

func createTestBagInternal(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID, namespace string, key string) string {
	t.Helper()
	versionID := createTestArtifactVersionInternal(t, ctx, repository, runID, namespace, key)
	bagID := doujiagit.StableBagID(runID, "synthetic:"+key, key)
	if err := repository.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{versionID},
		CreatedAt:          time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateBag(%s) error = %v", key, err)
	}
	return bagID
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

func createInternalDoujiaGitVersion(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID, namespace string, logicalKey string) string {
	t.Helper()
	payload := []byte(namespace + "/" + logicalKey)
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID(payload),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "memory://" + namespace + "/" + logicalKey,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  namespace,
		LogicalKey: logicalKey,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	return version.ArtifactVersionID
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

func hasDispatchedTask(items []core.TaskMetaData, taskID core.TaskID, op string) bool {
	for _, item := range items {
		if item.TaskID == taskID && item.Op == op {
			return true
		}
	}
	return false
}

func hasDispatchedOp(items []core.TaskMetaData, op string) bool {
	for _, item := range items {
		if item.Op == op {
			return true
		}
	}
	return false
}

func hasPipelineInstanceForDefinition(items []core.PipelineInstance, pipelineID core.PipelineID) bool {
	for _, item := range items {
		if item.PipelineID == pipelineID {
			return true
		}
	}
	return false
}

type fullDeliveryJSONHarness struct {
	ctx          context.Context
	runID        core.RunID
	run          core.PipelineRun
	service      *Service
	runRepo      repo.RunRepository
	taskRepo     repo.TaskRepository
	instanceRepo repo.PipelineInstanceRepository
	dispatcher   *internalRecordingDispatcher
	doujiaGit    doujiagit.Repository
}

func newFullDeliveryJSONHarness(t *testing.T, runID core.RunID) fullDeliveryJSONHarness {
	t.Helper()

	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("testdata", "full_delivery", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec(full delivery) error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry(full delivery) error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(full delivery) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &internalRecordingDispatcher{}
	service := NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	now := time.Now().UTC()
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	return fullDeliveryJSONHarness{
		ctx:          ctx,
		runID:        runID,
		run:          run,
		service:      service,
		runRepo:      runRepo,
		taskRepo:     taskRepo,
		instanceRepo: instanceRepo,
		dispatcher:   dispatcher,
		doujiaGit:    doujiaGitRepo,
	}
}

func (h fullDeliveryJSONHarness) advanceToSplit(t *testing.T) {
	t.Helper()

	requirementVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "ceo", "requirement")
	productPlanVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "pm01", "product_plan")
	architectureVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "architecture")
	containerContextVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "container_context")
	frontModuleVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "front_module_input")
	backendAPIVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "backend_api_module_input")
	backendStoreVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "backend_store_module_input")
	globalTestInputVersionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, "architect01", "global_test_input")

	feedbacks := []core.TaskMetaData{
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     h.runID,
			TaskID:    "ceo_write_requirement",
			AgentID:   "ceo",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "requirement",
				ArtifactVersionIDs: []string{requirementVersionID},
			}}},
		},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     h.runID,
			TaskID:    "pm_write_product_plan",
			AgentID:   "pm01",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "product_plan",
				ArtifactVersionIDs: []string{productPlanVersionID},
			}}},
		},
		{Direction: core.TaskDirectionFeedback, RunID: h.runID, TaskID: "ceo_review_product_plan", AgentID: "ceo", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     h.runID,
			TaskID:    "architect_write_architecture",
			AgentID:   "architect01",
			Op:        core.TaskOpWritePlan,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "architecture",
				ArtifactVersionIDs: []string{architectureVersionID},
			}}},
		},
		{Direction: core.TaskDirectionFeedback, RunID: h.runID, TaskID: "pm_review_architecture", AgentID: "pm01", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     h.runID,
			TaskID:    "architect_create_container",
			AgentID:   "architect01",
			Op:        core.TaskOpCreateContainer,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{
				Name:               "container_context",
				ArtifactVersionIDs: []string{containerContextVersionID},
			}}},
		},
		{
			Direction: core.TaskDirectionFeedback,
			RunID:     h.runID,
			TaskID:    "architect_split_modules",
			AgentID:   "architect01",
			Op:        core.TaskOpSplitModule,
			Result:    core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{
				{
					Name:               "front_module_input",
					Indexes:            map[string]string{"module_key": "front"},
					ArtifactVersionIDs: []string{frontModuleVersionID},
				},
				{
					Name:               "backend_module_input",
					Indexes:            map[string]string{"module_key": "backend_api"},
					ArtifactVersionIDs: []string{backendAPIVersionID},
				},
				{
					Name:               "backend_module_input",
					Indexes:            map[string]string{"module_key": "backend_store"},
					ArtifactVersionIDs: []string{backendStoreVersionID},
				},
				{
					Name:               "global_test_input",
					ArtifactVersionIDs: []string{globalTestInputVersionID},
				},
			}},
		},
	}
	for _, feedback := range feedbacks {
		if err := h.service.OnFeedback(h.ctx, feedback); err != nil {
			t.Fatalf("OnFeedback(%s) error = %v", feedback.TaskID, err)
		}
	}
}

func (h fullDeliveryJSONHarness) completeBackendGroup(t *testing.T) {
	t.Helper()

	backendGroup := h.childInstanceByParentAndPipeline(t, "root", "pipeline_backend_module_group")
	moduleChildren := h.childInstancesByParentAndPipeline(t, backendGroup.ID, "pipeline_module")
	sort.Slice(moduleChildren, func(i, j int) bool {
		return moduleChildren[i].InstanceKey < moduleChildren[j].InstanceKey
	})
	if len(moduleChildren) != 2 {
		t.Fatalf("backend module children = %+v, want backend_api and backend_store", moduleChildren)
	}
	h.completeInstance(t, moduleChildren[0], map[string][]string{
		"tested_module[module_key=backend_api]": {"bag_backend_api_tested"},
		"code_bag[module_key=backend_api]":      {"bag_backend_api_code"},
		"test_data_bag[module_key=backend_api]": {"bag_backend_api_test_data"},
	})
	h.completeInstance(t, moduleChildren[1], map[string][]string{
		"tested_module[module_key=backend_store]": {"bag_backend_store_tested"},
		"code_bag[module_key=backend_store]":      {"bag_backend_store_code"},
		"test_data_bag[module_key=backend_store]": {"bag_backend_store_test_data"},
	})
}

func (h fullDeliveryJSONHarness) advanceToMergeDispatch(t *testing.T) {
	t.Helper()

	h.advanceToSplit(t)
	front := h.childInstanceByParentAndPipeline(t, "root", "pipeline_front_module")
	globalTestData := h.childInstanceByParentAndPipeline(t, "root", "pipeline_global_test_data")

	h.completeInstance(t, front, map[string][]string{
		"tested_module[module_key=front]":    {"bag_front_tested"},
		"code_bag[module_key=front]":         {"bag_front_code"},
		"preview_edit_bag[module_key=front]": {"bag_front_preview"},
	})
	h.completeInstance(t, globalTestData, map[string][]string{
		"global_test_data": {"bag_global_test_data"},
	})
	h.completeBackendGroup(t)
}

func (h fullDeliveryJSONHarness) childInstanceByParentAndPipeline(t *testing.T, parentID core.PipelineInstanceID, pipelineID core.PipelineID) core.PipelineInstance {
	t.Helper()

	children := h.childInstancesByParentAndPipeline(t, parentID, pipelineID)
	if len(children) != 1 {
		t.Fatalf("children(parent=%s pipeline=%s) = %+v, want exactly one", parentID, pipelineID, children)
	}
	return children[0]
}

func (h fullDeliveryJSONHarness) childInstancesByParentAndPipeline(t *testing.T, parentID core.PipelineInstanceID, pipelineID core.PipelineID) []core.PipelineInstance {
	t.Helper()

	children, err := h.instanceRepo.ListChildren(h.ctx, h.runID, parentID)
	if err != nil {
		t.Fatalf("ListChildren(%s) error = %v", parentID, err)
	}
	out := make([]core.PipelineInstance, 0)
	for _, child := range children {
		if child.PipelineID == pipelineID {
			out = append(out, child)
		}
	}
	return out
}

func (h fullDeliveryJSONHarness) instance(t *testing.T, instanceID core.PipelineInstanceID) core.PipelineInstance {
	t.Helper()

	instance, err := h.instanceRepo.Get(h.ctx, h.runID, instanceID)
	if err != nil {
		t.Fatalf("Get(instance %s) error = %v", instanceID, err)
	}
	return instance
}

func (h fullDeliveryJSONHarness) completeInstance(t *testing.T, instance core.PipelineInstance, outputLists map[string][]string) {
	t.Helper()

	h.ensureSyntheticBagsExist(t, instance, outputLists)
	instance.Status = core.PipelineInstanceStatusCompleted
	instance.OutputBagIDLists = cloneBagLists(outputLists)
	instance.OutputBagIDs = latestBagIDs(outputLists)
	instance.UpdatedAt = time.Now().UTC()
	if err := h.instanceRepo.Update(h.ctx, instance); err != nil {
		t.Fatalf("Update(instance %s) error = %v", instance.ID, err)
	}
	if err := h.service.onPipelineInstanceCompleted(h.ctx, h.run, instance); err != nil {
		t.Fatalf("onPipelineInstanceCompleted(%s) error = %v", instance.ID, err)
	}
}

func (h fullDeliveryJSONHarness) ensureSyntheticBagsExist(t *testing.T, instance core.PipelineInstance, outputLists map[string][]string) {
	t.Helper()

	for key, bagIDs := range outputLists {
		for i, bagID := range bagIDs {
			if bagID == "" {
				continue
			}
			if _, err := h.doujiaGit.GetBag(h.ctx, bagID); err == nil {
				continue
			}
			versionID := createTestArtifactVersionInternal(t, h.ctx, h.doujiaGit, h.runID, string(instance.ID), sanitizeIDPart(key)+"_"+sanitizeIDPart(bagID)+"_"+sanitizeIDPart(string(rune('a'+i))))
			if err := h.doujiaGit.CreateBag(h.ctx, doujiagit.ArtifactBag{
				BagID:              bagID,
				RunID:              h.runID,
				ArtifactVersionIDs: []string{versionID},
				CreatedAt:          time.Now().UTC(),
			}); err != nil {
				t.Fatalf("CreateBag(%s) error = %v", bagID, err)
			}
		}
	}
}

func (h fullDeliveryJSONHarness) taskByOp(t *testing.T, op string) core.Task {
	t.Helper()

	tasks, err := h.taskRepo.ListByRun(h.ctx, h.runID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	for _, task := range tasks {
		if task.Op == op {
			return task
		}
	}
	t.Fatalf("task with op %q not found in %+v", op, tasks)
	return core.Task{}
}

func (h fullDeliveryJSONHarness) taskByPipelineAndOp(t *testing.T, instanceID core.PipelineInstanceID, op string) core.Task {
	t.Helper()

	tasks, err := h.taskRepo.ListByRun(h.ctx, h.runID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	var found *core.Task
	for _, task := range tasks {
		if task.PipelineInstanceID != instanceID || task.Op != op {
			continue
		}
		candidate := task
		if found == nil ||
			candidate.CreatedAt.After(found.CreatedAt) ||
			(candidate.CreatedAt.Equal(found.CreatedAt) && candidate.UpdatedAt.After(found.UpdatedAt)) {
			found = &candidate
		}
	}
	if found != nil {
		return *found
	}
	t.Fatalf("task with instance %q and op %q not found in %+v", instanceID, op, tasks)
	return core.Task{}
}

func (h fullDeliveryJSONHarness) runState(t *testing.T) core.PipelineRun {
	t.Helper()

	run, err := h.runRepo.Get(h.ctx, h.runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	return run
}

func cloneBagLists(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func latestBagIDs(lists map[string][]string) map[string]string {
	if len(lists) == 0 {
		return nil
	}
	out := make(map[string]string, len(lists))
	for key, values := range lists {
		if len(values) == 0 {
			continue
		}
		out[key] = values[len(values)-1]
	}
	return out
}

func countTaskInputBagNames(items []core.BagBindingRef) map[string]int {
	out := make(map[string]int)
	for _, item := range items {
		out[item.Name]++
	}
	return out
}

func findHandlerBindingByName(items []core.HandlerBindingRef, name string) (core.HandlerBindingRef, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return core.HandlerBindingRef{}, false
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

func TestLegacyReadyTasksAreIgnoredWithoutActiveRefFacts(t *testing.T) {
	ctx := context.Background()
	const runID core.RunID = "run_legacy_ready_ignored"
	now := time.Now().UTC()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &lowConcurrencyRecordingDispatcher{}
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := NewService(
		pipeline.NewMemoryRegistry(pipeline.PipelineSpec{ID: "pipeline_legacy_ready_ignored"}),
		runRepo,
		taskRepo,
		noopProvisioner{},
		dispatcher,
		nil,
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	run := core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_legacy_ready_ignored",
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

	if err := service.advanceByFacts(ctx, run); err != nil {
		t.Fatalf("advanceByFacts() error = %v", err)
	}
	if got := len(dispatcher.dispatched); got != 0 {
		t.Fatalf("dispatched tasks = %d, want none without active-ref facts", got)
	}
	childA, err := taskRepo.Get(ctx, runID, "child_a")
	if err != nil {
		t.Fatalf("Get(child_a) error = %v", err)
	}
	childB, err := taskRepo.Get(ctx, runID, "child_b")
	if err != nil {
		t.Fatalf("Get(child_b) error = %v", err)
	}
	if childA.Status != core.TaskStatusPending || childB.Status != core.TaskStatusPending {
		t.Fatalf("child statuses = %s/%s, want both pending because facts do not justify dispatch", childA.Status, childB.Status)
	}
}

type lowConcurrencyRecordingDispatcher struct {
	dispatched []core.TaskMetaData
}

func (d *lowConcurrencyRecordingDispatcher) Dispatch(_ context.Context, task core.TaskMetaData) error {
	d.dispatched = append(d.dispatched, task)
	return nil
}
