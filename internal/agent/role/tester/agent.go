package tester

import (
	"context"

	"devflow/internal/agent/core"
	rolecommon "devflow/internal/agent/role/common"
)

type Agent struct{}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "tester"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	return rolecommon.DispatchByOpID(ctx, req, map[string]rolecommon.OpHandler{
		"tester.test_data": a.runTestData,
		"tester.test_code": a.runTestCode,
	}, "unsupported_tester_op")
}

func (a *Agent) runTestData(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	if req.ToolLoop == nil {
		return agentFail("missing_tool_loop", "tester agent requires ToolLoop"), nil
	}
	if result, ok, err := a.validateTestDataUpstream(ctx, req); err != nil {
		return core.AgentResult{}, err
	} else if ok {
		return result, nil
	}
	return req.ToolLoop.Run(ctx, core.ToolLoopRequest{
		Task:     req.Task,
		Bundle:   req.Bundle,
		OpSpec:   req.OpSpec,
		Prompt:   req.Prompt,
		Handlers: req.Handlers,
		LLM:      req.LLM,
	})
}

func agentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
