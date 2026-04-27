package orchestrator

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
	"fmt"
	"strings"
	"time"
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
	case core.TaskResultCodeUpstreamMissing, core.TaskResultCodeReviewReject:
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
	if feedback.Op == core.TaskOpTestCode {
		run.Status = core.RunStatusCompleted
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", "run completed after global test_code")
		}
		return s.runs.Update(ctx, run)
	}
	if feedback.Op == core.TaskOpMergeCode {
		return s.createGlobalTestTask(ctx, run, task, feedback.ArtifactURIs)
	}
	items, err := s.tasks.ListByRun(ctx, run.ID)
	if err != nil {
		return err
	}
	outputs := make([]string, 0)
	for _, item := range items {
		if item.ParentID != nil && task.ParentID != nil && *item.ParentID == *task.ParentID {
			if item.ID == task.ID {
				outputs = append(outputs, feedback.ArtifactURIs...)
				continue
			}
			switch item.Status {
			case core.TaskStatusPending, core.TaskStatusDispatched, core.TaskStatusRunning:
				return nil
			}
			outputs = append(outputs, artifactRefsToStrings(item.OutputArtifactRefs)...)
		}
	}
	if s.mergeTaskExists(items, task.ParentID) {
		return nil
	}
	return s.createMergeTask(ctx, run, task, outputs)
}

func (s *Service) mergeTaskExists(items []core.Task, parentID *core.TaskID) bool {
	if parentID == nil {
		return false
	}
	for _, item := range items {
		if item.ParentID != nil && *item.ParentID == *parentID && item.AgentRole == core.AgentRoleArchitect && strings.Contains(string(item.ID), "merge_code") {
			return true
		}
	}
	return false
}

func (s *Service) createMergeTask(ctx context.Context, run core.PipelineRun, completedTask core.Task, inputArtifacts []string) error {
	if completedTask.ParentID == nil {
		return fmt.Errorf("merge task requires a parent task")
	}
	parent, err := s.tasks.Get(ctx, run.ID, *completedTask.ParentID)
	if err != nil {
		return err
	}
	parentID := parent.ID
	taskID := core.TaskID(fmt.Sprintf("%s_merge_code", parent.ID))
	task := core.Task{
		ID:                taskID,
		RunID:             run.ID,
		StageID:           core.StageID(taskID),
		AgentRole:         core.AgentRoleArchitect,
		AgentID:           parent.AgentID,
		ParentID:          &parentID,
		DependsOn:         &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("merge_code task created: %s", taskID))
	}
	return s.dispatchTask(ctx, run, task, core.TaskOpMergeCode, inputArtifacts)
}

func (s *Service) createGlobalTestTask(ctx context.Context, run core.PipelineRun, mergeTask core.Task, inputArtifacts []string) error {
	if mergeTask.ParentID == nil {
		return fmt.Errorf("global test task requires a parent task")
	}
	parentID := *mergeTask.ParentID
	taskID := core.TaskID(fmt.Sprintf("%s_global_test_code", parentID))
	task := core.Task{
		ID:                taskID,
		RunID:             run.ID,
		StageID:           core.StageID(taskID),
		AgentRole:         core.AgentRoleArchitect,
		AgentID:           mergeTask.AgentID,
		ParentID:          &parentID,
		DependsOn:         &mergeTask.ID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("global test_code task created: %s", taskID))
	}
	return s.dispatchTask(ctx, run, task, core.TaskOpTestCode, inputArtifacts)
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
	parent.InputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)

	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	parentStage, ok := findStageByID(spec, parent.StageID)
	if !ok {
		return fmt.Errorf("stage %q not found in pipeline %q", parent.StageID, run.PipelineID)
	}
	return s.dispatchTask(ctx, run, parent, parentStage.Op, feedback.ArtifactURIs)
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

	childOp, inputArtifacts := childTaskPlan(task, feedback)
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
	inputArtifacts := append([]string(nil), feedback.ArtifactURIs...)
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

func (s *Service) expandControlTasks(ctx context.Context, run core.PipelineRun, sourceTask core.Task, controls []core.Control) error {
	for _, control := range controls {
		role, op := controlRoleAndOp(control.Type)
		taskID := dynamicTaskID(sourceTask.ID, control.AgentName, op)
		parentID := sourceTask.ID
		task := core.Task{
			ID:                taskID,
			RunID:             run.ID,
			StageID:           core.StageID(taskID),
			AgentRole:         role,
			AgentID:           core.AgentID(strings.TrimSpace(control.AgentName)),
			ParentID:          &parentID,
			DependsOn:         &parentID,
			Status:            core.TaskStatusPending,
			InputArtifactRefs: toArtifactRefs(control.ArtifactURIs),
			CreatedAt:         time.Now().UTC(),
			UpdatedAt:         time.Now().UTC(),
		}
		if err := s.dispatchTask(ctx, run, task, op, control.ArtifactURIs); err != nil {
			return err
		}
	}
	return nil
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

func controlRoleAndOp(controlType core.ControlType) (core.AgentRole, string) {
	switch controlType {
	case core.ControlTypeNewCoder:
		return core.AgentRoleCoder, core.TaskOpWriteCode
	case core.ControlTypeNewTester:
		return core.AgentRoleTester, core.TaskOpTestData
	default:
		return "", ""
	}
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

func childTaskPlan(task core.Task, feedback core.TaskMetaData) (string, []string) {
	switch feedback.Result {
	case core.TaskResultCodeUpstreamMissing:
		inputs := artifactRefsToStrings(task.InputArtifactRefs)
		return core.TaskOpRewrite, inputs
	case core.TaskResultCodeReviewReject:
		return core.TaskOpReplan, feedback.ArtifactURIs
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
