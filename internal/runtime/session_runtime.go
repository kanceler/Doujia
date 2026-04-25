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
	CreateSession(ctx context.Context, runID core.RunID, projectRoot string) (Session, error)
	GetSessionByRun(ctx context.Context, runID core.RunID) (Session, error)
}

type InMemorySessionRuntime struct {
	mu         sync.RWMutex
	byRunID    map[core.RunID]Session
	ceoFactory Agent
	logger     logging.RunLogger
}

func NewInMemorySessionRuntime(ceoFactory Agent, logger logging.RunLogger) *InMemorySessionRuntime {
	return &InMemorySessionRuntime{
		byRunID:    make(map[core.RunID]Session),
		ceoFactory: ceoFactory,
		logger:     logger,
	}
}

func (r *InMemorySessionRuntime) CreateSession(_ context.Context, runID core.RunID, projectRoot string) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byRunID[runID]; ok {
		return existing, nil
	}
	workspacePath := filepath.Join(projectRoot, "agents", "ceo")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return Session{}, err
	}
	session := Session{
		ID:            core.SessionID(fmt.Sprintf("%s_session", runID)),
		RunID:         runID,
		CEOAgent:      "ceo",
		WorkspacePath: workspacePath,
		Agent: r.ceoFactory.Create(AgentInit{
			AgentID:       "ceo",
			RuntimeID:     core.RuntimeID(fmt.Sprintf("%s_ceo_runtime", runID)),
			RunID:         runID,
			WorkspacePath: workspacePath,
		}),
	}
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
