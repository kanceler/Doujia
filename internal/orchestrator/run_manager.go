package orchestrator

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RunManager struct {
	pipelines    pipeline.Registry
	runs         repo.RunRepository
	sessions     runtime.SessionRuntime
	orchestrator *Service
	projectsRoot string
	logger       logging.RunLogger
}

func NewRunManager(
	pipelines pipeline.Registry,
	runs repo.RunRepository,
	sessions runtime.SessionRuntime,
	orchestrator *Service,
	projectsRoot string,
	logger logging.RunLogger,
) *RunManager {
	return &RunManager{
		pipelines:    pipelines,
		runs:         runs,
		sessions:     sessions,
		orchestrator: orchestrator,
		projectsRoot: projectsRoot,
		logger:       logger,
	}
}

func (m *RunManager) CreateRun(ctx context.Context, runID core.RunID, pipelineID core.PipelineID, config core.RunConfig) error {
	if _, err := m.pipelines.Get(ctx, pipelineID); err != nil {
		return err
	}
	projectDir := filepath.Join(m.projectsRoot, string(runID))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	err := m.runs.Create(ctx, core.PipelineRun{
		ID:         runID,
		PipelineID: pipelineID,
		Status:     core.RunStatusCreated,
		ProjectDir: projectDir,
		Config:     config,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err == nil && m.logger != nil {
		_ = m.logger.Log(runID, "RunManager", fmt.Sprintf("run created: pipeline=%s project=%s", pipelineID, projectDir))
	}
	return err
}

func (m *RunManager) StartRun(ctx context.Context, runID core.RunID) error {
	run, err := m.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	session, err := m.sessions.CreateSession(ctx, run)
	if err != nil {
		return err
	}
	run.SessionID = session.ID
	run.Status = core.RunStatusRunning
	run.UpdatedAt = time.Now().UTC()
	if err := m.runs.Update(ctx, run); err != nil {
		return err
	}
	if m.logger != nil {
		_ = m.logger.Log(runID, "RunManager", fmt.Sprintf("run started: project=%s session=%s", run.ProjectDir, session.ID))
	}
	return m.orchestrator.Start(ctx, runID)
}
