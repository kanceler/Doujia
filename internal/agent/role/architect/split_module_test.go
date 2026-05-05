package architect

import (
	"strings"
	"testing"

	"devflow/internal/agent/schema"
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

func TestBuildSplitModulesNormalizesBackendBareNPMTestCommand(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_pet_journal",
				OwnedPaths:       []string{"pages/home/**", "components/**"},
				Responsibilities: []string{"Implement the frontend pages"},
				CoderFocus:       []string{"Use one shared request helper"},
				TesterFocus:      []string{"Validate page rendering"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_pet_records",
				OwnedPaths:       []string{"server/routes/pets.js"},
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
	if got := modules[1].Spec.TestCommand; got != "cd server && npm test" {
		t.Fatalf("backend test_command = %q, want cd server && npm test", got)
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

func TestBuildSplitModulesAllowsFrontendRootOwnedPath(t *testing.T) {
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
				ModuleName:       "frontend_ui",
				OwnedPaths:       []string{"./src/frontend/**"},
				Responsibilities: []string{"Implement the frontend experience"},
				CoderFocus:       []string{"Keep API calls behind one helper"},
				TesterFocus:      []string{"Validate frontend smoke behavior"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_api",
				OwnedPaths:       []string{"server/routes/**", "server/models/**"},
				Responsibilities: []string{"Implement backend APIs"},
				CoderFocus:       []string{"Keep response structures stable"},
				TesterFocus:      []string{"Validate API responses"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use one shared request helper to prepend the configured backend base URL",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on API payload fields"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if got := modules[0].Spec.OwnedPaths; len(got) != 1 || got[0] != "src/frontend/**" {
		t.Fatalf("frontend owned_paths = %+v, want src/frontend/**", got)
	}
	if !containsString(modules[1].Spec.ForbiddenPaths, "src/**") {
		t.Fatalf("backend forbidden_paths = %+v, want src/**", modules[1].Spec.ForbiddenPaths)
	}
}

func TestBuildSplitModulesAllowsFrontendRootStaticFiles(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_snake",
				OwnedPaths:       []string{"index.html", "style.css", "game.js"},
				Responsibilities: []string{"Implement the static game shell"},
				CoderFocus:       []string{"Keep gameplay logic in one script"},
				TesterFocus:      []string{"Validate the page loads and responds to input"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_placeholder",
				OwnedPaths:       []string{"server/routes/health.js"},
				Responsibilities: []string{"Expose a health endpoint"},
				CoderFocus:       []string{"Keep the handler minimal"},
				TesterFocus:      []string{"Validate the route returns success"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use a relative path strategy when a backend exists",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on route naming"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if got := modules[0].Spec.OwnedPaths; len(got) != 3 || got[0] != "index.html" || got[1] != "style.css" || got[2] != "game.js" {
		t.Fatalf("frontend owned_paths = %+v, want concrete root static files", got)
	}
}

func TestBuildSplitModulesAllowsFrontendElectronPaths(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "npm test",
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_snake_electron",
				OwnedPaths:       []string{"index.html", "style.css", "game.js", "electron/config/builder.config.js"},
				Responsibilities: []string{"Implement the static game shell and Electron packaging config"},
				CoderFocus:       []string{"Keep gameplay logic in one script"},
				TesterFocus:      []string{"Validate the page loads and packaging config exists"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_placeholder",
				OwnedPaths:       []string{"server/routes/health.js"},
				Responsibilities: []string{"Expose a health endpoint"},
				CoderFocus:       []string{"Keep the route stable"},
				TesterFocus:      []string{"Validate the response payload"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use local static files inside Electron",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Electron shell loads the game entrypoint"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if !containsString(modules[0].Spec.OwnedPaths, "electron/config/builder.config.js") {
		t.Fatalf("frontend owned_paths = %+v, want electron config path", modules[0].Spec.OwnedPaths)
	}
	if !containsString(modules[1].Spec.ForbiddenPaths, "electron/**") {
		t.Fatalf("backend forbidden_paths = %+v, want electron/** forbidden", modules[1].Spec.ForbiddenPaths)
	}
}

func TestBuildSplitModulesUsesEnvironmentPathPolicy(t *testing.T) {
	t.Parallel()

	container := splitContainerContext{
		RepoDir:            "/workspace/repo",
		WorktreesDir:       "/workspace/worktrees",
		TestRunsDir:        "/workspace/test-runs",
		BranchPrefix:       "feature/",
		DefaultTestCommand: "npm test",
	}
	env := schema.EnvironmentSpec{
		DefaultTestCommand: "npm test",
		PathPolicy: schema.PathPolicy{
			FrontendRoots:  []string{"desktop/**", "game-shell/**"},
			BackendRoots:   []string{"api/**"},
			PackagingRoots: []string{"package/**"},
		},
	}

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_desktop_snake",
				OwnedPaths:       []string{"desktop/main.js", "game-shell/index.html", "package/windows/builder.config.js"},
				Responsibilities: []string{"Implement the desktop game shell and packaging"},
				CoderFocus:       []string{"Keep gameplay logic local"},
				TesterFocus:      []string{"Validate shell smoke behavior"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_placeholder",
				OwnedPaths:       []string{"api/health.js"},
				Responsibilities: []string{"Expose a health endpoint"},
				CoderFocus:       []string{"Keep response stable"},
				TesterFocus:      []string{"Validate API response"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use local desktop assets",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Desktop shell loads the entrypoint"},
		},
	}

	modules, err := buildSplitModules(container, env, schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err != nil {
		t.Fatalf("buildSplitModules() error = %v", err)
	}
	if !containsString(modules[0].Spec.OwnedPaths, "desktop/main.js") {
		t.Fatalf("frontend owned_paths = %+v, want desktop/main.js", modules[0].Spec.OwnedPaths)
	}
	if !containsString(modules[1].Spec.ForbiddenPaths, "desktop/**") {
		t.Fatalf("backend forbidden_paths = %+v, want desktop/** forbidden", modules[1].Spec.ForbiddenPaths)
	}
	if containsString(modules[1].Spec.OwnedPaths, "server/utils/**") {
		t.Fatalf("backend owned_paths = %+v, do not want default server enrichment under custom path policy", modules[1].Spec.OwnedPaths)
	}
	for _, path := range modules[1].Spec.RuntimeWritePaths {
		if strings.HasPrefix(path, "server/") {
			t.Fatalf("backend runtime_write_paths = %+v, do not want server path under custom path policy", modules[1].Spec.RuntimeWritePaths)
		}
	}
	for _, path := range modules[0].Spec.RuntimeWritePaths {
		if strings.HasPrefix(path, "miniprogram/") {
			t.Fatalf("frontend runtime_write_paths = %+v, do not want miniprogram path under custom path policy", modules[0].Spec.RuntimeWritePaths)
		}
	}
	seed := buildSeedTests(modules[0])
	if strings.Contains(seed, "miniprogram/tests") {
		t.Fatalf("frontend seed tests = %s, do not want miniprogram test path under custom path policy", seed)
	}
}

func TestSemanticSplitPromptsKeepBackendAwayFromPackagingPaths(t *testing.T) {
	t.Parallel()

	env := schema.EnvironmentSpec{
		DefaultTestCommand: "npm test",
		PathPolicy: schema.PathPolicy{
			FrontendRoots:  []string{"index.html", "style.css", "game.js"},
			BackendRoots:   []string{"server/**"},
			PackagingRoots: []string{"electron/**"},
		},
	}
	config := schema.RunDeliveryConfig{BackendModuleCount: 1}

	initial := buildSemanticSplitPrompt("static desktop snake game", env, config)
	if !strings.Contains(initial, "Never assign frontend or packaging paths to backend modules") {
		t.Fatalf("initial prompt missing backend path guard:\n%s", initial)
	}
	if !strings.Contains(initial, "create a minimal backend placeholder under backend_path_roots") {
		t.Fatalf("initial prompt missing backend fallback guidance:\n%s", initial)
	}

	repair := buildSemanticSplitRepairPrompt("static desktop snake game", env, config, `{"modules":[]}`, `module02 has invalid backend owned_path "electron/**"`)
	if !strings.Contains(repair, "Never assign frontend or packaging paths to backend modules") {
		t.Fatalf("repair prompt missing backend path guard:\n%s", repair)
	}
	if !strings.Contains(repair, "create a minimal backend placeholder under backend_path_roots") {
		t.Fatalf("repair prompt missing backend fallback guidance:\n%s", repair)
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

func TestValidateSemanticSplitPlanRejectsFrontendWildcardOwnedPath(t *testing.T) {
	t.Parallel()

	plan := semanticSplitPlan{
		Modules: []semanticModulePlan{
			{
				ModuleID:         "module01",
				ModuleRole:       "frontend",
				ModuleName:       "frontend_snake",
				OwnedPaths:       []string{"*.html"},
				Responsibilities: []string{"Render the static game shell"},
				CoderFocus:       []string{"Keep gameplay logic simple"},
				TesterFocus:      []string{"Validate the page renders"},
			},
			{
				ModuleID:         "module02",
				ModuleRole:       "backend",
				ModuleName:       "backend_health",
				OwnedPaths:       []string{"server/routes/health.js"},
				Responsibilities: []string{"Expose a health endpoint"},
				CoderFocus:       []string{"Keep the route stable"},
				TesterFocus:      []string{"Validate the response payload"},
			},
		},
		SharedContract: semanticSharedContract{
			APIPrefix:               "/api",
			FrontendBaseURLStrategy: "Use a relative path strategy when a backend exists",
			MergeRules:              []string{"Do not edit files outside owned_paths"},
			IntegrationChecks:       []string{"Frontend and backend must agree on route naming"},
		},
	}

	err := validateSemanticSplitPlan(schema.RunDeliveryConfig{BackendModuleCount: 1}, plan)
	if err == nil || !strings.Contains(err.Error(), `invalid frontend owned_path "*.html"`) {
		t.Fatalf("validateSemanticSplitPlan() error = %v, want wildcard frontend owned_path validation error", err)
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
