package orchestrator

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
	"fmt"
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
	if feedback.Result == core.TaskResultCodeFail {
		task.Status = core.TaskStatusFailed
	} else {
		task.Status = core.TaskStatusDone
	}
	task.OutputArtifactRefs = toArtifactRefs(feedback.ArtifactURIs)
	task.UpdatedAt = time.Now().UTC()
	if err := s.tasks.Update(ctx, task); err != nil {
		return err
	}

	run, err := s.runs.Get(ctx, task.RunID)
	if err != nil {
		return err
	}
	if feedback.Result == core.TaskResultCodeFail {
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

	nextTask.Status = core.TaskStatusDispatched
	if err := s.tasks.Create(ctx, nextTask); err != nil {
		return err
	}

	dispatch := core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        run.ID,
		TaskID:       nextTask.ID,
		DependsOn:    nextTask.DependsOn,
		AgentID:      nextTask.AgentID,
		Op:           nextStage.Op,
		ArtifactURIs: feedback.ArtifactURIs,
	}
	if s.logger != nil {
		_ = s.logger.LogTaskMeta(run.ID, "Orchestrator", "dispatch created", dispatch)
	}

	if nextStage.AgentRole == core.AgentRoleCEO && s.sessionDispatcher != nil {
		return s.sessionDispatcher.DispatchToSession(ctx, dispatch)
	}

	if _, err := s.provision.EnsureAgent(ctx, runtime.EnsureAgentRequest{
		RunID:       run.ID,
		Role:        nextStage.AgentRole,
		AgentID:     nextStage.AgentAlias,
		ProjectRoot: run.ProjectDir,
		RunConfig:   run.Config,
	}); err != nil {
		return err
	}
	return s.dispatcher.Dispatch(ctx, dispatch)
}

func findNextStage(spec pipeline.PipelineSpec, current core.StageID) (pipeline.StageSpec, bool) {
	for i, stage := range spec.Stages {
		if stage.ID == current && i+1 < len(spec.Stages) {
			return spec.Stages[i+1], true
		}
	}
	return pipeline.StageSpec{}, false
}

func toArtifactRefs(items []string) []core.ArtifactRef {
	out := make([]core.ArtifactRef, 0, len(items))
	for _, item := range items {
		out = append(out, core.ArtifactRef(item))
	}
	return out
}
