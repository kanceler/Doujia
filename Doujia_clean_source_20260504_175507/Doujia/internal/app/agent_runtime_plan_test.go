package app

import (
	"context"
	"strings"
	"testing"

	agentcore "devflow/internal/agent/core"
	"devflow/internal/core"
	"devflow/internal/runtime"
)

func TestBuildAgentRuntimePlanUsesRoleSpecsForSessionAndTaskRoles(t *testing.T) {
	resolver := &recordingFactoryResolver{called: map[string]bool{}}
	plan, err := buildAgentRuntimePlan([]agentcore.RoleSpec{
		{ID: "ceo", InteractionMode: "session"},
		{ID: "pm", InteractionMode: "task"},
		{ID: "remote_architect", InteractionMode: "task", ExecutionDriver: "rpc", DriverRef: "http://127.0.0.1:18080"},
	}, resolver.FactoryForRole)
	if err != nil {
		t.Fatalf("buildAgentRuntimePlan() error = %v", err)
	}
	if plan.sessionTemplate.Role != core.AgentRole("ceo") {
		t.Fatalf("session role = %q, want ceo", plan.sessionTemplate.Role)
	}
	if plan.sessionTemplate.AgentID != core.AgentID("ceo") {
		t.Fatalf("session agent_id = %q, want ceo", plan.sessionTemplate.AgentID)
	}
	if len(plan.sessionRoles) != 1 || plan.sessionRoles[0] != core.AgentRole("ceo") {
		t.Fatalf("sessionRoles = %+v, want [ceo]", plan.sessionRoles)
	}
	if _, ok := plan.taskFactories[core.AgentRole("pm")]; !ok {
		t.Fatal("pm task factory missing")
	}
	if _, ok := plan.taskFactories[core.AgentRole("remote_architect")]; !ok {
		t.Fatal("remote_architect task factory missing")
	}
	if !resolver.called["ceo"] || !resolver.called["pm"] || !resolver.called["remote_architect"] {
		t.Fatalf("resolver called = %+v", resolver.called)
	}
}

func TestBuildAgentRuntimePlanRejectsMissingSessionRole(t *testing.T) {
	_, err := buildAgentRuntimePlan([]agentcore.RoleSpec{
		{ID: "pm", InteractionMode: "task"},
	}, (&recordingFactoryResolver{called: map[string]bool{}}).FactoryForRole)
	if err == nil || !strings.Contains(err.Error(), "requires exactly one session role") {
		t.Fatalf("buildAgentRuntimePlan() error = %v, want missing session role error", err)
	}
}

func TestBuildAgentRuntimePlanRejectsMultipleSessionRoles(t *testing.T) {
	_, err := buildAgentRuntimePlan([]agentcore.RoleSpec{
		{ID: "ceo", InteractionMode: "session"},
		{ID: "operator", InteractionMode: "session"},
	}, (&recordingFactoryResolver{called: map[string]bool{}}).FactoryForRole)
	if err == nil || !strings.Contains(err.Error(), "multiple session roles") {
		t.Fatalf("buildAgentRuntimePlan() error = %v, want multiple session roles error", err)
	}
}

type recordingFactoryResolver struct {
	called map[string]bool
}

func (r *recordingFactoryResolver) FactoryForRole(role core.AgentRole) runtime.Agent {
	r.called[string(role)] = true
	return runtimeAgentStub{}
}

type runtimeAgentStub struct{}

func (runtimeAgentStub) Create(runtime.AgentInit, runtime.AgentDeps) runtime.Agent {
	return runtimeAgentStub{}
}

func (runtimeAgentStub) Execute(_ context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	return core.TaskMetaData{
		Direction: core.TaskDirectionFeedback,
		RunID:     task.RunID,
		TaskID:    task.TaskID,
		Result:    core.TaskResultCodeOK,
	}, nil
}
