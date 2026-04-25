package runtime

import (
	"context"
	"devflow/internal/core"
)

type Agent interface {
	Create(init AgentInit) Agent
	Execute(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error)
}

type AgentInit struct {
	AgentID       core.AgentID
	RuntimeID     core.RuntimeID
	RunID         core.RunID
	WorkspacePath string
}
