package bootstrap

import (
	"context"
	"fmt"

	agentcore "devflow/internal/agent/core"
)

type GenericRoleAgent struct {
	spec agentcore.RoleSpec
}

func NewGenericRoleAgent(spec agentcore.RoleSpec) *GenericRoleAgent {
	return &GenericRoleAgent{spec: spec}
}

func (a *GenericRoleAgent) Role() string {
	return a.spec.ID
}

func (a *GenericRoleAgent) Run(ctx context.Context, req agentcore.AgentRunRequest) (agentcore.AgentResult, error) {
	if req.ToolLoop == nil {
		return agentcore.AgentResult{}, fmt.Errorf("generic role %q requires tool loop", a.spec.ID)
	}
	return req.ToolLoop.Run(ctx, agentcore.ToolLoopRequest{
		Task:     req.Task,
		Bundle:   req.Bundle,
		OpSpec:   req.OpSpec,
		Prompt:   req.Prompt,
		Handlers: req.Handlers,
		LLM:      req.LLM,
	})
}
