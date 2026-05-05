package bootstrap

import agentcore "devflow/internal/agent/core"

const (
	rpcHandlerProtocol       = subprocessHandlerProtocol
	rpcHandlerResultProtocol = subprocessHandlerResultProtocol
	rpcRoleProtocol          = subprocessRoleProtocol
	rpcRoleResultProtocol    = subprocessRoleResultProtocol
	rpcToolProtocol          = "doujia.agent.tool/v1"
	rpcToolResultProtocol    = "doujia.agent.tool_result/v1"
)

type rpcHandlerEnvelope struct {
	Protocol string                   `json:"protocol"`
	Handler  string                   `json:"handler"`
	Request  agentcore.HandlerRequest `json:"request"`
}

type rpcHandlerResultEnvelope struct {
	Protocol string                    `json:"protocol"`
	Response agentcore.HandlerResponse `json:"response"`
	Error    string                    `json:"error,omitempty"`
}

type rpcRoleEnvelope struct {
	Protocol    string                     `json:"protocol"`
	Role        string                     `json:"role"`
	Task        agentcore.Task             `json:"task"`
	Bundle      agentcore.AgentInputBundle `json:"bundle"`
	OpSpec      agentcore.OpSpec           `json:"op_spec"`
	ToolGateway *rpcToolGatewayInfo        `json:"tool_gateway,omitempty"`
}

type rpcRoleResultEnvelope struct {
	Protocol string                `json:"protocol"`
	Result   agentcore.AgentResult `json:"result"`
	Error    string                `json:"error,omitempty"`
}

type rpcToolGatewayInfo struct {
	BaseURL      string               `json:"base_url"`
	Token        string               `json:"token"`
	AllowedTools []agentcore.ToolSpec `json:"allowed_tools"`
}

type rpcToolEnvelope struct {
	Protocol string                   `json:"protocol"`
	Tool     string                   `json:"tool"`
	Request  agentcore.HandlerRequest `json:"request"`
}

type rpcToolResultEnvelope struct {
	Protocol string                    `json:"protocol"`
	Response agentcore.HandlerResponse `json:"response"`
	Error    string                    `json:"error,omitempty"`
}
