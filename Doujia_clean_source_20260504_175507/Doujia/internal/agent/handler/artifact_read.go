package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"devflow/internal/agent/core"
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

	ref, ok := readRefForLogicalKey(req.Bundle, logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("artifact_logical_key_not_allowed: %s", logicalKey)
	}
	absPath, err := filepath.Abs(ref.Path)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	contentType := artifactContentType(ref.ObjectType, ref.ContentType, absPath)
	encoding := strings.TrimSpace(ref.Encoding)
	if encoding == "" {
		encoding = defaultReadEncoding(ref.ObjectType, contentType)
	}
	data := map[string]any{
		"logical_key":  logicalKey,
		"path":         absPath,
		"object_type":  ref.ObjectType,
		"content_type": contentType,
		"encoding":     encoding,
		"size_bytes":   len(content),
	}
	if encoding == "base64" || isBinaryObjectType(ref.ObjectType) {
		data["content_base64"] = base64.StdEncoding.EncodeToString(content)
	} else {
		data["content"] = string(content)
	}

	return core.HandlerResponse{
		Data: data,
	}, nil
}

type artifactReadRef struct {
	Path        string
	ObjectType  string
	ContentType string
	Encoding    string
}

func readRefForLogicalKey(bundle core.AgentInputBundle, logicalKey string) (artifactReadRef, bool) {
	for i := len(bundle.Inputs) - 1; i >= 0; i-- {
		input := bundle.Inputs[i]
		if input.LogicalKey == logicalKey && input.Path != "" {
			return artifactReadRef{
				Path:        input.Path,
				ObjectType:  input.ObjectType,
				ContentType: input.ContentType,
				Encoding:    input.Encoding,
			}, true
		}
	}
	for i := len(bundle.PreviousOutputs) - 1; i >= 0; i-- {
		previous := bundle.PreviousOutputs[i]
		if previous.LogicalKey == logicalKey && previous.Path != "" {
			return artifactReadRef{
				Path:        previous.Path,
				ObjectType:  previous.ObjectType,
				ContentType: previous.ContentType,
				Encoding:    previous.Encoding,
			}, true
		}
	}
	return artifactReadRef{}, false
}

func readPathForLogicalKey(bundle core.AgentInputBundle, logicalKey string) (string, bool) {
	ref, ok := readRefForLogicalKey(bundle, logicalKey)
	if !ok {
		return "", false
	}
	return ref.Path, true
}

func defaultReadEncoding(objectType, contentType string) string {
	if isBinaryObjectType(objectType) {
		return "base64"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/") ||
		strings.Contains(strings.ToLower(contentType), "charset=") ||
		strings.Contains(strings.ToLower(contentType), "json") {
		return "utf-8"
	}
	return "base64"
}

func isBinaryObjectType(objectType string) bool {
	switch strings.TrimSpace(strings.ToLower(objectType)) {
	case "binary", "image", "archive":
		return true
	default:
		return false
	}
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
