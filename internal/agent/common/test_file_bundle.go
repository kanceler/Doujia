package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TestFileBundle struct {
	SchemaVersion int        `json:"schema_version"`
	Kind          string     `json:"kind"`
	ModuleID      string     `json:"module_id"`
	TestCommand   string     `json:"test_command"`
	TestFiles     []TestFile `json:"test_files"`
}

type TestFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func ParseTestFileBundle(content []byte) (TestFileBundle, error) {
	var bundle TestFileBundle
	if err := json.Unmarshal(content, &bundle); err != nil {
		return TestFileBundle{}, fmt.Errorf("parse test file bundle json: %w", err)
	}
	if err := ValidateTestFileBundle(bundle); err != nil {
		return TestFileBundle{}, err
	}
	return bundle, nil
}

func ValidateTestFileBundle(bundle TestFileBundle, allowedKinds ...string) error {
	if bundle.SchemaVersion != 1 {
		return fmt.Errorf("test file bundle schema_version must be 1")
	}
	if strings.TrimSpace(bundle.Kind) == "" {
		return fmt.Errorf("test file bundle kind is required")
	}
	if len(allowedKinds) > 0 {
		allowed := false
		for _, kind := range allowedKinds {
			if bundle.Kind == kind {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("test file bundle kind %q is not allowed", bundle.Kind)
		}
	}
	if strings.TrimSpace(bundle.ModuleID) == "" {
		return fmt.Errorf("test file bundle module_id is required")
	}
	if strings.TrimSpace(bundle.TestCommand) == "" {
		return fmt.Errorf("test file bundle test_command is required")
	}
	if len(bundle.TestFiles) == 0 {
		return fmt.Errorf("test file bundle test_files is required")
	}
	for i, file := range bundle.TestFiles {
		if err := validateBundlePath(file.Path); err != nil {
			return fmt.Errorf("test_files[%d] path: %w", i, err)
		}
	}
	return nil
}

func MaterializeTestFiles(worktree string, bundle TestFileBundle) ([]string, error) {
	if strings.TrimSpace(worktree) == "" {
		return nil, fmt.Errorf("worktree is required")
	}
	if err := ValidateTestFileBundle(bundle); err != nil {
		return nil, err
	}
	worktreeAbs, err := filepath.Abs(filepath.Clean(worktree))
	if err != nil {
		return nil, fmt.Errorf("resolve worktree: %w", err)
	}
	written := make([]string, 0, len(bundle.TestFiles))
	for _, file := range bundle.TestFiles {
		if err := validateBundlePath(file.Path); err != nil {
			return nil, err
		}
		relative := filepath.FromSlash(strings.TrimSpace(file.Path))
		target := filepath.Join(worktreeAbs, relative)
		targetAbs, err := filepath.Abs(filepath.Clean(target))
		if err != nil {
			return nil, fmt.Errorf("resolve test file path %q: %w", file.Path, err)
		}
		if !isPathInside(worktreeAbs, targetAbs) {
			return nil, fmt.Errorf("test file path %q escapes worktree", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
			return nil, fmt.Errorf("create test file directory %q: %w", file.Path, err)
		}
		if err := os.WriteFile(targetAbs, []byte(file.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write test file %q: %w", file.Path, err)
		}
		written = append(written, targetAbs)
	}
	return written, nil
}

func BundleTestCommand(bundle TestFileBundle) string {
	return strings.TrimSpace(bundle.TestCommand)
}

func validateBundlePath(raw string) error {
	path := filepath.ToSlash(strings.TrimSpace(raw))
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if strings.HasPrefix(path, "/") || filepath.IsAbs(filepath.FromSlash(path)) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	for _, segment := range strings.Split(path, "/") {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "." {
			continue
		}
		if segment == ".." {
			return fmt.Errorf("paths containing .. are not allowed")
		}
		if segment == ".git" || segment == ".devflow" {
			return fmt.Errorf("paths targeting %s are not allowed", segment)
		}
	}
	return nil
}

func isPathInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
