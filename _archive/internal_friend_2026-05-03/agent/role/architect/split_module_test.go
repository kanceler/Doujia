package architect

import (
	"strings"
	"testing"

	"doujia/internal/agent/schema"
)

func TestBuildSplitModulesCreatesFixedFrontendAndDynamicBackends(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "cd server && npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "cd server && npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_pet_journal",
				OwnedPaths:       []string{"pages/home/**", "pages/pet/**", "pages/records/**", "pages/record-form/**", "pages/record-detail/**", "components/**", "utils/request.js", "app.js", "app.json", "app.wxss"},
				Responsibilities: []string{"Implement mini-program pages and request flow"},
				CoderFocus:       []string{"Keep all frontend API calls in one request helper"},
				TesterFocus:      []string{"Validate page rendering and API error states"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_summary",
				OwnedPaths:       []string{"server/routes/pets.js", "server/models/pet.js"},
				Responsibilities: []string{"Implement pet CRUD and summary endpoints"},
				CoderFocus:       []string{"Keep pet DTO fields stable for frontend"},
				TesterFocus:      []string{"Validate pet CRUD and summary responses"},
			},
			{
				ModuleID:         "module03",
				ModuleRole:       "backend",
				ModuleName:       "backend_record_services",
				OwnedPaths:       []string{"server/routes/feedings.js", "server/routes/weights.js", "server/models/record.js"},
				Responsibilities: []string{"Implement record CRUD flows"},
				CoderFocus:       []string{"Keep record type payloads consistent"},
				TesterFocus:      []string{"Validate record CRUD by type"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on pet_id and record type fields"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 2}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if len(modules) != 3 {
		t.Fatalf("buildSplitModules() len = %d, want 3", len(modules))
	}
	if modules[0].Spec.ModuleID != "module01" || modules[0].ModuleRole != "frontend" {
		t.Fatalf("module01 = %+v, want fixed frontend module", modules[0])
	}
	if modules[0].Spec.ModuleRole != "frontend" || modules[0].Spec.ImplementationRole != "front" {
		t.Fatalf("module01 spec = %+v, want frontend/front routing metadata", modules[0].Spec)
	}
	if modules[1].Spec.ModuleID != "module02" || modules[1].ModuleRole != "backend" {
		t.Fatalf("module02 = %+v, want backend module", modules[1])
	}
	if modules[1].Spec.ModuleRole != "backend" || modules[1].Spec.ImplementationRole != "coder" {
		t.Fatalf("module02 spec = %+v, want backend/coder routing metadata", modules[1].Spec)
	}
	if modules[2].Spec.ModuleID != "module03" || modules[2].ModuleRole != "backend" {
		t.Fatalf("module03 = %+v, want backend module", modules[2])
	}
	if modules[2].Spec.ModuleRole != "backend" || modules[2].Spec.ImplementationRole != "coder" {
		t.Fatalf("module03 spec = %+v, want backend/coder routing metadata", modules[2].Spec)
	}
}

func TestBuildSplitModulesAssignsFrontendAndBackendOwnedPaths(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "cd server && npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "cd server && npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_pet_journal",
				OwnedPaths:       []string{"pages/home/**", "pages/pet/**", "components/**", "utils/request.js", "app.js", "app.json", "app.wxss"},
				Responsibilities: []string{"Implement the frontend pages"},
				CoderFocus:       []string{"Use one shared request helper"},
				TesterFocus:      []string{"Validate page rendering"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_records",
				OwnedPaths:       []string{"server/routes/pets.js", "server/routes/feedings.js", "server/models/pet.js", "server/models/record.js"},
				Responsibilities: []string{"Implement backend CRUD endpoints"},
				CoderFocus:       []string{"Keep response structures stable"},
				TesterFocus:      []string{"Validate CRUD response payloads"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on pet_id fields"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if len(modules[0].Spec.OwnedPaths) == 0 || modules[0].Spec.OwnedPaths[0] != "pages/home/**" {
		t.Fatalf("frontend owned_paths = %+v, want semantic frontend paths", modules[0].Spec.OwnedPaths)
	}
	if len(modules[1].Spec.OwnedPaths) == 0 || modules[1].Spec.OwnedPaths[0] != "server/routes/pets.js" {
		t.Fatalf("backend owned_paths = %+v, want semantic backend paths", modules[1].Spec.OwnedPaths)
	}
}

func TestBuildSplitModulesAssignsRuntimeWritePathsByModuleRole(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "cd server && npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "cd server && npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_pet_journal",
				OwnedPaths:       []string{"miniprogram/**"},
				Responsibilities: []string{"Implement the frontend pages"},
				CoderFocus:       []string{"Use one shared request helper"},
				TesterFocus:      []string{"Validate page rendering"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_core",
				OwnedPaths:       []string{"server/app.js", "server/db/**", "server/utils/**", "server/routes/pets.js"},
				Responsibilities: []string{"Implement pet endpoints"},
				CoderFocus:       []string{"Keep response structures stable"},
				TesterFocus:      []string{"Validate CRUD response payloads"},
			},
			{
				ModuleID:         "module03",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_records",
				OwnedPaths:       []string{"server/routes/feedings.js", "server/routes/weights.js", "server/routes/health.js"},
				Responsibilities: []string{"Implement record endpoints"},
				CoderFocus:       []string{"Keep record payloads stable"},
				TesterFocus:      []string{"Validate record response payloads"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on pet_id fields"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 2}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}

	if got := modules[0].Spec.RuntimeWritePaths; len(got) != 1 || got[0] != "miniprogram/tests/**" {
		t.Fatalf("frontend runtime_write_paths = %+v, want miniprogram/tests/**", got)
	}
	if got := modules[1].Spec.RuntimeWritePaths; len(got) == 0 || got[0] != "server/package.json" {
		t.Fatalf("module02 runtime_write_paths = %+v, want backend shared scaffold paths", got)
	}
	if got := modules[2].Spec.RuntimeWritePaths; len(got) != 1 || got[0] != "server/tests/module03/**" {
		t.Fatalf("module03 runtime_write_paths = %+v, want isolated backend test path", got)
	}
}

func TestBuildSplitModulesEnrichesBackendCoreOwnedPathsAndSharedForbids(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "cd server && npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "cd server && npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_pet_journal",
				OwnedPaths:       []string{"miniprogram/**"},
				Responsibilities: []string{"Implement the frontend pages"},
				CoderFocus:       []string{"Use one shared request helper"},
				TesterFocus:      []string{"Validate page rendering"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_core",
				OwnedPaths:       []string{"server/app.js", "server/db/**", "server/routes/pets.js"},
				Responsibilities: []string{"Implement pet endpoints"},
				CoderFocus:       []string{"Keep response structures stable"},
				TesterFocus:      []string{"Validate CRUD response payloads"},
			},
			{
				ModuleID:         "module03",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_records",
				OwnedPaths:       []string{"server/routes/feedings.js", "server/routes/weights.js", "server/routes/health.js"},
				Responsibilities: []string{"Implement record endpoints"},
				CoderFocus:       []string{"Keep record payloads stable"},
				TesterFocus:      []string{"Validate record response payloads"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on pet_id fields"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 2}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}

	if !containsString(modules[1].Spec.OwnedPaths, "server/utils/**") {
		t.Fatalf("module02 owned_paths = %+v, want server/utils/** enrichment", modules[1].Spec.OwnedPaths)
	}
	if !containsString(modules[2].Spec.ForbiddenPaths, "server/package.json") {
		t.Fatalf("module03 forbidden_paths = %+v, want shared backend package forbidden", modules[2].Spec.ForbiddenPaths)
	}
	if !containsString(modules[0].Spec.ForbiddenPaths, "README.md") {
		t.Fatalf("module01 forbidden_paths = %+v, want README.md explicitly forbidden", modules[0].Spec.ForbiddenPaths)
	}
}

func TestBuildSeedTestsKeepsFilesInsideRuntimeWritableScopes(t *testing.T) {
	t.Parallel()

	frontend := splitModule{
		ModuleRole: "frontend",
		Spec: schema.ModuleSpec{
			ModuleID:           "module01",
			ModuleName:         "frontend",
			ModuleRole:         "frontend",
			ImplementationRole: "front",
			OwnedPaths:         []string{"miniprogram/**"},
			RuntimeWritePaths:  []string{"miniprogram/tests/**"},
			TestCommand:        "echo frontend",
			Complexity:         "high",
		},
		Plan: semanticModulePlan{TesterFocus: []string{"Validate frontend smoke flow"}},
	}
	backend := splitModule{
		ModuleRole: "backend",
		Spec: schema.ModuleSpec{
			ModuleID:           "module03",
			ModuleName:         "backend",
			ModuleRole:         "backend",
			ImplementationRole: "coder",
			OwnedPaths:         []string{"server/routes/feedings.js"},
			RuntimeWritePaths:  []string{"server/tests/module03/**"},
			TestCommand:        "cd server && npm test",
			Complexity:         "high",
		},
		Plan: semanticModulePlan{TesterFocus: []string{"Validate backend smoke flow"}},
	}

	frontendSeed := buildSeedTests(frontend)
	if !strings.Contains(frontendSeed, `"miniprogram/tests/frontend.smoke.md"`) {
		t.Fatalf("frontend buildSeedTests() = %s, want miniprogram runtime test path", frontendSeed)
	}
	if strings.Contains(frontendSeed, `"tests/frontend.smoke.md"`) {
		t.Fatalf("frontend buildSeedTests() = %s, do not want legacy root tests path", frontendSeed)
	}

	backendSeed := buildSeedTests(backend)
	if !strings.Contains(backendSeed, `"server/tests/module03/smoke.test.js"`) {
		t.Fatalf("backend buildSeedTests() = %s, want module-scoped backend test path", backendSeed)
	}
}

func TestValidateSemanticSplitPlanRejectsWrongModuleRole(t *testing.T) {
	t.Parallel()

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "backend",
				ModuleName:       "wrong_frontend",
				OwnedPaths:       []string{"server/routes/pets.js"},
				Responsibilities: []string{"Incorrectly uses backend role"},
				CoderFocus:       []string{"bad"},
				TesterFocus:      []string{"bad"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_records",
				OwnedPaths:       []string{"server/models/pet.js"},
				Responsibilities: []string{"Implement pet endpoints"},
				CoderFocus:       []string{"Keep DTOs stable"},
				TesterFocus:      []string{"Validate payloads"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on pet_id fields"},
		},
	}

	err := validateSemanticSplitPlan(schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err == nil || !strings.Contains(err.Error(), "module01") {
		t.Fatalf("validateSemanticSplitPlan() error = %v, want module01 role validation error", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
