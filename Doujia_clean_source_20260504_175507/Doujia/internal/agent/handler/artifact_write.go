package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"devflow/internal/agent/core"
)

type ArtifactWriteHandler struct{}

func NewArtifactWriteHandler() *ArtifactWriteHandler {
	return &ArtifactWriteHandler{}
}

type scopedArtifactWriteHandler struct {
	bundle core.AgentInputBundle
}

func (h *ArtifactWriteHandler) Name() string {
	return "artifact_write"
}

func (h *ArtifactWriteHandler) Description() string {
	return "在 output_dir 下写入产物文件"
}

func (h *ArtifactWriteHandler) ToolSpec() core.ToolSpec {
	return artifactWriteToolSpec()
}

func (h *ArtifactWriteHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return handleArtifactWrite(ctx, req)
}

func (h *ArtifactWriteHandler) WithScope(bundle core.AgentInputBundle) core.Handler {
	return &scopedArtifactWriteHandler{bundle: bundle}
}

func (h *scopedArtifactWriteHandler) Name() string {
	return "artifact_write"
}

func (h *scopedArtifactWriteHandler) Description() string {
	return "在 output_dir 下写入产物文件"
}

func (h *scopedArtifactWriteHandler) ToolSpec() core.ToolSpec {
	return artifactWriteToolSpec()
}

func (h *scopedArtifactWriteHandler) Handle(ctx context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if req.Bundle.InputDir == "" && req.Bundle.OutputDir == "" && len(req.Bundle.Inputs) == 0 && len(req.Bundle.PreviousOutputs) == 0 {
		req.Bundle = h.bundle
	}
	return handleArtifactWrite(ctx, req)
}

func artifactWriteToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name:        "artifact_write",
		Description: "Write a declared output artifact by logical_key.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"logical_key"},
			"properties": map[string]any{
				"logical_key": map[string]any{
					"type":        "string",
					"description": "Declared output logical_key.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Complete text content for a text artifact.",
				},
				"content_base64": map[string]any{
					"type":        "string",
					"description": "Base64-encoded bytes for a binary artifact.",
				},
			},
		},
	}
}

func handleArtifactWrite(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	if _, ok := req.Args["output_dir"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("output_dir is controlled by the runtime")
	}
	if _, ok := req.Args["path"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("path is controlled by logical_key")
	}
	if _, ok := req.Args["file_name"]; ok {
		return core.HandlerResponse{}, fmt.Errorf("file_name is controlled by logical_key")
	}
	logicalKey, err := requiredStringArg(req.Args, "logical_key")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	content, hasContent, err := optionalStringArg(req.Args, "content")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	contentBase64, hasContentBase64, err := optionalStringArg(req.Args, "content_base64")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if hasContent == hasContentBase64 {
		return core.HandlerResponse{}, fmt.Errorf("artifact_write requires exactly one of content or content_base64")
	}
	outputSpec, ok := req.OpSpec.FindOutput(logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("artifact_logical_key_not_allowed: %s", logicalKey)
	}
	contentBytes := []byte(content)
	encoding := strings.TrimSpace(outputSpec.Encoding)
	if hasContentBase64 {
		contentBytes, err = base64.StdEncoding.DecodeString(contentBase64)
		if err != nil {
			return core.HandlerResponse{}, fmt.Errorf("content_base64 is invalid: %w", err)
		}
		if encoding == "" {
			encoding = "base64"
		}
	} else if encoding == "" {
		encoding = "utf-8"
	}
	outputDir := req.Bundle.OutputDir
	if outputDir == "" {
		return core.HandlerResponse{}, fmt.Errorf("artifact_write requires scoped output_dir")
	}

	outputDirAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if err := os.MkdirAll(outputDirAbs, 0o755); err != nil {
		return core.HandlerResponse{}, err
	}
	outputDirReal, err := filepath.EvalSymlinks(outputDirAbs)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	targetPath := filepath.Join(outputDirAbs, outputSpec.FileName)
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if !isInsideDir(outputDirAbs, targetAbs) {
		return core.HandlerResponse{}, fmt.Errorf("path for logical_key %q is outside output_dir %q", logicalKey, outputDir)
	}

	targetParent := filepath.Dir(targetAbs)
	existingParent, err := nearestExistingPath(targetParent)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	existingParentReal, err := filepath.EvalSymlinks(existingParent)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if !isInsideDir(outputDirReal, existingParentReal) {
		return core.HandlerResponse{}, fmt.Errorf("path for logical_key %q resolves outside output_dir %q", logicalKey, outputDir)
	}
	if err := os.MkdirAll(targetParent, 0o755); err != nil {
		return core.HandlerResponse{}, err
	}
	targetParentReal, err := filepath.EvalSymlinks(targetParent)
	if err != nil {
		return core.HandlerResponse{}, err
	}
	if !isInsideDir(outputDirReal, targetParentReal) {
		return core.HandlerResponse{}, fmt.Errorf("path for logical_key %q resolves outside output_dir %q", logicalKey, outputDir)
	}
	if info, err := os.Lstat(targetAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return core.HandlerResponse{}, fmt.Errorf("path for logical_key %q is a symlink", logicalKey)
	} else if err != nil && !os.IsNotExist(err) {
		return core.HandlerResponse{}, err
	}

	if err := os.WriteFile(targetAbs, contentBytes, 0o644); err != nil {
		return core.HandlerResponse{}, err
	}

	artifactURI := buildArtifactURI(req.Bundle.OutputURIBase, outputSpec.FileName)
	contentType := artifactContentType(outputSpec.ObjectType, outputSpec.ContentType, outputSpec.FileName)
	return core.HandlerResponse{
		Data: map[string]any{
			"logical_key":  logicalKey,
			"object_type":  outputSpec.ObjectType,
			"content_type": contentType,
			"encoding":     encoding,
			"status":       "produced",
			"path":         targetAbs,
			"artifact_uri": artifactURI,
			"size_bytes":   len(contentBytes),
		},
	}, nil
}

func buildArtifactURI(base, fileName string) string {
	base = strings.TrimSpace(strings.ReplaceAll(base, "\\", "/"))
	fileName = strings.TrimSpace(strings.ReplaceAll(fileName, "\\", "/"))
	if base == "" || fileName == "" {
		return ""
	}
	return path.Clean(strings.TrimSuffix(base, "/") + "/" + fileName)
}

func nearestExistingPath(path string) (string, error) {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("no existing parent found for %q", path)
		}
		path = parent
	}
}

func isInsideDir(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func requiredStringParam(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", fmt.Errorf("missing %q param", key)
	}
	s, ok := value.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("param %q must be a non-empty string", key)
	}
	return s, nil
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing %q arg", key)
	}
	s, ok := value.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("arg %q must be a non-empty string", key)
	}
	return s, nil
}

func optionalStringArg(args map[string]any, key string) (string, bool, error) {
	value, ok := args[key]
	if !ok {
		return "", false, nil
	}
	s, ok := value.(string)
	if !ok || s == "" {
		return "", true, fmt.Errorf("arg %q must be a non-empty string", key)
	}
	return s, true, nil
}

func artifactContentType(objectType, declared, fileName string) string {
	declared = strings.TrimSpace(declared)
	if declared != "" {
		return declared
	}
	switch strings.TrimSpace(strings.ToLower(objectType)) {
	case "markdown":
		return "text/markdown; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "html":
		return "text/html; charset=utf-8"
	case "css":
		return "text/css; charset=utf-8"
	case "javascript", "js":
		return "text/javascript; charset=utf-8"
	case "typescript", "ts":
		return "text/typescript; charset=utf-8"
	case "text":
		return "text/plain; charset=utf-8"
	case "archive":
		return "application/zip"
	case "binary":
		return "application/octet-stream"
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs", ".cjs":
		return "text/javascript; charset=utf-8"
	case ".ts", ".tsx":
		return "text/typescript; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func stringSliceParam(params map[string]any, key string) ([]string, error) {
	value, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("missing %q param", key)
	}
	switch v := value.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("param %q must contain only strings", key)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("param %q must be []string", key)
	}
}
