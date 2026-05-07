package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcore "devflow/internal/agent/core"
)

type RPCRoleAgent struct {
	spec agentcore.RoleSpec
}

func NewRPCRoleAgent(spec agentcore.RoleSpec) *RPCRoleAgent {
	return &RPCRoleAgent{spec: spec}
}

func (a *RPCRoleAgent) Role() string {
	return a.spec.ID
}

func (a *RPCRoleAgent) Run(ctx context.Context, req agentcore.AgentRunRequest) (agentcore.AgentResult, error) {
	if strings.TrimSpace(a.spec.DriverRef) == "" {
		return agentcore.AgentResult{}, fmt.Errorf("rpc role %q requires driver_ref", a.spec.ID)
	}
	path := strings.TrimSpace(a.spec.EndpointPath)
	if path == "" {
		path = "/role"
	}

	var gatewayInfo *rpcToolGatewayInfo
	var closeGateway func()
	if req.Handlers != nil && len(req.OpSpec.AllowedTools) > 0 {
		gateway := newRPCToolGateway(req.Handlers, req.OpSpec.AllowedTools)
		baseURL, token, closeFn, err := gateway.start(ctx)
		if err != nil {
			return agentcore.AgentResult{}, err
		}
		closeGateway = closeFn
		gatewayInfo = &rpcToolGatewayInfo{
			BaseURL:      baseURL,
			Token:        token,
			AllowedTools: req.OpSpec.AllowedTools,
		}
	}
	if closeGateway != nil {
		defer closeGateway()
	}

	timeout := time.Duration(a.spec.TimeoutSeconds) * time.Second
	client := newRPCClient(a.spec.DriverRef, path, timeout)
	var result rpcRoleResultEnvelope
	err := client.postJSON(ctx, rpcRoleEnvelope{
		Protocol:    rpcRoleProtocol,
		Role:        a.spec.ID,
		Task:        req.Task,
		Bundle:      req.Bundle,
		OpSpec:      req.OpSpec,
		ToolGateway: gatewayInfo,
	}, &result)
	if err != nil {
		return agentcore.AgentResult{}, fmt.Errorf("rpc role %q failed: %w", a.spec.ID, err)
	}
	if result.Protocol != rpcRoleResultProtocol {
		return agentcore.AgentResult{}, fmt.Errorf("rpc role %q returned unsupported protocol %q", a.spec.ID, result.Protocol)
	}
	if strings.TrimSpace(result.Error) != "" {
		return agentcore.AgentResult{}, fmt.Errorf("rpc role %q error: %s", a.spec.ID, result.Error)
	}
	return result.Result, nil
}
