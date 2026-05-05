package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentcore "devflow/internal/agent/core"
)

const (
	subprocessRoleProtocol       = "doujia.agent.role/v1"
	subprocessRoleResultProtocol = "doujia.agent.role_result/v1"
)

type SubprocessRoleAgent struct {
	spec agentcore.RoleSpec
}

func NewSubprocessRoleAgent(spec agentcore.RoleSpec) *SubprocessRoleAgent {
	return &SubprocessRoleAgent{spec: spec}
}

func (a *SubprocessRoleAgent) Role() string {
	return a.spec.ID
}

func (a *SubprocessRoleAgent) Run(ctx context.Context, req agentcore.AgentRunRequest) (agentcore.AgentResult, error) {
	if strings.TrimSpace(a.spec.DriverRef) == "" {
		return agentcore.AgentResult{}, fmt.Errorf("subprocess role %q requires driver_ref", a.spec.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	body, err := json.Marshal(subprocessRoleEnvelope{
		Protocol: subprocessRoleProtocol,
		Task:     req.Task,
		Bundle:   req.Bundle,
		OpSpec:   req.OpSpec,
	})
	if err != nil {
		return agentcore.AgentResult{}, err
	}

	stdout, stderr, err := runJSONSubprocess(ctx, a.spec.DriverRef, body)
	if err != nil {
		return agentcore.AgentResult{}, fmt.Errorf("subprocess role %q failed: %w: %s", a.spec.ID, err, stderr)
	}

	var result subprocessRoleResultEnvelope
	if err := json.Unmarshal(stdout, &result); err != nil {
		return agentcore.AgentResult{}, fmt.Errorf("decode subprocess role %q response: %w", a.spec.ID, err)
	}
	if result.Protocol != subprocessRoleResultProtocol {
		return agentcore.AgentResult{}, fmt.Errorf("subprocess role %q returned unsupported protocol %q", a.spec.ID, result.Protocol)
	}
	if strings.TrimSpace(result.Error) != "" {
		return agentcore.AgentResult{}, fmt.Errorf("subprocess role %q error: %s", a.spec.ID, result.Error)
	}
	return result.Result, nil
}

type subprocessRoleEnvelope struct {
	Protocol string                     `json:"protocol"`
	Task     agentcore.Task             `json:"task"`
	Bundle   agentcore.AgentInputBundle `json:"bundle"`
	OpSpec   agentcore.OpSpec           `json:"op_spec"`
}

type subprocessRoleResultEnvelope struct {
	Protocol string                `json:"protocol"`
	Result   agentcore.AgentResult `json:"result"`
	Error    string                `json:"error,omitempty"`
}
