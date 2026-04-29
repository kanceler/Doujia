package orchestrator_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devflow/internal/app"
	"devflow/internal/core"
	"devflow/internal/orchestrator"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

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

	run = waitForRunStatus(t, ctx, bootstrap, runID, core.RunStatusCompleted)
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusCompleted)
	}
}

func TestPhaseOneFlowWithExternalCEOFeedbackLLM(t *testing.T) {
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runRoot := filepath.Join(projectsRoot, "run_llm_002")
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}

	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := newLLMTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	const runID core.RunID = "run_llm_002"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow", realLLMRunConfig(t)); err != nil {
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
	pmPlan := filepath.Join(projectsRoot, string(runID), "agents", "pm01", "artifacts", "prd", "plan_v1.md")
	if _, err := os.Stat(pmPlan); err != nil {
		t.Fatalf("pm plan artifact should exist after llm success: %v", err)
	}
	architectDesign := filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "design", "architecture_v1.md")
	if _, err := os.Stat(architectDesign); err != nil {
		t.Fatalf("architect design artifact should exist after llm success: %v", err)
	}
}

func TestPhaseTwoFlowWithExternalCEOFeedbackLLM(t *testing.T) {
	projectsRoot := filepath.Clean(filepath.Join("..", "..", "runtime", "workspaces"))
	runID := uniqueRunID("run_phase_two_llm")
	runRoot := filepath.Join(projectsRoot, string(runID))
	if err := os.RemoveAll(runRoot); err != nil {
		t.Fatalf("RemoveAll(runRoot) error = %v", err)
	}
	t.Logf("run artifacts: %s", runRoot)

	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := newLLMTestContext(t)
	bootstrap.Modules.TaskRuntime.SetExecutionContext(ctx)
	if sessionRuntime, ok := bootstrap.Modules.SessionRuntime.(*runtime.InMemorySessionRuntime); ok {
		sessionRuntime.SetExecutionContext(ctx)
	}

	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, pipeline.PipelineIDPhaseTwo, realLLMRunConfig(t)); err != nil {
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
		Op:        core.TaskOpWritePlan,
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
	wantFiles := []string{
		filepath.Join(projectsRoot, string(runID), "agents", "pm01", "artifacts", "prd", "plan_v1.md"),
		filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "design", "architecture_v1.md"),
		filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "branches", "main_branch.md"),
	}
	for _, wantFile := range wantFiles {
		if _, err := os.Stat(wantFile); err != nil {
			t.Fatalf("expected artifact should exist after llm success: %s (%v)", wantFile, err)
		}
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
			DependsOn:         &parentID,
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
			DependsOn:          &parentID,
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
			DependsOn:    &testDataID,
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
		{ID: coderID, RunID: runID, StageID: core.StageID(coderID), AgentRole: core.AgentRoleCoder, AgentID: "coder01", ParentID: &parentID, DependsOn: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusDone, InputArtifactRefs: toRefs(coderInputs), OutputArtifactRefs: toRefs([]string{coderBranch}), CreatedAt: now, UpdatedAt: now},
		{ID: testDataID, RunID: runID, StageID: core.StageID(testDataID), AgentRole: core.AgentRoleTester, AgentID: "tester01", ParentID: &parentID, DependsOn: &parentID, DependsOnIDs: []core.TaskID{parentID}, Status: core.TaskStatusDone, InputArtifactRefs: toRefs(testerInputs), OutputArtifactRefs: toRefs([]string{fullTests}), CreatedAt: now, UpdatedAt: now},
		{ID: testCodeID, RunID: runID, StageID: core.StageID(testCodeID), AgentRole: core.AgentRoleTester, AgentID: "tester01", ParentID: &parentID, DependsOn: &testDataID, DependsOnIDs: []core.TaskID{coderID, testDataID}, Status: core.TaskStatusDispatched, InputArtifactRefs: toRefs(append(append([]string{}, testerInputs...), coderBranch, fullTests)), CreatedAt: now, UpdatedAt: now},
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
		TaskID:    "task_01",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	})
	if err != nil {
		t.Fatalf("task_01 execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}

	run := waitForRunStatus(t, ctx, bootstrap, runID, core.RunStatusCompleted)
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
		t.Fatalf("delivery config artifact should not expose llm config: %s", string(deliveryConfigContent))
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
	deadline := time.Now().Add(llmWaitTimeout())
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
	deadline := time.Now().Add(llmWaitTimeout())
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
	return 2 * time.Second
}

func llmWaitTimeout() time.Duration {
	return 20 * time.Minute
}

func newLLMTestContext(t *testing.T) context.Context {
	t.Helper()
	if deadline, ok := t.Deadline(); ok {
		buffered := deadline.Add(-15 * time.Second)
		if buffered.After(time.Now()) {
			ctx, _ := context.WithDeadline(context.Background(), buffered)
			return ctx
		}
	}
	ctx, _ := context.WithTimeout(context.Background(), llmWaitTimeout())
	return ctx
}

func noopRunConfig() core.RunConfig {
	return core.RunConfig{
		LLM: core.LLMConfig{
			ProviderType: "noop",
		},
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

func realLLMRunConfig(t *testing.T) core.RunConfig {
	t.Helper()
	requestTimeout := 10 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("DEVFLOW_LLM_REQUEST_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("DEVFLOW_LLM_REQUEST_TIMEOUT must be a Go duration such as 180s or 10m: %v", err)
		}
		requestTimeout = parsed
	}
	return core.RunConfig{
		LLM: core.LLMConfig{
			ProviderType:   "openai_compatible",
			BaseURL:        envOrDefault("DEVFLOW_LLM_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"),
			APIKey:         envOrDefault("DEVFLOW_LLM_API_KEY", "ark-ef80e8dd-8a74-43c6-955a-e51f3e34b655-0b328"),
			Model:          envOrDefault("DEVFLOW_LLM_MODEL", "ep-20260423222531-dnqtj"),
			RequestTimeout: requestTimeout,
		},
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

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
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
