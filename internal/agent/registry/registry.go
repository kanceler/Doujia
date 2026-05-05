package registry

import (
	"fmt"
	"sort"
	"strings"

	"devflow/internal/agent/core"
)

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
	specsByRoleOp map[string]core.OpSpec
	specsByID     map[string]core.OpSpec
}

func NewOpRegistry() *OpRegistry {
	return &OpRegistry{
		specsByRoleOp: map[string]core.OpSpec{},
		specsByID:     map[string]core.OpSpec{},
	}
}

func (r *OpRegistry) Register(spec core.OpSpec) {
	r.specsByRoleOp[opKey(spec.Role, spec.Op)] = spec
	if opID := strings.TrimSpace(spec.Role + "." + spec.Op); opID != "" {
		r.specsByID[opID] = spec
	}
}

func (r *OpRegistry) RegisterWithID(opID string, spec core.OpSpec) {
	r.specsByRoleOp[opKey(spec.Role, spec.Op)] = spec
	opID = strings.TrimSpace(opID)
	if opID != "" {
		r.specsByID[opID] = spec
	}
}

func (r *OpRegistry) Get(role, op string) (core.OpSpec, bool) {
	spec, ok := r.specsByRoleOp[opKey(role, op)]
	return spec, ok
}

func (r *OpRegistry) GetByID(opID string) (core.OpSpec, bool) {
	spec, ok := r.specsByID[strings.TrimSpace(opID)]
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

type PluginRegistry struct {
	agents    *AgentRegistry
	ops       *OpRegistry
	handlers  *HandlerRegistry
	roleSpecs map[string]core.RoleSpec
	opRegs    map[string]core.OpRegistration
}

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		agents:    NewAgentRegistry(),
		ops:       NewOpRegistry(),
		handlers:  NewHandlerRegistry(),
		roleSpecs: map[string]core.RoleSpec{},
		opRegs:    map[string]core.OpRegistration{},
	}
}

func (r *PluginRegistry) Agents() *AgentRegistry {
	return r.agents
}

func (r *PluginRegistry) Ops() *OpRegistry {
	return r.ops
}

func (r *PluginRegistry) Handlers() *HandlerRegistry {
	return r.handlers
}

func (r *PluginRegistry) RegisterHandler(reg core.HandlerRegistration) error {
	reg = core.NormalizeHandlerRegistration(reg)
	if reg.Handler == nil {
		return fmt.Errorf("handler %q implementation is required", reg.Spec.ID)
	}
	if reg.Spec.ID == "" {
		return fmt.Errorf("handler_id is required")
	}
	if reg.Spec.ID != reg.Handler.Name() {
		return fmt.Errorf("handler_id %q does not match handler.Name() %q", reg.Spec.ID, reg.Handler.Name())
	}
	r.handlers.Register(reg.Handler)
	return nil
}

func (r *PluginRegistry) RegisterOp(reg core.OpRegistration) error {
	reg = core.NormalizeOpRegistration(reg)
	if reg.ID == "" {
		return fmt.Errorf("op_id is required")
	}
	if strings.TrimSpace(reg.Spec.Role) == "" {
		return fmt.Errorf("op %q role is required", reg.ID)
	}
	if strings.TrimSpace(reg.Spec.Op) == "" {
		return fmt.Errorf("op %q name is required", reg.ID)
	}
	required := reg.RequiredHandlerIDs
	if len(required) == 0 {
		required = allowedToolNames(reg.Spec)
	}
	for _, handlerID := range required {
		if _, ok := r.handlers.Get(handlerID); !ok {
			return fmt.Errorf("op %q requires unknown handler %q", reg.ID, handlerID)
		}
	}
	allowed := reg.AllowedHandlerIDs
	if len(allowed) == 0 {
		allowed = allowedToolNames(reg.Spec)
	}
	for _, handlerID := range allowed {
		if _, ok := r.handlers.Get(handlerID); !ok {
			return fmt.Errorf("op %q allows unknown handler %q", reg.ID, handlerID)
		}
	}
	r.ops.RegisterWithID(reg.ID, reg.Spec)
	r.opRegs[reg.ID] = reg
	return nil
}

func (r *PluginRegistry) RegisterRole(reg core.RoleRegistration) error {
	reg = core.NormalizeRoleRegistration(reg)
	if reg.Agent == nil {
		return fmt.Errorf("role %q implementation is required", reg.Spec.ID)
	}
	if reg.Spec.ID == "" {
		return fmt.Errorf("role_id is required")
	}
	if reg.Spec.ID != reg.Agent.Role() {
		return fmt.Errorf("role_id %q does not match agent.Role() %q", reg.Spec.ID, reg.Agent.Role())
	}
	for _, binding := range reg.Spec.SupportedOps {
		if binding.Name == "" {
			return fmt.Errorf("role %q supported_ops entry name is required", reg.Spec.ID)
		}
		if binding.OpID == "" {
			return fmt.Errorf("role %q supported_ops.%s op_id is required", reg.Spec.ID, binding.Name)
		}
		if _, ok := r.opRegs[binding.OpID]; !ok {
			return fmt.Errorf("role %q references unknown op %q", reg.Spec.ID, binding.OpID)
		}
	}
	r.agents.Register(reg.Agent)
	r.roleSpecs[reg.Spec.ID] = reg.Spec
	return nil
}

func (r *PluginRegistry) RoleSpec(roleID string) (core.RoleSpec, bool) {
	spec, ok := r.roleSpecs[strings.TrimSpace(roleID)]
	return spec, ok
}

func (r *PluginRegistry) RoleSpecs() []core.RoleSpec {
	if len(r.roleSpecs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(r.roleSpecs))
	for id := range r.roleSpecs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]core.RoleSpec, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.roleSpecs[id])
	}
	return out
}

func ResolveRoleOpID(spec core.RoleSpec, alias string) (string, bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", false
	}
	for _, binding := range spec.SupportedOps {
		if strings.TrimSpace(binding.Name) != alias {
			continue
		}
		if opID := strings.TrimSpace(binding.OpID); opID != "" {
			return opID, true
		}
		return "", false
	}
	return "", false
}

func allowedToolNames(spec core.OpSpec) []string {
	if len(spec.AllowedTools) == 0 {
		return nil
	}
	out := make([]string, 0, len(spec.AllowedTools))
	for _, tool := range spec.AllowedTools {
		name := strings.TrimSpace(tool.Name)
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return coreUniqueStrings(out)
}

func coreUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
