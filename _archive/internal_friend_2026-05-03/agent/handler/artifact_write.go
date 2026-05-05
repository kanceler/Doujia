package handler

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"doujia/internal/agent/core"
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
		Description: "根据 logical_key 和 content 写入已声明的输出产物",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"logical_key", "content"},
			"properties": map[string]any{
				"logical_key": map[string]any{
					"type":        "string",
					"description": "目标输出产物的 logical_key。",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "要写入的完整产物内容。",
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
	content, err := requiredStringArg(req.Args, "content")
	if err != nil {
		return core.HandlerResponse{}, err
	}
	outputSpec, ok := req.OpSpec.FindOutput(logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("artifact_logical_key_not_allowed: %s", logicalKey)
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

	if err := os.WriteFile(targetAbs, []byte(content), 0o644); err != nil {
		return core.HandlerResponse{}, err
	}

	artifactURI := buildArtifactURI(req.Bundle.OutputURIBase, outputSpec.FileName)
	return core.HandlerResponse{
		Data: map[string]any{
			"logical_key":  logicalKey,
			"object_type":  outputSpec.ObjectType,
			"status":       "produced",
			"path":         targetAbs,
			"artifact_uri": artifactURI,
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
