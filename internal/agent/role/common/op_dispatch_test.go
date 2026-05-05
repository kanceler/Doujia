package common

import (
	"context"
	"testing"

	"devflow/internal/agent/core"
)

func TestDispatchByOpIDUsesResolvedOpID(t *testing.T) {
	t.Parallel()

	called := false
	result, err := DispatchByOpID(context.Background(), core.AgentRunRequest{
		Task: core.Task{
			Role: "pm",
			Op:   "write_plan",
			OpID: "pm.write_plan",
		},
	}, map[string]OpHandler{
		"pm.write_plan": func(context.Context, core.AgentRunRequest) (core.AgentResult, error) {
			called = true
			return core.AgentResult{Result: "kok"}, nil
		},
	}, "unsupported_op")
	if err != nil {
		t.Fatalf("DispatchByOpID() error = %v", err)
	}
	if !called {
		t.Fatal("DispatchByOpID() did not call resolved handler")
	}
	if result.Result != "kok" {
		t.Fatalf("DispatchByOpID() result = %q, want kok", result.Result)
	}
}

func TestDispatchByOpIDFallsBackToRoleDotAlias(t *testing.T) {
	t.Parallel()

	called := false
	_, err := DispatchByOpID(context.Background(), core.AgentRunRequest{
		Task: core.Task{
			Role: "tester",
			Op:   "test_code",
		},
	}, map[string]OpHandler{
		"tester.test_code": func(context.Context, core.AgentRunRequest) (core.AgentResult, error) {
			called = true
			return core.AgentResult{Result: "kok"}, nil
		},
	}, "unsupported_op")
	if err != nil {
		t.Fatalf("DispatchByOpID() error = %v", err)
	}
	if !called {
		t.Fatal("DispatchByOpID() did not use role.op fallback")
	}
}
