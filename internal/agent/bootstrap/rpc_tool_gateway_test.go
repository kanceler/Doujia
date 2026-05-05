package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	agentcore "devflow/internal/agent/core"
)

func TestRPCToolGatewayCallsRegisteredHandler(t *testing.T) {
	registry := gatewayStubRegistry{handlers: map[string]agentcore.Handler{
		"echo": gatewayStubHandler{name: "echo"},
	}}
	gateway := newRPCToolGateway(registry, []agentcore.ToolSpec{{Name: "echo"}})
	baseURL, token, closeFn, err := gateway.start(context.Background())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	defer closeFn()

	body, _ := json.Marshal(rpcToolEnvelope{
		Protocol: rpcToolProtocol,
		Tool:     "echo",
		Request:  agentcore.HandlerRequest{Args: map[string]any{"text": "hello"}},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/tools/echo", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()

	var result rpcToolResultEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || result.Response.Data["text"] != "hello" {
		t.Fatalf("status/result = %d/%+v", resp.StatusCode, result)
	}
}

func TestRPCToolGatewayRejectsUnauthorizedTool(t *testing.T) {
	registry := gatewayStubRegistry{handlers: map[string]agentcore.Handler{
		"secret": gatewayStubHandler{name: "secret"},
	}}
	gateway := newRPCToolGateway(registry, []agentcore.ToolSpec{{Name: "echo"}})
	baseURL, token, closeFn, err := gateway.start(context.Background())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	defer closeFn()

	body, _ := json.Marshal(rpcToolEnvelope{
		Protocol: rpcToolProtocol,
		Tool:     "secret",
		Request:  agentcore.HandlerRequest{},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/tools/secret", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRPCToolGatewayRejectsBadToken(t *testing.T) {
	gateway := newRPCToolGateway(gatewayStubRegistry{}, []agentcore.ToolSpec{{Name: "echo"}})
	baseURL, _, closeFn, err := gateway.start(context.Background())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	defer closeFn()

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/tools/echo", strings.NewReader(`{"protocol":"doujia.agent.tool/v1"}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

type gatewayStubRegistry struct {
	handlers map[string]agentcore.Handler
}

func (r gatewayStubRegistry) Get(name string) (agentcore.Handler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

type gatewayStubHandler struct {
	name string
}

func (h gatewayStubHandler) Name() string {
	return h.name
}

func (h gatewayStubHandler) Description() string {
	return h.name
}

func (h gatewayStubHandler) ToolSpec() agentcore.ToolSpec {
	return agentcore.ToolSpec{Name: h.name}
}

func (h gatewayStubHandler) Handle(_ context.Context, req agentcore.HandlerRequest) (agentcore.HandlerResponse, error) {
	return agentcore.HandlerResponse{Data: req.Args}, nil
}
