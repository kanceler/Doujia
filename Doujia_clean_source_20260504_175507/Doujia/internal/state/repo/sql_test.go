package repo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"devflow/internal/core"
)

func TestSQLiteRepositoriesRoundTrip(t *testing.T) {
	ctx := context.Background()
	repos, db, err := OpenSQLiteRepositories(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenSQLiteRepositories() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	run := RunRecord{
		ID:         "run_sql",
		PipelineID: "phase_two_delivery_flow",
		Status:     core.RunStatusRunning,
		ProjectDir: t.TempDir(),
		SessionID:  "session_sql",
		Config: core.RunConfig{Delivery: core.DeliveryConfig{
			MaxCoderAgents: 2,
			Git:            core.GitRunConfig{MainBranch: "main"},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repos.Runs.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	olderRun := run
	olderRun.ID = "run_sql_older"
	olderRun.CreatedAt = now.Add(-time.Hour)
	olderRun.UpdatedAt = now.Add(-time.Hour)
	if err := repos.Runs.Create(ctx, olderRun); err != nil {
		t.Fatalf("Create(older run) error = %v", err)
	}

	parentID := core.TaskID("task_01")
	task := TaskRecord{
		ID:                 "task_02",
		RunID:              run.ID,
		PipelineInstanceID: "root",
		StageID:            "task_02",
		Op:                 core.TaskOpWritePlan,
		AgentRole:          core.AgentRolePM,
		AgentID:            "pm01",
		Status:             core.TaskStatusDispatched,
		ParentID:           &parentID,
		DependsOnIDs:       []core.TaskID{parentID},
		InputArtifactRefs:  []core.ArtifactRef{"projects/run_sql/agents/ceo/artifacts/requirement/requirement_v1.md"},
		OutputArtifactRefs: []core.ArtifactRef{"projects/run_sql/agents/pm01/artifacts/prd/plan_v1.md"},
		InputBagIDs:        []string{"bag_requirement"},
		InputBags:          []core.BagBindingRef{{Name: "requirement", BagID: "bag_requirement"}},
		OutputBagIDs:       []string{"bag_plan"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := repos.Tasks.Create(ctx, task); err != nil {
		t.Fatalf("Create(task) error = %v", err)
	}
	rootInstance := PipelineInstanceRecord{
		ID:         "root",
		RunID:      run.ID,
		PipelineID: run.PipelineID,
		Status:     core.PipelineInstanceStatusRunning,
		Params: map[string]string{
			"request_id": "req01",
		},
		AgentBindings: map[string]core.AgentID{
			"architect": "architect01",
		},
		InputBagIDs: map[string]string{
			"requirement": "bag_requirement",
		},
		InputBagIDLists: map[string][]string{
			"requirement": {"bag_requirement"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repos.Instances.Create(ctx, rootInstance); err != nil {
		t.Fatalf("Create(root instance) error = %v", err)
	}
	parentInstanceID := rootInstance.ID
	childInstance := PipelineInstanceRecord{
		ID:                 "module01",
		RunID:              run.ID,
		PipelineID:         "pipeline_module",
		ParentID:           &parentInstanceID,
		ParentTransitionID: "test_all_modules",
		InstanceKey:        "module01",
		Status:             core.PipelineInstanceStatusCreated,
		Params: map[string]string{
			"module_key": "module01",
		},
		AgentBindings: map[string]core.AgentID{
			"architect": "architect01",
			"coder":     "coder01",
			"tester":    "tester01",
		},
		InputBagIDs: map[string]string{
			"module_input": "bag_module01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repos.Instances.Create(ctx, childInstance); err != nil {
		t.Fatalf("Create(child instance) error = %v", err)
	}
	childInstance.Status = core.PipelineInstanceStatusCompleted
	childInstance.OutputBagIDs = map[string]string{"tested_module": "bag_tested_module01"}
	childInstance.OutputBagIDLists = map[string][]string{"tested_module": {"bag_tested_module01"}}
	childInstance.UpdatedAt = now.Add(time.Second)
	if err := repos.Instances.Update(ctx, childInstance); err != nil {
		t.Fatalf("Update(child instance) error = %v", err)
	}
	task.Status = core.TaskStatusDone
	task.Result = core.TaskResultCodeOK
	task.UpdatedAt = now.Add(time.Second)
	if err := repos.Tasks.Update(ctx, task); err != nil {
		t.Fatalf("Update(task) error = %v", err)
	}
	if err := repos.Artifacts.Create(ctx, ArtifactRecord{
		ID:        "artifact_01",
		RunID:     run.ID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Kind:      "prd",
		URI:       string(task.OutputArtifactRefs[0]),
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("Create(artifact) error = %v", err)
	}
	if err := repos.Events.Create(ctx, EventRecord{
		ID:          "event_01",
		RunID:       run.ID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		Type:        "task_done",
		Message:     "task completed",
		PayloadJSON: `{"result":"kok"}`,
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("Create(event) error = %v", err)
	}

	gotRun, err := repos.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if gotRun.Config.Delivery.MaxCoderAgents != 2 || gotRun.Config.Delivery.Git.MainBranch != "main" {
		t.Fatalf("run config delivery = %#v, want persisted delivery config", gotRun.Config.Delivery)
	}
	gotRuns, err := repos.Runs.List(ctx)
	if err != nil {
		t.Fatalf("List(runs) error = %v", err)
	}
	if len(gotRuns) != 2 || gotRuns[0].ID != run.ID || gotRuns[1].ID != olderRun.ID {
		t.Fatalf("runs = %#v, want newest run first", gotRuns)
	}
	gotTasks, err := repos.Tasks.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(tasks) error = %v", err)
	}
	if len(gotTasks) != 1 || gotTasks[0].Result != core.TaskResultCodeOK {
		t.Fatalf("tasks = %#v, want one ok task", gotTasks)
	}
	if len(gotTasks[0].InputBagIDs) != 1 || gotTasks[0].InputBagIDs[0] != "bag_requirement" {
		t.Fatalf("task input bags = %v, want [bag_requirement]", gotTasks[0].InputBagIDs)
	}
	if len(gotTasks[0].InputBags) != 1 || gotTasks[0].InputBags[0].Name != "requirement" {
		t.Fatalf("task input bag bindings = %#v, want named requirement binding", gotTasks[0].InputBags)
	}
	if len(gotTasks[0].OutputBagIDs) != 1 || gotTasks[0].OutputBagIDs[0] != "bag_plan" {
		t.Fatalf("task output bags = %v, want [bag_plan]", gotTasks[0].OutputBagIDs)
	}
	if gotTasks[0].PipelineInstanceID != "root" {
		t.Fatalf("task pipeline instance id = %q, want root", gotTasks[0].PipelineInstanceID)
	}
	gotInstances, err := repos.Instances.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(instances) error = %v", err)
	}
	if len(gotInstances) != 2 {
		t.Fatalf("pipeline instances = %#v, want root and child", gotInstances)
	}
	gotChild, err := repos.Instances.Get(ctx, run.ID, childInstance.ID)
	if err != nil {
		t.Fatalf("Get(child instance) error = %v", err)
	}
	if gotChild.Status != core.PipelineInstanceStatusCompleted || gotChild.AgentBindings["coder"] != "coder01" {
		t.Fatalf("child instance = %#v, want completed coder01", gotChild)
	}
	if gotChild.OutputBagIDs["tested_module"] != "bag_tested_module01" {
		t.Fatalf("child output bags = %v, want tested module bag", gotChild.OutputBagIDs)
	}
	if gotChild.OutputBagIDLists["tested_module"][0] != "bag_tested_module01" {
		t.Fatalf("child output bag lists = %v, want tested module bag", gotChild.OutputBagIDLists)
	}
	gotChildren, err := repos.Instances.ListChildren(ctx, run.ID, rootInstance.ID)
	if err != nil {
		t.Fatalf("ListChildren(instances) error = %v", err)
	}
	if len(gotChildren) != 1 || gotChildren[0].ID != childInstance.ID {
		t.Fatalf("children = %#v, want module01", gotChildren)
	}
	gotArtifacts, err := repos.Artifacts.ListByTask(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("ListByTask(artifacts) error = %v", err)
	}
	if len(gotArtifacts) != 1 || gotArtifacts[0].URI != string(task.OutputArtifactRefs[0]) {
		t.Fatalf("artifacts = %#v", gotArtifacts)
	}
	gotEvents, err := repos.Events.ListByTask(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("ListByTask(events) error = %v", err)
	}
	if len(gotEvents) != 1 || gotEvents[0].Type != "task_done" {
		t.Fatalf("events = %#v", gotEvents)
	}
}

func TestSQLiteRepositoriesSupportIterativeSessionData(t *testing.T) {
	ctx := context.Background()
	repos, db, err := OpenSQLiteRepositories(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenSQLiteRepositories() error = %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	run := RunRecord{
		ID:                       "run_iterative_sql",
		PipelineID:               "phase_two_delivery_flow",
		Status:                   core.RunStatusAwaitingAcceptance,
		ProjectDir:               t.TempDir(),
		SessionID:                "session_iterative_sql",
		CurrentIterationNo:       2,
		LatestDeliveryFrontierID: "frontier_02",
		LatestAcceptanceCheckpointTaskID: "task_accept",
		Config: core.RunConfig{Delivery: core.DeliveryConfig{
			MaxCoderAgents: 2,
			Git:            core.GitRunConfig{MainBranch: "main"},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repos.Runs.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	iteration := RunIterationRecord{
		RunID:                     run.ID,
		IterationNo:               2,
		StartFrontierID:           "frontier_01",
		DeliveryFrontierID:        "frontier_02",
		AcceptanceCheckpointTaskID: "task_accept",
		Status:                    core.RunStatusAwaitingAcceptance,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if err := repos.Iterations.Create(ctx, iteration); err != nil {
		t.Fatalf("Create(iteration) error = %v", err)
	}

	if err := repos.SessionMessages.Create(ctx, SessionMessageRecord{
		ID:         "message_01",
		RunID:      run.ID,
		IterationNo: 2,
		Role:       "ceo",
		MessageType: "chat",
		Content:    "please add more validation",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(session message) error = %v", err)
	}
	if err := repos.SessionArtifacts.Create(ctx, SessionArtifactRecord{
		ID:         "artifact_01",
		RunID:      run.ID,
		IterationNo: 2,
		Kind:       "requirement_doc",
		Title:      "Requirement Doc",
		Content:    "iterate once more",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(session artifact) error = %v", err)
	}
	if err := repos.ImprovementItems.Create(ctx, ImprovementItemRecord{
		ItemID:     "item_01",
		RunID:      run.ID,
		IterationNo: 2,
		Title:      "Add validation",
		Detail:     "show validation before continuing",
		Source:     "ceo_summary",
		Status:     "open",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Create(improvement item) error = %v", err)
	}

	gotRun, err := repos.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get(run) error = %v", err)
	}
	if gotRun.Status != core.RunStatusAwaitingAcceptance || gotRun.CurrentIterationNo != 2 || gotRun.LatestDeliveryFrontierID != "frontier_02" || gotRun.LatestAcceptanceCheckpointTaskID != "task_accept" {
		t.Fatalf("run = %#v, want iterative fields persisted", gotRun)
	}

	gotIterations, err := repos.Iterations.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(iterations) error = %v", err)
	}
	if len(gotIterations) != 1 || gotIterations[0].IterationNo != 2 || gotIterations[0].DeliveryFrontierID != "frontier_02" {
		t.Fatalf("iterations = %#v, want iteration 2", gotIterations)
	}

	gotMessages, err := repos.SessionMessages.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(session messages) error = %v", err)
	}
	if len(gotMessages) != 1 || gotMessages[0].Content != "please add more validation" {
		t.Fatalf("session messages = %#v", gotMessages)
	}

	gotArtifacts, err := repos.SessionArtifacts.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(session artifacts) error = %v", err)
	}
	if len(gotArtifacts) != 1 || gotArtifacts[0].Kind != "requirement_doc" {
		t.Fatalf("session artifacts = %#v", gotArtifacts)
	}

	gotItems, err := repos.ImprovementItems.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun(improvement items) error = %v", err)
	}
	if len(gotItems) != 1 || gotItems[0].Title != "Add validation" {
		t.Fatalf("improvement items = %#v", gotItems)
	}
}
