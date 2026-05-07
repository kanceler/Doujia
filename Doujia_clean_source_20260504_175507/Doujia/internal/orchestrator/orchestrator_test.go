package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"devflow/internal/app"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/orchestrator"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

func TestLegacyPrefixSplitModuleDispatchUsesDeclaredHistoricalInputBags(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec(full delivery) error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_legacy_split_declared_inputs"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	requirementVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "ceo", "requirement")
	productPlanVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "pm01", "pm_plan")
	architectureVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "architecture_plan")
	containerContextVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "container_context")

	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "ceo_write_requirement", AgentID: "ceo", Op: core.TaskOpWritePlan, Result: core.TaskResultCodeOK, Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "requirement", ArtifactVersionIDs: []string{requirementVersionID}}}}},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "pm_write_plan", AgentID: "pm01", Op: core.TaskOpWritePlan, Result: core.TaskResultCodeOK, Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "product_plan", ArtifactVersionIDs: []string{productPlanVersionID}}}}},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "ceo_review_product_plan", AgentID: "ceo", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "architect_write_plan", AgentID: "architect01", Op: core.TaskOpWritePlan, Result: core.TaskResultCodeOK, Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "architecture", ArtifactVersionIDs: []string{architectureVersionID}}}}},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "pm_review_architecture", AgentID: "pm01", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "architect_create_container", AgentID: "architect01", Op: core.TaskOpCreateContainer, Result: core.TaskResultCodeOK, Commit: &core.CommitReceipt{Result: core.TaskResultCodeOK, ProducedBags: []core.CommittedBagDef{{Name: "container_context", ArtifactVersionIDs: []string{containerContextVersionID}}}}},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}

	splitTask, err := taskRepo.Get(ctx, runID, "split_module")
	if err != nil {
		t.Fatalf("Get(split_module) error = %v", err)
	}
	if splitTask.Status != core.TaskStatusDispatched {
		t.Fatalf("split_module status = %s, want dispatched", splitTask.Status)
	}
	inputsByName := make(map[string]string, len(splitTask.InputBags))
	for _, bag := range splitTask.InputBags {
		inputsByName[bag.Name] = bag.BagID
	}
	if inputsByName["architecture"] == "" {
		t.Fatalf("split_module named input bags = %#v, missing architecture", splitTask.InputBags)
	}
	if inputsByName["container_context"] == "" {
		t.Fatalf("split_module named input bags = %#v, missing container_context", splitTask.InputBags)
	}
	if len(splitTask.InputBagIDs) != 2 {
		t.Fatalf("split_module input bag ids = %#v, want architecture and container_context", splitTask.InputBagIDs)
	}
}

func TestPhaseOneFlowWithExternalCEOFeedback(t *testing.T) {
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runRoot := filepath.Join(projectsRoot, "run_002")
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}

	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := context.Background()

	const runID core.RunID = "run_002"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow", noopRunConfig()); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	ceoWorkspace := filepath.Join(run.ProjectDir, "agents", "ceo")
	if _, err := os.Stat(ceoWorkspace); err != nil {
		t.Fatalf("ceo workspace should exist: %v", err)
	}
	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	if session.Agent == nil {
		t.Fatalf("ceo agent should be initialized")
	}

	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "ceo",
		Op:        "ceo_write_requirement",
	})
	if err != nil {
		t.Fatalf("task_01 execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}

	run, err = bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	pmWorkspace := filepath.Join(run.ProjectDir, "agents", "pm01")
	if _, err := os.Stat(pmWorkspace); err != nil {
		t.Fatalf("pm workspace should exist: %v", err)
	}

	pmPlan := filepath.Join(pmWorkspace, "artifacts", "plan", "plan_v1.md")
	waitForFile(t, pmPlan)
	if _, err := os.Stat(pmPlan); err != nil {
		t.Fatalf("pm plan artifact should exist: %v", err)
	}

	architectDesign := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "architecture", "architecture_v1.md")
	waitForFile(t, architectDesign)
	if _, err := os.Stat(architectDesign); err != nil {
		t.Fatalf("architect design artifact should exist: %v", err)
	}

	run = waitForRunStatus(t, ctx, bootstrap, runID, core.RunStatusAwaitingAcceptance)
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusAwaitingAcceptance)
	}
	if run.LatestAcceptanceCheckpointTaskID == "" {
		t.Fatalf("run latest acceptance checkpoint task id = empty, want generated acceptance task")
	}
}

func TestPhaseOneFlowWithExternalCEOFeedbackProtocolMockExtended(t *testing.T) {
	requireProtocolMockExtendedTest(t)
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runRoot := filepath.Join(projectsRoot, "run_protocolmock_002")
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}

	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := newExtendedTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	const runID core.RunID = "run_protocolmock_002"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow", extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "ceo",
		Op:        "ceo_write_requirement",
	})
	if err != nil {
		t.Fatalf("task_01 execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusCompleted {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusCompleted)
	}
	pmPlan := filepath.Join(projectsRoot, string(runID), "agents", "pm01", "artifacts", "plan", "plan_v1.md")
	if _, err := os.Stat(pmPlan); err != nil {
		t.Fatalf("pm plan artifact should exist after protocolmock success: %v", err)
	}
	architectDesign := filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "architecture", "architecture_v1.md")
	if _, err := os.Stat(architectDesign); err != nil {
		t.Fatalf("architect design artifact should exist after protocolmock success: %v", err)
	}
}

func TestPhaseTwoFlowWithExternalCEOFeedbackProtocolMockExtended(t *testing.T) {
	requireProtocolMockExtendedTest(t)
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runID := uniqueRunID("run_phase_two_protocolmock")
	runRoot := filepath.Join(projectsRoot, string(runID))
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}
	t.Logf("run artifacts: %s", runRoot)

	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := newExtendedTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, pipeline.PipelineIDPhaseTwo, extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusCompleted {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusCompleted)
	}
	wantFiles := []string{
		filepath.Join(projectsRoot, string(runID), "agents", "pm01", "artifacts", "plan", "plan_v1.md"),
		filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "architecture", "architecture_v1.md"),
	}
	for _, wantFile := range wantFiles {
		if _, err := os.Stat(wantFile); err != nil {
			t.Fatalf("expected artifact should exist after protocolmock success: %s (%v)", wantFile, err)
		}
	}
}

func TestPhaseTwoFlowWithExternalCEOFeedbackProtocolMockExtendedSQLite(t *testing.T) {
	if os.Getenv("DEVFLOW_SQLITE_PROTOCOLMOCK_EXTENDED_TEST") != "1" {
		t.Skip("set DEVFLOW_SQLITE_PROTOCOLMOCK_EXTENDED_TEST=1 to run the SQLite-backed extended full-chain test")
	}
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runID := uniqueRunID("run_phase_two_protocolmock_sqlite")
	runRoot := filepath.Join(projectsRoot, string(runID))
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}
	t.Logf("run artifacts: %s", runRoot)

	ctx := newExtendedTestContext(t)
	dbPath := filepath.Join(runRoot, "state.db")
	bootstrap, db, err := app.NewSQLiteBootstrap(ctx, projectsRoot, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBootstrap() error = %v", err)
	}
	defer db.Close()
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, pipeline.PipelineIDPhaseTwo, extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusCompleted {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusCompleted)
	}
	artifacts, err := bootstrap.Internals.ArtifactRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(artifacts) error = %v", err)
	}
	if len(artifacts) == 0 {
		t.Fatalf("SQLite artifact metadata should not be empty")
	}
	events, err := bootstrap.Internals.EventRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "run_completed") {
		t.Fatalf("SQLite event metadata should include run_completed")
	}
}

func TestPhaseTwoProtocolMockRecoverExhaustsAttemptsFast(t *testing.T) {
	requireProtocolMockExtendedTest(t)
	projectsRoot := t.TempDir()
	runID := uniqueRunID("run_phase_two_protocolmock_recover_exhaust")
	runRoot := filepath.Join(projectsRoot, string(runID))
	registryPath := writeProtocolMockForceBugRegistry(t)

	bootstrap, err := app.NewBootstrapWithOptions(projectsRoot, app.BootstrapOptions{
		PipelineRegistryPath: registryPath,
		AgentMode:            "protocolmock",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	ctx := newExtendedTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "pipeline_full_delivery", extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusFailed {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want failed after protocolmock recover exhaustion", run.Status)
	}

	tasks, err := bootstrap.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	debugAttempts := 0
	foundAttempt03 := false
	for _, task := range tasks {
		if task.StageID != "debug_code" {
			continue
		}
		debugAttempts++
		if strings.HasSuffix(string(task.ID), "_debug_code_attempt_03") {
			foundAttempt03 = true
		}
	}
	if debugAttempts < 3 {
		t.Fatalf("debug attempt count = %d, want at least 3", debugAttempts)
	}
	if !foundAttempt03 {
		t.Fatalf("tasks = %#v, want one debug_code attempt_03 task", tasks)
	}
	events, err := bootstrap.Internals.EventRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "task_attempts_exhausted") {
		t.Fatalf("events = %#v, want task_attempts_exhausted", events)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "events.log")); err != nil {
		t.Fatalf("events.log should exist for failed protocolmock run: %v", err)
	}
}

func TestPhaseTwoProtocolMockRecoverExhaustsAttemptsFastSQLite(t *testing.T) {
	if os.Getenv("DEVFLOW_SQLITE_PROTOCOLMOCK_EXTENDED_TEST") != "1" {
		t.Skip("set DEVFLOW_SQLITE_PROTOCOLMOCK_EXTENDED_TEST=1 to run the SQLite-backed extended full-chain test")
	}
	projectsRoot := t.TempDir()
	runID := uniqueRunID("run_phase_two_protocolmock_recover_exhaust_sqlite")
	runRoot := filepath.Join(projectsRoot, string(runID))
	registryPath := writeProtocolMockForceBugRegistry(t)

	ctx := newExtendedTestContext(t)
	dbPath := filepath.Join(runRoot, "state.db")
	bootstrap, db, err := app.NewSQLiteBootstrapWithOptions(ctx, projectsRoot, dbPath, app.BootstrapOptions{
		PipelineRegistryPath: registryPath,
		AgentMode:            "protocolmock",
	})
	if err != nil {
		t.Fatalf("NewSQLiteBootstrapWithOptions() error = %v", err)
	}
	defer db.Close()
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "pipeline_full_delivery", extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusFailed {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want failed after protocolmock recover exhaustion", run.Status)
	}
	events, err := bootstrap.Internals.EventRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "task_attempts_exhausted") {
		t.Fatalf("events = %#v, want task_attempts_exhausted", events)
	}
}

func TestPhaseTwoProtocolMockRecoverSucceedsAfterSingleBug(t *testing.T) {
	requireProtocolMockExtendedTest(t)
	projectsRoot := t.TempDir()
	runID := uniqueRunID("run_phase_two_protocolmock_recover_success")
	registryPath := writeProtocolMockForceBugOnceRegistry(t)

	bootstrap, err := app.NewBootstrapWithOptions(projectsRoot, app.BootstrapOptions{
		PipelineRegistryPath: registryPath,
		AgentMode:            "protocolmock",
	})
	if err != nil {
		t.Fatalf("NewBootstrapWithOptions() error = %v", err)
	}

	ctx := newExtendedTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "pipeline_full_delivery", extendedRunConfig(t)); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatusAny(t, ctx, bootstrap, runID, []core.RunStatus{
		core.RunStatusCompleted,
		core.RunStatusFailed,
	})
	if run.Status != core.RunStatusCompleted {
		logRunEventsOnTimeout(t, bootstrap, ctx, runID)
		t.Fatalf("run status = %s, want completed after single recover", run.Status)
	}

	tasks, err := bootstrap.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	foundDebug := false
	foundRetest := false
	for _, task := range tasks {
		if task.StageID == "debug_code" {
			foundDebug = true
		}
		if strings.Contains(string(task.StageID), "test_code") && task.Result == core.TaskResultCodeOK {
			foundRetest = true
		}
	}
	if !foundDebug {
		t.Fatalf("tasks = %#v, want at least one debug_code task", tasks)
	}
	if !foundRetest {
		t.Fatalf("tasks = %#v, want a successful test_code retry", tasks)
	}
	events, err := bootstrap.Internals.EventRepository.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "doujiagit_recover") || !hasEventType(events, "run_completed") {
		t.Fatalf("events = %#v, want doujiagit_recover and run_completed", events)
	}
}

func TestFullDeliveryJSONRegistryDrivesLegacyMainTaskPrefix(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_full_delivery_json_prefix"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first, err := taskRepo.Get(ctx, runID, "ceo_write_requirement")
	if err != nil {
		t.Fatalf("Get(ceo_write_requirement) error = %v", err)
	}
	if first.Status != core.TaskStatusWaitingExternal {
		t.Fatalf("first status = %s, want waiting_external", first.Status)
	}

	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "ceo_write_requirement", AgentID: "ceo", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_full_delivery_json_prefix/agents/ceo/artifacts/requirement/requirement_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "pm_write_plan", AgentID: "pm01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_full_delivery_json_prefix/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "ceo_review_product_plan", AgentID: "ceo", Op: core.TaskOpReviewPlan, ArtifactURIs: []string{"projects/run_full_delivery_json_prefix/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "architect_write_plan", AgentID: "architect01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_full_delivery_json_prefix/agents/architect01/artifacts/architecture/global_architecture_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "pm_review_architecture", AgentID: "pm01", Op: core.TaskOpReviewPlan, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}

	containerTask, err := taskRepo.Get(ctx, runID, "architect_create_container")
	if err != nil {
		t.Fatalf("Get(architect_create_container) error = %v", err)
	}
	if containerTask.Status != core.TaskStatusDispatched {
		t.Fatalf("architect_create_container status = %s, want dispatched", containerTask.Status)
	}
	if countDispatchOp(dispatcher.dispatched, core.TaskOpCreateContainer) != 1 {
		t.Fatalf("dispatches = %+v, want one create_container dispatch", dispatcher.dispatched)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "architect_create_container",
		AgentID:      "architect01",
		Op:           core.TaskOpCreateContainer,
		ArtifactURIs: []string{"projects/run_full_delivery_json_prefix/agents/architect01/artifacts/container/container_context.json"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("create_container feedback error = %v", err)
	}

	splitTask, err := taskRepo.Get(ctx, runID, "split_module")
	if err != nil {
		t.Fatalf("Get(split_module) error = %v", err)
	}
	if splitTask.Status != core.TaskStatusDispatched {
		t.Fatalf("split_module status = %s, want dispatched", splitTask.Status)
	}
	if countDispatchOp(dispatcher.dispatched, core.TaskOpSplitModule) != 1 {
		t.Fatalf("dispatches = %+v, want one split_module dispatch", dispatcher.dispatched)
	}
}

func TestStartPipelineControlsCreatePipelineInstances(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_start_pipeline_controls"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	splitTask := core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.Create(ctx, splitTask); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}

	feedback := core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params:       map[string]string{"module_key": "module01"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder01",
					"tester": "tester01",
				},
				InputBags: map[string]string{"module_input": "bag_module01"},
			},
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module02",
				Params:       map[string]string{"module_key": "module02"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder02",
					"tester": "tester02",
				},
				InputBags: map[string]string{"module_input": "bag_module02"},
			},
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "write_global_test_data",
				PipelineID:   "pipeline_global_test_data",
				InstanceKey:  "global",
				InputBags:    map[string]string{"global_test_input": "bag_global"},
			},
		},
	}
	if err := service.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("OnFeedback() error = %v", err)
	}

	instances, err := instanceRepo.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(instances) error = %v", err)
	}
	if len(instances) != 8 {
		t.Fatalf("instances = %#v, want root + 3 controlled children + 4 nested single-call children", instances)
	}
	root, err := instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root) error = %v", err)
	}
	if root.AgentBindings["architect"] != "architect01" || root.AgentBindings["pm"] != "pm01" {
		t.Fatalf("root agent bindings = %#v, want inherited defaults", root.AgentBindings)
	}
	module01ID := core.PipelineInstanceID("root_test_all_modules_module01")
	module01, err := instanceRepo.Get(ctx, runID, module01ID)
	if err != nil {
		t.Fatalf("Get(module01) error = %v", err)
	}
	if module01.PipelineID != "pipeline_module" || module01.ParentTransitionID != "test_all_modules" || module01.InstanceKey != "module01" {
		t.Fatalf("module01 instance = %#v", module01)
	}
	if module01.Params["module_key"] != "module01" || module01.InputBagIDs["module_input"] != "bag_module01" {
		t.Fatalf("module01 params/bags = %#v / %#v", module01.Params, module01.InputBagIDs)
	}
	if module01.AgentBindings["architect"] != "architect01" || module01.AgentBindings["coder"] != "coder01" || module01.AgentBindings["tester"] != "tester01" {
		t.Fatalf("module01 agent bindings = %#v", module01.AgentBindings)
	}
	globalID := core.PipelineInstanceID("root_write_global_test_data_global")
	global, err := instanceRepo.Get(ctx, runID, globalID)
	if err != nil {
		t.Fatalf("Get(global) error = %v", err)
	}
	if global.PipelineID != "pipeline_global_test_data" || global.InputBagIDs["global_test_input"] != "bag_global" {
		t.Fatalf("global instance = %#v", global)
	}
	if global.AgentBindings["architect"] != "architect01" {
		t.Fatalf("global inherited architect = %#v", global.AgentBindings)
	}
	if global.Status != core.PipelineInstanceStatusRunning {
		t.Fatalf("global status = %s, want running", global.Status)
	}

	codeInstanceID := core.PipelineInstanceID("root_test_all_modules_module01_write_code_single")
	codeInstance, err := instanceRepo.Get(ctx, runID, codeInstanceID)
	if err != nil {
		t.Fatalf("Get(module01 write_code child) error = %v", err)
	}
	if codeInstance.PipelineID != "pipeline_write_code" || codeInstance.AgentBindings["coder"] != "coder01" || codeInstance.InputBagIDs["module_input"] != "bag_module01" {
		t.Fatalf("module01 write_code instance = %#v", codeInstance)
	}

	wantDispatches := map[string]bool{
		"coder01|write_code|bag_module01":  false,
		"tester01|test_data|bag_module01":  false,
		"coder02|write_code|bag_module02":  false,
		"tester02|test_data|bag_module02":  false,
		"architect01|test_data|bag_global": false,
	}
	for _, dispatch := range dispatcher.dispatched {
		if len(dispatch.InputBagIDs) != 1 {
			continue
		}
		key := fmt.Sprintf("%s|%s|%s", dispatch.AgentID, dispatch.Op, dispatch.InputBagIDs[0])
		if _, ok := wantDispatches[key]; ok {
			wantDispatches[key] = true
		}
	}
	for key, seen := range wantDispatches {
		if !seen {
			t.Fatalf("dispatch %s not found in %#v", key, dispatcher.dispatched)
		}
	}

	codeTaskID := core.TaskID("root_test_all_modules_module01_write_code_single_write_code")
	codeTask, err := taskRepo.Get(ctx, runID, codeTaskID)
	if err != nil {
		t.Fatalf("Get(code task) error = %v", err)
	}
	if codeTask.PipelineInstanceID != codeInstanceID || codeTask.AgentID != "coder01" {
		t.Fatalf("code task = %#v", codeTask)
	}
}

func TestStartPipelineControlUsesJSONBindingsForInstanceInitialization(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)

	const runID core.RunID = "run_start_pipeline_json_bindings"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params: map[string]string{
					"module_key": "module01",
					"ignored":    "should_not_enter_instance",
				},
				AgentBindings: map[string]core.AgentID{
					"coder":     "coder01",
					"tester":    "tester01",
					"architect": "wrong_architect_should_not_override_parent",
				},
				InputBags: map[string]string{
					"module_input": "bag_module01",
					"extra":        "bag_should_not_enter_instance",
				},
			},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	module, err := instanceRepo.Get(ctx, runID, "root_test_all_modules_module01")
	if err != nil {
		t.Fatalf("Get(module instance) error = %v", err)
	}
	if module.Params["module_key"] != "module01" || module.Params["ignored"] != "" {
		t.Fatalf("module params = %#v, want only bound module_key", module.Params)
	}
	if module.AgentBindings["architect"] != "architect01" || module.AgentBindings["coder"] != "coder01" || module.AgentBindings["tester"] != "tester01" {
		t.Fatalf("module agent bindings = %#v, want inherited architect and bound coder/tester", module.AgentBindings)
	}
	if module.InputBagIDs["module_input"] != "bag_module01" || module.InputBagIDs["extra"] != "" {
		t.Fatalf("module input bags = %#v, want only bound module_input", module.InputBagIDs)
	}
}

func TestStartPipelineControlInputBagsCanReferenceProducedBagNames(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		&recordingDispatcher{},
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_start_pipeline_output_key_input"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}
	moduleVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "module01_input")
	globalVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_input")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{
				{Name: "module01_input", ArtifactVersionIDs: []string{moduleVersionID}},
				{Name: "global_test_input", ArtifactVersionIDs: []string{globalVersionID}},
			},
			Control: []core.Control{
				{
					Type:         core.ControlTypeStartPipeline,
					TransitionID: "test_all_modules",
					PipelineID:   "pipeline_module",
					InstanceKey:  "module01",
					Params:       map[string]string{"module_key": "module01"},
					AgentBindings: map[string]core.AgentID{
						"coder":  "coder01",
						"tester": "tester01",
					},
					InputBags: map[string]string{"module_input": "module01_input"},
				},
				{
					Type:         core.ControlTypeStartPipeline,
					TransitionID: "write_global_test_data",
					PipelineID:   "pipeline_global_test_data",
					InstanceKey:  "global",
					InputBags:    map[string]string{"global_test_input": "global_test_input"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	splitTask, err := taskRepo.Get(ctx, runID, "split_module")
	if err != nil {
		t.Fatalf("Get(split task) error = %v", err)
	}
	if got, want := len(splitTask.OutputBagIDs), 2; got != want {
		t.Fatalf("split output bags = %v, want %d", splitTask.OutputBagIDs, want)
	}
	module, err := instanceRepo.Get(ctx, runID, "root_test_all_modules_module01")
	if err != nil {
		t.Fatalf("Get(module instance) error = %v", err)
	}
	if module.InputBagIDs["module_input"] != splitTask.OutputBagIDs[0] {
		t.Fatalf("module input bags = %#v, want module01 output key mapped to %s", module.InputBagIDs, splitTask.OutputBagIDs[0])
	}
	global, err := instanceRepo.Get(ctx, runID, "root_write_global_test_data_global")
	if err != nil {
		t.Fatalf("Get(global instance) error = %v", err)
	}
	if global.InputBagIDs["global_test_input"] != splitTask.OutputBagIDs[1] {
		t.Fatalf("global input bags = %#v, want global output key mapped to %s", global.InputBagIDs, splitTask.OutputBagIDs[1])
	}
}

func TestStartPipelineControlMissingSignatureBindingDoesNotCreatePartialInstances(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		&recordingDispatcher{},
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)

	const runID core.RunID = "run_start_pipeline_missing_binding"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}

	err = service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params:       map[string]string{"module_key": "module01"},
				AgentBindings: map[string]core.AgentID{
					"coder": "coder01",
				},
				InputBags: map[string]string{"module_input": "bag_module01"},
			},
		},
	})
	if err == nil {
		t.Fatalf("OnFeedback() error = nil, want missing tester binding error")
	}
	if !strings.Contains(err.Error(), "agent_bindings.tester") {
		t.Fatalf("OnFeedback() error = %v, want agent_bindings.tester", err)
	}
	instances, listErr := instanceRepo.ListByRun(ctx, runID)
	if listErr != nil {
		t.Fatalf("ListByRun(instances) error = %v", listErr)
	}
	if len(instances) != 1 || instances[0].ID != "root" {
		t.Fatalf("instances = %#v, want only root after failed control expansion", instances)
	}
}

func TestPipelineInstanceTaskFeedbackDoesNotAdvanceRootPipeline(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_instance_task_feedback"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params:       map[string]string{"module_key": "module01"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder01",
					"tester": "tester01",
				},
				InputBags: map[string]string{"module_input": "bag_module01"},
			},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	codeTaskID := core.TaskID("root_test_all_modules_module01_write_code_single_write_code")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       codeTaskID,
		AgentID:      "coder01",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{"projects/run_instance_task_feedback/agents/coder01/artifacts/code/module01_code_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("code task feedback error = %v", err)
	}

	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusRunning {
		t.Fatalf("run status = %s, want still running", run.Status)
	}
	instance, err := instanceRepo.Get(ctx, runID, "root_test_all_modules_module01_write_code_single")
	if err != nil {
		t.Fatalf("Get(code instance) error = %v", err)
	}
	if instance.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("code instance status = %s, want completed", instance.Status)
	}
	if countDispatchOp(dispatcher.dispatched, core.TaskOpMergeCode) != 0 {
		t.Fatalf("merge should not dispatch before module aggregation, dispatches = %#v", dispatcher.dispatched)
	}
}

func TestPipelineInstanceTaskFeedbackEntersTransitionToStateBeforeCompletion(t *testing.T) {
	ctx := context.Background()
	registrySpec := pipeline.RegistrySpec{
		SchemaVersion:   pipeline.RegistrySchemaVersionV04,
		RegistryID:      "two_step_registry",
		EntryPipelineID: "pipeline_two_step",
		PipelineDefs: []pipeline.PipelineDefSpec{
			{
				SchemaVersion: pipeline.PipelineSchemaVersionV04,
				PipelineID:    "pipeline_two_step",
				Kind:          "entry",
				Namespace: pipeline.NamespaceSpec{Agents: []pipeline.SignatureAgentSpec{
					{Name: "worker", Role: core.AgentRoleArchitect, DefaultAgentID: "worker01"},
				}},
				StartState:    "start",
				DeliveryState: "done",
				States: []pipeline.StateSpec{
					{ID: "start", Kind: "start", Proof: pipeline.ProofSpec{Type: "external"}, Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"first"}}},
					{ID: "first_done", Kind: "state", Proof: pipeline.ProofSpec{Type: "transition_result", Transition: "first"}, Next: &pipeline.NextSpec{Type: "all", Transitions: []string{"second"}}},
					{ID: "done", Kind: "delivery", Proof: pipeline.ProofSpec{Type: "transition_result", Transition: "second"}},
				},
				Transitions: []pipeline.TransitionSpec{
					{ID: "first", Kind: "task", FromState: "start", ToState: "first_done", Agent: &pipeline.AgentSpec{Role: core.AgentRoleArchitect, Alias: "worker"}, Op: "first_op", OutputBags: []pipeline.BagSpec{{Name: "first_output"}}},
					{ID: "second", Kind: "task", FromState: "first_done", ToState: "done", Agent: &pipeline.AgentSpec{Role: core.AgentRoleArchitect, Alias: "worker"}, Op: "second_op", InputBags: []pipeline.BagSpec{{Name: "first_output"}}, OutputBags: []pipeline.BagSpec{{Name: "final_output"}}},
				},
			},
		},
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_two_step_pipeline_instance"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_two_step",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	firstTaskID := core.TaskID("root_first")
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:            "root",
		RunID:         runID,
		PipelineID:    "pipeline_two_step",
		InstanceKey:   "root",
		Status:        core.PipelineInstanceStatusRunning,
		AgentBindings: map[string]core.AgentID{"worker": "worker01"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:                 firstTaskID,
		RunID:              runID,
		PipelineInstanceID: "root",
		StageID:            "first",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "worker01",
		Op:                 "first_op",
		Status:             core.TaskStatusDispatched,
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(first task) error = %v", err)
	}
	firstVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "worker01", "first_output")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    firstTaskID,
		AgentID:   "worker01",
		Op:        "first_op",
		Result:    core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "first_output", ArtifactVersionIDs: []string{firstVersionID}}},
		},
	}); err != nil {
		t.Fatalf("first feedback error = %v", err)
	}

	root, err := instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after first) error = %v", err)
	}
	if root.Status == core.PipelineInstanceStatusCompleted {
		t.Fatalf("root completed after first task; want still running")
	}
	firstTask, err := taskRepo.Get(ctx, runID, firstTaskID)
	if err != nil {
		t.Fatalf("Get(done first task) error = %v", err)
	}
	secondTaskID := core.TaskID("root_second")
	secondTask, err := taskRepo.Get(ctx, runID, secondTaskID)
	if err != nil {
		t.Fatalf("Get(second task) error = %v", err)
	}
	if secondTask.Status != core.TaskStatusDispatched || secondTask.PipelineInstanceID != "root" {
		t.Fatalf("second task = %#v, want dispatched in root", secondTask)
	}
	if got := uniqueTestStrings(secondTask.InputBagIDs); !sameTestStringSet(got, firstTask.OutputBagIDs) {
		t.Fatalf("second input bags = %v, want first output bags %v", got, firstTask.OutputBagIDs)
	}

	secondVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "worker01", "final_output")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      secondTaskID,
		AgentID:     "worker01",
		Op:          "second_op",
		InputBagIDs: secondTask.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "final_output", ArtifactVersionIDs: []string{secondVersionID}}},
		},
	}); err != nil {
		t.Fatalf("second feedback error = %v", err)
	}
	root, err = instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after second) error = %v", err)
	}
	if root.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("root status = %s, want completed", root.Status)
	}
	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want awaiting_acceptance", run.Status)
	}
}

func TestPipelineModuleStartsTestCodeAfterCodeAndTestDataComplete(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_module_aggregate_test_code"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params:       map[string]string{"module_key": "module01"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder01",
					"tester": "tester01",
				},
				InputBags: map[string]string{"module_input": "bag_module01"},
			},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}
	moduleInputVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "module_input")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module01",
		RunID:              runID,
		ArtifactVersionIDs: []string{moduleInputVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module input) error = %v", err)
	}
	codeVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "code")
	testDataVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "test_data")

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_all_modules_module01_write_code_single_write_code",
		AgentID:     "coder01",
		Op:          core.TaskOpWriteCode,
		InputBagIDs: []string{"bag_module01"},
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{
				{Name: "code_bag", ArtifactVersionIDs: []string{codeVersionID}},
			},
		},
	}); err != nil {
		t.Fatalf("write_code feedback error = %v", err)
	}
	codeTask, err := taskRepo.Get(ctx, runID, "root_test_all_modules_module01_write_code_single_write_code")
	if err != nil {
		t.Fatalf("Get(code task) error = %v", err)
	}
	codeBagID := codeTask.OutputBagIDs[0]
	if countDispatchOp(dispatcher.dispatched, core.TaskOpTestCode) != 0 {
		t.Fatalf("test_code should not dispatch before test_data completes: %#v", dispatcher.dispatched)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_all_modules_module01_write_test_data_single_write_test_data",
		AgentID:     "tester01",
		Op:          core.TaskOpTestData,
		InputBagIDs: []string{"bag_module01"},
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{
				{Name: "test_data_bag", ArtifactVersionIDs: []string{testDataVersionID}},
			},
		},
	}); err != nil {
		t.Fatalf("write_test_data feedback error = %v", err)
	}
	testDataTask, err := taskRepo.Get(ctx, runID, "root_test_all_modules_module01_write_test_data_single_write_test_data")
	if err != nil {
		t.Fatalf("Get(test data task) error = %v", err)
	}
	testDataBagID := testDataTask.OutputBagIDs[0]

	testCodeTaskID := core.TaskID("root_test_all_modules_module01_test_code_single_test_code")
	testCodeTask, err := taskRepo.Get(ctx, runID, testCodeTaskID)
	if err != nil {
		t.Fatalf("Get(test_code task) error = %v", err)
	}
	if testCodeTask.AgentID != "tester01" || testCodeTask.Status != core.TaskStatusDispatched {
		t.Fatalf("test_code task = %#v", testCodeTask)
	}
	if got := uniqueTestStrings(testCodeTask.InputBagIDs); !sameTestStringSet(got, []string{"bag_module01", codeBagID, testDataBagID}) {
		t.Fatalf("test_code input bags = %v, want [bag_module01 %s %s]", got, codeBagID, testDataBagID)
	}
	module, err := instanceRepo.Get(ctx, runID, "root_test_all_modules_module01")
	if err != nil {
		t.Fatalf("Get(module instance) error = %v", err)
	}
	if got := module.InputBagIDLists["module_test_input"]; !sameTestStringSet(got, []string{codeBagID, testDataBagID}) {
		t.Fatalf("module_test_input list = %v, want [%s %s]", got, codeBagID, testDataBagID)
	}
}

func TestPipelineTestCodeBugDispatchesDebugAndRetriesTestCode(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec, "pipeline_test_code")
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_test_code_bug_debug_retry_json"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_test_code",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:            "root",
		RunID:         runID,
		PipelineID:    "pipeline_test_code",
		InstanceKey:   "root",
		Status:        core.PipelineInstanceStatusRunning,
		Params:        map[string]string{"module_key": "module01"},
		AgentBindings: map[string]core.AgentID{"coder": "coder01", "tester": "tester01"},
		InputBagIDs: map[string]string{
			"module_input":  "bag_module_input",
			"code_bag":      "bag_module_code",
			"test_data_bag": "bag_module_test_data",
		},
		InputBagIDLists: map[string][]string{
			"module_input":  {"bag_module_input"},
			"code_bag":      {"bag_module_code"},
			"test_data_bag": {"bag_module_test_data"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}
	moduleInputVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "module_input")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_input",
		RunID:              runID,
		ArtifactVersionIDs: []string{moduleInputVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module input) error = %v", err)
	}
	codeVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "module_code")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_code",
		RunID:              runID,
		ArtifactVersionIDs: []string{codeVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module code) error = %v", err)
	}
	testDataVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "module_test_data")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_test_data",
		RunID:              runID,
		ArtifactVersionIDs: []string{testDataVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module test data) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:                 "root_test_code",
		RunID:              runID,
		PipelineInstanceID: "root",
		StageID:            "test_code",
		AgentRole:          core.AgentRoleTester,
		AgentID:            "tester01",
		Op:                 core.TaskOpTestCode,
		Status:             core.TaskStatusDispatched,
		InputBagIDs:        []string{"bag_module_input", "bag_module_code", "bag_module_test_data"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(test_code task) error = %v", err)
	}

	failureVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: []string{"bag_module_input", "bag_module_code", "bag_module_test_data"},
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersionID}}},
		},
	}); err != nil {
		t.Fatalf("test_code bug feedback error = %v", err)
	}

	root, err := instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after bug) error = %v", err)
	}
	failureBagID := root.OutputBagIDs["failure_report"]
	if failureBagID == "" {
		t.Fatalf("root output bags = %#v, want failure_report mapped by name", root.OutputBagIDs)
	}
	if root.OutputBagIDs["tested_module"] != "" {
		t.Fatalf("root output bags = %#v, failure_report should not be mapped as tested_module", root.OutputBagIDs)
	}
	debugTask, err := taskRepo.Get(ctx, runID, "root_debug_code")
	if err != nil {
		t.Fatalf("Get(debug task) error = %v", err)
	}
	if debugTask.Status != core.TaskStatusDispatched || debugTask.AgentID != "coder01" {
		t.Fatalf("debug task = %#v, want coder01 dispatched", debugTask)
	}
	if got := uniqueTestStrings(debugTask.InputBagIDs); !sameTestStringSet(got, []string{"bag_module_input", "bag_module_code", "bag_module_test_data", failureBagID}) {
		t.Fatalf("debug input bags = %v, want module input + code/test data + failure_report %s", got, failureBagID)
	}
	moves, err := doujiaGitRepo.ListRefMoveEvents(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("ListRefMoveEvents(after bug) error = %v", err)
	}
	var recoverMove doujiagit.RefMoveEvent
	for _, move := range moves {
		if move.Mode == doujiagit.RefMoveModeRecover {
			recoverMove = move
			break
		}
	}
	if recoverMove.EventID == "" {
		t.Fatalf("ref moves after bug = %+v, want one recover move", moves)
	}
	if len(recoverMove.FromFrontierSnapshotIDs) != 1 || len(recoverMove.ToFrontierSnapshotIDs) != 1 {
		t.Fatalf("recover move frontiers = %+v", recoverMove)
	}
	recoverFrontier, err := doujiaGitRepo.GetFrontierSnapshot(ctx, recoverMove.ToFrontierSnapshotIDs[0])
	if err != nil {
		t.Fatalf("GetFrontierSnapshot(recover) error = %v", err)
	}
	if recoverFrontier.CreatedByMode != doujiagit.RefMoveModeRecover {
		t.Fatalf("recover frontier mode = %s, want recover", recoverFrontier.CreatedByMode)
	}
	if !strings.Contains(recoverMove.DetailsJSON, `"debug_task_id":"root_debug_code"`) ||
		!strings.Contains(recoverMove.DetailsJSON, failureBagID) ||
		!strings.Contains(recoverMove.DetailsJSON, "bag_module_input") ||
		!strings.Contains(recoverMove.DetailsJSON, "bag_module_test_data") ||
		!strings.Contains(recoverMove.DetailsJSON, "bag_module_code") {
		t.Fatalf("recover details = %s, want debug task, failure report, kept module/code/test data", recoverMove.DetailsJSON)
	}

	debugVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "code_debugged")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_debug_code",
		AgentID:     "coder01",
		Op:          "debug_write_code",
		InputBagIDs: debugTask.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{debugVersionID}}},
		},
	}); err != nil {
		t.Fatalf("debug_code feedback error = %v", err)
	}

	root, err = instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after debug) error = %v", err)
	}
	debuggedCodeBagID := root.OutputBagIDs["code_bag"]
	if debuggedCodeBagID == "" {
		t.Fatalf("root output bags = %#v, want code_bag from debug", root.OutputBagIDs)
	}
	retryTask, err := taskRepo.Get(ctx, runID, "root_test_code")
	if err != nil {
		t.Fatalf("Get(retry test_code task) error = %v", err)
	}
	if retryTask.Status != core.TaskStatusDispatched {
		t.Fatalf("retry task = %#v, want redispatched", retryTask)
	}
	if got := uniqueTestStrings(retryTask.InputBagIDs); !sameTestStringSet(got, []string{"bag_module_input", "bag_module_test_data", debuggedCodeBagID}) {
		t.Fatalf("retry input bags = %v, want module input + original test data + debugged code %s", got, debuggedCodeBagID)
	}

	secondFailureVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report_second")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: retryTask.InputBagIDs,
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{secondFailureVersionID}}},
		},
	}); err != nil {
		t.Fatalf("second test_code bug feedback error = %v", err)
	}
	secondDebugTask, err := taskRepo.Get(ctx, runID, "root_debug_code_attempt_02")
	if err != nil {
		t.Fatalf("Get(second debug task) error = %v", err)
	}
	if secondDebugTask.Status != core.TaskStatusDispatched || secondDebugTask.StageID != "debug_code" {
		t.Fatalf("second debug task = %#v, want attempt 02 dispatched for debug_code transition", secondDebugTask)
	}
}

func TestPipelineDebugCodeExhaustsAttemptsAndFailsRun(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec, "pipeline_test_code")
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	eventRepo := repo.NewMemoryEventRepository()
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		&recordingDispatcher{},
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)
	service.SetEventRepository(eventRepo)

	const runID core.RunID = "run_test_code_debug_attempts_exhausted"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_test_code",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := instanceRepo.Create(ctx, core.PipelineInstance{
		ID:            "root",
		RunID:         runID,
		PipelineID:    "pipeline_test_code",
		InstanceKey:   "root",
		Status:        core.PipelineInstanceStatusRunning,
		Params:        map[string]string{"module_key": "module01"},
		AgentBindings: map[string]core.AgentID{"coder": "coder01", "tester": "tester01"},
		InputBagIDs: map[string]string{
			"module_input":  "bag_module_input",
			"code_bag":      "bag_module_code_v1",
			"test_data_bag": "bag_module_test_data",
		},
		InputBagIDLists: map[string][]string{
			"module_input":  {"bag_module_input"},
			"code_bag":      {"bag_module_code_v1"},
			"test_data_bag": {"bag_module_test_data"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}

	moduleInputVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "module_input_exhaust")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_input",
		RunID:              runID,
		ArtifactVersionIDs: []string{moduleInputVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module input) error = %v", err)
	}
	codeVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "module_code_exhaust_v1")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_code_v1",
		RunID:              runID,
		ArtifactVersionIDs: []string{codeVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module code) error = %v", err)
	}
	testDataVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "module_test_data_exhaust")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_module_test_data",
		RunID:              runID,
		ArtifactVersionIDs: []string{testDataVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(module test data) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:                 "root_test_code",
		RunID:              runID,
		PipelineInstanceID: "root",
		StageID:            "test_code",
		AgentRole:          core.AgentRoleTester,
		AgentID:            "tester01",
		Op:                 core.TaskOpTestCode,
		Status:             core.TaskStatusDispatched,
		InputBagIDs:        []string{"bag_module_input", "bag_module_code_v1", "bag_module_test_data"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("Create(test_code task) error = %v", err)
	}

	failureVersion1 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report_exhaust_01")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: []string{"bag_module_input", "bag_module_code_v1", "bag_module_test_data"},
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion1}}},
		},
	}); err != nil {
		t.Fatalf("first test_code bug feedback error = %v", err)
	}
	debugTask1, err := taskRepo.Get(ctx, runID, "root_debug_code")
	if err != nil {
		t.Fatalf("Get(first debug task) error = %v", err)
	}
	debugVersion1 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "module_code_exhaust_v2")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_debug_code",
		AgentID:     "coder01",
		Op:          "debug_write_code",
		InputBagIDs: debugTask1.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{debugVersion1}}},
		},
	}); err != nil {
		t.Fatalf("first debug_code feedback error = %v", err)
	}
	retryTask1, err := taskRepo.Get(ctx, runID, "root_test_code")
	if err != nil {
		t.Fatalf("Get(retry test_code task after first debug) error = %v", err)
	}

	failureVersion2 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report_exhaust_02")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: retryTask1.InputBagIDs,
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion2}}},
		},
	}); err != nil {
		t.Fatalf("second test_code bug feedback error = %v", err)
	}
	debugTask2, err := taskRepo.Get(ctx, runID, "root_debug_code_attempt_02")
	if err != nil {
		t.Fatalf("Get(second debug task) error = %v", err)
	}
	debugVersion2 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "module_code_exhaust_v3")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_debug_code_attempt_02",
		AgentID:     "coder01",
		Op:          "debug_write_code",
		InputBagIDs: debugTask2.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{debugVersion2}}},
		},
	}); err != nil {
		t.Fatalf("second debug_code feedback error = %v", err)
	}
	retryTask2, err := taskRepo.Get(ctx, runID, "root_test_code")
	if err != nil {
		t.Fatalf("Get(retry test_code task after second debug) error = %v", err)
	}

	failureVersion3 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report_exhaust_03")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: retryTask2.InputBagIDs,
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion3}}},
		},
	}); err != nil {
		t.Fatalf("third test_code bug feedback error = %v", err)
	}
	debugTask3, err := taskRepo.Get(ctx, runID, "root_debug_code_attempt_03")
	if err != nil {
		t.Fatalf("Get(third debug task) error = %v", err)
	}
	if debugTask3.Status != core.TaskStatusDispatched {
		t.Fatalf("third debug task = %#v, want dispatched", debugTask3)
	}

	debugVersion3 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "coder01", "module_code_exhaust_v4")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_debug_code_attempt_03",
		AgentID:     "coder01",
		Op:          "debug_write_code",
		InputBagIDs: debugTask3.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{debugVersion3}}},
		},
	}); err != nil {
		t.Fatalf("third debug_code feedback error = %v", err)
	}
	retryTask3, err := taskRepo.Get(ctx, runID, "root_test_code")
	if err != nil {
		t.Fatalf("Get(retry test_code task after third debug) error = %v", err)
	}

	failureVersion4 := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "tester01", "failure_report_exhaust_04")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_test_code",
		AgentID:     "tester01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: retryTask3.InputBagIDs,
		Result:      core.TaskResultCodeBug,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeBug,
			ProducedBags: []core.CommittedBagDef{{Name: "failure_report", ArtifactVersionIDs: []string{failureVersion4}}},
		},
	}); err != nil {
		t.Fatalf("fourth test_code bug feedback error = %v", err)
	}

	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusFailed {
		t.Fatalf("run status = %s, want failed after debug attempts exhausted", run.Status)
	}
	if _, err := taskRepo.Get(ctx, runID, "root_debug_code_attempt_04"); err == nil {
		t.Fatalf("unexpected fourth debug task created after exhausting max attempts")
	}
	events, err := eventRepo.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "task_attempts_exhausted") {
		t.Fatalf("events = %#v, want task_attempts_exhausted", events)
	}
}

func TestRootStartsGlobalTestAfterMergeAndGlobalDataComplete(t *testing.T) {
	ctx := context.Background()
	registrySpec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	definitions, err := pipeline.NewJSONRegistry(registrySpec)
	if err != nil {
		t.Fatalf("NewJSONRegistry() error = %v", err)
	}
	legacyRegistry, err := pipeline.NewLegacyRegistryFromSpec(registrySpec)
	if err != nil {
		t.Fatalf("NewLegacyRegistryFromSpec() error = %v", err)
	}
	runRepo := repo.NewMemoryRunRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		legacyRegistry,
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineDefinitionRegistry(definitions)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_root_global_test_after_merge"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "pipeline_full_delivery",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "split_module",
		RunID:     runID,
		StageID:   "split_module",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Status:    core.TaskStatusDispatched,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create(split task) error = %v", err)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "split_module",
		AgentID:   "architect01",
		Op:        core.TaskOpSplitModule,
		Result:    core.TaskResultCodeOK,
		Control: []core.Control{
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module01",
				Params:       map[string]string{"module_key": "module01"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder01",
					"tester": "tester01",
				},
				InputBags: map[string]string{"module_input": "bag_module01"},
			},
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "test_all_modules",
				PipelineID:   "pipeline_module",
				InstanceKey:  "module02",
				Params:       map[string]string{"module_key": "module02"},
				AgentBindings: map[string]core.AgentID{
					"coder":  "coder02",
					"tester": "tester02",
				},
				InputBags: map[string]string{"module_input": "bag_module02"},
			},
			{
				Type:         core.ControlTypeStartPipeline,
				TransitionID: "write_global_test_data",
				PipelineID:   "pipeline_global_test_data",
				InstanceKey:  "global",
				InputBags:    map[string]string{"global_test_input": "bag_global_test_input"},
			},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}
	containerContextVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "container_context")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_container_context",
		RunID:              runID,
		ArtifactVersionIDs: []string{containerContextVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(container context) error = %v", err)
	}
	root, err := instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after split_module) error = %v", err)
	}
	if root.OutputBagIDs == nil {
		root.OutputBagIDs = make(map[string]string)
	}
	if root.OutputBagIDLists == nil {
		root.OutputBagIDLists = make(map[string][]string)
	}
	root.OutputBagIDs["container_context"] = "bag_container_context"
	root.OutputBagIDLists["container_context"] = []string{"bag_container_context"}
	globalInputVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_input")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "bag_global_test_input",
		RunID:              runID,
		ArtifactVersionIDs: []string{globalInputVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(global test input) error = %v", err)
	}
	root.OutputBagIDs["global_test_input"] = "bag_global_test_input"
	root.OutputBagIDLists["global_test_input"] = []string{"bag_global_test_input"}
	if err := instanceRepo.Update(ctx, root); err != nil {
		t.Fatalf("Update(root context bags) error = %v", err)
	}
	for _, moduleKey := range []string{"module01", "module02"} {
		versionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", moduleKey+"_input")
		if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
			BagID:              "bag_" + moduleKey,
			RunID:              runID,
			ArtifactVersionIDs: []string{versionID},
			CreatedAt:          now,
		}); err != nil {
			t.Fatalf("CreateBag(%s input) error = %v", moduleKey, err)
		}
	}

	completeModuleHappyPath := func(moduleKey string, coderID string, testerID string) string {
		t.Helper()
		codeVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, coderID, moduleKey+"_code")
		testDataVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, testerID, moduleKey+"_test_data")
		testedVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, testerID, moduleKey+"_tested")
		prefix := core.TaskID("root_test_all_modules_" + moduleKey)
		if err := service.OnFeedback(ctx, core.TaskMetaData{
			Direction:   core.TaskDirectionFeedback,
			RunID:       runID,
			TaskID:      core.TaskID(string(prefix) + "_write_code_single_write_code"),
			AgentID:     core.AgentID(coderID),
			Op:          core.TaskOpWriteCode,
			InputBagIDs: []string{"bag_" + moduleKey},
			Result:      core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{
				Result:       core.TaskResultCodeOK,
				ProducedBags: []core.CommittedBagDef{{Name: "code_bag", ArtifactVersionIDs: []string{codeVersionID}}},
			},
		}); err != nil {
			t.Fatalf("%s write_code feedback error = %v", moduleKey, err)
		}
		if err := service.OnFeedback(ctx, core.TaskMetaData{
			Direction:   core.TaskDirectionFeedback,
			RunID:       runID,
			TaskID:      core.TaskID(string(prefix) + "_write_test_data_single_write_test_data"),
			AgentID:     core.AgentID(testerID),
			Op:          core.TaskOpTestData,
			InputBagIDs: []string{"bag_" + moduleKey},
			Result:      core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{
				Result:       core.TaskResultCodeOK,
				ProducedBags: []core.CommittedBagDef{{Name: "test_data_bag", ArtifactVersionIDs: []string{testDataVersionID}}},
			},
		}); err != nil {
			t.Fatalf("%s write_test_data feedback error = %v", moduleKey, err)
		}
		testCodeTaskID := core.TaskID(string(prefix) + "_test_code_single_test_code")
		testCodeTask, err := taskRepo.Get(ctx, runID, testCodeTaskID)
		if err != nil {
			t.Fatalf("Get(%s test_code task) error = %v", moduleKey, err)
		}
		if err := service.OnFeedback(ctx, core.TaskMetaData{
			Direction:   core.TaskDirectionFeedback,
			RunID:       runID,
			TaskID:      testCodeTaskID,
			AgentID:     core.AgentID(testerID),
			Op:          core.TaskOpTestCode,
			InputBagIDs: testCodeTask.InputBagIDs,
			Result:      core.TaskResultCodeOK,
			Commit: &core.CommitReceipt{
				Result:       core.TaskResultCodeOK,
				ProducedBags: []core.CommittedBagDef{{Name: "tested_module", ArtifactVersionIDs: []string{testedVersionID}}},
			},
		}); err != nil {
			t.Fatalf("%s test_code feedback error = %v", moduleKey, err)
		}
		doneTask, err := taskRepo.Get(ctx, runID, testCodeTaskID)
		if err != nil {
			t.Fatalf("Get(%s done test_code task) error = %v", moduleKey, err)
		}
		return doneTask.OutputBagIDs[0]
	}

	module01BagID := completeModuleHappyPath("module01", "coder01", "tester01")
	if countDispatchOp(dispatcher.dispatched, core.TaskOpMergeCode) != 0 {
		t.Fatalf("merge should not dispatch before all modules complete: %#v", dispatcher.dispatched)
	}
	module02BagID := completeModuleHappyPath("module02", "coder02", "tester02")

	mergeTaskID := core.TaskID("root_merge_code_single_merge_code")
	mergeTask, err := taskRepo.Get(ctx, runID, mergeTaskID)
	if err != nil {
		t.Fatalf("Get(merge task) error = %v", err)
	}
	if mergeTask.AgentID != "architect01" || mergeTask.Status != core.TaskStatusDispatched {
		t.Fatalf("merge task = %#v", mergeTask)
	}
	root, err = instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root) error = %v", err)
	}
	wantMergeInputs := append([]string{module01BagID, module02BagID, "bag_container_context", "bag_global_test_input"}, root.OutputBagIDLists["code_bag"]...)
	if got := uniqueTestStrings(mergeTask.InputBagIDs); !sameTestStringSet(got, wantMergeInputs) {
		t.Fatalf("merge input bags = %v, want %v", got, wantMergeInputs)
	}
	if got := root.OutputBagIDLists["tested_module"]; !sameTestStringSet(got, []string{module01BagID, module02BagID}) {
		t.Fatalf("root tested_module list = %v, want [%s %s]", got, module01BagID, module02BagID)
	}
	if _, err := taskRepo.Get(ctx, runID, "root_global_test_code_single_global_test_code"); err == nil {
		t.Fatalf("global_test_code should wait for merge_code and global_test_data")
	}

	globalDataVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_data")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      "root_write_global_test_data_global_write_global_test_data",
		AgentID:     "architect01",
		Op:          core.TaskOpTestData,
		InputBagIDs: []string{"bag_global_test_input"},
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "global_test_data", ArtifactVersionIDs: []string{globalDataVersionID}}},
		},
	}); err != nil {
		t.Fatalf("global test data feedback error = %v", err)
	}
	globalDataTask, err := taskRepo.Get(ctx, runID, "root_write_global_test_data_global_write_global_test_data")
	if err != nil {
		t.Fatalf("Get(global data task) error = %v", err)
	}
	globalDataBagID := globalDataTask.OutputBagIDs[0]
	if _, err := taskRepo.Get(ctx, runID, "root_global_test_code_single_global_test_code"); err == nil {
		t.Fatalf("global_test_code should still wait for merge_code")
	}

	staleVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, "run_old_global_test", "architect01", "stale_global_test_input")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              "aggregate:root:global_test_ready:global_test_code_input",
		RunID:              "run_old_global_test",
		ArtifactVersionIDs: []string{staleVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(stale aggregate global test input) error = %v", err)
	}

	mergedVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "merged_code")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      mergeTaskID,
		AgentID:     "architect01",
		Op:          core.TaskOpMergeCode,
		InputBagIDs: mergeTask.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "merged_code", ArtifactVersionIDs: []string{mergedVersionID}}},
		},
	}); err != nil {
		t.Fatalf("merge_code feedback error = %v", err)
	}
	mergeTask, err = taskRepo.Get(ctx, runID, mergeTaskID)
	if err != nil {
		t.Fatalf("Get(done merge task) error = %v", err)
	}
	mergedBagID := mergeTask.OutputBagIDs[0]

	globalTestTaskID := core.TaskID("root_global_test_code_single_global_test_code")
	globalTestTask, err := taskRepo.Get(ctx, runID, globalTestTaskID)
	if err != nil {
		t.Fatalf("Get(global test task) error = %v", err)
	}
	if globalTestTask.AgentID != "architect01" || globalTestTask.Status != core.TaskStatusDispatched {
		t.Fatalf("global test task = %#v", globalTestTask)
	}
	if got := uniqueTestStrings(globalTestTask.InputBagIDs); len(got) != 1 {
		t.Fatalf("global test input bags = %v, want one synthesized global_test_code_input bag", got)
	}
	globalTestInputBagID := globalTestTask.InputBagIDs[0]
	if globalTestInputBagID == mergedBagID || globalTestInputBagID == globalDataBagID {
		t.Fatalf("global test input bag = %s, want synthesized bag not a source bag", globalTestInputBagID)
	}
	globalTestInputBag, err := doujiaGitRepo.GetBag(ctx, globalTestInputBagID)
	if err != nil {
		t.Fatalf("GetBag(global test input) error = %v", err)
	}
	mergedBag, err := doujiaGitRepo.GetBag(ctx, mergedBagID)
	if err != nil {
		t.Fatalf("GetBag(merged) error = %v", err)
	}
	globalDataBag, err := doujiaGitRepo.GetBag(ctx, globalDataBagID)
	if err != nil {
		t.Fatalf("GetBag(global data) error = %v", err)
	}
	containerBag, err := doujiaGitRepo.GetBag(ctx, "bag_container_context")
	if err != nil {
		t.Fatalf("GetBag(container context) error = %v", err)
	}
	wantGlobalTestVersions := uniqueTestStrings(append(append(append([]string{}, containerBag.ArtifactVersionIDs...), mergedBag.ArtifactVersionIDs...), globalDataBag.ArtifactVersionIDs...))
	if got := uniqueTestStrings(globalTestInputBag.ArtifactVersionIDs); !sameTestStringSet(got, wantGlobalTestVersions) {
		t.Fatalf("global test input bag versions = %v, want %v", got, wantGlobalTestVersions)
	}
	root, err = instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after global ready) error = %v", err)
	}
	if got := root.InputBagIDLists["global_test_code_input"]; !sameTestStringSet(got, []string{globalTestInputBagID}) {
		t.Fatalf("global_test_code_input list = %v, want [%s]", got, globalTestInputBagID)
	}

	reportVersionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_report")
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:   core.TaskDirectionFeedback,
		RunID:       runID,
		TaskID:      globalTestTaskID,
		AgentID:     "architect01",
		Op:          core.TaskOpTestCode,
		InputBagIDs: globalTestTask.InputBagIDs,
		Result:      core.TaskResultCodeOK,
		Commit: &core.CommitReceipt{
			Result:       core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{{Name: "global_test_report", ArtifactVersionIDs: []string{reportVersionID}}},
		},
	}); err != nil {
		t.Fatalf("global_test_code feedback error = %v", err)
	}
	root, err = instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root after delivery) error = %v", err)
	}
	if root.Status != core.PipelineInstanceStatusCompleted {
		t.Fatalf("root status = %s, want completed", root.Status)
	}
	if root.OutputBagIDs["global_test_report"] == "" {
		t.Fatalf("root output bags = %#v, want global_test_report", root.OutputBagIDs)
	}
	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want awaiting_acceptance", run.Status)
	}
}

func TestOnFeedbackFailMarksTaskAndRunFailed(t *testing.T) {
	bootstrap := app.NewBootstrap(t.TempDir())
	ctx := context.Background()

	const runID core.RunID = "run_fail_feedback"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow", noopRunConfig()); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "ceo",
		Op:        "ceo_write_requirement",
		Result:    core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "task_02",
		AgentID:   "pm01",
		Op:        "pm_write_plan",
		Result:    core.TaskResultCodeFail,
	}); err != nil {
		t.Fatalf("task_02 feedback error = %v", err)
	}

	task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, "task_02")
	if err != nil {
		t.Fatalf("Get(task_02) error = %v", err)
	}
	if task.Status != core.TaskStatusFailed {
		t.Fatalf("task_02 status = %s, want %s", task.Status, core.TaskStatusFailed)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusFailed {
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusFailed)
	}
	if _, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, "task_03"); err == nil {
		t.Fatalf("task_03 should not be created after failed feedback")
	}
}

func TestOnFeedbackPersistsArtifactMetadata(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	artifactRepo := repo.NewMemoryArtifactRepository()
	eventRepo := repo.NewMemoryEventRepository()
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		&recordingDispatcher{},
		&recordingSessionRuntime{},
		nil,
	)
	service.SetArtifactRepository(artifactRepo)
	service.SetEventRepository(eventRepo)

	const runID core.RunID = "run_artifact_metadata"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseOne,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	const requirementURI = "projects/run_artifact_metadata/agents/ceo/artifacts/requirement/requirement_v1.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_01",
		AgentID:      "ceo",
		Op:           "ceo_write_requirement",
		ArtifactURIs: []string{requirementURI},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}

	task, err := taskRepo.Get(ctx, runID, "task_01")
	if err != nil {
		t.Fatalf("Get(task_01) error = %v", err)
	}
	if task.Op != "ceo_write_requirement" {
		t.Fatalf("task op = %s, want ceo_write_requirement", task.Op)
	}
	if task.Result != core.TaskResultCodeOK {
		t.Fatalf("task result = %s, want %s", task.Result, core.TaskResultCodeOK)
	}

	artifacts, err := artifactRepo.ListByTask(ctx, runID, "task_01")
	if err != nil {
		t.Fatalf("ListByTask() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d, want 1", len(artifacts))
	}
	if artifacts[0].URI != requirementURI {
		t.Fatalf("artifact uri = %s, want %s", artifacts[0].URI, requirementURI)
	}
	if artifacts[0].Kind != "requirement" {
		t.Fatalf("artifact kind = %s, want requirement", artifacts[0].Kind)
	}

	events, err := eventRepo.ListByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListByRun(events) error = %v", err)
	}
	if !hasEventType(events, "feedback_received") {
		t.Fatalf("events should contain feedback_received: %#v", events)
	}
	if !hasEventType(events, "task_status_updated") {
		t.Fatalf("events should contain task_status_updated: %#v", events)
	}
}

func TestOnFeedbackCommitReceiptCreatesDoujiaGitSnapshot(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	dispatcher := &recordingDispatcher{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		&recordingSessionRuntime{},
		nil,
	)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_commit_receipt_snapshot"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseOne,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	object, err := doujiaGitRepo.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte("requirement")),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/sha256/requirement",
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := doujiaGitRepo.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  "ceo",
		LogicalKey: "requirement",
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := doujiaGitRepo.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "task_01",
		AgentID:   "ceo",
		Op:        "ceo_write_requirement",
		Commit: &core.CommitReceipt{
			Result: core.TaskResultCodeOK,
			ProducedBags: []core.CommittedBagDef{
				{Name: "requirement", ArtifactVersionIDs: []string{version.ArtifactVersionID}},
			},
			MaterializedOutputRefs: []string{"projects/run_commit_receipt_snapshot/agents/ceo/artifacts/requirement/requirement_v1.md"},
			DiagnosticsJSON:        `{"source":"test"}`,
		},
	}); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}

	ref, err := doujiaGitRepo.GetRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if len(ref.FrontierSnapshotIDs) != 1 {
		t.Fatalf("frontier snapshot count = %d, want 1", len(ref.FrontierSnapshotIDs))
	}
	if ref.FrontierSnapshotID == "" {
		t.Fatalf("ref current frontier snapshot id should be filled")
	}
	frontier, err := doujiaGitRepo.GetFrontierSnapshot(ctx, ref.FrontierSnapshotID)
	if err != nil {
		t.Fatalf("GetFrontierSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(frontier.TaskSnapshotIDs, ref.FrontierSnapshotIDs) {
		t.Fatalf("frontier task snapshots = %v, want %v", frontier.TaskSnapshotIDs, ref.FrontierSnapshotIDs)
	}
	moves, err := doujiaGitRepo.ListRefMoveEvents(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("ListRefMoveEvents() error = %v", err)
	}
	if len(moves) != 1 || !reflect.DeepEqual(moves[0].ToFrontierSnapshotIDs, []string{ref.FrontierSnapshotID}) {
		t.Fatalf("ref move events = %+v, want one move to %s", moves, ref.FrontierSnapshotID)
	}
	snapshot, err := doujiaGitRepo.GetSnapshot(ctx, ref.FrontierSnapshotIDs[0])
	if err != nil {
		t.Fatalf("GetSnapshot() error = %v", err)
	}
	if snapshot.TaskID != "task_01" || snapshot.Result != core.TaskResultCodeOK {
		t.Fatalf("snapshot = %+v, want task_01 ok", snapshot)
	}
	if len(snapshot.OutputBagIDs) != 1 {
		t.Fatalf("snapshot output bag count = %d, want 1", len(snapshot.OutputBagIDs))
	}
	bag, err := doujiaGitRepo.GetBag(ctx, snapshot.OutputBagIDs[0])
	if err != nil {
		t.Fatalf("GetBag() error = %v", err)
	}
	if len(bag.ArtifactVersionIDs) != 1 || bag.ArtifactVersionIDs[0] != version.ArtifactVersionID {
		t.Fatalf("bag versions = %v, want [%s]", bag.ArtifactVersionIDs, version.ArtifactVersionID)
	}
	producer, err := doujiaGitRepo.ProducerOfBag(ctx, bag.BagID)
	if err != nil {
		t.Fatalf("ProducerOfBag() error = %v", err)
	}
	if producer != snapshot.SnapshotID {
		t.Fatalf("producer = %q, want %q", producer, snapshot.SnapshotID)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.dispatched))
	}
	dispatch := dispatcher.dispatched[0]
	if got, want := len(dispatch.InputBags), 1; got != want {
		t.Fatalf("dispatch input bags = %#v, want %d named binding", dispatch.InputBags, want)
	}
	if dispatch.InputBags[0].Name != "requirement" {
		t.Fatalf("dispatch input bag name = %q, want requirement", dispatch.InputBags[0].Name)
	}
	if dispatch.InputBags[0].BagID != snapshot.OutputBagIDs[0] {
		t.Fatalf("dispatch input bag id = %q, want %q", dispatch.InputBags[0].BagID, snapshot.OutputBagIDs[0])
	}
}

func TestOnFeedbackRewriteBlocksTaskAndCreatesRewriteChild(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_child_rewrite"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "phase_one_requirement_flow",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_01",
		AgentID:      "ceo",
		Op:           "ceo_write_requirement",
		ArtifactURIs: []string{"projects/run_child_rewrite/agents/ceo/artifacts/requirement/requirement_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}
	rewriteArtifacts := []string{"projects/run_child_rewrite/agents/pm01/artifacts/review/rewrite_instruction.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_02",
		AgentID:      "pm01",
		Op:           "pm_write_plan",
		ArtifactURIs: rewriteArtifacts,
		Result:       core.TaskResultCodeRewrite,
	}); err != nil {
		t.Fatalf("task_02 feedback error = %v", err)
	}

	task, err := taskRepo.Get(ctx, runID, "task_02")
	if err != nil {
		t.Fatalf("Get(task_02) error = %v", err)
	}
	if task.Status != core.TaskStatusBlocked {
		t.Fatalf("task_02 status = %s, want %s", task.Status, core.TaskStatusBlocked)
	}

	child, err := taskRepo.Get(ctx, runID, "task_02_child_01")
	if err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
	if child.ParentID == nil || *child.ParentID != "task_02" {
		t.Fatalf("child parent = %v, want task_02", child.ParentID)
	}
	if child.AgentID != "ceo" {
		t.Fatalf("child agent_id = %s, want ceo", child.AgentID)
	}
	if len(sessionDispatcher.dispatched) != 1 {
		t.Fatalf("session dispatch count = %d, want 1", len(sessionDispatcher.dispatched))
	}
	if sessionDispatcher.dispatched[0].Op != core.TaskOpRewrite {
		t.Fatalf("child op = %s, want %s", sessionDispatcher.dispatched[0].Op, core.TaskOpRewrite)
	}
	for _, want := range []string{
		"projects/run_child_rewrite/agents/ceo/artifacts/requirement/requirement_v1.md",
		rewriteArtifacts[0],
	} {
		if !containsString(sessionDispatcher.dispatched[0].ArtifactURIs, want) {
			t.Fatalf("rewrite child artifact uris = %v, missing %s", sessionDispatcher.dispatched[0].ArtifactURIs, want)
		}
	}
}

func TestOnFeedbackReplanBlocksTaskAndCreatesReplanChild(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_child_replan"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "phase_one_requirement_flow",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	task01Artifacts := []string{"projects/run_child_replan/agents/ceo/artifacts/requirement/requirement_v1.md"}
	task02Artifacts := []string{"projects/run_child_replan/agents/pm01/artifacts/prd/plan_v1.md"}
	task04Artifacts := []string{"projects/run_child_replan/agents/architect01/artifacts/design/architecture_v1.md"}

	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_01", AgentID: "ceo", Op: "ceo_write_requirement", ArtifactURIs: task01Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_02", AgentID: "pm01", Op: "pm_write_plan", ArtifactURIs: task02Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_03", AgentID: "ceo", Op: "ceo_review_plan", ArtifactURIs: task02Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_04", AgentID: "architect01", Op: "architecture_generation", ArtifactURIs: task04Artifacts, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}
	reviewArtifacts := []string{"projects/run_child_replan/agents/pm01/artifacts/review/review_note_v1.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_05",
		AgentID:      "pm01",
		Op:           "pm_review_design",
		ArtifactURIs: reviewArtifacts,
		Result:       core.TaskResultCodeReplan,
	}); err != nil {
		t.Fatalf("task_05 feedback error = %v", err)
	}

	task, err := taskRepo.Get(ctx, runID, "task_05")
	if err != nil {
		t.Fatalf("Get(task_05) error = %v", err)
	}
	if task.Status != core.TaskStatusBlocked {
		t.Fatalf("task_05 status = %s, want %s", task.Status, core.TaskStatusBlocked)
	}

	child, err := taskRepo.Get(ctx, runID, "task_05_child_01")
	if err != nil {
		t.Fatalf("Get(child) error = %v", err)
	}
	if child.ParentID == nil || *child.ParentID != "task_05" {
		t.Fatalf("child parent = %v, want task_05", child.ParentID)
	}
	if child.AgentID != "architect01" {
		t.Fatalf("child agent_id = %s, want architect01", child.AgentID)
	}
	if len(dispatcher.dispatched) == 0 {
		t.Fatalf("expected task runtime dispatch to record child task")
	}
	last := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if last.Op != core.TaskOpReplan {
		t.Fatalf("child op = %s, want %s", last.Op, core.TaskOpReplan)
	}
	wantChildArtifacts := []string{
		task02Artifacts[0],
		task04Artifacts[0],
		reviewArtifacts[0],
	}
	for _, want := range wantChildArtifacts {
		if !containsString(last.ArtifactURIs, want) {
			t.Fatalf("child artifact uris = %v, missing %s", last.ArtifactURIs, want)
		}
	}
}

func TestChildFeedbackRedispatchesParentWithMergedArtifacts(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseOne()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_child_feedback_merge"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: "phase_one_requirement_flow",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	task01Artifacts := []string{"projects/run_child_feedback_merge/agents/ceo/artifacts/requirement/requirement_v1.md"}
	task02Artifacts := []string{"projects/run_child_feedback_merge/agents/pm01/artifacts/prd/plan_v1.md"}
	task04Artifacts := []string{"projects/run_child_feedback_merge/agents/architect01/artifacts/design/architecture_v1.md"}
	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_01", AgentID: "ceo", Op: "ceo_write_requirement", ArtifactURIs: task01Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_02", AgentID: "pm01", Op: "pm_write_plan", ArtifactURIs: task02Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_03", AgentID: "ceo", Op: "ceo_review_plan", ArtifactURIs: task02Artifacts, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_04", AgentID: "architect01", Op: "architecture_generation", ArtifactURIs: task04Artifacts, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}

	reviewArtifacts := []string{"projects/run_child_feedback_merge/agents/pm01/artifacts/review/review_note_v1.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_05",
		AgentID:      "pm01",
		Op:           "pm_review_design",
		ArtifactURIs: reviewArtifacts,
		Result:       core.TaskResultCodeReplan,
	}); err != nil {
		t.Fatalf("task_05 feedback error = %v", err)
	}

	replannedDesign := []string{"projects/run_child_feedback_merge/agents/architect01/artifacts/design/architecture_v2.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_05_child_01",
		ParentID:     taskIDPtr("task_05"),
		AgentID:      "architect01",
		Op:           core.TaskOpReplan,
		ArtifactURIs: replannedDesign,
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("task_05_child_01 feedback error = %v", err)
	}

	parent, err := taskRepo.Get(ctx, runID, "task_05")
	if err != nil {
		t.Fatalf("Get(task_05) error = %v", err)
	}
	if parent.Status != core.TaskStatusDispatched {
		t.Fatalf("task_05 status = %s, want %s", parent.Status, core.TaskStatusDispatched)
	}
	last := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if last.TaskID != "task_05" || last.Op != "pm_review_design" {
		t.Fatalf("last dispatch = %+v, want redispatched task_05 review", last)
	}
	wantRedispatchArtifacts := []string{
		task02Artifacts[0],
		replannedDesign[0],
	}
	for _, want := range wantRedispatchArtifacts {
		if !containsString(last.ArtifactURIs, want) {
			t.Fatalf("redispatch artifact uris = %v, missing %s", last.ArtifactURIs, want)
		}
	}
}

func TestPhaseTwoSplitModuleControlCreatesDynamicCoderTesterTasks(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_control"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_01", AgentID: "ceo", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control/agents/ceo/artifacts/requirement/requirement_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_02", AgentID: "pm01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_03", AgentID: "ceo", Op: core.TaskOpReviewPlan, ArtifactURIs: []string{"projects/run_phase_two_control/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_04", AgentID: "architect01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control/agents/architect01/artifacts/architecture/global_architecture_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_05", AgentID: "pm01", Op: core.TaskOpReviewPlan, ArtifactURIs: []string{"projects/run_phase_two_control/agents/architect01/artifacts/architecture/global_architecture_v1.md"}, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}
	if len(dispatcher.dispatched) == 0 {
		t.Fatalf("expected split_module dispatch")
	}
	splitDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if splitDispatch.Op != core.TaskOpSplitModule {
		t.Fatalf("last op = %s, want %s", splitDispatch.Op, core.TaskOpSplitModule)
	}
	if !containsString(splitDispatch.ArtifactURIs, orchestrator.DeliveryConfigArtifactURI(runID)) {
		t.Fatalf("split_module artifact uris = %v, want delivery config uri", splitDispatch.ArtifactURIs)
	}

	coderTaskURI := "projects/run_phase_two_control/agents/architect01/artifacts/modules/coder01_task.md"
	testerTaskURI := "projects/run_phase_two_control/agents/architect01/artifacts/tests/tester01_task.md"
	mainBranchURI := "projects/run_phase_two_control/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run_phase_two_control/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run_phase_two_control/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{"projects/run_phase_two_control/agents/architect01/artifacts/module/module_plan_v1.json"},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "coder01", ArtifactURIs: []string{coderTaskURI, mainBranchURI, contractURI, seedURI}},
			{Type: core.ControlTypeNewTester, AgentName: "tester01", ArtifactURIs: []string{testerTaskURI, coderTaskURI, contractURI, seedURI}},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	coderTask, err := taskRepo.Get(ctx, runID, "task_06_coder01_write_code")
	if err != nil {
		t.Fatalf("Get(coder task) error = %v", err)
	}
	if coderTask.AgentRole != core.AgentRoleCoder || coderTask.AgentID != "coder01" || coderTask.Status != core.TaskStatusDispatched {
		t.Fatalf("coder task = %+v, want coder01 dispatched", coderTask)
	}
	testerTask, err := taskRepo.Get(ctx, runID, "task_06_tester01_test_data")
	if err != nil {
		t.Fatalf("Get(tester task) error = %v", err)
	}
	if testerTask.AgentRole != core.AgentRoleTester || testerTask.AgentID != "tester01" || testerTask.Status != core.TaskStatusDispatched {
		t.Fatalf("tester task = %+v, want tester01 dispatched", testerTask)
	}
	testCodeTask, err := taskRepo.Get(ctx, runID, "task_06_tester01_test_code")
	if err != nil {
		t.Fatalf("Get(test code task) error = %v", err)
	}
	if testCodeTask.Status != core.TaskStatusPending {
		t.Fatalf("test code status = %s, want pending", testCodeTask.Status)
	}
	if len(testCodeTask.DependsOnIDs) != 2 {
		t.Fatalf("test code deps = %v, want coder+test_data deps", testCodeTask.DependsOnIDs)
	}
	for _, want := range []string{testerTaskURI, coderTaskURI, contractURI, seedURI} {
		if !containsString(artifactRefsToStringsForTest(testCodeTask.InputArtifactRefs), want) {
			t.Fatalf("test_code static inputs = %v, missing %s", testCodeTask.InputArtifactRefs, want)
		}
	}

	if len(dispatcher.dispatched) < 3 {
		t.Fatalf("dispatch count = %d, want split + coder/tester dispatches", len(dispatcher.dispatched))
	}
	if countDispatchOp(dispatcher.dispatched, core.TaskOpWriteCode) != 1 || countDispatchOp(dispatcher.dispatched, core.TaskOpTestData) != 1 {
		t.Fatalf("dispatches = %+v, want one write_code and one test_data", dispatcher.dispatched)
	}
}

func TestPhaseTwoSplitModuleControlCreatesDynamicTasksWithSQLite(t *testing.T) {
	ctx := context.Background()
	repos, db, err := repo.OpenSQLiteRepositories(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenSQLiteRepositories() error = %v", err)
	}
	defer db.Close()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		repos.Runs,
		repos.Tasks,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)
	service.SetArtifactRepository(repos.Artifacts)
	service.SetEventRepository(repos.Events)

	const runID core.RunID = "run_phase_two_control_sqlite"
	if err := repos.Runs.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := service.Start(ctx, runID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_01", AgentID: "ceo", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/ceo/artifacts/requirement/requirement_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_02", AgentID: "pm01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_03", AgentID: "ceo", Op: core.TaskOpReviewPlan, ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/pm01/artifacts/prd/plan_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_04", AgentID: "architect01", Op: core.TaskOpWritePlan, ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/architect01/artifacts/architecture/global_architecture_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_05", AgentID: "pm01", Op: core.TaskOpReviewPlan, ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/architect01/artifacts/architecture/global_architecture_v1.md"}, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}

	coderTaskURI := "projects/run_phase_two_control_sqlite/agents/architect01/artifacts/modules/coder01_task.md"
	testerTaskURI := "projects/run_phase_two_control_sqlite/agents/architect01/artifacts/tests/tester01_task.md"
	mainBranchURI := "projects/run_phase_two_control_sqlite/agents/architect01/artifacts/branches/main_branch.md"
	contractURI := "projects/run_phase_two_control_sqlite/agents/architect01/artifacts/contracts/module01_contract.json"
	seedURI := "projects/run_phase_two_control_sqlite/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{"projects/run_phase_two_control_sqlite/agents/architect01/artifacts/module/module_plan_v1.json"},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "coder01", ArtifactURIs: []string{coderTaskURI, mainBranchURI, contractURI, seedURI}},
			{Type: core.ControlTypeNewTester, AgentName: "tester01", ArtifactURIs: []string{testerTaskURI, coderTaskURI, contractURI, seedURI}},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	coderTask, err := repos.Tasks.Get(ctx, runID, "task_06_coder01_write_code")
	if err != nil {
		t.Fatalf("Get(coder task) error = %v", err)
	}
	if coderTask.Status != core.TaskStatusDispatched {
		t.Fatalf("coder task status = %s, want dispatched", coderTask.Status)
	}
	if countDispatchOp(dispatcher.dispatched, core.TaskOpWriteCode) != 1 || countDispatchOp(dispatcher.dispatched, core.TaskOpTestData) != 1 {
		t.Fatalf("dispatches = %+v, want one write_code and one test_data", dispatcher.dispatched)
	}
}

func TestPhaseTwoModulePairCreatesTestCodeAndMergeDependencies(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_pair_deps"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "task_06",
		RunID:     runID,
		StageID:   "task_06",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Status:    core.TaskStatusDispatched,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(task_06) error = %v", err)
	}

	coderTask01 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/modules/coder01_task.md"
	testerTask01 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/tests/tester01_task.md"
	mainBranchURI := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/branches/main_branch.md"
	contract01 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/contracts/module01_contract.json"
	seed01 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/seed_tests/module01_seed_tests.json"
	coderTask02 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/modules/coder02_task.md"
	testerTask02 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/tests/tester02_task.md"
	contract02 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/contracts/module02_contract.json"
	seed02 := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/seed_tests/module02_seed_tests.json"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/architect01/artifacts/module/module_plan_v1.json"},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "coder01", ArtifactURIs: []string{coderTask01, mainBranchURI, contract01, seed01}},
			{Type: core.ControlTypeNewTester, AgentName: "tester01", ArtifactURIs: []string{testerTask01, coderTask01, contract01, seed01}},
			{Type: core.ControlTypeNewCoder, AgentName: "coder02", ArtifactURIs: []string{coderTask02, mainBranchURI, contract02, seed02}},
			{Type: core.ControlTypeNewTester, AgentName: "tester02", ArtifactURIs: []string{testerTask02, coderTask02, contract02, seed02}},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	if len(dispatcher.dispatched) != 4 {
		t.Fatalf("dispatch count after split = %d, want 4 write/test-data tasks", len(dispatcher.dispatched))
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_coder01_write_code",
		ParentID:     taskIDPtr("task_06"),
		AgentID:      "coder01",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/coder01/artifacts/code/coder01_code_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("coder01 feedback error = %v", err)
	}
	testCode01, err := taskRepo.Get(ctx, runID, "task_06_tester01_test_code")
	if err != nil {
		t.Fatalf("Get(tester01 test_code) error = %v", err)
	}
	if testCode01.Status != core.TaskStatusPending {
		t.Fatalf("tester01 test_code status = %s, want pending until test_data done", testCode01.Status)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_tester01_test_data",
		ParentID:     taskIDPtr("task_06"),
		AgentID:      "tester01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/tester01/artifacts/test/tester01_data_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("tester01 test_data feedback error = %v", err)
	}
	testCode01, err = taskRepo.Get(ctx, runID, "task_06_tester01_test_code")
	if err != nil {
		t.Fatalf("Get(tester01 test_code) error = %v", err)
	}
	if testCode01.Status != core.TaskStatusDispatched {
		t.Fatalf("tester01 test_code status = %s, want dispatched", testCode01.Status)
	}
	last := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if last.TaskID != "task_06_tester01_test_code" || last.Op != core.TaskOpTestCode {
		t.Fatalf("last dispatch = %+v, want tester01 test_code", last)
	}
	wantTestCodeInputs := []string{
		testerTask01,
		coderTask01,
		contract01,
		seed01,
		"projects/run_phase_two_pair_deps/agents/coder01/artifacts/code/coder01_code_v1.md",
		"projects/run_phase_two_pair_deps/agents/tester01/artifacts/test/tester01_data_v1.md",
	}
	for _, want := range wantTestCodeInputs {
		if !containsString(last.ArtifactURIs, want) {
			t.Fatalf("tester01 test_code inputs = %v, missing %s", last.ArtifactURIs, want)
		}
	}

	feedbacks := []core.TaskMetaData{
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_06_coder02_write_code", ParentID: taskIDPtr("task_06"), AgentID: "coder02", Op: core.TaskOpWriteCode, ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/coder02/artifacts/code/coder02_code_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_06_tester02_test_data", ParentID: taskIDPtr("task_06"), AgentID: "tester02", Op: core.TaskOpTestData, ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/tester02/artifacts/test/tester02_data_v1.md"}, Result: core.TaskResultCodeOK},
		{Direction: core.TaskDirectionFeedback, RunID: runID, TaskID: "task_06_tester01_test_code", ParentID: taskIDPtr("task_06"), AgentID: "tester01", Op: core.TaskOpTestCode, ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/tester01/artifacts/test/tester01_report_v1.md"}, Result: core.TaskResultCodeOK},
	}
	for _, feedback := range feedbacks {
		if err := service.OnFeedback(ctx, feedback); err != nil {
			t.Fatalf("feedback %s error = %v", feedback.TaskID, err)
		}
	}
	mergeTask, err := taskRepo.Get(ctx, runID, "task_06_merge_code")
	if err != nil {
		t.Fatalf("Get(merge task) error = %v", err)
	}
	if mergeTask.Status != core.TaskStatusPending {
		t.Fatalf("merge status = %s, want pending until tester02 test_code done", mergeTask.Status)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_tester02_test_code",
		ParentID:     taskIDPtr("task_06"),
		AgentID:      "tester02",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{"projects/run_phase_two_pair_deps/agents/tester02/artifacts/test/tester02_report_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("tester02 test_code feedback error = %v", err)
	}
	mergeTask, err = taskRepo.Get(ctx, runID, "task_06_merge_code")
	if err != nil {
		t.Fatalf("Get(merge task) error = %v", err)
	}
	if mergeTask.Status != core.TaskStatusDispatched {
		t.Fatalf("merge status = %s, want dispatched", mergeTask.Status)
	}
	mergeDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if mergeDispatch.TaskID != "task_06_merge_code" || mergeDispatch.Op != core.TaskOpMergeCode {
		t.Fatalf("last dispatch = %+v, want merge_code", mergeDispatch)
	}
	wantMergeInputs := []string{
		"projects/run_phase_two_pair_deps/agents/tester01/artifacts/test/tester01_report_v1.md",
		"projects/run_phase_two_pair_deps/agents/tester02/artifacts/test/tester02_report_v1.md",
		"projects/run_phase_two_pair_deps/agents/coder01/artifacts/code/coder01_code_v1.md",
		"projects/run_phase_two_pair_deps/agents/coder02/artifacts/code/coder02_code_v1.md",
		coderTask01,
		coderTask02,
		contract01,
		contract02,
		seed01,
		seed02,
	}
	for _, want := range wantMergeInputs {
		if !containsString(mergeDispatch.ArtifactURIs, want) {
			t.Fatalf("merge inputs = %v, missing %s", mergeDispatch.ArtifactURIs, want)
		}
	}

	globalTestDataTask, err := taskRepo.Get(ctx, runID, "task_06_global_test_data")
	if err != nil {
		t.Fatalf("Get(global test_data task) error = %v", err)
	}
	if globalTestDataTask.Status != core.TaskStatusPending {
		t.Fatalf("global test_data status = %s, want pending until merge done", globalTestDataTask.Status)
	}
	globalTestCodeTask, err := taskRepo.Get(ctx, runID, "task_06_global_test_code")
	if err != nil {
		t.Fatalf("Get(global test_code task) error = %v", err)
	}
	if globalTestCodeTask.Status != core.TaskStatusPending {
		t.Fatalf("global test_code status = %s, want pending until global test_data done", globalTestCodeTask.Status)
	}

	mergeArtifact := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/code/merged_code_v1.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_merge_code",
		ParentID:     taskIDPtr("task_06"),
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mergeArtifact},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("merge feedback error = %v", err)
	}
	globalTestDataTask, err = taskRepo.Get(ctx, runID, "task_06_global_test_data")
	if err != nil {
		t.Fatalf("Get(global test_data task after merge) error = %v", err)
	}
	if globalTestDataTask.Status != core.TaskStatusDispatched {
		t.Fatalf("global test_data status after merge = %s, want dispatched", globalTestDataTask.Status)
	}
	globalDataDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if globalDataDispatch.TaskID != "task_06_global_test_data" || globalDataDispatch.Op != core.TaskOpTestData {
		t.Fatalf("last dispatch = %+v, want architect global test_data", globalDataDispatch)
	}
	if !containsString(globalDataDispatch.ArtifactURIs, mergeArtifact) {
		t.Fatalf("global test_data inputs = %v, missing %s", globalDataDispatch.ArtifactURIs, mergeArtifact)
	}

	globalDataArtifact := "projects/run_phase_two_pair_deps/agents/architect01/artifacts/test_data/architect_test_data.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_global_test_data",
		ParentID:     taskIDPtr("task_06"),
		AgentID:      "architect01",
		Op:           core.TaskOpTestData,
		ArtifactURIs: []string{globalDataArtifact},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("global test_data feedback error = %v", err)
	}
	globalTestCodeTask, err = taskRepo.Get(ctx, runID, "task_06_global_test_code")
	if err != nil {
		t.Fatalf("Get(global test_code task after test_data) error = %v", err)
	}
	if globalTestCodeTask.Status != core.TaskStatusDispatched {
		t.Fatalf("global test_code status after test_data = %s, want dispatched", globalTestCodeTask.Status)
	}
	globalCodeDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if globalCodeDispatch.TaskID != "task_06_global_test_code" || globalCodeDispatch.Op != core.TaskOpTestCode {
		t.Fatalf("last dispatch = %+v, want architect global test_code", globalCodeDispatch)
	}
	for _, want := range []string{mergeArtifact, globalDataArtifact} {
		if !containsString(globalCodeDispatch.ArtifactURIs, want) {
			t.Fatalf("global test_code inputs = %v, missing %s", globalCodeDispatch.ArtifactURIs, want)
		}
	}
}

func TestPhaseTwoInvalidControlCreatesResplitTask(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_invalid_control"
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	if err := taskRepo.Create(ctx, core.Task{
		ID:        "task_06",
		RunID:     runID,
		StageID:   "task_06",
		AgentRole: core.AgentRoleArchitect,
		AgentID:   "architect01",
		Status:    core.TaskStatusDispatched,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create(task_06) error = %v", err)
	}

	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{"projects/run_phase_two_invalid_control/agents/architect01/artifacts/module/module_plan_v1.json"},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "", ArtifactURIs: []string{"projects/run_phase_two_invalid_control/agents/architect01/artifacts/module/module01.md"}},
		},
	}); err != nil {
		t.Fatalf("split_module feedback error = %v", err)
	}

	task, err := taskRepo.Get(ctx, runID, "task_06")
	if err != nil {
		t.Fatalf("Get(task_06) error = %v", err)
	}
	if task.Status != core.TaskStatusBlocked {
		t.Fatalf("task_06 status = %s, want %s", task.Status, core.TaskStatusBlocked)
	}
	child, err := taskRepo.Get(ctx, runID, "task_06_child_01")
	if err != nil {
		t.Fatalf("Get(resplit child) error = %v", err)
	}
	if child.AgentRole != core.AgentRoleArchitect || child.AgentID != "architect01" {
		t.Fatalf("child = %+v, want architect01", child)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.dispatched))
	}
	if dispatcher.dispatched[0].Op != core.TaskOpResplitModule {
		t.Fatalf("child op = %s, want %s", dispatcher.dispatched[0].Op, core.TaskOpResplitModule)
	}
	if !containsString(dispatcher.dispatched[0].ArtifactURIs, orchestrator.DeliveryConfigArtifactURI(runID)) {
		t.Fatalf("resplit artifact uris = %v, want delivery config uri", dispatcher.dispatched[0].ArtifactURIs)
	}
}

func TestDynamicCoderBugCreatesDebugTaskAndDebugSuccessUnblocksDependents(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_coder_debug_retry"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	parentID := core.TaskID("task_06")
	coderID := core.TaskID("task_06_coder01_write_code")
	testDataID := core.TaskID("task_06_tester01_test_data")
	testCodeID := core.TaskID("task_06_tester01_test_code")
	tasks := []core.Task{
		{
			ID:        parentID,
			RunID:     runID,
			StageID:   "task_06",
			AgentRole: core.AgentRoleArchitect,
			AgentID:   "architect01",
			Status:    core.TaskStatusDone,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:                coderID,
			RunID:             runID,
			StageID:           core.StageID(coderID),
			AgentRole:         core.AgentRoleCoder,
			AgentID:           "coder01",
			ParentID:          &parentID,
			DependsOnIDs:      []core.TaskID{parentID},
			Status:            core.TaskStatusDispatched,
			InputArtifactRefs: toRefs([]string{"projects/run_phase_two_coder_debug_retry/agents/architect01/artifacts/modules/coder01_task.md", "projects/run_phase_two_coder_debug_retry/agents/architect01/artifacts/branches/main_branch.md"}),
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		{
			ID:                 testDataID,
			RunID:              runID,
			StageID:            core.StageID(testDataID),
			AgentRole:          core.AgentRoleTester,
			AgentID:            "tester01",
			ParentID:           &parentID,
			DependsOnIDs:       []core.TaskID{parentID},
			Status:             core.TaskStatusDone,
			OutputArtifactRefs: toRefs([]string{"projects/run_phase_two_coder_debug_retry/agents/tester01/artifacts/test/tester01_test_data_v1.md"}),
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		{
			ID:           testCodeID,
			RunID:        runID,
			StageID:      core.StageID(testCodeID),
			AgentRole:    core.AgentRoleTester,
			AgentID:      "tester01",
			ParentID:     &parentID,
			DependsOnIDs: []core.TaskID{coderID, testDataID},
			Status:       core.TaskStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	for _, task := range tasks {
		if err := taskRepo.Create(ctx, task); err != nil {
			t.Fatalf("Create(%s) error = %v", task.ID, err)
		}
	}

	bugArtifacts := []string{
		"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/branches/coder01_failed_branch.md",
		"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/test_reports/coder01_host_test_failure.md",
		"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/failures/write_code_failure.md",
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       coderID,
		ParentID:     taskIDPtr(parentID),
		AgentID:      "coder01",
		Op:           core.TaskOpWriteCode,
		ArtifactURIs: bugArtifacts,
		Result:       core.TaskResultCodeBug,
	}); err != nil {
		t.Fatalf("coder bug feedback error = %v", err)
	}

	coderTask, err := taskRepo.Get(ctx, runID, coderID)
	if err != nil {
		t.Fatalf("Get(coder task) error = %v", err)
	}
	if coderTask.Status != core.TaskStatusBlocked {
		t.Fatalf("coder status = %s, want %s", coderTask.Status, core.TaskStatusBlocked)
	}
	debugTask, err := taskRepo.Get(ctx, runID, "task_06_coder01_write_code_child_01")
	if err != nil {
		t.Fatalf("Get(debug child) error = %v", err)
	}
	if debugTask.AgentID != "coder01" || debugTask.AgentRole != core.AgentRoleCoder {
		t.Fatalf("debug child = %+v, want coder01 debug child", debugTask)
	}
	if len(dispatcher.dispatched) == 0 {
		t.Fatal("expected debug dispatch")
	}
	firstDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if firstDispatch.Op != core.TaskOpDebug {
		t.Fatalf("child op = %s, want %s", firstDispatch.Op, core.TaskOpDebug)
	}
	for _, want := range []string{
		"projects/run_phase_two_coder_debug_retry/agents/architect01/artifacts/modules/coder01_task.md",
		"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/branches/coder01_failed_branch.md",
		"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/test_reports/coder01_host_test_failure.md",
	} {
		if !containsString(firstDispatch.ArtifactURIs, want) {
			t.Fatalf("debug artifacts = %v, missing %s", firstDispatch.ArtifactURIs, want)
		}
	}

	debugOutput := []string{"projects/run_phase_two_coder_debug_retry/agents/coder01/artifacts/branches/coder01_debug_branch.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_coder01_write_code_child_01",
		ParentID:     taskIDPtr(coderID),
		AgentID:      "coder01",
		Op:           core.TaskOpDebug,
		ArtifactURIs: debugOutput,
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("debug child feedback error = %v", err)
	}

	coderTask, err = taskRepo.Get(ctx, runID, coderID)
	if err != nil {
		t.Fatalf("Get(coder task after debug) error = %v", err)
	}
	if coderTask.Status != core.TaskStatusDone {
		t.Fatalf("coder status after debug = %s, want %s", coderTask.Status, core.TaskStatusDone)
	}
	lastDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if lastDispatch.TaskID != testCodeID || lastDispatch.Op != core.TaskOpTestCode {
		t.Fatalf("last dispatch = %+v, want tester test_code dispatch", lastDispatch)
	}
	if !containsString(lastDispatch.ArtifactURIs, debugOutput[0]) || !containsString(lastDispatch.ArtifactURIs, "projects/run_phase_two_coder_debug_retry/agents/tester01/artifacts/test/tester01_test_data_v1.md") {
		t.Fatalf("test_code artifacts = %v, want debug branch + test data", lastDispatch.ArtifactURIs)
	}
}

func TestDynamicTesterTestCodeBugCreatesCoderDebugTaskAndRetests(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_tester_debug_retry"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	parentID := core.TaskID("task_06")
	coderID := core.TaskID("task_06_coder01_write_code")
	testDataID := core.TaskID("task_06_tester01_test_data")
	testCodeID := core.TaskID("task_06_tester01_test_code")
	coderInputs := []string{
		"projects/run_phase_two_tester_debug_retry/agents/architect01/artifacts/modules/coder01_task.md",
		"projects/run_phase_two_tester_debug_retry/agents/architect01/artifacts/branches/main_branch.md",
		"projects/run_phase_two_tester_debug_retry/agents/architect01/artifacts/contracts/module01_contract.json",
		"projects/run_phase_two_tester_debug_retry/agents/architect01/artifacts/seed_tests/module01_seed_tests.json",
	}
	testerInputs := []string{
		"projects/run_phase_two_tester_debug_retry/agents/architect01/artifacts/tests/tester01_task.md",
		coderInputs[0],
		coderInputs[2],
		coderInputs[3],
	}
	coderBranch := "projects/run_phase_two_tester_debug_retry/agents/coder01/artifacts/branches/coder01_branch.md"
	fullTests := "projects/run_phase_two_tester_debug_retry/agents/tester01/artifacts/test_data/full_test_files.json"
	tasks := []core.Task{
		{ID: parentID, RunID: runID, StageID: "task_06", AgentRole: core.AgentRoleArchitect, AgentID: "architect01", Status: core.TaskStatusDone, CreatedAt: now, UpdatedAt: now},
		{ID: coderID, RunID: runID, StageID: core.StageID(coderID), AgentRole: core.AgentRoleCoder, AgentID: "coder01", ParentID: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusDone, InputArtifactRefs: toRefs(coderInputs), OutputArtifactRefs: toRefs([]string{coderBranch}), CreatedAt: now, UpdatedAt: now},
		{ID: testDataID, RunID: runID, StageID: core.StageID(testDataID), AgentRole: core.AgentRoleTester, AgentID: "tester01", ParentID: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusDone, InputArtifactRefs: toRefs(testerInputs), OutputArtifactRefs: toRefs([]string{fullTests}), CreatedAt: now, UpdatedAt: now},
		{ID: testCodeID, RunID: runID, StageID: core.StageID(testCodeID), AgentRole: core.AgentRoleTester, AgentID: "tester01", ParentID: &parentID, DependsOnIDs: []core.TaskID{coderID, testDataID}, Status: core.TaskStatusDispatched, InputArtifactRefs: toRefs(append(append([]string{}, testerInputs...), coderBranch, fullTests)), CreatedAt: now, UpdatedAt: now},
	}
	for _, task := range tasks {
		if err := taskRepo.Create(ctx, task); err != nil {
			t.Fatalf("Create(%s) error = %v", task.ID, err)
		}
	}

	reportURI := "projects/run_phase_two_tester_debug_retry/agents/tester01/artifacts/test_reports/test_failure_report.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       testCodeID,
		ParentID:     taskIDPtr(parentID),
		AgentID:      "tester01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{reportURI},
		Result:       core.TaskResultCodeBug,
	}); err != nil {
		t.Fatalf("tester test_code bug feedback error = %v", err)
	}

	testCodeTask, err := taskRepo.Get(ctx, runID, testCodeID)
	if err != nil {
		t.Fatalf("Get(test_code task) error = %v", err)
	}
	if testCodeTask.Status != core.TaskStatusBlocked {
		t.Fatalf("test_code status = %s, want blocked", testCodeTask.Status)
	}
	debugTask, err := taskRepo.Get(ctx, runID, "task_06_tester01_test_code_child_01")
	if err != nil {
		t.Fatalf("Get(debug child) error = %v", err)
	}
	if debugTask.AgentID != "coder01" || debugTask.AgentRole != core.AgentRoleCoder {
		t.Fatalf("debug child = %+v, want coder01 debug task", debugTask)
	}
	debugDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if debugDispatch.Op != core.TaskOpDebug {
		t.Fatalf("debug dispatch op = %s, want %s", debugDispatch.Op, core.TaskOpDebug)
	}
	for _, want := range []string{coderInputs[0], coderInputs[1], coderInputs[2], coderInputs[3], coderBranch, fullTests, reportURI} {
		if !containsString(debugDispatch.ArtifactURIs, want) {
			t.Fatalf("debug inputs = %v, missing %s", debugDispatch.ArtifactURIs, want)
		}
	}

	debugBranch := "projects/run_phase_two_tester_debug_retry/agents/coder01/artifacts/branches/coder01_debug_branch.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06_tester01_test_code_child_01",
		ParentID:     taskIDPtr(testCodeID),
		AgentID:      "coder01",
		Op:           core.TaskOpDebug,
		ArtifactURIs: []string{debugBranch},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("debug child feedback error = %v", err)
	}
	lastDispatch := dispatcher.dispatched[len(dispatcher.dispatched)-1]
	if lastDispatch.TaskID != testCodeID || lastDispatch.Op != core.TaskOpTestCode {
		t.Fatalf("last dispatch = %+v, want redispatched tester test_code", lastDispatch)
	}
	for _, want := range []string{debugBranch, fullTests, coderInputs[2], coderInputs[3]} {
		if !containsString(lastDispatch.ArtifactURIs, want) {
			t.Fatalf("redispatched test_code inputs = %v, missing %s", lastDispatch.ArtifactURIs, want)
		}
	}
}

func TestDynamicTestDataTransientTimeoutRetriesOnceThenFails(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	dispatcher := &recordingDispatcher{}
	sessionDispatcher := &recordingSessionRuntime{}
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		recordingProvisioner{},
		dispatcher,
		sessionDispatcher,
		nil,
	)

	const runID core.RunID = "run_phase_two_test_data_timeout_retry"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	parentID := core.TaskID("task_06")
	testDataID := core.TaskID("task_06_tester02_test_data")
	testDataInputs := []string{
		"projects/run_phase_two_test_data_timeout_retry/agents/architect01/artifacts/container/container_context.json",
		"projects/run_phase_two_test_data_timeout_retry/agents/architect01/artifacts/module_specs/module02_spec.json",
	}
	for _, task := range []core.Task{
		{
			ID:        parentID,
			RunID:     runID,
			StageID:   "task_06",
			AgentRole: core.AgentRoleArchitect,
			AgentID:   "architect01",
			Status:    core.TaskStatusDone,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:                testDataID,
			RunID:             runID,
			StageID:           core.StageID(testDataID),
			AgentRole:         core.AgentRoleTester,
			AgentID:           "tester02",
			ParentID:          &parentID,
			DependsOnIDs:      []core.TaskID{parentID},
			Status:            core.TaskStatusDispatched,
			InputArtifactRefs: toRefs(testDataInputs),
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	} {
		if err := taskRepo.Create(ctx, task); err != nil {
			t.Fatalf("Create(%s) error = %v", task.ID, err)
		}
	}

	timeoutDiagnostics := `{"result":"kfail","outputs":null,"errors":[{"code":"agent_run_failed","message":"Post \"https://ark.cn-beijing.volces.com/api/v3/chat/completions\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)"}]}`
	timeoutFeedback := core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    testDataID,
		ParentID:  taskIDPtr(parentID),
		AgentID:   "tester02",
		Op:        core.TaskOpTestData,
		Result:    core.TaskResultCodeFail,
		Commit: &core.CommitReceipt{
			Result:          core.TaskResultCodeFail,
			DiagnosticsJSON: timeoutDiagnostics,
		},
	}

	if err := service.OnFeedback(ctx, timeoutFeedback); err != nil {
		t.Fatalf("first timeout feedback error = %v", err)
	}

	task, err := taskRepo.Get(ctx, runID, testDataID)
	if err != nil {
		t.Fatalf("Get(test_data after retry) error = %v", err)
	}
	if task.Status != core.TaskStatusDispatched {
		t.Fatalf("test_data status after transient retry = %s, want dispatched", task.Status)
	}
	if task.ErrorMessage != "dynamic_transient_retry_count:1" {
		t.Fatalf("test_data error marker = %q, want single retry marker", task.ErrorMessage)
	}
	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run after retry) error = %v", err)
	}
	if run.Status != core.RunStatusRunning {
		t.Fatalf("run status after transient retry = %s, want running", run.Status)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("dispatch count after transient retry = %d, want 1", len(dispatcher.dispatched))
	}
	retryDispatch := dispatcher.dispatched[0]
	if retryDispatch.TaskID != testDataID || retryDispatch.Op != core.TaskOpTestData {
		t.Fatalf("retry dispatch = %+v, want tester02 test_data redispatch", retryDispatch)
	}
	for _, want := range testDataInputs {
		if !containsString(retryDispatch.ArtifactURIs, want) {
			t.Fatalf("retry dispatch artifacts = %v, missing %s", retryDispatch.ArtifactURIs, want)
		}
	}

	if err := service.OnFeedback(ctx, timeoutFeedback); err != nil {
		t.Fatalf("second timeout feedback error = %v", err)
	}

	task, err = taskRepo.Get(ctx, runID, testDataID)
	if err != nil {
		t.Fatalf("Get(test_data after second timeout) error = %v", err)
	}
	if task.Status != core.TaskStatusFailed {
		t.Fatalf("test_data status after second timeout = %s, want failed", task.Status)
	}
	run, err = runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run after second timeout) error = %v", err)
	}
	if run.Status != core.RunStatusFailed {
		t.Fatalf("run status after second timeout = %s, want failed", run.Status)
	}
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("dispatch count after second timeout = %d, want still 1", len(dispatcher.dispatched))
	}
}

func TestPhaseTwoStubFullFlowWithCoderTester(t *testing.T) {
	projectsRoot := t.TempDir()
	const runID core.RunID = "run_phase_two_stub_full"
	runPhaseTwoStubFlow(t, projectsRoot, runID)
}

func TestPhaseTwoStubFullFlowWritesEventsLog(t *testing.T) {
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	const runID core.RunID = "run_phase2_log_stub"
	runRoot := filepath.Join(projectsRoot, string(runID))
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}
	run := runPhaseTwoStubFlow(t, projectsRoot, runID)
	eventsLog := filepath.Join(run.ProjectDir, "events.log")
	if _, err := os.Stat(eventsLog); err != nil {
		t.Fatalf("events.log should exist: %v", err)
	}
}

func runPhaseTwoStubFlow(t *testing.T, projectsRoot string, runID core.RunID) core.PipelineRun {
	t.Helper()
	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := context.Background()

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, pipeline.PipelineIDPhaseTwo, noopRunConfig()); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := bootstrap.Modules.RunManager.StartRun(ctx, runID); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	session, err := bootstrap.Modules.SessionRuntime.GetSessionByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetSessionByRun() error = %v", err)
	}
	feedback, err := session.Agent.Execute(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("ceo_write_requirement execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("ceo_write_requirement feedback error = %v", err)
	}

	run := waitForRunStatus(t, ctx, bootstrap, runID, core.RunStatusAwaitingAcceptance)
	coderOutput := filepath.Join(run.ProjectDir, "agents", "coder01", "artifacts", "code", "coder01_code_v1.md")
	testerDataOutput := filepath.Join(run.ProjectDir, "agents", "tester01", "artifacts", "test", "tester01_test_data_v1.md")
	testerCodeOutput := filepath.Join(run.ProjectDir, "agents", "tester01", "artifacts", "test", "tester01_test_code_v1.md")
	mergeOutput := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "code", "merged_code_v1.md")
	globalTestDataOutput := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "test_data", "architect_test_data.md")
	globalTestOutput := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "test", "global_test_report_v1.md")
	deliveryConfigOutput := filepath.Join(run.ProjectDir, "system", "run_delivery_config.json")
	if _, err := os.Stat(coderOutput); err != nil {
		t.Fatalf("coder output should exist: %v", err)
	}
	if _, err := os.Stat(testerDataOutput); err != nil {
		t.Fatalf("tester data output should exist: %v", err)
	}
	if _, err := os.Stat(testerCodeOutput); err != nil {
		t.Fatalf("tester code output should exist: %v", err)
	}
	if _, err := os.Stat(mergeOutput); err != nil {
		t.Fatalf("merge output should exist: %v", err)
	}
	if _, err := os.Stat(globalTestDataOutput); err != nil {
		t.Fatalf("global test data output should exist: %v", err)
	}
	if _, err := os.Stat(globalTestOutput); err != nil {
		t.Fatalf("global test output should exist: %v", err)
	}
	deliveryConfigContent, err := os.ReadFile(deliveryConfigOutput)
	if err != nil {
		t.Fatalf("delivery config artifact should exist: %v", err)
	}
	if !strings.Contains(string(deliveryConfigContent), `"max_coder_agents": 2`) {
		t.Fatalf("delivery config artifact should include max coder agents: %s", string(deliveryConfigContent))
	}
	if !strings.Contains(string(deliveryConfigContent), `"max_tester_agents": 2`) {
		t.Fatalf("delivery config artifact should include max tester agents: %s", string(deliveryConfigContent))
	}
	if !strings.Contains(string(deliveryConfigContent), `"main_branch": "main"`) {
		t.Fatalf("delivery config artifact should include main branch: %s", string(deliveryConfigContent))
	}
	if strings.Contains(string(deliveryConfigContent), "api_key") || strings.Contains(string(deliveryConfigContent), "provider_type") {
		t.Fatalf("delivery config artifact should only expose delivery config: %s", string(deliveryConfigContent))
	}
	return run
}

func waitForFile(t *testing.T, fullPath string) {
	t.Helper()
	deadline := time.Now().Add(defaultWaitTimeout())
	for time.Now().Before(deadline) {
		if _, err := os.Stat(fullPath); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("file %s not found: %v", fullPath, err)
	}
}

func waitForTaskStatus(t *testing.T, ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, taskID core.TaskID, want core.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(defaultWaitTimeout())
	for time.Now().Before(deadline) {
		task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, taskID)
		if err == nil && task.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", taskID, err)
	}
	t.Fatalf("task %s status = %s, want %s", taskID, task.Status, want)
}

func waitForTaskStatusAny(t *testing.T, ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, taskID core.TaskID, want []core.TaskStatus) core.Task {
	t.Helper()
	deadline := time.Now().Add(extendedWaitTimeout())
	for time.Now().Before(deadline) {
		task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, taskID)
		if err == nil {
			for _, status := range want {
				if task.Status == status {
					return task
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := bootstrap.Internals.TaskRepository.Get(ctx, runID, taskID)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", taskID, err)
	}
	t.Fatalf("task %s status = %s, want one of %v", taskID, task.Status, want)
	return core.Task{}
}

func waitForRunStatus(t *testing.T, ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, want core.RunStatus) core.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(defaultWaitTimeout())
	for time.Now().Before(deadline) {
		run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
		if err == nil && run.Status == want {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	t.Fatalf("run status = %s, want %s", run.Status, want)
	return core.PipelineRun{}
}

func waitForRunStatusAny(t *testing.T, ctx context.Context, bootstrap *app.Bootstrap, runID core.RunID, want []core.RunStatus) core.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(extendedWaitTimeout())
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			logRunEventsOnTimeout(t, bootstrap, ctx, runID)
			t.Fatalf("run context ended before desired status %v: %v; if needed, rerun with a larger go test timeout such as `go test -timeout 30m ...`", want, err)
		}
		run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
		if err == nil {
			for _, status := range want {
				if run.Status == status {
					return run
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	logRunEventsOnTimeout(t, bootstrap, ctx, runID)
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	t.Fatalf("run status = %s, want one of %v", run.Status, want)
	return core.PipelineRun{}
}

func logRunEventsOnTimeout(t *testing.T, bootstrap *app.Bootstrap, ctx context.Context, runID core.RunID) {
	t.Helper()
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return
	}
	eventsPath := filepath.Join(run.ProjectDir, "events.log")
	if content, readErr := os.ReadFile(eventsPath); readErr == nil {
		t.Logf("events.log on timeout:\n%s", string(content))
	}
}

func uniqueRunID(prefix string) core.RunID {
	return core.RunID(fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()))
}

func defaultWaitTimeout() time.Duration {
	return 10 * time.Second
}

func extendedWaitTimeout() time.Duration {
	return 20 * time.Minute
}

func requireProtocolMockExtendedTest(t *testing.T) {
	t.Helper()
	if os.Getenv("DEVFLOW_PROTOCOLMOCK_EXTENDED_TEST") != "1" {
		t.Skip("set DEVFLOW_PROTOCOLMOCK_EXTENDED_TEST=1 to run extended protocolmock full-chain tests")
	}
}

func newExtendedTestContext(t *testing.T) context.Context {
	t.Helper()
	if deadline, ok := t.Deadline(); ok {
		buffered := deadline.Add(-15 * time.Second)
		if buffered.After(time.Now()) {
			ctx, _ := context.WithDeadline(context.Background(), buffered)
			return ctx
		}
	}
	ctx, _ := context.WithTimeout(context.Background(), extendedWaitTimeout())
	return ctx
}

func noopRunConfig() core.RunConfig {
	return core.RunConfig{
		Delivery: core.DeliveryConfig{
			MaxCoderAgents:         2,
			MaxTesterAgents:        2,
			RequireTesterPerModule: true,
			AllowParallelWork:      true,
			Git: core.GitRunConfig{
				MainBranch: "main",
			},
		},
	}
}

func extendedRunConfig(t *testing.T) core.RunConfig {
	t.Helper()
	return core.RunConfig{
		Delivery: core.DeliveryConfig{
			MaxCoderAgents:         2,
			MaxTesterAgents:        2,
			RequireTesterPerModule: true,
			AllowParallelWork:      true,
			Git: core.GitRunConfig{
				MainBranch: "main",
			},
		},
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func countDispatchOp(items []core.TaskMetaData, op string) int {
	count := 0
	for _, item := range items {
		if item.Op == op {
			count++
		}
	}
	return count
}

func writeProtocolMockForceBugRegistry(t *testing.T) string {
	t.Helper()
	spec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	spec = rewriteProtocolMockForceBugRegistry(spec)
	path := filepath.Join(t.TempDir(), "pipeline_protocolmock_force_bug.json")
	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(registry) error = %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func writeProtocolMockForceBugOnceRegistry(t *testing.T) string {
	t.Helper()
	spec, err := pipeline.LoadRegistrySpec(filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json"))
	if err != nil {
		t.Fatalf("LoadRegistrySpec() error = %v", err)
	}
	spec = rewriteProtocolMockForceBugOnceRegistry(spec)
	path := filepath.Join(t.TempDir(), "pipeline_protocolmock_force_bug_once.json")
	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(registry) error = %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func rewriteProtocolMockForceBugRegistry(spec pipeline.RegistrySpec) pipeline.RegistrySpec {
	for i := range spec.PipelineDefs {
		def := &spec.PipelineDefs[i]
		switch def.PipelineID {
		case "pipeline_test_code":
			rewriteTransitionID(def, "test_code", "test_code_force_bug")
		case "pipeline_full_delivery":
			rewriteControlTransitionID(def, "test_all_modules", "pipeline_test_code", "test_code_force_bug")
		}
	}
	return spec
}

func rewriteProtocolMockForceBugOnceRegistry(spec pipeline.RegistrySpec) pipeline.RegistrySpec {
	for i := range spec.PipelineDefs {
		def := &spec.PipelineDefs[i]
		if def.PipelineID == "pipeline_module" {
			rewriteTransitionID(def, "write_code", "write_code_force_bug_once")
		}
	}
	return spec
}

func rewriteTransitionID(def *pipeline.PipelineDefSpec, fromID string, toID string) {
	for i := range def.Transitions {
		if def.Transitions[i].ID == fromID {
			def.Transitions[i].ID = toID
		}
	}
	for i := range def.States {
		state := &def.States[i]
		if state.Proof.Transition == fromID {
			state.Proof.Transition = toID
		}
		for j := range state.Exposes.Bags {
			if state.Exposes.Bags[j].FromTransition == fromID {
				state.Exposes.Bags[j].FromTransition = toID
			}
		}
		if state.Next != nil {
			if state.Next.SourceTransition == fromID {
				state.Next.SourceTransition = toID
			}
			for j, transitionID := range state.Next.Transitions {
				if transitionID == fromID {
					state.Next.Transitions[j] = toID
				}
			}
		}
	}
}

func rewriteControlTransitionID(def *pipeline.PipelineDefSpec, controlID string, pipelineID core.PipelineID, toTransitionID string) {
	for i := range def.Transitions {
		transition := &def.Transitions[i]
		if transition.Control == nil {
			continue
		}
		if transition.Control.TransitionID == controlID && transition.Control.PipelineID == pipelineID {
			transition.Control.TransitionID = toTransitionID
		}
	}
}

func TestResumeFromRefMaterializesCompletedRun(t *testing.T) {
	ctx := context.Background()
	runRepo := repo.NewMemoryRunRepository()
	taskRepo := repo.NewMemoryTaskRepository()
	instanceRepo := repo.NewMemoryPipelineInstanceRepository()
	artifactRepo := repo.NewMemoryArtifactRepository()
	doujiaGitRepo := doujiagit.NewMemoryRepository()
	service := orchestrator.NewService(
		pipeline.NewMemoryRegistry(pipeline.BuiltinPhaseTwo()),
		runRepo,
		taskRepo,
		nil,
		&recordingDispatcher{},
		&recordingSessionRuntime{},
		nil,
	)
	service.SetPipelineInstanceRepository(instanceRepo)
	service.SetArtifactRepository(artifactRepo)
	service.SetDoujiaGitRepository(doujiaGitRepo)

	const runID core.RunID = "run_resume_from_ref"
	now := time.Now().UTC()
	if err := runRepo.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipeline.PipelineIDPhaseTwo,
		Status:     core.RunStatusCreated,
		ProjectDir: t.TempDir(),
		Config:     noopRunConfig(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	versionID := createDoujiaGitVersion(t, ctx, doujiaGitRepo, runID, "architect01", "global_test_report")
	snapshotID := doujiagit.StableSnapshotID(runID, "task_06_global_test_code", now.Add(time.Second))
	bagID := doujiagit.StableBagID(runID, snapshotID, "global_test_report")
	if err := doujiaGitRepo.CreateBag(ctx, doujiagit.ArtifactBag{
		BagID:              bagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{versionID},
		CreatedAt:          now.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}
	if err := doujiaGitRepo.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:         snapshotID,
		RunID:              runID,
		TaskID:             "task_06_global_test_code",
		PipelineInstanceID: "root",
		TransitionID:       "task_06_global_test_code",
		AgentRole:          core.AgentRoleArchitect,
		AgentID:            "architect01",
		Op:                 core.TaskOpTestCode,
		Result:             core.TaskResultCodeOK,
		OutputBagIDs:       []string{bagID},
		RuntimeContextJSON: `{"task":{"pipeline_instance_id":"root","transition_id":"task_06_global_test_code","agent_role":"architect","agent_id":"architect01","op":"test_code"},"pipeline_instance":{"id":"root","run_id":"run_resume_from_ref","pipeline_id":"phase_two_delivery_flow","instance_key":"root","status":"completed","agent_bindings":{"architect":"architect01"},"output_bag_ids":{"global_test_report":"` + bagID + `"},"output_bag_id_lists":{"global_test_report":["` + bagID + `"]},"created_at":"` + now.Format(time.RFC3339Nano) + `","updated_at":"` + now.Add(2*time.Second).Format(time.RFC3339Nano) + `"}}`,
		CreatedAt:          now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	frontierID := doujiagit.StableFrontierSnapshotID(runID, []string{snapshotID}, nil, now.Add(3*time.Second))
	if err := doujiaGitRepo.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID: frontierID,
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshotID},
		CreatedByMode:      doujiagit.RefMoveModeAdvance,
		CreatedAt:          now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := doujiaGitRepo.UpdateRef(ctx, doujiagit.Ref{
		RefName:            doujiagit.DefaultRefName,
		RunID:              runID,
		FrontierSnapshotID: frontierID,
		UpdatedAt:          now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}

	result, err := service.ResumeFromRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		t.Fatalf("ResumeFromRef() error = %v", err)
	}
	if result.MaterializedTasks != 1 || result.MaterializedInstances != 1 || result.RunStatus != core.RunStatusAwaitingAcceptance {
		t.Fatalf("ResumeFromRef() = %#v, want one task and one instance materialized into awaiting_acceptance run", result)
	}
	run, err := runRepo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		t.Fatalf("run status = %s, want awaiting_acceptance", run.Status)
	}
	task, err := taskRepo.Get(ctx, runID, "task_06_global_test_code")
	if err != nil {
		t.Fatalf("Get(materialized task) error = %v", err)
	}
	if task.Status != core.TaskStatusDone || task.AgentID != "architect01" || task.Op != core.TaskOpTestCode {
		t.Fatalf("materialized task = %#v", task)
	}
	if task.PipelineInstanceID != "root" || task.StageID != "task_06_global_test_code" {
		t.Fatalf("materialized task instance/stage = %s/%s", task.PipelineInstanceID, task.StageID)
	}
	root, err := instanceRepo.Get(ctx, runID, "root")
	if err != nil {
		t.Fatalf("Get(root instance) error = %v", err)
	}
	if root.Status != core.PipelineInstanceStatusCompleted || root.OutputBagIDs["global_test_report"] != bagID {
		t.Fatalf("root instance = %#v, want completed with global_test_report %s", root, bagID)
	}
	if got := uniqueTestStrings(task.OutputBagIDs); !sameTestStringSet(got, []string{bagID}) {
		t.Fatalf("task output bags = %v, want %s", got, bagID)
	}
	if got := artifactRefsToStringsForTest(task.OutputArtifactRefs); !sameTestStringSet(got, []string{"objects/architect01/global_test_report"}) {
		t.Fatalf("task output artifact refs = %v", got)
	}
	records, err := artifactRepo.ListByTask(ctx, runID, task.ID)
	if err != nil {
		t.Fatalf("ListByTask() error = %v", err)
	}
	if len(records) != 1 || records[0].URI != "objects/architect01/global_test_report" {
		t.Fatalf("artifact records = %#v", records)
	}
}

func createDoujiaGitVersion(t *testing.T, ctx context.Context, repository doujiagit.Repository, runID core.RunID, namespace string, logicalKey string) string {
	t.Helper()
	object, err := repository.UpsertObject(ctx, doujiagit.ArtifactObject{
		ObjectID:   doujiagit.StableObjectID([]byte(string(runID) + ":" + namespace + ":" + logicalKey)),
		ObjectType: doujiagit.ObjectTypeBlob,
		StorageURI: "objects/" + namespace + "/" + logicalKey,
	})
	if err != nil {
		t.Fatalf("UpsertObject(%s/%s) error = %v", namespace, logicalKey, err)
	}
	logical, err := repository.UpsertLogicalArtifact(ctx, doujiagit.LogicalArtifact{
		RunID:      runID,
		Namespace:  namespace,
		LogicalKey: logicalKey,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact(%s/%s) error = %v", namespace, logicalKey, err)
	}
	version, err := repository.UpsertArtifactVersion(ctx, doujiagit.ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion(%s/%s) error = %v", namespace, logicalKey, err)
	}
	return version.ArtifactVersionID
}

func sameTestStringSet(left []string, right []string) bool {
	left = uniqueTestStrings(left)
	right = uniqueTestStrings(right)
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, item := range left {
		seen[item] = true
	}
	for _, item := range right {
		if !seen[item] {
			return false
		}
	}
	return true
}

func uniqueTestStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func hasEventType(items []repo.EventRecord, eventType string) bool {
	for _, item := range items {
		if item.Type == eventType {
			return true
		}
	}
	return false
}

func taskIDPtr(id core.TaskID) *core.TaskID {
	return &id
}

func toRefs(items []string) []core.ArtifactRef {
	out := make([]core.ArtifactRef, 0, len(items))
	for _, item := range items {
		out = append(out, core.ArtifactRef(item))
	}
	return out
}

func artifactRefsToStringsForTest(items []core.ArtifactRef) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

type recordingProvisioner struct{}

func (recordingProvisioner) EnsureAgent(_ context.Context, req runtime.EnsureAgentRequest) (runtime.EnsureAgentResult, error) {
	return runtime.EnsureAgentResult{
		AgentID:       req.AgentID,
		RuntimeID:     core.RuntimeID("test_runtime"),
		WorkspacePath: filepath.Join(req.ProjectRoot, "agents", string(req.AgentID)),
	}, nil
}

type recordingDispatcher struct {
	dispatched []core.TaskMetaData
}

func (d *recordingDispatcher) Dispatch(_ context.Context, task core.TaskMetaData) error {
	d.dispatched = append(d.dispatched, task)
	return nil
}

type recordingSessionRuntime struct {
	dispatched []core.TaskMetaData
}

func (r *recordingSessionRuntime) CreateSession(_ context.Context, _ core.PipelineRun) (runtime.Session, error) {
	return runtime.Session{}, nil
}

func (r *recordingSessionRuntime) GetSessionByRun(_ context.Context, _ core.RunID) (runtime.Session, error) {
	return runtime.Session{}, nil
}

func (r *recordingSessionRuntime) DispatchToSession(_ context.Context, task core.TaskMetaData) error {
	r.dispatched = append(r.dispatched, task)
	return nil
}
