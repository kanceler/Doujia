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

type FeedbackSink interface {
	OnFeedback(ctx context.Context, feedback core.TaskMetaData) error
}

type EnsureAgentRequest struct {
	RunID       core.RunID
	Role        core.AgentRole
	AgentID     core.AgentID
	ProjectRoot string
	RunConfig   core.RunConfig
}

type EnsureAgentResult struct {
	AgentID       core.AgentID
	RuntimeID     core.RuntimeID
	WorkspacePath string
}

type AgentProvisioner interface {
	EnsureAgent(ctx context.Context, req EnsureAgentRequest) (EnsureAgentResult, error)
}

type agentInstance struct {
	runID         core.RunID
	role          core.AgentRole
	agentID       core.AgentID
	runtimeID     core.RuntimeID
	workspacePath string
	agent         Agent
}

type queuedTask struct {
	runtimeID core.RuntimeID
	task      core.TaskMetaData
}

type TaskRuntime struct {
	mu         sync.RWMutex
	factories  map[core.AgentRole]Agent
	instances  map[core.RuntimeID]agentInstance
	agentIndex map[agentRuntimeKey]core.RuntimeID
	workerCount int
	queue      chan queuedTask
	sink       FeedbackSink
	binder     Binder
	nextID     int
	logger     logging.RunLogger
	config     AgentRuntimeConfig
	execCtx    context.Context
}

func NewTaskRuntime(workerCount int, sink FeedbackSink, logger logging.RunLogger, config AgentRuntimeConfig) *TaskRuntime {
	if workerCount <= 0 {
		workerCount = 1
	}
	tr := &TaskRuntime{
		factories:  make(map[core.AgentRole]Agent),
		instances:  make(map[core.RuntimeID]agentInstance),
		agentIndex: make(map[agentRuntimeKey]core.RuntimeID),
		workerCount: workerCount,
		queue:      make(chan queuedTask, 128),
		sink:       sink,
		logger:     logger,
		config:     config,
		execCtx:    context.Background(),
	}
	for i := 0; i < workerCount; i++ {
		go tr.worker()
	}
	return tr
}

func (r *TaskRuntime) WorkerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.workerCount
}

func (r *TaskRuntime) SetBinder(binder Binder) {
	r.binder = binder
}

func (r *TaskRuntime) SetFeedbackSink(sink FeedbackSink) {
	r.sink = sink
}

func (r *TaskRuntime) SetExecutionContext(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx == nil {
		r.execCtx = context.Background()
		return
	}
	r.execCtx = ctx
}

func (r *TaskRuntime) RegisterTemplate(role core.AgentRole, factory Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[role] = factory
}

func (r *TaskRuntime) EnsureAgent(ctx context.Context, req EnsureAgentRequest) (EnsureAgentResult, error) {
	r.mu.Lock()
	key := newAgentRuntimeKey(req.RunID, req.AgentID)
	if runtimeID, ok := r.agentIndex[key]; ok {
		r.mu.Unlock()
		return EnsureAgentResult{AgentID: req.AgentID, RuntimeID: runtimeID}, nil
	}

	template, ok := r.factories[req.Role]
	if !ok {
		r.mu.Unlock()
		return EnsureAgentResult{}, fmt.Errorf("no template registered for role %q", req.Role)
	}

	r.nextID++
	runtimeID := core.RuntimeID(fmt.Sprintf("%04d", r.nextID))
	workspacePath := filepath.Join(req.ProjectRoot, "agents", string(req.AgentID))
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		r.mu.Unlock()
		return EnsureAgentResult{}, err
	}
	instance := agentInstance{
		runID:         req.RunID,
		role:          req.Role,
		agentID:       req.AgentID,
		runtimeID:     runtimeID,
		workspacePath: workspacePath,
	}
	init := AgentInit{
		AgentID:       req.AgentID,
		RuntimeID:     runtimeID,
		RunID:         req.RunID,
		RunRoot:       req.ProjectRoot,
		RunConfig:     req.RunConfig,
		WorkspacePath: workspacePath,
	}
	deps, err := buildAgentDeps(init, r.config)
	if err != nil {
		r.mu.Unlock()
		return EnsureAgentResult{}, err
	}
	instance.agent = template.Create(init, deps)
	r.instances[runtimeID] = instance
	r.agentIndex[key] = runtimeID
	r.mu.Unlock()

	if r.binder != nil {
		if err := r.binder.Bind(ctx, req.RunID, req.AgentID, runtimeID); err != nil {
			return EnsureAgentResult{}, err
		}
	}
	if r.logger != nil {
		_ = r.logger.Log(req.RunID, "TaskRuntime", fmt.Sprintf("agent created: role=%s agent_id=%s runtime_id=%s workspace=%s", req.Role, req.AgentID, runtimeID, workspacePath))
	}

	return EnsureAgentResult{AgentID: req.AgentID, RuntimeID: runtimeID, WorkspacePath: workspacePath}, nil
}

func (r *TaskRuntime) Submit(_ context.Context, runtimeID core.RuntimeID, task core.TaskMetaData) error {
	if r.logger != nil {
		_ = r.logger.LogTaskMeta(task.RunID, "TaskRuntime", fmt.Sprintf("task queued for runtime_id=%s", runtimeID), task)
	}
	r.queue <- queuedTask{runtimeID: runtimeID, task: task}
	return nil
}

func (r *TaskRuntime) worker() {
	for item := range r.queue {
		r.mu.RLock()
		instance, ok := r.instances[item.runtimeID]
		r.mu.RUnlock()
		if !ok {
			continue
		}
		if r.logger != nil {
			_ = r.logger.LogTaskMeta(item.task.RunID, "TaskRuntime", fmt.Sprintf("worker start runtime_id=%s agent_id=%s", item.runtimeID, instance.agentID), item.task)
		}
		feedback, err := instance.agent.Execute(r.executionContext(), item.task)
		if err != nil {
			feedback = core.TaskMetaData{
				Direction:    core.TaskDirectionFeedback,
				RunID:        item.task.RunID,
				TaskID:       item.task.TaskID,
				ParentID:     item.task.ParentID,
				DependsOnIDs: item.task.DependsOnIDs,
				AgentID:      instance.agentID,
				Op:           item.task.Op,
				InputBagIDs:  item.task.InputBagIDs,
				InputBags:    append([]core.BagBindingRef(nil), item.task.InputBags...),
				Result:       core.TaskResultCodeFail,
			}
		}
		if r.logger != nil {
			_ = r.logger.LogTaskMeta(item.task.RunID, "TaskRuntime", fmt.Sprintf("worker feedback runtime_id=%s agent_id=%s", item.runtimeID, instance.agentID), feedback)
		}
		if r.sink != nil {
			if err := r.sink.OnFeedback(context.Background(), feedback); err != nil && r.logger != nil {
				_ = r.logger.Log(item.task.RunID, "TaskRuntime", fmt.Sprintf("feedback sink error task_id=%s: %v", feedback.TaskID, err))
			}
		}
	}
}

func (r *TaskRuntime) executionContext() context.Context {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.execCtx == nil {
		return context.Background()
	}
	return r.execCtx
}
