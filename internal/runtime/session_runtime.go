package runtime

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Session struct {
	ID            core.SessionID
	RunID         core.RunID
	CEOAgent      core.AgentID
	WorkspacePath string
	Agent         Agent
}

type SessionRuntime interface {
	CreateSession(ctx context.Context, run core.PipelineRun) (Session, error)
	GetSessionByRun(ctx context.Context, runID core.RunID) (Session, error)
	DispatchToSession(ctx context.Context, task core.TaskMetaData) error
}

type InMemorySessionRuntime struct {
	mu         sync.RWMutex
	byRunID    map[core.RunID]Session
	ceoFactory Agent
	logger     logging.RunLogger
	config     AgentRuntimeConfig
	sink       FeedbackSink
}

func NewInMemorySessionRuntime(ceoFactory Agent, logger logging.RunLogger, config AgentRuntimeConfig) *InMemorySessionRuntime {
	return &InMemorySessionRuntime{
		byRunID:    make(map[core.RunID]Session),
		ceoFactory: ceoFactory,
		logger:     logger,
		config:     config,
	}
}

func (r *InMemorySessionRuntime) SetFeedbackSink(sink FeedbackSink) {
	r.sink = sink
}

func (r *InMemorySessionRuntime) CreateSession(ctx context.Context, run core.PipelineRun) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runID := run.ID
	if existing, ok := r.byRunID[runID]; ok {
		return existing, nil
	}
	projectRoot := run.ProjectDir
	workspacePath := filepath.Join(projectRoot, "agents", "ceo")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return Session{}, err
	}
	session := Session{
		ID:            core.SessionID(fmt.Sprintf("%s_session", runID)),
		RunID:         runID,
		CEOAgent:      "ceo",
		WorkspacePath: workspacePath,
	}
	init := AgentInit{
		AgentID:       "ceo",
		RuntimeID:     core.RuntimeID(fmt.Sprintf("%s_ceo_runtime", runID)),
		RunID:         runID,
		RunRoot:       projectRoot,
		RunConfig:     run.Config,
		WorkspacePath: workspacePath,
	}
	if err := loadTaskHistory(ctx, &init, r.config); err != nil {
		return Session{}, err
	}
	deps, err := buildAgentDeps(init, r.config)
	if err != nil {
		return Session{}, err
	}
	session.Agent = r.ceoFactory.Create(init, deps)
	r.byRunID[runID] = session
	if r.logger != nil {
		_ = r.logger.Log(runID, "SessionRuntime", fmt.Sprintf("session created: session_id=%s workspace=%s", session.ID, session.WorkspacePath))
	}
	return session, nil
}

func (r *InMemorySessionRuntime) GetSessionByRun(_ context.Context, runID core.RunID) (Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.byRunID[runID]
	if !ok {
		return Session{}, fmt.Errorf("session for run %q not found", runID)
	}
	return session, nil
}

func (r *InMemorySessionRuntime) DispatchToSession(ctx context.Context, task core.TaskMetaData) error {
	session, err := r.GetSessionByRun(ctx, task.RunID)
	if err != nil {
		return err
	}
	if r.logger != nil {
		_ = r.logger.LogTaskMeta(task.RunID, "SessionRuntime", "dispatch_to_session", task)
	}
	feedback, err := session.Agent.Execute(ctx, task)
	if err != nil {
		feedback = core.TaskMetaData{
			Direction: core.TaskDirectionFeedback,
			RunID:     task.RunID,
			TaskID:    task.TaskID,
			ParentID:  task.ParentID,
			DependsOn: task.DependsOn,
			AgentID:   session.CEOAgent,
			Op:        task.Op,
			Result:    core.TaskResultCodeFail,
		}
	}
	if r.sink != nil {
		return r.sink.OnFeedback(ctx, feedback)
	}
	return nil
}
