package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcore "devflow/internal/agent/core"
	appcore "devflow/internal/core"
)

func TestRPCRoleRoundTripWithProducedBags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRoleEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Protocol != rpcRoleProtocol || req.Role != "remote_pm" {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(rpcRoleResultEnvelope{
			Protocol: rpcRoleResultProtocol,
			Result: agentcore.AgentResult{
				Result: "kok",
				Outputs: []agentcore.AgentOutput{{
					LogicalKey: "product_plan",
					ObjectType: "markdown",
					Status:     "created",
					Path:       "product_plan.md",
				}},
				ProducedBags: []appcore.ProducedBagManifest{{
					Name:    "product_plan",
					Members: []appcore.ProducedBagMember{{LogicalKey: "product_plan"}},
				}},
			},
		})
	}))
	defer server.Close()

	agent := NewRPCRoleAgent(agentcore.RoleSpec{
		ID:              "remote_pm",
		ExecutionDriver: "rpc",
		DriverRef:       server.URL,
	})
	result, err := agent.Run(context.Background(), agentcore.AgentRunRequest{
		Task:   agentcore.Task{Role: "remote_pm", Op: "write_plan"},
		Bundle: agentcore.AgentInputBundle{OutputDir: t.TempDir()},
		OpSpec: agentcore.OpSpec{Role: "remote_pm", Op: "write_plan"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" || len(result.ProducedBags) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRPCRoleInjectsToolGatewayAndAllowsCallback(t *testing.T) {
	registry := roleGatewayRegistry{handlers: map[string]agentcore.Handler{
		"echo": roleGatewayHandler{name: "echo"},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRoleEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode role request: %v", err)
		}
		if req.ToolGateway == nil {
			t.Fatal("tool_gateway is nil")
		}
		body, _ := json.Marshal(rpcToolEnvelope{
			Protocol: rpcToolProtocol,
			Tool:     "echo",
			Request:  agentcore.HandlerRequest{Args: map[string]any{"text": "from remote"}},
		})
		toolReq, _ := http.NewRequest(http.MethodPost, req.ToolGateway.BaseURL+"/tools/echo", bytes.NewReader(body))
		toolReq.Header.Set("Authorization", "Bearer "+req.ToolGateway.Token)
		resp, err := http.DefaultClient.Do(toolReq)
		if err != nil {
			t.Fatalf("tool callback error = %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tool callback status = %d", resp.StatusCode)
		}
		_ = json.NewEncoder(w).Encode(rpcRoleResultEnvelope{
			Protocol: rpcRoleResultProtocol,
			Result:   agentcore.AgentResult{Result: "kok"},
		})
	}))
	defer server.Close()

	agent := NewRPCRoleAgent(agentcore.RoleSpec{ID: "remote_pm", DriverRef: server.URL})
	result, err := agent.Run(context.Background(), agentcore.AgentRunRequest{
		Task: agentcore.Task{Role: "remote_pm", Op: "write_plan"},
		OpSpec: agentcore.OpSpec{
			AllowedTools: []agentcore.ToolSpec{{Name: "echo", Description: "Echo"}},
		},
		Handlers: registry,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRPCRoleRejectsBadResultProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rpcRoleResultEnvelope{Protocol: "wrong"})
	}))
	defer server.Close()

	agent := NewRPCRoleAgent(agentcore.RoleSpec{ID: "remote_pm", DriverRef: server.URL})
	_, err := agent.Run(context.Background(), agentcore.AgentRunRequest{})
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("Run() error = %v, want unsupported protocol", err)
	}
}

type roleGatewayRegistry struct {
	handlers map[string]agentcore.Handler
}

func (r roleGatewayRegistry) Get(name string) (agentcore.Handler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

type roleGatewayHandler struct {
	name string
}

func (h roleGatewayHandler) Name() string {
	return h.name
}

func (h roleGatewayHandler) Description() string {
	return h.name
}

func (h roleGatewayHandler) ToolSpec() agentcore.ToolSpec {
	return agentcore.ToolSpec{Name: h.name}
}

func (h roleGatewayHandler) Handle(_ context.Context, req agentcore.HandlerRequest) (agentcore.HandlerResponse, error) {
	return agentcore.HandlerResponse{Data: req.Args}, nil
}
