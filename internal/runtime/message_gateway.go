package runtime

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/logging"
	"fmt"
	"sync"
)

type Binder interface {
	Bind(ctx context.Context, agentID core.AgentID, runtimeID core.RuntimeID) error
}

type Dispatcher interface {
	Dispatch(ctx context.Context, task core.TaskMetaData) error
}

type RuntimeSubmitter interface {
	Submit(ctx context.Context, runtimeID core.RuntimeID, task core.TaskMetaData) error
}

type MessageGateway struct {
	mu        sync.RWMutex
	bindings  map[core.AgentID]core.RuntimeID
	submitter RuntimeSubmitter
	logger    logging.RunLogger
}

func NewMessageGateway(submitter RuntimeSubmitter, logger logging.RunLogger) *MessageGateway {
	return &MessageGateway{
		bindings:  make(map[core.AgentID]core.RuntimeID),
		submitter: submitter,
		logger:    logger,
	}
}

func (g *MessageGateway) Bind(_ context.Context, agentID core.AgentID, runtimeID core.RuntimeID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bindings[agentID] = runtimeID
	return nil
}

func (g *MessageGateway) Dispatch(ctx context.Context, task core.TaskMetaData) error {
	g.mu.RLock()
	runtimeID, ok := g.bindings[task.AgentID]
	g.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent %q is not bound to a runtime", task.AgentID)
	}
	if g.logger != nil {
		_ = g.logger.LogTaskMeta(task.RunID, "MessageGateway", fmt.Sprintf("dispatch_to runtime_id=%s", runtimeID), task)
	}
	return g.submitter.Submit(ctx, runtimeID, task)
}
