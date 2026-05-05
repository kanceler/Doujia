package common

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doujia/internal/agent/core"
	"doujia/internal/agent/llm"
)

type UpstreamArtifactIssueParams struct {
	ProblemLogicalKey string
	ProblemFile       string
	Problem           string
	WhyBlocked        string
	SuggestedRepair   string
}

func BuildUpstreamArtifactIssueResult(ctx context.Context, req core.AgentRunRequest, params UpstreamArtifactIssueParams) (core.AgentResult, error) {
	content := buildUpstreamArtifactIssueContent(ctx, req, params)

	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("artifact_write handler is required")
	}
	resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": core.LKUpstreamArtifactIssue,
			"content":     content,
		},
	})
	if err != nil {
		return core.AgentResult{}, err
	}

	return core.AgentResult{
		Result:  "kbug",
		Message: "Upstream artifact is invalid; current op cannot continue safely.",
		Outputs: []core.AgentOutput{
			{
				LogicalKey:  core.LKUpstreamArtifactIssue,
				ObjectType:  stringValue(resp.Data["object_type"]),
				Status:      stringValue(resp.Data["status"]),
				Path:        stringValue(resp.Data["path"]),
				ArtifactURI: stringValue(resp.Data["artifact_uri"]),
			},
		},
		Errors: []core.AgentError{
			{
				Code:    "invalid_upstream_artifact",
				Message: strings.TrimSpace(params.Problem),
			},
		},
	}, nil
}

func ReadArtifactContent(bundle core.AgentInputBundle, logicalKey string) (string, error) {
	path, ok := ArtifactPath(bundle, logicalKey)
	if !ok {
		return "", fmt.Errorf("missing artifact %q", logicalKey)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func ReadJSONArtifact[T any](bundle core.AgentInputBundle, logicalKey string) (T, error) {
	var value T
	content, err := ReadArtifactContent(bundle, logicalKey)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return value, err
	}
	return value, nil
}

func ArtifactPath(bundle core.AgentInputBundle, logicalKey string) (string, bool) {
	for _, input := range bundle.Inputs {
		if input.LogicalKey == logicalKey && strings.TrimSpace(input.Path) != "" {
			return input.Path, true
		}
	}
	for _, previous := range bundle.PreviousOutputs {
		if previous.LogicalKey == logicalKey && strings.TrimSpace(previous.Path) != "" {
			return previous.Path, true
		}
	}
	return "", false
}

func ArtifactFileName(bundle core.AgentInputBundle, logicalKey string, fallback string) string {
	path, ok := ArtifactPath(bundle, logicalKey)
	if ok {
		return filepath.Base(path)
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return logicalKey
}

func buildUpstreamArtifactIssueContent(ctx context.Context, req core.AgentRunRequest, params UpstreamArtifactIssueParams) string {
	if adapter, ok := req.LLM.(llm.Adapter); ok && adapter != nil {
		prompt := strings.TrimSpace(strings.Join([]string{
			"Write a concise Markdown report for an upstream artifact validation failure.",
			"Return only Markdown.",
			"",
			"Current op:",
			"- role: " + req.Task.Role,
			"- op: " + req.Task.Op,
			"- agent_id: " + req.Task.AgentID,
			"",
			"Problem artifact:",
			"- logical_key: " + params.ProblemLogicalKey,
			"- file: " + params.ProblemFile,
			"",
			"Problem:",
			params.Problem,
			"",
			"Why this blocks execution:",
			params.WhyBlocked,
			"",
			"Suggested repair:",
			params.SuggestedRepair,
		}, "\n"))
		resp, err := adapter.Chat(ctx, llm.ChatRequest{
			Messages: []llm.Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
		})
		if err == nil && strings.TrimSpace(resp.Message.Content) != "" {
			return ensureTrailingNewline(resp.Message.Content)
		}
	}

	return buildUpstreamArtifactIssueTemplate(req, params)
}

func buildUpstreamArtifactIssueTemplate(req core.AgentRunRequest, params UpstreamArtifactIssueParams) string {
	lines := []string{
		"# Upstream Artifact Issue",
		"",
		"## Result",
		"",
		"kbug",
		"",
		"## Current Op",
		"",
		"- role: " + req.Task.Role,
		"- op: " + req.Task.Op,
		"- agent_id: " + req.Task.AgentID,
		"",
		"## Problem Artifact",
		"",
		"- logical_key: " + params.ProblemLogicalKey,
		"- file: " + params.ProblemFile,
		"",
		"## Problem",
		"",
		strings.TrimSpace(params.Problem),
		"",
		"## Why This Blocks Execution",
		"",
		strings.TrimSpace(params.WhyBlocked),
		"",
		"## Suggested Repair",
		"",
		strings.TrimSpace(params.SuggestedRepair),
	}
	return strings.Join(lines, "\n") + "\n"
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
