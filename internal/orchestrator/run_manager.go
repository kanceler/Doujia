package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"devflow/internal/core"
	"devflow/internal/logging"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
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
	if err := writeDeliveryConfigArtifact(projectDir, config.Delivery); err != nil {
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

func DeliveryConfigArtifactURI(runID core.RunID) string {
	return fmt.Sprintf("projects/%s/system/run_delivery_config.json", runID)
}

func writeDeliveryConfigArtifact(projectDir string, config core.DeliveryConfig) error {
	config = normalizeDeliveryConfig(config)
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	fullPath := filepath.Join(projectDir, "system", "run_delivery_config.json")
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, append(content, '\n'), 0o644)
}

func normalizeDeliveryConfig(config core.DeliveryConfig) core.DeliveryConfig {
	if config.MaxCoderAgents <= 0 {
		config.MaxCoderAgents = 1
	}
	if config.MaxTesterAgents <= 0 {
		config.MaxTesterAgents = 1
	}
	if config.Git.MainBranch == "" {
		config.Git.MainBranch = "main"
	}
	return config
}
