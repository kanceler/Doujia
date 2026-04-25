package artifact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScopedLocalStore allows reads anywhere under RunRoot, but only writes under WorkspaceRoot.
type ScopedLocalStore struct {
	RunRoot       string
	WorkspaceRoot string
}

func (s *ScopedLocalStore) Read(_ context.Context, uri string) ([]byte, error) {
	fullPath, err := s.resolveReadPath(uri)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

func (s *ScopedLocalStore) Write(_ context.Context, uri string, content []byte) error {
	fullPath, err := s.resolveWritePath(uri)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0o644)
}

func (s *ScopedLocalStore) resolveReadPath(uri string) (string, error) {
	return s.resolveWithinRoot(s.RunRoot, uri, "read root")
}

func (s *ScopedLocalStore) resolveWritePath(uri string) (string, error) {
	runRoot := strings.TrimSpace(s.RunRoot)
	workspaceRoot := strings.TrimSpace(s.WorkspaceRoot)
	if runRoot == "" {
		return "", fmt.Errorf("run root is required")
	}
	if workspaceRoot == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	targetAbs, err := s.resolveAbsoluteWithinRoot(runRoot, uri, "run root")
	if err != nil {
		return "", err
	}
	workspaceAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(workspaceAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact uri escapes workspace root: %s", uri)
	}
	return targetAbs, nil
}

func (s *ScopedLocalStore) resolveWithinRoot(root, uri, label string) (string, error) {
	targetAbs, err := s.resolveAbsoluteWithinRoot(root, uri, label)
	if err != nil {
		return "", err
	}
	return targetAbs, nil
}

func (s *ScopedLocalStore) resolveAbsoluteWithinRoot(root, uri, label string) (string, error) {
	root = strings.TrimSpace(root)
	uri = strings.TrimSpace(uri)
	if root == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	if uri == "" {
		return "", fmt.Errorf("artifact uri is required")
	}
	if filepath.IsAbs(uri) {
		return "", fmt.Errorf("artifact uri must be relative: %s", uri)
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(normalizeArtifactURI(uri))))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact uri escapes %s: %s", label, uri)
	}
	return targetAbs, nil
}

func normalizeArtifactURI(uri string) string {
	uri = filepath.ToSlash(strings.TrimSpace(uri))
	parts := strings.Split(uri, "/")
	if len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" {
		return strings.Join(parts[2:], "/")
	}
	return uri
}
