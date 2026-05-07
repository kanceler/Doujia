package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcore "devflow/internal/agent/core"
)

type RPCHandler struct {
	spec agentcore.HandlerSpec
}

func NewRPCHandler(spec agentcore.HandlerSpec) *RPCHandler {
	return &RPCHandler{spec: spec}
}

func (h *RPCHandler) Name() string {
	return h.spec.ID
}

func (h *RPCHandler) Description() string {
	return h.spec.Description
}

func (h *RPCHandler) ToolSpec() agentcore.ToolSpec {
	return agentcore.ToolSpec{Name: h.spec.ID, Description: h.spec.Description}
}

func (h *RPCHandler) Handle(ctx context.Context, req agentcore.HandlerRequest) (agentcore.HandlerResponse, error) {
	if strings.TrimSpace(h.spec.ImplRef) == "" {
		return agentcore.HandlerResponse{}, fmt.Errorf("rpc handler %q requires impl_ref", h.spec.ID)
	}
	path := strings.TrimSpace(h.spec.EndpointPath)
	if path == "" {
		path = "/handler"
	}
	timeout := time.Duration(h.spec.TimeoutSeconds) * time.Second
	client := newRPCClient(h.spec.ImplRef, path, timeout)
	var result rpcHandlerResultEnvelope
	err := client.postJSON(ctx, rpcHandlerEnvelope{
		Protocol: rpcHandlerProtocol,
		Handler:  h.spec.ID,
		Request:  req,
	}, &result)
	if err != nil {
		return agentcore.HandlerResponse{}, fmt.Errorf("rpc handler %q failed: %w", h.spec.ID, err)
	}
	if result.Protocol != rpcHandlerResultProtocol {
		return agentcore.HandlerResponse{}, fmt.Errorf("rpc handler %q returned unsupported protocol %q", h.spec.ID, result.Protocol)
	}
	if strings.TrimSpace(result.Error) != "" {
		return agentcore.HandlerResponse{}, fmt.Errorf("rpc handler %q error: %s", h.spec.ID, result.Error)
	}
	return result.Response, nil
}
