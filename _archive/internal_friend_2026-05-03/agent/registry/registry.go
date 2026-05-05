package registry

import "doujia/internal/agent/core"

type AgentRegistry struct {
	agents map[string]core.Agent
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: map[string]core.Agent{}}
}

func (r *AgentRegistry) Register(agent core.Agent) {
	r.agents[agent.Role()] = agent
}

func (r *AgentRegistry) Get(role string) (core.Agent, bool) {
	agent, ok := r.agents[role]
	return agent, ok
}

type OpRegistry struct {
	specs map[string]core.OpSpec
}

func NewOpRegistry() *OpRegistry {
	return &OpRegistry{specs: map[string]core.OpSpec{}}
}

func (r *OpRegistry) Register(spec core.OpSpec) {
	r.specs[opKey(spec.Role, spec.Op)] = spec
}

func (r *OpRegistry) Get(role, op string) (core.OpSpec, bool) {
	spec, ok := r.specs[opKey(role, op)]
	return spec, ok
}

func opKey(role, op string) string {
	return role + "." + op
}

type HandlerRegistry struct {
	handlers map[string]core.Handler
}

func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: map[string]core.Handler{}}
}

func (r *HandlerRegistry) Register(handler core.Handler) {
	r.handlers[handler.Name()] = handler
}

func (r *HandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r.handlers[name]
	return handler, ok
}
