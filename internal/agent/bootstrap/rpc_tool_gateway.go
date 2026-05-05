package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	agentcore "devflow/internal/agent/core"
)

type rpcToolGateway struct {
	registry agentcore.HandlerRegistryLike
	allowed  map[string]struct{}
}

func newRPCToolGateway(registry agentcore.HandlerRegistryLike, tools []agentcore.ToolSpec) rpcToolGateway {
	allowed := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	return rpcToolGateway{registry: registry, allowed: allowed}
}

func (g rpcToolGateway) start(ctx context.Context) (baseURL string, token string, closeFn func(), err error) {
	token, err = randomToken()
	if err != nil {
		return "", "", nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	mux.HandleFunc("/tools/", g.handleTool(token))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	closeFn = func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-done
	}
	return "http://" + listener.Addr().String(), token, closeFn, nil
}

func (g rpcToolGateway) handleTool(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/tools/")
		if _, ok := g.allowed[name]; !ok {
			http.Error(w, "tool is not allowed for this run", http.StatusForbidden)
			return
		}
		handler, ok := g.registry.Get(name)
		if !ok {
			http.Error(w, "tool is not registered", http.StatusNotFound)
			return
		}
		var envelope rpcToolEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if envelope.Protocol != rpcToolProtocol || envelope.Tool != name {
			http.Error(w, "invalid tool protocol", http.StatusBadRequest)
			return
		}
		response, err := handler.Handle(r.Context(), envelope.Request)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(rpcToolResultEnvelope{
				Protocol: rpcToolResultProtocol,
				Error:    err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(rpcToolResultEnvelope{
			Protocol: rpcToolResultProtocol,
			Response: response,
		})
	}
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate rpc tool token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
