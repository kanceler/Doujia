package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcore "devflow/internal/agent/core"
)

func TestRPCHandlerRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcHandlerEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Protocol != rpcHandlerProtocol || req.Handler != "echo_handler" {
			t.Fatalf("request = %+v", req)
		}
		if req.Request.Args["text"] != "hello" {
			t.Fatalf("args = %+v", req.Request.Args)
		}
		_ = json.NewEncoder(w).Encode(rpcHandlerResultEnvelope{
			Protocol: rpcHandlerResultProtocol,
			Response: agentcore.HandlerResponse{Data: map[string]any{"text": "hello"}},
		})
	}))
	defer server.Close()

	h := NewRPCHandler(agentcore.HandlerSpec{
		ID:              "echo_handler",
		ExecutionDriver: "rpc",
		ImplRef:         server.URL,
	})
	resp, err := h.Handle(context.Background(), agentcore.HandlerRequest{
		Args: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resp.Data["text"] != "hello" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestRPCHandlerRejectsBadResultProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rpcHandlerResultEnvelope{Protocol: "wrong"})
	}))
	defer server.Close()

	h := NewRPCHandler(agentcore.HandlerSpec{ID: "echo_handler", ImplRef: server.URL})
	_, err := h.Handle(context.Background(), agentcore.HandlerRequest{})
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("Handle() error = %v, want unsupported protocol", err)
	}
}

func TestRPCHandlerRequiresImplRef(t *testing.T) {
	h := NewRPCHandler(agentcore.HandlerSpec{ID: "echo_handler"})
	_, err := h.Handle(context.Background(), agentcore.HandlerRequest{})
	if err == nil || !strings.Contains(err.Error(), "requires impl_ref") {
		t.Fatalf("Handle() error = %v, want impl_ref error", err)
	}
}
