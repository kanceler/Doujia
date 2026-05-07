package registry

import (
	"context"
	"testing"

	agentcore "devflow/internal/agent/core"
)

func TestOpRegistrySupportsSharedOpAcrossRoles(t *testing.T) {
	reg := NewOpRegistry()
	reg.Register(agentcore.OpSpec{Role: "pm", Op: "write_plan"})
	reg.Register(agentcore.OpSpec{Role: "architect", Op: "write_plan"})

	if _, ok := reg.Get("pm", "write_plan"); !ok {
		t.Fatal("Get(pm, write_plan) = false, want true")
	}
	if _, ok := reg.Get("architect", "write_plan"); !ok {
		t.Fatal("Get(architect, write_plan) = false, want true")
	}
}

func TestOpRegistrySupportsLookupByOpID(t *testing.T) {
	reg := NewOpRegistry()
	reg.RegisterWithID("pm.write_plan", agentcore.OpSpec{Role: "pm", Op: "write_plan"})

	if _, ok := reg.GetByID("pm.write_plan"); !ok {
		t.Fatal("GetByID(pm.write_plan) = false, want true")
	}
}

type adapterTestRoleAgent struct {
	role string
}

func (a adapterTestRoleAgent) Role() string { return a.role }

func (a adapterTestRoleAgent) Run(_ context.Context, _ agentcore.AgentRunRequest) (agentcore.AgentResult, error) {
	return agentcore.AgentResult{}, nil
}

type testHandler struct {
	name string
}

func (h testHandler) Name() string                 { return h.name }
func (h testHandler) Description() string          { return h.name }
func (h testHandler) ToolSpec() agentcore.ToolSpec { return agentcore.ToolSpec{Name: h.name} }
func (h testHandler) Handle(_ context.Context, _ agentcore.HandlerRequest) (agentcore.HandlerResponse, error) {
	return agentcore.HandlerResponse{}, nil
}

func TestPluginRegistryRegistersHandlersOpsAndRolesInOrder(t *testing.T) {
	reg := NewPluginRegistry()

	if err := reg.RegisterHandler(agentcore.HandlerRegistration{
		Spec:    agentcore.HandlerSpec{ID: "artifact_write"},
		Handler: testHandler{name: "artifact_write"},
	}); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}

	opReg := agentcore.OpRegistration{
		ID: "pm.write_plan",
		Spec: agentcore.OpSpec{
			Role:         "pm",
			Op:           "write_plan",
			AllowedTools: []agentcore.ToolSpec{{Name: "artifact_write"}},
		},
	}
	if err := reg.RegisterOp(opReg); err != nil {
		t.Fatalf("RegisterOp() error = %v", err)
	}

	if err := reg.RegisterRole(agentcore.RoleRegistration{
		Spec: agentcore.RoleSpec{
			ID: "pm",
			SupportedOps: []agentcore.RoleOpBinding{
				{Name: "write_plan", OpID: "pm.write_plan"},
			},
		},
		Agent: adapterTestRoleAgent{role: "pm"},
	}); err != nil {
		t.Fatalf("RegisterRole() error = %v", err)
	}

	if _, ok := reg.Handlers().Get("artifact_write"); !ok {
		t.Fatal("handler not registered")
	}
	if _, ok := reg.Ops().Get("pm", "write_plan"); !ok {
		t.Fatal("op not registered")
	}
	if _, ok := reg.Agents().Get("pm"); !ok {
		t.Fatal("role agent not registered")
	}
}

func TestPluginRegistryAllowsRoleAliasToBindExistingOpOwnedByAnotherRole(t *testing.T) {
	reg := NewPluginRegistry()
	if err := reg.RegisterHandler(agentcore.HandlerRegistration{
		Spec:    agentcore.HandlerSpec{ID: "artifact_write"},
		Handler: testHandler{name: "artifact_write"},
	}); err != nil {
		t.Fatalf("RegisterHandler() error = %v", err)
	}
	if err := reg.RegisterOp(agentcore.OpRegistration{
		ID: "pm.write_plan",
		Spec: agentcore.OpSpec{
			Role:         "pm",
			Op:           "write_plan",
			AllowedTools: []agentcore.ToolSpec{{Name: "artifact_write"}},
		},
	}); err != nil {
		t.Fatalf("RegisterOp() error = %v", err)
	}

	err := reg.RegisterRole(agentcore.RoleRegistration{
		Spec: agentcore.RoleSpec{
			ID: "remote_pm",
			SupportedOps: []agentcore.RoleOpBinding{
				{Name: "write_plan", OpID: "pm.write_plan"},
			},
		},
		Agent: adapterTestRoleAgent{role: "remote_pm"},
	})
	if err != nil {
		t.Fatalf("RegisterRole(remote_pm) error = %v", err)
	}
}

func TestPluginRegistryRejectsOpWithUnknownHandler(t *testing.T) {
	reg := NewPluginRegistry()
	err := reg.RegisterOp(agentcore.OpRegistration{
		ID: "pm.write_plan",
		Spec: agentcore.OpSpec{
			Role:         "pm",
			Op:           "write_plan",
			AllowedTools: []agentcore.ToolSpec{{Name: "artifact_write"}},
		},
	})
	if err == nil {
		t.Fatal("RegisterOp() error = nil, want missing handler")
	}
}

func TestPluginRegistryRejectsRoleReferencingUnknownOp(t *testing.T) {
	reg := NewPluginRegistry()
	err := reg.RegisterRole(agentcore.RoleRegistration{
		Spec: agentcore.RoleSpec{
			ID: "pm",
			SupportedOps: []agentcore.RoleOpBinding{
				{Name: "write_plan", OpID: "pm.write_plan"},
			},
		},
		Agent: adapterTestRoleAgent{role: "pm"},
	})
	if err == nil {
		t.Fatal("RegisterRole() error = nil, want missing op")
	}
}

func TestResolveRoleOpIDUsesRoleAliasBinding(t *testing.T) {
	spec := agentcore.RoleSpec{
		ID: "pm",
		SupportedOps: []agentcore.RoleOpBinding{
			{Name: "write_plan", OpID: "pm.write_plan"},
			{Name: "review_plan", OpID: "pm.review_plan"},
		},
	}

	opID, ok := ResolveRoleOpID(spec, "write_plan")
	if !ok {
		t.Fatal("ResolveRoleOpID() ok = false, want true")
	}
	if opID != "pm.write_plan" {
		t.Fatalf("ResolveRoleOpID() = %q, want %q", opID, "pm.write_plan")
	}
}
