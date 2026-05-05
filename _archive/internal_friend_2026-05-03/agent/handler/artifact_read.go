package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"doujia/internal/agent/core"
)

type ArtifactReadHandler struct{}

func NewArtifactReadHandler() *ArtifactReadHandler {
	return &ArtifactReadHandler{}
}

type scopedArtifactReadHandler struct {
	bundle core.AgentInputBundle
}

func (h *ArtifactReadHandler) Name() string {
	return "artifact_read"
}

func (h *ArtifactReadHandler) Description() string {
	return "读取已登记的产物文件"
}

func (h *ArtifactReadHandler) ToolSpec() core.ToolSpec {
	return artifactReadToolSpec()
}

func (h *ArtifactReadHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleArtifactRead(ctx, req)
}

func (h *ArtifactReadHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedArtifactReadHandler{bundle: bundle}
}

func (h *scopedArtifactReadHandler) Name() string {
	return "artifact_read"
}

func (h *scopedArtifactReadHandler) Description() string {
	return "读取已登记的产物文件"
}

func (h *scopedArtifactReadHandler) ToolSpec() core.ToolSpec {
	return artifactReadToolSpec()
}

func (h *scopedArtifactReadHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleArtifactRead(ctx, req)
}

func artifactReadToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        "artifact_read",
		Description: "根据 logical_key 读取已登记的产物文件",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"logical_key"},
			"properties": map[string]any{
				"logical_key": map[string]any{
					"type":        "string",
					"description": "输入产物或上一轮输出产物的 logical_key。",
				},
			},
		},
	}
}

func handleArtifactRead(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if _, ok := req.Args["output_dir"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("output_dir is controlled by the runtime")
	}
	if _, ok := req.Args["path"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("path is controlled by logical_key")
	}
	if _, ok := req.Args["file_name"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("file_name is controlled by logical_key")
	}
	if _, ok := req.Args["allowed_paths"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("allowed_paths is controlled by the runtime")
	}
	logicalKey, err := requiredStringArg(req.Args, "logical_key")
	if err != nil {
		return core.HandlerResponse{}, err
	}

	path, ok := readPathForLogicalKey(req.Bundle, logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("artifact_logical_key_not_allowed: %s", logicalKey)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return core.HandlerResponse{}, err
	}

	return core.HandlerResponse{
		Data: map[string]any{
			"content":     string(content),
			"logical_key": logicalKey,
			"path":        absPath,
		},
	}, nil
}

func readPathForLogicalKey(bundle core.AgentInputBundle, logicalKey string) (string, bool) {
	for _, input := range bundle.Inputs {
		if input.LogicalKey == logicalKey && input.Path != "" {
			return input.Path, true
		}
	}
	for _, previous := range bundle.PreviousOutputs {
		if previous.LogicalKey == logicalKey && previous.Path != "" {
			return previous.Path, true
		}
	}
	return "", false
}

func pathInSet(path string, allowedPaths []string) bool {
	cleanPath := comparablePath(path)
	for _, allowed := range allowedPaths {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if comparablePath(absAllowed) == cleanPath {
			return true
		}
	}
	return false
}

func comparablePath(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}
