package orchestrator_test

import (
	"context"
	"devflow/internal/app"
	"devflow/internal/core"
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
