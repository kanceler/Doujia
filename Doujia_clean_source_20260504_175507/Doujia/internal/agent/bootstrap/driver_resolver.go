package bootstrap

import (
	"fmt"
	"strings"

	agentcore "devflow/internal/agent/core"
)

type DriverResolver interface {
	ResolveHandler(spec agentcore.HandlerSpec) (agentcore.Handler, error)
	ResolveRole(spec agentcore.RoleSpec) (agentcore.Agent, error)
	ResolveOp(reg agentcore.OpRegistration) (agentcore.OpSpec, error)
}

type DefaultDriverResolver struct {
	builtin BuiltinResolver
}

func NewDefaultDriverResolver() DefaultDriverResolver {
	return DefaultDriverResolver{builtin: NewBuiltinResolver()}
}

func (r DefaultDriverResolver) ResolveHandler(spec agentcore.HandlerSpec) (agentcore.Handler, error) {
	switch strings.TrimSpace(spec.ExecutionDriver) {
	case "", "builtin":
		return r.builtin.ResolveHandler(spec.ImplRef)
	case "subprocess":
		return NewSubprocessHandler(spec), nil
	case "rpc":
		return NewRPCHandler(spec), nil
	default:
		return nil, fmt.Errorf("handler %q has unsupported execution_driver %q", spec.ID, spec.ExecutionDriver)
	}
}

func (r DefaultDriverResolver) ResolveRole(spec agentcore.RoleSpec) (agentcore.Agent, error) {
	switch strings.TrimSpace(spec.ExecutionDriver) {
	case "", "builtin", "builtin_role":
		return r.builtin.ResolveRole(spec.DriverRef)
	case "generic_llm":
		return NewGenericRoleAgent(spec), nil
	case "subprocess":
		return NewSubprocessRoleAgent(spec), nil
	case "rpc":
		return NewRPCRoleAgent(spec), nil
	default:
		return nil, fmt.Errorf("role %q has unsupported execution_driver %q", spec.ID, spec.ExecutionDriver)
	}
}

func (r DefaultDriverResolver) ResolveOp(reg agentcore.OpRegistration) (agentcore.OpSpec, error) {
	return r.builtin.ResolveOp(reg.ImplRef)
}
