package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode"

	"doujia/internal/agent/core"
	"doujia/internal/agent/llm"
	"doujia/internal/agent/schema"
)

type splitContainerContext struct {
	RepoDir            string `json:"repo_dir"`
	WorktreesDir       string `json:"worktrees_dir"`
	TestRunsDir        string `json:"test_runs_dir"`
	BaseBranch         string `json:"base_branch"`
	BranchPrefix       string `json:"branch_prefix"`
	DefaultTestCommand string `json:"default_test_command"`
}

type splitModule struct {
	ModuleRole string
	Spec       schema.ModuleSpec
	Plan       semanticModulePlan
}

type semanticSplitPlan struct {
	Modules        []semanticModulePlan   `json:"modules"`
	SharedContract semanticSharedContract `json:"shared_contract"`
}

type semanticModulePlan struct {
	ModuleID         string   `json:"module_id"`
	ModuleRole       string   `json:"module_role"`
	ModuleName       string   `json:"module_name"`
	OwnedPaths       []string `json:"owned_paths"`
	Responsibilities []string `json:"responsibilities"`
	Dependencies     []string `json:"dependencies,omitempty"`
	CoderFocus       []string `json:"coder_focus"`
	TesterFocus      []string `json:"tester_focus"`
}

type semanticSharedContract struct {
	APIPrefix               string   `json:"api_prefix"`
	FrontendBaseURLStrategy string   `json:"frontend_base_url_strategy"`
	MergeRules              []string `json:"merge_rules"`
	IntegrationChecks       []string `json:"integration_checks"`
}

func (a *Agent) runSplitModule(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	architecturePlan, err := readBundleArtifactContent(req.Bundle, core.LKArchitecturePlan)
	if err != nil {
		return core.AgentResult{}, err
	}
	containerContent, err := readBundleArtifactContent(req.Bundle, core.LKContainerContext)
	if err != nil {
		return core.AgentResult{}, err
	}
	environmentContent, _ := readBundleArtifactContent(req.Bundle, core.LKEnvironmentSpec)

	var container splitContainerContext
	if err := json.Unmarshal([]byte(containerContent), &container); err != nil {
		return core.AgentResult{}, err
	}
	var env schema.EnvironmentSpec
	if strings.TrimSpace(environmentContent) != "" {
		_ = json.Unmarshal([]byte(environmentContent), &env)
	}

	config := schema.RunDeliveryConfig{BackendModuleCount: 1}
	for _, input := range req.Bundle.Inputs {
		if input.LogicalKey != core.LKRunDeliveryConfig || input.Path == "" {
			continue
		}
		loaded, err := schema.ReadRunDeliveryConfigFile(input.Path)
		if err != nil {
			return core.AgentResult{}, err
		}
		config = loaded
		break
	}

	semanticPlan, err := planSemanticSplit(ctx, req.LLM, architecturePlan, env, config)
	if err != nil {
		return agentFail("invalid_semantic_split", err.Error()), nil
	}

	modules, err := buildSplitModules(container, env, config, semanticPlan)
	if err != nil {
		return agentFail("invalid_semantic_split", err.Error()), nil
	}
	moduleSpecs := schema.ModuleSpecs{
		Modules:           make([]schema.ModuleSpec, 0, len(modules)),
		GlobalTestCommand: firstNonEmpty(env.DefaultTestCommand, container.DefaultTestCommand),
	}
	for _, module := range modules {
		moduleSpecs.Modules = append(moduleSpecs.Modules, module.Spec)
	}

	outputs := make([]core.AgentOutput, 0, 1+len(modules)*5)
	output, err := writeOutputArtifact(ctx, req, core.LKModuleSpecs, mustJSON(moduleSpecs))
	if err != nil {
		return core.AgentResult{}, err
	}
	outputs = append(outputs, output)

	for _, module := range modules {
		if err := writeModuleArtifacts(ctx, req, architecturePlan, semanticPlan.SharedContract, module, &outputs); err != nil {
			return core.AgentResult{}, err
		}
	}

	return core.AgentResult{
		Result:  "kok",
		Message: fmt.Sprintf("architect split_module completed with %d modules", len(modules)),
		Outputs: outputs,
	}, nil
}

func planSemanticSplit(ctx context.Context, llmClient core.LLMClientLike, architecturePlan string, env schema.EnvironmentSpec, config schema.RunDeliveryConfig) (semanticSplitPlan, error) {
	adapter, ok := llmClient.(llm.Adapter)
	if !ok || adapter == nil {
		return semanticSplitPlan{}, fmt.Errorf("split_module requires llm adapter")
	}

	initialPrompt := buildSemanticSplitPrompt(architecturePlan, env, config)
	plan, raw, err := requestSemanticSplitPlan(ctx, adapter, initialPrompt)
	if err != nil {
		return semanticSplitPlan{}, err
	}
	if err := validateSemanticSplitPlan(config, plan); err == nil {
		return plan, nil
	} else {
		repairPrompt := buildSemanticSplitRepairPrompt(architecturePlan, env, config, raw, err.Error())
		repaired, _, repairErr := requestSemanticSplitPlan(ctx, adapter, repairPrompt)
		if repairErr != nil {
			return semanticSplitPlan{}, fmt.Errorf("semantic split repair failed: %w", repairErr)
		}
		if repairValidationErr := validateSemanticSplitPlan(config, repaired); repairValidationErr != nil {
			return semanticSplitPlan{}, fmt.Errorf("semantic split plan invalid after repair: %w", repairValidationErr)
		}
		return repaired, nil
	}
}

func requestSemanticSplitPlan(ctx context.Context, adapter llm.Adapter, prompt string) (semanticSplitPlan, string, error) {
	resp, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "You are an architect split planner. Output valid JSON only. Do not use code fences. " +
					"Do not invent ports or hosts when the upstream artifacts do not specify them; instead describe the strategy in frontend_base_url_strategy.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
	})
	if err != nil {
		return semanticSplitPlan{}, "", err
	}
	plan, err := parseSemanticSplitPlanContent(resp.Message.Content)
	if err != nil {
		return semanticSplitPlan{}, resp.Message.Content, err
	}
	return plan, resp.Message.Content, nil
}

func buildSemanticSplitPrompt(architecturePlan string, env schema.EnvironmentSpec, config schema.RunDeliveryConfig) string {
	config = config.Normalized()
	moduleIDs := make([]string, 0, 1+config.BackendModuleCount)
	for i := 1; i <= 1+config.BackendModuleCount; i++ {
		moduleIDs = append(moduleIDs, fmt.Sprintf("module%02d", i))
	}

	return strings.TrimSpace(fmt.Sprintf(`
Read the upstream architecture plan and return one compact JSON object with this exact top-level shape:
{
  "modules": [
    {
      "module_id": "module01",
      "module_role": "frontend",
      "module_name": "...",
      "owned_paths": ["..."],
      "responsibilities": ["..."],
      "dependencies": ["..."],
      "coder_focus": ["..."],
      "tester_focus": ["..."]
    }
  ],
  "shared_contract": {
    "api_prefix": "/api",
    "frontend_base_url_strategy": "...",
    "merge_rules": ["..."],
    "integration_checks": ["..."]
  }
}

Hard constraints:
- Exactly %d modules are required: %s
- module01 must have module_role="frontend"
- module02 and later must have module_role="backend"
- Use the architecture plan as the source of truth for responsibilities and concrete owned_paths
- owned_paths must be safe repo-relative paths or directory globs ending with "/**"
- Frontend owned_paths must stay outside server/ and data/
- Backend owned_paths must stay inside server/ or data/
- Keep module owned_paths non-overlapping
- Make module responsibilities collectively cover the main capabilities from the architecture plan
- If upstream does not specify a literal port or host, do not invent one. Explain how frontend obtains the backend base URL in frontend_base_url_strategy
- Return JSON only, no comments, no markdown

Helpful context:
- backend_module_count: %d
- default_test_command: %s

Architecture plan:
%s
`, 1+config.BackendModuleCount, strings.Join(moduleIDs, ", "), config.BackendModuleCount, firstNonEmpty(env.DefaultTestCommand, "cd server && npm test"), strings.TrimSpace(architecturePlan)))
}

func buildSemanticSplitRepairPrompt(architecturePlan string, env schema.EnvironmentSpec, config schema.RunDeliveryConfig, previousOutput string, validationErr string) string {
	return strings.TrimSpace(fmt.Sprintf(`
The previous semantic split JSON is invalid.

Validation error:
%s

Previous JSON:
%s

Repair only the invalid parts and return the full corrected JSON object.
Keep the same top-level shape as before.
Do not change the required module count or the fixed rule that module01 is frontend and module02+ are backend.
Do not invent unspecified ports or hosts.

Helpful context:
- backend_module_count: %d
- default_test_command: %s

Architecture plan:
%s
`, validationErr, strings.TrimSpace(previousOutput), config.Normalized().BackendModuleCount, firstNonEmpty(env.DefaultTestCommand, "cd server && npm test"), strings.TrimSpace(architecturePlan)))
}

func parseSemanticSplitPlanContent(content string) (semanticSplitPlan, error) {
	var plan semanticSplitPlan
	for _, candidate := range semanticSplitCandidates(content) {
		if err := json.Unmarshal([]byte(candidate), &plan); err == nil {
			return plan, nil
		}
	}
	return semanticSplitPlan{}, fmt.Errorf("semantic split response must be valid JSON")
}

func semanticSplitCandidates(content string) []string {
	seen := map[string]bool{}
	add := func(candidates *[]string, value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		*candidates = append(*candidates, value)
	}

	trimmed := strings.TrimSpace(content)
	var candidates []string
	add(&candidates, trimmed)

	unfenced := trimJSONCodeFence(trimmed)
	add(&candidates, unfenced)

	if extracted, ok := extractFirstJSONObject(trimmed); ok {
		add(&candidates, extracted)
	}
	if extracted, ok := extractFirstJSONObject(unfenced); ok {
		add(&candidates, extracted)
	}
	return candidates
}

func trimJSONCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	rest := trimmed[3:]
	if idx := strings.Index(rest, "\n"); idx >= 0 {
		lang := strings.TrimSpace(rest[:idx])
		if lang == "" || isFenceLanguageLabel(lang) {
			body := rest[idx+1:]
			if end := strings.LastIndex(body, "```"); end >= 0 {
				return strings.TrimSpace(body[:end])
			}
		}
	}
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		return strings.TrimSpace(rest[:end])
	}
	return trimmed
}

func isFenceLanguageLabel(label string) bool {
	for _, r := range label {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func extractFirstJSONObject(content string) (string, bool) {
	start := strings.Index(content, "{")
	if start < 0 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		ch := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1], true
			}
		}
	}
	return "", false
}

func validateSemanticSplitPlan(config schema.RunDeliveryConfig, plan semanticSplitPlan) error {
	config = config.Normalized()
	expectedCount := 1 + config.BackendModuleCount
	if len(plan.Modules) != expectedCount {
		return fmt.Errorf("expected %d modules, got %d", expectedCount, len(plan.Modules))
	}
	if strings.TrimSpace(plan.SharedContract.APIPrefix) == "" || !strings.HasPrefix(strings.TrimSpace(plan.SharedContract.APIPrefix), "/") {
		return fmt.Errorf("shared_contract.api_prefix must start with /")
	}
	if strings.TrimSpace(plan.SharedContract.FrontendBaseURLStrategy) == "" {
		return fmt.Errorf("shared_contract.frontend_base_url_strategy must be non-empty")
	}
	if len(nonEmptyStrings(plan.SharedContract.MergeRules)) == 0 {
		return fmt.Errorf("shared_contract.merge_rules must be non-empty")
	}
	if len(nonEmptyStrings(plan.SharedContract.IntegrationChecks)) == 0 {
		return fmt.Errorf("shared_contract.integration_checks must be non-empty")
	}

	seenIDs := map[string]bool{}
	ownedByPath := map[string]string{}
	for i, module := range plan.Modules {
		expectedID := fmt.Sprintf("module%02d", i+1)
		expectedRole := "backend"
		if i == 0 {
			expectedRole = "frontend"
		}
		if strings.TrimSpace(module.ModuleID) != expectedID {
			return fmt.Errorf("%s must appear in order", expectedID)
		}
		if strings.TrimSpace(module.ModuleRole) != expectedRole {
			return fmt.Errorf("%s must have module_role=%s", expectedID, expectedRole)
		}
		if seenIDs[module.ModuleID] {
			return fmt.Errorf("duplicate module_id: %s", module.ModuleID)
		}
		seenIDs[module.ModuleID] = true
		if strings.TrimSpace(module.ModuleName) == "" {
			return fmt.Errorf("%s.module_name must be non-empty", module.ModuleID)
		}
		if len(nonEmptyStrings(module.Responsibilities)) == 0 {
			return fmt.Errorf("%s.responsibilities must be non-empty", module.ModuleID)
		}
		if len(nonEmptyStrings(module.CoderFocus)) == 0 {
			return fmt.Errorf("%s.coder_focus must be non-empty", module.ModuleID)
		}
		if len(nonEmptyStrings(module.TesterFocus)) == 0 {
			return fmt.Errorf("%s.tester_focus must be non-empty", module.ModuleID)
		}
		if len(module.OwnedPaths) == 0 {
			return fmt.Errorf("%s.owned_paths must be non-empty", module.ModuleID)
		}

		for _, rawPath := range module.OwnedPaths {
			ownedPath := normalizeOwnedPath(rawPath)
			if !isSafeSemanticOwnedPath(ownedPath) {
				return fmt.Errorf("%s has unsafe owned_path %q", module.ModuleID, rawPath)
			}
			if module.ModuleRole == "frontend" && !isAllowedFrontendOwnedPath(ownedPath) {
				return fmt.Errorf("%s has invalid frontend owned_path %q", module.ModuleID, rawPath)
			}
			if module.ModuleRole == "backend" && !isAllowedBackendOwnedPath(ownedPath) {
				return fmt.Errorf("%s has invalid backend owned_path %q", module.ModuleID, rawPath)
			}
			for existingPath, existingModuleID := range ownedByPath {
				if pathsOverlap(existingPath, ownedPath) {
					return fmt.Errorf("owned_paths overlap between %s and %s: %s vs %s", existingModuleID, module.ModuleID, existingPath, ownedPath)
				}
			}
			ownedByPath[ownedPath] = module.ModuleID
		}
	}
	return nil
}

func buildSplitModules(container splitContainerContext, env schema.EnvironmentSpec, config schema.RunDeliveryConfig, plan semanticSplitPlan) ([]splitModule, error) {
	if err := validateSemanticSplitPlan(config, plan); err != nil {
		return nil, err
	}

	config = config.Normalized()
	modules := make([]splitModule, 0, len(plan.Modules))
	for _, assignment := range plan.Modules {
		testCommand := firstNonEmpty(env.DefaultTestCommand, container.DefaultTestCommand, "cd server && npm test")
		ownedPaths := ownedPathsForModule(assignment)
		runtimeWritePaths := normalizeOwnedPaths(runtimeWritePathsForModule(assignment))
		forbiddenPaths := forbiddenPathsForModule(assignment)
		tester := "backend-tester"
		if assignment.ModuleRole == "frontend" {
			testCommand = frontendTestCommand(env, container)
			tester = "frontend-tester"
		}

		modules = append(modules, splitModule{
			ModuleRole: assignment.ModuleRole,
			Plan:       assignment,
			Spec: schema.ModuleSpec{
				ModuleID:           assignment.ModuleID,
				ModuleName:         assignment.ModuleName,
				ModuleRole:         assignment.ModuleRole,
				ImplementationRole: implementationRoleForModuleRole(assignment.ModuleRole),
				BranchName:         container.BranchPrefix + assignment.ModuleID + "-" + assignment.ModuleRole,
				WorktreeDir:        path.Join(container.WorktreesDir, assignment.ModuleID),
				TestRunDir:         path.Join(container.TestRunsDir, assignment.ModuleID),
				OwnedPaths:         ownedPaths,
				RuntimeWritePaths:  runtimeWritePaths,
				ForbiddenPaths:     forbiddenPaths,
				TestCommand:        testCommand,
				Complexity:         "high",
				Tester:             tester,
			},
		})
	}
	return modules, nil
}

func implementationRoleForModuleRole(moduleRole string) string {
	if strings.TrimSpace(moduleRole) == "frontend" {
		return "front"
	}
	return "coder"
}

func frontendTestCommand(env schema.EnvironmentSpec, container splitContainerContext) string {
	command := firstNonEmpty(env.DefaultTestCommand, container.DefaultTestCommand)
	if strings.Contains(strings.ToLower(command), "miniprogram") {
		return command
	}
	return "echo \"frontend seed tests pending\" && exit 0"
}

func writeModuleArtifacts(ctx context.Context, req core.AgentRunRequest, architecturePlan string, shared semanticSharedContract, module splitModule, outputs *[]core.AgentOutput) error {
	for _, item := range []struct {
		key     string
		content string
	}{
		{key: core.ModuleSpecKey(module.Spec.ModuleID), content: mustJSON(module.Spec)},
		{key: core.ModuleCoderTaskKey(module.Spec.ModuleID), content: buildCoderTask(architecturePlan, shared, module)},
		{key: core.ModuleTesterTaskKey(module.Spec.ModuleID), content: buildTesterTask(architecturePlan, shared, module)},
		{key: core.ModuleContractKey(module.Spec.ModuleID), content: buildModuleContract(shared, module)},
		{key: core.ModuleSeedTestsKey(module.Spec.ModuleID), content: buildSeedTests(module)},
	} {
		output, err := writeOutputArtifact(ctx, req, item.key, item.content)
		if err != nil {
			return err
		}
		*outputs = append(*outputs, output)
	}
	return nil
}

func buildCoderTask(architecturePlan string, shared semanticSharedContract, module splitModule) string {
	return fmt.Sprintf(`# %s Coder Task

## Goal
Implement the %s module defined by the validated semantic split plan.

## Scope
- module_id: %s
- module_name: %s
- module_role: %s
- implementation_role: %s
- test_command: %s

## Responsibilities
%s

## Owned Paths
%s

## Runtime Write Paths
%s

## Dependencies
%s

## Coding Focus
%s

## Shared Contract
- api_prefix: %s
- frontend_base_url_strategy: %s
- merge_rules:
%s
- integration_checks:
%s

## Requirements
- Keep all changes inside owned_paths.
- Runtime-only support files may use runtime_write_paths when needed for scoped tests or shared setup.
- Shared scaffold files and documentation are pre-created upstream; do not rewrite README.md or shared backend setup files unless they are explicitly listed in owned_paths or runtime_write_paths.
- Do not touch forbidden_paths or files owned by other modules.
- Make the module pass the declared test command.

## Architecture Context
%s
`, module.Spec.ModuleID, module.ModuleRole, module.Spec.ModuleID, module.Spec.ModuleName, module.ModuleRole, module.Spec.ImplementationRole, module.Spec.TestCommand, bulletList(module.Plan.Responsibilities), bulletList(module.Spec.OwnedPaths), bulletList(module.Spec.RuntimeWritePaths), bulletListOrNone(module.Plan.Dependencies), bulletList(module.Plan.CoderFocus), shared.APIPrefix, shared.FrontendBaseURLStrategy, bulletList(shared.MergeRules), bulletList(shared.IntegrationChecks), trimForTask(architecturePlan))
}

func buildTesterTask(architecturePlan string, shared semanticSharedContract, module splitModule) string {
	return fmt.Sprintf(`# %s Tester Task

## Goal
Prepare focused tests for the %s module based on the validated semantic split plan.

## Scope
- module_id: %s
- module_name: %s
- test_run_dir: %s
- test_command: %s

## Responsibilities To Protect
%s

## Testing Focus
%s

## Shared Integration Checks
%s

## Architecture Context
%s
`, module.Spec.ModuleID, module.ModuleRole, module.Spec.ModuleID, module.Spec.ModuleName, module.Spec.TestRunDir, module.Spec.TestCommand, bulletList(module.Plan.Responsibilities), bulletList(module.Plan.TesterFocus), bulletList(shared.IntegrationChecks), trimForTask(architecturePlan))
}

func buildModuleContract(shared semanticSharedContract, module splitModule) string {
	payload := map[string]any{
		"kind":                       "module_contract",
		"module_id":                  module.Spec.ModuleID,
		"module_name":                module.Spec.ModuleName,
		"module_role":                module.ModuleRole,
		"implementation_role":        module.Spec.ImplementationRole,
		"owned_paths":                module.Spec.OwnedPaths,
		"runtime_write_paths":        module.Spec.RuntimeWritePaths,
		"forbidden_paths":            module.Spec.ForbiddenPaths,
		"responsibilities":           module.Plan.Responsibilities,
		"dependencies":               module.Plan.Dependencies,
		"coder_focus":                module.Plan.CoderFocus,
		"tester_focus":               module.Plan.TesterFocus,
		"api_prefix":                 shared.APIPrefix,
		"frontend_base_url_strategy": shared.FrontendBaseURLStrategy,
		"merge_rules":                shared.MergeRules,
		"integration_checks":         shared.IntegrationChecks,
	}
	return mustJSON(payload)
}

func buildSeedTests(module splitModule) string {
	files := map[string]string{}
	if module.ModuleRole == "frontend" {
		files["miniprogram/tests/frontend.smoke.md"] = "# Frontend smoke checklist\n- App boots\n- Main pages render\n- Shared request helper uses the agreed API prefix\n"
	} else {
		files["server/tests/"+module.Spec.ModuleID+"/smoke.test.js"] = "describe('" + module.Spec.ModuleID + " smoke', () => { test('placeholder', () => { expect(true).toBe(true); }); });\n"
	}
	payload := map[string]any{
		"kind":         "seed_tests",
		"module_id":    module.Spec.ModuleID,
		"test_command": module.Spec.TestCommand,
		"focus":        module.Plan.TesterFocus,
		"files":        files,
	}
	return mustJSON(payload)
}

func mustJSON(v any) string {
	body, _ := json.MarshalIndent(v, "", "  ")
	return string(body) + "\n"
}

func bulletList(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range nonEmptyStrings(items) {
		lines = append(lines, "- "+item)
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

func bulletListOrNone(items []string) string {
	return bulletList(items)
}

func trimForTask(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= 1200 {
		return text
	}
	return string(runes[:1200]) + "\n\n(truncated)"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func normalizeOwnedPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, raw := range paths {
		normalized := normalizeOwnedPath(raw)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeOwnedPath(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
}

func appendUniquePath(paths []string, value string) []string {
	value = normalizeOwnedPath(value)
	if value == "" {
		return paths
	}
	for _, existing := range paths {
		if existing == value {
			return paths
		}
	}
	return append(paths, value)
}

func runtimeWritePathsForModule(module semanticModulePlan) []string {
	if strings.TrimSpace(module.ModuleRole) == "frontend" {
		return []string{"miniprogram/tests/**"}
	}
	if strings.TrimSpace(module.ModuleID) == "module02" {
		return []string{
			"server/package.json",
			"server/package-lock.json",
			"server/jest.config.js",
			"server/tests/module02/**",
		}
	}
	return []string{"server/tests/" + strings.TrimSpace(module.ModuleID) + "/**"}
}

func ownedPathsForModule(module semanticModulePlan) []string {
	paths := normalizeOwnedPaths(module.OwnedPaths)
	if strings.TrimSpace(module.ModuleRole) == "backend" && strings.TrimSpace(module.ModuleID) == "module02" {
		paths = appendUniquePath(paths, "server/utils/**")
	}
	return paths
}

func forbiddenPathsForModule(module semanticModulePlan) []string {
	if strings.TrimSpace(module.ModuleRole) == "frontend" {
		return []string{"server/**", "data/**", "README.md", ".git/**"}
	}

	paths := []string{"miniprogram/**", "pages/**", "components/**", "utils/**", "assets/**", "services/**", "store/**", "app.js", "app.json", "app.wxss", "README.md", ".git/**"}
	if strings.TrimSpace(module.ModuleID) != "module02" {
		for _, extra := range []string{
			"server/app.js",
			"server/db/**",
			"server/utils/**",
			"server/package.json",
			"server/package-lock.json",
			"server/jest.config.js",
		} {
			paths = appendUniquePath(paths, extra)
		}
	}
	return paths
}

func isSafeSemanticOwnedPath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") {
		return false
	}
	base := strings.TrimSuffix(value, "/**")
	if base == "." || base == ".." || strings.HasPrefix(base, "../") {
		return false
	}
	return !strings.HasPrefix(base, ".git/")
}

func isAllowedFrontendOwnedPath(value string) bool {
	value = normalizeOwnedPath(value)
	if strings.HasPrefix(value, "server/") || strings.HasPrefix(value, "data/") {
		return false
	}
	if value == "app.js" || value == "app.json" || value == "app.wxss" || value == "project.config.json" || value == "sitemap.json" {
		return true
	}
	allowedPrefixes := []string{
		"miniprogram/",
		"pages/",
		"components/",
		"utils/",
		"assets/",
		"services/",
		"store/",
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func isAllowedBackendOwnedPath(value string) bool {
	value = normalizeOwnedPath(value)
	return strings.HasPrefix(value, "server/") || strings.HasPrefix(value, "data/")
}

func pathsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	leftBase := strings.TrimSuffix(left, "/**")
	rightBase := strings.TrimSuffix(right, "/**")
	leftGlob := strings.HasSuffix(left, "/**")
	rightGlob := strings.HasSuffix(right, "/**")

	if leftGlob && (rightBase == leftBase || strings.HasPrefix(rightBase, leftBase+"/")) {
		return true
	}
	if rightGlob && (leftBase == rightBase || strings.HasPrefix(leftBase, rightBase+"/")) {
		return true
	}
	return false
}
