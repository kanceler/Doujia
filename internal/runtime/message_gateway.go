package runtime

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"fmt"
	"sync"
)

type Binder interface {
	Bind(ctx context.Context, runID core.RunID, agentID core.AgentID, runtimeID core.RuntimeID) error
}

type Dispatcher interface {
	Dispatch(ctx context.Context, task core.TaskMetaData) error
}

type RuntimeSubmitter interface {
	Submit(ctx context.Context, runtimeID core.RuntimeID, task core.TaskMetaData) error
}

type MessageGateway struct {
	mu        sync.RWMutex
	bindings  map[agentRuntimeKey]core.RuntimeID
	submitter RuntimeSubmitter
	logger    logging.RunLogger
}

func NewMessageGateway(submitter RuntimeSubmitter, logger logging.RunLogger) *MessageGateway {
	return &MessageGateway{
		bindings:  make(map[agentRuntimeKey]core.RuntimeID),
		submitter: submitter,
		logger:    logger,
	}
}

func (g *MessageGateway) Bind(_ context.Context, runID core.RunID, agentID core.AgentID, runtimeID core.RuntimeID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bindings[newAgentRuntimeKey(runID, agentID)] = runtimeID
	return nil
}

func (g *MessageGateway) Dispatch(ctx context.Context, task core.TaskMetaData) error {
	g.mu.RLock()
	runtimeID, ok := g.bindings[newAgentRuntimeKey(task.RunID, task.AgentID)]
	g.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent %q in run %q is not bound to a runtime", task.AgentID, task.RunID)
	}
	if g.logger != nil {
		_ = g.logger.LogTaskMeta(task.RunID, "MessageGateway", fmt.Sprintf("dispatch_to runtime_id=%s", runtimeID), task)
	}
	return g.submitter.Submit(ctx, runtimeID, task)
}

type agentRuntimeKey struct {
	runID   core.RunID
	agentID core.AgentID
}

func newAgentRuntimeKey(runID core.RunID, agentID core.AgentID) agentRuntimeKey {
	return agentRuntimeKey{runID: runID, agentID: agentID}
}
