package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

type Service struct {
	pipelines         pipeline.Registry
	runs              repo.RunRepository
	tasks             repo.TaskRepository
	provision         runtime.AgentProvisioner
	dispatcher        runtime.Dispatcher
	sessionDispatcher runtime.SessionRuntime
	logger            logging.RunLogger
}

func NewService(
	pipelines pipeline.Registry,
	runs repo.RunRepository,
	tasks repo.TaskRepository,
	provision runtime.AgentProvisioner,
	dispatcher runtime.Dispatcher,
	sessionDispatcher runtime.SessionRuntime,
	logger logging.RunLogger,
) *Service {
	return &Service{
		pipelines:         pipelines,
		runs:              runs,
		tasks:             tasks,
		provision:         provision,
		dispatcher:        dispatcher,
		sessionDispatcher: sessionDispatcher,
		logger:            logger,
	}
}

func (s *Service) Start(ctx context.Context, runID core.RunID) error {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	if len(spec.Stages) == 0 {
		return fmt.Errorf("pipeline %q has no stages", spec.ID)
	}
	first := spec.Stages[0]
	if s.logger != nil {
		_ = s.logger.Log(runID, "Orchestrator", fmt.Sprintf("start run with first stage=%s", first.ID))
	}
	return s.tasks.Create(ctx, core.Task{
		ID:        core.TaskID(first.ID),
		RunID:     runID,
		StageID:   first.ID,
		AgentRole: first.AgentRole,
		AgentID:   first.AgentAlias,
		Status:    core.TaskStatusWaitingExternal,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Service) OnFeedback(ctx context.Context, feedback core.TaskMetaData) error {
	if feedback.Direction != core.TaskDirectionFeedback {
		return fmt.Errorf("expected feedback metadata")
	}
	if s.logger != nil {
		_ = s.logger.LogTaskMeta(feedback.RunID, "Orchestrator", "feedback received", feedback)
	}

	task, err := s.tasks.Get(ctx, feedback.RunID, feedback.TaskID)
	if err != nil {
		return err
	}

	run, err := s.runs.Get(ctx, task.RunID)
	if err != nil {
		return err
	}

	if task.ParentID != nil {
		if isDynamicControlTask(task) {
			return s.handleDynamicTaskFeedback(ctx, run, task, feedback)
		}
		return s.handleChildFeedback(ctx, run, task, feedback)
	}

	switch feedback.Result {
	case core.TaskResultCodeOK:
		if len(feedback.Control) > 0 {
			if err := validateControls(feedback.Control); err != nil {
				task.Status = core.TaskStatusBlocked
				task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
				task.UpdatedAt = time.Now().UTC()
				if updateErr := s.tasks.Update(ctx, task); updateErr != nil {
					return updateErr
				}
				return s.createResplitTask(ctx, run, task, feedback, err)
			}
			if _, err := pairControlsByModule(feedback.Control); err != nil {
				task.Status = core.TaskStatusBlocked
				task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
				task.UpdatedAt = time.Now().UTC()
				if updateErr := s.tasks.Update(ctx, task); updateErr != nil {
					return updateErr
				}
				return s.createResplitTask(ctx, run, task, feedback, err)
			}
		}
		task.Status = core.TaskStatusDone
		task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
		task.UpdatedAt = time.Now().UTC()
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		if len(feedback.Control) > 0 {
			return s.expandControlTasks(ctx, run, task, feedback.Control)
		}
	case core.TaskResultCodeRewrite, core.TaskResultCodeReplan:
		task.Status = core.TaskStatusBlocked
		task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
		task.UpdatedAt = time.Now().UTC()
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		return s.createChildTask(ctx, run, task, feedback)
	case core.TaskResultCodeControlInvalid:
		task.Status = core.TaskStatusBlocked
		task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
		task.UpdatedAt = time.Now().UTC()
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		return s.createResplitTask(ctx, run, task, feedback, fmt.Errorf("agent reported invalid control"))
	default:
		task.Status = core.TaskStatusFailed
		task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
		task.UpdatedAt = time.Now().UTC()
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("run failed on task=%s", task.ID))
		}
		return s.runs.Update(ctx, run)
	}

	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	nextStage, ok := findNextStage(spec, task.StageID)
	if !ok {
		run.Status = core.RunStatusCompleted
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", "run completed")
		}
		return s.runs.Update(ctx, run)
	}

	nextTask := core.Task{
		ID:                core.TaskID(nextStage.ID),
		RunID:             run.ID,
		StageID:           nextStage.ID,
		AgentRole:         nextStage.AgentRole,
		AgentID:           nextStage.AgentAlias,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(feedback.ArtifactURIs),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if nextStage.DependsOn != nil {
		dep := core.TaskID(*nextStage.DependsOn)
		nextTask.DependsOn = &dep
	}

	if nextStage.External {
		nextTask.Status = core.TaskStatusWaitingExternal
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("next external task created: %s", nextTask.ID))
		}
		return s.tasks.Create(ctx, nextTask)
	}

	return s.dispatchTask(ctx, run, nextTask, nextStage.Op, feedback.ArtifactURIs)
}

func isDynamicControlTask(task core.Task) bool {
	return task.ParentID != nil && task.StageID == core.StageID(task.ID)
}

func (s *Service) handleDynamicTaskFeedback(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
	task.UpdatedAt = time.Now().UTC()
	if feedback.Result == core.TaskResultCodeBug && task.AgentRole == core.AgentRoleTester && feedback.Op == core.TaskOpTestCode {
		task.Status = core.TaskStatusBlocked
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		return s.createCoderDebugFromTestFailure(ctx, run, task, feedback)
	}
	if feedback.Result == core.TaskResultCodeBug && canRetryDynamicTask(task, feedback) {
		task.Status = core.TaskStatusBlocked
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		return s.createDynamicDebugTask(ctx, run, task, feedback)
	}
	if feedback.Result != core.TaskResultCodeOK {
		task.Status = core.TaskStatusFailed
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		return s.runs.Update(ctx, run)
	}
	task.Status = core.TaskStatusDone
	if err := s.tasks.Update(ctx, task); err != nil {
		return err
	}
	if isGlobalTestTask(task, feedback) {
		run.Status = core.RunStatusCompleted
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", "run completed after global test_code")
		}
		return s.runs.Update(ctx, run)
	}
	return s.advanceReadyTasks(ctx, run)
}

func isGlobalTestTask(task core.Task, feedback core.TaskMetaData) bool {
	return feedback.Op == core.TaskOpTestCode && task.AgentRole == core.AgentRoleArchitect
}

func canRetryDynamicTask(task core.Task, feedback core.TaskMetaData) bool {
	return task.AgentRole == core.AgentRoleCoder && feedback.Op == core.TaskOpWriteCode
}

func findNextStage(spec pipeline.PipelineSpec, current core.StageID) (pipeline.StageSpec, bool) {
	for i, stage := range spec.Stages {
		if stage.ID == current && i+1 < len(spec.Stages) {
			return spec.Stages[i+1], true
		}
	}
	return pipeline.StageSpec{}, false
}

func findStageByID(spec pipeline.PipelineSpec, id core.StageID) (pipeline.StageSpec, bool) {
	for _, stage := range spec.Stages {
		if stage.ID == id {
			return stage, true
		}
	}
	return pipeline.StageSpec{}, false
}

func (s *Service) handleChildFeedback(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
	task.UpdatedAt = time.Now().UTC()
	if feedback.Result != core.TaskResultCodeOK {
		task.Status = core.TaskStatusFailed
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("run failed on child task=%s", task.ID))
		}
		return s.runs.Update(ctx, run)
	}
	task.Status = core.TaskStatusDone
	if err := s.tasks.Update(ctx, task); err != nil {
		return err
	}

	parent, err := s.tasks.Get(ctx, run.ID, *task.ParentID)
	if err != nil {
		return err
	}
	if feedback.Op == core.TaskOpDebug && canRetryDynamicTask(parent, core.TaskMetaData{Op: core.TaskOpWriteCode}) {
		parent.Status = core.TaskStatusDone
		parent.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
		parent.UpdatedAt = time.Now().UTC()
		if err := s.tasks.Update(ctx, parent); err != nil {
			return err
		}
		return s.advanceReadyTasks(ctx, run)
	}
	if feedback.Op == core.TaskOpDebug && parent.AgentRole == core.AgentRoleTester && opForTask(parent) == core.TaskOpTestCode {
		parentInputs := mergeArtifactLists(
			artifactRefsToStrings(parent.InputArtifactRefs),
			artifactRefsToStrings(task.InputArtifactRefs),
			feedback.ArtifactURIs,
		)
		parent.InputArtifactRefs = toArtifactRefs(parentInputs)
		parent.UpdatedAt = time.Now().UTC()
		return s.dispatchTask(ctx, run, parent, core.TaskOpTestCode, parentInputs)
	}
	parentInputs := mergeArtifactLists(
		artifactRefsToStrings(parent.InputArtifactRefs),
		artifactRefsToStrings(task.InputArtifactRefs),
		feedback.ArtifactURIs,
	)
	parent.InputArtifactRefs = toArtifactRefs(parentInputs)

	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	parentStage, ok := findStageByID(spec, parent.StageID)
	if !ok {
		return fmt.Errorf("stage %q not found in pipeline %q", parent.StageID, run.PipelineID)
	}
	return s.dispatchTask(ctx, run, parent, parentStage.Op, parentInputs)
}

func (s *Service) createChildTask(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	if task.DependsOn == nil {
		return s.failRun(ctx, run, task.ID)
	}

	upstreamTask, err := s.tasks.Get(ctx, run.ID, *task.DependsOn)
	if err != nil {
		return err
	}
	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	if _, ok := findStageByID(spec, upstreamTask.StageID); !ok {
		return fmt.Errorf("stage %q not found in pipeline %q", upstreamTask.StageID, run.PipelineID)
	}

	childOp, inputArtifacts := childTaskPlan(task, upstreamTask, feedback)
	if childOp == "" {
		return s.failRun(ctx, run, task.ID)
	}

	childID, err := s.nextChildTaskID(ctx, run.ID, task.ID)
	if err != nil {
		return err
	}
	parentID := task.ID
	childTask := core.Task{
		ID:                childID,
		RunID:             run.ID,
		StageID:           upstreamTask.StageID,
		AgentRole:         upstreamTask.AgentRole,
		AgentID:           upstreamTask.AgentID,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	return s.dispatchTask(ctx, run, childTask, childOp, inputArtifacts)
}

func (s *Service) createResplitTask(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData, controlErr error) error {
	childID, err := s.nextChildTaskID(ctx, run.ID, task.ID)
	if err != nil {
		return err
	}
	parentID := task.ID
	inputArtifacts := append(artifactRefsToStrings(task.InputArtifactRefs), feedback.ArtifactURIs...)
	childTask := core.Task{
		ID:                childID,
		RunID:             run.ID,
		StageID:           task.StageID,
		AgentRole:         task.AgentRole,
		AgentID:           task.AgentID,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("control invalid on task=%s: %v", task.ID, controlErr))
	}
	return s.dispatchTask(ctx, run, childTask, core.TaskOpResplitModule, inputArtifacts)
}

func (s *Service) createDynamicDebugTask(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	childID, err := s.nextChildTaskID(ctx, run.ID, task.ID)
	if err != nil {
		return err
	}
	parentID := task.ID
	inputArtifacts := mergeArtifactLists(
		artifactRefsToStrings(task.InputArtifactRefs),
		feedback.ArtifactURIs,
	)
	childTask := core.Task{
		ID:                childID,
		RunID:             run.ID,
		StageID:           task.StageID,
		AgentRole:         task.AgentRole,
		AgentID:           task.AgentID,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	return s.dispatchTask(ctx, run, childTask, core.TaskOpDebug, inputArtifacts)
}

func (s *Service) createCoderDebugFromTestFailure(ctx context.Context, run core.PipelineRun, testCodeTask core.Task, feedback core.TaskMetaData) error {
	var coderTask core.Task
	found := false
	for _, depID := range taskDependencies(testCodeTask) {
		dep, err := s.tasks.Get(ctx, run.ID, depID)
		if err != nil {
			return err
		}
		if dep.AgentRole == core.AgentRoleCoder && opForTask(dep) == core.TaskOpWriteCode {
			coderTask = dep
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("coder dependency not found for test_code task %q", testCodeTask.ID)
	}
	childID, err := s.nextChildTaskID(ctx, run.ID, testCodeTask.ID)
	if err != nil {
		return err
	}
	parentID := testCodeTask.ID
	inputArtifacts := mergeArtifactLists(
		artifactRefsToStrings(coderTask.InputArtifactRefs),
		artifactRefsToStrings(coderTask.OutputArtifactRefs),
		artifactRefsToStrings(testCodeTask.InputArtifactRefs),
		feedback.ArtifactURIs,
	)
	childTask := core.Task{
		ID:                childID,
		RunID:             run.ID,
		StageID:           coderTask.StageID,
		AgentRole:         core.AgentRoleCoder,
		AgentID:           coderTask.AgentID,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	return s.dispatchTask(ctx, run, childTask, core.TaskOpDebug, inputArtifacts)
}

func (s *Service) expandControlTasks(ctx context.Context, run core.PipelineRun, sourceTask core.Task, controls []core.Control) error {
	plans, err := buildModuleTaskPlans(sourceTask, controls)
	if err != nil {
		return err
	}
	for _, task := range plans {
		if err := s.tasks.Create(ctx, task); err != nil {
			return err
		}
		if s.logger != nil && task.Status == core.TaskStatusPending {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("pending task created: task=%s op=%s deps=%s", task.ID, opForTask(task), formatTaskDeps(taskDependencies(task))))
		}
	}
	return s.advanceReadyTasks(ctx, run)
}

type modulePair struct {
	coder  core.Control
	tester core.Control
}

func buildModuleTaskPlans(sourceTask core.Task, controls []core.Control) ([]core.Task, error) {
	pairs, err := pairControlsByModule(controls)
	if err != nil {
		return nil, err
	}
	parentID := sourceTask.ID
	now := time.Now().UTC()
	tasks := make([]core.Task, 0, len(pairs)*3+3)
	testCodeIDs := make([]core.TaskID, 0, len(pairs))
	for _, pair := range pairs {
		coderID := dynamicTaskID(sourceTask.ID, pair.coder.AgentName, core.TaskOpWriteCode)
		testDataID := dynamicTaskID(sourceTask.ID, pair.tester.AgentName, core.TaskOpTestData)
		testCodeID := dynamicTaskID(sourceTask.ID, pair.tester.AgentName, core.TaskOpTestCode)
		tasks = append(tasks,
			core.Task{
				ID:                coderID,
				RunID:             sourceTask.RunID,
				StageID:           core.StageID(coderID),
				AgentRole:         core.AgentRoleCoder,
				AgentID:           core.AgentID(strings.TrimSpace(pair.coder.AgentName)),
				ParentID:          &parentID,
				DependsOn:         &parentID,
				DependsOnIDs:      []core.TaskID{parentID},
				Status:            core.TaskStatusPending,
				InputArtifactRefs: toArtifactRefs(pair.coder.ArtifactURIs),
				CreatedAt:         now,
				UpdatedAt:         now,
			},
			core.Task{
				ID:                testDataID,
				RunID:             sourceTask.RunID,
				StageID:           core.StageID(testDataID),
				AgentRole:         core.AgentRoleTester,
				AgentID:           core.AgentID(strings.TrimSpace(pair.tester.AgentName)),
				ParentID:          &parentID,
				DependsOn:         &parentID,
				DependsOnIDs:      []core.TaskID{parentID},
				Status:            core.TaskStatusPending,
				InputArtifactRefs: toArtifactRefs(pair.tester.ArtifactURIs),
				CreatedAt:         now,
				UpdatedAt:         now,
			},
			core.Task{
				ID:                testCodeID,
				RunID:             sourceTask.RunID,
				StageID:           core.StageID(testCodeID),
				AgentRole:         core.AgentRoleTester,
				AgentID:           core.AgentID(strings.TrimSpace(pair.tester.AgentName)),
				ParentID:          &parentID,
				DependsOn:         &testDataID,
				DependsOnIDs:      []core.TaskID{coderID, testDataID},
				Status:            core.TaskStatusPending,
				InputArtifactRefs: toArtifactRefs(pair.tester.ArtifactURIs),
				CreatedAt:         now,
				UpdatedAt:         now,
			},
		)
		testCodeIDs = append(testCodeIDs, testCodeID)
	}
	mergeID := core.TaskID(fmt.Sprintf("%s_merge_code", sourceTask.ID))
	globalTestDataID := core.TaskID(fmt.Sprintf("%s_global_test_data", sourceTask.ID))
	globalTestID := core.TaskID(fmt.Sprintf("%s_global_test_code", sourceTask.ID))
	tasks = append(tasks,
		core.Task{
			ID:           mergeID,
			RunID:        sourceTask.RunID,
			StageID:      core.StageID(mergeID),
			AgentRole:    core.AgentRoleArchitect,
			AgentID:      sourceTask.AgentID,
			ParentID:     &parentID,
			DependsOn:    lastTaskIDPtr(testCodeIDs),
			DependsOnIDs: testCodeIDs,
			Status:       core.TaskStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		core.Task{
			ID:           globalTestDataID,
			RunID:        sourceTask.RunID,
			StageID:      core.StageID(globalTestDataID),
			AgentRole:    core.AgentRoleArchitect,
			AgentID:      sourceTask.AgentID,
			ParentID:     &parentID,
			DependsOn:    &mergeID,
			DependsOnIDs: []core.TaskID{mergeID},
			Status:       core.TaskStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		core.Task{
			ID:           globalTestID,
			RunID:        sourceTask.RunID,
			StageID:      core.StageID(globalTestID),
			AgentRole:    core.AgentRoleArchitect,
			AgentID:      sourceTask.AgentID,
			ParentID:     &parentID,
			DependsOn:    &globalTestDataID,
			DependsOnIDs: []core.TaskID{mergeID, globalTestDataID},
			Status:       core.TaskStatusPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	)
	return tasks, nil
}

func pairControlsByModule(controls []core.Control) ([]modulePair, error) {
	byModule := make(map[string]*modulePair)
	order := make([]string, 0)
	for _, control := range controls {
		moduleURI := controlPairingKey(control)
		if _, ok := byModule[moduleURI]; !ok {
			byModule[moduleURI] = &modulePair{}
			order = append(order, moduleURI)
		}
		pair := byModule[moduleURI]
		switch control.Type {
		case core.ControlTypeNewCoder:
			if strings.TrimSpace(pair.coder.AgentName) != "" {
				return nil, fmt.Errorf("module %q has duplicate coder controls", moduleURI)
			}
			pair.coder = control
		case core.ControlTypeNewTester:
			if strings.TrimSpace(pair.tester.AgentName) != "" {
				return nil, fmt.Errorf("module %q has duplicate tester controls", moduleURI)
			}
			pair.tester = control
		}
	}
	pairs := make([]modulePair, 0, len(order))
	for _, moduleURI := range order {
		pair := byModule[moduleURI]
		if strings.TrimSpace(pair.coder.AgentName) == "" || strings.TrimSpace(pair.tester.AgentName) == "" {
			return nil, fmt.Errorf("module %q requires one coder and one tester", moduleURI)
		}
		pairs = append(pairs, *pair)
	}
	return pairs, nil
}

func controlPairingKey(control core.Control) string {
	if control.Type == core.ControlTypeNewTester && len(control.ArtifactURIs) > 1 {
		if key := strings.TrimSpace(control.ArtifactURIs[1]); key != "" {
			return key
		}
	}
	return strings.TrimSpace(control.ArtifactURIs[0])
}

func lastTaskIDPtr(items []core.TaskID) *core.TaskID {
	if len(items) == 0 {
		return nil
	}
	return &items[len(items)-1]
}

func (s *Service) advanceReadyTasks(ctx context.Context, run core.PipelineRun) error {
	for {
		items, err := s.tasks.ListByRun(ctx, run.ID)
		if err != nil {
			return err
		}
		byID := make(map[core.TaskID]core.Task, len(items))
		for _, item := range items {
			byID[item.ID] = item
		}
		dispatched := false
		for _, item := range items {
			if item.Status != core.TaskStatusPending || len(taskDependencies(item)) == 0 {
				continue
			}
			ready, inputArtifacts, err := readyTaskInputs(item, byID)
			if err != nil {
				return err
			}
			if !ready {
				continue
			}
			if len(inputArtifacts) == 0 {
				inputArtifacts = artifactRefsToStrings(item.InputArtifactRefs)
			}
			if s.logger != nil {
				_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("ready task unlocked: task=%s op=%s deps=%s inputs=%d", item.ID, opForTask(item), formatTaskDeps(taskDependencies(item)), len(inputArtifacts)))
			}
			if err := s.dispatchTask(ctx, run, item, opForTask(item), inputArtifacts); err != nil {
				return err
			}
			dispatched = true
		}
		if !dispatched {
			return nil
		}
	}
}

func taskDependencies(task core.Task) []core.TaskID {
	if len(task.DependsOnIDs) > 0 {
		return task.DependsOnIDs
	}
	if task.DependsOn != nil {
		return []core.TaskID{*task.DependsOn}
	}
	return nil
}

func formatTaskDeps(items []core.TaskID) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, string(item))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func readyTaskInputs(task core.Task, byID map[core.TaskID]core.Task) (bool, []string, error) {
	if len(task.InputArtifactRefs) > 0 {
		inputs := artifactRefsToStrings(task.InputArtifactRefs)
		for _, depID := range taskDependencies(task) {
			dep, ok := byID[depID]
			if !ok {
				return false, nil, fmt.Errorf("dependency task %q not found for task %q", depID, task.ID)
			}
			if dep.Status != core.TaskStatusDone {
				return false, nil, nil
			}
			inputs = append(inputs, artifactRefsToStrings(dep.OutputArtifactRefs)...)
		}
		return true, uniqueStrings(inputs), nil
	}
	if strings.Contains(string(task.ID), "_merge_code") {
		return readyMergeTaskInputs(task, byID)
	}
	if strings.Contains(string(task.ID), "_global_test_data") {
		return readyGlobalTestDataInputs(task, byID)
	}
	if strings.Contains(string(task.ID), "_global_test_code") {
		return readyGlobalTestInputs(task, byID)
	}
	inputs := make([]string, 0)
	for _, depID := range taskDependencies(task) {
		dep, ok := byID[depID]
		if !ok {
			return false, nil, fmt.Errorf("dependency task %q not found for task %q", depID, task.ID)
		}
		if dep.Status != core.TaskStatusDone {
			return false, nil, nil
		}
		inputs = append(inputs, artifactRefsToStrings(dep.OutputArtifactRefs)...)
	}
	return true, inputs, nil
}

func readyMergeTaskInputs(task core.Task, byID map[core.TaskID]core.Task) (bool, []string, error) {
	inputs := make([]string, 0)
	for _, depID := range taskDependencies(task) {
		dep, ok := byID[depID]
		if !ok {
			return false, nil, fmt.Errorf("dependency task %q not found for task %q", depID, task.ID)
		}
		if dep.Status != core.TaskStatusDone {
			return false, nil, nil
		}
		inputs = append(inputs, artifactRefsToStrings(dep.OutputArtifactRefs)...)
	}
	if task.ParentID == nil {
		return true, inputs, nil
	}
	if parent, ok := byID[*task.ParentID]; ok {
		inputs = append(inputs, artifactRefsToStrings(parent.InputArtifactRefs)...)
		inputs = append(inputs, artifactRefsToStrings(parent.OutputArtifactRefs)...)
	}
	for _, item := range byID {
		if item.ParentID == nil || *item.ParentID != *task.ParentID {
			continue
		}
		if item.AgentRole != core.AgentRoleCoder || !strings.Contains(string(item.ID), "_write_code") {
			continue
		}
		if item.Status != core.TaskStatusDone {
			return false, nil, nil
		}
		inputs = append(inputs, artifactRefsToStrings(item.InputArtifactRefs)...)
		inputs = append(inputs, artifactRefsToStrings(item.OutputArtifactRefs)...)
	}
	return true, uniqueStrings(inputs), nil
}

func readyGlobalTestDataInputs(task core.Task, byID map[core.TaskID]core.Task) (bool, []string, error) {
	inputs := make([]string, 0)
	for _, depID := range taskDependencies(task) {
		dep, ok := byID[depID]
		if !ok {
			return false, nil, fmt.Errorf("dependency task %q not found for task %q", depID, task.ID)
		}
		if dep.Status != core.TaskStatusDone {
			return false, nil, nil
		}
		inputs = append(inputs, artifactRefsToStrings(dep.InputArtifactRefs)...)
		inputs = append(inputs, artifactRefsToStrings(dep.OutputArtifactRefs)...)
	}
	if task.ParentID != nil {
		if parent, ok := byID[*task.ParentID]; ok {
			inputs = append(inputs, artifactRefsToStrings(parent.InputArtifactRefs)...)
			inputs = append(inputs, artifactRefsToStrings(parent.OutputArtifactRefs)...)
		}
	}
	return true, uniqueStrings(inputs), nil
}

func readyGlobalTestInputs(task core.Task, byID map[core.TaskID]core.Task) (bool, []string, error) {
	inputs := make([]string, 0)
	for _, depID := range taskDependencies(task) {
		dep, ok := byID[depID]
		if !ok {
			return false, nil, fmt.Errorf("dependency task %q not found for task %q", depID, task.ID)
		}
		if dep.Status != core.TaskStatusDone {
			return false, nil, nil
		}
		inputs = append(inputs, artifactRefsToStrings(dep.InputArtifactRefs)...)
		inputs = append(inputs, artifactRefsToStrings(dep.OutputArtifactRefs)...)
	}
	if task.ParentID != nil {
		if parent, ok := byID[*task.ParentID]; ok {
			inputs = append(inputs, artifactRefsToStrings(parent.InputArtifactRefs)...)
			inputs = append(inputs, artifactRefsToStrings(parent.OutputArtifactRefs)...)
		}
	}
	return true, uniqueStrings(inputs), nil
}

func opForTask(task core.Task) string {
	id := string(task.ID)
	switch {
	case strings.Contains(id, "_write_code"):
		return core.TaskOpWriteCode
	case strings.Contains(id, "_test_data"):
		return core.TaskOpTestData
	case strings.Contains(id, "_merge_code"):
		return core.TaskOpMergeCode
	case strings.Contains(id, "_global_test_code"), strings.Contains(id, "_test_code"):
		return core.TaskOpTestCode
	default:
		return ""
	}
}

func validateControls(controls []core.Control) error {
	for i, control := range controls {
		if control.Type != core.ControlTypeNewCoder && control.Type != core.ControlTypeNewTester {
			return fmt.Errorf("control[%d] has unsupported type %q", i, control.Type)
		}
		if strings.TrimSpace(control.AgentName) == "" {
			return fmt.Errorf("control[%d] agent_name is required", i)
		}
		if len(nonEmptyStrings(control.ArtifactURIs)) == 0 {
			return fmt.Errorf("control[%d] artifact_uris is required", i)
		}
	}
	return nil
}

func dynamicTaskID(sourceTaskID core.TaskID, agentName string, op string) core.TaskID {
	cleanAgent := sanitizeIDPart(agentName)
	cleanOp := sanitizeIDPart(op)
	return core.TaskID(fmt.Sprintf("%s_%s_%s", sourceTaskID, cleanAgent, cleanOp))
}

func sanitizeIDPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	out := strings.Trim(builder.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func nonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			out = append(out, item)
		}
	}
	return out
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool)
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

func mergeArtifactLists(groups ...[]string) []string {
	merged := make([]string, 0)
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return uniqueStrings(merged)
}

func childTaskPlan(task core.Task, upstreamTask core.Task, feedback core.TaskMetaData) (string, []string) {
	switch feedback.Result {
	case core.TaskResultCodeRewrite:
		inputs := mergeArtifactLists(
			artifactRefsToStrings(task.InputArtifactRefs),
			feedback.ArtifactURIs,
		)
		return core.TaskOpRewrite, inputs
	case core.TaskResultCodeReplan:
		inputs := mergeArtifactLists(
			artifactRefsToStrings(upstreamTask.InputArtifactRefs),
			artifactRefsToStrings(upstreamTask.OutputArtifactRefs),
			artifactRefsToStrings(task.InputArtifactRefs),
			artifactRefsToStrings(task.OutputArtifactRefs),
			feedback.ArtifactURIs,
		)
		return core.TaskOpReplan, inputs
	default:
		return "", nil
	}
}

func (s *Service) nextChildTaskID(ctx context.Context, runID core.RunID, parentID core.TaskID) (core.TaskID, error) {
	items, err := s.tasks.ListByRun(ctx, runID)
	if err != nil {
		return "", err
	}
	count := 0
	for _, item := range items {
		if item.ParentID != nil && *item.ParentID == parentID {
			count++
		}
	}
	return core.TaskID(fmt.Sprintf("%s_child_%02d", parentID, count+1)), nil
}

func (s *Service) dispatchTask(ctx context.Context, run core.PipelineRun, task core.Task, op string, artifactURIs []string) error {
	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	stage, ok := findStageByID(spec, task.StageID)
	if !ok && task.ParentID == nil {
		return fmt.Errorf("stage %q not found in pipeline %q", task.StageID, run.PipelineID)
	}

	if ok && stage.External && task.ParentID == nil {
		task.Status = core.TaskStatusWaitingExternal
		task.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("next external task created: %s", task.ID))
		}
		return s.upsertTask(ctx, task)
	}

	artifactURIs = appendDeliveryConfigForOp(run.ID, op, artifactURIs)
	task.InputArtifactRefs = toArtifactRefs(artifactURIs)
	task.Status = core.TaskStatusDispatched
	task.UpdatedAt = time.Now().UTC()
	if err := s.upsertTask(ctx, task); err != nil {
		return err
	}

	dispatch := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        run.ID,
		TaskID:       task.ID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      task.AgentID,
		Op:           op,
		ArtifactURIs: artifactURIs,
	}
	if s.logger != nil {
		_ = s.logger.LogTaskMeta(run.ID, "Orchestrator", "dispatch created", dispatch)
	}

	if task.AgentRole == core.AgentRoleCEO && s.sessionDispatcher != nil {
		return s.sessionDispatcher.DispatchToSession(ctx, dispatch)
	}

	if _, err := s.provision.EnsureAgent(ctx, runtime.EnsureAgentRequest{
		RunID:       run.ID,
		Role:        task.AgentRole,
		AgentID:     task.AgentID,
		ProjectRoot: run.ProjectDir,
		RunConfig:   run.Config,
	}); err != nil {
		return err
	}
	return s.dispatcher.Dispatch(ctx, dispatch)
}

func appendDeliveryConfigForOp(runID core.RunID, op string, artifactURIs []string) []string {
	if op != core.TaskOpSplitModule && op != core.TaskOpResplitModule {
		return artifactURIs
	}
	configURI := DeliveryConfigArtifactURI(runID)
	out := append([]string(nil), artifactURIs...)
	for _, uri := range out {
		if uri == configURI {
			return out
		}
	}
	return append(out, configURI)
}

func (s *Service) upsertTask(ctx context.Context, task core.Task) error {
	if _, err := s.tasks.Get(ctx, task.RunID, task.ID); err == nil {
		return s.tasks.Update(ctx, task)
	}
	return s.tasks.Create(ctx, task)
}

func (s *Service) failRun(ctx context.Context, run core.PipelineRun, taskID core.TaskID) error {
	run.Status = core.RunStatusFailed
	run.UpdatedAt = time.Now().UTC()
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("run failed on task=%s", taskID))
	}
	return s.runs.Update(ctx, run)
}

func toArtifactRefs(items []string) []core.ArtifactRef {
	out := make([]core.ArtifactRef, 0, len(items))
	for _, item := range items {
		out = append(out, core.ArtifactRef(item))
	}
	return out
}

func artifactRefsToStrings(items []core.ArtifactRef) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}
