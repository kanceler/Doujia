package app

import (
	"fmt"
	"strings"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/core"
	"devflow/internal/runtime"
)

type agentRuntimePlan struct {
	sessionTemplate runtime.SessionTemplate
	sessionRoles    []core.AgentRole
	taskFactories   map[core.AgentRole]runtime.Agent
}

type agentFactoryResolver func(role core.AgentRole) runtime.Agent

func buildAgentRuntimePlan(specs []agentcore.RoleSpec, factoryForRole agentFactoryResolver) (agentRuntimePlan, error) {
	if factoryForRole == nil {
		return agentRuntimePlan{}, fmt.Errorf("agent factory resolver is required")
	}
	plan := agentRuntimePlan{
		taskFactories: make(map[core.AgentRole]runtime.Agent),
	}
	for _, spec := range specs {
		roleID := core.AgentRole(strings.TrimSpace(spec.ID))
		if roleID == "" {
			continue
		}
		factory := factoryForRole(roleID)
		switch normalizeInteractionMode(spec.InteractionMode) {
		case "session":
			plan.sessionRoles = append(plan.sessionRoles, roleID)
			if len(plan.sessionRoles) > 1 {
				return agentRuntimePlan{}, fmt.Errorf("multiple session roles are not supported yet: %v", plan.sessionRoles)
			}
			plan.sessionTemplate = runtime.SessionTemplate{
				Role:    roleID,
				AgentID: core.AgentID(roleID),
				Factory: factory,
			}
		case "task":
			plan.taskFactories[roleID] = factory
		}
	}
	if len(plan.sessionRoles) == 0 {
		return agentRuntimePlan{}, fmt.Errorf("agent runtime plan requires exactly one session role, but none were registered")
	}
	return plan, nil
}

func normalizeInteractionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "session":
		return "session"
	case "", "task", "hybrid":
		return "task"
	default:
		return "task"
	}
}
