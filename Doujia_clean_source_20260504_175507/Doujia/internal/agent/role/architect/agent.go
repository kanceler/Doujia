package architect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"devflow/internal/agent/core"
	"devflow/internal/agent/llm"
	rolecommon "devflow/internal/agent/role/common"
	"devflow/internal/agent/schema"
)

type Agent struct{}

func NewAgent() *Agent {
	return &Agent{}
}

func (a *Agent) Role() string {
	return "architect"
}

func (a *Agent) Run(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	return rolecommon.DispatchByOpID(ctx, req, map[string]rolecommon.OpHandler{
		"architect.merge_code":       a.runMergeCode,
		"architect.test_code":        a.runTestCode,
		"architect.write_plan":       a.runWritePlan,
		"architect.split_module":     a.runSplitModuleWithValidation,
		"architect.test_data":        a.runTestData,
		"architect.create_container": a.runCreateContainer,
	}, "unsupported_architect_op")
}

func (a *Agent) runWritePlan(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	if req.ToolLoop == nil {
		return agentFail("missing_tool_loop", "architect agent requires ToolLoop"), nil
	}
	result, err := req.ToolLoop.Run(ctx, core.ToolLoopRequest{
		Task:     req.Task,
		Bundle:   req.Bundle,
		OpSpec:   req.OpSpec,
		Prompt:   req.Prompt,
		Handlers: req.Handlers,
		LLM:      req.LLM,
	})
	if err != nil || result.Result != "kok" {
		return result, err
	}
	if err := validateArchitectWritePlanOutputs(result); err != nil {
		return agentFail("invalid_environment_spec", "environment_spec validation failed: "+err.Error()), nil
	}
	return result, nil
}

func (a *Agent) runSplitModuleWithValidation(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	result, err := a.runSplitModule(ctx, req)
	if err != nil || result.Result != "kok" {
		return result, err
	}
	if err := validateArchitectSplitModuleOutputs(result); err != nil {
		return agentFail("invalid_module_specs", "module spec validation failed: "+err.Error()), nil
	}
	return result, nil
}

func (a *Agent) runTestData(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	if req.ToolLoop == nil {
		return agentFail("missing_tool_loop", "architect agent requires ToolLoop"), nil
	}
	if result, ok, err := a.validateTestDataUpstream(ctx, req); err != nil {
		return core.AgentResult{}, err
	} else if ok {
		return result, nil
	}
	result, err := req.ToolLoop.Run(ctx, core.ToolLoopRequest{
		Task:     req.Task,
		Bundle:   req.Bundle,
		OpSpec:   req.OpSpec,
		Prompt:   req.Prompt,
		Handlers: req.Handlers,
		LLM:      req.LLM,
	})
	if err != nil || result.Result != "kok" {
		return result, err
	}
	hasMerged := bundleHasLogicalKey(req.Bundle, core.LKMergedMainBranch)
	if err := validateArchitectTestDataOutputs(result, hasMerged); err != nil {
		return agentFail("invalid_global_test_artifacts", "global test artifact validation failed: "+err.Error()), nil
	}
	if err := normalizeGlobalTestCommandsForServerPackage(req.Bundle, result); err != nil {
		return agentFail("invalid_global_test_artifacts", "global test command normalization failed: "+err.Error()), nil
	}
	return result, nil
}

func validateArchitectWritePlanOutputs(result core.AgentResult) error {
	for _, output := range result.Outputs {
		if output.LogicalKey != core.LKEnvironmentSpec || output.Status != "produced" || output.Path == "" {
			continue
		}
		spec, err := schema.ReadEnvironmentSpecFile(output.Path)
		if err != nil {
			return err
		}
		return spec.Validate()
	}
	return fmt.Errorf("missing produced output %q", core.LKEnvironmentSpec)
}

func validateArchitectSplitModuleOutputs(result core.AgentResult) error {
	var moduleSpecsPath string
	var singlePaths []string
	for _, output := range result.Outputs {
		if output.Status != "produced" || output.Path == "" {
			continue
		}
		switch output.LogicalKey {
		case core.LKModuleSpecs:
			moduleSpecsPath = output.Path
		default:
			if strings.HasPrefix(output.LogicalKey, "module") && strings.HasSuffix(output.LogicalKey, "_spec") {
				singlePaths = append(singlePaths, output.Path)
			}
		}
	}
	if moduleSpecsPath == "" {
		return fmt.Errorf("missing produced output %q", core.LKModuleSpecs)
	}
	return schema.ValidateModuleSpecFiles(moduleSpecsPath, singlePaths...)
}

func validateArchitectTestDataOutputs(result core.AgentResult, hasMergedMainBranch bool) error {
	var acceptancePath string
	var commandsPath string
	for _, output := range result.Outputs {
		if output.Status != "produced" || output.Path == "" {
			continue
		}
		switch output.LogicalKey {
		case core.LKGlobalAcceptanceTests:
			acceptancePath = output.Path
		case core.LKGlobalTestCommands:
			commandsPath = output.Path
		}
	}
	if acceptancePath == "" {
		return fmt.Errorf("missing produced output %q", core.LKGlobalAcceptanceTests)
	}
	if commandsPath == "" {
		return fmt.Errorf("missing produced output %q", core.LKGlobalTestCommands)
	}
	acceptance, err := schema.ReadGlobalAcceptanceTestsFile(acceptancePath)
	if err != nil {
		return err
	}
	if err := acceptance.Validate(hasMergedMainBranch); err != nil {
		return err
	}
	commands, err := schema.ReadGlobalTestCommandsFile(commandsPath)
	if err != nil {
		return err
	}
	return commands.Validate()
}

func normalizeGlobalTestCommandsForServerPackage(bundle core.AgentInputBundle, result core.AgentResult) error {
	if !moduleSpecsUseServerPackage(bundle) {
		return nil
	}

	commandsPath := producedOutputPath(result, core.LKGlobalTestCommands)
	if commandsPath == "" {
		return fmt.Errorf("missing produced output %q", core.LKGlobalTestCommands)
	}
	commands, err := schema.ReadGlobalTestCommandsFile(commandsPath)
	if err != nil {
		return err
	}

	changed := false
	for i := range commands.Commands {
		normalized, ok := normalizeRootNPMCommandToServer(commands.Commands[i].Command)
		if !ok {
			continue
		}
		commands.Commands[i].Command = normalized
		changed = true
	}
	if !changed {
		return nil
	}
	if err := commands.Validate(); err != nil {
		return err
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(commands); err != nil {
		return err
	}
	return os.WriteFile(commandsPath, body.Bytes(), 0o644)
}

func moduleSpecsUseServerPackage(bundle core.AgentInputBundle) bool {
	moduleSpecs, err := rolecommon.ReadJSONArtifact[schema.ModuleSpecs](bundle, core.LKModuleSpecs)
	if err != nil {
		return false
	}
	if commandTargetsServer(moduleSpecs.GlobalTestCommand) {
		return true
	}
	for _, module := range moduleSpecs.Modules {
		if commandTargetsServer(module.TestCommand) {
			return true
		}
		for _, path := range append(append([]string{}, module.OwnedPaths...), module.RuntimeWritePaths...) {
			path = strings.Trim(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
			if path == "server/package.json" || path == "server/**" || strings.HasPrefix(path, "server/package.") {
				return true
			}
		}
	}
	return false
}

func commandTargetsServer(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	return command == "cd server" ||
		strings.HasPrefix(command, "cd server &&") ||
		strings.HasPrefix(command, "cd server;") ||
		strings.Contains(command, " server/package.json")
}

func normalizeRootNPMCommandToServer(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	switch strings.ToLower(trimmed) {
	case "npm install":
		return "cd server && npm install", true
	case "npm test":
		return "cd server && npm test", true
	case "npm run test":
		return "cd server && npm run test", true
	default:
		return command, false
	}
}

func producedOutputPath(result core.AgentResult, logicalKey string) string {
	for _, output := range result.Outputs {
		if output.LogicalKey == logicalKey && output.Status == "produced" && strings.TrimSpace(output.Path) != "" {
			return output.Path
		}
	}
	return ""
}

func bundleHasLogicalKey(bundle core.AgentInputBundle, logicalKey string) bool {
	for _, input := range bundle.Inputs {
		if input.LogicalKey == logicalKey {
			return true
		}
	}
	for _, previous := range bundle.PreviousOutputs {
		if previous.LogicalKey == logicalKey {
			return true
		}
	}
	return false
}

func (a *Agent) validateTestDataUpstream(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, bool, error) {
	requiredKeys := []string{
		core.LKContainerContext,
		core.LKModuleSpecs,
		core.LKArchitecturePlan,
		core.LKEnvironmentSpec,
	}
	for _, logicalKey := range requiredKeys {
		if !bundleHasLogicalKey(req.Bundle, logicalKey) {
			return core.AgentResult{}, false, nil
		}
	}

	container, err := readMergeInput[mergeContainerContext](req.Bundle, core.LKContainerContext)
	if err != nil {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is unreadable or invalid JSON: "+err.Error(), "architect.test_data cannot generate global test artifacts without a valid container_context.")
		return result, true, buildErr
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field repo_dir.", "architect.test_data cannot generate global test artifacts without repo_dir.")
		return result, true, buildErr
	}

	moduleSpecs, err := rolecommon.ReadJSONArtifact[schema.ModuleSpecs](req.Bundle, core.LKModuleSpecs)
	if err != nil {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json is unreadable or invalid JSON: "+err.Error(), "architect.test_data cannot derive global coverage without a valid module_specs artifact.")
		return result, true, buildErr
	}
	if len(moduleSpecs.Modules) == 0 {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json is missing required field modules.", "architect.test_data cannot derive global coverage without module_specs.modules.")
		return result, true, buildErr
	}
	if err := moduleSpecs.Validate(); err != nil {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json failed schema validation: "+err.Error(), "architect.test_data cannot derive global coverage from malformed module_specs.")
		return result, true, buildErr
	}

	architecturePlan, err := rolecommon.ReadArtifactContent(req.Bundle, core.LKArchitecturePlan)
	if err != nil {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKArchitecturePlan, "architecture_plan.md is unreadable: "+err.Error(), "architect.test_data cannot derive global acceptance scenarios without an architecture plan.")
		return result, true, buildErr
	}
	if strings.TrimSpace(architecturePlan) == "" {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKArchitecturePlan, "architecture_plan.md is empty.", "architect.test_data cannot derive global acceptance scenarios from an empty architecture plan.")
		return result, true, buildErr
	}

	environmentSpec, err := rolecommon.ReadJSONArtifact[schema.EnvironmentSpec](req.Bundle, core.LKEnvironmentSpec)
	if err != nil {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKEnvironmentSpec, "environment_spec.json is unreadable or invalid JSON: "+err.Error(), "architect.test_data cannot derive runnable test commands without a valid environment_spec.")
		return result, true, buildErr
	}
	if strings.TrimSpace(moduleSpecs.GlobalTestCommand) == "" && strings.TrimSpace(environmentSpec.DefaultTestCommand) == "" {
		result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKEnvironmentSpec, "environment_spec.default_test_command is empty and module_specs.global_test_command is absent.", "architect.test_data cannot produce runnable global test commands without any upstream default command.")
		return result, true, buildErr
	}

	if bundleHasLogicalKey(req.Bundle, core.LKMergedMainBranch) {
		merged, err := rolecommon.ReadJSONArtifact[architectMergedMainBranch](req.Bundle, core.LKMergedMainBranch)
		if err != nil {
			result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKMergedMainBranch, "merged_main_branch.json is unreadable or invalid JSON: "+err.Error(), "architect.test_data cannot enrich target commit information from a malformed merged_main_branch artifact.")
			return result, true, buildErr
		}
		if strings.TrimSpace(merged.MergedCommit) == "" {
			result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKMergedMainBranch, "merged_main_branch.json is missing required field merged_commit.", "architect.test_data cannot reference the merged target commit without merged_main_branch.merged_commit.")
			return result, true, buildErr
		}
		if strings.TrimSpace(merged.Result) == "" {
			result, buildErr := a.buildTestDataUpstreamIssue(ctx, req, core.LKMergedMainBranch, "merged_main_branch.json is missing required field result.", "architect.test_data cannot interpret the optional merged_main_branch artifact without merged_main_branch.result.")
			return result, true, buildErr
		}
	}
	return core.AgentResult{}, false, nil
}

func (a *Agent) buildTestDataUpstreamIssue(ctx context.Context, req core.AgentRunRequest, logicalKey, problem, whyBlocked string) (core.AgentResult, error) {
	return rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
		ProblemLogicalKey: logicalKey,
		ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, logicalKey, logicalKey+".json"),
		Problem:           problem,
		WhyBlocked:        whyBlocked,
		SuggestedRepair:   "Repair or rerun the upstream stage that produced this artifact.",
	})
}

type mergeContainerContext struct {
	ContainerID string `json:"container_id"`
	RepoDir     string `json:"repo_dir"`
	BaseBranch  string `json:"base_branch"`
}

type mergeModuleSpecs struct {
	Modules []mergeModuleSpec `json:"modules"`
}

type mergeModuleSpec struct {
	ModuleID   string `json:"module_id"`
	ModuleName string `json:"module_name"`
	ModuleRole string `json:"module_role"`
}

type mergeCoderBranch struct {
	ModuleID   string `json:"module_id"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Result     string `json:"result"`
	BaseBranch string `json:"base_branch"`
	BaseCommit string `json:"base_commit"`
	TestPassed *bool  `json:"test_passed"`
}

type mergeModuleReport struct {
	ModuleID   string `json:"module_id"`
	Result     string `json:"result"`
	TestPassed *bool  `json:"test_passed"`
}

func (a *Agent) runMergeCode(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	container, err := readMergeInput[mergeContainerContext](req.Bundle, core.LKContainerContext)
	if err != nil {
		return a.buildMergeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is unreadable or invalid JSON: "+err.Error(), "architect.merge_code cannot start without a valid container_context.")
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		return a.buildMergeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field repo_dir.", "architect.merge_code cannot merge without repo_dir.")
	}
	if strings.TrimSpace(container.BaseBranch) == "" {
		return a.buildMergeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field base_branch.", "architect.merge_code cannot merge without base_branch.")
	}
	moduleSpecs, err := readMergeInput[mergeModuleSpecs](req.Bundle, core.LKModuleSpecs)
	if err != nil {
		return a.buildMergeUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json is unreadable or invalid JSON: "+err.Error(), "architect.merge_code cannot resolve merge inputs without valid module_specs.")
	}
	if len(moduleSpecs.Modules) == 0 {
		return a.buildMergeUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json is missing required field modules.", "architect.merge_code cannot resolve merge inputs without module_specs.modules.")
	}

	moduleNames := map[string]string{}
	for _, module := range moduleSpecs.Modules {
		if module.ModuleID != "" {
			moduleNames[module.ModuleID] = module.ModuleName
		}
	}

	branches := make([]mergeCoderBranch, 0, len(moduleSpecs.Modules))
	reports := make([]mergeModuleReport, 0, len(moduleSpecs.Modules))
	for _, module := range moduleSpecs.Modules {
		moduleID := strings.TrimSpace(module.ModuleID)
		if moduleID == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.LKModuleSpecs, "module_specs.json contains a module with empty module_id.", "architect.merge_code cannot map per-module merge inputs without module_id.")
		}
		moduleLookupKeys := mergeModuleLookupKeys(module)
		branch, err := readMergeModuleInput[mergeCoderBranch](req.Bundle, moduleLookupKeys, []string{"code_bag", "front_code_bag", "backend_code_bag"}, core.LKCoderBranch, core.ModuleCoderBranchKey(moduleID))
		if err != nil {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is unreadable or invalid JSON: "+err.Error(), "architect.merge_code cannot continue without a valid coder_branch artifact.")
		}
		if strings.TrimSpace(branch.Branch) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field branch.", "architect.merge_code cannot continue without coder_branch.branch.")
		}
		if strings.TrimSpace(branch.Commit) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field commit.", "architect.merge_code cannot continue without coder_branch.commit.")
		}
		if strings.TrimSpace(branch.BaseBranch) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field base_branch.", "architect.merge_code cannot continue without coder_branch.base_branch.")
		}
		if strings.TrimSpace(branch.BaseCommit) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field base_commit.", "architect.merge_code cannot continue without coder_branch.base_commit.")
		}
		if strings.TrimSpace(branch.Result) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field result.", "architect.merge_code cannot continue without coder_branch.result.")
		}
		if branch.TestPassed == nil {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleCoderBranchKey(moduleID), core.ModuleCoderBranchKey(moduleID)+".json is missing required field test_passed.", "architect.merge_code cannot continue without coder_branch.test_passed.")
		}
		report, err := readMergeModuleInput[mergeModuleReport](req.Bundle, moduleLookupKeys, []string{"tested_module", "front_tested_module", "backend_tested_module"}, core.LKModuleTestReport, core.ModuleTestReportKey(moduleID))
		if err != nil {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleTestReportKey(moduleID), core.ModuleTestReportKey(moduleID)+".json is unreadable or invalid JSON: "+err.Error(), "architect.merge_code cannot continue without a valid module_test_report artifact.")
		}
		if strings.TrimSpace(report.Result) == "" {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleTestReportKey(moduleID), core.ModuleTestReportKey(moduleID)+".json is missing required field result.", "architect.merge_code cannot continue without module_test_report.result.")
		}
		if report.TestPassed == nil {
			return a.buildMergeUpstreamIssue(ctx, req, core.ModuleTestReportKey(moduleID), core.ModuleTestReportKey(moduleID)+".json is missing required field test_passed.", "architect.merge_code cannot continue without module_test_report.test_passed.")
		}
		branches = append(branches, branch)
		reports = append(reports, report)
	}

	issues, extras := validateMergePreconditions(container, branches, reports)
	if len(issues) > 0 {
		return a.writeMergeFailureOutputs(ctx, req, container, branches, moduleNames, "kfail", issues, "", extras)
	}

	cherryPickHandler, ok := req.Handlers.Get("container_git_cherry_pick")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("container_git_cherry_pick handler is required")
	}

	commits := make([]string, 0, len(branches))
	for _, branch := range branches {
		commits = append(commits, branch.Commit)
	}
	cherryPickResp, err := cherryPickHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"base_branch": container.BaseBranch,
			"commits":     commits,
		},
	})
	if err != nil {
		return a.writeMergeFailureOutputs(ctx, req, container, branches, moduleNames, "kbug", []string{"cherry-pick failed: " + err.Error()}, "", nil)
	}

	cherryResult, _ := cherryPickResp.Data["result"].(string)
	switch strings.ToLower(strings.TrimSpace(cherryResult)) {
	case "kok", "ok", "success":
		mergedCommit, _ := cherryPickResp.Data["merged_commit"].(string)
		return a.writeMergeSuccessOutputs(ctx, req, container, branches, moduleNames, commits, mergedCommit)
	case "conflict":
		failedCommit, _ := cherryPickResp.Data["failed_commit"].(string)
		issues := []string{"cherry-pick conflict: failed_commit=" + failedCommit}
		if stderr, _ := cherryPickResp.Data["stderr"].(string); strings.TrimSpace(stderr) != "" {
			issues = append(issues, "stderr: "+strings.TrimSpace(stderr))
		}
		return a.writeMergeFailureOutputs(ctx, req, container, branches, moduleNames, "kbug", issues, failedCommit, nil)
	default:
		return a.writeMergeFailureOutputs(ctx, req, container, branches, moduleNames, "kbug", []string{"unknown cherry-pick result: " + cherryResult}, "", nil)
	}
}

func validateMergePreconditions(container mergeContainerContext, branches []mergeCoderBranch, reports []mergeModuleReport) ([]string, map[string]any) {
	issues := []string{}
	baseCommitToModules := map[string][]string{}
	for i, report := range reports {
		branch := branches[i]
		reportPassed := report.TestPassed != nil && *report.TestPassed
		if report.Result != "kok" || !reportPassed {
			issues = append(issues, fmt.Sprintf("%s module_test_report result=%s, test_passed=%t", report.ModuleID, report.Result, reportPassed))
		}
		if strings.TrimSpace(branch.Result) != "" && !coderBranchAllowsMerge(branch) {
			issues = append(issues, fmt.Sprintf("%s coder_branch.result=%s", branch.ModuleID, branch.Result))
		}
		if branch.TestPassed != nil && !*branch.TestPassed {
			issues = append(issues, fmt.Sprintf("%s coder_branch.test_passed=false", branch.ModuleID))
		}
		if strings.TrimSpace(branch.Commit) == "" {
			issues = append(issues, fmt.Sprintf("%s missing commit", branch.ModuleID))
		}
		if strings.TrimSpace(branch.BaseBranch) == "" {
			issues = append(issues, fmt.Sprintf("%s missing coder_branch.base_branch", branch.ModuleID))
		} else if branch.BaseBranch != container.BaseBranch {
			issues = append(issues, fmt.Sprintf("%s coder_branch.base_branch=%s does not match container base_branch=%s", branch.ModuleID, branch.BaseBranch, container.BaseBranch))
		}
		if strings.TrimSpace(branch.BaseCommit) == "" {
			issues = append(issues, fmt.Sprintf("%s missing coder_branch.base_commit", branch.ModuleID))
		} else {
			baseCommitToModules[branch.BaseCommit] = append(baseCommitToModules[branch.BaseCommit], branch.ModuleID)
		}
	}
	return issues, mergeFailureExtras(branches, baseCommitToModules)
}

func coderBranchAllowsMerge(branch mergeCoderBranch) bool {
	switch strings.ToLower(strings.TrimSpace(branch.Result)) {
	case "kok":
		return true
	case "success", "ok":
		return branch.TestPassed != nil && *branch.TestPassed
	default:
		return false
	}
}

func (a *Agent) buildMergeUpstreamIssue(ctx context.Context, req core.AgentRunRequest, logicalKey, problem, whyBlocked string) (core.AgentResult, error) {
	return rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
		ProblemLogicalKey: logicalKey,
		ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, logicalKey, logicalKey+".json"),
		Problem:           problem,
		WhyBlocked:        whyBlocked,
		SuggestedRepair:   "Repair or rerun the upstream stage that produced this artifact.",
	})
}

func mergeFailureExtras(branches []mergeCoderBranch, baseCommitToModules map[string][]string) map[string]any {
	if len(baseCommitToModules) <= 1 {
		return nil
	}
	failedModules := make([]map[string]any, 0, len(branches))
	for _, branch := range branches {
		failedModules = append(failedModules, map[string]any{
			"module_id":   branch.ModuleID,
			"base_branch": branch.BaseBranch,
			"base_commit": branch.BaseCommit,
		})
	}
	return map[string]any{
		"merge_strategy": "blocked_by_base_commit_mismatch",
		"failed_reason":  "Coder branches were created from different base commits.",
		"failed_modules": failedModules,
	}
}

func (a *Agent) writeMergeSuccessOutputs(ctx context.Context, req core.AgentRunRequest, container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, commits []string, mergedCommit string) (core.AgentResult, error) {
	summary := buildMergeSuccessSummary(container, branches, moduleNames, mergedCommit)
	branchJSON := buildMergedMainBranchJSON(container, branches, moduleNames, "kok", mergedCommit, commits, nil)
	note := buildMergeSuccessNote(container, branches, moduleNames, mergedCommit)
	return writeMergeOutputs(ctx, req, summary, branchJSON, note, "")
}

func (a *Agent) writeMergeFailureOutputs(ctx context.Context, req core.AgentRunRequest, container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, resultCode string, issues []string, failedCommit string, extras map[string]any) (core.AgentResult, error) {
	summary := generateMergeIssueSummary(ctx, req.LLM, container, branches, moduleNames, issues)
	if extras == nil {
		extras = map[string]any{}
	}
	extras["issues"] = issues
	extras["failed_commit"] = failedCommit
	branchJSON := buildMergedMainBranchJSON(container, branches, moduleNames, resultCode, "", nil, extras)
	note := buildMergeFailureNote(container, branches, moduleNames, resultCode, issues)
	report := buildMergeCodeReport(container, branches, moduleNames, resultCode, issues, extras)
	return writeMergeOutputs(ctx, req, summary, branchJSON, note, report)
}

func readMergeInput[T any](bundle core.AgentInputBundle, logicalKey string) (T, error) {
	var out T
	for _, input := range bundle.Inputs {
		if input.LogicalKey != logicalKey || input.Path == "" {
			continue
		}
		return readMergeJSONFile[T](input.Path)
	}
	return out, fmt.Errorf("missing merge input %q", logicalKey)
}

func mergeModuleLookupKeys(module mergeModuleSpec) []string {
	moduleID := strings.TrimSpace(module.ModuleID)
	out := []string{}
	if moduleID != "" {
		out = append(out, moduleID)
	}
	if strings.TrimSpace(module.ModuleRole) == "frontend" && moduleID != "front" {
		out = append(out, "front")
	}
	return out
}

func readMergeModuleInput[T any](bundle core.AgentInputBundle, moduleKeys []string, bagNames []string, genericLogicalKey string, moduleLogicalKey string) (T, error) {
	if hasMergeInput(bundle, moduleLogicalKey) {
		return readMergeInput[T](bundle, moduleLogicalKey)
	}
	versionIDs := indexedBagVersionIDs(bundle, bagNames, moduleKeys)
	var out T
	if len(versionIDs) == 0 {
		return out, fmt.Errorf("missing merge input %q", moduleLogicalKey)
	}
	for _, input := range bundle.Inputs {
		if input.LogicalKey != genericLogicalKey || input.Path == "" || !versionIDs[input.ArtifactVersionID] {
			continue
		}
		return readMergeJSONFile[T](input.Path)
	}
	return out, fmt.Errorf("missing merge input %q", moduleLogicalKey)
}

func hasMergeInput(bundle core.AgentInputBundle, logicalKey string) bool {
	for _, input := range bundle.Inputs {
		if input.LogicalKey == logicalKey && input.Path != "" {
			return true
		}
	}
	return false
}

func indexedBagVersionIDs(bundle core.AgentInputBundle, bagNames []string, moduleKeys []string) map[string]bool {
	wanted := make(map[string]bool, len(bagNames))
	for _, bagName := range bagNames {
		if name := strings.TrimSpace(bagName); name != "" {
			wanted[name] = true
		}
	}
	wantedModules := make(map[string]bool, len(moduleKeys))
	for _, moduleKey := range moduleKeys {
		if key := strings.TrimSpace(moduleKey); key != "" {
			wantedModules[key] = true
		}
	}
	out := make(map[string]bool)
	for _, bag := range bundle.Bags {
		if !wanted[bag.Name] || !wantedModules[bag.Indexes["module_key"]] {
			continue
		}
		for _, versionID := range bag.ArtifactVersionIDs {
			if strings.TrimSpace(versionID) != "" {
				out[versionID] = true
			}
		}
	}
	return out
}

func readMergeJSONFile[T any](path string) (T, error) {
	var out T
	absPath, err := filepath.Abs(path)
	if err != nil {
		return out, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(content, &out); err != nil {
		return out, err
	}
	return out, nil
}

func writeMergeOutputs(ctx context.Context, req core.AgentRunRequest, summary string, branchJSON string, note string, report string) (core.AgentResult, error) {
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("artifact_write handler is required")
	}

	items := []struct {
		key     string
		content string
	}{
		{key: core.LKMergedCodeSummary, content: summary},
		{key: core.LKMergedMainBranch, content: branchJSON},
		{key: core.LKMergedMainBranchNote, content: note},
	}
	if _, ok := req.OpSpec.FindOutput(core.LKMergeCodeReport); ok && strings.TrimSpace(report) != "" {
		items = append(items, struct {
			key     string
			content string
		}{key: core.LKMergeCodeReport, content: report})
	}

	outputs := make([]core.AgentOutput, 0, len(items))
	for _, item := range items {
		resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
			Task:   req.Task,
			Bundle: req.Bundle,
			OpSpec: req.OpSpec,
			Args: map[string]any{
				"logical_key": item.key,
				"content":     item.content,
			},
		})
		if err != nil {
			return core.AgentResult{}, err
		}
		outputs = append(outputs, core.AgentOutput{
			LogicalKey:  item.key,
			ObjectType:  stringValue(resp.Data["object_type"]),
			ContentType: stringValue(resp.Data["content_type"]),
			Encoding:    stringValue(resp.Data["encoding"]),
			Status:      stringValue(resp.Data["status"]),
			Path:        stringValue(resp.Data["path"]),
			ArtifactURI: stringValue(resp.Data["artifact_uri"]),
		})
	}

	return core.AgentResult{
		Result:  "kok",
		Message: "architect merge_code completed",
		Outputs: outputs,
	}, nil
}

func buildMergeSuccessSummary(container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, mergedCommit string) string {
	return fmt.Sprintf("# Merge Code Summary\n\n- base_branch: %s\n- repo_dir: %s\n- merged_commit: %s\n- merged_modules: %s\n",
		container.BaseBranch,
		container.RepoDir,
		mergedCommit,
		formatMergeModuleList(branches, moduleNames, ", "),
	)
}

func buildMergeSuccessNote(container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, mergedCommit string) string {
	return fmt.Sprintf("# Merged Main Branch\n\nresult: kok\nbase_branch: %s\nmerged_commit: %s\nmodules:\n%s\n",
		container.BaseBranch,
		mergedCommit,
		formatMergeModuleBullets(branches, moduleNames),
	)
}

func buildMergeFailureNote(container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, resultCode string, issues []string) string {
	return fmt.Sprintf("# Merged Main Branch\n\nresult: %s\nbase_branch: %s\nmodules:\n%s\nissues:\n- %s\n",
		resultCode,
		container.BaseBranch,
		formatMergeModuleBullets(branches, moduleNames),
		strings.Join(issues, "\n- "),
	)
}

func buildMergeCodeReport(container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, resultCode string, issues []string, extras map[string]any) string {
	lines := []string{
		"# Merge Code Report",
		"",
		"## Result",
		"",
		resultCode,
		"",
		"## Base Branch",
		"",
		"- base_branch: " + container.BaseBranch,
		"",
		"## Modules",
		"",
	}
	for _, branch := range branches {
		lines = append(lines, "- "+branch.ModuleID+" ("+moduleNames[branch.ModuleID]+"): branch="+branch.Branch+", base_branch="+branch.BaseBranch+", base_commit="+branch.BaseCommit)
	}
	lines = append(lines, "", "## Issues", "")
	for _, issue := range issues {
		lines = append(lines, "- "+issue)
	}
	if failedReason, _ := extras["failed_reason"].(string); strings.TrimSpace(failedReason) != "" {
		lines = append(lines, "", "## Reason", "", failedReason)
	}
	if strategy, _ := extras["merge_strategy"].(string); strings.TrimSpace(strategy) != "" {
		lines = append(lines, "", "## Merge Strategy", "", strategy)
	}
	return strings.Join(lines, "\n") + "\n"
}

func buildMergedMainBranchJSON(container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, resultCode string, mergedCommit string, appliedCommits []string, extras map[string]any) string {
	modules := make([]map[string]any, 0, len(branches))
	for _, branch := range branches {
		modules = append(modules, map[string]any{
			"module_id":   branch.ModuleID,
			"module_name": moduleNames[branch.ModuleID],
			"branch":      branch.Branch,
			"commit":      branch.Commit,
			"base_branch": branch.BaseBranch,
			"base_commit": branch.BaseCommit,
		})
	}
	payload := map[string]any{
		"schema_version": 2,
		"kind":           "merged_main_branch",
		"result":         resultCode,
		"container_id":   container.ContainerID,
		"repo_dir":       container.RepoDir,
		"base_branch":    container.BaseBranch,
		"merged_commit":  mergedCommit,
		"modules":        modules,
	}
	if len(appliedCommits) > 0 {
		payload["applied_commits"] = appliedCommits
	}
	for key, value := range extras {
		payload[key] = value
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	return string(body) + "\n"
}

func generateMergeIssueSummary(ctx context.Context, llmClient core.LLMClientLike, container mergeContainerContext, branches []mergeCoderBranch, moduleNames map[string]string, issues []string) string {
	adapter, ok := llmClient.(llm.Adapter)
	if !ok || adapter == nil {
		return fallbackMergeIssueSummary(branches, moduleNames, issues)
	}
	prompt := "Write a concise Markdown summary explaining why architect.merge_code could not complete the real merge.\n" +
		"Context:\n" +
		"- base_branch: " + container.BaseBranch + "\n" +
		"- repo_dir: " + container.RepoDir + "\n" +
		"- modules: " + formatMergeModuleList(branches, moduleNames, ", ") + "\n" +
		"- issues:\n  - " + strings.Join(issues, "\n  - ") + "\n" +
		"Return only Markdown prose."
	resp, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	})
	if err != nil || strings.TrimSpace(resp.Message.Content) == "" {
		return fallbackMergeIssueSummary(branches, moduleNames, issues)
	}
	return resp.Message.Content
}

func fallbackMergeIssueSummary(branches []mergeCoderBranch, moduleNames map[string]string, issues []string) string {
	return "# Merge Issue Summary\n\nModules " + formatMergeModuleList(branches, moduleNames, ", ") + " could not be merged.\n- " + strings.Join(issues, "\n- ") + "\n"
}

func formatMergeModuleList(branches []mergeCoderBranch, moduleNames map[string]string, sep string) string {
	parts := make([]string, 0, len(branches))
	for _, branch := range branches {
		parts = append(parts, branch.ModuleID+" ("+moduleNames[branch.ModuleID]+")")
	}
	return strings.Join(parts, sep)
}

func formatMergeModuleBullets(branches []mergeCoderBranch, moduleNames map[string]string) string {
	lines := make([]string, 0, len(branches))
	for _, branch := range branches {
		lines = append(lines, "- "+branch.ModuleID+" "+moduleNames[branch.ModuleID])
	}
	return strings.Join(lines, "\n")
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func buildArchitecturePlan(pmPlanContent string) string {
	project := summarizePMPlan(pmPlanContent, 58)
	tech := technologyDescription(pmPlanContent)

	return fmt.Sprintf(`# Architecture Plan

## Project Positioning
%s

## Technology Direction
%s

## Runtime
Project code lives in one repository and can run locally or inside containers.

## Deliverables
Deliver runnable project code, core pages, and the main product capabilities.`, project, tech)
}

func summarizePMPlan(content string, maxRunes int) string {
	text := firstMeaningfulText(content)
	if text == "" {
		return "This project turns product requirements into a structured, deliverable software result."
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimRightFunc(string(runes[:maxRunes]), unicode.IsPunct) + "."
}

func firstMeaningfulText(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		parts = append(parts, line)
	}
	return strings.Join(parts, " ")
}

func technologyDescription(pmPlanContent string) string {
	techs := mentionedTechs(pmPlanContent)
	if len(techs) > 0 {
		return "Use the technologies mentioned in the PM plan: " + strings.Join(techs, ", ") + "."
	}
	return "Use a lightweight and general implementation approach focused on page rendering and core content flow."
}

func mentionedTechs(content string) []string {
	candidates := []string{"React", "Vue", "Node", "Static Web"}
	techs := []string{}
	for _, candidate := range candidates {
		if strings.Contains(content, candidate) {
			techs = append(techs, candidate)
		}
	}
	return techs
}

func agentFail(code, message string) core.AgentResult {
	return core.AgentResult{
		Result: "kfail",
		Errors: []core.AgentError{
			{Code: code, Message: message},
		},
	}
}
