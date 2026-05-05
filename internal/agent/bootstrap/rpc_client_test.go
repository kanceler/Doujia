package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentcore "devflow/internal/agent/core"
)

func TestRPCWireProtocolsMatchSubprocessProtocols(t *testing.T) {
	if rpcHandlerProtocol != subprocessHandlerProtocol {
		t.Fatalf("handler protocol = %q, want %q", rpcHandlerProtocol, subprocessHandlerProtocol)
	}
	if rpcHandlerResultProtocol != subprocessHandlerResultProtocol {
		t.Fatalf("handler result protocol = %q, want %q", rpcHandlerResultProtocol, subprocessHandlerResultProtocol)
	}
	if rpcRoleProtocol != subprocessRoleProtocol {
		t.Fatalf("role protocol = %q, want %q", rpcRoleProtocol, subprocessRoleProtocol)
	}
	if rpcRoleResultProtocol != subprocessRoleResultProtocol {
		t.Fatalf("role result protocol = %q, want %q", rpcRoleResultProtocol, subprocessRoleResultProtocol)
	}
}

func TestRPCClientPostsJSONAndDecodesResponse(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var req rpcHandlerEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Protocol != rpcHandlerProtocol || req.Handler != "echo_handler" {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(rpcHandlerResultEnvelope{
			Protocol: rpcHandlerResultProtocol,
			Response: agentcore.HandlerResponse{Data: map[string]any{"text": "hello"}},
		})
	}))
	defer server.Close()

	client := newRPCClient(server.URL, "/handler", 3*time.Second)
	var out rpcHandlerResultEnvelope
	err := client.postJSON(context.Background(), rpcHandlerEnvelope{
		Protocol: rpcHandlerProtocol,
		Handler:  "echo_handler",
	}, &out)
	if err != nil {
		t.Fatalf("postJSON() error = %v", err)
	}
	if gotPath != "/handler" {
		t.Fatalf("path = %q, want /handler", gotPath)
	}
	if out.Response.Data["text"] != "hello" {
		t.Fatalf("response = %+v", out.Response)
	}
}

func TestRPCClientReturnsHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusBadGateway)
	}))
	defer server.Close()

	client := newRPCClient(server.URL, "/handler", 3*time.Second)
	err := client.postJSON(context.Background(), map[string]any{"protocol": rpcHandlerProtocol}, &rpcHandlerResultEnvelope{})
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("postJSON() error = %v, want status error containing 502 and broken", err)
	}
}
