package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"devflow/internal/agent/common"
	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

const splitModuleStrategy = "one_coder_one_tester_per_module"
const defaultDeliveryProfile = "frontend_web"
const deliveryRoleFrontendWebApp = "frontend_web_app"

type splitRunConfig struct {
	MainBranch               string            `json:"main_branch"`
	CoderAgents              int               `json:"coder_agents"`
	TesterAgents             int               `json:"tester_agents"`
	MaxModules               int               `json:"max_modules"`
	AssignmentStrategy       string            `json:"assignment_strategy"`
	AgentNamePrefix          map[string]string `json:"agent_name_prefix"`
	MaxCoderAgents           int               `json:"max_coder_agents"`
	MaxTesterAgents          int               `json:"max_tester_agents"`
	GlobalVerifyCommands     []string          `json:"global_verify_commands,omitempty"`
	GlobalTestTimeoutSeconds int               `json:"global_test_timeout_seconds,omitempty"`
	Git                      core.GitRunConfig `json:"git"`
}

type splitModuleDoc struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Coder           string   `json:"coder"`
	Tester          string   `json:"tester"`
	DeliveryProfile string   `json:"delivery_profile,omitempty"`
	DeliveryRole    string   `json:"delivery_role,omitempty"`
	Responsibility  string   `json:"responsibility,omitempty"`
	Deliverables    []string `json:"deliverables,omitempty"`
	TestFocus       []string `json:"test_focus,omitempty"`
}

func (a *Agent) executeSplitModule(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		if strictDeliveryEnabled() {
			return core.TaskMetaData{}, fmt.Errorf("split_module file fallback disabled in strict delivery mode")
		}
		return a.executeSplitModuleFileFallback(ctx, task)
	}

	designURI, design, configRaw, err := a.resolveSplitModuleInputs(ctx, task.ArtifactURIs)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	cfg, err := parseSplitRunConfig(configRaw)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	repoInfo := a.ensureMainBranchRepo(ctx, cfg.MainBranch)
	mainBranchURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "branches", "main_branch.md")

	modules, source := a.planSplitModules(ctx, cfg, design)
	if strictDeliveryEnabled() && source == "system_fallback" {
		return core.TaskMetaData{}, fmt.Errorf("split_module llm fallback disabled in strict delivery mode")
	}
	verifyCommands := frontendWebGlobalVerifyCommands(pickGlobalVerifyCommands(cfg, design, repoInfo.RepositoryPath, modules), modules)
	if err := a.artifactStore.Write(ctx, mainBranchURI, []byte(buildMainBranchArtifact(a.runID, repoInfo, []string{designURI}, verifyCommands))); err != nil {
		return core.TaskMetaData{}, err
	}
	planURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "module", "module_plan_v1.json")
	planRaw, _ := json.MarshalIndent(map[string]any{
		"modules":                         modules,
		"source":                          source,
		"main_branch_artifact":            mainBranchURI,
		"official_global_verify_commands": verifyCommands,
	}, "", "  ")
	if err := a.artifactStore.Write(ctx, planURI, append(planRaw, '\n')); err != nil {
		return core.TaskMetaData{}, err
	}

	controls := make([]core.Control, 0, len(modules)*2)
	for _, module := range modules {
		coderTaskURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "modules", module.Coder+"_task.md")
		testerTaskURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "tests", module.Tester+"_task.md")
		contractURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "contracts", module.ID+"_contract.json")
		seedTestsURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "seed_tests", module.ID+"_seed_tests.json")
		if err := a.artifactStore.Write(ctx, contractURI, []byte(buildModuleContractArtifact(module, cfg, designURI, design))); err != nil {
			return core.TaskMetaData{}, err
		}
		if err := a.artifactStore.Write(ctx, seedTestsURI, []byte(buildSeedTestsArtifact(module, contractURI))); err != nil {
			return core.TaskMetaData{}, err
		}
		if err := a.artifactStore.Write(ctx, coderTaskURI, []byte(buildCoderModuleTask(module, cfg.MainBranch, designURI, design, mainBranchURI, contractURI, seedTestsURI))); err != nil {
			return core.TaskMetaData{}, err
		}
		if err := a.artifactStore.Write(ctx, testerTaskURI, []byte(buildTesterModuleTask(module, cfg.MainBranch, coderTaskURI, designURI, design, contractURI, seedTestsURI))); err != nil {
			return core.TaskMetaData{}, err
		}
		controls = append(controls,
			core.Control{Type: core.ControlTypeNewCoder, AgentName: module.Coder, ArtifactURIs: []string{coderTaskURI, mainBranchURI, contractURI, seedTestsURI}},
			core.Control{Type: core.ControlTypeNewTester, AgentName: module.Tester, ArtifactURIs: []string{testerTaskURI, coderTaskURI, contractURI, seedTestsURI}},
		)
	}

	a.logStep(fmt.Sprintf("split_module artifacts written: modules=%d source=%s", len(modules), source))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{planURI, mainBranchURI},
		Result:       core.TaskResultCodeOK,
		Control:      controls,
	}, nil
}

func (a *Agent) resolveSplitModuleInputs(ctx context.Context, uris []string) (string, string, string, error) {
	var designURI string
	var design string
	var configRaw string
	for _, uri := range uris {
		normalized := filepath.ToSlash(strings.TrimSpace(uri))
		if normalized == "" {
			continue
		}
		content, err := a.artifactStore.Read(ctx, normalized)
		if err != nil {
			return "", "", "", fmt.Errorf("read artifact %s: %w", normalized, err)
		}
		if agentengine.InferArtifactKind(normalized) == "design" {
			designURI = normalized
			design = string(content)
			continue
		}
		lower := strings.ToLower(normalized)
		if strings.Contains(lower, "run_config") || strings.Contains(lower, "delivery_config") || strings.Contains(lower, "/config/") || strings.Contains(lower, "/system/") {
			configRaw = string(content)
		}
	}
	if strings.TrimSpace(design) == "" {
		return "", "", "", fmt.Errorf("split_module requires architecture design artifact")
	}
	if strings.TrimSpace(configRaw) == "" {
		return "", "", "", fmt.Errorf("split_module requires run config artifact")
	}
	return designURI, design, configRaw, nil
}

func parseSplitRunConfig(raw string) (splitRunConfig, error) {
	block, err := extractSplitJSONBlock(raw)
	if err != nil {
		return splitRunConfig{}, err
	}
	var cfg splitRunConfig
	if err := json.Unmarshal([]byte(block), &cfg); err != nil {
		return splitRunConfig{}, fmt.Errorf("parse run config json: %w", err)
	}
	if strings.TrimSpace(cfg.MainBranch) == "" {
		cfg.MainBranch = strings.TrimSpace(cfg.Git.MainBranch)
	}
	if strings.TrimSpace(cfg.MainBranch) == "" {
		cfg.MainBranch = "main"
	}
	if cfg.CoderAgents <= 0 {
		cfg.CoderAgents = cfg.MaxCoderAgents
	}
	if cfg.TesterAgents <= 0 {
		cfg.TesterAgents = cfg.MaxTesterAgents
	}
	if cfg.CoderAgents <= 0 {
		cfg.CoderAgents = 1
	}
	if cfg.TesterAgents <= 0 {
		cfg.TesterAgents = cfg.CoderAgents
	}
	if cfg.TesterAgents != cfg.CoderAgents {
		return splitRunConfig{}, fmt.Errorf("tester agent count must equal coder agent count")
	}
	if cfg.MaxModules <= 0 || cfg.MaxModules > cfg.CoderAgents {
		cfg.MaxModules = cfg.CoderAgents
	}
	if strings.TrimSpace(cfg.AssignmentStrategy) == "" {
		cfg.AssignmentStrategy = splitModuleStrategy
	}
	if cfg.AssignmentStrategy != splitModuleStrategy {
		return splitRunConfig{}, fmt.Errorf("unsupported assignment_strategy %q", cfg.AssignmentStrategy)
	}
	if cfg.AgentNamePrefix == nil {
		cfg.AgentNamePrefix = map[string]string{}
	}
	if strings.TrimSpace(cfg.AgentNamePrefix["coder"]) == "" {
		cfg.AgentNamePrefix["coder"] = "coder"
	}
	if strings.TrimSpace(cfg.AgentNamePrefix["tester"]) == "" {
		cfg.AgentNamePrefix["tester"] = "tester"
	}
	return cfg, nil
}

func normalizeGlobalVerifyCommands(commands []string) []string {
	out := make([]string, 0, len(commands))
	seen := map[string]bool{}
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		out = append(out, command)
	}
	return out
}

func commandsFromGlobalVerificationStrategy(design string) []string {
	section := markdownSection(design, "Global Verification Strategy")
	if strings.TrimSpace(section) == "" {
		return nil
	}

	candidates := []string{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "`-* "))
		if looksLikeVerifyCommand(line) {
			candidates = append(candidates, line)
		}
	}
	return normalizeGlobalVerifyCommands(candidates)
}

func markdownSection(markdown string, heading string) string {
	lines := strings.Split(markdown, "\n")
	target := strings.ToLower(strings.TrimSpace(heading))
	inSection := false
	level := 0
	out := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			hashes := 0
			for hashes < len(trimmed) && trimmed[hashes] == '#' {
				hashes++
			}
			title := strings.TrimSpace(trimmed[hashes:])
			if inSection && hashes <= level {
				break
			}
			if strings.EqualFold(title, target) {
				inSection = true
				level = hashes
				continue
			}
		}
		if inSection {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func looksLikeVerifyCommand(line string) bool {
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"go test", "go build", "npm test", "npm run", "node --test", "pytest", "cargo test"} {
		if line == prefix || strings.HasPrefix(line, prefix+" ") {
			return true
		}
	}
	return false
}

func inferGlobalVerifyCommandsFromDesignAndModules(design string, modules []splitModuleDoc) []string {
	lower := strings.ToLower(design)
	switch {
	case strings.Contains(lower, "go ") || strings.Contains(lower, "golang"):
		return []string{"go test ./..."}
	case strings.Contains(lower, "node") || strings.Contains(lower, "javascript") || strings.Contains(lower, "commonjs"):
		return []string{"node --test"}
	case strings.Contains(lower, "python") || strings.Contains(lower, "pytest"):
		return []string{"pytest"}
	case strings.Contains(lower, "rust") || strings.Contains(lower, "cargo"):
		return []string{"cargo test"}
	}

	if splitModulesLookLikeNode(modules) {
		return []string{"node --test"}
	}

	return nil
}

func splitModulesLookLikeNode(modules []splitModuleDoc) bool {
	for _, module := range modules {
		text := strings.ToLower(strings.Join(append(append([]string{module.Name, module.Responsibility}, module.Deliverables...), module.TestFocus...), " "))
		if strings.Contains(text, "node") || strings.Contains(text, "javascript") || strings.Contains(text, ".js") || strings.Contains(text, "commonjs") {
			return true
		}
	}
	return false
}

func detectGlobalVerifyCommandsFromRepo(repoDir string) []string {
	if strings.TrimSpace(repoDir) == "" {
		return nil
	}

	if fileExists(filepath.Join(repoDir, "go.mod")) {
		return []string{"go test ./..."}
	}

	if fileExists(filepath.Join(repoDir, "package.json")) {
		scripts := packageJSONScripts(repoDir)
		commands := make([]string, 0, 2)
		if containsScript(scripts, "test") {
			commands = append(commands, "npm test")
		}
		if containsScript(scripts, "build") {
			commands = append(commands, "npm run build")
		}
		if len(commands) > 0 {
			return commands
		}
	}

	if fileExists(filepath.Join(repoDir, "pyproject.toml")) || fileExists(filepath.Join(repoDir, "pytest.ini")) {
		return []string{"pytest"}
	}

	if fileExists(filepath.Join(repoDir, "Cargo.toml")) {
		return []string{"cargo test"}
	}

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func packageJSONScripts(repoDir string) map[string]any {
	content, err := os.ReadFile(filepath.Join(repoDir, "package.json"))
	if err != nil {
		return nil
	}
	var payload struct {
		Scripts map[string]any `json:"scripts"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil
	}
	return payload.Scripts
}

func containsScript(scripts map[string]any, name string) bool {
	if scripts == nil {
		return false
	}
	_, ok := scripts[name]
	return ok
}

func pickGlobalVerifyCommands(cfg splitRunConfig, design string, repoDir string, modules []splitModuleDoc) []string {
	if commands := normalizeGlobalVerifyCommands(cfg.GlobalVerifyCommands); len(commands) > 0 {
		return commands
	}

	if commands := commandsFromGlobalVerificationStrategy(design); len(commands) > 0 {
		return commands
	}

	if commands := inferGlobalVerifyCommandsFromDesignAndModules(design, modules); len(commands) > 0 {
		return commands
	}

	if commands := detectGlobalVerifyCommandsFromRepo(repoDir); len(commands) > 0 {
		return commands
	}

	return nil
}

func frontendWebGlobalVerifyCommands(base []string, modules []splitModuleDoc) []string {
	commands := []string{"node --test"}
	commands = append(commands, base...)
	for _, module := range modules {
		commands = append(commands, seedTestCommand(module.ID))
	}
	return normalizeGlobalVerifyCommands(commands)
}

func extractSplitJSONBlock(raw string) (string, error) {
	re := regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")
	matches := re.FindStringSubmatch(raw)
	if len(matches) == 2 {
		return strings.TrimSpace(matches[1]), nil
	}
	trimmed := strings.TrimSpace(raw)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1]), nil
	}
	return "", fmt.Errorf("run config must contain json")
}

type mainBranchInfo struct {
	RepositoryPath string
	MainBranch     string
	Status         string
	InitialCommit  string
}

func (a *Agent) ensureMainBranchRepo(ctx context.Context, mainBranch string) mainBranchInfo {
	runRoot, err := a.runRootFromWorkspace()
	if err != nil {
		return mainBranchInfo{MainBranch: mainBranch, Status: "workspace_unavailable"}
	}
	repoPath := filepath.Join(runRoot, "project_repo")
	info := mainBranchInfo{RepositoryPath: filepath.Clean(repoPath), MainBranch: mainBranch, Status: "initialized"}
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		info.Status = "mkdir_failed"
		return info
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		_ = runGitCommand(ctx, repoPath, "checkout", mainBranch)
		commit, _ := gitCommandOutput(ctx, repoPath, "rev-parse", "HEAD")
		info.InitialCommit = strings.TrimSpace(commit)
		info.Status = "existing"
		return info
	}
	if err := runGitCommand(ctx, repoPath, "init"); err != nil {
		info.Status = "git_unavailable"
		return info
	}
	_ = runGitCommand(ctx, repoPath, "config", "user.name", "Architect Agent")
	_ = runGitCommand(ctx, repoPath, "config", "user.email", "architect-agent@example.local")
	_ = runGitCommand(ctx, repoPath, "symbolic-ref", "HEAD", "refs/heads/"+mainBranch)
	_ = os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Project Workspace\n\nInitialized by DevFlow split_module.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(".DS_Store\nnode_modules/\ndist/\nbuild/\n.env\n"), 0o644)
	_ = runGitCommand(ctx, repoPath, "add", ".")
	if err := runGitCommand(ctx, repoPath, "commit", "-m", "chore: initialize project workspace"); err != nil {
		info.Status = "initialized_without_commit"
		return info
	}
	commit, _ := gitCommandOutput(ctx, repoPath, "rev-parse", "HEAD")
	info.InitialCommit = strings.TrimSpace(commit)
	return info
}

func (a *Agent) runRootFromWorkspace() (string, error) {
	workspace := strings.TrimSpace(a.workspacePath)
	if workspace == "" {
		return "", fmt.Errorf("workspace path is required")
	}
	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(workspaceAbs)), nil
}

func runGitCommand(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitCommandOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (a *Agent) planSplitModules(ctx context.Context, cfg splitRunConfig, design string) ([]splitModuleDoc, string) {
	if llm.IsNoop(a.llmClient) {
		return fallbackSplitModules(cfg, design), "system_fallback"
	}
	prompt := buildSplitModulePrompt(cfg, design)
	a.logStep(fmt.Sprintf("split_module llm request start: prompt_chars=%d", len(prompt)))
	raw, err := a.llmClient.Complete(ctx, prompt)
	if err != nil {
		a.logStep(fmt.Sprintf("split_module llm request failed, fallback used: %v", err))
		return fallbackSplitModules(cfg, design), "system_fallback"
	}
	var output struct {
		Modules []splitModuleDoc `json:"modules"`
	}
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		a.logStep(fmt.Sprintf("split_module parse model output failed, fallback used: %v", err))
		return fallbackSplitModules(cfg, design), "system_fallback"
	}
	modules := normalizeSplitModules(cfg, output.Modules)
	if len(modules) == 0 {
		return fallbackSplitModules(cfg, design), "system_fallback"
	}
	return modules, "llm"
}

func buildSplitModulePrompt(cfg splitRunConfig, design string) string {
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are an Architect Agent. Split the project into paired coder/tester module tasks.\n\n")
	builder.WriteString("# Delivery Constraints\n")
	builder.WriteString(fmt.Sprintf("- max_modules: %d\n", cfg.MaxModules))
	builder.WriteString(fmt.Sprintf("- coder_agents: %d\n", cfg.CoderAgents))
	builder.WriteString(fmt.Sprintf("- tester_agents: %d\n", cfg.TesterAgents))
	builder.WriteString(fmt.Sprintf("- main_branch: %s\n\n", cfg.MainBranch))
	builder.WriteString(fmt.Sprintf("- default_delivery_profile: %s\n", defaultDeliveryProfile))
	builder.WriteString(fmt.Sprintf("- required_delivery_role: %s\n\n", deliveryRoleFrontendWebApp))
	builder.WriteString("# Architecture Design\n\n")
	builder.WriteString(strings.TrimSpace(design))
	builder.WriteString("\n\n# Output JSON Schema\n")
	builder.WriteString(`{"modules":[{"id":"module01","name":"short_name","coder":"coder01","tester":"tester01","delivery_profile":"frontend_web","delivery_role":"frontend_web_app","responsibility":"one paragraph","deliverables":["item"],"test_focus":["item"]}]}`)
	builder.WriteString("\n\n# Constraints\n")
	builder.WriteString("1. Return exactly one JSON object.\n")
	builder.WriteString("2. Use at most max_modules modules.\n")
	builder.WriteString("3. Every module must have exactly one coder and one tester.\n")
	builder.WriteString("4. Responsibilities must be distinct and together form a complete delivery.\n")
	builder.WriteString("5. deliverables and test_focus must be concrete.\n")
	builder.WriteString("6. Default every module to delivery_profile=frontend_web unless a later explicit delivery type is provided.\n")
	builder.WriteString("7. Exactly one module must use delivery_role=frontend_web_app and own index.html, README.md, package.json if needed.\n")
	builder.WriteString("8. All other modules must leave delivery_role empty and must not modify index.html, README.md, or package.json.\n")
	return builder.String()
}

func normalizeSplitModules(cfg splitRunConfig, modules []splitModuleDoc) []splitModuleDoc {
	if len(modules) == 0 {
		return nil
	}
	if len(modules) > cfg.MaxModules {
		modules = modules[:cfg.MaxModules]
	}
	out := make([]splitModuleDoc, 0, len(modules))
	for i, module := range modules {
		idx := i + 1
		module.ID = strings.TrimSpace(module.ID)
		if module.ID == "" {
			module.ID = fmt.Sprintf("module%02d", idx)
		}
		module.Name = safeModuleName(module.Name, idx)
		module.Coder = numberedAgentName(cfg.AgentNamePrefix["coder"], idx)
		module.Tester = numberedAgentName(cfg.AgentNamePrefix["tester"], idx)
		module.Responsibility = strings.TrimSpace(module.Responsibility)
		if module.Responsibility == "" {
			module.Responsibility = fallbackResponsibility(module.Name, idx, len(modules))
		}
		module.Deliverables = normalizeLines(module.Deliverables)
		if len(module.Deliverables) == 0 {
			module.Deliverables = fallbackDeliverables(module.Name, idx, len(modules))
		}
		module.TestFocus = normalizeLines(module.TestFocus)
		if len(module.TestFocus) == 0 {
			module.TestFocus = fallbackTestFocus(module.Name, idx, len(modules))
		}
		out = append(out, module)
	}
	return applyDeliveryDefaults(out)
}

func fallbackSplitModules(cfg splitRunConfig, design string) []splitModuleDoc {
	count := cfg.MaxModules
	if count <= 0 {
		count = 1
	}
	modules := make([]splitModuleDoc, 0, count)
	defaultNames := fallbackModuleNames(count, design)
	for i := 1; i <= count; i++ {
		name := defaultNames[i-1]
		modules = append(modules, splitModuleDoc{
			ID:             fmt.Sprintf("module%02d", i),
			Name:           name,
			Coder:          numberedAgentName(cfg.AgentNamePrefix["coder"], i),
			Tester:         numberedAgentName(cfg.AgentNamePrefix["tester"], i),
			Responsibility: fallbackResponsibility(name, i, count),
			Deliverables:   fallbackDeliverables(name, i, count),
			TestFocus:      fallbackTestFocus(name, i, count),
		})
	}
	return applyDeliveryDefaults(modules)
}

func applyDeliveryDefaults(modules []splitModuleDoc) []splitModuleDoc {
	if len(modules) == 0 {
		return modules
	}
	candidates := make([]int, 0, 1)
	for i := range modules {
		modules[i].DeliveryProfile = defaultDeliveryProfile
		modules[i].DeliveryRole = strings.TrimSpace(modules[i].DeliveryRole)
		if strings.EqualFold(modules[i].DeliveryRole, deliveryRoleFrontendWebApp) {
			modules[i].DeliveryRole = deliveryRoleFrontendWebApp
			candidates = append(candidates, i)
			continue
		}
		modules[i].DeliveryRole = ""
	}
	owner := frontendWebAppOwnerIndex(modules, candidates)
	for i := range modules {
		if i == owner {
			modules[i].DeliveryRole = deliveryRoleFrontendWebApp
		} else {
			modules[i].DeliveryRole = ""
		}
	}
	return modules
}

func frontendWebAppOwnerIndex(modules []splitModuleDoc, candidates []int) int {
	search := candidates
	if len(search) == 0 {
		search = make([]int, len(modules))
		for i := range modules {
			search[i] = i
		}
	}
	if len(search) == 0 {
		return -1
	}
	best := search[len(search)-1]
	bestScore := 0
	for _, index := range search {
		if index < 0 || index >= len(modules) {
			continue
		}
		score := frontendWebEntryScore(modules[index])
		if score > bestScore {
			best = index
			bestScore = score
		}
	}
	return best
}

func frontendWebEntryScore(module splitModuleDoc) int {
	text := splitModuleSearchText(module)
	score := 0
	keywords := []string{
		"index.html",
		"app shell",
		"shell",
		"ui",
		"renderer",
		"hud",
		"browser-runnable",
		"entry",
		"main.js",
	}
	for weight, keyword := range keywords {
		if containsEntryToken(text, keyword) {
			score += len(keyword) * (len(keywords) - weight)
		}
	}
	return score
}

func splitModuleSearchText(module splitModuleDoc) string {
	parts := []string{module.Name, module.Responsibility}
	parts = append(parts, module.Deliverables...)
	parts = append(parts, module.TestFocus...)
	return strings.ToLower(strings.Join(parts, " "))
}

func containsEntryToken(text, keyword string) bool {
	text = strings.ToLower(text)
	keyword = strings.ToLower(keyword)
	for offset := 0; offset <= len(text)-len(keyword); {
		index := strings.Index(text[offset:], keyword)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(keyword)
		if isEntryTokenBoundary(text, start-1) && isEntryTokenBoundary(text, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func isEntryTokenBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	ch := text[index]
	return !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'))
}

func fallbackModuleNames(count int, design string) []string {
	lower := strings.ToLower(design)
	if strings.Contains(lower, "snake") || strings.Contains(design, "\u8d2a\u5403\u86c7") {
		switch count {
		case 1:
			return []string{"snake_game"}
		case 2:
			return []string{"snake_core", "snake_ui"}
		default:
			return []string{"snake_core", "snake_ui", "snake_quality"}
		}
	}
	out := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		out = append(out, fmt.Sprintf("module_%02d", i))
	}
	return out
}

func fallbackResponsibility(name string, index int, count int) string {
	switch name {
	case "snake_game":
		return "Deliver the complete playable Snake game, including game loop, rendering, input, scoring, and restart flow."
	case "snake_core":
		return "Own the core Snake gameplay, including state management, movement, collision detection, food spawning, and score calculation."
	case "snake_ui":
		return "Own the game UI and interaction, including rendering, keyboard controls, start/end/restart flow, and run instructions."
	case "snake_quality":
		return "Own quality-oriented work, including tests, runtime verification, failure handling, and delivery notes."
	default:
		return fmt.Sprintf("Own module slice %d/%d and deliver one distinct, implementation-ready part of the project.", index, count)
	}
}

func fallbackDeliverables(name string, index int, count int) []string {
	switch name {
	case "snake_game":
		return []string{"Playable game code", "Run instructions", "Required assets or support files"}
	case "snake_core":
		return []string{"Core gameplay code", "State and rule implementation", "Basic verification method"}
	case "snake_ui":
		return []string{"UI and input control code", "Start/end/restart flow", "Supplemental run instructions"}
	case "snake_quality":
		return []string{"Tests or verification scripts", "Troubleshooting notes", "Delivery addendum"}
	default:
		return []string{fmt.Sprintf("Core code for module %d", index), "Required notes", "Verification method"}
	}
}

func fallbackTestFocus(name string, index int, count int) []string {
	switch name {
	case "snake_game":
		return []string{"Game starts successfully", "Keyboard controls work", "Score and game-over logic are correct"}
	case "snake_core":
		return []string{"Movement rules are correct", "Collision and growth logic are correct", "Food and score logic are correct"}
	case "snake_ui":
		return []string{"Rendering is stable", "Key response is correct", "Restart flow works"}
	case "snake_quality":
		return []string{"Runtime verification passes", "Key failure paths are covered", "Delivery notes are usable"}
	default:
		return []string{fmt.Sprintf("Verify the core responsibility of module %d/%d", index, count)}
	}
}

func safeModuleName(name string, index int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Sprintf("module_%02d", index)
	}
	return strings.ReplaceAll(name, " ", "_")
}

func normalizeLines(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func numberedAgentName(prefix string, index int) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "agent"
	}
	if strings.HasSuffix(prefix, "_") || strings.HasSuffix(prefix, "-") {
		return fmt.Sprintf("%s%02d", prefix, index)
	}
	return fmt.Sprintf("%s%02d", prefix, index)
}

func buildMainBranchArtifact(runID core.RunID, info mainBranchInfo, basis []string, verifyCommands []string) string {
	verifyCommands = normalizeGlobalVerifyCommands(verifyCommands)
	testCommand := ""
	if len(verifyCommands) > 0 {
		testCommand = verifyCommands[0]
	}
	raw, _ := common.MarshalJSONArtifact(map[string]any{
		"schema_version":         1,
		"kind":                   "main_branch",
		"run_id":                 string(runID),
		"repo_dir":               filepath.ToSlash(info.RepositoryPath),
		"branch":                 info.MainBranch,
		"commit":                 info.InitialCommit,
		"status":                 info.Status,
		"input_artifact_uris":    basis,
		"delivery_profile":       defaultDeliveryProfile,
		"required_files":         []string{"index.html", "README.md", "src/"},
		"required_behavior":      []string{"index.html loads the browser application", "page renders visible UI", "README documents how to run the app", "node --test passes"},
		"global_verify_commands": verifyCommands,
		"test_command":           testCommand,
	})
	return string(raw)
}

func buildModuleContractArtifact(module splitModuleDoc, cfg splitRunConfig, designURI string, design string) string {
	moduleName := safeModuleName(module.Name, 1)
	entryFile := "src/" + moduleName + ".js"
	apiName := publicAPIName(moduleName)
	notes := "Coder must implement only the declared API. Tester must not test APIs outside this contract."
	if strings.TrimSpace(designURI) != "" {
		notes += " Source architecture: " + strings.TrimSpace(designURI) + "."
	}
	if strings.TrimSpace(cfg.MainBranch) != "" {
		notes += " Main branch: " + strings.TrimSpace(cfg.MainBranch) + "."
	}
	if strings.TrimSpace(design) != "" {
		notes += " Module responsibility is derived from the architecture design."
	}
	payload := map[string]any{
		"schema_version":             1,
		"kind":                       "module_contract",
		"module_id":                  module.ID,
		"module_name":                moduleName,
		"language":                   "javascript",
		"module_system":              "commonjs",
		"entry_files":                []string{entryFile},
		"public_api":                 []map[string]any{{"name": apiName, "kind": "function", "input": "none", "output": "module result"}},
		"allowed_files":              []string{entryFile},
		"forbidden_files":            []string{"test/**", "index.html", "README.md", "package.json"},
		"official_seed_test_command": seedTestCommand(module.ID),
		"notes":                      notes,
	}
	if isFrontendWebDeliveryModule(module) {
		payload["delivery_profile"] = defaultDeliveryProfile
		payload["delivery_role"] = deliveryRoleFrontendWebApp
		payload["module_system"] = "browser"
		payload["entry_files"] = []string{"index.html"}
		payload["public_api"] = []map[string]any{{"name": "frontend_web_app", "kind": "browser_app", "input": "browser", "output": "visible UI"}}
		payload["allowed_files"] = []string{"index.html", "README.md", "package.json", "src/**"}
		payload["required_files"] = []string{"index.html", "README.md"}
		payload["required_behavior"] = []string{"index.html loads application code from src", "page renders visible UI", "README documents how to run the app", "node --test passes"}
		payload["forbidden_files"] = []string{"test/**"}
		payload["notes"] = notes + " This delivery module owns the browser-runnable frontend_web app entry and must integrate the final UI."
	}
	raw, _ := common.MarshalJSONArtifact(payload)
	return string(raw)
}

func buildSeedTestsArtifact(module splitModuleDoc, contractURI string) string {
	moduleName := safeModuleName(module.Name, 1)
	entryFile := "../src/" + moduleName + ".js"
	apiName := publicAPIName(moduleName)
	testPath := "test/" + module.ID + ".seed.test.js"
	var content string
	if isFrontendWebDeliveryModule(module) {
		content = fmt.Sprintf(`const assert = require('assert');
const fs = require('fs');

assert.ok(fs.existsSync('index.html'));
assert.ok(fs.existsSync('README.md'));
assert.ok(fs.existsSync('src'));
assert.ok(fs.statSync('src').isDirectory());
assert.match(fs.readFileSync('index.html', 'utf8'), /script|module|src\//i);
console.log('frontend_web seed tests passed for %s using %s');
`, module.ID, contractURI)
	} else {
		content = fmt.Sprintf(`const assert = require('assert');
const moduleApi = require('%s');
assert.strictEqual(typeof moduleApi.%s, 'function');
console.log('seed tests passed for %s using %s');
`, entryFile, apiName, module.ID, contractURI)
	}
	raw, _ := common.MarshalJSONArtifact(common.TestFileBundle{
		SchemaVersion: 1,
		Kind:          "seed_tests",
		ModuleID:      module.ID,
		TestCommand:   seedTestCommand(module.ID),
		TestFiles: []common.TestFile{
			{Path: testPath, Content: content},
		},
	})
	return string(raw)
}

func isFrontendWebDeliveryModule(module splitModuleDoc) bool {
	return strings.EqualFold(strings.TrimSpace(module.DeliveryProfile), defaultDeliveryProfile) &&
		strings.EqualFold(strings.TrimSpace(module.DeliveryRole), deliveryRoleFrontendWebApp)
}

func seedTestCommand(moduleID string) string {
	return "node test/" + strings.TrimSpace(moduleID) + ".seed.test.js"
}

func publicAPIName(moduleName string) string {
	parts := strings.FieldsFunc(moduleName, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.'
	})
	if len(parts) == 0 {
		return "createModule"
	}
	var builder strings.Builder
	builder.WriteString("create")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			builder.WriteString(part[1:])
		}
	}
	name := builder.String()
	if name == "create" {
		return "createModule"
	}
	return name
}

func buildCoderModuleTask(module splitModuleDoc, mainBranch string, designURI string, design string, mainBranchURI string, contractURI string, seedTestsURI string) string {
	var builder strings.Builder
	builder.WriteString("# Programmer Task: ")
	builder.WriteString(module.Name)
	builder.WriteString("\n\n## Pairing Metadata\n\n")
	builder.WriteString("- module_id: ")
	builder.WriteString(module.ID)
	builder.WriteString("\n- coder_agent: ")
	builder.WriteString(module.Coder)
	builder.WriteString("\n- paired_tester_agent: ")
	builder.WriteString(module.Tester)
	builder.WriteString("\n- delivery_profile: ")
	builder.WriteString(module.DeliveryProfile)
	if strings.TrimSpace(module.DeliveryRole) != "" {
		builder.WriteString("\n- delivery_role: ")
		builder.WriteString(module.DeliveryRole)
	}
	builder.WriteString("\n- architecture_uri: ")
	builder.WriteString(designURI)
	builder.WriteString("\n- main_branch_uri: ")
	builder.WriteString(mainBranchURI)
	builder.WriteString("\n- module_contract_uri: ")
	builder.WriteString(contractURI)
	builder.WriteString("\n- seed_tests_uri: ")
	builder.WriteString(seedTestsURI)
	builder.WriteString("\n\n## Goal\n\nImplement ")
	builder.WriteString(module.Name)
	builder.WriteString(" according to the architecture document.\n\n## Module Scope\n\n")
	builder.WriteString(strings.TrimSpace(module.Responsibility))
	if isFrontendWebDeliveryModule(module) {
		builder.WriteString("\n\nThis is the frontend_web delivery module. It owns index.html, README.md, package.json if needed, and the browser-runnable app entry.")
	} else if strings.EqualFold(strings.TrimSpace(module.DeliveryProfile), defaultDeliveryProfile) {
		builder.WriteString("\n\nThis is not the frontend_web_app entry module. Do not modify index.html, README.md, or package.json. Implement only the module-owned src files declared by the contract.")
	}
	builder.WriteString("\n\n## Deliverables\n\n")
	for _, item := range module.Deliverables {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(item))
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Git\n\n- Start from main branch: ")
	builder.WriteString(mainBranch)
	builder.WriteString("\n- Return a coder branch artifact after implementation.\n")
	builder.WriteString("\n## Architecture Context\n\n")
	builder.WriteString(strings.TrimSpace(design))
	builder.WriteString("\n")
	return builder.String()
}

func buildTesterModuleTask(module splitModuleDoc, mainBranch string, coderTaskURI string, designURI string, design string, contractURI string, seedTestsURI string) string {
	var builder strings.Builder
	builder.WriteString("# Tester Task: ")
	builder.WriteString(module.Tester)
	builder.WriteString("\n\n## Pairing Metadata\n\n")
	builder.WriteString("- module_id: ")
	builder.WriteString(module.ID)
	builder.WriteString("\n- tester_agent: ")
	builder.WriteString(module.Tester)
	builder.WriteString("\n- paired_coder_agent: ")
	builder.WriteString(module.Coder)
	builder.WriteString("\n- delivery_profile: ")
	builder.WriteString(module.DeliveryProfile)
	if strings.TrimSpace(module.DeliveryRole) != "" {
		builder.WriteString("\n- delivery_role: ")
		builder.WriteString(module.DeliveryRole)
	}
	builder.WriteString("\n- module_task_uri: ")
	builder.WriteString(coderTaskURI)
	builder.WriteString("\n- architecture_uri: ")
	builder.WriteString(designURI)
	builder.WriteString("\n- module_contract_uri: ")
	builder.WriteString(contractURI)
	builder.WriteString("\n- seed_tests_uri: ")
	builder.WriteString(seedTestsURI)
	builder.WriteString("\n\n## Goal\n\nCreate test data and later validate the paired coder branch for ")
	builder.WriteString(module.Name)
	builder.WriteString(".\n\n## Test Focus\n\n")
	for _, item := range module.TestFocus {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(item))
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Git\n\n- Main branch: ")
	builder.WriteString(mainBranch)
	builder.WriteString("\n")
	builder.WriteString("\n## Architecture Context\n\n")
	builder.WriteString(strings.TrimSpace(design))
	builder.WriteString("\n")
	return builder.String()
}

func (a *Agent) executeSplitModuleFileFallback(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	a.logStep("split_module file fallback output used")
	module := splitModuleDoc{
		ID:              "module01",
		Name:            "snake_game",
		Coder:           "coder01",
		Tester:          "tester01",
		DeliveryProfile: defaultDeliveryProfile,
		DeliveryRole:    deliveryRoleFrontendWebApp,
		Responsibility:  fallbackResponsibility("snake_game", 1, 1),
		Deliverables:    fallbackDeliverables("snake_game", 1, 1),
		TestFocus:       fallbackTestFocus("snake_game", 1, 1),
	}
	verifyCommands := frontendWebGlobalVerifyCommands(nil, []splitModuleDoc{module})
	mainOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "branches", "main_branch.md"), buildMainBranchArtifact(a.runID, mainBranchInfo{MainBranch: "main", Status: "file_fallback"}, nil, verifyCommands))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	contractOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "contracts", "module01_contract.json"), buildModuleContractArtifact(module, splitRunConfig{MainBranch: "main"}, "", defaultSnakeArchitectureContext()))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	seedOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "seed_tests", "module01_seed_tests.json"), buildSeedTestsArtifact(module, contractOutput))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	moduleOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "modules", "coder01_task.md"), buildCoderModuleTask(module, "main", "", defaultSnakeArchitectureContext(), mainOutput, contractOutput, seedOutput))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	testerOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "tests", "tester01_task.md"), buildTesterModuleTask(module, "main", moduleOutput, "", defaultSnakeArchitectureContext(), contractOutput, seedOutput))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	planRaw, _ := json.Marshal(map[string]any{
		"modules":                         []splitModuleDoc{module},
		"official_global_verify_commands": verifyCommands,
	})
	planOutput, err := common.WriteAgentOutput(a.workspacePath, filepath.Join("artifacts", "module", "module_plan_v1.json"), string(planRaw))
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: []string{planOutput, mainOutput},
		Result:       core.TaskResultCodeOK,
		Control: []core.Control{
			{Type: core.ControlTypeNewCoder, AgentName: "coder01", ArtifactURIs: []string{moduleOutput, mainOutput, contractOutput, seedOutput}},
			{Type: core.ControlTypeNewTester, AgentName: "tester01", ArtifactURIs: []string{testerOutput, moduleOutput, contractOutput, seedOutput}},
		},
	}, nil
}

func defaultSnakeArchitectureContext() string {
	return `# Architecture Context

- A main game loop drives the Snake runtime.
- The game renders the board, snake, food, and score.
- The game handles keyboard input, start, game over, and restart.
- The final delivery must be real runnable code.`
}

func strictDeliveryEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("DEVFLOW_STRICT_DELIVERY")))
	return value == "1" || value == "true"
}
