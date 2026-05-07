package tester

import (
	"context"
	"encoding/json"
	"strings"

	"devflow/internal/agent/core"
	rolecommon "devflow/internal/agent/role/common"
	"devflow/internal/agent/schema"
)

func (a *Agent) validateTestDataUpstream(ctx context.Context, req core.AgentRunRequest) (core.AgentResult, bool, error) {
	moduleSpecContent, err := readBundleArtifactContent(req.Bundle, core.LKModuleSpec)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleSpec,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleSpec, "module_spec.json"),
			Problem:           "module_spec.json is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_data cannot generate test artifacts without a valid module_spec.",
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
			WhyBlocked:        "tester.test_data cannot generate test artifacts without a valid module_spec.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(moduleSpec.ModuleID) == "" {
		return a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field module_id.", "tester.test_data cannot scope module test artifacts without module_id.")
	}
	if strings.TrimSpace(moduleSpec.TestCommand) == "" {
		return a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleSpec, "module_spec.json is missing required field test_command.", "tester.test_data cannot prepare runnable test artifacts without test_command.")
	}

	contractContent, err := readBundleArtifactContent(req.Bundle, core.LKModuleContract)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleContract,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleContract, "module_contract.json"),
			Problem:           "module_contract artifact is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_data cannot validate module boundaries without a readable module_contract artifact.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	var contract map[string]any
	if err := json.Unmarshal([]byte(contractContent), &contract); err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKModuleContract,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKModuleContract, "module_contract.json"),
			Problem:           "module_contract artifact is invalid JSON: " + err.Error(),
			WhyBlocked:        "tester.test_data cannot validate module boundaries without a valid module_contract artifact.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	if strings.TrimSpace(mapStringValue(contract, "module_id")) == "" && strings.TrimSpace(mapStringValue(contract, "kind")) == "" {
		return a.buildTestDataUpstreamIssue(ctx, req, core.LKModuleContract, "module_contract artifact is missing basic structure such as module_id or kind.", "tester.test_data cannot trust module boundaries without basic module_contract structure.")
	}

	seedTestsContent, err := readBundleArtifactContent(req.Bundle, core.LKSeedTests)
	if err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKSeedTests,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKSeedTests, "seed_tests.json"),
			Problem:           "seed_tests.json is unreadable: " + err.Error(),
			WhyBlocked:        "tester.test_data cannot expand seed tests without a readable seed_tests artifact.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	var seedTests map[string]any
	if err := json.Unmarshal([]byte(seedTestsContent), &seedTests); err != nil {
		result, buildErr := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
			ProblemLogicalKey: core.LKSeedTests,
			ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, core.LKSeedTests, "seed_tests.json"),
			Problem:           "seed_tests.json is invalid JSON: " + err.Error(),
			WhyBlocked:        "tester.test_data cannot expand seed tests without a valid seed_tests artifact.",
			SuggestedRepair:   "Repair or rerun architect.split_module.",
		})
		return result, true, buildErr
	}
	if !hasNonEmptyCollection(seedTests, "test_files") && !hasNonEmptyCollection(seedTests, "files") {
		return a.buildTestDataUpstreamIssue(ctx, req, core.LKSeedTests, "seed_tests.json is missing required field test_files/files.", "tester.test_data cannot expand or reuse seed test files without seed_tests.test_files or seed_tests.files.")
	}
	if strings.TrimSpace(mapStringValue(seedTests, "test_command")) == "" {
		return a.buildTestDataUpstreamIssue(ctx, req, core.LKSeedTests, "seed_tests.json is missing required field test_command.", "tester.test_data cannot prepare runnable test bundles without seed_tests.test_command.")
	}

	return core.AgentResult{}, false, nil
}

func (a *Agent) buildTestDataUpstreamIssue(ctx context.Context, req core.AgentRunRequest, logicalKey, problem, whyBlocked string) (core.AgentResult, bool, error) {
	result, err := rolecommon.BuildUpstreamArtifactIssueResult(ctx, req, rolecommon.UpstreamArtifactIssueParams{
		ProblemLogicalKey: logicalKey,
		ProblemFile:       rolecommon.ArtifactFileName(req.Bundle, logicalKey, logicalKey+".json"),
		Problem:           problem,
		WhyBlocked:        whyBlocked,
		SuggestedRepair:   "Repair or rerun the upstream stage that produced this artifact.",
	})
	return result, true, err
}

func mapStringValue(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func hasNonEmptyCollection(m map[string]any, key string) bool {
	value, ok := m[key]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return false
	}
}
