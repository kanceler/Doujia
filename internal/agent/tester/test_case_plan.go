package tester

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"devflow/internal/agent/common"
)

type TestCasePlan struct {
	SchemaVersion int        `json:"schema_version"`
	Kind          string     `json:"kind"`
	ModuleID      string     `json:"module_id"`
	Cases         []TestCase `json:"cases"`
}

type TestCase struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Target   string `json:"target"`
	Scenario string `json:"scenario"`
	Expected string `json:"expected"`
}

var testCaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func parseTestCasePlan(raw string) (TestCasePlan, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()

	var plan TestCasePlan
	if err := decoder.Decode(&plan); err != nil {
		return TestCasePlan{}, fmt.Errorf("parse test_case_plan json: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return TestCasePlan{}, err
	}
	return plan, nil
}

func validateTestCasePlan(plan TestCasePlan, contract moduleContract, seedBundle common.TestFileBundle) []TestCase {
	if plan.SchemaVersion != 1 {
		return nil
	}
	if strings.TrimSpace(plan.Kind) != "test_case_plan" {
		return nil
	}
	contractModuleID := strings.TrimSpace(contract.ModuleID)
	seedModuleID := strings.TrimSpace(seedBundle.ModuleID)
	if contractModuleID == "" || seedModuleID == "" {
		return nil
	}
	if contractModuleID != seedModuleID {
		return nil
	}
	if strings.TrimSpace(plan.ModuleID) != contractModuleID {
		return nil
	}

	allowedTargets := make(map[string]struct{}, len(contract.PublicAPI))
	for _, api := range contract.PublicAPI {
		name := strings.TrimSpace(api.Name)
		if name == "" {
			continue
		}
		allowedTargets[name] = struct{}{}
	}

	valid := make([]TestCase, 0, len(plan.Cases))
	for _, candidate := range plan.Cases {
		if len(valid) >= 8 {
			break
		}
		if !isAllowedCaseType(candidate.Type) {
			continue
		}
		if _, ok := allowedTargets[strings.TrimSpace(candidate.Target)]; !ok {
			continue
		}
		if strings.TrimSpace(candidate.Name) == "" ||
			strings.TrimSpace(candidate.Scenario) == "" ||
			strings.TrimSpace(candidate.Expected) == "" {
			continue
		}
		if !testCaseNamePattern.MatchString(candidate.Name) {
			continue
		}
		valid = append(valid, TestCase{
			Name:     strings.TrimSpace(candidate.Name),
			Type:     strings.TrimSpace(candidate.Type),
			Target:   strings.TrimSpace(candidate.Target),
			Scenario: strings.TrimSpace(candidate.Scenario),
			Expected: strings.TrimSpace(candidate.Expected),
		})
	}
	return valid
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("parse test_case_plan json: %w", err)
	}
	return fmt.Errorf("parse test_case_plan json: trailing content is not allowed")
}

func isAllowedCaseType(value string) bool {
	switch strings.TrimSpace(value) {
	case "boundary", "error", "state", "integration":
		return true
	default:
		return false
	}
}
