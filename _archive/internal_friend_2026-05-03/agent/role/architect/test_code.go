package architect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"doujia/internal/agent/core"
	rolecommon "doujia/internal/agent/role/common"
	"doujia/internal/agent/schema"
)

type architectMergedMainBranch struct {
	Result       string `json:"result"`
	MergedCommit string `json:"merged_commit"`
	BaseBranch   string `json:"base_branch"`
}

func (a *Agent) runTestCode(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	if result, ok, err := a.validateTestCodeUpstream(ctx, req); err != nil {
		return core.AgentResult{}, err
	} else if ok {
		return result, nil
	}

	commandsContent, err := readBundleArtifactContent(req.Bundle, "global_test_commands")
	if err != nil {
		return core.AgentResult{}, err
	}
	mergedContent, _ := readBundleArtifactContent(req.Bundle, "merged_main_branch")
	containerContent, _ := readBundleArtifactContent(req.Bundle, "container_context")

	var commands schema.GlobalTestCommands
	if err := json.Unmarshal([]byte(commandsContent), &commands); err != nil {
		return core.AgentResult{}, err
	}
	if err := commands.Validate(); err != nil {
		return core.AgentResult{}, err
	}
	var merged architectMergedMainBranch
	_ = json.Unmarshal([]byte(mergedContent), &merged)

	execHandler, ok := req.Handlers.Get("container_exec")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("container_exec handler is required")
	}

	commandReports := make([]map[string]any, 0, len(commands.Commands))
	allPassed := true
	for _, cmd := range commands.Commands {
		resp, execErr := execHandler.Handle(ctx, core.HandlerRequest{
			Task:   req.Task,
			Bundle: req.Bundle,
			OpSpec: req.OpSpec,
			Args: map[string]any{
				"command": cmd.Command,
			},
		})
		exitCode := intValue(resp.Data["exit_code"])
		stdout := stringValue(resp.Data["stdout"])
		stderr := stringValue(resp.Data["stderr"])
		durationMs := int64Value(resp.Data["duration_ms"])
		if execErr != nil {
			allPassed = false
			stderr = strings.TrimSpace(stderr + "\n" + execErr.Error())
			if exitCode == 0 {
				exitCode = 1
			}
		} else if exitCode != 0 {
			allPassed = false
		}
		commandReports = append(commandReports, map[string]any{
			"name":        cmd.Name,
			"command":     cmd.Command,
			"cwd_from":    cmd.CwdFrom,
			"exit_code":   exitCode,
			"stdout":      stdout,
			"stderr":      stderr,
			"duration_ms": durationMs,
		})
	}

	reportBody := buildGlobalTestReportJSON(merged, commandReports, allPassed)
	reportOutput, err := writeOutputArtifact(ctx, req, "global_test_report", reportBody)
	if err != nil {
		return core.AgentResult{}, err
	}
	deliveryGuide := buildDeliveryGuide(containerContent, merged, commandReports, allPassed)
	deliveryOutput, err := writeOutputArtifact(ctx, req, "delivery_guide", deliveryGuide)
	if err != nil {
		return core.AgentResult{}, err
	}

	if allPassed {
		return core.AgentResult{
			Result:  "kok",
			Message: "architect test_code completed",
			Outputs: []core.AgentOutput{reportOutput, deliveryOutput},
		}, nil
	}
	return core.AgentResult{
		Result:  "kfail",
		Message: "architect test_code failed",
		Outputs: []core.AgentOutput{reportOutput, deliveryOutput},
		Errors: []core.AgentError{
			{Code: "test_code_failed", Message: "global acceptance tests failed; see global_test_report"},
		},
	}, nil
}

func (a *Agent) validateTestCodeUpstream(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, bool, error) {
	container, err := rolecommon.ReadJSONArtifact[mergeContainerContext](req.Bundle, core.LKContainerContext)
	if err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is unreadable or invalid JSON: "+err.Error(), "architect.test_code cannot execute global acceptance commands without a valid container_context.")
	}
	if strings.TrimSpace(container.RepoDir) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKContainerContext, "container_context.json is missing required field repo_dir.", "architect.test_code cannot execute global acceptance commands without repo_dir.")
	}

	merged, err := rolecommon.ReadJSONArtifact[architectMergedMainBranch](req.Bundle, core.LKMergedMainBranch)
	if err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKMergedMainBranch, "merged_main_branch.json is unreadable or invalid JSON: "+err.Error(), "architect.test_code cannot identify the tested revision without a valid merged_main_branch artifact.")
	}
	if strings.TrimSpace(merged.MergedCommit) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKMergedMainBranch, "merged_main_branch.json is missing required field merged_commit.", "architect.test_code cannot identify the tested revision without merged_main_branch.merged_commit.")
	}

	acceptance, err := rolecommon.ReadJSONArtifact[schema.GlobalAcceptanceTests](req.Bundle, core.LKGlobalAcceptanceTests)
	if err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKGlobalAcceptanceTests, "global_acceptance_tests.json is unreadable or invalid JSON: "+err.Error(), "architect.test_code cannot determine required acceptance coverage without a valid global_acceptance_tests artifact.")
	}
	if err := acceptance.Validate(true); err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKGlobalAcceptanceTests, "global_acceptance_tests.json failed schema validation: "+err.Error(), "architect.test_code cannot determine required acceptance coverage without global_acceptance_tests.scenarios.")
	}

	commands, err := rolecommon.ReadJSONArtifact[schema.GlobalTestCommands](req.Bundle, core.LKGlobalTestCommands)
	if err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKGlobalTestCommands, "global_test_commands.json is unreadable or invalid JSON: "+err.Error(), "architect.test_code cannot execute acceptance commands without a valid global_test_commands artifact.")
	}
	if err := commands.Validate(); err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKGlobalTestCommands, "global_test_commands.json failed schema validation: "+err.Error(), "architect.test_code cannot execute acceptance commands without global_test_commands.commands.")
	}
	return core.AgentResult{}, false, nil
}

func (a *Agent) buildTestCodeUpstreamIssue(ctx context.Context, req core.AgentRunRequest, logicalKey, problem, whyBlocked string) (core.AgentResult, bool, error) {
	result, err := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
		ProblemLogicalKey: logicalKey,
		ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, logicalKey, logicalKey+".json"),
		Problem:           problem,
		WhyBlocked:        whyBlocked,
		SuggestedRepair:   "Repair or rerun the upstream stage that produced this artifact.",
	})
	return result, true, err
}

func buildGlobalTestReportJSON(merged architectMergedMainBranch, commands []map[string]any, allPassed bool) string {
	result := "kfail"
	if allPassed {
		result = "kok"
	}
	payload := map[string]any{
		"kind":          "global_test_report",
		"result":        result,
		"test_passed":   allPassed,
		"tested_branch": merged.BaseBranch,
		"tested_commit": merged.MergedCommit,
		"commands":      commands,
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	return string(body) + "\n"
}

func buildDeliveryGuide(containerContent string, merged architectMergedMainBranch, commands []map[string]any, allPassed bool) string {
	status := "failed"
	if allPassed {
		status = "passed"
	}
	lines := []string{
		"# 项目交付说明",
		"",
		"## 验收结果",
		"",
		"- result: " + status,
		"- tested_branch: " + merged.BaseBranch,
		"- tested_commit: " + merged.MergedCommit,
		"",
		"## 测试命令",
		"",
	}
	for _, command := range commands {
		lines = append(lines, "- "+stringValue(command["name"])+": `"+stringValue(command["command"])+"`")
	}
	if strings.TrimSpace(containerContent) != "" {
		lines = append(lines, "", "## 容器上下文", "", "```json", strings.TrimSpace(containerContent), "```")
	}
	return strings.Join(lines, "\n") + "\n"
}

func readBundleArtifactContent(bundle core.AgentInputBundle, logicalKey string) (string, error) {
	for _, input := range bundle.Inputs {
		if input.LogicalKey == logicalKey && input.Path != "" {
			body, err := os.ReadFile(input.Path)
			if err != nil {
				return "", err
			}
			return string(body), nil
		}
	}
	for _, previous := range bundle.PreviousOutputs {
		if previous.LogicalKey == logicalKey && previous.Path != "" {
			body, err := os.ReadFile(previous.Path)
			if err != nil {
				return "", err
			}
			return string(body), nil
		}
	}
	return "", fmt.Errorf("missing artifact %q", logicalKey)
}

func writeOutputArtifact(ctx context.Context, req core.AgentRunRequest, logicalKey, content string) (core.AgentOutput, error) {
	writeHandler, ok := req.Handlers.Get("artifact_write")
	if !ok {
		return core.AgentOutput{}, fmt.Errorf("artifact_write handler is required")
	}
	resp, err := writeHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"logical_key": logicalKey,
			"content":     content,
		},
	})
	if err != nil {
		return core.AgentOutput{}, err
	}
	return core.AgentOutput{
		LogicalKey:  logicalKey,
		ObjectType:  stringValue(resp.Data["object_type"]),
		Status:      stringValue(resp.Data["status"]),
		Path:        stringValue(resp.Data["path"]),
		ArtifactURI: stringValue(resp.Data["artifact_uri"]),
	}, nil
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}
