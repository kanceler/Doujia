package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentSpecValidateRequiresCheckCommands(t *testing.T) {
	t.Parallel()

	spec := EnvironmentSpec{
		Runtime:            "node",
		Image:              "node:20-bookworm",
		PackageManager:     "npm",
		DefaultTestCommand: "npm test",
	}
	err := spec.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing check_commands")
	}
	if !strings.Contains(err.Error(), "check_commands") {
		t.Fatalf("Validate() error = %v, want check_commands", err)
	}
}

func TestEnvironmentSpecValidateRejectsUnsupportedImage(t *testing.T) {
	t.Parallel()

	spec := EnvironmentSpec{
		Runtime:            "node",
		Image:              "node:18-alpine",
		PackageManager:     "npm",
		CheckCommands:      []string{"node --version"},
		DefaultTestCommand: "npm test",
	}
	err := spec.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unsupported image")
	}
	if !strings.Contains(err.Error(), "image_not_allowed") {
		t.Fatalf("Validate() error = %v, want image_not_allowed", err)
	}
}

func TestEnvironmentSpecPromptContractIncludesRequiredFields(t *testing.T) {
	t.Parallel()

	text := EnvironmentSpecPromptContract()
	for _, want := range []string{
		"runtime",
		"image",
		"package_manager",
		"check_commands",
		"default_test_command",
		"repo_init_files",
		"path_policy",
		"frontend_roots",
		"backend_roots",
		"packaging_roots",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("EnvironmentSpecPromptContract() missing %q in:\n%s", want, text)
		}
	}
}

func TestEnvironmentSpecValidateAllowsPathPolicy(t *testing.T) {
	t.Parallel()

	spec := EnvironmentSpec{
		Runtime:            "node",
		Image:              "node:20-bookworm",
		PackageManager:     "npm",
		CheckCommands:      []string{"node --version"},
		DefaultTestCommand: "npm test",
		PathPolicy: PathPolicy{
			FrontendRoots: []string{"app/**", "desktop/**", "index.html"},
			BackendRoots:  []string{"api/**"},
			PackagingRoots: []string{
				"build/**",
			},
		},
	}

	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEnvironmentSpecValidateRejectsUnsafePathPolicy(t *testing.T) {
	t.Parallel()

	spec := EnvironmentSpec{
		Runtime:            "node",
		Image:              "node:20-bookworm",
		PackageManager:     "npm",
		CheckCommands:      []string{"node --version"},
		DefaultTestCommand: "npm test",
		PathPolicy: PathPolicy{
			FrontendRoots: []string{"../app/**"},
		},
	}

	err := spec.Validate()
	if err == nil || !strings.Contains(err.Error(), "path_policy") {
		t.Fatalf("Validate() error = %v, want path_policy validation error", err)
	}
}

func TestModuleSpecsValidateRejectsDuplicateModuleID(t *testing.T) {
	t.Parallel()

	err := (ModuleSpecs{
		Modules: []ModuleSpec{
			{ModuleID: "module01", ModuleName: "frontend", WorktreeDir: "/workspace/worktrees/module01", OwnedPaths: []string{"miniprogram/**"}, TestCommand: "npm test", Complexity: "low", ImplementationRole: "front"},
			{ModuleID: "module01", ModuleName: "frontend-copy", WorktreeDir: "/workspace/worktrees/module01b", OwnedPaths: []string{"miniprogram/**"}, TestCommand: "npm test", Complexity: "low", ImplementationRole: "front"},
		},
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate_module_id") {
		t.Fatalf("Validate() error = %v, want duplicate_module_id", err)
	}
}

func TestModuleSpecValidateRequiresImplementationRole(t *testing.T) {
	t.Parallel()

	err := (ModuleSpec{
		ModuleID:    "module01",
		ModuleName:  "frontend",
		WorktreeDir: "/workspace/worktrees/module01",
		OwnedPaths:  []string{"pages/**"},
		TestCommand: "npm test",
		Complexity:  "high",
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "implementation_role") {
		t.Fatalf("Validate() error = %v, want implementation_role requirement", err)
	}
}

func TestModuleSpecValidateRejectsInvalidRoleFields(t *testing.T) {
	t.Parallel()

	err := (ModuleSpec{
		ModuleID:           "module01",
		ModuleName:         "frontend",
		ModuleRole:         "mobile",
		ImplementationRole: "designer",
		WorktreeDir:        "/workspace/worktrees/module01",
		OwnedPaths:         []string{"pages/**"},
		TestCommand:        "npm test",
		Complexity:         "high",
	}).Validate()
	if err == nil || (!strings.Contains(err.Error(), "module_role") && !strings.Contains(err.Error(), "implementation_role")) {
		t.Fatalf("Validate() error = %v, want role validation error", err)
	}
}

func TestModuleSpecPromptContractIncludesRoleFields(t *testing.T) {
	t.Parallel()

	text := ModuleSpecPromptContract()
	for _, want := range []string{"module_role", "implementation_role", "runtime_write_paths"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ModuleSpecPromptContract() missing %q in:\n%s", want, text)
		}
	}
}

func TestModuleSpecValidateRejectsUnsafeRuntimeWritePaths(t *testing.T) {
	t.Parallel()

	err := (ModuleSpec{
		ModuleID:           "module02",
		ModuleName:         "backend",
		ModuleRole:         "backend",
		ImplementationRole: "coder",
		WorktreeDir:        "/workspace/worktrees/module02",
		OwnedPaths:         []string{"server/routes/pets.js"},
		RuntimeWritePaths:  []string{"../server/package.json"},
		TestCommand:        "cd server && npm test",
		Complexity:         "high",
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid_path_pattern") {
		t.Fatalf("Validate() error = %v, want invalid runtime_write_paths pattern", err)
	}
}

func TestValidateModuleSpecFilesRejectsRuntimeWritePathsMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	moduleSpecsPath := filepath.Join(dir, "module_specs.json")
	moduleSpecPath := filepath.Join(dir, "module01_spec.json")

	if err := os.WriteFile(moduleSpecsPath, []byte(`{
  "modules": [
    {
      "module_id": "module01",
      "module_name": "frontend",
      "module_role": "frontend",
      "implementation_role": "front",
      "branch_name": "feature/module01-frontend",
      "worktree_dir": "/workspace/worktrees/module01",
      "test_run_dir": "/workspace/test-runs/module01",
      "owned_paths": ["miniprogram/**"],
      "runtime_write_paths": ["miniprogram/tests/**"],
      "test_command": "echo frontend",
      "complexity": "high"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write module_specs.json: %v", err)
	}
	if err := os.WriteFile(moduleSpecPath, []byte(`{
  "module_id": "module01",
  "module_name": "frontend",
  "module_role": "frontend",
  "implementation_role": "front",
  "branch_name": "feature/module01-frontend",
  "worktree_dir": "/workspace/worktrees/module01",
  "test_run_dir": "/workspace/test-runs/module01",
  "owned_paths": ["miniprogram/**"],
  "runtime_write_paths": ["miniprogram/spec-tests/**"],
  "test_command": "echo frontend",
  "complexity": "high"
}`), 0o644); err != nil {
		t.Fatalf("write module01_spec.json: %v", err)
	}

	err := ValidateModuleSpecFiles(moduleSpecsPath, moduleSpecPath)
	if err == nil || !strings.Contains(err.Error(), "runtime_write_paths mismatch") {
		t.Fatalf("ValidateModuleSpecFiles() error = %v, want runtime_write_paths mismatch", err)
	}
}

func TestGlobalTestCommandsValidateRequiresCommandFields(t *testing.T) {
	t.Parallel()

	err := (GlobalTestCommands{
		Kind:     "global_test_commands",
		Commands: []GlobalTestCommand{{Name: "smoke"}},
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), ".command") {
		t.Fatalf("Validate() error = %v, want missing command field", err)
	}
}
