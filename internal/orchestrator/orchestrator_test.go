package orchestrator_test

import (
	"context"
	"devflow/internal/app"
	"devflow/internal/core"
	"os"
	"path"
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
	if err := bootstrap.Modules.RunManager.CreateRun(ctx, runID, "phase_one_requirement_flow"); err != nil {
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

	waitForTaskStatus(t, ctx, bootstrap, "task_03", core.TaskStatusWaitingExternal)
	pmPlan := filepath.Join(pmWorkspace, "artifacts", "prd", "plan_v1.md")
	if _, err := os.Stat(pmPlan); err != nil {
		t.Fatalf("pm plan artifact should exist: %v", err)
	}
	pmPlanURI := path.Join("projects", string(runID), "agents", "pm01", "artifacts", "prd", "plan_v1.md")

	feedback, err = session.Agent.Execute(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        runID,
		TaskID:       "task_03",
		AgentID:      "ceo",
		Op:           "ceo_review_plan",
		ArtifactURIs: []string{pmPlanURI},
	})
	if err != nil {
		t.Fatalf("task_03 execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("task_03 feedback error = %v", err)
	}

	waitForTaskStatus(t, ctx, bootstrap, "task_06", core.TaskStatusWaitingExternal)

	architectDesign := filepath.Join(run.ProjectDir, "agents", "architect01", "artifacts", "design", "architecture_v1.md")
	if _, err := os.Stat(architectDesign); err != nil {
		t.Fatalf("architect design artifact should exist: %v", err)
	}
	architectDesignURI := path.Join("projects", string(runID), "agents", "architect01", "artifacts", "design", "architecture_v1.md")
	feedback, err = session.Agent.Execute(ctx, core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        runID,
		TaskID:       "task_06",
		AgentID:      "ceo",
		Op:           "ceo_user_confirm",
		ArtifactURIs: []string{architectDesignURI},
	})
	if err != nil {
		t.Fatalf("task_06 execute error = %v", err)
	}
	if err := bootstrap.Internals.Orchestrator.OnFeedback(ctx, feedback); err != nil {
		t.Fatalf("task_06 feedback error = %v", err)
	}

	run, err = bootstrap.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %s, want %s", run.Status, core.RunStatusCompleted)
	}
}

func waitForTaskStatus(t *testing.T, ctx context.Context, bootstrap *app.Bootstrap, taskID core.TaskID, want core.TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout())
	for time.Now().Before(deadline) {
		task, err := bootstrap.Internals.TaskRepository.Get(ctx, taskID)
		if err == nil && task.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := bootstrap.Internals.TaskRepository.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", taskID, err)
	}
	t.Fatalf("task %s status = %s, want %s", taskID, task.Status, want)
}

func testWaitTimeout() time.Duration {
	if hasLLMTestConfig() {
		return 120 * time.Second
	}
	return 2 * time.Second
}

func hasLLMTestConfig() bool {
	return os.Getenv("LLM_PROVIDER") != "" ||
		os.Getenv("LLM_API_KEY") != "" ||
		os.Getenv("LLM_MODEL") != ""
}
