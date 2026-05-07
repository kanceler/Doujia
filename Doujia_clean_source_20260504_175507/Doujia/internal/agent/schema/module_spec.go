package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ModuleSpec struct {
	ModuleID           string   `json:"module_id"`
	ModuleName         string   `json:"module_name"`
	ModuleRole         string   `json:"module_role,omitempty"`
	ImplementationRole string   `json:"implementation_role"`
	BranchName         string   `json:"branch_name,omitempty"`
	WorktreeDir        string   `json:"worktree_dir"`
	TestRunDir         string   `json:"test_run_dir,omitempty"`
	OwnedPaths         []string `json:"owned_paths"`
	RuntimeWritePaths  []string `json:"runtime_write_paths,omitempty"`
	ForbiddenPaths     []string `json:"forbidden_paths,omitempty"`
	TestCommand        string   `json:"test_command"`
	Complexity         string   `json:"complexity"`
	Tester             string   `json:"tester,omitempty"`
}

type ModuleSpecs struct {
	Modules           []ModuleSpec `json:"modules"`
	GlobalTestCommand string       `json:"global_test_command,omitempty"`
}

func (s ModuleSpec) Validate() error {
	required := map[string]string{
		"module_id":           s.ModuleID,
		"module_name":         s.ModuleName,
		"implementation_role": s.ImplementationRole,
		"worktree_dir":        s.WorktreeDir,
		"test_command":        s.TestCommand,
		"complexity":          s.Complexity,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing_required_field: %s", field)
		}
	}

	switch strings.TrimSpace(s.ModuleRole) {
	case "", "frontend", "backend":
	default:
		return fmt.Errorf("invalid_module_role: %s", s.ModuleRole)
	}

	switch strings.TrimSpace(s.ImplementationRole) {
	case "front", "coder":
	default:
		return fmt.Errorf("invalid_implementation_role: %s", s.ImplementationRole)
	}

	switch strings.TrimSpace(s.Complexity) {
	case "low", "high":
	default:
		return fmt.Errorf("invalid_complexity: %s", s.Complexity)
	}

	if len(s.OwnedPaths) == 0 {
		return fmt.Errorf("missing_required_field: owned_paths")
	}
	patterns := append([]string{}, s.OwnedPaths...)
	patterns = append(patterns, s.RuntimeWritePaths...)
	patterns = append(patterns, s.ForbiddenPaths...)
	for _, p := range patterns {
		if !isSafeRelativePattern(p) {
			return fmt.Errorf("invalid_path_pattern: %s", p)
		}
	}
	return nil
}

func (s ModuleSpecs) Validate() error {
	if len(s.Modules) == 0 {
		return fmt.Errorf("missing_required_field: modules")
	}
	seen := map[string]bool{}
	for _, module := range s.Modules {
		if err := module.Validate(); err != nil {
			return fmt.Errorf("module %q: %w", module.ModuleID, err)
		}
		if seen[module.ModuleID] {
			return fmt.Errorf("duplicate_module_id: %s", module.ModuleID)
		}
		seen[module.ModuleID] = true
	}
	if s.GlobalTestCommand != "" && strings.TrimSpace(s.GlobalTestCommand) == "" {
		return fmt.Errorf("invalid_global_test_command")
	}
	return nil
}

func ReadModuleSpecFile(path string) (ModuleSpec, error) {
	var value ModuleSpec
	return value, readSchemaJSONFile(path, &value)
}

func ReadModuleSpecsFile(path string) (ModuleSpecs, error) {
	var value ModuleSpecs
	return value, readSchemaJSONFile(path, &value)
}

func ModuleSpecPromptContract() string {
	return strings.TrimSpace(`module_spec-related JSON must be valid JSON and must satisfy all of the following constraints:
- module_specs.json must contain:
  - "modules": a non-empty array; every item must be a complete module_spec object
  - optional "global_test_command": string
- Every module_spec object must contain:
  - "module_id": non-empty string
  - "module_name": non-empty string
  - "implementation_role": must be exactly "front" or "coder"
  - "worktree_dir": non-empty string
  - "owned_paths": a non-empty string array of repo-relative writable paths or directory globs ending with "/**"
  - "test_command": non-empty string
  - "complexity": must be exactly "low" or "high"
- Optional module_spec fields:
  - "module_role": if present, must be exactly "frontend" or "backend"
  - "branch_name": string
  - "test_run_dir": string
  - "runtime_write_paths": string array of extra repo-relative writable paths or directory globs ending with "/**" used only for runtime support files such as scoped tests or shared setup files
  - "forbidden_paths": string array
  - "tester": string
- Path constraints:
  - owned_paths / runtime_write_paths / forbidden_paths entries must be repo-relative paths or directory globs ending with "/**"
  - absolute paths are not allowed
  - "." / ".." / parent-directory escapes are not allowed
  - writing into .git/ is not allowed
- Single-module files such as module01_spec.json and module02_spec.json must each also be complete module_spec objects.
- module_specs.json.modules must match the single-module spec files one by one, and module_id values must not repeat.
- Output JSON only. Do not output comments. Do not output Markdown code fences.`)
}

func validateModuleSpecConsistency(collection ModuleSpecs, singles ...ModuleSpec) error {
	if err := collection.Validate(); err != nil {
		return err
	}
	byID := map[string]ModuleSpec{}
	for _, module := range collection.Modules {
		byID[module.ModuleID] = module
	}
	for _, single := range singles {
		if err := single.Validate(); err != nil {
			return err
		}
		combined, ok := byID[single.ModuleID]
		if !ok {
			return fmt.Errorf("module_id %q missing from module_specs.modules", single.ModuleID)
		}
		if combined.ModuleName != single.ModuleName {
			return fmt.Errorf("module %q module_name mismatch", single.ModuleID)
		}
		if combined.WorktreeDir != single.WorktreeDir {
			return fmt.Errorf("module %q worktree_dir mismatch", single.ModuleID)
		}
		if combined.TestRunDir != single.TestRunDir {
			return fmt.Errorf("module %q test_run_dir mismatch", single.ModuleID)
		}
		if combined.BranchName != single.BranchName {
			return fmt.Errorf("module %q branch_name mismatch", single.ModuleID)
		}
		if combined.TestCommand != single.TestCommand {
			return fmt.Errorf("module %q test_command mismatch", single.ModuleID)
		}
		if combined.Complexity != single.Complexity {
			return fmt.Errorf("module %q complexity mismatch", single.ModuleID)
		}
		if combined.ModuleRole != single.ModuleRole {
			return fmt.Errorf("module %q module_role mismatch", single.ModuleID)
		}
		if combined.ImplementationRole != single.ImplementationRole {
			return fmt.Errorf("module %q implementation_role mismatch", single.ModuleID)
		}
		if !sameStringSlice(combined.OwnedPaths, single.OwnedPaths) {
			return fmt.Errorf("module %q owned_paths mismatch", single.ModuleID)
		}
		if !sameStringSlice(combined.RuntimeWritePaths, single.RuntimeWritePaths) {
			return fmt.Errorf("module %q runtime_write_paths mismatch", single.ModuleID)
		}
		if !sameStringSlice(combined.ForbiddenPaths, single.ForbiddenPaths) {
			return fmt.Errorf("module %q forbidden_paths mismatch", single.ModuleID)
		}
		if combined.Tester != single.Tester {
			return fmt.Errorf("module %q tester mismatch", single.ModuleID)
		}
	}
	return nil
}

func ValidateModuleSpecFiles(moduleSpecsPath string, singleModulePaths ...string) error {
	combined, err := ReadModuleSpecsFile(moduleSpecsPath)
	if err != nil {
		return err
	}
	singles := make([]ModuleSpec, 0, len(singleModulePaths))
	for _, p := range singleModulePaths {
		spec, err := ReadModuleSpecFile(p)
		if err != nil {
			return err
		}
		singles = append(singles, spec)
	}
	return validateModuleSpecConsistency(combined, singles...)
}

func readSchemaJSONFile(path string, dest any) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, dest)
}

func isSafeRelativePattern(value string) bool {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if value == "" || strings.HasPrefix(value, "/") {
		return false
	}
	base := strings.TrimSuffix(value, "/**")
	cleaned := filepath.ToSlash(filepath.Clean(base))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	return !strings.HasPrefix(cleaned, ".git/")
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
