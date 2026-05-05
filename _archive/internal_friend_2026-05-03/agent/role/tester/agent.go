package tester

import (
	"context"

	"doujia/internal/agent/core"
)

type Agent struct{}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "tester"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	switch req.Task.Op {
	case "test_data":
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
	case "test_code":
		return a.runTestCode(ctx, req)
	default:
		return agentFail("unsupported_tester_op", "tester does not support op: "+req.Task.Op), nil
	}
}

func agentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
