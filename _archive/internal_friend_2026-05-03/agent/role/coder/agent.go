package coder

import (
	"context"
	"fmt"
	"strings"

	"doujia/internal/agent/core"
	rolecommon "doujia/internal/agent/role/common"
	"doujia/internal/agent/schema"
)

type Agent struct{}

type writeCodeContainerContext struct {
	RepoDir      string `json:"repo_dir"`
	BaseBranch   string `json:"base_branch"`
	WorktreesDir string `json:"worktrees_dir"`
}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "coder"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	switch req.Task.Op {
	case "write_code":
		if req.ToolLoop == nil {
			return agentFail("missing_tool_loop", "coder agent requires ToolLoop"), nil
		}
		if result, ok, err := validateWriteCodeUpstream(ctx, req); err != nil {
			return core.AgentResult{}, err
		} else if ok {
			return result, nil
		}
		if err := prepareWriteCodeWorktree(ctx, req); err != nil {
			return agentFail("worktree_prepare_failed", "coder write_code worktree prepare failed: "+err.Error()), nil
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
		return agentFail("unsupported_coder_op", "coder does not support op: "+req.Task.Op), nil
	}
}

func validateWriteCodeUpstream(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, bool, error) {
	container, err := rolecommon.ReadJSONArtifact[writeCodeContainerContext](req.Bundle, core.LKContainerContext)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKContainerContext,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKContainerContext, "container_context.json"),
			Problem:           "container_context.json is unreadable or invalid JSON: " + err.Error(),
			WhyBlocked:        "coder.write_code cannot prepare a Git worktree without a valid container context.",
			SuggestedRepair:   "Repair or rerun the upstream stage that produced container_context.json.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field repo_dir.", "coder.write_code cannot prepare a Git worktree without repo_dir.")
	}
	if strings.TrimSpace(container.BaseBranch) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field base_branch.", "coder.write_code cannot prepare a Git worktree without base_branch.")
	}
	if strings.TrimSpace(container.WorktreesDir) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field worktrees_dir.", "coder.write_code cannot prepare a Git worktree without worktrees_dir.")
	}

	moduleSpec, err := rolecommon.ReadJSONArtifact[schema.ModuleSpec](req.Bundle, core.LKModuleSpec)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleSpec,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleSpec, "module_spec.json"),
			Problem:           "module_spec.json is unreadable or invalid JSON: " + err.Error(),
			WhyBlocked:        "coder.write_code cannot prepare or scope module work without a valid module_spec.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(moduleSpec.ModuleID) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field module_id.", "coder.write_code cannot record a branch result without module_id.")
	}
	if strings.TrimSpace(moduleSpec.BranchName) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field branch_name.", "coder.write_code cannot prepare a Git branch and worktree without branch_name.")
	}
	if strings.TrimSpace(moduleSpec.WorktreeDir) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field worktree_dir.", "coder.write_code cannot prepare a Git worktree without worktree_dir.")
	}
	if len(moduleSpec.OwnedPaths) == 0 {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field owned_paths.", "coder.write_code cannot constrain file edits safely without owned_paths.")
	}
	if strings.TrimSpace(moduleSpec.TestCommand) == "" {
		return buildWriteCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field test_command.", "coder.write_code cannot run self-checks without test_command.")
	}

	return core.AgentResult{}, false, nil
}

func buildWriteCodeUpstreamIssue(ctx context.Context, req core.AgentRunRequest, logicalKey, problem, whyBlocked string) (core.AgentResult, bool, error) {
	result, err := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
		ProblemLogicalKey: logicalKey,
		ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, logicalKey, logicalKey+".json"),
		Problem:           problem,
		WhyBlocked:        whyBlocked,
		SuggestedRepair:   "Repair or rerun the upstream stage that produced this artifact.",
	})
	return result, true, err
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
	output, writeErr := writeCoderFailureArtifact(ctx, req, runErr)
	if writeErr != nil {
		return agentFail("write_code_failed", "coder write_code failed: "+runErr.Error()+"; additionally failed to write write_code_failure artifact: "+writeErr.Error()), nil
	}
	return core.AgentResult{
		Result:  "kfail",
		Message: "coder write_code failed",
		Outputs: []core.AgentOutput{output},
		Errors: []core.AgentError{
			{Code: "write_code_failed", Message: runErr.Error()},
		},
	}, nil
}

func writeCoderFailureArtifact(ctx context.Context, req core.AgentRunRequest, runErr error) (core.AgentOutput, error) {
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentOutput{}, fmt.Errorf("artifact_write handler is required")
	}
	content := strings.TrimSpace(strings.Join([]string{
		"# Coder Write Code Failure",
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
