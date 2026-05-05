package tester

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"devflow/internal/agent/core"
	rolecommon "devflow/internal/agent/role/common"
	"devflow/internal/agent/schema"
)

type testerCoderBranch struct {
	ModuleID    string `json:"module_id"`
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	Worktree    string `json:"worktree"`
	Result      string `json:"result"`
	TestCommand string `json:"test_command"`
	TestPassed  bool   `json:"test_passed"`
}

type testerFullTestFiles struct {
	Kind        string          `json:"kind"`
	Files       json.RawMessage `json:"files"`
	TestCommand string          `json:"test_command"`
}

func (a *Agent) runTestCode(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, error) {
	if result, ok, err := a.validateTestCodeUpstream(ctx, req); err != nil {
		return core.AgentResult{}, err
	} else if ok {
		return result, nil
	}

	moduleSpecContent, _ := readBundleArtifactContent(req.Bundle, "module_spec")
	coderBranchContent, _ := readBundleArtifactContent(req.Bundle, "coder_branch")
	fullTestFilesContent, _ := readBundleArtifactContent(req.Bundle, "full_test_files")

	var moduleSpec schema.ModuleSpec
	_ = json.Unmarshal([]byte(moduleSpecContent), &moduleSpec)
	var coderBranch testerCoderBranch
	_ = json.Unmarshal([]byte(coderBranchContent), &coderBranch)
	var fullTestFiles testerFullTestFiles
	_ = json.Unmarshal([]byte(fullTestFilesContent), &fullTestFiles)

	testCommand := selectTestCommand(req.Task.ExecutionMode, fullTestFiles.TestCommand, coderBranch.TestCommand, moduleSpec.TestCommand)
	testCommand = normalizeModuleTestCommand(moduleSpec, testCommand)
	if strings.TrimSpace(testCommand) == "" {
		return a.writeTestCodeFailure(ctx, req, moduleSpec.ModuleID, coderBranch.Branch, coderBranch.Commit, "missing test_command")
	}

	runHandler, ok := req.Handlers.Get("container_run")
	if !ok {
		return core.AgentResult{}, fmt.Errorf("container_run handler is required")
	}
	runResp, err := runHandler.Handle(ctx, core.HandlerRequest{
		Task:   req.Task,
		Bundle: req.Bundle,
		OpSpec: req.OpSpec,
		Args: map[string]any{
			"command": testCommand,
		},
	})

	exitCode := intValue(runResp.Data["exit_code"])
	stdout := stringValue(runResp.Data["stdout"])
	stderr := stringValue(runResp.Data["stderr"])
	durationMs := int64Value(runResp.Data["duration_ms"])
	if err != nil {
		stderr = strings.TrimSpace(stderr + "\n" + err.Error())
		if exitCode == 0 {
			exitCode = 1
		}
	}

	testPassed := err == nil && exitCode == 0
	reportBody := buildModuleTestReportJSON(moduleSpec.ModuleID, coderBranch.Branch, coderBranch.Commit, testCommand, testPassed, exitCode, durationMs, stdout, stderr)
	reportOutput, writeErr := writeOutputArtifact(ctx, req, "module_test_report", reportBody)
	if writeErr != nil {
		return core.AgentResult{}, writeErr
	}

	if testPassed {
		return core.AgentResult{
			Result:  "kok",
			Message: "tester test_code completed",
			Outputs: []core.AgentOutput{reportOutput},
		}, nil
	}
	return core.AgentResult{
		Result:  "kbug",
		Message: "tester test_code failed",
		Outputs: []core.AgentOutput{reportOutput},
		Errors: []core.AgentError{
			{Code: "test_code_failed", Message: "module tests failed; see module_test_report"},
		},
	}, nil
}

func (a *Agent) validateTestCodeUpstream(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, bool, error) {
	moduleSpecContent, err := readBundleArtifactContent(req.Bundle, core.LKModuleSpec)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleSpec,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleSpec, "module_spec.json"),
			Problem:           "module_spec.json is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot determine the target module without a valid module_spec.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	var moduleSpec schema.ModuleSpec
	if err := json.Unmarshal([]byte(moduleSpecContent), &moduleSpec); err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleSpec,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleSpec, "module_spec.json"),
			Problem:           "module_spec.json is invalid JSON: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot determine the target module without a valid module_spec.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(moduleSpec.ModuleID) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field module_id.", "tester.test_code cannot attribute results without module_id.")
	}

	coderBranchContent, err := readBundleArtifactContent(req.Bundle, core.LKCoderBranch)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKCoderBranch,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKCoderBranch, "coder_branch.json"),
			Problem:           "coder_branch.json is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot run tests without a valid coder_branch artifact.",
			SuggestedRepair:   "Repair or rerun coder.write_code.",
		})
		return result, true, buildErr
	}
	var coderBranch testerCoderBranch
	if err := json.Unmarshal([]byte(coderBranchContent), &coderBranch); err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKCoderBranch,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKCoderBranch, "coder_branch.json"),
			Problem:           "coder_branch.json is invalid JSON: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot run tests without a valid coder_branch artifact.",
			SuggestedRepair:   "Repair or rerun coder.write_code.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(coderBranch.Commit) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKCoderBranch, "coder_branch.json is missing required field commit.", "tester.test_code cannot run tests without the tested commit.")
	}
	if strings.TrimSpace(coderBranch.Branch) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKCoderBranch, "coder_branch.json is missing required field branch.", "tester.test_code cannot report the tested branch without coder_branch.branch.")
	}
	if strings.TrimSpace(coderBranch.Worktree) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKCoderBranch, "coder_branch.json is missing required field worktree.", "tester.test_code cannot locate the module worktree without coder_branch.worktree.")
	}
	if strings.TrimSpace(coderBranch.Result) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKCoderBranch, "coder_branch.json is missing required field result.", "tester.test_code cannot determine whether the upstream coder result is runnable.")
	}
	if !coderBranchAllowsTesting(coderBranch) {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKCoderBranch, "coder_branch.result must be kok before tester.test_code can continue.", "tester.test_code cannot safely test a coder output that did not complete successfully.")
	}

	fullTestFilesContent, err := readBundleArtifactContent(req.Bundle, core.LKFullTestFiles)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKFullTestFiles,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKFullTestFiles, "full_test_files.json"),
			Problem:           "full_test_files.json is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot write or run module tests without full_test_files.",
			SuggestedRepair:   "Repair or rerun the upstream test generation stage.",
		})
		return result, true, buildErr
	}
	var fullTestFiles testerFullTestFiles
	if err := json.Unmarshal([]byte(fullTestFilesContent), &fullTestFiles); err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKFullTestFiles,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKFullTestFiles, "full_test_files.json"),
			Problem:           "full_test_files.json is invalid JSON: " + err.Error(),
			WhyBlocked:        "tester.test_code cannot write or run module tests without valid full_test_files.",
			SuggestedRepair:   "Repair or rerun the upstream test generation stage.",
		})
		return result, true, buildErr
	}
	fileCount, err := fullTestFileCount(fullTestFiles.Files)
	if err != nil {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKFullTestFiles, "full_test_files.json field files must be either an array or an object map.", "tester.test_code cannot write module tests without valid full_test_files.files.")
	}
	if fileCount == 0 {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKFullTestFiles, "full_test_files.json is missing required field files.", "tester.test_code cannot write module tests without full_test_files.files.")
	}
	if strings.TrimSpace(fullTestFiles.TestCommand) == "" {
		return a.buildTestCodeUpstreamIssue(ctx, req, core.LKFullTestFiles, "full_test_files.json is missing required field test_command.", "tester.test_code cannot execute module tests without full_test_files.test_command.")
	}

	return core.AgentResult{}, false, nil
}

func coderBranchAllowsTesting(coderBranch testerCoderBranch) bool {
	switch strings.ToLower(strings.TrimSpace(coderBranch.Result)) {
	case "kok":
		return true
	case "success":
		return coderBranch.TestPassed
	default:
		return false
	}
}

func fullTestFileCount(raw json.RawMessage) (int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	switch raw[0] {
	case '[':
		var files []json.RawMessage
		if err := json.Unmarshal(raw, &files); err != nil {
			return 0, err
		}
		return len(files), nil
	case '{':
		var files map[string]json.RawMessage
		if err := json.Unmarshal(raw, &files); err != nil {
			return 0, err
		}
		return len(files), nil
	default:
		return 0, fmt.Errorf("unsupported files shape")
	}
}

func selectTestCommand(executionMode, fullTestFilesCommand, coderBranchCommand, moduleSpecCommand string) string {
	if executionMode == core.ExecutionModeRepair {
		return firstNonEmpty(fullTestFilesCommand, coderBranchCommand, moduleSpecCommand)
	}
	// In normal execution, prefer the latest command emitted by coder/debug repair over stale test-data defaults.
	return firstNonEmpty(coderBranchCommand, fullTestFilesCommand, moduleSpecCommand)
}

func normalizeModuleTestCommand(moduleSpec schema.ModuleSpec, command string) string {
	command = strings.TrimSpace(command)
	if isBareNPMTestCommand(command) && isBackendServerModule(moduleSpec) {
		return "cd server && " + command
	}
	return command
}

func isBackendServerModule(moduleSpec schema.ModuleSpec) bool {
	if strings.TrimSpace(moduleSpec.ModuleRole) == "frontend" || strings.TrimSpace(moduleSpec.ImplementationRole) == "front" {
		return false
	}
	paths := append(append([]string{}, moduleSpec.OwnedPaths...), moduleSpec.RuntimeWritePaths...)
	for _, rawPath := range paths {
		path := strings.ReplaceAll(strings.TrimSpace(rawPath), "\\", "/")
		for strings.HasPrefix(path, "./") {
			path = strings.TrimPrefix(path, "./")
		}
		if path == "server/**" || strings.HasPrefix(path, "server/") {
			return true
		}
	}
	return false
}

func isBareNPMTestCommand(command string) bool {
	return strings.TrimSpace(command) == "npm test"
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

func (a *Agent) writeTestCodeFailure(ctx context.Context, req core.AgentRunRequest, moduleID, branch, commit, issue string) (core.AgentResult, error) {
	reportBody := buildModuleTestReportJSON(moduleID, branch, commit, "", false, 1, 0, "", issue)
	reportOutput, err := writeOutputArtifact(ctx, req, "module_test_report", reportBody)
	if err != nil {
		return core.AgentResult{}, err
	}
	return core.AgentResult{
		Result:  "kbug",
		Message: "tester test_code failed",
		Outputs: []core.AgentOutput{reportOutput},
		Errors: []core.AgentError{
			{Code: "test_code_failed", Message: "module tests failed; see module_test_report"},
		},
	}, nil
}

func buildModuleTestReportJSON(moduleID, branch, commit, testCommand string, testPassed bool, exitCode int, durationMs int64, stdout, stderr string) string {
	result := "kbug"
	if testPassed {
		result = "kok"
	}
	payload := map[string]any{
		"kind":          "module_test_report",
		"module_id":     moduleID,
		"result":        result,
		"test_passed":   testPassed,
		"tested_branch": branch,
		"tested_commit": commit,
		"test_command":  testCommand,
		"exit_code":     exitCode,
		"duration_ms":   durationMs,
		"stdout":        stdout,
		"stderr":        stderr,
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	return string(body) + "\n"
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
		ContentType: stringValue(resp.Data["content_type"]),
		Encoding:    stringValue(resp.Data["encoding"]),
		Status:      stringValue(resp.Data["status"]),
		Path:        stringValue(resp.Data["path"]),
		ArtifactURI: stringValue(resp.Data["artifact_uri"]),
	}, nil
}

func readBundleArtifactContent(bundle core.AgentInputBundle, logicalKey string) (string, error) {
	for i := len(bundle.Inputs) - 1; i >= 0; i-- {
		input := bundle.Inputs[i]
		if input.LogicalKey == logicalKey && input.Path != "" {
			data, err := osReadFile(input.Path)
			return data, err
		}
	}
	for i := len(bundle.PreviousOutputs) - 1; i >= 0; i-- {
		previous := bundle.PreviousOutputs[i]
		if previous.LogicalKey == logicalKey && previous.Path != "" {
			data, err := osReadFile(previous.Path)
			return data, err
		}
	}
	return "", fmt.Errorf("missing artifact %q", logicalKey)
}

func osReadFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
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
