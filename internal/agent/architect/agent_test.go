package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
)

type testArtifactStore struct {
	files  map[string]string
	writes map[string]string
}

func (s *testArtifactStore) Read(_ context.Context, uri string) ([]byte, error) {
	return []byte(s.files[uri]), nil
}

func (s *testArtifactStore) Write(_ context.Context, uri string, content []byte) error {
	if s.writes == nil {
		s.writes = make(map[string]string)
	}
	s.writes[uri] = string(content)
	return nil
}

type testLLM struct {
	prompt string
	reply  string
}

func (l *testLLM) Complete(_ context.Context, prompt string) (string, error) {
	l.prompt = prompt
	return l.reply, nil
}

type mergeRunner struct {
	report  mergeCodeReport
	workDir string
	prompt  string
	err     error
}

func (r *mergeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
	r.workDir = req.WorkDir
	r.prompt = req.Prompt
	if r.err != nil {
		return common.OpenCodeResult{ExitCode: 1}, r.err
	}
	if err := os.MkdirAll(filepath.Join(req.WorkDir, ".devflow"), 0o755); err != nil {
		return common.OpenCodeResult{}, err
	}
	raw, err := json.Marshal(r.report)
	if err != nil {
		return common.OpenCodeResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, ".devflow", "result.json"), raw, 0o644); err != nil {
		return common.OpenCodeResult{}, err
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

type testCodeRunner struct {
	report  architectTestCodeReport
	workDir string
	prompt  string
	err     error
}

type architectTestGitManager struct {
	results  []common.CommandResult
	errors   []error
	commands []string
	workdirs []string
}

func (m *architectTestGitManager) CreateCoderWorktree(ctx context.Context, req common.CreateCoderWorktreeRequest) (common.Worktree, error) {
	return common.Worktree{}, fmt.Errorf("not used")
}

func (m *architectTestGitManager) HasChanges(ctx context.Context, worktree string) (bool, error) {
	return false, fmt.Errorf("not used")
}

func (m *architectTestGitManager) RunTestCommand(ctx context.Context, worktree string, command string) (common.CommandResult, error) {
	m.workdirs = append(m.workdirs, worktree)
	m.commands = append(m.commands, command)
	index := len(m.commands) - 1

	var result common.CommandResult
	if index < len(m.results) {
		result = m.results[index]
	}

	var err error
	if index < len(m.errors) {
		err = m.errors[index]
	}

	return result, err
}

func (m *architectTestGitManager) CommitAll(ctx context.Context, worktree string, message string) (common.CommitResult, error) {
	return common.CommitResult{}, fmt.Errorf("not used")
}

type trapOpenCodeRunner struct{}

func (trapOpenCodeRunner) Run(context.Context, common.OpenCodeRequest) (common.OpenCodeResult, error) {
	return common.OpenCodeResult{}, fmt.Errorf("OpenCode must not be called on default global test path")
}

func (r *testCodeRunner) Run(_ context.Context, req common.OpenCodeRequest) (common.OpenCodeResult, error) {
	r.workDir = req.WorkDir
	r.prompt = req.Prompt
	if r.err != nil {
		return common.OpenCodeResult{ExitCode: 1}, r.err
	}
	if err := os.MkdirAll(filepath.Join(req.WorkDir, ".devflow"), 0o755); err != nil {
		return common.OpenCodeResult{}, err
	}
	raw, err := json.Marshal(r.report)
	if err != nil {
		return common.OpenCodeResult{}, err
	}
	if err := os.WriteFile(filepath.Join(req.WorkDir, ".devflow", "result.json"), raw, 0o644); err != nil {
		return common.OpenCodeResult{}, err
	}
	return common.OpenCodeResult{ExitCode: 0, Duration: time.Millisecond}, nil
}

func TestExecuteArchitectReplanUsesPreviousDesignFromHistory(t *testing.T) {
	prdURI := "projects/run1/agents/pm01/artifacts/prd/plan_v2.md"
	reviewURI := "projects/run1/agents/pm01/artifacts/review/replan_instruction.md"
	previousDesignURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	store := &testArtifactStore{
		files: map[string]string{
			prdURI:            "# Product Plan\n\nAdd pause and resume support.\n",
			reviewURI:         "# Review\n\nClarify state transitions and recovery flow.\n",
			previousDesignURI: "# Architecture v1\n\nExisting modules: GameLoop and Renderer.\n",
		},
	}
	llmClient := &testLLM{
		reply: `{"summary":"replanned architecture","artifact_outputs":[{"type":"design","filename":"architecture_v2.md","content":"# Architecture v2\n\nUpdated design.\n"}],"control":[]}`,
	}
	agent := &Agent{
		agentID:       "architect01",
		runID:         "run1",
		artifactStore: store,
		llmClient:     llmClient,
		taskHistory: []core.AgentTaskHistory{
			{
				RunID:              "run1",
				TaskID:             "task_architecture_generation",
				AgentID:            "architect01",
				Op:                 "architecture_generation",
				Status:             core.TaskStatusDone,
				OutputArtifactURIs: []string{previousDesignURI},
			},
		},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_replan",
		AgentID:      "architect01",
		Op:           core.TaskOpReplan,
		ArtifactURIs: []string{prdURI, reviewURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	outputURI := "projects/run1/agents/architect01/artifacts/design/architecture_v2.md"
	if got := feedback.ArtifactURIs[0]; got != outputURI {
		t.Fatalf("feedback output = %q, want %q", got, outputURI)
	}
	if !strings.Contains(llmClient.prompt, "Add pause and resume support.") {
		t.Fatalf("prompt missing latest prd:\n%s", llmClient.prompt)
	}
	if !strings.Contains(llmClient.prompt, "Clarify state transitions and recovery flow.") {
		t.Fatalf("prompt missing review:\n%s", llmClient.prompt)
	}
	if !strings.Contains(llmClient.prompt, "Existing modules: GameLoop and Renderer.") {
		t.Fatalf("prompt missing previous design from history:\n%s", llmClient.prompt)
	}
	written := store.writes[outputURI]
	if !strings.Contains(written, "# Document Basis") || !strings.Contains(written, previousDesignURI) {
		t.Fatalf("written output missing document basis:\n%s", written)
	}
	if got, want := len(agent.taskHistory), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
}

func TestParseSplitRunConfigSupportsGitAndMaxAgentFields(t *testing.T) {
	cfg, err := parseSplitRunConfig(`{
		"max_coder_agents": 2,
		"max_tester_agents": 2,
		"git": {"main_branch": "develop"}
	}`)
	if err != nil {
		t.Fatalf("parseSplitRunConfig returned error: %v", err)
	}
	if cfg.MainBranch != "develop" {
		t.Fatalf("MainBranch = %q, want develop", cfg.MainBranch)
	}
	if cfg.CoderAgents != 2 || cfg.TesterAgents != 2 {
		t.Fatalf("agent counts = coder:%d tester:%d, want 2/2", cfg.CoderAgents, cfg.TesterAgents)
	}
	if cfg.MaxModules != 2 {
		t.Fatalf("MaxModules = %d, want 2", cfg.MaxModules)
	}
}

func TestPickGlobalVerifyCommandsConfigOverrideWins(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := splitRunConfig{GlobalVerifyCommands: []string{" npm test ", "npm run build", "npm test"}}
	got := pickGlobalVerifyCommands(cfg, "# Design\n\n## Global Verification Strategy\n\n- go test ./...\n", repoDir, nil)
	if want := []string{"npm test", "npm run build"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestPickGlobalVerifyCommandsStrategyWinsOverRepo(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	design := "# Design\n\n## Global Verification Strategy\n\n- npm test\n- npm run build\n"
	got := pickGlobalVerifyCommands(splitRunConfig{}, design, repoDir, nil)
	if want := []string{"npm test", "npm run build"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestDetectGlobalVerifyCommandsFromPackageJSON(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"scripts":{"test":"node --test","build":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := detectGlobalVerifyCommandsFromRepo(repoDir)
	if want := []string{"npm test", "npm run build"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestArchitectureRecipeRequiresGlobalVerificationStrategy(t *testing.T) {
	recipe, err := agentengine.GetRecipe("architecture_generation")
	if err != nil {
		t.Fatalf("GetRecipe() error = %v", err)
	}
	if !strings.Contains(recipe.UserInstruction, "Global Verification Strategy") {
		t.Fatalf("architecture recipe missing Global Verification Strategy instruction: %s", recipe.UserInstruction)
	}
}

func TestExecuteSplitModuleWritesPairedControls(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	configURI := "projects/run1/system/run_delivery_config.json"
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nSplit into two modules.\n\n## Global Verification Strategy\n\n- npm test\n- npm run build\n",
			configURI: `{
				"max_coder_agents": 2,
				"max_tester_agents": 2,
				"git": {"main_branch": "main"}
			}`,
		},
	}
	agent := &Agent{
		agentID:       "architect01",
		runID:         "run1",
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_split",
		AgentID:      "architect01",
		Op:           core.TaskOpSplitModule,
		ArtifactURIs: []string{designURI, configURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := len(feedback.Control), 4; got != want {
		t.Fatalf("control count = %d, want %d", got, want)
	}
	mainBranchURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	if !strings.Contains(store.writes[mainBranchURI], `"kind": "main_branch"`) {
		t.Fatalf("main branch artifact missing kind:\n%s", store.writes[mainBranchURI])
	}
	if !strings.Contains(store.writes[mainBranchURI], `"global_verify_commands": [`) || !strings.Contains(store.writes[mainBranchURI], `"test_command": "node --test"`) {
		t.Fatalf("main branch artifact missing global verify commands:\n%s", store.writes[mainBranchURI])
	}
	planURI := "projects/run1/agents/architect01/artifacts/module/module_plan_v1.json"
	if !strings.Contains(store.writes[planURI], `"official_global_verify_commands": [`) || !strings.Contains(store.writes[planURI], `"npm run build"`) {
		t.Fatalf("module plan missing official global verify commands:\n%s", store.writes[planURI])
	}

	var coderControl core.Control
	var testerControl core.Control
	for _, control := range feedback.Control {
		if control.Type == core.ControlTypeNewCoder && coderControl.AgentName == "" {
			coderControl = control
		}
		if control.Type == core.ControlTypeNewTester && testerControl.AgentName == "" {
			testerControl = control
		}
	}
	if len(coderControl.ArtifactURIs) != 4 || coderControl.ArtifactURIs[1] != mainBranchURI {
		t.Fatalf("coder control artifacts = %v, want task + main branch + contract + seed tests", coderControl.ArtifactURIs)
	}
	if len(testerControl.ArtifactURIs) != 4 || testerControl.ArtifactURIs[1] != coderControl.ArtifactURIs[0] {
		t.Fatalf("tester control artifacts = %v, want tester task + paired coder task + contract + seed tests", testerControl.ArtifactURIs)
	}
	contractURI := coderControl.ArtifactURIs[2]
	seedURI := coderControl.ArtifactURIs[3]
	if contractURI != testerControl.ArtifactURIs[2] || seedURI != testerControl.ArtifactURIs[3] {
		t.Fatalf("coder/tester contract+seed uris differ: coder=%v tester=%v", coderControl.ArtifactURIs, testerControl.ArtifactURIs)
	}
	if !strings.Contains(contractURI, "/artifacts/contracts/") || !strings.Contains(seedURI, "/artifacts/seed_tests/") {
		t.Fatalf("contract/seed uris = %q / %q, want contract and seed_tests artifact paths", contractURI, seedURI)
	}
	var contract map[string]any
	if err := json.Unmarshal([]byte(store.writes[contractURI]), &contract); err != nil {
		t.Fatalf("contract artifact is not valid json: %v\n%s", err, store.writes[contractURI])
	}
	if contract["kind"] != "module_contract" {
		t.Fatalf("contract kind = %v, want module_contract", contract["kind"])
	}
	if contract["module_id"] == "" || contract["official_seed_test_command"] == "" {
		t.Fatalf("contract missing module_id or official_seed_test_command:\n%s", store.writes[contractURI])
	}
	var seed map[string]any
	if err := json.Unmarshal([]byte(store.writes[seedURI]), &seed); err != nil {
		t.Fatalf("seed artifact is not valid json: %v\n%s", err, store.writes[seedURI])
	}
	if seed["kind"] != "seed_tests" || seed["test_command"] != contract["official_seed_test_command"] {
		t.Fatalf("seed artifact = %s\ncontract = %s", store.writes[seedURI], store.writes[contractURI])
	}
	testerTask := store.writes[testerControl.ArtifactURIs[0]]
	if !strings.Contains(testerTask, "- paired_coder_agent: "+coderControl.AgentName) {
		t.Fatalf("tester task missing pairing metadata:\n%s", testerTask)
	}
	if !strings.Contains(testerTask, "- module_task_uri: "+coderControl.ArtifactURIs[0]) {
		t.Fatalf("tester task missing coder task uri:\n%s", testerTask)
	}
	if !strings.Contains(store.writes[coderControl.ArtifactURIs[0]], "- module_contract_uri: "+contractURI) ||
		!strings.Contains(store.writes[coderControl.ArtifactURIs[0]], "- seed_tests_uri: "+seedURI) {
		t.Fatalf("coder task missing contract/seed uris:\n%s", store.writes[coderControl.ArtifactURIs[0]])
	}
	if !strings.Contains(testerTask, "- module_contract_uri: "+contractURI) ||
		!strings.Contains(testerTask, "- seed_tests_uri: "+seedURI) {
		t.Fatalf("tester task missing contract/seed uris:\n%s", testerTask)
	}
}

func TestFrontendWebNormalizeSplitModulesDefaultsDeliveryRole(t *testing.T) {
	cfg := splitRunConfig{
		MaxModules:      2,
		AgentNamePrefix: map[string]string{"coder": "coder", "tester": "tester"},
	}
	modules := normalizeSplitModules(cfg, []splitModuleDoc{
		{Name: "snake_core"},
		{Name: "snake_ui"},
	})
	if got, want := len(modules), 2; got != want {
		t.Fatalf("module count = %d, want %d", got, want)
	}
	for _, module := range modules {
		if module.DeliveryProfile != defaultDeliveryProfile {
			t.Fatalf("module %s delivery_profile = %q, want %q", module.ID, module.DeliveryProfile, defaultDeliveryProfile)
		}
	}
	if modules[0].DeliveryRole == deliveryRoleFrontendWebApp {
		t.Fatalf("first module should not automatically own frontend entry: %+v", modules[0])
	}
	if modules[1].DeliveryRole != deliveryRoleFrontendWebApp {
		t.Fatalf("last module delivery_role = %q, want %q", modules[1].DeliveryRole, deliveryRoleFrontendWebApp)
	}
}

func TestFrontendWebNormalizeSplitModulesDemotesDuplicateEntryOwners(t *testing.T) {
	cfg := splitRunConfig{
		MaxModules:      2,
		AgentNamePrefix: map[string]string{"coder": "coder", "tester": "tester"},
	}
	modules := normalizeSplitModules(cfg, []splitModuleDoc{
		{
			Name:           "game_engine_input_logic",
			DeliveryRole:   " frontend_web_app ",
			Responsibility: "Own game rules and input logic.",
		},
		{
			Name:           "app_shell_ui_rendering",
			DeliveryRole:   "FRONTEND_WEB_APP",
			Responsibility: "Own the app shell, renderer, and browser-runnable entry.",
		},
	})

	entryOwners := 0
	for _, module := range modules {
		if module.DeliveryProfile != defaultDeliveryProfile {
			t.Fatalf("module %s delivery_profile = %q, want %q", module.Name, module.DeliveryProfile, defaultDeliveryProfile)
		}
		if module.DeliveryRole == deliveryRoleFrontendWebApp {
			entryOwners++
			if module.Name != "app_shell_ui_rendering" {
				t.Fatalf("entry owner = %s, want app_shell_ui_rendering", module.Name)
			}
		}
		if module.Name == "game_engine_input_logic" && module.DeliveryRole != "" {
			t.Fatalf("game_engine_input_logic delivery_role = %q, want empty", module.DeliveryRole)
		}
	}
	if entryOwners != 1 {
		t.Fatalf("entry owner count = %d, want 1: %+v", entryOwners, modules)
	}
}

func TestFrontendWebNormalizeSplitModulesChoosesSingleBestEntryOwner(t *testing.T) {
	cfg := splitRunConfig{
		MaxModules:      2,
		AgentNamePrefix: map[string]string{"coder": "coder", "tester": "tester"},
	}
	modules := normalizeSplitModules(cfg, []splitModuleDoc{
		{
			Name:           "game_engine_input_logic",
			Responsibility: "Own game rules and input logic.",
		},
		{
			Name:           "app_shell_ui_rendering",
			Responsibility: "Own app shell UI rendering and the browser-runnable entry.",
			Deliverables:   []string{"index.html integration", "main.js renderer bootstrap"},
		},
	})
	if modules[1].DeliveryRole != deliveryRoleFrontendWebApp {
		t.Fatalf("app shell delivery_role = %q, want %q", modules[1].DeliveryRole, deliveryRoleFrontendWebApp)
	}
	if modules[0].DeliveryRole != "" {
		t.Fatalf("game engine delivery_role = %q, want empty", modules[0].DeliveryRole)
	}

	fallback := normalizeSplitModules(cfg, []splitModuleDoc{
		{Name: "physics"},
		{Name: "rules"},
	})
	if fallback[1].DeliveryRole != deliveryRoleFrontendWebApp {
		t.Fatalf("fallback last module delivery_role = %q, want %q", fallback[1].DeliveryRole, deliveryRoleFrontendWebApp)
	}
	if fallback[0].DeliveryRole != "" {
		t.Fatalf("fallback first module delivery_role = %q, want empty", fallback[0].DeliveryRole)
	}
}

func TestCoderTaskForNonEntryModuleForbidsEntryFiles(t *testing.T) {
	module := splitModuleDoc{
		ID:              "module01",
		Name:            "game_engine_input_logic",
		Coder:           "coder01",
		Tester:          "tester01",
		DeliveryProfile: defaultDeliveryProfile,
		Responsibility:  "Implement game logic.",
		Deliverables:    []string{"src/game_engine_input_logic.js"},
	}
	task := buildCoderModuleTask(module, "main", "design.md", "# Design", "main.md", "contract.json", "seed.json")

	for _, want := range []string{
		"This is not the frontend_web_app entry module",
		"Do not modify index.html, README.md, or package.json",
		"Implement only the module-owned src files declared by the contract",
	} {
		if !strings.Contains(task, want) {
			t.Fatalf("coder task missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, "This is the frontend_web delivery module") {
		t.Fatalf("non-entry coder task contains entry-module instruction:\n%s", task)
	}
}

func TestFrontendWebModuleContractSeparatesDeliveryFiles(t *testing.T) {
	ordinary := splitModuleDoc{ID: "module01", Name: "snake_core", DeliveryProfile: defaultDeliveryProfile}
	delivery := splitModuleDoc{ID: "module02", Name: "snake_ui", DeliveryProfile: defaultDeliveryProfile, DeliveryRole: deliveryRoleFrontendWebApp}

	var ordinaryContract map[string]any
	if err := json.Unmarshal([]byte(buildModuleContractArtifact(ordinary, splitRunConfig{MainBranch: "main"}, "design.md", "# Design")), &ordinaryContract); err != nil {
		t.Fatalf("ordinary contract json: %v", err)
	}
	if _, ok := ordinaryContract["delivery_profile"]; ok {
		t.Fatalf("ordinary contract should not require frontend delivery files:\n%v", ordinaryContract)
	}
	if got := strings.Join(jsonStringSlice(ordinaryContract["allowed_files"]), ","); got != "src/snake_core.js" {
		t.Fatalf("ordinary allowed_files = %q, want src/snake_core.js", got)
	}
	for _, forbidden := range []string{"index.html", "README.md", "package.json"} {
		if !containsString(jsonStringSlice(ordinaryContract["forbidden_files"]), forbidden) {
			t.Fatalf("ordinary forbidden_files missing %s: %v", forbidden, ordinaryContract["forbidden_files"])
		}
	}

	var deliveryContract map[string]any
	if err := json.Unmarshal([]byte(buildModuleContractArtifact(delivery, splitRunConfig{MainBranch: "main"}, "design.md", "# Design")), &deliveryContract); err != nil {
		t.Fatalf("delivery contract json: %v", err)
	}
	if deliveryContract["delivery_profile"] != defaultDeliveryProfile || deliveryContract["delivery_role"] != deliveryRoleFrontendWebApp {
		t.Fatalf("delivery contract missing frontend metadata:\n%v", deliveryContract)
	}
	for _, allowed := range []string{"index.html", "README.md", "package.json", "src/**"} {
		if !containsString(jsonStringSlice(deliveryContract["allowed_files"]), allowed) {
			t.Fatalf("delivery allowed_files missing %s: %v", allowed, deliveryContract["allowed_files"])
		}
	}
	for _, required := range []string{"index.html", "README.md"} {
		if !containsString(jsonStringSlice(deliveryContract["required_files"]), required) {
			t.Fatalf("delivery required_files missing %s: %v", required, deliveryContract["required_files"])
		}
	}
}

func TestFrontendWebSeedTestsRequireBrowserEntry(t *testing.T) {
	module := splitModuleDoc{ID: "module02", Name: "snake_ui", DeliveryProfile: defaultDeliveryProfile, DeliveryRole: deliveryRoleFrontendWebApp}
	var seed common.TestFileBundle
	if err := json.Unmarshal([]byte(buildSeedTestsArtifact(module, "contract.json")), &seed); err != nil {
		t.Fatalf("seed json: %v", err)
	}
	if got, want := seed.TestCommand, "node test/module02.seed.test.js"; got != want {
		t.Fatalf("seed command = %q, want %q", got, want)
	}
	content := seed.TestFiles[0].Content
	for _, want := range []string{"fs.existsSync('index.html')", "fs.existsSync('README.md')", "fs.existsSync('src')", "script|module|src"} {
		if !strings.Contains(content, want) {
			t.Fatalf("delivery seed test missing %q:\n%s", want, content)
		}
	}
}

func TestExecuteMergeCodeWritesMergedArtifactOnLLMFallbackSuccess(t *testing.T) {
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/coder01_task.md"
	leftURI := "projects/run1/agents/coder01/artifacts/branches/coder01_branch.md"
	rightURI := "projects/run1/agents/coder02/artifacts/branches/coder02_branch.md"
	repoDir := t.TempDir()
	gitOutputForArchitectTest(t, repoDir, "init", "-b", "main")
	gitOutputForArchitectTest(t, repoDir, "config", "user.name", "DevFlow Test")
	gitOutputForArchitectTest(t, repoDir, "config", "user.email", "devflow@example.local")
	writeRepoFile(t, repoDir, "shared.txt", "base\n")
	base := commitRepoAll(t, repoDir, "base")
	leftHead := createCoderBranchFromBase(t, repoDir, "left", base, func() {
		writeRepoFile(t, repoDir, "shared.txt", "left\n")
		commitRepoAll(t, repoDir, "left change")
	})
	rightHead := createCoderBranchFromBase(t, repoDir, "right", base, func() {
		writeRepoFile(t, repoDir, "shared.txt", "right\n")
		commitRepoAll(t, repoDir, "right change")
	})
	mustCheckoutBranch(t, repoDir, "main")
	store := &testArtifactStore{
		files: map[string]string{
			mainURI:   mainBranchArtifactJSONForTest(t, filepath.ToSlash(repoDir), "main", base),
			moduleURI: "# Programmer Task: Engine\n",
			leftURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       filepath.ToSlash(repoDir),
				BaseBranch:    "main",
				BaseCommit:    base,
				Branch:        "left",
				Commit:        leftHead,
				ModuleTaskURI: moduleURI,
			}),
			rightURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{
				SchemaVersion: 1,
				Kind:          "coder_branch",
				RepoDir:       filepath.ToSlash(repoDir),
				BaseBranch:    "main",
				BaseCommit:    base,
				Branch:        "right",
				Commit:        rightHead,
				ModuleTaskURI: "projects/run1/agents/coder02/artifacts/modules/coder02_task.md",
			}),
		},
	}
	runner := &mergeRunner{
		report: mergeCodeReport{
			Status:         "passed",
			Summary:        "merged successfully",
			MergedBranches: []string{"devflow/run1/coder01/task/engine"},
			TestCommand:    "go test ./...",
			TestPassed:     true,
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		llmClient:      &testLLM{},
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, moduleURI, leftURI, rightURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := runner.workDir, filepath.FromSlash(repoDir); got != want {
		t.Fatalf("runner workdir = %q, want %q", got, want)
	}
	mergedURI := "projects/run1/agents/architect01/artifacts/code/merged_code_v1.md"
	if got := feedback.ArtifactURIs[0]; got != mergedURI {
		t.Fatalf("feedback output = %q, want %q", got, mergedURI)
	}
	if !strings.Contains(runner.prompt, moduleURI) || !strings.Contains(runner.prompt, leftURI) || !strings.Contains(runner.prompt, rightURI) {
		t.Fatalf("merge prompt missing input artifacts:\n%s", runner.prompt)
	}
	if !strings.Contains(store.writes[mergedURI], "merged successfully") {
		t.Fatalf("merged artifact missing summary:\n%s", store.writes[mergedURI])
	}
}

func TestExecuteMergeCodeFailsWhenCoderBranchRepoDiffersFromMainRepo(t *testing.T) {
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	moduleURI := "projects/run1/agents/architect01/artifacts/modules/coder01_task.md"
	branchURI := "projects/run1/agents/coder01/artifacts/branches/coder01_branch.md"
	mainRepo := filepath.ToSlash(t.TempDir())
	otherRepo := filepath.ToSlash(t.TempDir())
	store := &testArtifactStore{
		files: map[string]string{
			mainURI:   mainBranchArtifactJSONForTest(t, mainRepo, "main", "base123"),
			moduleURI: "# Programmer Task: Engine\n",
			branchURI: coderBranchArtifactJSONForTest(t, common.CoderBranchArtifact{SchemaVersion: 1, Kind: "coder_branch", RepoDir: otherRepo, BaseBranch: "main", BaseCommit: "base123", Branch: "devflow/run1/coder01/task/engine", Commit: "coder123", ModuleTaskURI: moduleURI}),
		},
	}
	runner := &mergeRunner{}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		llmClient:      &testLLM{},
		openCodeRunner: runner,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_merge",
		AgentID:      "architect01",
		Op:           core.TaskOpMergeCode,
		ArtifactURIs: []string{mainURI, moduleURI, branchURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	if runner.workDir != "" {
		t.Fatalf("runner should not execute when repo validation fails, got workdir %q", runner.workDir)
	}
	reportURI := "projects/run1/agents/architect01/artifacts/merge_reports/merge_code_report.md"
	if got := feedback.ArtifactURIs[0]; got != reportURI {
		t.Fatalf("feedback output = %q, want %q", got, reportURI)
	}
	report := store.writes[reportURI]
	for _, want := range []string{"coder branch repo must match main repo", filepath.FromSlash(mainRepo), filepath.FromSlash(otherRepo)} {
		if !strings.Contains(report, want) {
			t.Fatalf("failure report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteGlobalTestCodeFailsWhenMainBranchArtifactMissing(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nGlobal invariants.\n",
		},
	}
	agent := &Agent{
		runID:         "run1",
		agentID:       "architect01",
		artifactStore: store,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	if !strings.Contains(report, "main branch artifact missing") {
		t.Fatalf("failure report missing expected reason:\n%s", report)
	}
}

func TestExecuteGlobalTestCodeRunsGlobalVerifyCommands(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := filepath.ToSlash(t.TempDir())
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nGlobal invariants.\n",
			mainURI:   mainBranchArtifactJSONForTest(t, repoDir, "main", "base123", "npm test", "npm run build"),
		},
	}
	gitManager := &architectTestGitManager{
		results: []common.CommandResult{
			{ExitCode: 0, Duration: time.Millisecond},
			{ExitCode: 0, Duration: time.Millisecond},
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: trapOpenCodeRunner{},
		gitManager:     gitManager,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeOK {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeOK)
	}
	if got, want := strings.Join(gitManager.commands, ","), "npm test,npm run build"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if got, want := gitManager.workdirs[0], filepath.FromSlash(repoDir); got != want {
		t.Fatalf("workdir = %q, want %q", got, want)
	}
}

func TestExecuteGlobalTestCodeCommandFailureIsBug(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	mergedURI := "projects/run1/agents/architect01/artifacts/code/merged_code_v1.md"
	repoDir := filepath.ToSlash(t.TempDir())
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nGlobal invariants.\n",
			mainURI:   mainBranchArtifactJSONForTest(t, repoDir, "main", "base123", "go test ./..."),
			mergedURI: "# Merged Code\n\nSummary.\n",
		},
	}
	gitManager := &architectTestGitManager{
		results: []common.CommandResult{
			{ExitCode: 1, Stderr: "tests failed"},
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: trapOpenCodeRunner{},
		gitManager:     gitManager,
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI, mainURI, mergedURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeBug {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeBug)
	}
	reportURI := "projects/run1/agents/architect01/artifacts/test_reports/architect_test_code_report.md"
	if got := feedback.ArtifactURIs[0]; got != reportURI {
		t.Fatalf("feedback output = %q, want %q", got, reportURI)
	}
	report := store.writes[reportURI]
	for _, want := range []string{"global verification command failed", "tests failed", designURI, mainURI} {
		if !strings.Contains(report, want) {
			t.Fatalf("failure report missing %q:\n%s", want, report)
		}
	}
}

func TestExecuteGlobalTestCodeFailsWithoutCommand(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := filepath.ToSlash(t.TempDir())
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nGlobal invariants.\n",
			mainURI:   mainBranchArtifactJSONForTest(t, repoDir, "main", "base123"),
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: trapOpenCodeRunner{},
		gitManager:     &architectTestGitManager{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
}

func TestExecuteGlobalTestCodeFailsWhenRepoMissing(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	missingRepo := filepath.Join(t.TempDir(), "missing")
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nGlobal invariants.\n",
			mainURI:   mainBranchArtifactJSONForTest(t, filepath.ToSlash(missingRepo), "main", "base123", "go test ./..."),
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: trapOpenCodeRunner{},
		gitManager:     &architectTestGitManager{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
}

func TestFrontendWebGlobalTestFailsWhenDeliveryEntryMissing(t *testing.T) {
	designURI := "projects/run1/agents/architect01/artifacts/design/architecture_v1.md"
	mainURI := "projects/run1/agents/architect01/artifacts/branches/main_branch.md"
	repoDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoDir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := &testArtifactStore{
		files: map[string]string{
			designURI: "# Architecture\n\nFrontend web delivery.\n",
			mainURI: frontendWebMainBranchArtifactJSONForTest(t, common.BranchArtifact{
				SchemaVersion:        1,
				Kind:                 "main_branch",
				RepoDir:              filepath.ToSlash(repoDir),
				Branch:               "main",
				Commit:               "base123",
				DeliveryProfile:      defaultDeliveryProfile,
				RequiredFiles:        []string{"index.html", "README.md", "src/"},
				TestCommand:          "node --test",
				GlobalVerifyCommands: []string{"node --test"},
			}),
		},
	}
	agent := &Agent{
		runID:          "run1",
		agentID:        "architect01",
		artifactStore:  store,
		openCodeRunner: trapOpenCodeRunner{},
		gitManager:     &architectTestGitManager{},
	}

	feedback, err := agent.Execute(context.Background(), core.TaskMetaData{
		Direction:    core.TaskDirectionDispatch,
		RunID:        "run1",
		TaskID:       "task_global_test",
		AgentID:      "architect01",
		Op:           core.TaskOpTestCode,
		ArtifactURIs: []string{designURI, mainURI},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if feedback.Result != core.TaskResultCodeFail {
		t.Fatalf("feedback result = %s, want %s", feedback.Result, core.TaskResultCodeFail)
	}
	report := store.writes[feedback.ArtifactURIs[0]]
	if !strings.Contains(report, "frontend_web delivery missing index.html") {
		t.Fatalf("failure report missing frontend_web reason:\n%s", report)
	}
}

func mainBranchArtifactJSONForTest(t *testing.T, repoDir, branch, commit string, commands ...string) string {
	t.Helper()
	commands = normalizeGlobalVerifyCommands(commands)
	testCommand := ""
	if len(commands) > 0 {
		testCommand = commands[0]
	}
	raw, err := common.MarshalJSONArtifact(common.BranchArtifact{
		SchemaVersion:        1,
		Kind:                 "main_branch",
		RepoDir:              repoDir,
		Branch:               branch,
		Commit:               commit,
		TestCommand:          testCommand,
		GlobalVerifyCommands: commands,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func frontendWebMainBranchArtifactJSONForTest(t *testing.T, artifact common.BranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func coderBranchArtifactJSONForTest(t *testing.T, artifact common.CoderBranchArtifact) string {
	t.Helper()
	raw, err := common.MarshalJSONArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var _ common.OpenCodeRunner = (*mergeRunner)(nil)
var _ common.OpenCodeRunner = (*testCodeRunner)(nil)
