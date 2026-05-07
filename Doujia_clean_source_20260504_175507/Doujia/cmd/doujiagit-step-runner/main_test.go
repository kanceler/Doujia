package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devflow/internal/core"
)

func TestStepRunnerInitNextAgentApply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "step.db")
	projectsRoot := filepath.Join(tmp, "workspaces")
	registryPath := filepath.Join("..", "..", "docs", "v2", "pipeline_full_delivery.spec.json")
	baseArgs := []string{
		"-db", dbPath,
		"-projects", projectsRoot,
		"-run", "run_step_cli",
		"-pipeline-registry", registryPath,
		"-pipeline", "pipeline_full_delivery",
		"-agent-mode", "protocolmock",
	}

	var initOut bytes.Buffer
	if err := runWithArgs(ctx, append([]string{"init"}, append(baseArgs, "-reset", "-requirement", "step runner requirement")...), &initOut); err != nil {
		t.Fatalf("init error = %v\n%s", err, initOut.String())
	}
	initSummary := decodeJSONOutput[stepSummary](t, initOut.Bytes())
	if initSummary.RunID != "run_step_cli" {
		t.Fatalf("init run_id = %q, want run_step_cli", initSummary.RunID)
	}
	if initSummary.NextTask == nil || initSummary.NextTask.Status != core.TaskStatusDispatched {
		t.Fatalf("init next task = %+v, want dispatched task", initSummary.NextTask)
	}
	if initSummary.NextTask.Op != core.TaskOpWritePlan {
		t.Fatalf("init next op = %q, want write_plan", initSummary.NextTask.Op)
	}

	var nextOut bytes.Buffer
	if err := runWithArgs(ctx, append([]string{"next"}, baseArgs...), &nextOut); err != nil {
		t.Fatalf("next error = %v\n%s", err, nextOut.String())
	}
	nextSummary := decodeJSONOutput[stepSummary](t, nextOut.Bytes())
	if nextSummary.NextTask == nil || nextSummary.NextTask.TaskID != initSummary.NextTask.TaskID {
		t.Fatalf("next task = %+v, want %s", nextSummary.NextTask, initSummary.NextTask.TaskID)
	}

	feedbackPath := filepath.Join(tmp, "pm_feedback.json")
	var agentOut bytes.Buffer
	agentArgs := append([]string{"agent"}, append(baseArgs, "-task", string(nextSummary.NextTask.TaskID), "-feedback", feedbackPath)...)
	if err := runWithArgs(ctx, agentArgs, &agentOut); err != nil {
		t.Fatalf("agent error = %v\n%s", err, agentOut.String())
	}
	agentSummary := decodeJSONOutput[agentStepSummary](t, agentOut.Bytes())
	if agentSummary.FeedbackPath != feedbackPath {
		t.Fatalf("feedback path = %q, want %q", agentSummary.FeedbackPath, feedbackPath)
	}
	if agentSummary.Feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("agent feedback result = %q, want kok", agentSummary.Feedback.Result)
	}
	if _, err := os.Stat(feedbackPath); err != nil {
		t.Fatalf("feedback file was not written: %v", err)
	}

	var stillNextOut bytes.Buffer
	if err := runWithArgs(ctx, append([]string{"next"}, baseArgs...), &stillNextOut); err != nil {
		t.Fatalf("next after agent error = %v\n%s", err, stillNextOut.String())
	}
	stillNext := decodeJSONOutput[stepSummary](t, stillNextOut.Bytes())
	if stillNext.NextTask == nil || stillNext.NextTask.TaskID != nextSummary.NextTask.TaskID {
		t.Fatalf("agent command should not apply feedback; next = %+v, want same task %s", stillNext.NextTask, nextSummary.NextTask.TaskID)
	}

	var applyOut bytes.Buffer
	applyArgs := append([]string{"apply"}, append(baseArgs, "-feedback", feedbackPath)...)
	if err := runWithArgs(ctx, applyArgs, &applyOut); err != nil {
		t.Fatalf("apply error = %v\n%s", err, applyOut.String())
	}
	applySummary := decodeJSONOutput[stepSummary](t, applyOut.Bytes())
	if applySummary.AppliedTaskID != nextSummary.NextTask.TaskID {
		t.Fatalf("applied task = %q, want %q", applySummary.AppliedTaskID, nextSummary.NextTask.TaskID)
	}
	if applySummary.NextTask == nil {
		t.Fatal("apply next task = nil, want following task")
	}
	if applySummary.NextTask.TaskID == nextSummary.NextTask.TaskID {
		t.Fatalf("apply next task = %q, want a new task", applySummary.NextTask.TaskID)
	}
	if applySummary.NextTask.Status != core.TaskStatusDispatched {
		t.Fatalf("apply next status = %q, want dispatched", applySummary.NextTask.Status)
	}

	ceoFeedbackPath := filepath.Join(tmp, "ceo_feedback.json")
	var ceoAgentOut bytes.Buffer
	ceoAgentArgs := append([]string{"agent"}, append(baseArgs, "-task", string(applySummary.NextTask.TaskID), "-feedback", ceoFeedbackPath)...)
	if err := runWithArgs(ctx, ceoAgentArgs, &ceoAgentOut); err != nil {
		t.Fatalf("ceo agent error = %v\n%s", err, ceoAgentOut.String())
	}
	ceoSummary := decodeJSONOutput[agentStepSummary](t, ceoAgentOut.Bytes())
	if ceoSummary.Feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("ceo feedback result = %q, want kok", ceoSummary.Feedback.Result)
	}
}

func decodeJSONOutput[T any](t *testing.T, body []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode JSON output error = %v\n%s", err, string(body))
	}
	return out
}
