package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	tasks        repo.TaskRepository
	iterations   repo.RunIterationRepository
	messages     repo.SessionMessageRepository
	artifacts    repo.SessionArtifactRepository
	improvements repo.ImprovementItemRepository
	sessions     runtime.SessionRuntime
	orchestrator *Service
	projectsRoot string
	logger       logging.RunLogger
}

func NewRunManager(
	pipelines pipeline.Registry,
	runs repo.RunRepository,
	tasks repo.TaskRepository,
	iterations repo.RunIterationRepository,
	messages repo.SessionMessageRepository,
	artifacts repo.SessionArtifactRepository,
	improvements repo.ImprovementItemRepository,
	sessions runtime.SessionRuntime,
	orchestrator *Service,
	projectsRoot string,
	logger logging.RunLogger,
) *RunManager {
	return &RunManager{
		pipelines:    pipelines,
		runs:         runs,
		tasks:        tasks,
		iterations:   iterations,
		messages:     messages,
		artifacts:    artifacts,
		improvements: improvements,
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
	projectsRoot, err := filepath.Abs(filepath.Clean(m.projectsRoot))
	if err != nil {
		return err
	}
	projectDir := filepath.Join(projectsRoot, string(runID))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}
	if err := writeDeliveryConfigArtifact(projectDir, config.Delivery); err != nil {
		return err
	}
	now := time.Now().UTC()
	err = m.runs.Create(ctx, core.PipelineRun{
		ID:                 runID,
		PipelineID:         pipelineID,
		Status:             core.RunStatusCreated,
		ProjectDir:         projectDir,
		CurrentIterationNo: 1,
		Config:             config,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err == nil && m.logger != nil {
		_ = m.logger.Log(runID, "RunManager", fmt.Sprintf("run created: pipeline=%s project=%s", pipelineID, projectDir))
	}
	if err == nil && m.iterations != nil {
		_ = m.iterations.Create(ctx, repo.RunIterationRecord{
			RunID:       runID,
			IterationNo: 1,
			Status:      core.RunStatusCreated,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
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

func (m *RunManager) ApproveAcceptance(ctx context.Context, runID core.RunID, comment string) (core.PipelineRun, error) {
	run, err := m.runs.Get(ctx, runID)
	if err != nil {
		return core.PipelineRun{}, err
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		return core.PipelineRun{}, fmt.Errorf("run %q is not awaiting acceptance", runID)
	}
	run.Status = core.RunStatusCompleted
	run.UpdatedAt = time.Now().UTC()
	if err := m.runs.Update(ctx, run); err != nil {
		return core.PipelineRun{}, err
	}
	if m.iterations != nil {
		iterations, _ := m.iterations.ListByRun(ctx, runID)
		for _, iteration := range iterations {
			if iteration.IterationNo != run.CurrentIterationNo {
				continue
			}
			iteration.Status = core.RunStatusCompleted
			iteration.UpdatedAt = run.UpdatedAt
			_ = m.iterations.Update(ctx, iteration)
			break
		}
	}
	if strings.TrimSpace(comment) != "" && m.messages != nil {
		_ = m.messages.Create(ctx, repo.SessionMessageRecord{
			ID:          fmt.Sprintf("approval_%d", time.Now().UTC().UnixNano()),
			RunID:       runID,
			IterationNo: run.CurrentIterationNo,
			Role:        "user",
			MessageType: "acceptance_approve",
			Content:     comment,
			CreatedAt:   run.UpdatedAt,
		})
	}
	return run, nil
}

func (m *RunManager) ContinueIteration(ctx context.Context, runID core.RunID, selectedItemIDs []string, freeformText string) (core.PipelineRun, error) {
	run, err := m.runs.Get(ctx, runID)
	if err != nil {
		return core.PipelineRun{}, err
	}
	if run.Status != core.RunStatusAwaitingAcceptance {
		return core.PipelineRun{}, fmt.Errorf("run %q is not awaiting acceptance", runID)
	}
	now := time.Now().UTC()
	nextIterationNo := run.CurrentIterationNo + 1
	run.Status = core.RunStatusRunning
	run.CurrentIterationNo = nextIterationNo
	run.LatestAcceptanceCheckpointTaskID = ""
	run.UpdatedAt = now
	if err := m.runs.Update(ctx, run); err != nil {
		return core.PipelineRun{}, err
	}
	if m.iterations != nil {
		_ = m.iterations.Create(ctx, repo.RunIterationRecord{
			RunID:              runID,
			IterationNo:        nextIterationNo,
			StartFrontierID:    run.LatestDeliveryFrontierID,
			Status:             core.RunStatusRunning,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}
	if m.messages != nil {
		summary := strings.TrimSpace(freeformText)
		if len(selectedItemIDs) > 0 {
			summary = strings.TrimSpace(summary + "\nselected improvement items: " + strings.Join(selectedItemIDs, ","))
		}
		if summary != "" {
			_ = m.messages.Create(ctx, repo.SessionMessageRecord{
				ID:          fmt.Sprintf("continue_%d", now.UnixNano()),
				RunID:       runID,
				IterationNo: nextIterationNo,
				Role:        "user",
				MessageType: "acceptance_continue",
				Content:     summary,
				CreatedAt:   now,
			})
		}
	}
	if m.improvements != nil {
		items, _ := m.improvements.ListByRun(ctx, runID)
		selected := make(map[string]bool, len(selectedItemIDs))
		for _, id := range selectedItemIDs {
			selected[strings.TrimSpace(id)] = true
		}
		for _, item := range items {
			if !selected[item.ItemID] {
				continue
			}
			item.Status = "selected"
			item.UpdatedAt = now
			_ = m.improvements.Update(ctx, item)
		}
	}
	if err := m.createIterationRequirementTask(ctx, run, nextIterationNo); err != nil {
		return core.PipelineRun{}, err
	}
	return run, nil
}

func (m *RunManager) createIterationRequirementTask(ctx context.Context, run core.PipelineRun, iterationNo int) error {
	taskID := core.TaskID(fmt.Sprintf("iter_%02d__ceo_write_requirement", iterationNo))
	return m.tasks.Create(ctx, core.Task{
		ID:        taskID,
		RunID:     run.ID,
		StageID:   "ceo_write_requirement",
		AgentRole: core.AgentRoleCEO,
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
		Status:    core.TaskStatusWaitingExternal,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
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
	if config.GlobalTestTimeoutSeconds <= 0 {
		config.GlobalTestTimeoutSeconds = 60
	}
	return config
}
