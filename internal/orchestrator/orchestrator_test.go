package orchestrator_test

import (
	"context"
	"devflow/internal/app"
	"devflow/internal/core"
	"devflow/internal/orchestrator"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if _, err := os.Stat(pmPlan); err != nil {
		t.Fatalf("pm plan artifact should exist: %v", err)
	}

	architectDesign := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "design", "architecture_v1.md")
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
	ctx := context.Background()

	const runID core.RunID = "run_llm_002"
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow", realLLMRunConfig()); err != nil {
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
	if run.Status == core.RunStatusCompleted {
		pmPlan := filepath.Join(projectsRoot, string(runID), "agents", "pm01", "artifacts", "prd", "plan_v1.md")
		if _, err := os.Stat(pmPlan); err != nil {
			t.Fatalf("pm plan artifact should exist after llm success: %v", err)
		}
		architectDesign := filepath.Join(projectsRoot, string(runID), "agents", "architect01", "artifacts", "design", "architecture_v1.md")
		if _, err := os.Stat(architectDesign); err != nil {
			t.Fatalf("architect design artifact should exist after llm success: %v", err)
		}
		return
	}

	if run.Status != core.RunStatusFailed {
		t.Fatalf("run status = %s, want %s after llm failure", run.Status, core.RunStatusFailed)
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

func TestOnFeedbackUpstreamMissingBlocksTaskAndCreatesChildForUpstream(t *testing.T) {
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

	const runID core.RunID = "run_child_missing"
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
		ArtifactURIs: []string{"projects/run_child_missing/agents/ceo/artifacts/requirement/requirement_v1.md"},
		Result:       core.TaskResultCodeOK,
	}); err != nil {
		t.Fatalf("task_01 feedback error = %v", err)
	}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     runID,
		TaskID:    "task_02",
		AgentID:   "pm01",
		Op:        "pm_write_plan",
		Result:    core.TaskResultCodeUpstreamMissing,
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
}

func TestOnFeedbackReviewRejectBlocksTaskAndCreatesChildForUpstream(t *testing.T) {
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

	const runID core.RunID = "run_child_reject"
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
	task01Artifacts := []string{"projects/run_child_reject/agents/ceo/artifacts/requirement/requirement_v1.md"}
	task02Artifacts := []string{"projects/run_child_reject/agents/pm01/artifacts/prd/plan_v1.md"}
	task04Artifacts := []string{"projects/run_child_reject/agents/architect01/artifacts/design/architecture_v1.md"}

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
	reviewArtifacts := []string{"projects/run_child_reject/agents/pm01/artifacts/review/review_note_v1.md"}
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_05",
		AgentID:      "pm01",
		Op:           "pm_review_design",
		ArtifactURIs: reviewArtifacts,
		Result:       core.TaskResultCodeReviewReject,
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
	if len(last.ArtifactURIs) != 1 || last.ArtifactURIs[0] != reviewArtifacts[0] {
		t.Fatalf("child artifact uris = %v, want %v", last.ArtifactURIs, reviewArtifacts)
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

	moduleURI := "projects/run_phase_two_control/agents/architect01/artifacts/module/module01.md"
	if err := service.OnFeedback(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{"projects/run_phase_two_control/agents/architect01/artifacts/module/module_plan_v1.json"},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "coder01", ArtifactURIs: []string{moduleURI}},
			{Type: core.ControlTypeNewTester, AgentName: "tester01", ArtifactURIs: []string{moduleURI}},
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

	if len(dispatcher.dispatched) < 2 {
		t.Fatalf("dispatch count = %d, want at least 2", len(dispatcher.dispatched))
	}
	lastTwo := dispatcher.dispatched[len(dispatcher.dispatched)-2:]
	if lastTwo[0].Op != core.TaskOpWriteCode || lastTwo[1].Op != core.TaskOpTestData {
		t.Fatalf("last dynamic ops = %s, %s; want %s, %s", lastTwo[0].Op, lastTwo[1].Op, core.TaskOpWriteCode, core.TaskOpTestData)
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
}

func TestPhaseTwoStubFullFlowWithCoderTester(t *testing.T) {
	projectsRoot := t.TempDir()
	bootstrap := app.NewBootstrap(projectsRoot)
	ctx := context.Background()

	const runID core.RunID = "run_phase_two_stub_full"
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
	testerOutput := filepath.Join(run.ProjectDir, "agents", "tester01", "artifacts", "test", "tester01_test_v1.md")
	mergeOutput := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "code", "merged_code_v1.md")
	globalTestOutput := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "test", "global_test_report_v1.md")
	if _, err := os.Stat(coderOutput); err != nil {
		t.Fatalf("coder output should exist: %v", err)
	}
	if _, err := os.Stat(testerOutput); err != nil {
		t.Fatalf("tester output should exist: %v", err)
	}
	if _, err := os.Stat(mergeOutput); err != nil {
		t.Fatalf("merge output should exist: %v", err)
	}
	if _, err := os.Stat(globalTestOutput); err != nil {
		t.Fatalf("global test output should exist: %v", err)
	}
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
	run, err := bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	t.Fatalf("run status = %s, want one of %v", run.Status, want)
	return core.PipelineRun{}
}

func defaultWaitTimeout() time.Duration {
	return 2 * time.Second
}

func llmWaitTimeout() time.Duration {
	return 120 * time.Second
}

func noopRunConfig() core.RunConfig {
	return core.RunConfig{
		LLM: core.LLMConfig{
			ProviderType: "noop",
		},
	}
}

func realLLMRunConfig() core.RunConfig {
	return core.RunConfig{
		LLM: core.LLMConfig{
			ProviderType:   "openai_compatible",
			BaseURL:        "https://sub2.de5.net/v1",
			APIKey:         "gx-fed5baba3364a47754945f49ef13959e908c7d5a28943be772c01f72957737ea",
			Model:          "gpt-5.4",
			RequestTimeout: 180 * time.Second,
		},
	}
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
