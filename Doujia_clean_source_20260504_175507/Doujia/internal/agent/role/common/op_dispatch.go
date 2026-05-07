package common

import (
	"context"
	"fmt"
	"strings"

	"devflow/internal/agent/core"
)

type OpHandler func(context.Context, core.AgentRunRequest) (core.AgentResult, error)

func DispatchByOpID(ctx context.Context, req core.AgentRunRequest, handlers map[string]OpHandler, unsupportedCode string) (core.AgentResult, error) {
	opID := strings.TrimSpace(req.Task.OpID)
	if opID == "" {
		opID = strings.TrimSpace(req.Task.Role + "." + req.Task.Op)
	}
	handler, ok := handlers[opID]
	if !ok {
		return AgentFail(unsupportedCode, fmt.Sprintf("%s does not support op_id: %s", req.Task.Role, opID)), nil
	}
	return handler(ctx, req)
}

func AgentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
