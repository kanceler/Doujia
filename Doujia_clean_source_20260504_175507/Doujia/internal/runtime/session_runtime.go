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
	Role          core.AgentRole
	AgentID       core.AgentID
	WorkspacePath string
	Agent         Agent
}

type SessionTemplate struct {
	Role    core.AgentRole
	AgentID core.AgentID
	Factory Agent
}

type SessionRuntime interface {
	CreateSession(ctx context.Context, run core.PipelineRun) (Session, error)
	GetSessionByRun(ctx context.Context, runID core.RunID) (Session, error)
	DispatchToSession(ctx context.Context, task core.TaskMetaData) error
}

type InMemorySessionRuntime struct {
	mu       sync.RWMutex
	byRunID  map[core.RunID]Session
	template SessionTemplate
	resolver InputBundleResolver
	logger   logging.RunLogger
	config   AgentRuntimeConfig
	sink     FeedbackSink
	execCtx  context.Context
}

func NewInMemorySessionRuntime(template SessionTemplate, logger logging.RunLogger, config AgentRuntimeConfig) *InMemorySessionRuntime {
	return &InMemorySessionRuntime{
		byRunID:  make(map[core.RunID]Session),
		template: normalizeSessionTemplate(template),
		logger:   logger,
		config:   config,
		execCtx:  context.Background(),
	}
}

func (r *InMemorySessionRuntime) SetFeedbackSink(sink FeedbackSink) {
	r.sink = sink
}

func (r *InMemorySessionRuntime) SetInputBundleResolver(resolver InputBundleResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolver = resolver
}

func (r *InMemorySessionRuntime) SetExecutionContext(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx == nil {
		r.execCtx = context.Background()
		return
	}
	r.execCtx = ctx
}

func (r *InMemorySessionRuntime) CreateSession(ctx context.Context, run core.PipelineRun) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runID := run.ID
	if existing, ok := r.byRunID[runID]; ok {
		return existing, nil
	}
	template := normalizeSessionTemplate(r.template)
	projectRoot := run.ProjectDir
	workspacePath := filepath.Join(projectRoot, "agents", string(template.AgentID))
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return Session{}, err
	}
	session := Session{
		ID:            core.SessionID(fmt.Sprintf("%s_session", runID)),
		RunID:         runID,
		Role:          template.Role,
		AgentID:       template.AgentID,
		WorkspacePath: workspacePath,
	}
	init := AgentInit{
		AgentID:       template.AgentID,
		RuntimeID:     core.RuntimeID(fmt.Sprintf("%s_%s_runtime", runID, template.AgentID)),
		RunID:         runID,
		RunRoot:       projectRoot,
		RunConfig:     run.Config,
		WorkspacePath: workspacePath,
	}
	deps, err := buildAgentDeps(init, r.config)
	if err != nil {
		return Session{}, err
	}
	session.Agent = template.Factory.Create(init, deps)
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
	r.mu.RLock()
	resolver := r.resolver
	r.mu.RUnlock()
	if resolver != nil && (len(task.InputBags) > 0 || len(task.InputBagIDs) > 0) && task.InputBundle == nil {
		inputBags := task.InputBags
		if len(inputBags) == 0 {
			inputBags = InputBagBindingsFromIDs(task.InputBagIDs)
		}
		bundle, err := resolver.ResolveInputBundle(ctx, inputBags)
		if err != nil {
			return fmt.Errorf("resolve input bags for session task %q: %w", task.TaskID, err)
		}
		task.InputBags = append([]core.BagBindingRef(nil), inputBags...)
		task.InputBundle = &bundle
	}
	if len(task.ArtifactURIs) == 0 && task.InputBundle != nil {
		task.ArtifactURIs = artifactURIsFromInputBundle(*task.InputBundle)
	}
	if r.logger != nil {
		_ = r.logger.LogTaskMeta(task.RunID, "SessionRuntime", "dispatch_to_session", task)
	}
	feedback, err := session.Agent.Execute(r.executionContext(ctx), task)
	if err != nil {
		feedback = core.TaskMetaData{
			Direction:    core.TaskDirectionFeedback,
			RunID:        task.RunID,
			TaskID:       task.TaskID,
			ParentID:     task.ParentID,
			DependsOnIDs: task.DependsOnIDs,
			AgentID:      session.AgentID,
			Op:           task.Op,
			InputBagIDs:  append([]string(nil), task.InputBagIDs...),
			InputBags:    append([]core.BagBindingRef(nil), task.InputBags...),
			Result:       core.TaskResultCodeFail,
		}
	}
	if r.sink != nil {
		return r.sink.OnFeedback(ctx, feedback)
	}
	return nil
}

func (r *InMemorySessionRuntime) executionContext(fallback context.Context) context.Context {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.execCtx != nil {
		return r.execCtx
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}

func normalizeSessionTemplate(template SessionTemplate) SessionTemplate {
	if template.Role == "" {
		template.Role = core.AgentRoleCEO
	}
	if template.AgentID == "" {
		template.AgentID = core.AgentID(template.Role)
	}
	return template
}
