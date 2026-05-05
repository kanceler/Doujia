package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doujia/internal/agent/core"
)

func ValidateInputs(task core.Task, bundle core.AgentInputBundle, spec core.OpSpec) error {
	rule, ok := spec.ModeInputRules[task.ExecutionMode]
	if !ok {
		return fmt.Errorf("unknown execution_mode %q", task.ExecutionMode)
	}

	inputs := map[string]core.InputArtifact{}
	for _, input := range bundle.Inputs {
		inputs[input.LogicalKey] = input
	}

	required := append([]core.InputRequirement{}, spec.BaseRequiredInputs...)
	required = append(required, rule.ExtraRequiredInputs...)
	for _, req := range required {
		input, ok := inputs[req.LogicalKey]
		if !ok || input.Path == "" {
			return fmt.Errorf("missing required input %q", req.LogicalKey)
		}
	}

	return nil
}

func ValidateOutputs(_ core.Task, bundle core.AgentInputBundle, spec core.OpSpec, result core.AgentResult) error {
	expectedByKey := map[string]core.OutputSpec{}
	for _, output := range spec.ExpectedOutputs {
		expectedByKey[output.LogicalKey] = output
	}

	outputsByKey := map[string]core.AgentOutput{}
	enforceRequiredOutputs := result.Result == "kok"
	for _, output := range result.Outputs {
		expected, ok := expectedByKey[output.LogicalKey]
		if !ok {
			return fmt.Errorf("unexpected output %q", output.LogicalKey)
		}
		if _, exists := outputsByKey[output.LogicalKey]; exists {
			return fmt.Errorf("duplicate output %q", output.LogicalKey)
		}
		if output.ObjectType != expected.ObjectType {
			return fmt.Errorf("output %q object_type mismatch: want %q got %q", output.LogicalKey, expected.ObjectType, output.ObjectType)
		}
		switch output.Status {
		case "produced":
			if output.Path == "" {
				return fmt.Errorf("produced output %q has empty path", output.LogicalKey)
			}
			if !isInsideDir(bundle.OutputDir, output.Path) {
				return fmt.Errorf("output %q path %q is outside output_dir %q", output.LogicalKey, output.Path, bundle.OutputDir)
			}
			if _, err := os.Stat(output.Path); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("produced output %q file does not exist: %s", output.LogicalKey, output.Path)
				}
				return fmt.Errorf("produced output %q file is not accessible: %w", output.LogicalKey, err)
			}
			if strings.TrimSpace(bundle.OutputURIBase) != "" && strings.TrimSpace(output.ArtifactURI) == "" {
				return fmt.Errorf("produced output %q has empty artifact_uri", output.LogicalKey)
			}
			if enforceRequiredOutputs {
				expectedPath, err := expectedProducedPath(bundle, expected)
				if err != nil {
					return fmt.Errorf("output %q has invalid expected path: %w", output.LogicalKey, err)
				}
				if filepath.Clean(output.Path) != filepath.Clean(expectedPath) {
					return fmt.Errorf("output %q path mismatch: want %q got %q", output.LogicalKey, expectedPath, output.Path)
				}
			}
		case "reused":
			if strings.TrimSpace(output.ArtifactVersionID) == "" {
				return fmt.Errorf("reused output %q has empty artifact_version_id", output.LogicalKey)
			}
		default:
			return fmt.Errorf("output %q has invalid status %q", output.LogicalKey, output.Status)
		}
		outputsByKey[output.LogicalKey] = output
	}

	if enforceRequiredOutputs {
		for _, expected := range spec.ExpectedOutputs {
			output, ok := outputsByKey[expected.LogicalKey]
			if !ok {
				if expected.Required {
					return fmt.Errorf("missing required output %q", expected.LogicalKey)
				}
				continue
			}
			if output.ObjectType != expected.ObjectType {
				return fmt.Errorf("output %q object_type mismatch: want %q got %q", expected.LogicalKey, expected.ObjectType, output.ObjectType)
			}
		}
	}

	return nil
}

func expectedProducedPath(bundle core.AgentInputBundle, expected core.OutputSpec) (string, error) {
	if expected.FileName == "" {
		return "", fmt.Errorf("file_name is empty")
	}
	if filepath.IsAbs(expected.FileName) {
		return "", fmt.Errorf("file_name %q must be relative", expected.FileName)
	}
	path := filepath.Join(bundle.OutputDir, expected.FileName)
	if !isInsideDir(bundle.OutputDir, path) {
		return "", fmt.Errorf("file_name %q escapes output_dir", expected.FileName)
	}
	return path, nil
}

func isInsideDir(parent, child string) bool {
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	childAbs, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(parentAbs, childAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}

func BuildReuseResult(_ core.Task, bundle core.AgentInputBundle, spec core.OpSpec) (core.AgentResult, error) {
	previousByKey := map[string]core.PreviousOutputRef{}
	for _, previous := range bundle.PreviousOutputs {
		previousByKey[previous.LogicalKey] = previous
	}

	outputs := []core.AgentOutput{}
	for _, expected := range spec.ExpectedOutputs {
		previous, ok := previousByKey[expected.LogicalKey]
		if !ok {
			if expected.Required {
				return core.AgentResult{}, fmt.Errorf("missing required previous output %q", expected.LogicalKey)
			}
			continue
		}
		artifactVersionID := strings.TrimSpace(previous.ArtifactVersionID)
		if artifactVersionID == "" {
			return core.AgentResult{}, fmt.Errorf("previous output %q has empty artifact_version_id", expected.LogicalKey)
		}
		outputs = append(outputs, core.AgentOutput{
			LogicalKey:        expected.LogicalKey,
			ObjectType:        expected.ObjectType,
			Status:            "reused",
			Path:              previous.Path,
			ArtifactURI:       previous.ArtifactURI,
			ArtifactVersionID: artifactVersionID,
			Description:       previous.Description,
		})
	}

	return core.AgentResult{
		Result:  "kok",
		Message: "reuse completed",
		Outputs: outputs,
	}, nil
}
