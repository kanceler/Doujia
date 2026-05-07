package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	agentcore "devflow/internal/agent/core"
)

const (
	subprocessHandlerProtocol       = "doujia.agent.handler/v1"
	subprocessHandlerResultProtocol = "doujia.agent.handler_result/v1"
)

type SubprocessHandler struct {
	spec agentcore.HandlerSpec
}

func NewSubprocessHandler(spec agentcore.HandlerSpec) *SubprocessHandler {
	return &SubprocessHandler{spec: spec}
}

func (h *SubprocessHandler) Name() string {
	return h.spec.ID
}

func (h *SubprocessHandler) Description() string {
	return h.spec.Description
}

func (h *SubprocessHandler) ToolSpec() agentcore.ToolSpec {
	return agentcore.ToolSpec{Name: h.spec.ID, Description: h.spec.Description}
}

func (h *SubprocessHandler) Handle(ctx context.Context, req agentcore.HandlerRequest) (agentcore.HandlerResponse, error) {
	if strings.TrimSpace(h.spec.ImplRef) == "" {
		return agentcore.HandlerResponse{}, fmt.Errorf("subprocess handler %q requires impl_ref", h.spec.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body, err := json.Marshal(subprocessHandlerEnvelope{
		Protocol: subprocessHandlerProtocol,
		Handler:  h.spec.ID,
		Request:  req,
	})
	if err != nil {
		return agentcore.HandlerResponse{}, err
	}

	stdout, stderr, err := runJSONSubprocess(ctx, h.spec.ImplRef, body)
	if err != nil {
		return agentcore.HandlerResponse{}, fmt.Errorf("subprocess handler %q failed: %w: %s", h.spec.ID, err, stderr)
	}

	var result subprocessHandlerResultEnvelope
	if err := json.Unmarshal(stdout, &result); err != nil {
		return agentcore.HandlerResponse{}, fmt.Errorf("decode subprocess handler %q response: %w", h.spec.ID, err)
	}
	if result.Protocol != subprocessHandlerResultProtocol {
		return agentcore.HandlerResponse{}, fmt.Errorf("subprocess handler %q returned unsupported protocol %q", h.spec.ID, result.Protocol)
	}
	if strings.TrimSpace(result.Error) != "" {
		return agentcore.HandlerResponse{}, fmt.Errorf("subprocess handler %q error: %s", h.spec.ID, result.Error)
	}
	return result.Response, nil
}

type subprocessHandlerEnvelope struct {
	Protocol string                   `json:"protocol"`
	Handler  string                   `json:"handler"`
	Request  agentcore.HandlerRequest `json:"request"`
}

type subprocessHandlerResultEnvelope struct {
	Protocol string                    `json:"protocol"`
	Response agentcore.HandlerResponse `json:"response"`
	Error    string                    `json:"error,omitempty"`
}

func runJSONSubprocess(ctx context.Context, path string, stdin []byte) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), strings.TrimSpace(stderr.String()), err
}
