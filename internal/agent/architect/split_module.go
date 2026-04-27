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

	"devflow/internal/agentengine"
	"devflow/internal/core"
	"devflow/internal/llm"
)

const splitModuleStrategy = "one_coder_per_module_tester_can_cover_multiple"

type runSplitConfig struct {
	MainBranch         string            `json:"main_branch"`
	CoderAgents        int               `json:"coder_agents"`
	TesterAgents       int               `json:"tester_agents"`
	MaxModules         int               `json:"max_modules"`
	AssignmentStrategy string            `json:"assignment_strategy"`
	AgentNamePrefix    map[string]string `json:"agent_name_prefix"`
	mainBranchDefault  bool
}

type splitModuleInputs struct {
	designURI string
	design    string
	configURI string
	configRaw string
	runConfig runSplitConfig
	basisURIs []string
}

type splitModulePlan struct {
	Modules     []splitModuleDefinition  `json:"modules"`
	CoderTasks  []splitAgentTaskDocument `json:"coder_tasks"`
	TesterTasks []splitAgentTaskDocument `json:"tester_tasks"`
	source      string
}

type splitModuleDefinition struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Goal           string   `json:"goal"`
	Scope          []string `json:"scope,omitempty"`
	OutOfScope     []string `json:"out_of_scope,omitempty"`
	Inputs         []string `json:"inputs,omitempty"`
	Outputs        []string `json:"outputs,omitempty"`
	Dependencies   []string `json:"dependencies,omitempty"`
	Interfaces     []string `json:"interfaces,omitempty"`
	SharedTypes    []string `json:"shared_types,omitempty"`
	DeveloperNotes []string `json:"developer_notes,omitempty"`
	TesterNotes    []string `json:"tester_notes,omitempty"`
}

type splitAgentTaskDocument struct {
	AgentName string   `json:"agent_name"`
	ModuleID  string   `json:"module_id,omitempty"`
	ModuleIDs []string `json:"module_ids,omitempty"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
}

type gitMainBranchInfo struct {
	RepositoryPath string
	MainBranch     string
	Status         string
	InitialCommit  string
}

func (a *Agent) executeSplitModule(ctx context.Context, task core.TaskMetaData) (core.TaskMetaData, error) {
	if a.artifactStore == nil {
		return core.TaskMetaData{}, fmt.Errorf("artifact store is required for split_module")
	}
	inputs, err := a.resolveSplitModuleInputs(ctx, task.ArtifactURIs)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	repoInfo, err := a.initProjectRepo(ctx, inputs.runConfig.MainBranch)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	plan, err := a.buildSplitModulePlan(ctx, inputs)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	plan = normalizeSplitPlan(plan, inputs.runConfig)

	mainBranchURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "branches", "main_branch.md")
	if err := a.artifactStore.Write(ctx, mainBranchURI, []byte(buildMainBranchDocument(a.runID, repoInfo, inputs))); err != nil {
		return core.TaskMetaData{}, fmt.Errorf("write main branch artifact: %w", err)
	}

	taskURIs, err := a.writeSplitTaskArtifacts(ctx, inputs, plan, mainBranchURI)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	controls, err := buildSplitControls(plan, taskURIs, mainBranchURI)
	if err != nil {
		return core.TaskMetaData{}, err
	}
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        task.RunID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      task.AgentID,
		Op:           task.Op,
		ArtifactURIs: []string{mainBranchURI},
		Result:       core.TaskResultCodeOK,
		Control:      controls,
	}, nil
}

func (a *Agent) resolveSplitModuleInputs(ctx context.Context, uris []string) (splitModuleInputs, error) {
	var inputs splitModuleInputs
	for _, uri := range uris {
		content, err := a.artifactStore.Read(ctx, uri)
		if err != nil {
			return splitModuleInputs{}, fmt.Errorf("read artifact %s: %w", uri, err)
		}
		normalizedURI := filepath.ToSlash(strings.TrimSpace(uri))
		if agentengine.InferArtifactKind(normalizedURI) == "design" {
			inputs.designURI = normalizedURI
			inputs.design = string(content)
			continue
		}
		lower := strings.ToLower(normalizedURI)
		if strings.Contains(lower, "run_config") || strings.Contains(lower, "/config/") {
			inputs.configURI = normalizedURI
			inputs.configRaw = string(content)
		}
	}
	if strings.TrimSpace(inputs.design) == "" {
		return splitModuleInputs{}, fmt.Errorf("split_module requires architecture design artifact")
	}
	if strings.TrimSpace(inputs.configRaw) == "" {
		return splitModuleInputs{}, fmt.Errorf("split_module requires run config artifact")
	}
	cfg, err := parseRunSplitConfig(inputs.configRaw)
	if err != nil {
		return splitModuleInputs{}, err
	}
	inputs.runConfig = cfg
	inputs.basisURIs = []string{inputs.designURI, inputs.configURI}
	return inputs, nil
}

func parseRunSplitConfig(raw string) (runSplitConfig, error) {
	block, err := extractJSONBlock(raw)
	if err != nil {
		return runSplitConfig{}, err
	}
	var cfg runSplitConfig
	if err := json.Unmarshal([]byte(block), &cfg); err != nil {
		return runSplitConfig{}, fmt.Errorf("parse run config json: %w", err)
	}
	if strings.TrimSpace(cfg.MainBranch) == "" {
		cfg.MainBranch = "main"
		cfg.mainBranchDefault = true
	}
	if cfg.CoderAgents <= 0 {
		return runSplitConfig{}, fmt.Errorf("run config coder_agents must be greater than zero")
	}
	if cfg.TesterAgents <= 0 {
		return runSplitConfig{}, fmt.Errorf("run config tester_agents must be greater than zero")
	}
	if cfg.MaxModules <= 0 {
		cfg.MaxModules = cfg.CoderAgents
	}
	if strings.TrimSpace(cfg.AssignmentStrategy) == "" {
		cfg.AssignmentStrategy = splitModuleStrategy
	}
	if cfg.AssignmentStrategy != splitModuleStrategy {
		return runSplitConfig{}, fmt.Errorf("unsupported assignment_strategy %q", cfg.AssignmentStrategy)
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

func extractJSONBlock(raw string) (string, error) {
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
	return "", fmt.Errorf("run config must contain a json code block")
}

func (a *Agent) initProjectRepo(ctx context.Context, mainBranch string) (gitMainBranchInfo, error) {
	runRoot, err := a.runRootFromWorkspace()
	if err != nil {
		return gitMainBranchInfo{}, err
	}
	repoPath := filepath.Join(runRoot, "project_repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return gitMainBranchInfo{}, err
	}
	info := gitMainBranchInfo{
		RepositoryPath: filepath.Clean(repoPath),
		MainBranch:     mainBranch,
		Status:         "initialized",
	}
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
		if err := runGit(ctx, repoPath, "checkout", mainBranch); err != nil {
			if err := runGit(ctx, repoPath, "checkout", "-B", mainBranch); err != nil {
				return gitMainBranchInfo{}, fmt.Errorf("checkout main branch: %w", err)
			}
		}
		commit, _ := gitOutput(ctx, repoPath, "rev-parse", "HEAD")
		info.InitialCommit = strings.TrimSpace(commit)
		info.Status = "existing"
		return info, nil
	}
	if err := runGit(ctx, repoPath, "init"); err != nil {
		return gitMainBranchInfo{}, fmt.Errorf("git init: %w", err)
	}
	_ = runGit(ctx, repoPath, "config", "user.name", "Architect Agent")
	_ = runGit(ctx, repoPath, "config", "user.email", "architect-agent@example.local")
	if err := runGit(ctx, repoPath, "symbolic-ref", "HEAD", "refs/heads/"+mainBranch); err != nil {
		return gitMainBranchInfo{}, fmt.Errorf("set main branch: %w", err)
	}
	if err := writeProjectRepoBootstrap(repoPath); err != nil {
		return gitMainBranchInfo{}, err
	}
	if err := runGit(ctx, repoPath, "add", "."); err != nil {
		return gitMainBranchInfo{}, fmt.Errorf("git add: %w", err)
	}
	if err := runGit(ctx, repoPath, "commit", "-m", "chore: initialize project workspace"); err != nil {
		info.Status = "initialized_without_commit"
		return info, nil
	}
	commit, _ := gitOutput(ctx, repoPath, "rev-parse", "HEAD")
	info.InitialCommit = strings.TrimSpace(commit)
	return info, nil
}

func (a *Agent) runRootFromWorkspace() (string, error) {
	workspace := strings.TrimSpace(a.workspacePath)
	if workspace == "" {
		return "", fmt.Errorf("workspace path is required for split_module git initialization")
	}
	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(workspaceAbs)), nil
}

func writeProjectRepoBootstrap(repoPath string) error {
	readme := "# Project Workspace\n\nThis repository was initialized by the Architect Agent during split_module.\n"
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte(readme), 0o644); err != nil {
		return fmt.Errorf("write project README: %w", err)
	}
	ignore := ".DS_Store\nnode_modules/\ndist/\nbuild/\n.env\n"
	if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(ignore), 0o644); err != nil {
		return fmt.Errorf("write project gitignore: %w", err)
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (a *Agent) buildSplitModulePlan(ctx context.Context, inputs splitModuleInputs) (splitModulePlan, error) {
	if llm.IsNoop(a.llmClient) {
		plan := fallbackSplitModulePlan(inputs)
		plan.source = "fallback"
		return plan, nil
	}
	raw, err := a.llmClient.Complete(ctx, buildSplitModulePrompt(inputs))
	if err != nil {
		a.logStep(fmt.Sprintf("split_module llm request failed, fallback used: %v", err))
		plan := fallbackSplitModulePlan(inputs)
		plan.source = "fallback"
		return plan, nil
	}
	plan, err := parseSplitModulePlan(raw)
	if err != nil {
		a.logStep(fmt.Sprintf("split_module llm output parse failed, fallback used: %v", err))
		plan := fallbackSplitModulePlan(inputs)
		plan.source = "fallback"
		return plan, nil
	}
	plan.source = "llm"
	return plan, nil
}

func buildSplitModulePrompt(inputs splitModuleInputs) string {
	cfg := inputs.runConfig
	var builder strings.Builder
	builder.WriteString("# Role\n")
	builder.WriteString("You are an Architect Agent responsible for splitting a technical architecture into parallel implementation and testing tasks.\n\n")
	builder.WriteString("# Architecture Design\n")
	builder.WriteString(inputs.design)
	builder.WriteString("\n\n# Run Config\n")
	builder.WriteString(inputs.configRaw)
	builder.WriteString("\n\n# Requirements\n")
	builder.WriteString(fmt.Sprintf("- main_branch: %s\n", cfg.MainBranch))
	builder.WriteString(fmt.Sprintf("- coder_agents: %d\n", cfg.CoderAgents))
	builder.WriteString(fmt.Sprintf("- tester_agents: %d\n", cfg.TesterAgents))
	builder.WriteString(fmt.Sprintf("- max_modules: %d\n", cfg.MaxModules))
	builder.WriteString("- Split modules so different programmers can work independently.\n")
	builder.WriteString("- Every programmer task must include clear interface contracts, shared data types, dependencies, file/directory ownership, forbidden changes, and integration notes.\n")
	builder.WriteString("- Tester tasks may cover multiple modules when tester count is lower than module count.\n")
	builder.WriteString("- Do not invent technology choices that are not implied by the architecture. Mark unknown choices as undecided.\n\n")
	builder.WriteString("# Output JSON Schema\n")
	builder.WriteString(`{"modules":[{"id":"safe_id","name":"ModuleName","goal":"...","scope":["..."],"out_of_scope":["..."],"inputs":["..."],"outputs":["..."],"dependencies":["..."],"interfaces":["..."],"shared_types":["..."],"developer_notes":["..."],"tester_notes":["..."]}],"coder_tasks":[{"agent_name":"coder_module_id","module_id":"module_id","title":"程序员任务书：ModuleName","content":"complete markdown task document"}],"tester_tasks":[{"agent_name":"tester_01","module_ids":["module_id"],"title":"测试员任务书：tester_01","content":"complete markdown task document"}]}`)
	builder.WriteString("\n\n# Constraints\n")
	builder.WriteString("1. Return exactly one JSON object.\n")
	builder.WriteString("2. Do not use markdown code fences.\n")
	builder.WriteString("3. modules count must not exceed max_modules or coder_agents.\n")
	builder.WriteString("4. coder_tasks count must equal modules count.\n")
	builder.WriteString("5. tester_tasks count must equal tester_agents.\n")
	builder.WriteString("6. module ids must use lowercase letters, digits, underscores, or hyphens only.\n")
	return builder.String()
}

func parseSplitModulePlan(raw string) (splitModulePlan, error) {
	jsonText, err := extractJSONObject(raw)
	if err != nil {
		return splitModulePlan{}, err
	}
	var plan splitModulePlan
	if err := json.Unmarshal([]byte(jsonText), &plan); err != nil {
		return splitModulePlan{}, fmt.Errorf("parse split module json: %w", err)
	}
	return plan, nil
}

func extractJSONObject(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			trimmed = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("model output does not contain a json object")
	}
	return trimmed[start : end+1], nil
}

func normalizeSplitPlan(plan splitModulePlan, cfg runSplitConfig) splitModulePlan {
	limit := cfg.MaxModules
	if cfg.CoderAgents < limit {
		limit = cfg.CoderAgents
	}
	if limit <= 0 {
		limit = 1
	}
	plan.Modules = sanitizeModules(plan.Modules, limit)
	if len(plan.Modules) == 0 {
		plan = fallbackSplitModulePlan(splitModuleInputs{runConfig: cfg})
		plan.Modules = sanitizeModules(plan.Modules, limit)
	}
	plan.CoderTasks = normalizeCoderTasks(plan, cfg)
	plan.TesterTasks = normalizeTesterTasks(plan, cfg)
	return plan
}

func sanitizeModules(modules []splitModuleDefinition, limit int) []splitModuleDefinition {
	out := make([]splitModuleDefinition, 0, limit)
	seen := map[string]bool{}
	for _, module := range modules {
		module.ID = sanitizeID(module.ID)
		if module.ID == "" || seen[module.ID] {
			continue
		}
		if strings.TrimSpace(module.Name) == "" {
			module.Name = module.ID
		}
		if strings.TrimSpace(module.Goal) == "" {
			module.Goal = "完成 " + module.Name + " 模块实现。"
		}
		out = append(out, module)
		seen[module.ID] = true
		if len(out) >= limit {
			break
		}
	}
	return out
}

func sanitizeID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var builder strings.Builder
	lastSep := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastSep = false
		case r == '_' || r == '-':
			if !lastSep {
				builder.WriteRune('_')
				lastSep = true
			}
		case r == ' ':
			if !lastSep {
				builder.WriteRune('_')
				lastSep = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

func normalizeCoderTasks(plan splitModulePlan, cfg runSplitConfig) []splitAgentTaskDocument {
	byModule := map[string]splitAgentTaskDocument{}
	for _, task := range plan.CoderTasks {
		task.ModuleID = sanitizeID(task.ModuleID)
		if task.ModuleID == "" {
			continue
		}
		byModule[task.ModuleID] = task
	}
	tasks := make([]splitAgentTaskDocument, 0, len(plan.Modules))
	for _, module := range plan.Modules {
		task := byModule[module.ID]
		task.ModuleID = module.ID
		task.ModuleIDs = nil
		if strings.TrimSpace(task.AgentName) == "" {
			task.AgentName = cfg.AgentNamePrefix["coder"] + "_" + module.ID
		}
		if strings.TrimSpace(task.Title) == "" {
			task.Title = "程序员任务书：" + module.Name
		}
		if strings.TrimSpace(task.Content) == "" {
			task.Content = buildCoderTaskContent(module, task.AgentName, cfg.MainBranch)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func normalizeTesterTasks(plan splitModulePlan, cfg runSplitConfig) []splitAgentTaskDocument {
	byAgent := map[string]splitAgentTaskDocument{}
	for _, task := range plan.TesterTasks {
		if strings.TrimSpace(task.AgentName) != "" {
			byAgent[task.AgentName] = task
		}
	}
	assignments := assignModulesToTesters(plan.Modules, cfg.TesterAgents, cfg.AgentNamePrefix["tester"])
	tasks := make([]splitAgentTaskDocument, 0, cfg.TesterAgents)
	for i := 0; i < cfg.TesterAgents; i++ {
		agentName := fmt.Sprintf("%s_%02d", cfg.AgentNamePrefix["tester"], i+1)
		task := byAgent[agentName]
		task.AgentName = agentName
		task.ModuleID = ""
		task.ModuleIDs = assignments[agentName]
		if strings.TrimSpace(task.Title) == "" {
			task.Title = "测试员任务书：" + agentName
		}
		if strings.TrimSpace(task.Content) == "" {
			task.Content = buildTesterTaskContent(agentName, assignedModuleDefs(plan.Modules, task.ModuleIDs), cfg.MainBranch)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func assignModulesToTesters(modules []splitModuleDefinition, testerCount int, prefix string) map[string][]string {
	assignments := map[string][]string{}
	for i := 0; i < testerCount; i++ {
		assignments[fmt.Sprintf("%s_%02d", prefix, i+1)] = nil
	}
	if testerCount <= 0 {
		return assignments
	}
	for i, module := range modules {
		agentName := fmt.Sprintf("%s_%02d", prefix, i%testerCount+1)
		assignments[agentName] = append(assignments[agentName], module.ID)
	}
	return assignments
}

func assignedModuleDefs(modules []splitModuleDefinition, ids []string) []splitModuleDefinition {
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	out := make([]splitModuleDefinition, 0, len(ids))
	for _, module := range modules {
		if allowed[module.ID] {
			out = append(out, module)
		}
	}
	return out
}

func fallbackSplitModulePlan(inputs splitModuleInputs) splitModulePlan {
	design := inputs.design
	modules := make([]splitModuleDefinition, 0)
	add := func(id, name, goal string) {
		modules = append(modules, splitModuleDefinition{
			ID:   id,
			Name: name,
			Goal: goal,
			Scope: []string{
				"根据架构书完成模块职责范围内的实现。",
			},
			DeveloperNotes: []string{"保持模块边界清晰，遵守架构书中的接口约定。"},
			TesterNotes:    []string{"覆盖模块核心功能、边界条件和集成接口。"},
		})
	}
	if strings.Contains(design, "GameEngine") {
		add("game_engine", "GameEngine", "实现核心游戏状态推进规则。")
	}
	if strings.Contains(design, "InputHandler") || strings.Contains(design, "Scheduler") {
		add("input_scheduler", "InputScheduler", "实现输入处理和游戏循环调度。")
	}
	if strings.Contains(design, "Renderer") {
		add("renderer_state", "RendererState", "实现渲染适配和页面状态展示。")
	}
	if strings.Contains(design, "FoodGenerator") || strings.Contains(design, "CollisionDetector") {
		add("rules_support", "RulesSupport", "实现食物生成和碰撞检测等规则支撑。")
	}
	if len(modules) == 0 {
		add("core_module", "CoreModule", "实现架构书中定义的核心模块。")
	}
	return splitModulePlan{Modules: modules, source: "fallback"}
}

func (a *Agent) writeSplitTaskArtifacts(ctx context.Context, inputs splitModuleInputs, plan splitModulePlan, mainBranchURI string) (map[string]string, error) {
	taskURIs := map[string]string{}
	basis := append([]string{}, inputs.basisURIs...)
	basis = append(basis, mainBranchURI)
	for _, task := range plan.CoderTasks {
		uri := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "modules", task.AgentName+"_task.md")
		content := prependDocumentBasis(task.Content, basis)
		if err := a.artifactStore.Write(ctx, uri, []byte(content)); err != nil {
			return nil, fmt.Errorf("write coder task %s: %w", task.AgentName, err)
		}
		taskURIs[task.AgentName] = uri
	}
	for _, task := range plan.TesterTasks {
		uri := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "tests", task.AgentName+"_task.md")
		content := prependDocumentBasis(task.Content, basis)
		if err := a.artifactStore.Write(ctx, uri, []byte(content)); err != nil {
			return nil, fmt.Errorf("write tester task %s: %w", task.AgentName, err)
		}
		taskURIs[task.AgentName] = uri
	}
	return taskURIs, nil
}

func buildSplitControls(plan splitModulePlan, taskURIs map[string]string, mainBranchURI string) ([]core.Control, error) {
	controls := make([]core.Control, 0, len(plan.CoderTasks)+len(plan.TesterTasks))
	for _, task := range plan.CoderTasks {
		uri := taskURIs[task.AgentName]
		if uri == "" {
			return nil, fmt.Errorf("missing coder task artifact for %s", task.AgentName)
		}
		controls = append(controls, core.Control{
			Type:         core.ControlTypeNewCoder,
			AgentName:    task.AgentName,
			ArtifactURIs: []string{uri, mainBranchURI},
		})
	}
	for _, task := range plan.TesterTasks {
		uri := taskURIs[task.AgentName]
		if uri == "" {
			return nil, fmt.Errorf("missing tester task artifact for %s", task.AgentName)
		}
		controls = append(controls, core.Control{
			Type:         core.ControlTypeNewTester,
			AgentName:    task.AgentName,
			ArtifactURIs: []string{uri, mainBranchURI},
		})
	}
	return controls, nil
}

func buildMainBranchDocument(runID core.RunID, info gitMainBranchInfo, inputs splitModuleInputs) string {
	payload := map[string]any{
		"schema_version":      1,
		"kind":                "main_branch",
		"run_id":              string(runID),
		"repo_dir":            filepath.ToSlash(info.RepositoryPath),
		"branch":              info.MainBranch,
		"commit":              info.InitialCommit,
		"status":              info.Status,
		"input_artifact_uris": inputs.basisURIs,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	return string(append(raw, '\n'))
}

func buildCoderTaskContent(module splitModuleDefinition, agentName, mainBranch string) string {
	var builder strings.Builder
	builder.WriteString("# 程序员任务书：")
	builder.WriteString(module.Name)
	builder.WriteString("\n\n## 模块目标\n\n")
	builder.WriteString(module.Goal)
	builder.WriteString("\n\n")
	writeListSection(&builder, "职责范围", module.Scope)
	writeListSection(&builder, "不负责范围", module.OutOfScope)
	writeListSection(&builder, "输入", module.Inputs)
	writeListSection(&builder, "输出", module.Outputs)
	writeListSection(&builder, "依赖模块", module.Dependencies)
	writeListSection(&builder, "对外接口", module.Interfaces)
	writeListSection(&builder, "公共数据结构", module.SharedTypes)
	writeListSection(&builder, "实现注意事项", module.DeveloperNotes)
	builder.WriteString("## Git 要求\n\n")
	builder.WriteString("- 从主分支 ")
	builder.WriteString(mainBranch)
	builder.WriteString(" 创建开发分支。\n")
	builder.WriteString("- 建议分支名：")
	builder.WriteString(strings.ReplaceAll(agentName, "_", "/"))
	builder.WriteString("\n")
	return builder.String()
}

func buildTesterTaskContent(agentName string, modules []splitModuleDefinition, mainBranch string) string {
	var builder strings.Builder
	builder.WriteString("# 测试员任务书：")
	builder.WriteString(agentName)
	builder.WriteString("\n\n## 负责模块\n\n")
	for _, module := range modules {
		builder.WriteString("- ")
		builder.WriteString(module.ID)
		builder.WriteString("：")
		builder.WriteString(module.Name)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	for _, module := range modules {
		builder.WriteString("## ")
		builder.WriteString(module.Name)
		builder.WriteString(" 测试要求\n\n")
		notes := module.TesterNotes
		if len(notes) == 0 {
			notes = []string{"验证模块职责、接口契约、边界条件和集成行为。"}
		}
		for _, note := range notes {
			builder.WriteString("- ")
			builder.WriteString(note)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Git 要求\n\n")
	builder.WriteString("- 基于主分支 ")
	builder.WriteString(mainBranch)
	builder.WriteString(" 和对应 coder 分支进行测试。\n")
	return builder.String()
}

func writeListSection(builder *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	for _, item := range items {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
}
