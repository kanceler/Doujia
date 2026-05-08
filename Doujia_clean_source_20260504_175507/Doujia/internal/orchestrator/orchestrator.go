package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

var ErrDoujiaGitRequired = errors.New("doujiagit repository is required for scheduling")

type Service struct {
	pipelines         pipeline.Registry
	definitions       pipeline.DefinitionRegistry
	runs              repo.RunRepository
	iterations        repo.RunIterationRepository
	instances         repo.PipelineInstanceRepository
	tasks             repo.TaskRepository
	artifacts         repo.ArtifactRepository
	events            repo.EventRepository
	doujiaGit         doujiagit.Repository
	eventSeq          uint64
	advanceMu         sync.Mutex
	provision         runtime.AgentProvisioner
	dispatcher        runtime.Dispatcher
	sessionDispatcher runtime.SessionRuntime
	sessionRoles      map[core.AgentRole]bool
	logger            logging.RunLogger
}

func (s *Service) requireDoujiaGit() (doujiagit.Repository, error) {
	if s == nil || s.doujiaGit == nil {
		return nil, ErrDoujiaGitRequired
	}
	return s.doujiaGit, nil
}

type ResumeResult struct {
	RunID                 core.RunID     `json:"run_id"`
	RefName               string         `json:"ref_name"`
	FrontierSnapshotID    string         `json:"frontier_snapshot_id,omitempty"`
	MaterializedTasks     int            `json:"materialized_tasks"`
	MaterializedInstances int            `json:"materialized_instances"`
	DispatchedTasks       int            `json:"dispatched_tasks"`
	RunStatus             core.RunStatus `json:"run_status"`
}

type taskSnapshotRuntimeContext struct {
	Task             taskRuntimeSnapshot              `json:"task"`
	OutputBags       []core.BagBindingRef             `json:"output_bags,omitempty"`
	PipelineInstance *pipelineInstanceRuntimeSnapshot `json:"pipeline_instance,omitempty"`
}

type taskRuntimeSnapshot struct {
	PipelineInstanceID core.PipelineInstanceID `json:"pipeline_instance_id,omitempty"`
	TransitionID       core.StageID            `json:"transition_id,omitempty"`
	AgentRole          core.AgentRole          `json:"agent_role,omitempty"`
	AgentID            core.AgentID            `json:"agent_id,omitempty"`
	Op                 string                  `json:"op,omitempty"`
}

type pipelineInstanceRuntimeSnapshot struct {
	ID                 core.PipelineInstanceID     `json:"id"`
	RunID              core.RunID                  `json:"run_id"`
	PipelineID         core.PipelineID             `json:"pipeline_id"`
	ParentID           *core.PipelineInstanceID    `json:"parent_id,omitempty"`
	ParentTransitionID string                      `json:"parent_transition_id,omitempty"`
	InstanceKey        string                      `json:"instance_key,omitempty"`
	Status             core.PipelineInstanceStatus `json:"status,omitempty"`
	Params             map[string]string           `json:"params,omitempty"`
	AgentBindings      map[string]core.AgentID     `json:"agent_bindings,omitempty"`
	InputBagIDs        map[string]string           `json:"input_bag_ids,omitempty"`
	InputBagIDLists    map[string][]string         `json:"input_bag_id_lists,omitempty"`
	OutputBagIDs       map[string]string           `json:"output_bag_ids,omitempty"`
	OutputBagIDLists   map[string][]string         `json:"output_bag_id_lists,omitempty"`
	CreatedAt          time.Time                   `json:"created_at"`
	UpdatedAt          time.Time                   `json:"updated_at"`
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
		sessionRoles:      map[core.AgentRole]bool{core.AgentRoleCEO: true},
		logger:            logger,
	}
}

func (s *Service) SetSessionRoles(roles []core.AgentRole) {
	next := make(map[core.AgentRole]bool, len(roles))
	for _, role := range roles {
		if role == "" {
			continue
		}
		next[role] = true
	}
	if len(next) == 0 {
		next[core.AgentRoleCEO] = true
	}
	s.sessionRoles = next
}

func (s *Service) SetRunIterationRepository(iterations repo.RunIterationRepository) {
	s.iterations = iterations
}

func (s *Service) isSessionRole(role core.AgentRole) bool {
	if len(s.sessionRoles) == 0 {
		return role == core.AgentRoleCEO
	}
	return s.sessionRoles[role]
}

func (s *Service) ResumeFromRef(ctx context.Context, runID core.RunID, refName string) (ResumeResult, error) {
	if s.doujiaGit == nil {
		return ResumeResult{}, fmt.Errorf("doujia git repository is required")
	}
	if s.tasks == nil {
		return ResumeResult{}, fmt.Errorf("task repository is required")
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return ResumeResult{}, err
	}
	if strings.TrimSpace(refName) == "" {
		refName = doujiagit.DefaultRefName
	}
	ref, err := s.doujiaGit.GetRef(ctx, runID, refName)
	if err != nil {
		return ResumeResult{}, err
	}
	snapshotIDs, err := s.reachableSnapshotIDsFromRef(ctx, ref)
	if err != nil {
		return ResumeResult{}, err
	}
	materialized, complete, err := s.materializeTasksFromDoujiaGitSnapshots(ctx, run, snapshotIDs)
	if err != nil {
		return ResumeResult{}, err
	}
	materializedInstances, err := s.materializePipelineInstancesFromTaskSnapshots(ctx, run, snapshotIDs)
	if err != nil {
		return ResumeResult{}, err
	}
	dispatchedBefore, err := countTasksByStatus(ctx, s.tasks, runID, core.TaskStatusDispatched)
	if err != nil {
		return ResumeResult{}, err
	}
	if complete {
		if err := s.markRunAwaitingAcceptance(ctx, run, "", "resume_from_ref"); err != nil {
			return ResumeResult{}, err
		}
		run, err = s.runs.Get(ctx, runID)
		if err != nil {
			return ResumeResult{}, err
		}
	} else {
		if run.Status == core.RunStatusCreated || run.Status == "" {
			run.Status = core.RunStatusRunning
			run.UpdatedAt = time.Now().UTC()
			if err := s.runs.Update(ctx, run); err != nil {
				return ResumeResult{}, err
			}
		}
		if err := s.advanceByFacts(ctx, run); err != nil {
			return ResumeResult{}, err
		}
		latest, err := s.runs.Get(ctx, runID)
		if err != nil {
			return ResumeResult{}, err
		}
		run = latest
	}
	dispatchedAfter, err := countTasksByStatus(ctx, s.tasks, runID, core.TaskStatusDispatched)
	if err != nil {
		return ResumeResult{}, err
	}
	if latest, err := s.runs.Get(ctx, runID); err == nil {
		run = latest
	}
	return ResumeResult{
		RunID:                 runID,
		RefName:               ref.RefName,
		FrontierSnapshotID:    ref.FrontierSnapshotID,
		MaterializedTasks:     materialized,
		MaterializedInstances: materializedInstances,
		DispatchedTasks:       dispatchedAfter - dispatchedBefore,
		RunStatus:             run.Status,
	}, nil
}

func (s *Service) SetArtifactRepository(artifacts repo.ArtifactRepository) {
	s.artifacts = artifacts
}

func (s *Service) SetPipelineInstanceRepository(instances repo.PipelineInstanceRepository) {
	s.instances = instances
}

func (s *Service) SetPipelineDefinitionRegistry(definitions pipeline.DefinitionRegistry) {
	s.definitions = definitions
}

func (s *Service) SetEventRepository(events repo.EventRepository) {
	s.events = events
}

func (s *Service) SetDoujiaGitRepository(repository doujiagit.Repository) {
	s.doujiaGit = repository
}

func (s *Service) Start(ctx context.Context, runID core.RunID) error {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	if _, err := s.ensureRootPipelineInstance(ctx, run); err != nil {
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
		Op:        first.Op,
		Status:    core.TaskStatusWaitingExternal,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Service) OnFeedback(ctx context.Context, feedback core.TaskMetaData) error {
	if feedback.Direction != core.TaskDirectionFeedback {
		return fmt.Errorf("expected feedback metadata")
	}
	feedback = normalizeCommitFeedback(feedback)
	if s.logger != nil {
		_ = s.logger.LogTaskMeta(feedback.RunID, "Orchestrator", "feedback received", feedback)
	}
	s.recordEvent(ctx, feedback.RunID, feedback.TaskID, feedback.AgentID, "feedback_received", "feedback received", map[string]any{
		"op":              feedback.Op,
		"result":          feedback.Result,
		"artifact_count":  len(feedback.ArtifactURIs),
		"control_count":   len(feedback.Control),
		"depends_on_ids":  feedback.DependsOnIDs,
		"has_parent_task": feedback.ParentID != nil,
	})

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
	if task.PipelineInstanceID != "" {
		return s.handlePipelineInstanceTaskFeedback(ctx, run, task, feedback)
	}

	var fact feedbackFactContext
	switch feedback.Result {
	case core.TaskResultCodeOK:
		if len(feedback.Control) > 0 {
			if err := validateControls(feedback.Control); err != nil {
				task, updateErr := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusBlocked)
				if updateErr != nil {
					return updateErr
				}
				return s.createResplitTask(ctx, run, task, feedback, err)
			}
			if !hasStartPipelineControls(feedback.Control) {
				if _, err := pairControlsByModule(feedback.Control); err != nil {
					task, updateErr := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusBlocked)
					if updateErr != nil {
						return updateErr
					}
					return s.createResplitTask(ctx, run, task, feedback, err)
				}
			}
			if hasStartPipelineControls(feedback.Control) && (s.instances == nil || s.definitions == nil) {
				task, updateErr := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusBlocked)
				if updateErr != nil {
					return updateErr
				}
				return s.createResplitTask(ctx, run, task, feedback, fmt.Errorf("start_pipeline control requires pipeline definitions and instance repository"))
			}
		}
		task, fact, err = s.updateTaskFromFeedbackWithFact(ctx, task, feedback, core.TaskStatusDone, doujiagit.RefMoveModeAdvance)
		if err != nil {
			return err
		}
		if err := s.mergeRootTaskOutputs(ctx, run, task, feedback); err != nil {
			return err
		}
		if len(feedback.Control) > 0 {
			if hasStartPipelineControls(feedback.Control) {
				return s.expandStartPipelineControls(ctx, run, task, feedback)
			}
			return s.expandControlTasks(ctx, run, task, feedback.Control)
		}
		if fact.Committed && feedback.Result == core.TaskResultCodeOK && task.ExecutionMode != core.ExecutionModeRepair {
			return s.advanceByFacts(ctx, run)
		}
	case core.TaskResultCodeRewrite, core.TaskResultCodeReplan:
		task, fact, err = s.updateTaskFromFeedbackWithFact(ctx, task, feedback, core.TaskStatusBlocked, doujiagit.RefMoveModeAdvance)
		if err != nil {
			return err
		}
		return s.createChildTaskWithFact(ctx, run, task, feedback, fact)
	case core.TaskResultCodeControlInvalid:
		task, fact, err = s.updateTaskFromFeedbackWithFact(ctx, task, feedback, core.TaskStatusBlocked, doujiagit.RefMoveModeAdvance)
		if err != nil {
			return err
		}
		return s.createResplitTaskWithFact(ctx, run, task, feedback, fmt.Errorf("agent reported invalid control"), fact)
	default:
		task, fact, err = s.updateTaskFromFeedbackWithFact(ctx, task, feedback, core.TaskStatusFailed, doujiagit.RefMoveModeAdvance)
		if err != nil {
			return err
		}
		if fact.Committed {
			if err := s.advanceByFacts(ctx, run); err != nil {
				return err
			}
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("run failed on task=%s", task.ID))
		}
		s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "run_failed", "run failed", map[string]any{
			"task_id": task.ID,
			"result":  feedback.Result,
		})
		return s.runs.Update(ctx, run)
	}

	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	nextStage, ok := findNextStage(spec, task.StageID)
	if !ok {
		if handled, err := s.advanceRootPipelineFromLegacyTerminalTask(ctx, run, task, feedback, fact); err != nil || handled {
			return err
		}
		return s.markRunAwaitingAcceptance(ctx, run, task.ID, "root_pipeline_complete")
	}

	nextTask := core.Task{
		ID:                core.TaskID(nextStage.ID),
		RunID:             run.ID,
		StageID:           nextStage.ID,
		AgentRole:         nextStage.AgentRole,
		AgentID:           nextStage.AgentAlias,
		Op:                nextStage.Op,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(feedback.ArtifactURIs),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	previousTasks, _ := s.tasks.ListByRun(ctx, run.ID)
	nextTask.InputBags = legacyNextTaskInputBags(nextStage, task, feedback, previousTasks)
	nextTask.InputBagIDs = legacyNextTaskInputBagIDs(nextStage, task, feedback, nextTask.InputBags)
	if len(nextStage.DependsOnIDs) > 0 {
		nextTask.DependsOnIDs = stageIDsToTaskIDs(nextStage.DependsOnIDs)
	}

	if nextStage.External {
		nextTask.Status = core.TaskStatusWaitingExternal
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("next external task created: %s", nextTask.ID))
		}
		return s.tasks.Create(ctx, nextTask)
	}

	return s.dispatchTaskWithFact(ctx, run, nextTask, nextStage.Op, feedback.ArtifactURIs, fact)
}

func (s *Service) advanceRootPipelineFromLegacyTerminalTask(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData, fact feedbackFactContext) (bool, error) {
	if s.instances == nil || s.definitions == nil || task.PipelineInstanceID != "" || task.ParentID != nil {
		return false, nil
	}
	instance, err := s.ensureRootPipelineInstance(ctx, run)
	if err != nil {
		return false, err
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return false, nil
	}
	transition, ok := findTransition(def, string(task.StageID))
	if !ok {
		return false, nil
	}
	if err := s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, transition.ToState, feedback.Result, fact); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) updateTaskFromFeedback(ctx context.Context, task core.Task, feedback core.TaskMetaData, status core.TaskStatus) (core.Task, error) {
	task, _, err := s.updateTaskFromFeedbackWithFact(ctx, task, feedback, status, doujiagit.RefMoveModeAdvance)
	return task, err
}

func (s *Service) updateTaskFromFeedbackWithFact(ctx context.Context, task core.Task, feedback core.TaskMetaData, status core.TaskStatus, arrivalKind string) (core.Task, feedbackFactContext, error) {
	fact, err := s.commitFeedbackFact(ctx, task, feedback, arrivalKind)
	if err != nil {
		return core.Task{}, feedbackFactContext{}, err
	}
	task, err = s.updateTaskStateFromFeedback(ctx, task, feedback, status, fact)
	if err != nil {
		return core.Task{}, feedbackFactContext{}, err
	}
	return task, fact, nil
}

func (s *Service) updateTaskStateFromFeedback(ctx context.Context, task core.Task, feedback core.TaskMetaData, status core.TaskStatus, fact feedbackFactContext) (core.Task, error) {
	task.Status = status
	if strings.TrimSpace(task.Op) == "" {
		task.Op = feedback.Op
	}
	task.Result = feedback.Result
	task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
	task.UpdatedAt = time.Now().UTC()
	if len(fact.OutputBagIDs) > 0 {
		task.OutputBagIDs = append([]string(nil), fact.OutputBagIDs...)
	} else if shouldForwardInputBags(task, feedback, status) {
		task.OutputBagIDs = append([]string(nil), task.InputBagIDs...)
	}
	if err := s.tasks.Update(ctx, task); err != nil {
		return core.Task{}, err
	}
	if err := s.recordFeedbackArtifacts(ctx, task, feedback); err != nil {
		return core.Task{}, err
	}
	s.recordEvent(ctx, task.RunID, task.ID, task.AgentID, "task_status_updated", "task status updated", map[string]any{
		"status":           status,
		"op":               task.Op,
		"result":           feedback.Result,
		"output_artifacts": len(feedback.ArtifactURIs),
	})
	return task, nil
}

func (s *Service) handlePipelineInstanceTaskFeedback(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	switch feedback.Result {
	case core.TaskResultCodeOK, core.TaskResultCodeBug:
		status := core.TaskStatusDone
		if feedback.Result == core.TaskResultCodeBug {
			status = core.TaskStatusBlocked
		}
		task, fact, err := s.updateTaskFromFeedbackWithFact(ctx, task, feedback, status, doujiagit.RefMoveModeAdvance)
		if err != nil {
			return err
		}
		if s.instances == nil || s.definitions == nil {
			return nil
		}
		instance, err := s.instances.Get(ctx, run.ID, task.PipelineInstanceID)
		if err != nil {
			return err
		}
		def, err := s.definitions.GetDef(ctx, instance.PipelineID)
		if err != nil {
			return err
		}
		transition, ok := findTransition(def, string(task.StageID))
		if !ok {
			return fmt.Errorf("pipeline %q transition %q not found for task %q", def.PipelineID, task.StageID, task.ID)
		}
		outputBagLists, err := outputBagIDListsFromCommitForInstance(transition, feedback.Result, feedback.Commit, task.OutputBagIDs, instance)
		if err != nil {
			return err
		}
		if len(outputBagLists) > 0 {
			instance = mergeOutputBagLists(instance, outputBagLists)
		} else {
			outputBags := map[string]string(nil)
			if feedback.Commit == nil && sameStringSet(task.OutputBagIDs, task.InputBagIDs) {
				outputBags = bagIDsForSpecs(transition.InputBags, task.OutputBagIDs)
			}
			if len(outputBags) == 0 {
				outputBags = outputBagIDsForTransition(transition, feedback.Result, task.OutputBagIDs)
			}
			instance = mergeOutputBags(instance, outputBags)
		}
		instance.UpdatedAt = time.Now().UTC()
		if feedback.Result == core.TaskResultCodeBug && transition.Op == core.TaskOpTestCode {
			debugTask, ok, err := nextDebugTaskForRecover(run.ID, instance, def, transition, feedback.Result)
			if err != nil {
				return err
			}
			if ok {
				if err := s.recordDoujiaGitRecover(ctx, run, task, &debugTask, feedback); err != nil {
					return err
				}
			}
		}
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
		if fact.Committed && feedback.Result == core.TaskResultCodeOK && task.ExecutionMode != core.ExecutionModeRepair {
			return s.advanceByFacts(ctx, run)
		}
		if task.ExecutionMode == core.ExecutionModeRepair && feedback.Result == core.TaskResultCodeOK && task.ExceptionFrameID != "" {
			if fact.Committed {
				if err := s.advanceByFacts(ctx, run); err != nil {
					return err
				}
			}
			if handled, err := s.handleRepairHandlerSuccess(ctx, run, instance, def, transition, task, feedback); err != nil || handled {
				return err
			}
		}
		if feedback.Result == core.TaskResultCodeBug && !pipelineHasLocalRecover(def, transition, feedback.Result) {
			if handled, err := s.bubbleExceptionFrame(ctx, run, instance, def, transition, task, feedback); err != nil || handled {
				return err
			}
		}
		return s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, transition.ToState, feedback.Result, fact)
	default:
		if _, err := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusFailed); err != nil {
			return err
		}
		if s.instances != nil {
			if instance, err := s.instances.Get(ctx, run.ID, task.PipelineInstanceID); err == nil {
				instance.Status = core.PipelineInstanceStatusFailed
				instance.UpdatedAt = time.Now().UTC()
				_ = s.instances.Update(ctx, instance)
			}
		}
		return s.failRun(ctx, run, task.ID)
	}
}

func (s *Service) mergeRootTaskOutputs(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	if task.PipelineInstanceID != "" || s.instances == nil || s.definitions == nil || len(task.OutputBagIDs) == 0 {
		return nil
	}
	instance, err := s.ensureRootPipelineInstance(ctx, run)
	if err != nil {
		return err
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return nil
	}
	transition, ok := findTransition(def, string(task.StageID))
	if !ok {
		return nil
	}
	outputBagLists, err := outputBagIDListsFromCommitForInstance(transition, feedback.Result, feedback.Commit, task.OutputBagIDs, instance)
	if err != nil {
		return nil
	}
	if len(outputBagLists) > 0 {
		instance = mergeOutputBagLists(instance, outputBagLists)
		instance.UpdatedAt = time.Now().UTC()
		return s.instances.Update(ctx, instance)
	}
	outputBags := map[string]string(nil)
	if len(outputBags) == 0 {
		outputBags = outputBagIDsForTransition(transition, feedback.Result, task.OutputBagIDs)
	}
	if len(outputBags) == 0 {
		return nil
	}
	instance = mergeOutputBags(instance, outputBags)
	instance.UpdatedAt = time.Now().UTC()
	return s.instances.Update(ctx, instance)
}

func normalizeCommitFeedback(feedback core.TaskMetaData) core.TaskMetaData {
	if feedback.Commit == nil {
		return feedback
	}
	if feedback.Commit.Result != "" {
		feedback.Result = feedback.Commit.Result
	}
	if len(feedback.Commit.Control) > 0 {
		feedback.Control = feedback.Commit.Control
	}
	if len(feedback.Commit.MaterializedOutputRefs) > 0 {
		feedback.ArtifactURIs = feedback.Commit.MaterializedOutputRefs
	}
	return feedback
}

func firstString(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}

type feedbackFactContext struct {
	SnapshotID         string
	SnapshotVersionID  string
	FrontierSnapshotID string
	RefName            string
	OutputBagIDs       []string
	Committed          bool
}

type activeRefMember struct {
	Ref      doujiagit.Ref
	Snapshot doujiagit.TaskSnapshot
}

type activeRefAdvanceResult struct {
	Consumed                    bool
	Status                      string
	DecisionKind                string
	ContinuationID              string
	Reason                      string
	ProducedTaskIDs             []string
	ProducedPipelineInstanceIDs []string
	Replacement                 frontierReplacement
	ReplacementResult           frontierReplacementResult
}

type frontierReplacement struct {
	RefName                string
	RunID                  core.RunID
	FromFrontierSnapshotID string
	ConsumedSnapshotIDs    []string
	ProducedSnapshotIDs    []string
	Mode                   string
	Reason                 string
}

type frontierReplacementResult struct {
	ToFrontierSnapshotID string
	NewMemberSnapshotIDs []string
}

type runFactProjection struct {
	RunID                   core.RunID
	RefName                 string
	FrontierSnapshotID      string
	ActiveSnapshotIDs       []string
	UnconsumedSnapshotIDs   []string
	InFlightTaskIDs         []string
	TerminalSnapshotIDs     []string
	FailedSnapshotIDs       []string
	AwaitingAcceptance      bool
	HasAdvanceableWork      bool
	LegacyRunStatus         core.RunStatus
	LegacyProjectionIsStale bool
}

func (s *Service) buildRunFactProjection(ctx context.Context, run core.PipelineRun) (runFactProjection, error) {
	repository, err := s.requireDoujiaGit()
	if err != nil {
		return runFactProjection{}, err
	}
	projection := runFactProjection{
		RunID:           run.ID,
		RefName:         doujiagit.DefaultRefName,
		LegacyRunStatus: run.Status,
	}
	ref, err := repository.GetRef(ctx, run.ID, doujiagit.DefaultRefName)
	if err != nil {
		projection.LegacyProjectionIsStale = run.Status == core.RunStatusFailed
		return projection, nil
	}
	projection.RefName = ref.RefName
	projection.FrontierSnapshotID = ref.FrontierSnapshotID
	memberIDs := doujiagit.NormalizeIDs(ref.FrontierMemberSnapshotIDs)
	if len(memberIDs) == 0 && strings.TrimSpace(ref.FrontierSnapshotID) != "" {
		frontier, err := repository.GetFrontierSnapshot(ctx, ref.FrontierSnapshotID)
		if err != nil {
			return runFactProjection{}, err
		}
		memberIDs = doujiagit.NormalizeIDs(frontier.TaskSnapshotIDs)
	}
	projection.ActiveSnapshotIDs = append([]string(nil), memberIDs...)
	for _, snapshotID := range memberIDs {
		snapshot, err := repository.GetSnapshot(ctx, snapshotID)
		if err != nil {
			return runFactProjection{}, err
		}
		decision, err := repository.GetSnapshotProcessingDecision(ctx, run.ID, ref.RefName, snapshotID)
		consumed := false
		if err == nil {
			switch decision.Status {
			case doujiagit.SnapshotProcessingStatusAdvanced:
				consumed = true
			case doujiagit.SnapshotProcessingStatusTerminal:
				consumed = true
				projection.TerminalSnapshotIDs = append(projection.TerminalSnapshotIDs, snapshotID)
			}
		}
		if !consumed {
			projection.UnconsumedSnapshotIDs = append(projection.UnconsumedSnapshotIDs, snapshotID)
			if snapshot.Result == core.TaskResultCodeOK {
				projection.HasAdvanceableWork = true
			}
		}
		if snapshot.Result != "" && snapshot.Result != core.TaskResultCodeOK {
			projection.FailedSnapshotIDs = append(projection.FailedSnapshotIDs, snapshotID)
		}
	}
	projection.ActiveSnapshotIDs = doujiagit.NormalizeIDs(projection.ActiveSnapshotIDs)
	projection.UnconsumedSnapshotIDs = doujiagit.NormalizeIDs(projection.UnconsumedSnapshotIDs)
	projection.TerminalSnapshotIDs = doujiagit.NormalizeIDs(projection.TerminalSnapshotIDs)
	projection.FailedSnapshotIDs = doujiagit.NormalizeIDs(projection.FailedSnapshotIDs)
	if run.Status == core.RunStatusFailed && len(projection.FailedSnapshotIDs) == 0 {
		projection.LegacyProjectionIsStale = true
	}
	return projection, nil
}

func (s *Service) syncLegacyStatusProjection(ctx context.Context, run core.PipelineRun, projection runFactProjection) error {
	if projection.LegacyProjectionIsStale && run.Status == core.RunStatusFailed && len(projection.FailedSnapshotIDs) == 0 {
		run.Status = core.RunStatusRunning
		run.UpdatedAt = s.uniqueTimestamp()
		return s.runs.Update(ctx, run)
	}
	return nil
}

func (s *Service) commitFeedbackFact(ctx context.Context, task core.Task, feedback core.TaskMetaData, arrivalKind string) (feedbackFactContext, error) {
	if _, err := s.requireDoujiaGit(); err != nil {
		return feedbackFactContext{}, err
	}
	now := s.uniqueTimestamp()
	snapshotID := doujiagit.StableSnapshotID(task.RunID, task.ID, now)
	snapshotVersionID := snapshotID + ":v1"
	if strings.TrimSpace(arrivalKind) == "" {
		arrivalKind = doujiagit.RefMoveModeAdvance
	}
	var committedBags []core.CommittedBagDef
	if feedback.Commit != nil {
		committedBags = feedback.Commit.EffectiveCommittedBags()
	}
	outputBagIDs := make([]string, 0, len(committedBags))
	for _, def := range committedBags {
		outputKey := strings.TrimSpace(def.Name)
		if outputKey == "" {
			return feedbackFactContext{}, fmt.Errorf("committed bag name is required")
		}
		bagID := doujiagit.StableBagID(task.RunID, snapshotID, indexedBagLookupKey(outputKey, def.Indexes))
		if err := s.doujiaGit.CreateBag(ctx, doujiagit.ArtifactBag{
			BagID:              bagID,
			RunID:              task.RunID,
			ArtifactVersionIDs: def.ArtifactVersionIDs,
			CreatedAt:          now,
		}); err != nil {
			return feedbackFactContext{}, err
		}
		outputBagIDs = append(outputBagIDs, bagID)
	}
	diagnosticsJSON := ""
	if feedback.Commit != nil {
		diagnosticsJSON = feedback.Commit.DiagnosticsJSON
	}
	inputBagIDs := feedback.InputBagIDs
	if len(inputBagIDs) == 0 {
		inputBagIDs = task.InputBagIDs
	}
	runtimeContextJSON, err := s.taskSnapshotRuntimeContextJSON(ctx, task, feedback, outputBagIDs)
	if err != nil {
		return feedbackFactContext{}, err
	}
	repairMetadata := s.repairSnapshotMetadata(ctx, task)
	if err := s.doujiaGit.CreateSnapshot(ctx, doujiagit.TaskSnapshot{
		SnapshotID:                 snapshotID,
		RunID:                      task.RunID,
		TaskID:                     task.ID,
		LogicalSnapshotID:          logicalSnapshotIDForTask(task),
		SnapshotVersionID:          snapshotVersionID,
		SnapshotVersionNo:          1,
		ArrivalKind:                arrivalKind,
		PipelineInstanceID:         task.PipelineInstanceID,
		TransitionID:               task.StageID,
		AgentRole:                  task.AgentRole,
		AgentID:                    task.AgentID,
		Op:                         task.Op,
		Result:                     feedback.Result,
		InputBagIDs:                inputBagIDs,
		OutputBagIDs:               outputBagIDs,
		DiagnosticsJSON:            diagnosticsJSON,
		RuntimeContextJSON:         runtimeContextJSON,
		CreatedAt:                  now,
		BranchKind:                 repairMetadata.BranchKind,
		BranchFromSnapshotID:       repairMetadata.BranchFromSnapshotID,
		RecoverFromSnapshotID:      repairMetadata.RecoverFromSnapshotID,
		RecoverTargetSnapshotIDs:   repairMetadata.RecoverTargetSnapshotIDs,
		ReusableSnapshotIDs:        repairMetadata.ReusableSnapshotIDs,
		RecoverAnchorSnapshotIDs:   repairMetadata.RecoverAnchorSnapshotIDs,
		PreviousAttemptSnapshotIDs: repairMetadata.PreviousAttemptSnapshotIDs,
		FailureReportBagIDs:        repairMetadata.FailureReportBagIDs,
		PreviousOutputBagIDs:       repairMetadata.PreviousOutputBagIDs,
		RepairTargetTransitionID:   repairMetadata.RepairTargetTransitionID,
		RepairTargetTaskID:         repairMetadata.RepairTargetTaskID,
	}); err != nil {
		return feedbackFactContext{}, err
	}
	refName := doujiagit.DefaultRefName
	frontierSnapshotID, err := s.moveDoujiaGitRefWithRetry(ctx, task, snapshotID, refName, now, arrivalKind, s.consumedSnapshotIDsForFeedback(ctx, inputBagIDs))
	if err != nil {
		return feedbackFactContext{}, err
	}
	return feedbackFactContext{
		SnapshotID:         snapshotID,
		SnapshotVersionID:  snapshotVersionID,
		FrontierSnapshotID: frontierSnapshotID,
		RefName:            refName,
		OutputBagIDs:       outputBagIDs,
		Committed:          true,
	}, nil
}

type repairSnapshotMetadata struct {
	BranchKind                 string
	BranchFromSnapshotID       string
	RecoverFromSnapshotID      string
	RecoverTargetSnapshotIDs   []string
	ReusableSnapshotIDs        []string
	RecoverAnchorSnapshotIDs   []string
	PreviousAttemptSnapshotIDs []string
	FailureReportBagIDs        []string
	PreviousOutputBagIDs       []string
	RepairTargetTransitionID   string
	RepairTargetTaskID         string
}

func (s *Service) repairSnapshotMetadata(ctx context.Context, task core.Task) repairSnapshotMetadata {
	if task.ExecutionMode != core.ExecutionModeRepair || task.ExceptionFrameID == "" {
		return repairSnapshotMetadata{}
	}
	meta := repairSnapshotMetadata{
		BranchKind:               doujiagit.DecisionKindRepair,
		RepairTargetTransitionID: string(task.StageID),
		RepairTargetTaskID:       string(task.ID),
	}
	if s.doujiaGit == nil || s.instances == nil {
		return meta
	}
	frame, owner, ok, err := s.findExceptionFrameOwner(ctx, task.RunID, task.ExceptionFrameID)
	if err != nil || !ok {
		return meta
	}
	repairOwner := owner
	if len(frame.SelectedHandlers) > 0 {
		if selectedOwner, err := s.instances.Get(ctx, task.RunID, frame.SelectedHandlers[0].OwnerInstanceID); err == nil {
			repairOwner = selectedOwner
		}
	}
	meta.FailureReportBagIDs = bagIDsFromBindings(frame.FailureBags)
	meta.PreviousOutputBagIDs = previousOutputBagIDsForRepair(repairOwner, frame)
	meta.ReusableSnapshotIDs = snapshotsProducingBags(ctx, s.doujiaGit, reusableBagIDsForRepair(repairOwner, frame))
	meta.ReusableSnapshotIDs = doujiagit.NormalizeIDs(append(meta.ReusableSnapshotIDs, snapshotsProducingBags(ctx, s.doujiaGit, reusableFailedInputBagIDsForRepair(repairOwner, frame))...))
	meta.RecoverAnchorSnapshotIDs = snapshotsProducingBags(ctx, s.doujiaGit, bagIDsForInstanceInputs(repairOwner))
	meta.RecoverTargetSnapshotIDs = doujiagit.NormalizeIDs(append(append([]string(nil), meta.RecoverAnchorSnapshotIDs...), meta.ReusableSnapshotIDs...))
	meta.BranchFromSnapshotID = firstString(meta.RecoverAnchorSnapshotIDs)
	meta.RecoverFromSnapshotID = snapshotIDForTask(ctx, s.doujiaGit, task.RunID, frame.OriginTaskID)
	if meta.RecoverFromSnapshotID != "" {
		meta.PreviousAttemptSnapshotIDs = []string{meta.RecoverFromSnapshotID}
		if len(meta.FailureReportBagIDs) == 0 {
			if failedSnapshot, err := s.doujiaGit.GetSnapshot(ctx, meta.RecoverFromSnapshotID); err == nil {
				meta.FailureReportBagIDs = append([]string(nil), failedSnapshot.OutputBagIDs...)
			}
		}
	}
	return meta
}

func previousOutputBagIDsForRepair(owner core.PipelineInstance, frame core.ExceptionFrame) []string {
	out := make([]string, 0)
	for _, binding := range frame.SelectedHandlers {
		if binding.OwnerInstanceID != "" && binding.OwnerInstanceID != owner.ID {
			continue
		}
		for _, replace := range binding.Replaces {
			if strings.TrimSpace(replace.BagID) != "" {
				out = append(out, replace.BagID)
			}
		}
	}
	return uniqueStrings(out)
}

func reusableBagIDsForRepair(owner core.PipelineInstance, frame core.ExceptionFrame) []string {
	previous := make(map[string]bool)
	for _, id := range previousOutputBagIDsForRepair(owner, frame) {
		previous[id] = true
	}
	out := make([]string, 0)
	for _, bagID := range bagIDsForInstanceOutputs(owner) {
		if previous[bagID] {
			continue
		}
		out = append(out, bagID)
	}
	return uniqueStrings(out)
}

func reusableFailedInputBagIDsForRepair(owner core.PipelineInstance, frame core.ExceptionFrame) []string {
	replaced := make(map[string]bool)
	for _, binding := range frame.SelectedHandlers {
		if binding.OwnerInstanceID != "" && binding.OwnerInstanceID != owner.ID {
			continue
		}
		for _, replace := range binding.Replaces {
			if strings.TrimSpace(replace.BagID) != "" {
				replaced[replace.BagID] = true
			}
			if strings.TrimSpace(replace.Name) != "" {
				replaced[replace.Name] = true
			}
		}
	}
	out := make([]string, 0)
	for _, failed := range frame.FailedInputBags {
		if strings.TrimSpace(failed.BagID) == "" {
			continue
		}
		if replaced[failed.BagID] || replaced[failed.Name] {
			continue
		}
		out = append(out, failed.BagID)
	}
	return uniqueStrings(out)
}

func bagIDsForInstanceInputs(instance core.PipelineInstance) []string {
	out := make([]string, 0, len(instance.InputBagIDs))
	for _, bagID := range instance.InputBagIDs {
		out = append(out, bagID)
	}
	for _, bagIDs := range instance.InputBagIDLists {
		out = append(out, bagIDs...)
	}
	return uniqueStrings(out)
}

func bagIDsForInstanceOutputs(instance core.PipelineInstance) []string {
	out := make([]string, 0, len(instance.OutputBagIDs))
	for _, bagID := range instance.OutputBagIDs {
		out = append(out, bagID)
	}
	for _, bagIDs := range instance.OutputBagIDLists {
		out = append(out, bagIDs...)
	}
	return uniqueStrings(out)
}

func snapshotsProducingBags(ctx context.Context, repository doujiagit.Repository, bagIDs []string) []string {
	out := make([]string, 0)
	for _, bagID := range bagIDs {
		snapshotID, err := repository.ProducerOfBag(ctx, bagID)
		if err != nil {
			continue
		}
		out = append(out, snapshotID)
	}
	return doujiagit.NormalizeIDs(out)
}

func snapshotIDForTask(ctx context.Context, repository doujiagit.Repository, runID core.RunID, taskID core.TaskID) string {
	snapshots, err := repository.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		return ""
	}
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].TaskID == taskID {
			return snapshots[i].SnapshotID
		}
	}
	return ""
}

func (s *Service) activeRefMembers(ctx context.Context, runID core.RunID) ([]activeRefMember, error) {
	repository, err := s.requireDoujiaGit()
	if err != nil {
		return nil, err
	}
	ref, err := repository.GetRef(ctx, runID, doujiagit.DefaultRefName)
	if err != nil {
		return nil, nil
	}
	snapshotIDs := doujiagit.NormalizeIDs(ref.FrontierMemberSnapshotIDs)
	if len(snapshotIDs) == 0 && strings.TrimSpace(ref.FrontierSnapshotID) != "" {
		frontier, err := repository.GetFrontierSnapshot(ctx, ref.FrontierSnapshotID)
		if err != nil {
			return nil, err
		}
		snapshotIDs = doujiagit.NormalizeIDs(frontier.TaskSnapshotIDs)
	}
	members := make([]activeRefMember, 0, len(snapshotIDs))
	for _, snapshotID := range snapshotIDs {
		snapshot, err := repository.GetSnapshot(ctx, snapshotID)
		if err != nil {
			return nil, err
		}
		members = append(members, activeRefMember{Ref: ref, Snapshot: snapshot})
	}
	return members, nil
}

func (s *Service) advanceActiveRef(ctx context.Context, run core.PipelineRun) error {
	members, err := s.activeRefMembers(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, member := range members {
		consumed, err := s.snapshotAlreadyConsumed(ctx, run.ID, member.Ref.RefName, member.Snapshot.SnapshotID)
		if err != nil {
			return err
		}
		if consumed {
			continue
		}
		result, err := s.advanceActiveRefMember(ctx, run, member)
		if err != nil {
			return err
		}
		if result.Consumed {
			if err := s.recordSnapshotProcessingDecision(ctx, member, result); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) advanceByFacts(ctx context.Context, run core.PipelineRun) error {
	if _, err := s.requireDoujiaGit(); err != nil {
		return err
	}
	if err := s.advanceActiveRef(ctx, run); err != nil {
		return err
	}
	latest, err := s.runs.Get(ctx, run.ID)
	if err != nil {
		latest = run
	}
	projection, err := s.buildRunFactProjection(ctx, latest)
	if err != nil {
		return err
	}
	return s.syncLegacyStatusProjection(ctx, latest, projection)
}

func (s *Service) advanceActiveRefMember(ctx context.Context, run core.PipelineRun, member activeRefMember) (activeRefAdvanceResult, error) {
	snapshot := member.Snapshot
	if snapshot.Result != core.TaskResultCodeOK {
		return activeRefAdvanceResult{
			Consumed:     true,
			Status:       doujiagit.SnapshotProcessingStatusTerminal,
			DecisionKind: doujiagit.RefMoveModeAdvance,
			Reason:       fmt.Sprintf("snapshot result %s is terminal for phase 3 active-ref consumption", snapshot.Result),
		}, nil
	}
	if snapshot.PipelineInstanceID != "" {
		return s.advancePipelineInstanceActiveRefMember(ctx, run, member)
	}
	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	nextStage, ok := findNextStage(spec, stageIDForSnapshot(snapshot))
	if !ok {
		fact := feedbackFactContext{
			SnapshotID:         snapshot.SnapshotID,
			SnapshotVersionID:  snapshot.SnapshotVersionID,
			FrontierSnapshotID: member.Ref.FrontierSnapshotID,
			RefName:            member.Ref.RefName,
			OutputBagIDs:       append([]string(nil), snapshot.OutputBagIDs...),
			Committed:          true,
		}
		if handled, err := s.advanceRootPipelineFromLegacyTerminalSnapshot(ctx, run, snapshot, fact); err != nil || handled {
			if err != nil {
				return activeRefAdvanceResult{}, err
			}
			return activeRefAdvanceResult{
				Consumed:        true,
				Status:          doujiagit.SnapshotProcessingStatusAdvanced,
				DecisionKind:    doujiagit.RefMoveModeAdvance,
				ContinuationID:  string(stageIDForSnapshot(snapshot)),
				ProducedTaskIDs: nil,
				Replacement: frontierReplacement{
					RunID:                  run.ID,
					RefName:                member.Ref.RefName,
					FromFrontierSnapshotID: member.Ref.FrontierSnapshotID,
					ConsumedSnapshotIDs:    []string{snapshot.SnapshotID},
					Mode:                   doujiagit.RefMoveModeAdvance,
				},
				ReplacementResult: frontierReplacementResult{ToFrontierSnapshotID: member.Ref.FrontierSnapshotID},
			}, nil
		}
		if err := s.markRunAwaitingAcceptance(ctx, run, snapshot.TaskID, "root_pipeline_complete"); err != nil {
			return activeRefAdvanceResult{}, err
		}
		return activeRefAdvanceResult{
			Consumed:       true,
			Status:         doujiagit.SnapshotProcessingStatusAdvanced,
			DecisionKind:   doujiagit.RefMoveModeAdvance,
			ContinuationID: "run_awaiting_acceptance",
			Reason:         "root pipeline complete",
		}, nil
	}
	nextTask := core.Task{
		ID:                core.TaskID(nextStage.ID),
		RunID:             run.ID,
		StageID:           nextStage.ID,
		AgentRole:         nextStage.AgentRole,
		AgentID:           nextStage.AgentAlias,
		Op:                nextStage.Op,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: nil,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if len(nextStage.DependsOnIDs) > 0 {
		nextTask.DependsOnIDs = stageIDsToTaskIDs(nextStage.DependsOnIDs)
	}
	nextTask.InputBags = bagBindingsFromSnapshotOutput(nextStage, snapshot)
	nextTask.InputBagIDs = snapshot.OutputBagIDs
	fact := feedbackFactContext{
		SnapshotID:         snapshot.SnapshotID,
		SnapshotVersionID:  snapshot.SnapshotVersionID,
		FrontierSnapshotID: member.Ref.FrontierSnapshotID,
		RefName:            member.Ref.RefName,
		OutputBagIDs:       append([]string(nil), snapshot.OutputBagIDs...),
		Committed:          true,
	}
	if err := s.dispatchTaskWithFact(ctx, run, nextTask, nextStage.Op, nil, fact); err != nil {
		return activeRefAdvanceResult{}, err
	}
	return activeRefAdvanceResult{
		Consumed:        true,
		Status:          doujiagit.SnapshotProcessingStatusAdvanced,
		DecisionKind:    doujiagit.RefMoveModeAdvance,
		ContinuationID:  string(nextStage.ID),
		ProducedTaskIDs: []string{string(nextTask.ID)},
		Replacement: frontierReplacement{
			RunID:                  run.ID,
			RefName:                member.Ref.RefName,
			FromFrontierSnapshotID: member.Ref.FrontierSnapshotID,
			ConsumedSnapshotIDs:    []string{snapshot.SnapshotID},
			Mode:                   doujiagit.RefMoveModeAdvance,
		},
		ReplacementResult: frontierReplacementResult{ToFrontierSnapshotID: member.Ref.FrontierSnapshotID},
	}, nil
}

func (s *Service) advanceRootPipelineFromLegacyTerminalSnapshot(ctx context.Context, run core.PipelineRun, snapshot doujiagit.TaskSnapshot, fact feedbackFactContext) (bool, error) {
	if s.instances == nil || s.definitions == nil || snapshot.PipelineInstanceID != "" {
		return false, nil
	}
	instance, err := s.ensureRootPipelineInstance(ctx, run)
	if err != nil {
		return false, err
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return false, nil
	}
	transition, ok := findTransition(def, string(stageIDForSnapshot(snapshot)))
	if !ok {
		return false, nil
	}
	outputBagLists, err := outputBagIDListsFromSnapshotForInstance(transition, snapshot.Result, snapshot.OutputBagIDs, instance, snapshot)
	if err != nil {
		return false, err
	}
	if len(outputBagLists) > 0 {
		instance = mergeOutputBagLists(instance, outputBagLists)
	} else {
		instance = mergeOutputBags(instance, outputBagIDsForTransition(transition, snapshot.Result, snapshot.OutputBagIDs))
	}
	instance = mirrorOutputBagsToInputBags(instance)
	instance.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, instance); err != nil {
		return false, err
	}
	return true, s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, transition.ToState, snapshot.Result, fact)
}

func mirrorOutputBagsToInputBags(instance core.PipelineInstance) core.PipelineInstance {
	if len(instance.OutputBagIDs) == 0 && len(instance.OutputBagIDLists) == 0 {
		return instance
	}
	if instance.InputBagIDs == nil {
		instance.InputBagIDs = make(map[string]string, len(instance.OutputBagIDs))
	}
	if instance.InputBagIDLists == nil {
		instance.InputBagIDLists = make(map[string][]string, len(instance.OutputBagIDLists))
	}
	for key, value := range instance.OutputBagIDs {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		instance.InputBagIDs[key] = value
	}
	for key, values := range instance.OutputBagIDLists {
		if strings.TrimSpace(key) == "" {
			continue
		}
		for _, value := range values {
			instance.InputBagIDLists[key] = appendUniqueString(instance.InputBagIDLists[key], value)
		}
	}
	return instance
}

func (s *Service) advancePipelineInstanceActiveRefMember(ctx context.Context, run core.PipelineRun, member activeRefMember) (activeRefAdvanceResult, error) {
	snapshot := member.Snapshot
	if s.instances == nil || s.definitions == nil {
		return activeRefAdvanceResult{}, nil
	}
	instance, err := s.instances.Get(ctx, run.ID, snapshot.PipelineInstanceID)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	transition, ok := findTransition(def, string(snapshot.TransitionID))
	if !ok {
		return activeRefAdvanceResult{}, fmt.Errorf("pipeline %q transition %q not found for snapshot %q", def.PipelineID, snapshot.TransitionID, snapshot.SnapshotID)
	}
	outputBagLists, err := outputBagIDListsFromSnapshotForInstance(transition, snapshot.Result, snapshot.OutputBagIDs, instance, snapshot)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	if len(outputBagLists) > 0 {
		instance = mergeOutputBagLists(instance, outputBagLists)
	} else {
		instance = mergeOutputBags(instance, outputBagIDsForTransition(transition, snapshot.Result, snapshot.OutputBagIDs))
	}
	instance.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, instance); err != nil {
		return activeRefAdvanceResult{}, err
	}
	beforeTasks, err := s.tasks.ListByRun(ctx, run.ID)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	beforeTaskIDs := taskIDSet(beforeTasks)
	fact := feedbackFactContext{
		SnapshotID:         snapshot.SnapshotID,
		SnapshotVersionID:  snapshot.SnapshotVersionID,
		FrontierSnapshotID: member.Ref.FrontierSnapshotID,
		RefName:            member.Ref.RefName,
		OutputBagIDs:       append([]string(nil), snapshot.OutputBagIDs...),
		Committed:          true,
	}
	if err := s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, transition.ToState, snapshot.Result, fact); err != nil {
		return activeRefAdvanceResult{}, err
	}
	afterTasks, err := s.tasks.ListByRun(ctx, run.ID)
	if err != nil {
		return activeRefAdvanceResult{}, err
	}
	return activeRefAdvanceResult{
		Consumed:        true,
		Status:          doujiagit.SnapshotProcessingStatusAdvanced,
		DecisionKind:    doujiagit.RefMoveModeAdvance,
		ContinuationID:  string(transition.ToState),
		ProducedTaskIDs: producedTaskIDsSince(beforeTaskIDs, afterTasks),
		Replacement: frontierReplacement{
			RunID:                  run.ID,
			RefName:                member.Ref.RefName,
			FromFrontierSnapshotID: member.Ref.FrontierSnapshotID,
			ConsumedSnapshotIDs:    []string{snapshot.SnapshotID},
			Mode:                   doujiagit.RefMoveModeAdvance,
		},
		ReplacementResult: frontierReplacementResult{ToFrontierSnapshotID: member.Ref.FrontierSnapshotID},
	}, nil
}

func bagBindingsFromSnapshotOutput(nextStage pipeline.StageSpec, snapshot doujiagit.TaskSnapshot) []core.BagBindingRef {
	if len(snapshot.OutputBagIDs) == 0 {
		return nil
	}
	if bindings := outputBagBindingsFromSnapshotRuntime(snapshot); len(bindings) > 0 {
		return bindings
	}
	bindings := make([]core.BagBindingRef, 0, len(snapshot.OutputBagIDs))
	for i, bagID := range snapshot.OutputBagIDs {
		name := ""
		if i < len(nextStage.InputBags) {
			name = nextStage.InputBags[i].Name
		}
		bindings = append(bindings, core.BagBindingRef{Name: name, BagID: bagID})
	}
	return bindings
}

func outputBagBindingsFromSnapshotRuntime(snapshot doujiagit.TaskSnapshot) []core.BagBindingRef {
	if strings.TrimSpace(snapshot.RuntimeContextJSON) == "" {
		return nil
	}
	var runtimeContext taskSnapshotRuntimeContext
	if err := json.Unmarshal([]byte(snapshot.RuntimeContextJSON), &runtimeContext); err != nil {
		return nil
	}
	if len(runtimeContext.OutputBags) == 0 {
		return nil
	}
	out := make([]core.BagBindingRef, 0, len(runtimeContext.OutputBags))
	for _, binding := range runtimeContext.OutputBags {
		name := strings.TrimSpace(binding.Name)
		bagID := strings.TrimSpace(binding.BagID)
		if name == "" || bagID == "" {
			continue
		}
		out = append(out, core.BagBindingRef{
			Name:    name,
			BagID:   bagID,
			Indexes: cloneControlStringMap(binding.Indexes),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func outputBagIDListsFromSnapshotForInstance(transition pipeline.TransitionSpec, result core.TaskResultCode, outputBagIDs []string, instance core.PipelineInstance, snapshot doujiagit.TaskSnapshot) (map[string][]string, error) {
	bindings := outputBagBindingsFromSnapshotRuntime(snapshot)
	if len(bindings) == 0 {
		return nil, nil
	}
	commit := &core.CommitReceipt{Result: result}
	commit.ProducedBags = make([]core.CommittedBagDef, 0, len(bindings))
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			continue
		}
		commit.ProducedBags = append(commit.ProducedBags, core.CommittedBagDef{
			Name:    name,
			Indexes: cloneControlStringMap(binding.Indexes),
		})
	}
	if len(commit.ProducedBags) == 0 {
		return nil, nil
	}
	return outputBagIDListsFromCommitForInstance(transition, result, commit, outputBagIDs, instance)
}

func newFrontierMembers(current []string, consumed []string, produced []string) []string {
	consumedSet := make(map[string]bool)
	for _, id := range doujiagit.NormalizeIDs(consumed) {
		consumedSet[id] = true
	}
	out := make([]string, 0, len(current)+len(produced))
	seen := make(map[string]bool)
	for _, id := range doujiagit.NormalizeIDs(current) {
		if consumedSet[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range doujiagit.NormalizeIDs(produced) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *Service) replaceActiveFrontier(ctx context.Context, replacement frontierReplacement) (frontierReplacementResult, error) {
	repository, err := s.requireDoujiaGit()
	if err != nil {
		return frontierReplacementResult{}, err
	}
	replacement.RefName = strings.TrimSpace(replacement.RefName)
	if replacement.RefName == "" {
		replacement.RefName = doujiagit.DefaultRefName
	}
	if strings.TrimSpace(replacement.Mode) == "" {
		replacement.Mode = doujiagit.RefMoveModeAdvance
	}
	current, err := repository.GetRef(ctx, replacement.RunID, replacement.RefName)
	if err != nil {
		return frontierReplacementResult{}, err
	}
	if replacement.FromFrontierSnapshotID == "" {
		replacement.FromFrontierSnapshotID = current.FrontierSnapshotID
	}
	if err := s.validateFrontierReplacement(ctx, current, replacement); err != nil {
		return frontierReplacementResult{}, err
	}
	nextMembers := newFrontierMembers(current.FrontierMemberSnapshotIDs, replacement.ConsumedSnapshotIDs, replacement.ProducedSnapshotIDs)
	now := s.uniqueTimestamp()
	parentFrontiers := []string(nil)
	if strings.TrimSpace(current.FrontierSnapshotID) != "" {
		parentFrontiers = []string{current.FrontierSnapshotID}
	}
	frontierID := doujiagit.StableFrontierSnapshotID(replacement.RunID, nextMembers, parentFrontiers, now)
	if err := repository.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID:        frontierID,
		RunID:                     replacement.RunID,
		ParentFrontierSnapshotIDs: parentFrontiers,
		TaskSnapshotIDs:           nextMembers,
		CreatedByMode:             replacement.Mode,
		CreatedAt:                 now,
	}); err != nil {
		return frontierReplacementResult{}, err
	}
	_, _, err = repository.MoveRef(ctx, doujiagit.MoveRefRequest{
		Ref: doujiagit.Ref{
			RefName:                   replacement.RefName,
			RunID:                     replacement.RunID,
			FrontierSnapshotID:        frontierID,
			FrontierMemberSnapshotIDs: nextMembers,
			UpdatedAt:                 now,
		},
		ExpectedFrontierSnapshotID: current.FrontierSnapshotID,
		Event: doujiagit.RefMoveEvent{
			EventID:                 doujiagit.StableRefMoveEventID(replacement.RunID, replacement.RefName, parentFrontiers, []string{frontierID}, replacement.Mode, now),
			RunID:                   replacement.RunID,
			RefName:                 replacement.RefName,
			FromFrontierSnapshotIDs: parentFrontiers,
			ToFrontierSnapshotIDs:   []string{frontierID},
			Mode:                    replacement.Mode,
			Reason:                  replacement.Reason,
			CreatedAt:               now,
		},
	})
	if err != nil {
		return frontierReplacementResult{}, err
	}
	return frontierReplacementResult{
		ToFrontierSnapshotID: frontierID,
		NewMemberSnapshotIDs: nextMembers,
	}, nil
}

func (s *Service) validateFrontierReplacement(ctx context.Context, current doujiagit.Ref, replacement frontierReplacement) error {
	currentMembers := make(map[string]bool)
	for _, id := range doujiagit.NormalizeIDs(current.FrontierMemberSnapshotIDs) {
		currentMembers[id] = true
	}
	consumed := doujiagit.NormalizeIDs(replacement.ConsumedSnapshotIDs)
	produced := doujiagit.NormalizeIDs(replacement.ProducedSnapshotIDs)
	if len(consumed) == 0 && len(produced) == 0 {
		return fmt.Errorf("frontier replacement must consume or produce at least one snapshot")
	}
	for _, id := range consumed {
		if !currentMembers[id] {
			return fmt.Errorf("frontier replacement consumed snapshot %q is not in active ref", id)
		}
	}
	for _, id := range produced {
		if _, err := s.doujiaGit.GetSnapshot(ctx, id); err != nil {
			return fmt.Errorf("frontier replacement produced snapshot %q is missing: %w", id, err)
		}
	}
	if replacement.Mode != doujiagit.RefMoveModeRecover {
		seenLogical := make(map[string]string)
		for _, id := range newFrontierMembers(current.FrontierMemberSnapshotIDs, consumed, produced) {
			snapshot, err := s.doujiaGit.GetSnapshot(ctx, id)
			if err != nil {
				return fmt.Errorf("frontier replacement member snapshot %q is missing: %w", id, err)
			}
			logicalID := strings.TrimSpace(snapshot.LogicalSnapshotID)
			if logicalID == "" {
				continue
			}
			if existing := seenLogical[logicalID]; existing != "" && existing != id {
				return fmt.Errorf("frontier replacement would keep multiple versions of logical snapshot %q: %s and %s", logicalID, existing, id)
			}
			seenLogical[logicalID] = id
		}
	}
	return nil
}

func taskIDSet(tasks []core.Task) map[core.TaskID]bool {
	out := make(map[core.TaskID]bool, len(tasks))
	for _, task := range tasks {
		out[task.ID] = true
	}
	return out
}

func producedTaskIDsSince(before map[core.TaskID]bool, after []core.Task) []string {
	out := make([]string, 0)
	for _, task := range after {
		if before[task.ID] {
			continue
		}
		out = append(out, string(task.ID))
	}
	sort.Strings(out)
	return out
}

func (s *Service) recordSnapshotProcessingDecision(ctx context.Context, member activeRefMember, result activeRefAdvanceResult) error {
	if !result.Consumed {
		return nil
	}
	repository, err := s.requireDoujiaGit()
	if err != nil {
		return err
	}
	now := s.uniqueTimestamp()
	return repository.CreateSnapshotProcessingDecision(ctx, doujiagit.SnapshotProcessingDecision{
		RunID:                       member.Snapshot.RunID,
		RefName:                     member.Ref.RefName,
		SnapshotID:                  member.Snapshot.SnapshotID,
		SnapshotVersionID:           member.Snapshot.SnapshotVersionID,
		Status:                      result.Status,
		DecisionKind:                decisionKindForSnapshotProcessing(member.Snapshot, result.DecisionKind),
		ContinuationID:              result.ContinuationID,
		Reason:                      result.Reason,
		ProducedTaskIDs:             result.ProducedTaskIDs,
		ProducedPipelineInstanceIDs: result.ProducedPipelineInstanceIDs,
		ConsumedSnapshotIDs:         result.Replacement.ConsumedSnapshotIDs,
		ProducedSnapshotIDs:         result.Replacement.ProducedSnapshotIDs,
		FromFrontierSnapshotID:      result.Replacement.FromFrontierSnapshotID,
		ToFrontierSnapshotID:        result.ReplacementResult.ToFrontierSnapshotID,
		RecoverTargetSnapshotIDs:    member.Snapshot.RecoverTargetSnapshotIDs,
		ReusableSnapshotIDs:         member.Snapshot.ReusableSnapshotIDs,
		RecoverAnchorSnapshotIDs:    member.Snapshot.RecoverAnchorSnapshotIDs,
		FailedSnapshotID:            member.Snapshot.RecoverFromSnapshotID,
		PreviousAttemptSnapshotIDs:  member.Snapshot.PreviousAttemptSnapshotIDs,
		FailureReportBagIDs:         member.Snapshot.FailureReportBagIDs,
		PreviousOutputBagIDs:        member.Snapshot.PreviousOutputBagIDs,
		RepairTargetTransitionID:    member.Snapshot.RepairTargetTransitionID,
		RepairTargetTaskID:          member.Snapshot.RepairTargetTaskID,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
}

func decisionKindForSnapshotProcessing(snapshot doujiagit.TaskSnapshot, fallback string) string {
	if strings.TrimSpace(snapshot.BranchKind) != "" {
		return strings.TrimSpace(snapshot.BranchKind)
	}
	return fallback
}

func (s *Service) snapshotAlreadyConsumed(ctx context.Context, runID core.RunID, refName string, snapshotID string) (bool, error) {
	repository, err := s.requireDoujiaGit()
	if err != nil {
		return false, err
	}
	decision, err := repository.GetSnapshotProcessingDecision(ctx, runID, refName, snapshotID)
	if err != nil {
		return false, nil
	}
	switch decision.Status {
	case doujiagit.SnapshotProcessingStatusAdvanced, doujiagit.SnapshotProcessingStatusTerminal:
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) consumedSnapshotIDsForFeedback(ctx context.Context, inputBagIDs []string) []string {
	if s.doujiaGit == nil {
		return nil
	}
	out := make([]string, 0)
	for _, bagID := range inputBagIDs {
		snapshotID, err := s.doujiaGit.ProducerOfBag(ctx, bagID)
		if err != nil {
			continue
		}
		out = append(out, snapshotID)
	}
	return doujiagit.NormalizeIDs(out)
}

func (s *Service) moveDoujiaGitRefWithRetry(ctx context.Context, task core.Task, snapshotID string, refName string, now time.Time, moveMode string, consumedSnapshotIDs []string) (string, error) {
	if strings.TrimSpace(moveMode) == "" {
		moveMode = doujiagit.RefMoveModeAdvance
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		var parentFrontierSnapshotIDs []string
		currentMembers := []string(nil)
		if currentRef, err := s.doujiaGit.GetRef(ctx, task.RunID, refName); err == nil {
			currentMembers = append([]string(nil), currentRef.FrontierMemberSnapshotIDs...)
			if strings.TrimSpace(currentRef.FrontierSnapshotID) != "" {
				parentFrontierSnapshotIDs = []string{currentRef.FrontierSnapshotID}
			}
		}
		nextMembers := newFrontierMembers(currentMembers, consumedSnapshotIDs, []string{snapshotID})
		frontierTime := now.Add(time.Duration(attempt) * time.Nanosecond)
		frontierSnapshotID := doujiagit.StableFrontierSnapshotID(task.RunID, nextMembers, parentFrontierSnapshotIDs, frontierTime)
		refMoveEventID := doujiagit.StableRefMoveEventID(
			task.RunID,
			refName,
			parentFrontierSnapshotIDs,
			[]string{frontierSnapshotID},
			doujiagit.RefMoveModeAdvance,
			frontierTime,
		)
		if err := s.doujiaGit.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
			FrontierSnapshotID:        frontierSnapshotID,
			RunID:                     task.RunID,
			ParentFrontierSnapshotIDs: parentFrontierSnapshotIDs,
			TaskSnapshotIDs:           nextMembers,
			CreatedByMode:             moveMode,
			CreatedByEventID:          refMoveEventID,
			CreatedAt:                 frontierTime,
		}); err != nil {
			return "", err
		}
		_, _, err := s.doujiaGit.MoveRef(ctx, doujiagit.MoveRefRequest{
			Ref: doujiagit.Ref{
				RefName:                   doujiagit.DefaultRefName,
				RunID:                     task.RunID,
				FrontierSnapshotID:        frontierSnapshotID,
				FrontierMemberSnapshotIDs: nextMembers,
				UpdatedAt:                 frontierTime,
			},
			ExpectedFrontierSnapshotID: firstString(parentFrontierSnapshotIDs),
			Event: doujiagit.RefMoveEvent{
				EventID:                 refMoveEventID,
				RunID:                   task.RunID,
				RefName:                 refName,
				FromFrontierSnapshotIDs: parentFrontierSnapshotIDs,
				ToFrontierSnapshotIDs:   []string{frontierSnapshotID},
				Mode:                    moveMode,
				Reason:                  fmt.Sprintf("task %s committed", task.ID),
				CreatedAt:               frontierTime,
			},
		})
		if err == nil {
			return frontierSnapshotID, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "compare-and-swap conflict") {
			return "", err
		}
	}
	return "", lastErr
}

func (s *Service) taskSnapshotRuntimeContextJSON(ctx context.Context, task core.Task, feedback core.TaskMetaData, outputBagIDs []string) (string, error) {
	contextValue := taskSnapshotRuntimeContext{
		Task: taskRuntimeSnapshot{
			PipelineInstanceID: task.PipelineInstanceID,
			TransitionID:       task.StageID,
			AgentRole:          task.AgentRole,
			AgentID:            task.AgentID,
			Op:                 task.Op,
		},
		OutputBags: inputBagBindingsFromCommit(feedback.Commit, outputBagIDs),
	}
	if task.PipelineInstanceID != "" && s.instances != nil {
		instance, err := s.instances.Get(ctx, task.RunID, task.PipelineInstanceID)
		if err == nil {
			instance = s.applyTaskSnapshotToInstance(ctx, instance, task, feedback, outputBagIDs)
			contextValue.PipelineInstance = pipelineInstanceRuntimeSnapshotFromCore(instance)
		}
	}
	raw, err := json.Marshal(contextValue)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Service) applyTaskSnapshotToInstance(ctx context.Context, instance core.PipelineInstance, task core.Task, feedback core.TaskMetaData, outputBagIDs []string) core.PipelineInstance {
	if s.definitions == nil || strings.TrimSpace(string(instance.PipelineID)) == "" {
		return instance
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return instance
	}
	transition, ok := findTransition(def, string(task.StageID))
	if !ok {
		return instance
	}
	outputBags, err := outputBagIDsFromCommit(transition, feedback.Result, feedback.Commit, outputBagIDs)
	if err != nil {
		return instance
	}
	if len(outputBags) == 0 {
		outputBags = outputBagIDsForTransition(transition, feedback.Result, outputBagIDs)
	}
	if len(outputBags) > 0 {
		instance = mergeOutputBags(instance, outputBags)
		if instance.InputBagIDs == nil {
			instance.InputBagIDs = make(map[string]string)
		}
		if instance.InputBagIDLists == nil {
			instance.InputBagIDLists = make(map[string][]string)
		}
		for key, value := range outputBags {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			instance.InputBagIDs[key] = value
			instance.InputBagIDLists[key] = appendUniqueString(instance.InputBagIDLists[key], value)
		}
	}
	if feedback.Result == core.TaskResultCodeOK && transitionReachesDelivery(def, transition) {
		instance.Status = core.PipelineInstanceStatusCompleted
	}
	instance.UpdatedAt = time.Now().UTC()
	return instance
}

func transitionReachesDelivery(def pipeline.PipelineDefSpec, transition pipeline.TransitionSpec) bool {
	if transition.ToState == def.DeliveryState {
		return true
	}
	states := pipelineStatesByID(def)
	state, ok := states[transition.ToState]
	return ok && state.Kind == "delivery"
}

func pipelineInstanceRuntimeSnapshotFromCore(instance core.PipelineInstance) *pipelineInstanceRuntimeSnapshot {
	return &pipelineInstanceRuntimeSnapshot{
		ID:                 instance.ID,
		RunID:              instance.RunID,
		PipelineID:         instance.PipelineID,
		ParentID:           clonePipelineInstanceIDPtr(instance.ParentID),
		ParentTransitionID: instance.ParentTransitionID,
		InstanceKey:        instance.InstanceKey,
		Status:             instance.Status,
		Params:             cloneControlStringMap(instance.Params),
		AgentBindings:      cloneAgentBindings(instance.AgentBindings),
		InputBagIDs:        cloneControlStringMap(instance.InputBagIDs),
		InputBagIDLists:    cloneStringSliceMap(instance.InputBagIDLists),
		OutputBagIDs:       cloneControlStringMap(instance.OutputBagIDs),
		OutputBagIDLists:   cloneStringSliceMap(instance.OutputBagIDLists),
		CreatedAt:          instance.CreatedAt,
		UpdatedAt:          instance.UpdatedAt,
	}
}

func (snapshot pipelineInstanceRuntimeSnapshot) toCore() core.PipelineInstance {
	return core.PipelineInstance{
		ID:                 snapshot.ID,
		RunID:              snapshot.RunID,
		PipelineID:         snapshot.PipelineID,
		ParentID:           clonePipelineInstanceIDPtr(snapshot.ParentID),
		ParentTransitionID: snapshot.ParentTransitionID,
		InstanceKey:        snapshot.InstanceKey,
		Status:             snapshot.Status,
		Params:             cloneControlStringMap(snapshot.Params),
		AgentBindings:      cloneAgentBindings(snapshot.AgentBindings),
		InputBagIDs:        cloneControlStringMap(snapshot.InputBagIDs),
		InputBagIDLists:    cloneStringSliceMap(snapshot.InputBagIDLists),
		OutputBagIDs:       cloneControlStringMap(snapshot.OutputBagIDs),
		OutputBagIDLists:   cloneStringSliceMap(snapshot.OutputBagIDLists),
		CreatedAt:          snapshot.CreatedAt,
		UpdatedAt:          snapshot.UpdatedAt,
	}
}

func clonePipelineInstanceIDPtr(in *core.PipelineInstanceID) *core.PipelineInstanceID {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func (s *Service) recordDoujiaGitRecover(ctx context.Context, run core.PipelineRun, failedTask core.Task, debugTask *core.Task, feedback core.TaskMetaData) error {
	if s.doujiaGit == nil {
		return nil
	}
	now := s.uniqueTimestamp()
	refName := doujiagit.DefaultRefName
	currentRef, err := s.doujiaGit.GetRef(ctx, failedTask.RunID, refName)
	if err != nil || strings.TrimSpace(currentRef.FrontierSnapshotID) == "" {
		return nil
	}
	parentFrontierSnapshotIDs := []string{currentRef.FrontierSnapshotID}
	taskSnapshotIDs := append([]string(nil), currentRef.FrontierSnapshotIDs...)
	if len(taskSnapshotIDs) == 0 {
		return nil
	}
	details := recoverDetails{
		Kind:              "kbug_debug_code",
		FailedTaskID:      failedTask.ID,
		TesterAgentID:     failedTask.AgentID,
		KeepBagIDs:        recoverKeepBagIDs(failedTask, feedback),
		FailureReportBags: recoverFailureReportBagIDs(feedback, failedTask.OutputBagIDs),
		Reason:            fmt.Sprintf("task %s returned %s", failedTask.ID, feedback.Result),
	}
	if debugTask != nil {
		details.DebugTaskID = debugTask.ID
		details.CoderAgentID = debugTask.AgentID
		details.DebugInputBagIDs = append([]string(nil), debugTask.InputBagIDs...)
	}
	detailsJSON := marshalDetailsJSON(details)
	frontierSnapshotID := doujiagit.StableFrontierSnapshotID(failedTask.RunID, taskSnapshotIDs, parentFrontierSnapshotIDs, now)
	refMoveEventID := doujiagit.StableRefMoveEventID(
		failedTask.RunID,
		refName,
		parentFrontierSnapshotIDs,
		[]string{frontierSnapshotID},
		doujiagit.RefMoveModeRecover,
		now,
	)
	if err := s.doujiaGit.CreateFrontierSnapshot(ctx, doujiagit.FrontierSnapshot{
		FrontierSnapshotID:        frontierSnapshotID,
		RunID:                     failedTask.RunID,
		ParentFrontierSnapshotIDs: parentFrontierSnapshotIDs,
		TaskSnapshotIDs:           taskSnapshotIDs,
		CreatedByMode:             doujiagit.RefMoveModeRecover,
		CreatedByEventID:          refMoveEventID,
		DetailsJSON:               detailsJSON,
		CreatedAt:                 now,
	}); err != nil {
		return err
	}
	if _, _, err := s.doujiaGit.MoveRef(ctx, doujiagit.MoveRefRequest{
		Ref: doujiagit.Ref{
			RefName:                   refName,
			RunID:                     failedTask.RunID,
			FrontierSnapshotID:        frontierSnapshotID,
			FrontierMemberSnapshotIDs: taskSnapshotIDs,
			UpdatedAt:                 now,
		},
		ExpectedFrontierSnapshotID: firstString(parentFrontierSnapshotIDs),
		Event: doujiagit.RefMoveEvent{
			EventID:                 refMoveEventID,
			RunID:                   failedTask.RunID,
			RefName:                 refName,
			FromFrontierSnapshotIDs: parentFrontierSnapshotIDs,
			ToFrontierSnapshotIDs:   []string{frontierSnapshotID},
			Mode:                    doujiagit.RefMoveModeRecover,
			Reason:                  details.Reason,
			DetailsJSON:             detailsJSON,
			CreatedAt:               now,
		},
	}); err != nil {
		return err
	}
	debugTaskID := core.TaskID("")
	if debugTask != nil {
		debugTaskID = debugTask.ID
	}
	s.recordEvent(ctx, run.ID, failedTask.ID, failedTask.AgentID, "doujiagit_recover", "DoujiaGit recover frontier created", map[string]any{
		"frontier_snapshot_id": frontierSnapshotID,
		"debug_task_id":        debugTaskID,
		"mode":                 doujiagit.RefMoveModeRecover,
	})
	return nil
}

func (s *Service) reachableSnapshotIDsFromRef(ctx context.Context, ref doujiagit.Ref) (map[string]bool, error) {
	out := make(map[string]bool)
	visitedFrontiers := make(map[string]bool)
	var visitFrontier func(frontierID string) error
	visitFrontier = func(frontierID string) error {
		frontierID = strings.TrimSpace(frontierID)
		if frontierID == "" || visitedFrontiers[frontierID] {
			return nil
		}
		visitedFrontiers[frontierID] = true
		frontier, err := s.doujiaGit.GetFrontierSnapshot(ctx, frontierID)
		if err != nil {
			return err
		}
		for _, snapshotID := range frontier.TaskSnapshotIDs {
			if strings.TrimSpace(snapshotID) != "" {
				out[snapshotID] = true
			}
		}
		for _, parentID := range frontier.ParentFrontierSnapshotIDs {
			if err := visitFrontier(parentID); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.TrimSpace(ref.FrontierSnapshotID) != "" {
		if err := visitFrontier(ref.FrontierSnapshotID); err != nil {
			return nil, err
		}
	}
	for _, snapshotID := range ref.FrontierSnapshotIDs {
		if strings.TrimSpace(snapshotID) != "" {
			out[snapshotID] = true
		}
	}
	return out, nil
}

func (s *Service) materializeTasksFromDoujiaGitSnapshots(ctx context.Context, run core.PipelineRun, snapshotIDs map[string]bool) (int, bool, error) {
	snapshots, err := s.doujiaGit.ListSnapshotsByRun(ctx, run.ID)
	if err != nil {
		return 0, false, err
	}
	created := 0
	complete := false
	for _, snapshot := range snapshots {
		if len(snapshotIDs) > 0 && !snapshotIDs[snapshot.SnapshotID] {
			continue
		}
		if _, err := s.tasks.Get(ctx, run.ID, snapshot.TaskID); err == nil {
			if snapshotCompletesRun(run, snapshot, s.pipelines) {
				complete = true
			}
			continue
		}
		inputRefs, err := s.artifactRefsForBags(ctx, snapshot.InputBagIDs)
		if err != nil {
			return 0, false, err
		}
		outputRefs, err := s.artifactRefsForBags(ctx, snapshot.OutputBagIDs)
		if err != nil {
			return 0, false, err
		}
		task := core.Task{
			ID:                 snapshot.TaskID,
			RunID:              run.ID,
			PipelineInstanceID: snapshot.PipelineInstanceID,
			StageID:            stageIDForSnapshot(snapshot),
			AgentRole:          agentRoleForSnapshot(snapshot),
			AgentID:            agentIDForSnapshot(snapshot),
			Op:                 opForSnapshot(snapshot),
			Status:             taskStatusForSnapshot(snapshot),
			Result:             snapshot.Result,
			InputArtifactRefs:  toArtifactRefs(inputRefs),
			OutputArtifactRefs: toArtifactRefs(outputRefs),
			InputBagIDs:        append([]string(nil), snapshot.InputBagIDs...),
			OutputBagIDs:       append([]string(nil), snapshot.OutputBagIDs...),
			CreatedAt:          snapshot.CreatedAt,
			UpdatedAt:          snapshot.CreatedAt,
		}
		if err := s.tasks.Create(ctx, task); err != nil {
			return 0, false, err
		}
		created++
		if s.artifacts != nil {
			for _, uri := range outputRefs {
				if err := s.artifacts.Create(ctx, repo.ArtifactRecord{
					RunID:     run.ID,
					TaskID:    task.ID,
					AgentID:   task.AgentID,
					Kind:      artifactKindFromURI(uri),
					URI:       uri,
					CreatedAt: snapshot.CreatedAt,
				}); err != nil {
					return 0, false, err
				}
			}
		}
		if snapshotCompletesRun(run, snapshot, s.pipelines) {
			complete = true
		}
	}
	return created, complete, nil
}

func (s *Service) materializePipelineInstancesFromTaskSnapshots(ctx context.Context, run core.PipelineRun, snapshotIDs map[string]bool) (int, error) {
	if s.instances == nil {
		return 0, nil
	}
	snapshots, err := s.doujiaGit.ListSnapshotsByRun(ctx, run.ID)
	if err != nil {
		return 0, err
	}
	latestByID := make(map[core.PipelineInstanceID]core.PipelineInstance)
	for _, snapshot := range snapshots {
		if len(snapshotIDs) > 0 && !snapshotIDs[snapshot.SnapshotID] {
			continue
		}
		instance, ok := pipelineInstanceFromSnapshotRuntime(snapshot)
		if !ok {
			continue
		}
		if instance.RunID == "" {
			instance.RunID = run.ID
		}
		if existing, ok := latestByID[instance.ID]; ok && !instance.UpdatedAt.After(existing.UpdatedAt) {
			continue
		}
		latestByID[instance.ID] = instance
	}
	if len(latestByID) == 0 {
		return 0, nil
	}
	if _, hasRoot := latestByID["root"]; !hasRoot && s.definitions != nil {
		root, err := s.ensureRootPipelineInstance(ctx, run)
		if err != nil {
			return 0, err
		}
		latestByID[root.ID] = root
	}
	ordered := orderedPipelineInstances(latestByID)
	created := 0
	for _, instance := range ordered {
		if strings.TrimSpace(string(instance.ID)) == "" {
			continue
		}
		if instance.CreatedAt.IsZero() {
			instance.CreatedAt = time.Now().UTC()
		}
		if instance.UpdatedAt.IsZero() {
			instance.UpdatedAt = instance.CreatedAt
		}
		if instance.Status == "" {
			instance.Status = core.PipelineInstanceStatusRunning
		}
		if _, err := s.instances.Get(ctx, run.ID, instance.ID); err == nil {
			if err := s.instances.Update(ctx, instance); err != nil {
				return 0, err
			}
			continue
		}
		if err := s.instances.Create(ctx, instance); err != nil {
			return 0, err
		}
		created++
	}
	if s.definitions != nil {
		if err := s.replayPipelineInstanceOutputs(ctx, run); err != nil {
			return 0, err
		}
	}
	return created, nil
}

func pipelineInstanceFromSnapshotRuntime(snapshot doujiagit.TaskSnapshot) (core.PipelineInstance, bool) {
	if strings.TrimSpace(snapshot.RuntimeContextJSON) == "" {
		return core.PipelineInstance{}, false
	}
	var runtimeContext taskSnapshotRuntimeContext
	if err := json.Unmarshal([]byte(snapshot.RuntimeContextJSON), &runtimeContext); err != nil {
		return core.PipelineInstance{}, false
	}
	if runtimeContext.PipelineInstance == nil || runtimeContext.PipelineInstance.ID == "" {
		return core.PipelineInstance{}, false
	}
	return runtimeContext.PipelineInstance.toCore(), true
}

func orderedPipelineInstances(instances map[core.PipelineInstanceID]core.PipelineInstance) []core.PipelineInstance {
	out := make([]core.PipelineInstance, 0, len(instances))
	for _, instance := range instances {
		out = append(out, instance)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.ParentID == nil && right.ParentID != nil {
			return true
		}
		if left.ParentID != nil && right.ParentID == nil {
			return false
		}
		if len(left.ID) != len(right.ID) {
			return len(left.ID) < len(right.ID)
		}
		return left.ID < right.ID
	})
	return out
}

func (s *Service) replayPipelineInstanceOutputs(ctx context.Context, run core.PipelineRun) error {
	instances, err := s.instances.ListByRun(ctx, run.ID)
	if err != nil {
		return err
	}
	byID := make(map[core.PipelineInstanceID]core.PipelineInstance, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = instance
	}
	for _, child := range orderedPipelineInstances(byID) {
		if child.ParentID == nil || child.Status != core.PipelineInstanceStatusCompleted {
			continue
		}
		parent, ok := byID[*child.ParentID]
		if !ok {
			continue
		}
		parentDef, err := s.definitions.GetDef(ctx, parent.PipelineID)
		if err != nil {
			continue
		}
		transition, ok := findTransition(parentDef, child.ParentTransitionID)
		if !ok {
			continue
		}
		parent = mergeChildInstanceOutputs(parent, transition, child)
		parent.UpdatedAt = maxTime(parent.UpdatedAt, child.UpdatedAt)
		byID[parent.ID] = parent
	}
	for _, instance := range byID {
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
	}
	return nil
}

func maxTime(left time.Time, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func (s *Service) artifactRefsForBags(ctx context.Context, bagIDs []string) ([]string, error) {
	out := make([]string, 0)
	for _, bagID := range bagIDs {
		if strings.TrimSpace(bagID) == "" {
			continue
		}
		bag, err := s.doujiaGit.GetBag(ctx, bagID)
		if err != nil {
			return nil, err
		}
		for _, versionID := range bag.ArtifactVersionIDs {
			version, err := s.doujiaGit.GetArtifactVersion(ctx, versionID)
			if err != nil {
				return nil, err
			}
			for _, objectID := range version.ObjectIDs {
				object, err := s.doujiaGit.GetObject(ctx, objectID)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(object.StorageURI) != "" {
					out = append(out, object.StorageURI)
				}
			}
		}
	}
	return uniqueStrings(out), nil
}

func taskStatusForSnapshot(snapshot doujiagit.TaskSnapshot) core.TaskStatus {
	switch snapshot.Result {
	case core.TaskResultCodeOK:
		return core.TaskStatusDone
	case core.TaskResultCodeBug, core.TaskResultCodeRewrite, core.TaskResultCodeReplan, core.TaskResultCodeControlInvalid:
		return core.TaskStatusBlocked
	default:
		return core.TaskStatusFailed
	}
}

func stageIDForSnapshot(snapshot doujiagit.TaskSnapshot) core.StageID {
	if snapshot.TransitionID != "" {
		return snapshot.TransitionID
	}
	return core.StageID(snapshot.TaskID)
}

func agentRoleForSnapshot(snapshot doujiagit.TaskSnapshot) core.AgentRole {
	if snapshot.AgentRole != "" {
		return snapshot.AgentRole
	}
	return inferAgentRole(snapshot.TaskID)
}

func agentIDForSnapshot(snapshot doujiagit.TaskSnapshot) core.AgentID {
	if snapshot.AgentID != "" {
		return snapshot.AgentID
	}
	return inferAgentID(snapshot.TaskID)
}

func opForSnapshot(snapshot doujiagit.TaskSnapshot) string {
	if strings.TrimSpace(snapshot.Op) != "" {
		return snapshot.Op
	}
	return opForTaskID(snapshot.TaskID)
}

func snapshotCompletesRun(run core.PipelineRun, snapshot doujiagit.TaskSnapshot, registry pipeline.Registry) bool {
	if snapshot.Result != core.TaskResultCodeOK {
		return false
	}
	if strings.Contains(string(snapshot.TaskID), "_global_test_code") {
		return true
	}
	if registry == nil {
		return false
	}
	spec, err := registry.Get(context.Background(), run.PipelineID)
	if err != nil {
		return false
	}
	stage, ok := findStageByID(spec, core.StageID(snapshot.TaskID))
	if !ok || stage.ID == "" {
		return false
	}
	_, hasNext := findNextStage(spec, stage.ID)
	return !hasNext
}

func inferAgentRole(taskID core.TaskID) core.AgentRole {
	id := string(taskID)
	switch {
	case id == "task_01", id == "task_03":
		return core.AgentRoleCEO
	case id == "task_02", id == "task_05":
		return core.AgentRolePM
	case id == "task_04", id == "task_06", strings.Contains(id, "_merge_code"), strings.Contains(id, "_global_test"):
		return core.AgentRoleArchitect
	case strings.Contains(id, "coder"):
		return core.AgentRoleCoder
	case strings.Contains(id, "tester"):
		return core.AgentRoleTester
	default:
		return ""
	}
}

func inferAgentID(taskID core.TaskID) core.AgentID {
	id := string(taskID)
	switch {
	case id == "task_01", id == "task_03":
		return "ceo"
	case id == "task_02", id == "task_05":
		return "pm01"
	case id == "task_04", id == "task_06", strings.Contains(id, "_merge_code"), strings.Contains(id, "_global_test"):
		return "architect01"
	}
	for _, marker := range []string{"coder", "tester"} {
		index := strings.Index(id, marker)
		if index < 0 {
			continue
		}
		end := index + len(marker)
		for end < len(id) && id[end] >= '0' && id[end] <= '9' {
			end++
		}
		if end > index+len(marker) {
			return core.AgentID(id[index:end])
		}
	}
	return ""
}

func opForTaskID(taskID core.TaskID) string {
	id := string(taskID)
	switch id {
	case "task_01", "task_02", "task_04":
		return core.TaskOpWritePlan
	case "task_03", "task_05":
		return core.TaskOpReviewPlan
	case "task_06":
		return core.TaskOpSplitModule
	}
	return opForTask(core.Task{ID: taskID})
}

func countTasksByStatus(ctx context.Context, tasks repo.TaskRepository, runID core.RunID, status core.TaskStatus) (int, error) {
	if tasks == nil {
		return 0, nil
	}
	items, err := tasks.ListByRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if item.Status == status {
			count++
		}
	}
	return count, nil
}

type recoverDetails struct {
	Kind              string       `json:"kind"`
	FailedTaskID      core.TaskID  `json:"failed_task_id"`
	DebugTaskID       core.TaskID  `json:"debug_task_id"`
	CoderAgentID      core.AgentID `json:"coder_agent_id,omitempty"`
	TesterAgentID     core.AgentID `json:"tester_agent_id,omitempty"`
	KeepBagIDs        []string     `json:"keep_bag_ids,omitempty"`
	FailureReportBags []string     `json:"failure_report_bag_ids,omitempty"`
	DebugInputBagIDs  []string     `json:"debug_input_bag_ids,omitempty"`
	Reason            string       `json:"reason,omitempty"`
}

func recoverKeepBagIDs(task core.Task, feedback core.TaskMetaData) []string {
	failureReports := make(map[string]bool)
	for _, bagID := range recoverFailureReportBagIDs(feedback, task.OutputBagIDs) {
		failureReports[bagID] = true
	}
	out := make([]string, 0, len(task.InputBagIDs))
	for _, bagID := range task.InputBagIDs {
		if strings.TrimSpace(bagID) == "" || failureReports[bagID] {
			continue
		}
		out = append(out, bagID)
	}
	return uniqueStrings(out)
}

func recoverFailureReportBagIDs(feedback core.TaskMetaData, outputBagIDs []string) []string {
	if feedback.Commit == nil {
		return uniqueStrings(outputBagIDs)
	}
	out := make([]string, 0)
	for i, def := range feedback.Commit.EffectiveCommittedBags() {
		if i >= len(outputBagIDs) {
			break
		}
		if strings.TrimSpace(def.Name) == "failure_report" {
			out = append(out, outputBagIDs[i])
		}
	}
	if len(out) == 0 {
		out = append(out, outputBagIDs...)
	}
	return uniqueStrings(out)
}

func marshalDetailsJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func shouldForwardInputBags(task core.Task, feedback core.TaskMetaData, status core.TaskStatus) bool {
	if status != core.TaskStatusDone || feedback.Commit != nil || len(task.InputBagIDs) == 0 {
		return false
	}
	if len(feedback.ArtifactURIs) == 0 {
		return true
	}
	return sameStringSet(artifactRefsToStrings(task.InputArtifactRefs), feedback.ArtifactURIs)
}

func sameStringSet(left []string, right []string) bool {
	left = uniqueStrings(left)
	right = uniqueStrings(right)
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

func (s *Service) recordFeedbackArtifacts(ctx context.Context, task core.Task, feedback core.TaskMetaData) error {
	if s.artifacts == nil {
		return nil
	}
	now := time.Now().UTC()
	agentID := feedback.AgentID
	if agentID == "" {
		agentID = task.AgentID
	}
	for _, uri := range uniqueStrings(feedback.ArtifactURIs) {
		if err := s.artifacts.Create(ctx, repo.ArtifactRecord{
			ID:        string(feedback.RunID) + ":" + uri,
			RunID:     feedback.RunID,
			TaskID:    feedback.TaskID,
			AgentID:   agentID,
			Kind:      artifactKindFromURI(uri),
			URI:       uri,
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func artifactKindFromURI(uri string) string {
	normalized := strings.ReplaceAll(strings.ToLower(uri), "\\", "/")
	switch {
	case strings.Contains(normalized, "/artifacts/requirement/"):
		return "requirement"
	case strings.Contains(normalized, "/artifacts/prd/"), strings.Contains(normalized, "/artifacts/plan/"):
		return "prd"
	case strings.Contains(normalized, "/artifacts/design/"), strings.Contains(normalized, "/artifacts/architecture/"):
		return "architecture"
	case strings.Contains(normalized, "/artifacts/modules/"):
		return "module_task"
	case strings.Contains(normalized, "/artifacts/contracts/"):
		return "contract"
	case strings.Contains(normalized, "/artifacts/seed_tests/"):
		return "seed_tests"
	case strings.Contains(normalized, "/artifacts/test_data/"):
		return "test_data"
	case strings.Contains(normalized, "/artifacts/branches/"):
		return "branch"
	case strings.Contains(normalized, "/artifacts/test_reports/"):
		return "test_report"
	case strings.Contains(normalized, "/artifacts/merge_reports/"):
		return "merge_report"
	case strings.Contains(normalized, "/artifacts/code/"):
		return "code"
	default:
		return "unknown"
	}
}

func isDynamicControlTask(task core.Task) bool {
	return task.ParentID != nil && task.StageID == core.StageID(task.ID)
}

func (s *Service) handleDynamicTaskFeedback(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	if feedback.Result == core.TaskResultCodeBug && task.AgentRole == core.AgentRoleTester && feedback.Op == core.TaskOpTestCode {
		task, err := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusBlocked)
		if err != nil {
			return err
		}
		return s.createCoderDebugFromTestFailure(ctx, run, task, feedback)
	}
	if feedback.Result == core.TaskResultCodeBug && canRetryDynamicTask(task, feedback) {
		task, err := s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusBlocked)
		if err != nil {
			return err
		}
		return s.createDynamicDebugTask(ctx, run, task, feedback)
	}
	if shouldRetryDynamicTaskAfterTransientFailure(task, feedback) {
		return s.retryDynamicTaskAfterTransientFailure(ctx, run, task, feedback)
	}
	if feedback.Result != core.TaskResultCodeOK {
		var err error
		task, err = s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusFailed)
		if err != nil {
			return err
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "run_failed", "run failed", map[string]any{
			"task_id": task.ID,
			"result":  feedback.Result,
		})
		return s.runs.Update(ctx, run)
	}
	var err error
	task, err = s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusDone)
	if err != nil {
		return err
	}
	if isGlobalTestTask(task, feedback) {
		return s.markRunAwaitingAcceptance(ctx, run, task.ID, "global_test_completed")
	}
	return s.dispatchReadyDynamicDependents(ctx, run, task)
}

func (s *Service) dispatchReadyDynamicDependents(ctx context.Context, run core.PipelineRun, completed core.Task) error {
	if completed.ParentID == nil {
		return s.advanceByFacts(ctx, run)
	}
	tasks, err := s.tasks.ListByRun(ctx, run.ID)
	if err != nil {
		return err
	}
	byID := make(map[core.TaskID]core.Task, len(tasks))
	for _, item := range tasks {
		byID[item.ID] = item
	}
	for _, item := range tasks {
		if item.ParentID == nil || *item.ParentID != *completed.ParentID || item.Status != core.TaskStatusPending {
			continue
		}
		ready, inputs, err := readyTaskInputs(item, byID)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		item.InputBagIDs = readyTaskInputBagIDs(item, byID)
		op := item.Op
		if strings.TrimSpace(op) == "" {
			op = opForTask(item)
		}
		if err := s.dispatchTask(ctx, run, item, op, inputs); err != nil {
			return err
		}
	}
	return s.advanceByFacts(ctx, run)
}

func isGlobalTestTask(task core.Task, feedback core.TaskMetaData) bool {
	return feedback.Op == core.TaskOpTestCode && task.AgentRole == core.AgentRoleArchitect
}

func canRetryDynamicTask(task core.Task, feedback core.TaskMetaData) bool {
	return task.AgentRole == core.AgentRoleCoder && feedback.Op == core.TaskOpWriteCode
}

const dynamicTransientRetryMarkerPrefix = "dynamic_transient_retry_count:"

func shouldRetryDynamicTaskAfterTransientFailure(task core.Task, feedback core.TaskMetaData) bool {
	if feedback.Result != core.TaskResultCodeFail || feedback.Op != core.TaskOpTestData {
		return false
	}
	if task.AgentRole != core.AgentRoleTester && task.AgentRole != core.AgentRoleArchitect {
		return false
	}
	if dynamicTransientRetryCount(task) >= 1 {
		return false
	}
	return isTransientAgentFailureFeedback(feedback)
}

func isTransientAgentFailureFeedback(feedback core.TaskMetaData) bool {
	normalized := strings.ToLower(strings.TrimSpace(transientFailureDiagnostics(feedback)))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "context deadline exceeded") ||
		strings.Contains(normalized, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(normalized, "timeout exceeded") ||
		strings.Contains(normalized, "i/o timeout")
}

func transientFailureDiagnostics(feedback core.TaskMetaData) string {
	if feedback.Commit != nil && strings.TrimSpace(feedback.Commit.DiagnosticsJSON) != "" {
		return feedback.Commit.DiagnosticsJSON
	}
	return ""
}

func dynamicTransientRetryCount(task core.Task) int {
	value := strings.TrimSpace(task.ErrorMessage)
	if !strings.HasPrefix(value, dynamicTransientRetryMarkerPrefix) {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimPrefix(value, dynamicTransientRetryMarkerPrefix))
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func setDynamicTransientRetryCount(task *core.Task, count int) {
	task.ErrorMessage = fmt.Sprintf("%s%d", dynamicTransientRetryMarkerPrefix, count)
}

func (s *Service) retryDynamicTaskAfterTransientFailure(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData) error {
	fact, err := s.commitFeedbackFact(ctx, task, feedback, doujiagit.RefMoveModeAdvance)
	if err != nil {
		return err
	}
	retryCount := dynamicTransientRetryCount(task) + 1
	setDynamicTransientRetryCount(&task, retryCount)
	task.Result = feedback.Result
	task.UpdatedAt = time.Now().UTC()
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("retry dynamic task after transient failure: task=%s attempt=%d", task.ID, retryCount))
	}
	s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "task_retry_scheduled", "dynamic task retry scheduled after transient failure", map[string]any{
		"op":            feedback.Op,
		"result":        feedback.Result,
		"retry_count":   retryCount,
		"diagnostics":   transientFailureDiagnostics(feedback),
		"dynamic_task":  true,
		"transient_err": true,
	})
	return s.dispatchTaskWithFact(ctx, run, task, feedback.Op, artifactRefsToStrings(task.InputArtifactRefs), fact)
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
	if feedback.Result != core.TaskResultCodeOK {
		var err error
		task, err = s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusFailed)
		if err != nil {
			return err
		}
		run.Status = core.RunStatusFailed
		run.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("run failed on child task=%s", task.ID))
		}
		s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "run_failed", "run failed on child task", map[string]any{
			"task_id": task.ID,
			"result":  feedback.Result,
		})
		return s.runs.Update(ctx, run)
	}
	var err error
	task, err = s.updateTaskFromFeedback(ctx, task, feedback, core.TaskStatusDone)
	if err != nil {
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
		return s.dispatchReadyDynamicDependents(ctx, run, parent)
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
	return s.createChildTaskWithFact(ctx, run, task, feedback, feedbackFactContext{})
}

func (s *Service) createChildTaskWithFact(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData, fact feedbackFactContext) error {
	upstreamTaskID, ok := primaryTaskDependency(task)
	if !ok {
		return s.failRun(ctx, run, task.ID)
	}

	upstreamTask, err := s.tasks.Get(ctx, run.ID, upstreamTaskID)
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
		Op:                childOp,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	return s.dispatchTaskWithFact(ctx, run, childTask, childOp, inputArtifacts, fact)
}

func (s *Service) createResplitTask(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData, controlErr error) error {
	return s.createResplitTaskWithFact(ctx, run, task, feedback, controlErr, feedbackFactContext{})
}

func (s *Service) createResplitTaskWithFact(ctx context.Context, run core.PipelineRun, task core.Task, feedback core.TaskMetaData, controlErr error, fact feedbackFactContext) error {
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
		Op:                core.TaskOpResplitModule,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("control invalid on task=%s: %v", task.ID, controlErr))
	}
	return s.dispatchTaskWithFact(ctx, run, childTask, core.TaskOpResplitModule, inputArtifacts, fact)
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
		Op:                core.TaskOpDebug,
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
		Op:                core.TaskOpDebug,
		ParentID:          &parentID,
		Status:            core.TaskStatusPending,
		InputArtifactRefs: toArtifactRefs(inputArtifacts),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := s.recordDoujiaGitRecover(ctx, run, testCodeTask, &childTask, feedback); err != nil {
		return err
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
	for _, task := range plans {
		if len(task.DependsOnIDs) != 1 || task.DependsOnIDs[0] != sourceTask.ID {
			continue
		}
		if err := s.dispatchTask(ctx, run, task, task.Op, artifactRefsToStrings(task.InputArtifactRefs)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) expandStartPipelineControls(ctx context.Context, run core.PipelineRun, sourceTask core.Task, feedback core.TaskMetaData) error {
	parentInstance, err := s.ensureRootPipelineInstance(ctx, run)
	if err != nil {
		return err
	}
	parentDef, err := s.definitions.GetDef(ctx, parentInstance.PipelineID)
	if err != nil {
		return err
	}
	callTransitions, err := fromControlTransitionsAfterTask(parentDef, sourceTask, feedback.Result)
	if err != nil {
		return err
	}
	if len(callTransitions) == 0 {
		return fmt.Errorf("task %q produced start_pipeline controls, but no from_control call transition is reachable", sourceTask.ID)
	}

	instancesToStart := make([]core.PipelineInstance, 0, len(feedback.Control))
	for i, control := range feedback.Control {
		if control.Type != core.ControlTypeStartPipeline {
			continue
		}
		control = bindControlInputBagsFromProducedBags(control, feedback.Commit, sourceTask.OutputBagIDs)
		matched := false
		for _, transition := range callTransitions {
			if !matchesStartPipelineControl(transition, control) {
				continue
			}
			matched = true
			called, err := s.definitions.GetDef(ctx, transition.PipelineID)
			if err != nil {
				return err
			}
			instance, err := buildPipelineInstanceFromControl(run.ID, parentInstance, transition, called, control)
			if err != nil {
				return fmt.Errorf("control[%d] start_pipeline %s/%s: %w", i, control.TransitionID, control.InstanceKey, err)
			}
			instancesToStart = append(instancesToStart, instance)
		}
		if !matched {
			return fmt.Errorf("control[%d] start_pipeline transition %q pipeline %q is not reachable after task %q", i, control.TransitionID, control.PipelineID, sourceTask.ID)
		}
	}
	if len(instancesToStart) == 0 {
		return fmt.Errorf("start_pipeline controls did not match reachable from_control transitions after task %q", sourceTask.ID)
	}
	seenInstanceIDs := make(map[core.PipelineInstanceID]bool, len(instancesToStart))
	for _, instance := range instancesToStart {
		if seenInstanceIDs[instance.ID] {
			return fmt.Errorf("start_pipeline produced duplicate pipeline instance id %q", instance.ID)
		}
		seenInstanceIDs[instance.ID] = true
	}
	for _, instance := range instancesToStart {
		if err := s.instances.Create(ctx, instance); err != nil {
			return err
		}
		s.recordEvent(ctx, run.ID, sourceTask.ID, sourceTask.AgentID, "pipeline_instance_created", "pipeline instance created", map[string]any{
			"instance_id":          instance.ID,
			"pipeline_id":          instance.PipelineID,
			"parent_instance_id":   instance.ParentID,
			"parent_transition_id": instance.ParentTransitionID,
			"instance_key":         instance.InstanceKey,
		})
		if err := s.startPipelineInstance(ctx, run, instance); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) startPipelineInstance(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance) error {
	if s.instances == nil || s.definitions == nil {
		return nil
	}
	def, err := s.definitions.GetDef(ctx, instance.PipelineID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if instance.Status == core.PipelineInstanceStatusCreated {
		instance.Status = core.PipelineInstanceStatusRunning
		instance.UpdatedAt = now
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
	}
	return s.enterPipelineState(ctx, run, instance, def, def.StartState)
}

func (s *Service) enterPipelineState(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec, stateID string) error {
	states := pipelineStatesByID(def)
	state, ok := states[stateID]
	if !ok {
		return fmt.Errorf("pipeline %q state %q not found", def.PipelineID, stateID)
	}
	if state.Kind == "aggregate" {
		if !aggregateStateReady(state, &instance, states) {
			return nil
		}
		if err := s.materializeAggregateBags(ctx, run, instance, state, states); err != nil {
			return err
		}
		instance.UpdatedAt = time.Now().UTC()
		if s.instances != nil {
			if err := s.instances.Update(ctx, instance); err != nil {
				return err
			}
		}
	} else if len(state.Exposes.Bags) > 0 {
		instance = applyStateExposes(instance, state)
		instance.UpdatedAt = time.Now().UTC()
		if s.instances != nil {
			if err := s.instances.Update(ctx, instance); err != nil {
				return err
			}
		}
	}
	nextTransitionIDs, err := nextTransitionIDsForState(state, core.TaskResultCodeOK)
	if err != nil {
		return err
	}
	return s.startPipelineTransitions(ctx, run, instance, def, stateID, nextTransitionIDs)
}

func (s *Service) advancePipelineInstanceAfterTransition(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec, stateID string, result core.TaskResultCode) error {
	return s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, stateID, result, feedbackFactContext{})
}

func (s *Service) advancePipelineInstanceAfterTransitionWithFact(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec, stateID string, result core.TaskResultCode, fact feedbackFactContext) error {
	states := pipelineStatesByID(def)
	state, ok := states[stateID]
	if !ok {
		return fmt.Errorf("pipeline %q state %q not found", def.PipelineID, stateID)
	}
	if state.ID == def.DeliveryState || state.Kind == "delivery" {
		instance = applyStateExposes(instance, state)
		return s.completePipelineInstance(ctx, run, instance, def)
	}
	instance = applyStateExposes(instance, state)
	if state.Kind == "aggregate" {
		if !aggregateStateReady(state, &instance, states) {
			return nil
		}
		if err := s.materializeAggregateBags(ctx, run, instance, state, states); err != nil {
			return err
		}
		instance.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
	}
	if state.Kind != "aggregate" {
		instance.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
		if err := s.enterReadyAggregateStates(ctx, run, instance, def); err != nil {
			return err
		}
		latest, err := s.instances.Get(ctx, run.ID, instance.ID)
		if err != nil {
			return err
		}
		instance = latest
	}
	instance = applyStateExposes(instance, state)
	instance = mirrorOutputBagsToInputBags(instance)
	if nextCase, ok, err := nextCaseForResult(state, result); err != nil {
		return err
	} else if ok && nextCase.Ref != nil {
		replaceBags := nextCase.Ref.ReplaceBags
		if len(replaceBags) == 0 && strings.TrimSpace(nextCase.Ref.Mode) == doujiagit.RefMoveModeRecover {
			replaceBags = state.Exposes.Bags
		}
		if len(replaceBags) > 0 {
			instance = applyReplaceBags(instance, replaceBags)
		}
		instance.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
	}
	nextResult := result
	nextStateID, ok, err := enterStateForResult(state, result)
	if err != nil {
		return err
	}
	if ok {
		nextState, exists := states[nextStateID]
		if !exists {
			return fmt.Errorf("pipeline %q state %q not found", def.PipelineID, nextStateID)
		}
		if nextState.Proof.Type == "accepted_result" {
			nextResult = core.TaskResultCode(nextState.Proof.Result)
		}
		return s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, nextStateID, nextResult, fact)
	}
	nextTransitionIDs, err := nextTransitionIDsForState(state, result)
	if err != nil {
		return err
	}
	if len(nextTransitionIDs) == 0 {
		return nil
	}
	return s.startPipelineTransitionsWithFact(ctx, run, instance, def, state.ID, nextTransitionIDs, fact)
}

func applyStateExposes(instance core.PipelineInstance, state pipeline.StateSpec) core.PipelineInstance {
	if len(state.Exposes.Bags) == 0 {
		return instance
	}
	if instance.OutputBagIDs == nil {
		instance.OutputBagIDs = make(map[string]string)
	}
	if instance.OutputBagIDLists == nil {
		instance.OutputBagIDLists = make(map[string][]string)
	}
	if instance.InputBagIDs == nil {
		instance.InputBagIDs = make(map[string]string)
	}
	if instance.InputBagIDLists == nil {
		instance.InputBagIDLists = make(map[string][]string)
	}
	for _, exposed := range state.Exposes.Bags {
		name := strings.TrimSpace(exposed.Name)
		if name == "" {
			continue
		}
		ids := exposedBagIDs(instance, exposed)
		if len(ids) == 0 {
			continue
		}
		indexes := indexesForBagSpec(instance, exposed)
		keys := []string{name}
		if indexedKey := indexedBagLookupKey(name, indexes); indexedKey != name {
			keys = append(keys, indexedKey)
		}
		for _, key := range keys {
			instance.OutputBagIDs[key] = ids[len(ids)-1]
			instance.OutputBagIDLists[key] = uniqueStrings(append(instance.OutputBagIDLists[key], ids...))
			instance.InputBagIDs[key] = ids[len(ids)-1]
			instance.InputBagIDLists[key] = uniqueStrings(append(instance.InputBagIDLists[key], ids...))
		}
	}
	return instance
}

func exposedBagIDs(instance core.PipelineInstance, bag pipeline.BagSpec) []string {
	name := strings.TrimSpace(bag.Name)
	if name == "" {
		return nil
	}
	if indexes := indexesForBagSpec(instance, bag); len(indexes) > 0 {
		indexedKey := indexedBagLookupKey(name, indexes)
		out := append([]string(nil), instance.OutputBagIDLists[indexedKey]...)
		out = append(out, instance.InputBagIDLists[indexedKey]...)
		if value := strings.TrimSpace(instance.OutputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if value := strings.TrimSpace(instance.InputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if len(out) > 0 {
			return uniqueStrings(out)
		}
	}
	return bagIDsForName(instance, name)
}

func (s *Service) startPipelineTransitions(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec, stateID string, transitionIDs []string) error {
	return s.startPipelineTransitionsWithFact(ctx, run, instance, def, stateID, transitionIDs, feedbackFactContext{})
}

func (s *Service) startPipelineTransitionsWithFact(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec, stateID string, transitionIDs []string, fact feedbackFactContext) error {
	transitions := pipelineTransitionsByID(def)
	for _, transitionID := range transitionIDs {
		transition, ok := transitions[transitionID]
		if !ok {
			return fmt.Errorf("pipeline %q transition %q not found", def.PipelineID, transitionID)
		}
		if transition.FromState != stateID {
			return fmt.Errorf("pipeline %q transition %q starts at %q, want %q", def.PipelineID, transition.ID, transition.FromState, stateID)
		}
		switch transition.Kind {
		case "task":
			task, err := buildTaskFromPipelineTransition(run.ID, instance, transition)
			if err != nil {
				return err
			}
			var startTask bool
			var maxed bool
			task, startTask, maxed, err = s.pipelineTransitionAttemptTask(ctx, run.ID, task, transition)
			if err != nil {
				return err
			}
			if maxed {
				s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "task_attempts_exhausted", "task attempts exhausted", map[string]any{
					"pipeline_instance_id": instance.ID,
					"transition_id":        transition.ID,
					"max_attempts":         transition.Limits.MaxAttempts,
				})
				return s.failRun(ctx, run, task.ID)
			}
			if !startTask {
				continue
			}
			if err := s.dispatchTaskWithFact(ctx, run, task, transition.Op, nil, fact); err != nil {
				return err
			}
		case "call":
			if transition.Mode == "foreach" {
				if err := s.startForeachPipelineTransition(ctx, run, instance, def, transition); err != nil {
					return err
				}
				continue
			}
			if transition.Mode != "single" {
				return fmt.Errorf("pipeline %q transition %q call mode %q cannot be started without runtime control yet", def.PipelineID, transition.ID, transition.Mode)
			}
			called, err := s.definitions.GetDef(ctx, transition.PipelineID)
			if err != nil {
				return err
			}
			child, err := buildPipelineInstanceFromSingleCall(run.ID, instance, transition)
			if err != nil {
				return err
			}
			child = normalizeSingleCallInputBagLists(child, called)
			if existing, err := s.instances.Get(ctx, run.ID, child.ID); err == nil {
				if existing.Status == core.PipelineInstanceStatusCreated {
					if err := s.startPipelineInstance(ctx, run, existing); err != nil {
						return err
					}
				}
				continue
			}
			if err := s.instances.Create(ctx, child); err != nil {
				return err
			}
			s.recordEvent(ctx, run.ID, "", "", "pipeline_instance_created", "pipeline instance created", map[string]any{
				"instance_id":          child.ID,
				"pipeline_id":          child.PipelineID,
				"parent_instance_id":   child.ParentID,
				"parent_transition_id": child.ParentTransitionID,
				"instance_key":         child.InstanceKey,
			})
			if err := s.startPipelineInstance(ctx, run, child); err != nil {
				return err
			}
		case "gate":
			if err := s.advancePipelineInstanceAfterTransitionWithFact(ctx, run, instance, def, transition.ToState, core.TaskResultCodeOK, fact); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pipeline %q transition %q kind %q cannot be started", def.PipelineID, transition.ID, transition.Kind)
		}
	}
	return nil
}

func (s *Service) startForeachPipelineTransition(ctx context.Context, run core.PipelineRun, parent core.PipelineInstance, def pipeline.PipelineDefSpec, transition pipeline.TransitionSpec) error {
	if transition.Foreach == nil {
		return fmt.Errorf("pipeline %q transition %q foreach requires foreach spec", def.PipelineID, transition.ID)
	}
	called, err := s.definitions.GetDef(ctx, transition.PipelineID)
	if err != nil {
		return err
	}
	sourceName := strings.TrimSpace(transition.Foreach.ItemsFrom.Name)
	if sourceName == "" {
		sourceName = foreachPrimaryInputBagName(transition)
	}
	itemKey := strings.TrimSpace(transition.Foreach.ItemKey)
	if itemKey == "" {
		return fmt.Errorf("pipeline %q transition %q foreach.item_key is required", def.PipelineID, transition.ID)
	}
	bagIDs := bagIDsForName(parent, sourceName)
	indexesByBagID := indexedBagIndexesByID(parent, sourceName)
	started := false
	for _, bagID := range bagIDs {
		indexes := indexesByBagID[bagID]
		itemValue := strings.TrimSpace(indexes[itemKey])
		if itemValue == "" {
			continue
		}
		child, err := buildPipelineInstanceFromForeachCall(run.ID, parent, transition, called, sourceName, bagID, itemKey, itemValue)
		if err != nil {
			return err
		}
		if existing, err := s.instances.Get(ctx, run.ID, child.ID); err == nil {
			if existing.Status == core.PipelineInstanceStatusCreated {
				if err := s.startPipelineInstance(ctx, run, existing); err != nil {
					return err
				}
			}
			started = true
			continue
		}
		if err := s.instances.Create(ctx, child); err != nil {
			return err
		}
		s.recordEvent(ctx, run.ID, "", "", "pipeline_instance_created", "pipeline instance created", map[string]any{
			"instance_id":          child.ID,
			"pipeline_id":          child.PipelineID,
			"parent_instance_id":   child.ParentID,
			"parent_transition_id": child.ParentTransitionID,
			"instance_key":         child.InstanceKey,
		})
		if err := s.startPipelineInstance(ctx, run, child); err != nil {
			return err
		}
		started = true
	}
	if !started {
		return fmt.Errorf("pipeline %q transition %q foreach found no indexed items in bag %q by %q", def.PipelineID, transition.ID, sourceName, itemKey)
	}
	return nil
}

func foreachPrimaryInputBagName(transition pipeline.TransitionSpec) string {
	if transition.Bindings != nil {
		for _, binding := range transition.Bindings.InputBags {
			if values, ok := binding.(map[string]any); ok {
				if name, ok := stringFromAnyMap(values, "name"); ok {
					return name
				}
			}
		}
	}
	return ""
}

func (s *Service) enterReadyAggregateStates(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec) error {
	for _, state := range def.States {
		if state.Kind != "aggregate" {
			continue
		}
		if err := s.enterPipelineState(ctx, run, instance, def, state.ID); err != nil {
			return err
		}
		if s.instances != nil {
			latest, err := s.instances.Get(ctx, run.ID, instance.ID)
			if err != nil {
				return err
			}
			instance = latest
		}
	}
	return nil
}

func (s *Service) completePipelineInstance(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, def pipeline.PipelineDefSpec) error {
	if instance.Status == core.PipelineInstanceStatusCompleted {
		instance.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, instance); err != nil {
			return err
		}
		return s.onPipelineInstanceCompleted(ctx, run, instance)
	}
	instance.Status = core.PipelineInstanceStatusCompleted
	instance.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, instance); err != nil {
		return err
	}
	s.recordEvent(ctx, run.ID, "", "", "pipeline_instance_completed", "pipeline instance completed", map[string]any{
		"instance_id": instance.ID,
		"pipeline_id": def.PipelineID,
	})
	return s.onPipelineInstanceCompleted(ctx, run, instance)
}

func (s *Service) onPipelineInstanceCompleted(ctx context.Context, run core.PipelineRun, child core.PipelineInstance) error {
	if child.ParentID == nil || s.instances == nil || s.definitions == nil {
		return s.completeRunForRootInstance(ctx, run, child)
	}
	parent, err := s.instances.Get(ctx, run.ID, *child.ParentID)
	if err != nil {
		return err
	}
	parentDef, err := s.definitions.GetDef(ctx, parent.PipelineID)
	if err != nil {
		return err
	}
	transition, ok := findTransition(parentDef, child.ParentTransitionID)
	if !ok {
		return fmt.Errorf("parent pipeline %q transition %q not found for child instance %q", parentDef.PipelineID, child.ParentTransitionID, child.ID)
	}
	parent = mergeChildInstanceOutputs(parent, transition, child)
	childDef, err := s.definitions.GetDef(ctx, child.PipelineID)
	if err != nil {
		return err
	}
	parent = mergeChildInstanceHandlers(parent, transition, child, childDef)

	ready, err := callTransitionReady(ctx, s.instances, run.ID, parent.ID, transition)
	if err != nil {
		return err
	}
	if !ready {
		parent.UpdatedAt = time.Now().UTC()
		return s.instances.Update(ctx, parent)
	}

	if parent.Status == core.PipelineInstanceStatusCompleted {
		parent.UpdatedAt = time.Now().UTC()
		return s.instances.Update(ctx, parent)
	}
	if transition.ToState == parentDef.DeliveryState {
		parent.Status = core.PipelineInstanceStatusCompleted
		parent.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, parent); err != nil {
			return err
		}
		s.recordEvent(ctx, run.ID, "", "", "pipeline_instance_completed", "pipeline instance completed", map[string]any{
			"instance_id": parent.ID,
			"pipeline_id": parent.PipelineID,
		})
		return s.onPipelineInstanceCompleted(ctx, run, parent)
	}

	parent.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, parent); err != nil {
		return err
	}
	if err := s.enterPipelineState(ctx, run, parent, parentDef, transition.ToState); err != nil {
		return err
	}
	latest, err := s.instances.Get(ctx, run.ID, parent.ID)
	if err != nil {
		return err
	}
	return s.enterReadyAggregateStates(ctx, run, latest, parentDef)
}

func (s *Service) completeRunForRootInstance(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance) error {
	if instance.ParentID != nil || instance.Status != core.PipelineInstanceStatusCompleted {
		return nil
	}
	latest, err := s.runs.Get(ctx, run.ID)
	if err != nil {
		return err
	}
	if latest.Status == core.RunStatusCompleted || latest.Status == core.RunStatusFailed || latest.Status == core.RunStatusAwaitingAcceptance {
		return nil
	}
	return s.markRunAwaitingAcceptance(ctx, latest, "", "root_instance_completed")
}

func (s *Service) markRunAwaitingAcceptance(ctx context.Context, run core.PipelineRun, taskID core.TaskID, reason string) error {
	latest, err := s.runs.Get(ctx, run.ID)
	if err != nil {
		return err
	}
	if latest.Status == core.RunStatusCompleted || latest.Status == core.RunStatusFailed {
		return nil
	}
	now := time.Now().UTC()
	checkpointTaskID := core.TaskID(fmt.Sprintf("acceptance_iter_%02d", max(1, latest.CurrentIterationNo)))
	latest.Status = core.RunStatusAwaitingAcceptance
	latest.LatestAcceptanceCheckpointTaskID = checkpointTaskID
	latest.UpdatedAt = now
	if err := s.runs.Update(ctx, latest); err != nil {
		return err
	}
	if s.iterations != nil {
		iterations, _ := s.iterations.ListByRun(ctx, run.ID)
		for _, iteration := range iterations {
			if iteration.IterationNo != latest.CurrentIterationNo {
				continue
			}
			iteration.DeliveryFrontierID = latest.LatestDeliveryFrontierID
			iteration.AcceptanceCheckpointTaskID = checkpointTaskID
			iteration.Status = core.RunStatusAwaitingAcceptance
			iteration.UpdatedAt = now
			_ = s.iterations.Update(ctx, iteration)
			break
		}
	}
	if _, err := s.tasks.Get(ctx, run.ID, checkpointTaskID); err != nil {
		_ = s.tasks.Create(ctx, core.Task{
			ID:        checkpointTaskID,
			RunID:     run.ID,
			StageID:   "acceptance",
			AgentRole: core.AgentRoleCEO,
			AgentID:   "ceo",
			Op:        "acceptance_checkpoint",
			Status:    core.TaskStatusWaitingExternal,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if s.logger != nil {
		_ = s.logger.Log(run.ID, "Orchestrator", "run awaiting acceptance")
	}
	s.recordEvent(ctx, run.ID, taskID, "", "run_awaiting_acceptance", "run awaiting acceptance", map[string]any{
		"task_id":              taskID,
		"acceptance_task_id":   checkpointTaskID,
		"current_iteration_no": latest.CurrentIterationNo,
		"reason":               reason,
	})
	return nil
}

func (s *Service) ensureRootPipelineInstance(ctx context.Context, run core.PipelineRun) (core.PipelineInstance, error) {
	rootID := core.PipelineInstanceID("root")
	if s.instances == nil || s.definitions == nil {
		return core.PipelineInstance{ID: rootID, RunID: run.ID, PipelineID: run.PipelineID}, nil
	}
	if existing, err := s.instances.Get(ctx, run.ID, rootID); err == nil {
		return existing, nil
	}
	def, err := s.definitions.GetDef(ctx, run.PipelineID)
	if err != nil {
		return core.PipelineInstance{ID: rootID, RunID: run.ID, PipelineID: run.PipelineID}, nil
	}
	now := time.Now().UTC()
	instance := core.PipelineInstance{
		ID:            rootID,
		RunID:         run.ID,
		PipelineID:    run.PipelineID,
		InstanceKey:   "root",
		Status:        core.PipelineInstanceStatusRunning,
		AgentBindings: defaultAgentBindings(def.Namespace),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.instances.Create(ctx, instance); err != nil {
		return core.PipelineInstance{}, err
	}
	return instance, nil
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
				Op:                core.TaskOpWriteCode,
				ParentID:          &parentID,
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
				Op:                core.TaskOpTestData,
				ParentID:          &parentID,
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
				Op:                core.TaskOpTestCode,
				ParentID:          &parentID,
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
			Op:           core.TaskOpMergeCode,
			ParentID:     &parentID,
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
			Op:           core.TaskOpTestData,
			ParentID:     &parentID,
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
			Op:           core.TaskOpTestCode,
			ParentID:     &parentID,
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

func fromControlTransitionsAfterTask(def pipeline.PipelineDefSpec, sourceTask core.Task, result core.TaskResultCode) ([]pipeline.TransitionSpec, error) {
	states := make(map[string]pipeline.StateSpec, len(def.States))
	for _, state := range def.States {
		states[state.ID] = state
	}
	transitions := make(map[string]pipeline.TransitionSpec, len(def.Transitions))
	for _, transition := range def.Transitions {
		transitions[transition.ID] = transition
	}
	sourceTransition, ok := transitions[string(sourceTask.StageID)]
	if !ok {
		return nil, fmt.Errorf("source task stage %q not found in pipeline %q", sourceTask.StageID, def.PipelineID)
	}
	toState, ok := states[sourceTransition.ToState]
	if !ok {
		return nil, fmt.Errorf("source task %q to_state %q not found", sourceTask.StageID, sourceTransition.ToState)
	}
	nextTransitionIDs, err := nextTransitionIDsForState(toState, result)
	if err != nil {
		return nil, err
	}
	out := make([]pipeline.TransitionSpec, 0, len(nextTransitionIDs))
	for _, transitionID := range nextTransitionIDs {
		transition, ok := transitions[transitionID]
		if !ok {
			return nil, fmt.Errorf("transition %q not found in pipeline %q", transitionID, def.PipelineID)
		}
		if transition.Kind == "call" && transition.Mode == "from_control" {
			out = append(out, transition)
		}
	}
	return out, nil
}

func nextTransitionIDsForState(state pipeline.StateSpec, result core.TaskResultCode) ([]string, error) {
	if state.Next == nil {
		return nil, nil
	}
	switch state.Next.Type {
	case "all":
		return append([]string(nil), state.Next.Transitions...), nil
	case "by_result":
		if state.Next.Cases == nil {
			return nil, nil
		}
		nextCase, ok := state.Next.Cases[string(result)]
		if !ok {
			return nil, nil
		}
		return append([]string(nil), nextCase.Transitions...), nil
	default:
		return nil, fmt.Errorf("state %q next.type %q is unsupported for start_pipeline expansion", state.ID, state.Next.Type)
	}
}

func enterStateForResult(state pipeline.StateSpec, result core.TaskResultCode) (string, bool, error) {
	nextCase, ok, err := nextCaseForResult(state, result)
	if err != nil || !ok {
		return "", false, err
	}
	enterState := strings.TrimSpace(nextCase.EnterState)
	return enterState, enterState != "", nil
}

func nextCaseForResult(state pipeline.StateSpec, result core.TaskResultCode) (pipeline.NextCase, bool, error) {
	if state.Next == nil || state.Next.Type != "by_result" || state.Next.Cases == nil {
		return pipeline.NextCase{}, false, nil
	}
	nextCase, ok := state.Next.Cases[string(result)]
	if !ok {
		return pipeline.NextCase{}, false, nil
	}
	if strings.TrimSpace(nextCase.Action) == "fail_run" {
		return pipeline.NextCase{}, false, fmt.Errorf("state %q result %q requested fail_run", state.ID, result)
	}
	return nextCase, true, nil
}

func nextDebugTaskForRecover(runID core.RunID, instance core.PipelineInstance, def pipeline.PipelineDefSpec, completed pipeline.TransitionSpec, result core.TaskResultCode) (core.Task, bool, error) {
	states := pipelineStatesByID(def)
	state, ok := states[completed.ToState]
	if !ok {
		return core.Task{}, false, fmt.Errorf("pipeline %q state %q not found", def.PipelineID, completed.ToState)
	}
	visited := make(map[string]bool)
	return nextDebugTaskForRecoverState(runID, instance, def, state, result, visited)
}

func nextDebugTaskForRecoverState(runID core.RunID, instance core.PipelineInstance, def pipeline.PipelineDefSpec, state pipeline.StateSpec, result core.TaskResultCode, visited map[string]bool) (core.Task, bool, error) {
	if visited[state.ID] {
		return core.Task{}, false, nil
	}
	visited[state.ID] = true

	states := pipelineStatesByID(def)
	if nextStateID, ok, err := enterStateForResult(state, result); err != nil {
		return core.Task{}, false, err
	} else if ok {
		nextState, exists := states[nextStateID]
		if !exists {
			return core.Task{}, false, fmt.Errorf("pipeline %q state %q not found", def.PipelineID, nextStateID)
		}
		nextResult := result
		if nextState.Proof.Type == "accepted_result" {
			nextResult = core.TaskResultCode(nextState.Proof.Result)
		}
		return nextDebugTaskForRecoverState(runID, instance, def, nextState, nextResult, visited)
	}

	transitionIDs, err := nextTransitionIDsForState(state, result)
	if err != nil {
		return core.Task{}, false, err
	}
	transitions := pipelineTransitionsByID(def)
	for _, transitionID := range transitionIDs {
		transition, ok := transitions[transitionID]
		if !ok {
			return core.Task{}, false, fmt.Errorf("pipeline %q transition %q not found", def.PipelineID, transitionID)
		}
		if transition.Kind != "task" || !strings.Contains(strings.ToLower(transition.Op), "debug") {
			continue
		}
		task, err := buildTaskFromPipelineTransition(runID, instance, transition)
		if err != nil {
			return core.Task{}, false, err
		}
		return task, true, nil
	}
	return core.Task{}, false, nil
}

func pipelineHasLocalRecover(def pipeline.PipelineDefSpec, transition pipeline.TransitionSpec, result core.TaskResultCode) bool {
	states := pipelineStatesByID(def)
	state, ok := states[transition.ToState]
	if !ok || state.Next == nil || state.Next.Type != "by_result" {
		return false
	}
	nextCase, ok := state.Next.Cases[string(result)]
	if !ok {
		return false
	}
	return len(nextCase.Transitions) > 0 || strings.TrimSpace(nextCase.EnterState) != "" || nextCase.Ref != nil
}

func (s *Service) bubbleExceptionFrame(ctx context.Context, run core.PipelineRun, origin core.PipelineInstance, def pipeline.PipelineDefSpec, transition pipeline.TransitionSpec, task core.Task, feedback core.TaskMetaData) (bool, error) {
	frame := core.ExceptionFrame{
		ID:                       exceptionFrameID(task),
		RunID:                    run.ID,
		Result:                   feedback.Result,
		OriginTaskID:             task.ID,
		OriginPipelineInstanceID: origin.ID,
		FailedTransitionID:       transition.ID,
		FailedInputBags:          inputBagsForException(task, feedback),
		FailureBags:              failureBagsForException(transition, feedback),
		ResumeTransitionID:       transition.ID,
		CreatedAt:                time.Now().UTC(),
	}
	origin.ExceptionFrames = append(origin.ExceptionFrames, frame)
	origin.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, origin); err != nil {
		return false, err
	}
	if origin.ParentID == nil {
		return false, nil
	}
	parent, err := s.instances.Get(ctx, run.ID, *origin.ParentID)
	if err != nil {
		return false, err
	}
	selected := selectHandlerBindingsForException(parent, frame)
	if len(selected) == 0 {
		return false, nil
	}
	frame.SelectedHandlers = selected
	parent.ExceptionFrames = append(parent.ExceptionFrames, frame)
	parent.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, parent); err != nil {
		return false, err
	}
	for _, binding := range selected {
		if err := s.dispatchRepairHandler(ctx, run, binding, frame); err != nil {
			return false, err
		}
	}
	return true, nil
}

func exceptionFrameID(task core.Task) core.ExceptionFrameID {
	result := task.Result
	if result == "" {
		result = core.TaskResultCodeBug
	}
	return core.ExceptionFrameID(fmt.Sprintf("%s_exception_%s", task.ID, sanitizeIDPart(string(result))))
}

func inputBagsForException(task core.Task, feedback core.TaskMetaData) []core.BagBindingRef {
	if len(feedback.InputBags) > 0 {
		return append([]core.BagBindingRef(nil), feedback.InputBags...)
	}
	if len(task.InputBags) > 0 {
		return append([]core.BagBindingRef(nil), task.InputBags...)
	}
	return runtime.InputBagBindingsFromIDs(task.InputBagIDs)
}

func failureBagsForException(transition pipeline.TransitionSpec, feedback core.TaskMetaData) []core.BagBindingRef {
	if feedback.Commit == nil || len(feedback.Commit.ProducedBags) == 0 {
		return nil
	}
	outputIDs := outputBagIDsForTransition(transition, feedback.Result, feedback.Commit.MaterializedOutputRefs)
	out := make([]core.BagBindingRef, 0, len(feedback.Commit.ProducedBags))
	for _, bag := range feedback.Commit.ProducedBags {
		bagID := ""
		bagID = outputIDs[bag.Name]
		if bagID == "" && len(feedback.Commit.ProducedBags) == 1 {
			bagID = strings.TrimSpace(firstString(feedback.Commit.MaterializedOutputRefs))
		}
		out = append(out, core.BagBindingRef{Name: bag.Name, BagID: bagID, Indexes: cloneControlStringMap(bag.Indexes)})
	}
	return out
}

func (s *Service) dispatchRepairHandler(ctx context.Context, run core.PipelineRun, binding core.HandlerBindingRef, frame core.ExceptionFrame) error {
	owner, err := s.instances.Get(ctx, run.ID, binding.OwnerInstanceID)
	if err != nil {
		return err
	}
	def, err := s.definitions.GetDef(ctx, owner.PipelineID)
	if err != nil {
		return err
	}
	handler, ok := handlerSpecByName(def, binding.FromHandler)
	if !ok {
		handler, ok = handlerSpecByName(def, binding.Name)
	}
	if !ok {
		return fmt.Errorf("handler %q not found in pipeline %q", binding.Name, def.PipelineID)
	}
	transition, ok := findTransition(def, handler.Transition)
	if !ok {
		return fmt.Errorf("handler transition %q not found in pipeline %q", handler.Transition, def.PipelineID)
	}
	task, err := buildTaskFromPipelineTransition(run.ID, owner, transition)
	if err != nil {
		return err
	}
	task.ExecutionMode = core.ExecutionModeRepair
	task.ExceptionFrameID = frame.ID
	task.InputBags = repairTaskInputBags(owner, handler, frame)
	task.InputBagIDs = bagIDsFromBindings(task.InputBags)
	return s.dispatchTask(ctx, run, task, transition.Op, nil)
}

func (s *Service) handleRepairHandlerSuccess(ctx context.Context, run core.PipelineRun, owner core.PipelineInstance, def pipeline.PipelineDefSpec, transition pipeline.TransitionSpec, task core.Task, feedback core.TaskMetaData) (bool, error) {
	frame, parent, ok, err := s.findExceptionFrameOwner(ctx, run.ID, task.ExceptionFrameID)
	if err != nil || !ok {
		return false, err
	}
	outputBags, err := outputBagIDsFromCommitForInstance(transition, feedback.Result, feedback.Commit, task.OutputBagIDs, owner)
	if err != nil {
		return false, err
	}
	if len(outputBags) == 0 {
		outputBags = outputBagIDsForTransition(transition, feedback.Result, task.OutputBagIDs)
	}
	owner = mergeOutputBags(owner, outputBags)
	owner.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, owner); err != nil {
		return false, err
	}
	if parent.ID != "" {
		parent = replaceParentHandlerBags(parent, owner)
		parent.UpdatedAt = time.Now().UTC()
		if err := s.instances.Update(ctx, parent); err != nil {
			return false, err
		}
	}
	if frame.OriginPipelineInstanceID == "" || frame.ResumeTransitionID == "" {
		return true, nil
	}
	origin, err := s.instances.Get(ctx, run.ID, frame.OriginPipelineInstanceID)
	if err != nil {
		return false, err
	}
	originDef, err := s.definitions.GetDef(ctx, origin.PipelineID)
	if err != nil {
		return false, err
	}
	failedTransition, ok := findTransition(originDef, frame.ResumeTransitionID)
	if !ok {
		return false, fmt.Errorf("resume transition %q not found in pipeline %q", frame.ResumeTransitionID, origin.PipelineID)
	}
	origin = mergeRepairOutputsIntoOriginInputs(origin, owner, outputBags)
	origin.UpdatedAt = time.Now().UTC()
	if err := s.instances.Update(ctx, origin); err != nil {
		return false, err
	}
	retryTask, err := buildTaskFromPipelineTransition(run.ID, origin, failedTransition)
	if err != nil {
		return false, err
	}
	return true, s.dispatchTask(ctx, run, retryTask, failedTransition.Op, nil)
}

func (s *Service) findExceptionFrameOwner(ctx context.Context, runID core.RunID, frameID core.ExceptionFrameID) (core.ExceptionFrame, core.PipelineInstance, bool, error) {
	instances, err := s.instances.ListByRun(ctx, runID)
	if err != nil {
		return core.ExceptionFrame{}, core.PipelineInstance{}, false, err
	}
	for _, instance := range instances {
		for _, frame := range instance.ExceptionFrames {
			if frame.ID == frameID && len(frame.SelectedHandlers) > 0 {
				return frame, instance, true, nil
			}
		}
	}
	for _, instance := range instances {
		for _, frame := range instance.ExceptionFrames {
			if frame.ID == frameID {
				return frame, core.PipelineInstance{}, true, nil
			}
		}
	}
	return core.ExceptionFrame{}, core.PipelineInstance{}, false, nil
}

func replaceParentHandlerBags(parent core.PipelineInstance, owner core.PipelineInstance) core.PipelineInstance {
	for i, binding := range parent.HandlerBindings {
		if binding.OwnerInstanceID != owner.ID {
			continue
		}
		for j, replace := range binding.Replaces {
			ids := bagIDsForName(owner, replace.Name)
			if len(ids) == 0 {
				continue
			}
			binding.Replaces[j].BagID = ids[len(ids)-1]
		}
		parent.HandlerBindings[i] = binding
	}
	return parent
}

func mergeRepairOutputsIntoOriginInputs(origin core.PipelineInstance, owner core.PipelineInstance, outputBags map[string]string) core.PipelineInstance {
	if origin.InputBagIDs == nil {
		origin.InputBagIDs = make(map[string]string)
	}
	if origin.InputBagIDLists == nil {
		origin.InputBagIDLists = make(map[string][]string)
	}
	if origin.OutputBagIDs == nil {
		origin.OutputBagIDs = make(map[string]string)
	}
	if origin.OutputBagIDLists == nil {
		origin.OutputBagIDLists = make(map[string][]string)
	}
	for name, bagID := range outputBags {
		name = strings.TrimSpace(name)
		bagID = strings.TrimSpace(bagID)
		if name == "" || bagID == "" {
			continue
		}
		replaceInstanceBag(&origin, name, bagID)
		baseName := name
		if parsedName, _, ok := parseIndexedBagLookupKey(name); ok {
			baseName = parsedName
			replaceInstanceBag(&origin, baseName, bagID)
		}
		for key, value := range owner.OutputBagIDs {
			indexedName, indexes, ok := parseIndexedBagLookupKey(key)
			if !ok || indexedName != baseName || value != bagID {
				continue
			}
			indexedKey := indexedBagLookupKey(baseName, indexes)
			replaceInstanceBag(&origin, indexedKey, bagID)
		}
	}
	return origin
}

func replaceInstanceBag(instance *core.PipelineInstance, key string, bagID string) {
	key = strings.TrimSpace(key)
	bagID = strings.TrimSpace(bagID)
	if key == "" || bagID == "" {
		return
	}
	if instance.InputBagIDs == nil {
		instance.InputBagIDs = make(map[string]string)
	}
	if instance.InputBagIDLists == nil {
		instance.InputBagIDLists = make(map[string][]string)
	}
	if instance.OutputBagIDs == nil {
		instance.OutputBagIDs = make(map[string]string)
	}
	if instance.OutputBagIDLists == nil {
		instance.OutputBagIDLists = make(map[string][]string)
	}
	instance.InputBagIDs[key] = bagID
	instance.InputBagIDLists[key] = []string{bagID}
	instance.OutputBagIDs[key] = bagID
	instance.OutputBagIDLists[key] = []string{bagID}
}

func handlerSpecByName(def pipeline.PipelineDefSpec, name string) (pipeline.PipelineHandlerSpec, bool) {
	for _, handler := range def.Handlers {
		if strings.TrimSpace(handler.Name) == strings.TrimSpace(name) {
			return handler, true
		}
	}
	return pipeline.PipelineHandlerSpec{}, false
}

func repairTaskInputBags(owner core.PipelineInstance, handler pipeline.PipelineHandlerSpec, frame core.ExceptionFrame) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0)
	for _, input := range handler.InputBags {
		switch input.Source {
		case "exception":
			out = append(out, filterBindingsByName(frame.FailureBags, input.Name)...)
		case "owner_input":
			out = append(out, bagBindingsForSpec(owner, pipeline.BagSpec{Name: input.Name})...)
		case "owner_output":
			out = append(out, bagBindingsForSpec(owner, pipeline.BagSpec{Name: input.Name})...)
		default:
			out = append(out, bagBindingsForSpec(owner, pipeline.BagSpec{Name: input.Name})...)
		}
	}
	if len(out) == 0 {
		out = append(out, frame.FailureBags...)
	}
	return uniqueBagBindings(out)
}

func filterBindingsByName(bindings []core.BagBindingRef, name string) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0)
	for _, binding := range bindings {
		if strings.TrimSpace(binding.Name) == strings.TrimSpace(name) {
			out = append(out, binding)
		}
	}
	return out
}

func uniqueBagBindings(bindings []core.BagBindingRef) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0, len(bindings))
	seen := make(map[string]bool)
	for _, binding := range bindings {
		key := binding.Name + "\x00" + binding.BagID + "\x00" + indexedBagLookupKey("", binding.Indexes)
		if strings.TrimSpace(binding.BagID) == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, binding)
	}
	return out
}

func bagIDsFromBindings(bindings []core.BagBindingRef) []string {
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.BagID) != "" {
			out = append(out, binding.BagID)
		}
	}
	return uniqueStrings(out)
}

func matchesStartPipelineControl(transition pipeline.TransitionSpec, control core.Control) bool {
	if control.Type != core.ControlTypeStartPipeline {
		return false
	}
	if strings.TrimSpace(control.TransitionID) != transition.ID {
		return false
	}
	if control.PipelineID != transition.PipelineID {
		return false
	}
	if transition.Control != nil {
		if strings.TrimSpace(transition.Control.Type) != "" && strings.TrimSpace(transition.Control.Type) != string(control.Type) {
			return false
		}
		if strings.TrimSpace(transition.Control.TransitionID) != "" && strings.TrimSpace(transition.Control.TransitionID) != control.TransitionID {
			return false
		}
		if transition.Control.PipelineID != "" && transition.Control.PipelineID != control.PipelineID {
			return false
		}
	}
	return true
}

func bindControlInputBagsFromProducedBags(control core.Control, commit *core.CommitReceipt, outputBagIDs []string) core.Control {
	if commit == nil || len(control.InputBags) == 0 {
		return control
	}
	bagIDsByKey := commitOutputBagIDsByName(commit, outputBagIDs)
	if len(bagIDsByKey) == 0 {
		return control
	}
	next := control
	next.InputBags = make(map[string]string, len(control.InputBags))
	for key, value := range control.InputBags {
		lookupKey := strings.TrimSpace(value)
		if lookupKey == "" {
			lookupKey = strings.TrimSpace(key)
		}
		if bagID := controlOutputBagIDByLookup(bagIDsByKey, lookupKey, control.Params); bagID != "" {
			next.InputBags[key] = bagID
			continue
		}
		next.InputBags[key] = value
	}
	return next
}

func controlOutputBagIDByLookup(bagIDsByKey map[string]string, lookupKey string, params map[string]string) string {
	lookupKey = strings.TrimSpace(lookupKey)
	if lookupKey == "" {
		return ""
	}
	if moduleKey := strings.TrimSpace(params["module_key"]); moduleKey != "" && !strings.Contains(lookupKey, "[") {
		indexedKey := indexedBagLookupKey(lookupKey, map[string]string{"module_key": moduleKey})
		if bagID := strings.TrimSpace(bagIDsByKey[indexedKey]); bagID != "" {
			return bagID
		}
	}
	return strings.TrimSpace(bagIDsByKey[lookupKey])
}

func commitOutputBagIDsByName(commit *core.CommitReceipt, outputBagIDs []string) map[string]string {
	if commit == nil || len(outputBagIDs) == 0 {
		return nil
	}
	bags := commit.EffectiveCommittedBags()
	if len(bags) == 0 {
		return nil
	}
	out := make(map[string]string, len(bags))
	for i, def := range bags {
		if i >= len(outputBagIDs) {
			break
		}
		key := strings.TrimSpace(def.Name)
		if key == "" {
			continue
		}
		out[indexedBagLookupKey(key, def.Indexes)] = outputBagIDs[i]
		if len(def.Indexes) == 0 {
			out[key] = outputBagIDs[i]
		} else if _, exists := out[key]; !exists {
			out[key] = outputBagIDs[i]
		}
	}
	return out
}

func inputBagBindingsFromCommit(commit *core.CommitReceipt, outputBagIDs []string) []core.BagBindingRef {
	if commit == nil || len(outputBagIDs) == 0 {
		return nil
	}
	bags := commit.EffectiveCommittedBags()
	if len(bags) == 0 {
		return nil
	}
	out := make([]core.BagBindingRef, 0, len(bags))
	for i, def := range bags {
		if i >= len(outputBagIDs) {
			break
		}
		name := strings.TrimSpace(def.Name)
		bagID := strings.TrimSpace(outputBagIDs[i])
		if name == "" || bagID == "" {
			continue
		}
		out = append(out, core.BagBindingRef{
			Name:    name,
			BagID:   bagID,
			Indexes: cloneControlStringMap(def.Indexes),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func legacyNextTaskInputBags(nextStage pipeline.StageSpec, task core.Task, feedback core.TaskMetaData, tasks []core.Task) []core.BagBindingRef {
	if len(nextStage.InputBags) == 0 {
		return forwardedInputBags(task, feedback)
	}
	available := legacyAvailableBagBindings(task, feedback, tasks)
	out := make([]core.BagBindingRef, 0, len(nextStage.InputBags))
	seen := make(map[string]bool)
	for _, spec := range nextStage.InputBags {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		for _, binding := range available[name] {
			key := binding.Name + "\x00" + binding.BagID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, binding)
			if !spec.Collection {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func legacyNextTaskInputBagIDs(nextStage pipeline.StageSpec, task core.Task, feedback core.TaskMetaData, inputBags []core.BagBindingRef) []string {
	if len(nextStage.InputBags) == 0 {
		return append([]string(nil), task.OutputBagIDs...)
	}
	out := make([]string, 0, len(inputBags))
	for _, binding := range inputBags {
		if strings.TrimSpace(binding.BagID) != "" {
			out = append(out, binding.BagID)
		}
	}
	if len(out) == 0 {
		for _, binding := range legacyNextTaskInputBags(nextStage, task, feedback, nil) {
			out = append(out, binding.BagID)
		}
	}
	return uniqueStrings(out)
}

func legacyAvailableBagBindings(task core.Task, feedback core.TaskMetaData, tasks []core.Task) map[string][]core.BagBindingRef {
	out := make(map[string][]core.BagBindingRef)
	add := func(binding core.BagBindingRef) {
		name := strings.TrimSpace(binding.Name)
		bagID := strings.TrimSpace(binding.BagID)
		if name == "" || bagID == "" {
			return
		}
		binding.Name = name
		binding.BagID = bagID
		out[name] = append(out[name], binding)
	}
	for _, item := range tasks {
		for _, binding := range item.InputBags {
			add(binding)
		}
		for _, binding := range legacyOutputBagBindingsFromTask(item) {
			add(binding)
		}
	}
	for _, binding := range task.InputBags {
		add(binding)
	}
	for _, binding := range inputBagBindingsFromCommit(feedback.Commit, task.OutputBagIDs) {
		add(binding)
	}
	for _, binding := range legacyOutputBagBindingsFromTask(task) {
		add(binding)
	}
	return out
}

func legacyOutputBagBindingsFromTask(task core.Task) []core.BagBindingRef {
	if len(task.OutputBagIDs) == 0 {
		return nil
	}
	if len(task.OutputBagIDs) == len(task.InputBags) && shouldTreatOutputsAsForwardedInputs(task) {
		out := make([]core.BagBindingRef, 0, len(task.InputBags))
		for i, binding := range task.InputBags {
			binding.BagID = task.OutputBagIDs[i]
			out = append(out, binding)
		}
		return out
	}
	return nil
}

func shouldTreatOutputsAsForwardedInputs(task core.Task) bool {
	if len(task.InputBags) == 0 || len(task.OutputBagIDs) != len(task.InputBags) {
		return false
	}
	return sameStringSet(task.OutputBagIDs, task.InputBagIDs)
}

func forwardedInputBags(task core.Task, feedback core.TaskMetaData) []core.BagBindingRef {
	bags := inputBagBindingsFromCommit(feedback.Commit, task.OutputBagIDs)
	if len(bags) > 0 {
		return bags
	}
	if shouldForwardInputBags(task, feedback, core.TaskStatusDone) {
		return append([]core.BagBindingRef(nil), task.InputBags...)
	}
	return nil
}

func buildPipelineInstanceFromControl(runID core.RunID, parent core.PipelineInstance, transition pipeline.TransitionSpec, called pipeline.PipelineDefSpec, control core.Control) (core.PipelineInstance, error) {
	instanceKey := strings.TrimSpace(control.InstanceKey)
	if instanceKey == "" {
		return core.PipelineInstance{}, fmt.Errorf("start_pipeline control for transition %q requires instance_key", transition.ID)
	}
	if len(control.InputBags) == 0 {
		return core.PipelineInstance{}, fmt.Errorf("start_pipeline control for transition %q requires input_bags", transition.ID)
	}
	params, err := resolveControlParamBindings(parent, transition, control)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	agentBindings, localAgentBindings, err := resolveControlAgentBindings(parent, transition, control)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	inputBags, err := resolveControlInputBagBindings(parent, transition, control)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	inputBagLists, err := resolveControlInputBagListBindings(parent, transition, control)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	if len(inputBagLists) == 0 {
		inputBagLists = singletonBagLists(inputBags)
	}
	if err := validateStartPipelineInstanceBindings(transition, called, params, localAgentBindings, inputBags, inputBagLists); err != nil {
		return core.PipelineInstance{}, err
	}
	now := time.Now().UTC()
	parentID := parent.ID
	return core.PipelineInstance{
		ID:                 stablePipelineInstanceID(parent.ID, transition.ID, instanceKey),
		RunID:              runID,
		PipelineID:         transition.PipelineID,
		ParentID:           &parentID,
		ParentTransitionID: transition.ID,
		InstanceKey:        instanceKey,
		Status:             core.PipelineInstanceStatusCreated,
		Params:             params,
		AgentBindings:      agentBindings,
		InputBagIDs:        inputBags,
		InputBagIDLists:    inputBagLists,
		OutputBagIDs:       cloneControlStringMap(control.OutputBags),
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func buildPipelineInstanceFromSingleCall(runID core.RunID, parent core.PipelineInstance, transition pipeline.TransitionSpec) (core.PipelineInstance, error) {
	if strings.TrimSpace(string(transition.PipelineID)) == "" {
		return core.PipelineInstance{}, fmt.Errorf("call transition %q requires pipeline_id", transition.ID)
	}
	params, err := resolveParamBindings(parent, transition)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	agentBindings, err := resolveAgentBindings(parent, transition)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	inputBags, err := resolveInputBagBindings(parent, transition)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	inputBagLists, err := resolveInputBagListBindings(parent, transition)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	now := time.Now().UTC()
	parentID := parent.ID
	return core.PipelineInstance{
		ID:                 stablePipelineInstanceID(parent.ID, transition.ID, "single"),
		RunID:              runID,
		PipelineID:         transition.PipelineID,
		ParentID:           &parentID,
		ParentTransitionID: transition.ID,
		InstanceKey:        "single",
		Status:             core.PipelineInstanceStatusCreated,
		Params:             params,
		AgentBindings:      agentBindings,
		InputBagIDs:        inputBags,
		InputBagIDLists:    inputBagLists,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func normalizeSingleCallInputBagLists(child core.PipelineInstance, called pipeline.PipelineDefSpec) core.PipelineInstance {
	if len(child.InputBagIDLists) == 0 || len(called.Signature.InputBags) == 0 {
		return child
	}
	signature := make(map[string]pipeline.BagSpec, len(called.Signature.InputBags))
	for _, bag := range called.Signature.InputBags {
		name := strings.TrimSpace(bag.Name)
		if name != "" {
			signature[name] = bag
		}
	}
	for key, values := range child.InputBagIDLists {
		name := key
		if indexedName, _, ok := parseIndexedBagLookupKey(key); ok {
			name = indexedName
		}
		spec, ok := signature[strings.TrimSpace(name)]
		if !ok || spec.Collection {
			continue
		}
		primary := strings.TrimSpace(child.InputBagIDs[name])
		if primary == "" {
			primary = strings.TrimSpace(child.InputBagIDs[key])
		}
		switch {
		case primary != "":
			child.InputBagIDLists[key] = []string{primary}
		case len(values) > 0:
			child.InputBagIDLists[key] = []string{values[0]}
		}
	}
	return child
}

func buildPipelineInstanceFromForeachCall(runID core.RunID, parent core.PipelineInstance, transition pipeline.TransitionSpec, called pipeline.PipelineDefSpec, sourceName string, bagID string, itemKey string, itemValue string) (core.PipelineInstance, error) {
	if strings.TrimSpace(string(transition.PipelineID)) == "" {
		return core.PipelineInstance{}, fmt.Errorf("call transition %q requires pipeline_id", transition.ID)
	}
	params, err := resolveForeachParamBindings(parent, transition, itemKey, itemValue)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	agentBindings, err := resolveAgentBindings(parent, transition)
	if err != nil {
		return core.PipelineInstance{}, err
	}
	inputBags := foreachInputBagBindings(transition, sourceName, bagID)
	inputBagLists := singletonBagLists(inputBags)
	for _, bag := range called.Signature.InputBags {
		if strings.TrimSpace(bag.Name) == "" || len(bag.IndexedBy) == 0 {
			continue
		}
		if inputBagLists == nil {
			inputBagLists = make(map[string][]string)
		}
		if inputBags == nil {
			inputBags = make(map[string]string)
		}
		indexes := make(map[string]string)
		for _, key := range bag.IndexedBy {
			key = strings.TrimSpace(key)
			if key == itemKey && itemValue != "" {
				indexes[key] = itemValue
			}
		}
		if len(indexes) > 0 {
			indexedKey := indexedBagLookupKey(bag.Name, indexes)
			inputBags[indexedKey] = bagID
			inputBagLists[indexedKey] = []string{bagID}
		}
	}
	now := time.Now().UTC()
	parentID := parent.ID
	return core.PipelineInstance{
		ID:                 stablePipelineInstanceID(parent.ID, transition.ID, itemValue),
		RunID:              runID,
		PipelineID:         transition.PipelineID,
		ParentID:           &parentID,
		ParentTransitionID: transition.ID,
		InstanceKey:        itemValue,
		Status:             core.PipelineInstanceStatusCreated,
		Params:             params,
		AgentBindings:      agentBindings,
		InputBagIDs:        inputBags,
		InputBagIDLists:    inputBagLists,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func resolveForeachParamBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec, itemKey string, itemValue string) (map[string]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.Params) == 0 {
		return map[string]string{itemKey: itemValue}, nil
	}
	out := make(map[string]string, len(transition.Bindings.Params))
	for name, expr := range transition.Bindings.Params {
		value, err := resolveForeachBindingExpr(parent, expr, itemKey, itemValue)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.params.%s: %w", transition.ID, name, err)
		}
		out[name] = value
	}
	if _, ok := out[itemKey]; !ok && strings.TrimSpace(itemKey) != "" {
		out[itemKey] = itemValue
	}
	return out, nil
}

func foreachInputBagBindings(transition pipeline.TransitionSpec, sourceName string, bagID string) map[string]string {
	out := make(map[string]string)
	if transition.Bindings == nil || len(transition.Bindings.InputBags) == 0 {
		if sourceName != "" {
			out[sourceName] = bagID
		}
		return out
	}
	for name := range transition.Bindings.InputBags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = bagID
	}
	return out
}

func resolveControlParamBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec, control core.Control) (map[string]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.Params) == 0 {
		return cloneControlStringMap(control.Params), nil
	}
	out := make(map[string]string, len(transition.Bindings.Params))
	for name, expr := range transition.Bindings.Params {
		value, err := resolveControlBindingExpr(parent, control, expr)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.params.%s: %w", transition.ID, name, err)
		}
		out[name] = value
	}
	return out, nil
}

func resolveControlAgentBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec, control core.Control) (map[string]core.AgentID, map[string]core.AgentID, error) {
	local := make(map[string]core.AgentID)
	if transition.Bindings == nil || len(transition.Bindings.AgentBindings) == 0 {
		for name, agentID := range control.AgentBindings {
			name = strings.TrimSpace(name)
			if name == "" || strings.TrimSpace(string(agentID)) == "" {
				continue
			}
			local[name] = agentID
		}
		return mergeAgentBindings(parent.AgentBindings, local), local, nil
	}
	for name, expr := range transition.Bindings.AgentBindings {
		value, err := resolveControlBindingExpr(parent, control, expr)
		if err != nil {
			return nil, nil, fmt.Errorf("transition %q bindings.agent_bindings.%s: %w", transition.ID, name, err)
		}
		if strings.TrimSpace(value) != "" {
			local[name] = core.AgentID(value)
		}
	}
	return mergeAgentBindings(parent.AgentBindings, local), local, nil
}

func resolveControlInputBagBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec, control core.Control) (map[string]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.InputBags) == 0 {
		return cloneControlStringMap(control.InputBags), nil
	}
	out := make(map[string]string, len(transition.Bindings.InputBags))
	for name, binding := range transition.Bindings.InputBags {
		value, err := resolveControlInputBagBinding(parent, control, binding)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.input_bags.%s: %w", transition.ID, name, err)
		}
		out[name] = value
	}
	return out, nil
}

func resolveControlInputBagListBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec, control core.Control) (map[string][]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.InputBags) == 0 {
		return singletonBagLists(control.InputBags), nil
	}
	out := make(map[string][]string, len(transition.Bindings.InputBags))
	for name, binding := range transition.Bindings.InputBags {
		values, err := resolveControlInputBagBindingList(parent, control, binding)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.input_bags.%s: %w", transition.ID, name, err)
		}
		out[name] = values
	}
	return out, nil
}

func resolveControlInputBagBinding(parent core.PipelineInstance, control core.Control, binding any) (string, error) {
	switch typed := binding.(type) {
	case string:
		return resolveControlBindingExpr(parent, control, typed)
	case map[string]any:
		if value, ok := stringFromAnyMap(typed, "bag_id"); ok {
			return value, nil
		}
		bagRef := bagRefFromBindingObject(typed)
		if bagRef == "" {
			return "", fmt.Errorf("object binding requires name or bag_id")
		}
		values := bagIDsForName(parent, bagRef)
		if len(values) == 0 {
			return "", fmt.Errorf("parent instance %q has no bag for %q", parent.ID, bagRef)
		}
		return values[0], nil
	default:
		return "", fmt.Errorf("unsupported binding type %T", binding)
	}
}

func resolveControlInputBagBindingList(parent core.PipelineInstance, control core.Control, binding any) ([]string, error) {
	switch typed := binding.(type) {
	case string:
		value, err := resolveControlBindingExpr(parent, control, typed)
		if err != nil {
			return nil, err
		}
		return nonEmptyStrings([]string{value}), nil
	case map[string]any:
		if value, ok := stringFromAnyMap(typed, "bag_id"); ok {
			return []string{value}, nil
		}
		bagRef := bagRefFromBindingObject(typed)
		if bagRef == "" {
			return nil, fmt.Errorf("object binding requires name or bag_id")
		}
		values := bagIDsForName(parent, bagRef)
		if len(values) == 0 {
			return nil, fmt.Errorf("parent instance %q has no input bag list for %q", parent.ID, bagRef)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported binding type %T", binding)
	}
}

func validateStartPipelineInstanceBindings(transition pipeline.TransitionSpec, called pipeline.PipelineDefSpec, params map[string]string, localAgentBindings map[string]core.AgentID, inputBags map[string]string, inputBagLists map[string][]string) error {
	for _, param := range called.Signature.Params {
		if strings.TrimSpace(params[param]) == "" {
			return fmt.Errorf("called pipeline %q requires params.%s", called.PipelineID, param)
		}
	}
	for _, agent := range called.Signature.Agents {
		if strings.TrimSpace(string(localAgentBindings[agent.Name])) == "" {
			return fmt.Errorf("called pipeline %q requires agent_bindings.%s", called.PipelineID, agent.Name)
		}
	}
	for _, bag := range called.Signature.InputBags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(inputBags[name]) == "" && len(inputBagLists[name]) == 0 {
			return fmt.Errorf("called pipeline %q requires input_bags.%s", called.PipelineID, name)
		}
	}
	if transition.Bindings != nil {
		for name := range params {
			if !containsString(called.Signature.Params, name) {
				return fmt.Errorf("bindings.params.%s is not declared by called pipeline %q", name, called.PipelineID)
			}
		}
		requiredAgents := signatureAgentSet(called.Signature.Agents)
		for name := range localAgentBindings {
			if !requiredAgents[name] {
				return fmt.Errorf("bindings.agent_bindings.%s is not declared by called pipeline %q", name, called.PipelineID)
			}
		}
		requiredInputBags := signatureBagSet(called.Signature.InputBags)
		for name := range inputBags {
			if !requiredInputBags[name] {
				return fmt.Errorf("bindings.input_bags.%s is not declared by called pipeline %q", name, called.PipelineID)
			}
		}
	}
	return nil
}

func buildTaskFromPipelineTransition(runID core.RunID, instance core.PipelineInstance, transition pipeline.TransitionSpec) (core.Task, error) {
	if transition.Agent == nil {
		return core.Task{}, fmt.Errorf("task transition %q requires agent", transition.ID)
	}
	alias := strings.TrimSpace(string(transition.Agent.Alias))
	if alias == "" {
		return core.Task{}, fmt.Errorf("task transition %q requires agent alias", transition.ID)
	}
	agentID, ok := instance.AgentBindings[alias]
	if !ok || strings.TrimSpace(string(agentID)) == "" {
		return core.Task{}, fmt.Errorf("pipeline instance %q has no binding for agent alias %q", instance.ID, alias)
	}
	now := time.Now().UTC()
	return core.Task{
		ID:                 pipelineInstanceTaskID(instance.ID, transition.ID),
		RunID:              runID,
		PipelineInstanceID: instance.ID,
		StageID:            core.StageID(transition.ID),
		AgentRole:          transition.Agent.Role,
		AgentID:            agentID,
		Op:                 transition.Op,
		Status:             core.TaskStatusPending,
		InputBagIDs:        inputBagIDsForTransition(instance, transition.InputBags),
		InputBags:          inputBagBindingsForTransition(instance, transition.InputBags),
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func (s *Service) pipelineTransitionAttemptTask(ctx context.Context, runID core.RunID, base core.Task, transition pipeline.TransitionSpec) (core.Task, bool, bool, error) {
	if transition.Limits.MaxAttempts <= 0 {
		existing, err := s.tasks.Get(ctx, runID, base.ID)
		if err != nil {
			return base, true, false, nil
		}
		if taskStatusActive(existing.Status) || existing.Status == core.TaskStatusDone {
			return existing, false, false, nil
		}
		return base, true, false, nil
	}
	items, err := s.tasks.ListByRun(ctx, runID)
	if err != nil {
		return core.Task{}, false, false, err
	}
	attempts := make([]core.Task, 0)
	for _, item := range items {
		if item.PipelineInstanceID != base.PipelineInstanceID || item.StageID != base.StageID {
			continue
		}
		attempts = append(attempts, item)
	}
	sort.SliceStable(attempts, func(i, j int) bool {
		if !attempts[i].CreatedAt.Equal(attempts[j].CreatedAt) {
			return attempts[i].CreatedAt.Before(attempts[j].CreatedAt)
		}
		if !attempts[i].UpdatedAt.Equal(attempts[j].UpdatedAt) {
			return attempts[i].UpdatedAt.Before(attempts[j].UpdatedAt)
		}
		return attempts[i].ID < attempts[j].ID
	})
	if len(attempts) == 0 {
		return base, true, false, nil
	}
	latest := attempts[len(attempts)-1]
	if taskStatusActive(latest.Status) {
		return latest, false, false, nil
	}
	if transition.Limits.MaxAttempts > 0 && len(attempts) >= transition.Limits.MaxAttempts {
		if shouldStartAnotherPipelineAttempt(latest.Status, true) {
			return latest, false, true, nil
		}
		return latest, false, false, nil
	}
	if !shouldStartAnotherPipelineAttempt(latest.Status, transition.Limits.MaxAttempts > 0) {
		return latest, false, false, nil
	}
	next := base
	next.ID = pipelineInstanceTaskAttemptID(base.ID, len(attempts)+1)
	now := time.Now().UTC()
	next.CreatedAt = now
	next.UpdatedAt = now
	return next, true, false, nil
}

func shouldStartAnotherPipelineAttempt(status core.TaskStatus, retryDone bool) bool {
	switch status {
	case core.TaskStatusFailed, core.TaskStatusBlocked:
		return true
	case core.TaskStatusDone:
		return retryDone
	default:
		return false
	}
}

func taskStatusActive(status core.TaskStatus) bool {
	switch status {
	case core.TaskStatusPending, core.TaskStatusWaitingExternal, core.TaskStatusDispatched, core.TaskStatusRunning:
		return true
	default:
		return false
	}
}

func findTransition(def pipeline.PipelineDefSpec, id string) (pipeline.TransitionSpec, bool) {
	for _, transition := range def.Transitions {
		if transition.ID == id {
			return transition, true
		}
	}
	return pipeline.TransitionSpec{}, false
}

func pipelineStatesByID(def pipeline.PipelineDefSpec) map[string]pipeline.StateSpec {
	out := make(map[string]pipeline.StateSpec, len(def.States))
	for _, state := range def.States {
		out[state.ID] = state
	}
	return out
}

func pipelineTransitionsByID(def pipeline.PipelineDefSpec) map[string]pipeline.TransitionSpec {
	out := make(map[string]pipeline.TransitionSpec, len(def.Transitions))
	for _, transition := range def.Transitions {
		out[transition.ID] = transition
	}
	return out
}

func resolveParamBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec) (map[string]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.Params) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(transition.Bindings.Params))
	for name, expr := range transition.Bindings.Params {
		value, err := resolvePipelineBindingExpr(parent, expr)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.params.%s: %w", transition.ID, name, err)
		}
		out[name] = value
	}
	return out, nil
}

func resolveAgentBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec) (map[string]core.AgentID, error) {
	out := mergeAgentBindings(parent.AgentBindings, nil)
	if transition.Bindings == nil || len(transition.Bindings.AgentBindings) == 0 {
		return out, nil
	}
	for name, expr := range transition.Bindings.AgentBindings {
		value, err := resolvePipelineBindingExpr(parent, expr)
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.agent_bindings.%s: %w", transition.ID, name, err)
		}
		if strings.TrimSpace(value) != "" {
			out[name] = core.AgentID(value)
		}
	}
	return out, nil
}

func resolveInputBagBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec) (map[string]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.InputBags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(transition.Bindings.InputBags))
	for name, binding := range transition.Bindings.InputBags {
		value, err := resolveInputBagBinding(parent, binding, inputBagSpecByName(transition.InputBags, name))
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.input_bags.%s: %w", transition.ID, name, err)
		}
		out[name] = value
	}
	return out, nil
}

func resolveInputBagListBindings(parent core.PipelineInstance, transition pipeline.TransitionSpec) (map[string][]string, error) {
	if transition.Bindings == nil || len(transition.Bindings.InputBags) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(transition.Bindings.InputBags))
	for name, binding := range transition.Bindings.InputBags {
		values, err := resolveInputBagBindingList(parent, binding, inputBagSpecByName(transition.InputBags, name))
		if err != nil {
			return nil, fmt.Errorf("transition %q bindings.input_bags.%s: %w", transition.ID, name, err)
		}
		out[name] = values
		sourceName := name
		if typed, ok := binding.(map[string]any); ok {
			if bagRef := bagRefFromBindingObject(typed); bagRef != "" {
				sourceName = bagRef
			}
		}
		addIndexedInputBagLists(out, parent, name, sourceName, values)
	}
	return out, nil
}

func addIndexedInputBagLists(out map[string][]string, parent core.PipelineInstance, name string, sourceName string, values []string) {
	name = strings.TrimSpace(name)
	if name == "" || len(values) == 0 {
		return
	}
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		sourceName = name
	}
	indexesByBagID := indexedBagIndexesByID(parent, sourceName)
	if len(indexesByBagID) == 0 {
		return
	}
	for _, bagID := range values {
		bagID = strings.TrimSpace(bagID)
		if bagID == "" {
			continue
		}
		indexes := indexesByBagID[bagID]
		if len(indexes) == 0 {
			continue
		}
		key := indexedBagLookupKey(name, indexes)
		if key == name {
			continue
		}
		out[key] = appendUniqueString(out[key], bagID)
	}
}

func inputBagSpecByName(bags []pipeline.BagSpec, name string) pipeline.BagSpec {
	name = strings.TrimSpace(name)
	for _, bag := range bags {
		if strings.TrimSpace(bag.Name) == name {
			return bag
		}
	}
	return pipeline.BagSpec{Name: name}
}

func resolveInputBagBinding(parent core.PipelineInstance, binding any, specs ...pipeline.BagSpec) (string, error) {
	switch typed := binding.(type) {
	case string:
		return resolvePipelineBindingExpr(parent, typed)
	case map[string]any:
		if value, ok := stringFromAnyMap(typed, "bag_id"); ok {
			return value, nil
		}
		bagRef := bagRefFromBindingObject(typed)
		if bagRef == "" {
			return "", fmt.Errorf("object binding requires name or bag_id")
		}
		values := bagIDsForBindingObject(parent, bagRef, typed, specs...)
		if len(values) == 0 {
			return "", fmt.Errorf("parent instance %q has no bag for %q", parent.ID, bagRef)
		}
		return values[0], nil
	default:
		return "", fmt.Errorf("unsupported binding type %T", binding)
	}
}

func resolveInputBagBindingList(parent core.PipelineInstance, binding any, specs ...pipeline.BagSpec) ([]string, error) {
	switch typed := binding.(type) {
	case string:
		value, err := resolvePipelineBindingExpr(parent, typed)
		if err != nil {
			return nil, err
		}
		return nonEmptyStrings([]string{value}), nil
	case map[string]any:
		if value, ok := stringFromAnyMap(typed, "bag_id"); ok {
			return []string{value}, nil
		}
		bagRef := bagRefFromBindingObject(typed)
		if bagRef == "" {
			return nil, fmt.Errorf("object binding requires name or bag_id")
		}
		values := bagIDsForBindingObject(parent, bagRef, typed, specs...)
		if len(values) == 0 {
			return nil, fmt.Errorf("parent instance %q has no input bag list for %q", parent.ID, bagRef)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported binding type %T", binding)
	}
}

func bagIDsForBindingObject(parent core.PipelineInstance, bagRef string, binding map[string]any, specs ...pipeline.BagSpec) []string {
	bag := pipeline.BagSpec{Name: bagRef}
	if len(specs) > 0 {
		bag = specs[0]
	}
	bag.Name = bagRef
	if len(bag.IndexedBy) == 0 {
		if indexedBy := stringSliceFromAnyMap(binding, "indexed_by"); len(indexedBy) > 0 {
			bag.IndexedBy = indexedBy
		}
	}
	if len(bag.IndexedBy) == 0 && strings.TrimSpace(parent.Params["module_key"]) != "" {
		indexedBag := bag
		indexedBag.IndexedBy = []string{"module_key"}
		if values := bagIDsForSpec(parent, indexedBag); len(values) > 0 {
			return values
		}
	}
	return bagIDsForSpec(parent, bag)
}

func stringSliceFromAnyMap(values map[string]any, key string) []string {
	raw, ok := values[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func callTransitionReady(ctx context.Context, instances repo.PipelineInstanceRepository, runID core.RunID, parentID core.PipelineInstanceID, transition pipeline.TransitionSpec) (bool, error) {
	children, err := instances.ListChildren(ctx, runID, parentID)
	if err != nil {
		return false, err
	}
	matched := 0
	for _, child := range children {
		if child.ParentTransitionID != transition.ID {
			continue
		}
		matched++
		if child.Status == core.PipelineInstanceStatusFailed {
			return false, fmt.Errorf("child pipeline instance %q failed", child.ID)
		}
		if child.Status != core.PipelineInstanceStatusCompleted {
			return false, nil
		}
	}
	if transition.Mode == "single" {
		return matched == 1, nil
	}
	return matched > 0, nil
}

func mergeChildInstanceOutputs(parent core.PipelineInstance, transition pipeline.TransitionSpec, child core.PipelineInstance) core.PipelineInstance {
	if parent.OutputBagIDs == nil {
		parent.OutputBagIDs = make(map[string]string)
	}
	if parent.OutputBagIDLists == nil {
		parent.OutputBagIDLists = make(map[string][]string)
	}
	for _, bag := range transition.OutputBags {
		for _, binding := range childOutputBagBindings(child, bag) {
			bagID := strings.TrimSpace(binding.BagID)
			if strings.TrimSpace(bagID) == "" {
				continue
			}
			if strings.TrimSpace(bag.Name) != "" {
				parent.OutputBagIDs[bag.Name] = bagID
				parent.OutputBagIDLists[bag.Name] = appendUniqueString(parent.OutputBagIDLists[bag.Name], bagID)
				if key := indexedBagLookupKey(bag.Name, binding.Indexes); key != bag.Name {
					parent.OutputBagIDs[key] = bagID
					parent.OutputBagIDLists[key] = appendUniqueString(parent.OutputBagIDLists[key], bagID)
				}
			}
		}
	}
	return parent
}

func mergeChildInstanceHandlers(parent core.PipelineInstance, transition pipeline.TransitionSpec, child core.PipelineInstance, childDef pipeline.PipelineDefSpec) core.PipelineInstance {
	if len(transition.OutputHandlers) == 0 || len(childDef.Signature.ExportedHandlers) == 0 {
		return parent
	}
	exports := make(map[string]pipeline.HandlerExportSpec, len(childDef.Signature.ExportedHandlers))
	for _, export := range childDef.Signature.ExportedHandlers {
		if strings.TrimSpace(export.Name) != "" {
			exports[export.Name] = export
		}
	}
	for _, binding := range transition.OutputHandlers {
		fromHandler := strings.TrimSpace(binding.FromHandler)
		if fromHandler == "" {
			fromHandler = strings.TrimSpace(binding.Name)
		}
		export, ok := exports[fromHandler]
		if !ok {
			continue
		}
		indexes := indexesForHandlerBinding(child, binding, export)
		parent.HandlerBindings = appendOrReplaceHandlerBinding(parent.HandlerBindings, core.HandlerBindingRef{
			Name:               strings.TrimSpace(binding.Name),
			FromHandler:        fromHandler,
			OwnerInstanceID:    child.ID,
			ParentTransitionID: child.ParentTransitionID,
			Handles:            append([]string(nil), export.Handles...),
			Replaces:           handlerReplaceBindings(child, export.Replaces, indexes),
			Indexes:            cloneControlStringMap(indexes),
		})
	}
	return parent
}

func appendOrReplaceHandlerBinding(bindings []core.HandlerBindingRef, next core.HandlerBindingRef) []core.HandlerBindingRef {
	for i, existing := range bindings {
		if existing.Name == next.Name && existing.OwnerInstanceID == next.OwnerInstanceID && indexedBagLookupKey("", existing.Indexes) == indexedBagLookupKey("", next.Indexes) {
			bindings[i] = next
			return bindings
		}
	}
	return append(bindings, next)
}

func indexesForHandlerBinding(child core.PipelineInstance, binding pipeline.HandlerBindingSpec, export pipeline.HandlerExportSpec) map[string]string {
	keys := binding.IndexedBy
	if len(keys) == 0 {
		keys = export.IndexedBy
	}
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(child.Params[key]); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func handlerReplaceBindings(child core.PipelineInstance, replaces []pipeline.BagSpec, indexes map[string]string) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0)
	for _, replace := range replaces {
		bag := replace
		if len(bag.IndexedBy) == 0 && len(indexes) > 0 {
			for key := range indexes {
				bag.IndexedBy = append(bag.IndexedBy, key)
			}
			sort.Strings(bag.IndexedBy)
		}
		for _, binding := range childOutputBagBindings(child, pipeline.BagSpec{
			Name:       bag.Name,
			FromReturn: bag.Name,
			IndexedBy:  bag.IndexedBy,
		}) {
			if len(binding.Indexes) == 0 && len(indexes) > 0 {
				binding.Indexes = cloneControlStringMap(indexes)
			}
			out = append(out, binding)
		}
	}
	return out
}

func selectHandlerBindingsForException(instance core.PipelineInstance, frame core.ExceptionFrame) []core.HandlerBindingRef {
	selected := make([]core.HandlerBindingRef, 0)
	seen := make(map[string]bool)
	for _, binding := range instance.HandlerBindings {
		if !containsString(binding.Handles, string(frame.Result)) {
			continue
		}
		if !handlerMatchesException(binding, frame) {
			continue
		}
		key := string(binding.OwnerInstanceID) + "\x00" + binding.Name + "\x00" + indexedBagLookupKey("", binding.Indexes)
		if seen[key] {
			continue
		}
		seen[key] = true
		selected = append(selected, binding)
	}
	return selected
}

func handlerMatchesException(binding core.HandlerBindingRef, frame core.ExceptionFrame) bool {
	if len(frame.FailedInputBags) == 0 {
		return true
	}
	for _, replace := range binding.Replaces {
		for _, failed := range frame.FailedInputBags {
			if strings.TrimSpace(replace.BagID) != "" && replace.BagID == failed.BagID {
				return true
			}
			if strings.TrimSpace(replace.Name) != "" && replace.Name == failed.Name && indexesMatch(replace.Indexes, failed.Indexes) {
				return true
			}
		}
	}
	return false
}

func indexesMatch(left map[string]string, right map[string]string) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0
	}
	for key, value := range left {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if right[key] != value {
			return false
		}
	}
	return true
}

func childOutputBagIDs(child core.PipelineInstance, bag pipeline.BagSpec) []string {
	bindings := childOutputBagBindings(child, bag)
	out := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, binding.BagID)
	}
	return uniqueStrings(out)
}

func childOutputBagBindings(child core.PipelineInstance, bag pipeline.BagSpec) []core.BagBindingRef {
	returnName := callOutputReturnName(bag)
	fallbackIndexes := indexesForBagSpec(child, bag)
	indexedMatches := make([]core.BagBindingRef, 0)
	addIndexedMatch := func(name string, indexes map[string]string, bagID string) {
		name = strings.TrimSpace(name)
		bagID = strings.TrimSpace(bagID)
		if name == "" || bagID == "" {
			return
		}
		indexedMatches = append(indexedMatches, core.BagBindingRef{
			Name:    name,
			BagID:   bagID,
			Indexes: cloneControlStringMap(indexes),
		})
	}
	for key, values := range child.OutputBagIDLists {
		name, indexes, ok := parseIndexedBagLookupKey(key)
		if !ok || name != strings.TrimSpace(returnName) {
			continue
		}
		for _, value := range values {
			addIndexedMatch(bag.Name, indexes, value)
		}
	}
	for key, value := range child.OutputBagIDs {
		name, indexes, ok := parseIndexedBagLookupKey(key)
		if !ok || name != strings.TrimSpace(returnName) {
			continue
		}
		addIndexedMatch(bag.Name, indexes, value)
	}
	if len(indexedMatches) > 0 {
		out := make([]core.BagBindingRef, 0, len(indexedMatches))
		seen := make(map[string]bool, len(indexedMatches))
		for _, binding := range indexedMatches {
			seenKey := binding.Name + "\x00" + indexedBagLookupKey("", binding.Indexes) + "\x00" + binding.BagID
			if seen[seenKey] {
				continue
			}
			seen[seenKey] = true
			out = append(out, binding)
		}
		return out
	}

	keys := []string{returnName, "default"}
	out := make([]core.BagBindingRef, 0, len(keys))
	seen := make(map[string]bool)
	add := func(name string, indexes map[string]string, bagID string) {
		name = strings.TrimSpace(name)
		bagID = strings.TrimSpace(bagID)
		if name == "" || bagID == "" {
			return
		}
		seenKey := name + "\x00" + indexedBagLookupKey("", indexes) + "\x00" + bagID
		if seen[seenKey] {
			return
		}
		seen[seenKey] = true
		out = append(out, core.BagBindingRef{
			Name:    name,
			BagID:   bagID,
			Indexes: cloneControlStringMap(indexes),
		})
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		for _, value := range child.OutputBagIDLists[key] {
			add(bag.Name, fallbackIndexes, value)
		}
		add(bag.Name, fallbackIndexes, child.OutputBagIDs[key])
	}
	if len(out) == 0 && len(child.OutputBagIDs) == 1 {
		for _, value := range child.OutputBagIDs {
			add(bag.Name, fallbackIndexes, value)
		}
	}
	return out
}

func aggregateStateReady(state pipeline.StateSpec, instance *core.PipelineInstance, states map[string]pipeline.StateSpec) bool {
	switch state.Proof.Mode {
	case "join_bags":
		for _, source := range state.Proof.Sources {
			if strings.TrimSpace(source.Name) == "" {
				continue
			}
			if len(bagIDsForName(*instance, source.Name)) == 0 {
				return false
			}
		}
		for _, exposed := range state.Exposes.Bags {
			if strings.TrimSpace(exposed.Name) == "" {
				continue
			}
			ids := aggregateInputBagIDsForExposed(state, *instance, exposed, states)
			if len(ids) == 0 {
				return false
			}
			if instance.InputBagIDs == nil {
				instance.InputBagIDs = make(map[string]string)
			}
			if instance.InputBagIDLists == nil {
				instance.InputBagIDLists = make(map[string][]string)
			}
			syntheticID := syntheticAggregateBagID(instance.RunID, instance.ID, state.ID, exposed.Name)
			instance.InputBagIDs[exposed.Name] = syntheticID
			if shouldSynthesizeAggregateBag(exposed) {
				instance.InputBagIDLists[exposed.Name] = []string{syntheticID}
			} else {
				instance.InputBagIDLists[exposed.Name] = ids
			}
		}
		return true
	case "all":
		if len(state.Proof.States) > 0 {
			ids := make([]string, 0, len(state.Proof.States))
			for _, stateID := range state.Proof.States {
				stateIDs := referencedStateBagIDs(states, *instance, stateID)
				if len(stateIDs) == 0 {
					return false
				}
				ids = append(ids, stateIDs...)
			}
			ids = uniqueStrings(ids)
			for _, exposed := range state.Exposes.Bags {
				if strings.TrimSpace(exposed.Name) == "" {
					continue
				}
				exposedIDs := aggregateInputBagIDsForExposed(state, *instance, exposed, states)
				if len(exposedIDs) == 0 {
					return false
				}
				if instance.InputBagIDs == nil {
					instance.InputBagIDs = make(map[string]string)
				}
				if instance.InputBagIDLists == nil {
					instance.InputBagIDLists = make(map[string][]string)
				}
				syntheticID := syntheticAggregateBagID(instance.RunID, instance.ID, state.ID, exposed.Name)
				instance.InputBagIDs[exposed.Name] = syntheticID
				if shouldSynthesizeAggregateBag(exposed) {
					instance.InputBagIDLists[exposed.Name] = []string{syntheticID}
				} else {
					instance.InputBagIDLists[exposed.Name] = exposedIDs
				}
			}
			return true
		}
		for _, exposed := range state.Exposes.Bags {
			if strings.TrimSpace(exposed.Name) == "" {
				continue
			}
			ids := aggregateInputBagIDsForExposed(state, *instance, exposed, states)
			if len(ids) == 0 {
				return false
			}
			if instance.InputBagIDs == nil {
				instance.InputBagIDs = make(map[string]string)
			}
			if instance.InputBagIDLists == nil {
				instance.InputBagIDLists = make(map[string][]string)
			}
			syntheticID := syntheticAggregateBagID(instance.RunID, instance.ID, state.ID, exposed.Name)
			instance.InputBagIDs[exposed.Name] = syntheticID
			if shouldSynthesizeAggregateBag(exposed) {
				instance.InputBagIDLists[exposed.Name] = []string{syntheticID}
			} else {
				instance.InputBagIDLists[exposed.Name] = ids
			}
		}
		return true
	case "":
		return true
	default:
		return false
	}
}

func (s *Service) materializeAggregateBags(ctx context.Context, run core.PipelineRun, instance core.PipelineInstance, state pipeline.StateSpec, states map[string]pipeline.StateSpec) error {
	if s == nil || s.doujiaGit == nil || len(state.Exposes.Bags) == 0 {
		return nil
	}
	for _, exposed := range state.Exposes.Bags {
		if strings.TrimSpace(exposed.Name) == "" || !shouldSynthesizeAggregateBag(exposed) {
			continue
		}
		bagID := syntheticAggregateBagID(instance.RunID, instance.ID, state.ID, exposed.Name)
		if _, err := s.doujiaGit.GetBag(ctx, bagID); err == nil {
			continue
		}
		sourceIDs := aggregateInputBagIDsForExposed(state, instance, exposed, states)
		versionIDs := make([]string, 0)
		for _, sourceID := range sourceIDs {
			sourceID = strings.TrimSpace(sourceID)
			if sourceID == "" || sourceID == bagID {
				continue
			}
			sourceBag, err := s.doujiaGit.GetBag(ctx, sourceID)
			if err != nil {
				return fmt.Errorf("materialize aggregate bag %q source %q: %w", bagID, sourceID, err)
			}
			versionIDs = append(versionIDs, sourceBag.ArtifactVersionIDs...)
		}
		versionIDs = uniqueStrings(versionIDs)
		if len(versionIDs) == 0 {
			return fmt.Errorf("materialize aggregate bag %q: no artifact versions", bagID)
		}
		if err := s.doujiaGit.CreateBag(ctx, doujiagit.ArtifactBag{
			BagID:              bagID,
			RunID:              run.ID,
			ArtifactVersionIDs: versionIDs,
			CreatedAt:          time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func shouldSynthesizeAggregateBag(bag pipeline.BagSpec) bool {
	value := strings.TrimSpace(bag.Tags["synthesize"])
	return value == "bag" || value == "join_bags" || value == "true"
}

func aggregateInputBagIDs(state pipeline.StateSpec, instance core.PipelineInstance, states map[string]pipeline.StateSpec) []string {
	out := make([]string, 0)
	for _, source := range state.Proof.Sources {
		out = append(out, bagIDsForName(instance, source.Name)...)
	}
	for _, stateID := range state.Proof.States {
		out = append(out, referencedStateBagIDs(states, instance, stateID)...)
	}
	if len(out) != 0 {
		return uniqueStrings(out)
	}
	for _, exposed := range state.Exposes.Bags {
		for _, fromState := range referencedStateIDs(exposed) {
			out = append(out, referencedStateBagIDsForExposed(states, instance, fromState, exposed)...)
		}
	}
	return uniqueStrings(out)
}

func aggregateInputBagIDsForExposed(state pipeline.StateSpec, instance core.PipelineInstance, exposed pipeline.BagSpec, states map[string]pipeline.StateSpec) []string {
	fromStates := referencedStateIDs(exposed)
	if len(fromStates) == 0 {
		return aggregateInputBagIDs(state, instance, states)
	}
	out := make([]string, 0)
	for _, fromState := range fromStates {
		out = append(out, referencedStateBagIDsForExposed(states, instance, fromState, exposed)...)
	}
	return uniqueStrings(out)
}

func referencedStateIDs(exposed pipeline.BagSpec) []string {
	out := make([]string, 0, len(exposed.FromStates)+1)
	if stateID := strings.TrimSpace(exposed.FromState); stateID != "" {
		out = append(out, stateID)
	}
	for _, stateID := range exposed.FromStates {
		stateID = strings.TrimSpace(stateID)
		if stateID != "" {
			out = append(out, stateID)
		}
	}
	return uniqueStrings(out)
}

func referencedStateBagIDs(states map[string]pipeline.StateSpec, instance core.PipelineInstance, stateID string) []string {
	stateID = strings.TrimSpace(stateID)
	if stateID == "" {
		return nil
	}
	out := make([]string, 0)
	out = append(out, bagIDsForName(instance, stateID)...)
	out = append(out, bagIDsForState(instance, stateID)...)
	if state, ok := states[stateID]; ok {
		for _, exposed := range state.Exposes.Bags {
			out = append(out, exposedBagIDsForStateReference(instance, exposed)...)
		}
	}
	return uniqueStrings(out)
}

func referencedStateBagIDsForExposed(states map[string]pipeline.StateSpec, instance core.PipelineInstance, stateID string, exposed pipeline.BagSpec) []string {
	stateID = strings.TrimSpace(stateID)
	if stateID == "" {
		return nil
	}
	name := strings.TrimSpace(exposed.Name)
	if name != "" {
		if state, ok := states[stateID]; ok {
			out := make([]string, 0)
			for _, candidate := range state.Exposes.Bags {
				if strings.TrimSpace(candidate.Name) != name {
					continue
				}
				out = append(out, exposedBagIDsForStateReference(instance, candidate)...)
			}
			if len(out) > 0 {
				return uniqueStrings(out)
			}
		}
	}
	return referencedStateBagIDs(states, instance, stateID)
}

func exposedBagIDsForStateReference(instance core.PipelineInstance, bag pipeline.BagSpec) []string {
	name := strings.TrimSpace(bag.Name)
	if name == "" {
		return nil
	}
	if indexes := indexesForBagSpec(instance, bag); len(indexes) > 0 {
		indexedKey := indexedBagLookupKey(name, indexes)
		out := append([]string(nil), instance.OutputBagIDLists[indexedKey]...)
		if value := strings.TrimSpace(instance.OutputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if len(out) > 0 {
			return uniqueStrings(out)
		}
		out = append([]string(nil), instance.InputBagIDLists[indexedKey]...)
		if value := strings.TrimSpace(instance.InputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if len(out) > 0 {
			return uniqueStrings(out)
		}
	}
	out := append([]string(nil), instance.OutputBagIDLists[name]...)
	if value := strings.TrimSpace(instance.OutputBagIDs[name]); value != "" {
		out = append(out, value)
	}
	if len(out) > 0 {
		return uniqueStrings(out)
	}
	out = append([]string(nil), instance.InputBagIDLists[name]...)
	if value := strings.TrimSpace(instance.InputBagIDs[name]); value != "" {
		out = append(out, value)
	}
	return uniqueStrings(out)
}

func bagIDsForState(instance core.PipelineInstance, stateID string) []string {
	stateID = strings.TrimSpace(stateID)
	if stateID == "" {
		return nil
	}
	out := make([]string, 0)
	switch stateID {
	case "code_merged":
		out = append(out, bagIDsForName(instance, "merged_code")...)
	case "global_test_data_ready":
		out = append(out, bagIDsForName(instance, "global_test_data")...)
	case "modules_tested":
		out = append(out, bagIDsForName(instance, "tested_module")...)
	case "container_ready":
		out = append(out, bagIDsForName(instance, "container_context")...)
	}
	return uniqueStrings(out)
}

func bagIDsForName(instance core.PipelineInstance, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	out := append([]string(nil), instance.OutputBagIDLists[name]...)
	out = append(out, instance.InputBagIDLists[name]...)
	if len(out) > 0 {
		return uniqueStrings(out)
	}
	if value := strings.TrimSpace(instance.OutputBagIDs[name]); value != "" {
		out = append(out, value)
	}
	if value := strings.TrimSpace(instance.InputBagIDs[name]); value != "" {
		out = append(out, value)
	}
	return uniqueStrings(out)
}

func bagIDsForSpec(instance core.PipelineInstance, bag pipeline.BagSpec) []string {
	name := strings.TrimSpace(bag.Name)
	if name == "" {
		return nil
	}
	if indexes := indexesForBagSpec(instance, bag); len(indexes) > 0 {
		indexedKey := indexedBagLookupKey(name, indexes)
		out := append([]string(nil), instance.OutputBagIDLists[indexedKey]...)
		out = append(out, instance.InputBagIDLists[indexedKey]...)
		if value := strings.TrimSpace(instance.OutputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if value := strings.TrimSpace(instance.InputBagIDs[indexedKey]); value != "" {
			out = append(out, value)
		}
		if len(out) > 0 {
			return uniqueStrings(out)
		}
	}
	return bagIDsForName(instance, name)
}

func syntheticAggregateBagID(runID core.RunID, instanceID core.PipelineInstanceID, stateID string, name string) string {
	return fmt.Sprintf("aggregate:%s:%s:%s:%s", sanitizeIDPart(string(runID)), instanceID, sanitizeIDPart(stateID), sanitizeIDPart(name))
}

func appendUniqueString(items []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return items
	}
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func stringFromAnyMap(values map[string]any, key string) (string, bool) {
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func bagRefFromBindingObject(values map[string]any) string {
	if value, ok := stringFromAnyMap(values, "name"); ok {
		return value
	}
	return ""
}

func resolvePipelineBindingExpr(parent core.PipelineInstance, expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "${") || !strings.HasSuffix(expr, "}") {
		return expr, nil
	}
	path := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}"))
	parts := strings.Split(path, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("unsupported expression %q", expr)
	}
	key := strings.TrimSpace(parts[1])
	switch strings.TrimSpace(parts[0]) {
	case "params":
		value, ok := parent.Params[key]
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("parent instance %q has no param %q", parent.ID, key)
		}
		return value, nil
	case "agents":
		value, ok := parent.AgentBindings[key]
		if !ok || strings.TrimSpace(string(value)) == "" {
			return "", fmt.Errorf("parent instance %q has no agent binding %q", parent.ID, key)
		}
		return string(value), nil
	case "input_bags":
		value, ok := parent.InputBagIDs[key]
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("parent instance %q has no input bag %q", parent.ID, key)
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported expression %q", expr)
	}
}

func resolveForeachBindingExpr(parent core.PipelineInstance, expr string, itemKey string, itemValue string) (string, error) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "${") || !strings.HasSuffix(expr, "}") {
		return expr, nil
	}
	path := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}"))
	parts := strings.Split(path, ".")
	if len(parts) == 2 && strings.TrimSpace(parts[0]) == "foreach" {
		key := strings.TrimSpace(parts[1])
		if key == itemKey {
			return itemValue, nil
		}
		return "", fmt.Errorf("foreach has no value for %q", key)
	}
	return resolvePipelineBindingExpr(parent, expr)
}

func resolveControlBindingExpr(parent core.PipelineInstance, control core.Control, expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "${") || !strings.HasSuffix(expr, "}") {
		return expr, nil
	}
	path := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}"))
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("unsupported expression %q", expr)
	}
	switch strings.TrimSpace(parts[0]) {
	case "control":
		if len(parts) != 3 {
			return "", fmt.Errorf("unsupported expression %q", expr)
		}
		section := strings.TrimSpace(parts[1])
		key := strings.TrimSpace(parts[2])
		switch section {
		case "params":
			value, ok := control.Params[key]
			if !ok || strings.TrimSpace(value) == "" {
				return "", fmt.Errorf("control instance %q has no param %q", control.InstanceKey, key)
			}
			return value, nil
		case "agent_bindings":
			value, ok := control.AgentBindings[key]
			if !ok || strings.TrimSpace(string(value)) == "" {
				return "", fmt.Errorf("control instance %q has no agent binding %q", control.InstanceKey, key)
			}
			return string(value), nil
		case "input_bags":
			value, ok := control.InputBags[key]
			if !ok || strings.TrimSpace(value) == "" {
				return "", fmt.Errorf("control instance %q has no input bag %q", control.InstanceKey, key)
			}
			return value, nil
		case "output_bags":
			value, ok := control.OutputBags[key]
			if !ok || strings.TrimSpace(value) == "" {
				return "", fmt.Errorf("control instance %q has no output bag %q", control.InstanceKey, key)
			}
			return value, nil
		default:
			return "", fmt.Errorf("unsupported expression %q", expr)
		}
	case "params", "agents", "input_bags":
		return resolvePipelineBindingExpr(parent, expr)
	default:
		return "", fmt.Errorf("unsupported expression %q", expr)
	}
}

func inputBagIDsForTransition(instance core.PipelineInstance, bags []pipeline.BagSpec) []string {
	out := make([]string, 0, len(bags))
	for _, bag := range bags {
		if values := bagIDsForSpec(instance, bag); len(values) > 0 {
			out = append(out, values...)
		}
	}
	return uniqueStrings(out)
}

func inputBagBindingsForTransition(instance core.PipelineInstance, bags []pipeline.BagSpec) []core.BagBindingRef {
	out := make([]core.BagBindingRef, 0, len(bags))
	for _, bag := range bags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			continue
		}
		out = append(out, bagBindingsForSpec(instance, bag)...)
	}
	return out
}

func bagBindingsForSpec(instance core.PipelineInstance, bag pipeline.BagSpec) []core.BagBindingRef {
	name := strings.TrimSpace(bag.Name)
	if name == "" {
		return nil
	}
	indexes := indexesForBagSpec(instance, bag)
	ids := bagIDsForSpec(instance, bag)
	out := make([]core.BagBindingRef, 0, len(ids))
	seen := make(map[string]bool)
	add := func(bagID string, bindingIndexes map[string]string) {
		bagID = strings.TrimSpace(bagID)
		if bagID == "" || seen[bagID] {
			return
		}
		seen[bagID] = true
		out = append(out, core.BagBindingRef{
			Name:    name,
			BagID:   bagID,
			Indexes: cloneControlStringMap(bindingIndexes),
		})
	}
	if len(indexes) > 0 {
		for _, bagID := range ids {
			add(bagID, indexes)
		}
		return out
	}
	indexesByBagID := indexedBagIndexesByID(instance, name)
	for _, bagID := range ids {
		add(bagID, indexesByBagID[bagID])
	}
	return out
}

func indexedBagIndexesByID(instance core.PipelineInstance, name string) map[string]map[string]string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	out := make(map[string]map[string]string)
	add := func(key string, bagID string) {
		bagID = strings.TrimSpace(bagID)
		if bagID == "" {
			return
		}
		indexedName, indexes, ok := parseIndexedBagLookupKey(key)
		if !ok || indexedName != name || len(indexes) == 0 {
			return
		}
		out[bagID] = cloneControlStringMap(indexes)
	}
	for key, values := range instance.OutputBagIDLists {
		for _, value := range values {
			add(key, value)
		}
	}
	for key, value := range instance.OutputBagIDs {
		add(key, value)
	}
	for key, values := range instance.InputBagIDLists {
		for _, value := range values {
			add(key, value)
		}
	}
	for key, value := range instance.InputBagIDs {
		add(key, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func indexesForBagSpec(instance core.PipelineInstance, bag pipeline.BagSpec) map[string]string {
	if len(bag.IndexedBy) == 0 {
		return nil
	}
	out := make(map[string]string, len(bag.IndexedBy))
	for _, key := range bag.IndexedBy {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(instance.Params[key]); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pipelineInstanceTaskID(instanceID core.PipelineInstanceID, transitionID string) core.TaskID {
	return core.TaskID(fmt.Sprintf("%s_%s", sanitizeIDPart(string(instanceID)), sanitizeIDPart(transitionID)))
}

func pipelineInstanceTaskAttemptID(baseID core.TaskID, attempt int) core.TaskID {
	if attempt <= 1 {
		return baseID
	}
	return core.TaskID(fmt.Sprintf("%s_attempt_%02d", baseID, attempt))
}

func stablePipelineInstanceID(parentID core.PipelineInstanceID, transitionID string, instanceKey string) core.PipelineInstanceID {
	return core.PipelineInstanceID(fmt.Sprintf("%s_%s_%s", sanitizeIDPart(string(parentID)), sanitizeIDPart(transitionID), sanitizeIDPart(instanceKey)))
}

func defaultAgentBindings(namespace pipeline.NamespaceSpec) map[string]core.AgentID {
	out := make(map[string]core.AgentID)
	for _, agent := range namespace.Agents {
		name := strings.TrimSpace(agent.Name)
		if name == "" || agent.DefaultAgentID == "" {
			continue
		}
		out[name] = agent.DefaultAgentID
	}
	return out
}

func mergeAgentBindings(parent map[string]core.AgentID, overrides map[string]core.AgentID) map[string]core.AgentID {
	out := make(map[string]core.AgentID, len(parent)+len(overrides))
	for key, value := range parent {
		out[key] = value
	}
	for key, value := range overrides {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(string(value)) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func cloneControlStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func indexedBagLookupKey(name string, indexes map[string]string) string {
	name = strings.TrimSpace(name)
	if len(indexes) == 0 {
		return name
	}
	keys := make([]string, 0, len(indexes))
	for key := range indexes {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return name
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(indexes[key])
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	if len(parts) == 0 {
		return name
	}
	return name + "[" + strings.Join(parts, ",") + "]"
}

func parseIndexedBagLookupKey(key string) (string, map[string]string, bool) {
	key = strings.TrimSpace(key)
	if key == "" || !strings.HasSuffix(key, "]") {
		return key, nil, false
	}
	open := strings.Index(key, "[")
	if open <= 0 {
		return key, nil, false
	}
	name := strings.TrimSpace(key[:open])
	rawIndexes := strings.TrimSpace(strings.TrimSuffix(key[open+1:], "]"))
	if name == "" || rawIndexes == "" {
		return name, nil, false
	}
	indexes := make(map[string]string)
	for _, part := range strings.Split(rawIndexes, ",") {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			return name, nil, false
		}
		indexKey := strings.TrimSpace(keyValue[0])
		indexValue := strings.TrimSpace(keyValue[1])
		if indexKey == "" || indexValue == "" {
			return name, nil, false
		}
		indexes[indexKey] = indexValue
	}
	if len(indexes) == 0 {
		return name, nil, false
	}
	return name, indexes, true
}

func cloneAgentBindings(in map[string]core.AgentID) map[string]core.AgentID {
	if in == nil {
		return nil
	}
	out := make(map[string]core.AgentID, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func containsString(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func signatureAgentSet(items []pipeline.SignatureAgentSpec) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func signatureBagSet(items []pipeline.BagSpec) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func (s *Service) advanceReadyTasks(ctx context.Context, run core.PipelineRun) error {
	s.advanceMu.Lock()
	defer s.advanceMu.Unlock()

	for {
		items, err := s.tasks.ListByRun(ctx, run.ID)
		if err != nil {
			return err
		}
		byID := make(map[core.TaskID]core.Task, len(items))
		for _, item := range items {
			byID[item.ID] = item
		}
		if isLowConcurrencyRun(run.Config.Delivery) && hasActiveDispatchedTask(items) {
			return nil
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
			item.InputBagIDs = readyTaskInputBagIDs(item, byID)
			if s.logger != nil {
				_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("ready task unlocked: task=%s op=%s deps=%s inputs=%d", item.ID, opForTask(item), formatTaskDeps(taskDependencies(item)), len(inputArtifacts)))
			}
			if err := s.dispatchTask(ctx, run, item, opForTask(item), inputArtifacts); err != nil {
				return err
			}
			if isLowConcurrencyRun(run.Config.Delivery) {
				return nil
			}
			dispatched = true
		}
		if !dispatched {
			return nil
		}
	}
}

func hasActiveDispatchedTask(items []core.Task) bool {
	for _, item := range items {
		switch item.Status {
		case core.TaskStatusDispatched, core.TaskStatusRunning:
			return true
		}
	}
	return false
}

func isLowConcurrencyRun(config core.DeliveryConfig) bool {
	return !config.AllowParallelWork && config.MaxCoderAgents == 1 && config.MaxTesterAgents == 1
}

func taskDependencies(task core.Task) []core.TaskID {
	return append([]core.TaskID(nil), task.DependsOnIDs...)
}

func primaryTaskDependency(task core.Task) (core.TaskID, bool) {
	if len(task.DependsOnIDs) == 0 {
		return "", false
	}
	return task.DependsOnIDs[len(task.DependsOnIDs)-1], true
}

func stageIDsToTaskIDs(items []core.StageID) []core.TaskID {
	out := make([]core.TaskID, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(string(item)) == "" {
			continue
		}
		out = append(out, core.TaskID(item))
	}
	return out
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
		inputs = append(inputs, artifactRefsToStrings(dep.InputArtifactRefs)...)
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

func readyTaskInputBagIDs(task core.Task, byID map[core.TaskID]core.Task) []string {
	if strings.Contains(string(task.ID), "_merge_code") {
		return readyMergeTaskInputBagIDs(task, byID)
	}
	if strings.Contains(string(task.ID), "_global_test_data") || strings.Contains(string(task.ID), "_global_test_code") {
		return readyGlobalTestInputBagIDs(task, byID)
	}
	inputs := append([]string(nil), task.InputBagIDs...)
	if task.ParentID != nil {
		if parent, ok := byID[*task.ParentID]; ok {
			inputs = append(inputs, parent.InputBagIDs...)
			inputs = append(inputs, parent.OutputBagIDs...)
		}
	}
	for _, depID := range taskDependencies(task) {
		if dep, ok := byID[depID]; ok {
			inputs = append(inputs, dep.OutputBagIDs...)
		}
	}
	return uniqueStrings(inputs)
}

func readyMergeTaskInputBagIDs(task core.Task, byID map[core.TaskID]core.Task) []string {
	inputs := append([]string(nil), task.InputBagIDs...)
	for _, depID := range taskDependencies(task) {
		if dep, ok := byID[depID]; ok {
			inputs = append(inputs, dep.InputBagIDs...)
			inputs = append(inputs, dep.OutputBagIDs...)
		}
	}
	if task.ParentID != nil {
		if parent, ok := byID[*task.ParentID]; ok {
			inputs = append(inputs, parent.InputBagIDs...)
			inputs = append(inputs, parent.OutputBagIDs...)
		}
		for _, item := range byID {
			if item.ParentID == nil || *item.ParentID != *task.ParentID {
				continue
			}
			if item.AgentRole != core.AgentRoleCoder || !strings.Contains(string(item.ID), "_write_code") {
				continue
			}
			inputs = append(inputs, item.InputBagIDs...)
			inputs = append(inputs, item.OutputBagIDs...)
		}
	}
	return uniqueStrings(inputs)
}

func readyGlobalTestInputBagIDs(task core.Task, byID map[core.TaskID]core.Task) []string {
	inputs := append([]string(nil), task.InputBagIDs...)
	for _, depID := range taskDependencies(task) {
		if dep, ok := byID[depID]; ok {
			inputs = append(inputs, dep.InputBagIDs...)
			inputs = append(inputs, dep.OutputBagIDs...)
		}
	}
	if task.ParentID != nil {
		if parent, ok := byID[*task.ParentID]; ok {
			inputs = append(inputs, parent.InputBagIDs...)
			inputs = append(inputs, parent.OutputBagIDs...)
		}
	}
	return uniqueStrings(inputs)
}

func opForTask(task core.Task) string {
	if strings.TrimSpace(task.Op) != "" {
		return task.Op
	}
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
	hasStartPipeline := hasStartPipelineControls(controls)
	for i, control := range controls {
		if hasStartPipeline {
			if control.Type != core.ControlTypeStartPipeline {
				return fmt.Errorf("control[%d] cannot mix start_pipeline with legacy control type %q", i, control.Type)
			}
			if strings.TrimSpace(control.TransitionID) == "" {
				return fmt.Errorf("control[%d] transition_id is required", i)
			}
			if strings.TrimSpace(string(control.PipelineID)) == "" {
				return fmt.Errorf("control[%d] pipeline_id is required", i)
			}
			if strings.TrimSpace(control.InstanceKey) == "" {
				return fmt.Errorf("control[%d] instance_key is required", i)
			}
			if len(control.InputBags) == 0 {
				return fmt.Errorf("control[%d] input_bags is required", i)
			}
			continue
		}
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

func hasStartPipelineControls(controls []core.Control) bool {
	for _, control := range controls {
		if control.Type == core.ControlTypeStartPipeline {
			return true
		}
	}
	return false
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
	return s.dispatchTaskWithFact(ctx, run, task, op, artifactURIs, feedbackFactContext{})
}

func (s *Service) dispatchTaskWithFact(ctx context.Context, run core.PipelineRun, task core.Task, op string, artifactURIs []string, fact feedbackFactContext) error {
	spec, err := s.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return err
	}
	stage, ok := findStageByID(spec, task.StageID)
	if !ok && task.ParentID == nil && task.PipelineInstanceID == "" {
		return fmt.Errorf("stage %q not found in pipeline %q", task.StageID, run.PipelineID)
	}

	if ok && stage.External && task.ParentID == nil && task.PipelineInstanceID == "" {
		task.Status = core.TaskStatusWaitingExternal
		task.UpdatedAt = time.Now().UTC()
		if s.logger != nil {
			_ = s.logger.Log(run.ID, "Orchestrator", fmt.Sprintf("next external task created: %s", task.ID))
		}
		s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "task_waiting_external", "task waiting external", map[string]any{
			"op": task.Op,
		})
		return s.upsertTask(ctx, task)
	}

	artifactURIs = appendDeliveryConfigForOp(run.ID, op, artifactURIs)
	task.Op = op
	task.InputArtifactRefs = toArtifactRefs(artifactURIs)
	task.Status = core.TaskStatusDispatched
	task.UpdatedAt = time.Now().UTC()
	if err := s.upsertTask(ctx, task); err != nil {
		return err
	}

	payloadArtifactURIs := s.dispatchArtifactURIs(task, run.ID, op, artifactURIs)
	provenance := s.taskDispatchProvenance(ctx, run.ID, task)
	if fact.Committed {
		provenance = fact.dispatchProvenance()
	}
	dispatch := core.TaskMetaData{
		Direction:                core.TaskDirectionDispatch,
		RunID:                    run.ID,
		TaskID:                   task.ID,
		ParentID:                 task.ParentID,
		DependsOnIDs:             task.DependsOnIDs,
		AgentID:                  task.AgentID,
		Op:                       op,
		SourceSnapshotID:         provenance.SourceSnapshotID,
		SourceSnapshotVersionID:  provenance.SourceSnapshotVersionID,
		SourceFrontierSnapshotID: provenance.SourceFrontierSnapshotID,
		SourceRefName:            provenance.SourceRefName,
		ContinuationID:           provenance.ContinuationID,
		DecisionKind:             provenance.DecisionKind,
		ArtifactURIs:             payloadArtifactURIs,
		InputBagIDs:              task.InputBagIDs,
		InputBags:                append([]core.BagBindingRef(nil), task.InputBags...),
		ExecutionMode:            task.ExecutionMode,
	}
	if s.logger != nil {
		_ = s.logger.LogTaskMeta(run.ID, "Orchestrator", "dispatch created", dispatch)
	}
	s.recordEvent(ctx, run.ID, task.ID, task.AgentID, "task_dispatched", "task dispatched", map[string]any{
		"op":              op,
		"artifact_count":  len(artifactURIs),
		"depends_on_ids":  task.DependsOnIDs,
		"has_parent_task": task.ParentID != nil,
	})

	if s.isSessionRole(task.AgentRole) && s.sessionDispatcher != nil {
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

func (f feedbackFactContext) dispatchProvenance() taskDispatchProvenance {
	return taskDispatchProvenance{
		SourceSnapshotID:         f.SnapshotID,
		SourceSnapshotVersionID:  f.SnapshotVersionID,
		SourceFrontierSnapshotID: f.FrontierSnapshotID,
		SourceRefName:            f.RefName,
		ContinuationID:           f.SnapshotID,
		DecisionKind:             doujiagit.RefMoveModeAdvance,
	}
}

func (s *Service) dispatchArtifactURIs(task core.Task, runID core.RunID, op string, artifactURIs []string) []string {
	if len(task.InputBagIDs) == 0 || s.isSessionRole(task.AgentRole) {
		return artifactURIs
	}
	if task.ParentID != nil {
		return artifactURIs
	}
	if op == core.TaskOpSplitModule || op == core.TaskOpResplitModule {
		return artifactURIs
	}
	return nil
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
	s.recordEvent(ctx, run.ID, taskID, "", "run_failed", "run failed", map[string]any{
		"task_id": taskID,
	})
	return s.runs.Update(ctx, run)
}

func (s *Service) recordEvent(ctx context.Context, runID core.RunID, taskID core.TaskID, agentID core.AgentID, eventType string, message string, payload any) {
	if s.events == nil {
		return
	}
	now := s.uniqueTimestamp()
	payloadJSON := ""
	if payload != nil {
		if raw, err := json.Marshal(payload); err == nil {
			payloadJSON = string(raw)
		}
	}
	id := fmt.Sprintf("%s:%s:%d:%d", runID, eventType, now.UnixNano(), atomic.AddUint64(&s.eventSeq, 1))
	_ = s.events.Create(ctx, repo.EventRecord{
		ID:          id,
		RunID:       runID,
		TaskID:      taskID,
		AgentID:     agentID,
		Type:        eventType,
		Message:     message,
		PayloadJSON: payloadJSON,
		CreatedAt:   now,
	})
}

func (s *Service) uniqueTimestamp() time.Time {
	return time.Now().UTC().Add(time.Duration(atomic.AddUint64(&s.eventSeq, 1)) * time.Nanosecond)
}

func toArtifactRefs(items []string) []core.ArtifactRef {
	out := make([]core.ArtifactRef, 0, len(items))
	for _, item := range items {
		out = append(out, core.ArtifactRef(item))
	}
	return out
}

type taskDispatchProvenance struct {
	SourceSnapshotID         string
	SourceSnapshotVersionID  string
	SourceFrontierSnapshotID string
	SourceRefName            string
	ContinuationID           string
	DecisionKind             string
}

func (s *Service) taskDispatchProvenance(ctx context.Context, runID core.RunID, task core.Task) taskDispatchProvenance {
	if s.doujiaGit == nil {
		return taskDispatchProvenance{}
	}
	out := taskDispatchProvenance{
		SourceRefName:  doujiagit.DefaultRefName,
		ContinuationID: string(task.StageID),
		DecisionKind:   doujiagit.RefMoveModeAdvance,
	}
	if ref, err := s.doujiaGit.GetRef(ctx, runID, doujiagit.DefaultRefName); err == nil {
		out.SourceFrontierSnapshotID = ref.FrontierSnapshotID
	}
	sourceTaskID, ok := primaryTaskDependency(task)
	if !ok && task.ParentID != nil {
		sourceTaskID = *task.ParentID
		ok = true
	}
	if !ok {
		return out
	}
	snapshots, err := s.doujiaGit.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		return out
	}
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].TaskID != sourceTaskID {
			continue
		}
		out.SourceSnapshotID = snapshots[i].SnapshotID
		out.SourceSnapshotVersionID = snapshots[i].SnapshotVersionID
		return out
	}
	return out
}

func logicalSnapshotIDForTask(task core.Task) string {
	stageID := strings.TrimSpace(string(task.StageID))
	if stageID == "" {
		stageID = strings.TrimSpace(string(task.ID))
	}
	if stageID == "" {
		return ""
	}
	return "stage:" + stageID
}

func artifactRefsToStrings(items []core.ArtifactRef) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	return out
}

func outputBagIDsFromCommit(transition pipeline.TransitionSpec, result core.TaskResultCode, commit *core.CommitReceipt, outputBagIDs []string) (map[string]string, error) {
	return outputBagIDsFromCommitForInstance(transition, result, commit, outputBagIDs, core.PipelineInstance{})
}

func outputBagIDsFromCommitForInstance(transition pipeline.TransitionSpec, result core.TaskResultCode, commit *core.CommitReceipt, outputBagIDs []string, instance core.PipelineInstance) (map[string]string, error) {
	lists, err := outputBagIDListsFromCommitForInstance(transition, result, commit, outputBagIDs, instance)
	if err != nil {
		return nil, err
	}
	if len(lists) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(lists))
	for key, values := range lists {
		if len(values) == 0 {
			continue
		}
		out[key] = values[len(values)-1]
	}
	return out, nil
}

func outputBagIDListsFromCommit(transition pipeline.TransitionSpec, result core.TaskResultCode, commit *core.CommitReceipt, outputBagIDs []string, instance core.PipelineInstance) (map[string][]string, error) {
	return outputBagIDListsFromCommitForInstance(transition, result, commit, outputBagIDs, instance)
}

func outputBagIDListsFromCommitForInstance(transition pipeline.TransitionSpec, result core.TaskResultCode, commit *core.CommitReceipt, outputBagIDs []string, instance core.PipelineInstance) (map[string][]string, error) {
	if commit == nil || len(outputBagIDs) == 0 {
		return nil, nil
	}
	bags := commit.EffectiveCommittedBags()
	if len(bags) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(outputBagIDs))
	declared := outputBagSpecsByKey(outputBagSpecsForResult(transition, result))
	hasContract := len(declared) > 0 || len(transition.OutputBagsByResult) > 0 || len(transition.OutputBags) > 0
	for i, bagID := range outputBagIDs {
		if i >= len(bags) {
			break
		}
		bagID = strings.TrimSpace(bagID)
		if bagID == "" {
			continue
		}
		returnName := strings.TrimSpace(bags[i].Name)
		if returnName == "" {
			continue
		}
		bag, declaredForResult := declared[returnName]
		if hasContract && !declaredForResult {
			return nil, fmt.Errorf("transition %q result %q produced undeclared output bag %q", transition.ID, result, returnName)
		}
		indexes := cloneControlStringMap(bags[i].Indexes)
		outputName := returnName
		collection := false
		if declaredForResult {
			indexes = mergeMissingBagIndexes(indexes, bag, instance)
			if strings.TrimSpace(bag.Name) != "" {
				outputName = strings.TrimSpace(bag.Name)
			}
			collection = bag.Collection
		}
		indexedKey := indexedBagLookupKey(outputName, indexes)
		if err := appendOutputBagList(out, indexedKey, bagID, collection); err != nil {
			return nil, fmt.Errorf("transition %q result %q: %w", transition.ID, result, err)
		}
		if indexedKey != outputName {
			if err := appendOutputBagList(out, outputName, bagID, collection); err != nil {
				return nil, fmt.Errorf("transition %q result %q: %w", transition.ID, result, err)
			}
		}
	}
	for _, bag := range declared {
		if bag.Required != nil && *bag.Required {
			if len(out[bag.Name]) == 0 {
				return nil, fmt.Errorf("transition %q result %q missing required output bag %q", transition.ID, result, bag.Name)
			}
		}
	}
	return out, nil
}

func appendOutputBagList(out map[string][]string, key string, bagID string, collection bool) error {
	key = strings.TrimSpace(key)
	bagID = strings.TrimSpace(bagID)
	if key == "" || bagID == "" {
		return nil
	}
	if !collection && len(out[key]) > 0 {
		return fmt.Errorf("output bag %q is not a collection but got duplicate return", key)
	}
	out[key] = appendUniqueString(out[key], bagID)
	return nil
}

func mergeMissingBagIndexes(indexes map[string]string, bag pipeline.BagSpec, instance core.PipelineInstance) map[string]string {
	if len(bag.IndexedBy) == 0 {
		return indexes
	}
	if indexes == nil {
		indexes = make(map[string]string)
	}
	for _, key := range bag.IndexedBy {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(indexes[key]) != "" {
			continue
		}
		if value := resolveBagIndexValue(key, bag, instance); value != "" {
			indexes[key] = value
		}
	}
	if len(indexes) == 0 {
		return nil
	}
	return indexes
}

func resolveBagIndexValue(key string, bag pipeline.BagSpec, instance core.PipelineInstance) string {
	if expr := strings.TrimSpace(bag.Tags[key]); expr != "" {
		if value, err := resolvePipelineBindingExpr(instance, expr); err == nil {
			return strings.TrimSpace(value)
		}
	}
	if value := strings.TrimSpace(instance.Params[key]); value != "" {
		return value
	}
	if value := strings.TrimSpace(string(instance.AgentBindings[key])); value != "" {
		return value
	}
	return ""
}

func outputBagSpecsByKey(bags []pipeline.BagSpec) map[string]pipeline.BagSpec {
	out := make(map[string]pipeline.BagSpec, len(bags))
	for _, bag := range bags {
		key := callOutputReturnName(bag)
		if key != "" {
			out[key] = bag
		}
	}
	return out
}

func callOutputReturnName(bag pipeline.BagSpec) string {
	if name := strings.TrimSpace(bag.FromReturn); name != "" {
		return name
	}
	return strings.TrimSpace(bag.Name)
}

func outputBagSpecsForResult(transition pipeline.TransitionSpec, result core.TaskResultCode) []pipeline.BagSpec {
	if len(transition.OutputBagsByResult) > 0 {
		if bags, ok := transition.OutputBagsByResult[string(result)]; ok {
			return bags
		}
	}
	return transition.OutputBags
}

func outputBagIDsForTransition(transition pipeline.TransitionSpec, result core.TaskResultCode, outputBagIDs []string) map[string]string {
	out := bagIDsForSpecs(outputBagSpecsForResult(transition, result), outputBagIDs)
	if len(out) > 0 {
		return out
	}
	if len(outputBagIDs) == 0 {
		return nil
	}
	if len(outputBagIDs) == 1 {
		out = make(map[string]string, 1)
		out["default"] = outputBagIDs[0]
	}
	return out
}

func bagIDsForSpecs(bags []pipeline.BagSpec, bagIDs []string) map[string]string {
	if len(bags) == 0 || len(bagIDs) == 0 {
		return nil
	}
	out := make(map[string]string, len(bags))
	for i, bagID := range bagIDs {
		if i >= len(bags) {
			break
		}
		bagID = strings.TrimSpace(bagID)
		if bagID == "" {
			continue
		}
		bag := bags[i]
		if strings.TrimSpace(bag.Name) != "" {
			out[bag.Name] = bagID
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeOutputBags(instance core.PipelineInstance, outputBags map[string]string) core.PipelineInstance {
	if len(outputBags) == 0 {
		return instance
	}
	if instance.OutputBagIDs == nil {
		instance.OutputBagIDs = make(map[string]string)
	}
	if instance.OutputBagIDLists == nil {
		instance.OutputBagIDLists = make(map[string][]string)
	}
	for key, value := range outputBags {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		instance.OutputBagIDs[key] = value
		instance.OutputBagIDLists[key] = appendUniqueString(instance.OutputBagIDLists[key], value)
	}
	return instance
}

func mergeOutputBagLists(instance core.PipelineInstance, outputBags map[string][]string) core.PipelineInstance {
	if len(outputBags) == 0 {
		return instance
	}
	if instance.OutputBagIDs == nil {
		instance.OutputBagIDs = make(map[string]string)
	}
	if instance.OutputBagIDLists == nil {
		instance.OutputBagIDLists = make(map[string][]string)
	}
	for key, values := range outputBags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			instance.OutputBagIDs[key] = value
			instance.OutputBagIDLists[key] = appendUniqueString(instance.OutputBagIDLists[key], value)
		}
	}
	return instance
}

func applyReplaceBags(instance core.PipelineInstance, replaceBags []pipeline.BagSpec) core.PipelineInstance {
	for _, bag := range replaceBags {
		replacementID := replacementBagID(instance, bag)
		if strings.TrimSpace(replacementID) == "" {
			continue
		}
		keys := replacementBagKeys(instance, bag, replacementID)
		if instance.OutputBagIDs == nil {
			instance.OutputBagIDs = make(map[string]string)
		}
		if instance.OutputBagIDLists == nil {
			instance.OutputBagIDLists = make(map[string][]string)
		}
		if instance.InputBagIDs == nil {
			instance.InputBagIDs = make(map[string]string)
		}
		if instance.InputBagIDLists == nil {
			instance.InputBagIDLists = make(map[string][]string)
		}
		for _, key := range keys {
			instance.OutputBagIDs[key] = replacementID
			instance.OutputBagIDLists[key] = []string{replacementID}
			instance.InputBagIDs[key] = replacementID
			instance.InputBagIDLists[key] = []string{replacementID}
		}
	}
	return instance
}

func replacementBagID(instance core.PipelineInstance, bag pipeline.BagSpec) string {
	key := strings.TrimSpace(bag.Name)
	if key == "" {
		return ""
	}
	if value := strings.TrimSpace(instance.OutputBagIDs[key]); value != "" {
		return value
	}
	if values := instance.OutputBagIDLists[key]; len(values) > 0 {
		return values[len(values)-1]
	}
	return ""
}

func replacementBagKeys(instance core.PipelineInstance, bag pipeline.BagSpec, replacementID string) []string {
	keys := []string{}
	if strings.TrimSpace(bag.Name) != "" {
		keys = append(keys, bag.Name)
	}
	for key, value := range instance.OutputBagIDs {
		if value == replacementID {
			keys = append(keys, key)
		}
	}
	for key, values := range instance.OutputBagIDLists {
		for _, value := range values {
			if value == replacementID {
				keys = append(keys, key)
				break
			}
		}
	}
	return uniqueStrings(keys)
}

func singletonBagLists(in map[string]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, value := range in {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[key] = []string{value}
	}
	return out
}
