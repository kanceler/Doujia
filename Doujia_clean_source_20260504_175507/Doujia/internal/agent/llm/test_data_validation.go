package llm

import (
	"fmt"
	"os"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
)

const maxArchitectTestDataRepairAttempts = 2

type validationOutcome struct {
	retryable bool
	message   string
}

func validateArchitectTestData(bundle core.AgentInputBundle, produced map[string]core.AgentOutput) (validationOutcome, bool, error) {
	if !hasProducedOutputs(produced, "global_test_data", "global_acceptance_tests", "global_test_commands") {
		return validationOutcome{}, false, nil
	}

	hasMergedMainBranch := bundleHasLogicalKey(bundle, "merged_main_branch")

	globalTestDataContent, err := readProducedOutputContent(produced["global_test_data"])
	if err != nil {
		return validationOutcome{}, true, err
	}
	if strings.TrimSpace(globalTestDataContent) == "" {
		return validationOutcome{retryable: true, message: "retryable: global_test_data must not be empty"}, true, nil
	}

	acceptance, err := schema.ReadGlobalAcceptanceTestsFile(produced["global_acceptance_tests"].Path)
	if err != nil {
		return validationOutcome{retryable: true, message: "retryable: global_acceptance_tests must be valid JSON"}, true, nil
	}
	if err := acceptance.Validate(hasMergedMainBranch); err != nil {
		if strings.Contains(err.Error(), "target_commit must be absent") {
			return validationOutcome{message: "fatal: " + err.Error()}, true, nil
		}
		return validationOutcome{retryable: true, message: "retryable: " + err.Error()}, true, nil
	}

	commands, err := schema.ReadGlobalTestCommandsFile(produced["global_test_commands"].Path)
	if err != nil {
		return validationOutcome{retryable: true, message: "retryable: global_test_commands must be valid JSON"}, true, nil
	}
	if err := commands.Validate(); err != nil {
		return validationOutcome{retryable: true, message: "retryable: " + err.Error()}, true, nil
	}

	return validationOutcome{}, true, nil
}

func hasProducedOutputs(produced map[string]core.AgentOutput, logicalKeys ...string) bool {
	for _, key := range logicalKeys {
		output, ok := produced[key]
		if !ok || output.Status != "produced" || output.Path == "" {
			return false
		}
	}
	return true
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

func readProducedOutputContent(output core.AgentOutput) (string, error) {
	if output.Path == "" {
		return "", fmt.Errorf("produced output %q has empty path", output.LogicalKey)
	}
	content, err := os.ReadFile(output.Path)
	if err != nil {
		return "", fmt.Errorf("read produced output %q: %w", output.LogicalKey, err)
	}
	return string(content), nil
}
