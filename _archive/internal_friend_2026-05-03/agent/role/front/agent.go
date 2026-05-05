package front

import (
	"context"
	"fmt"
	"strings"

	"doujia/internal/agent/core"
)

type Agent struct{}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "front"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	switch req.Task.Op {
	case "write_code":
		if req.ToolLoop == nil {
			return agentFail("missing_tool_loop", "front agent requires ToolLoop"), nil
		}
		if err := prepareWriteCodeWorktree(ctx, req); err != nil {
			return agentFail("worktree_prepare_failed", "front write_code worktree prepare failed: "+err.Error()), nil
		}
		result, err := req.ToolLoop.Run(ctx, core.ToolLoopRequest{
			Task:     req.Task,
			Bundle:   req.Bundle,
			OpSpec:   req.OpSpec,
			Prompt:   req.Prompt,
			Handlers: req.Handlers,
			LLM:      req.LLM,
		})
		if err != nil {
			return writeCodeFailureResult(ctx, req, err)
		}
		return result, nil
	default:
		return agentFail("unsupported_front_op", "front does not support op: "+req.Task.Op), nil
	}
}

func prepareWriteCodeWorktree(ctx context.Context, req core.AgentRunRequest) error {
	handler, ok := req.Handlers.Get("container_git_worktree_prepare")
	if !ok {
		return fmt.Errorf("container_git_worktree_prepare handler is required")
	}
	_, err := handler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args:   map[string]any{},
	})
	return err
}

func writeCodeFailureResult(ctx context.Context, req core.AgentRunRequest, runErr error) (core.AgentResult, error) {
	output, writeErr := writeFrontFailureArtifact(ctx, req, runErr)
	if writeErr != nil {
		return agentFail("write_code_failed", "front write_code failed: "+runErr.Error()+"; additionally failed to write write_code_failure artifact: "+writeErr.Error()), nil
	}
	return core.AgentResult{
		Result:  "kfail",
		Message: "front write_code failed",
		Outputs: []core.AgentOutput{output},
		Errors: []core.AgentError{
			{Code: "write_code_failed", Message: runErr.Error()},
		},
	}, nil
}

func writeFrontFailureArtifact(ctx context.Context, req core.AgentRunRequest, runErr error) (core.AgentOutput, error) {
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentOutput{}, fmt.Errorf("artifact_write handler is required")
	}
	content := strings.TrimSpace(strings.Join([]string{
		"# Front Write Code Failure",
		"",
		"## Failure Stage",
		"",
		"tool_loop",
		"",
		"## Reason",
		"",
		runErr.Error(),
	}, "\n")) + "\n"
	resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": core.LKWriteCodeFailure,
			"content":     content,
		},
	})
	if err != nil {
		return core.AgentOutput{}, err
	}
	return core.AgentOutput{
		LogicalKey:  core.LKWriteCodeFailure,
		ObjectType:  stringValue(resp.Data["object_type"]),
		Status:      stringValue(resp.Data["status"]),
		Path:        stringValue(resp.Data["path"]),
		ArtifactURI: stringValue(resp.Data["artifact_uri"]),
	}, nil
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func agentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
