package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devflow/internal/agent/core"
	"devflow/internal/agent/schema"
)

func ValidateInputs(task core.Task, bundle core.AgentInputBundle, spec core.OpSpec) error {
	rule, ok := spec.ModeInputRules[task.ExecutionMode]
	if !ok {
		return fmt.Errorf("unknown execution_mode %q", task.ExecutionMode)
	}
	if len(spec.InputBags) > 0 {
		if err := validateInputBags(bundle, spec.InputBags); err != nil {
			return err
		}
		if len(rule.ExtraRequiredInputs) == 0 {
			return nil
		}
		inputs := map[string]core.InputArtifact{}
		for _, input := range bundle.Inputs {
			inputs[input.LogicalKey] = input
		}
		for _, req := range rule.ExtraRequiredInputs {
			input, ok := inputs[req.LogicalKey]
			if !ok || input.Path == "" {
				return fmt.Errorf("missing required input %q", req.LogicalKey)
			}
		}
		return nil
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
		if expected.ContentType != "" && output.ContentType != expected.ContentType {
			return fmt.Errorf("output %q content_type mismatch: want %q got %q", output.LogicalKey, expected.ContentType, output.ContentType)
		}
		if expected.Encoding != "" && output.Encoding != expected.Encoding {
			return fmt.Errorf("output %q encoding mismatch: want %q got %q", output.LogicalKey, expected.Encoding, output.Encoding)
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
			if err := validateProducedOutputContent(output); err != nil {
				return err
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
			if expected.ContentType != "" && output.ContentType != expected.ContentType {
				return fmt.Errorf("output %q content_type mismatch: want %q got %q", expected.LogicalKey, expected.ContentType, output.ContentType)
			}
			if expected.Encoding != "" && output.Encoding != expected.Encoding {
				return fmt.Errorf("output %q encoding mismatch: want %q got %q", expected.LogicalKey, expected.Encoding, output.Encoding)
			}
		}
	}
	if len(spec.OutputBags) > 0 {
		if err := validateOutputBags(result, spec.OutputBags); err != nil {
			return err
		}
	}

	return nil
}

func validateInputBags(bundle core.AgentInputBundle, specs []core.InputBagSpec) error {
	bagsByName := map[string][]core.AgentInputBag{}
	for _, bag := range bundle.Bags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			continue
		}
		bagsByName[name] = append(bagsByName[name], bag)
	}
	versionKeys := versionLogicalKeysByID(bundle)
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			return fmt.Errorf("input bag contract has empty name")
		}
		items := bagsByName[name]
		if len(items) == 0 {
			if spec.Required {
				return fmt.Errorf("missing required input bag %q", name)
			}
			continue
		}
		if !spec.Collection && len(items) > 1 {
			return fmt.Errorf("input bag %q is not a collection but got %d items", name, len(items))
		}
		if err := rejectDuplicateBagIndexes("input", name, items); err != nil {
			return err
		}
		for _, item := range items {
			members := map[string]bool{}
			for _, versionID := range item.ArtifactVersionIDs {
				for _, key := range versionKeys[versionID] {
					members[key] = true
				}
			}
			for _, req := range spec.Members {
				if !req.Required {
					continue
				}
				if !members[req.LogicalKey] {
					return fmt.Errorf("input bag %q missing required member %q", name, req.LogicalKey)
				}
			}
		}
	}
	return nil
}

func validateOutputBags(result core.AgentResult, specs []core.OutputBagSpec) error {
	specsByName := map[string]core.OutputBagSpec{}
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			return fmt.Errorf("output bag contract has empty name")
		}
		if _, exists := specsByName[name]; exists {
			return fmt.Errorf("duplicate output bag contract %q", name)
		}
		specsByName[name] = spec
	}
	producedByName := map[string][]core.ProducedBagManifest{}
	for _, bag := range result.ProducedBags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			return fmt.Errorf("produced bag name is required")
		}
		spec, ok := specsByName[name]
		if !ok {
			return fmt.Errorf("produced bag %q is not declared by output contract", name)
		}
		producedByName[name] = append(producedByName[name], bag)
		if !spec.Collection && len(producedByName[name]) > 1 {
			return fmt.Errorf("output bag %q is not a collection but got %d items", name, len(producedByName[name]))
		}
	}
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		items := producedByName[name]
		if len(items) == 0 {
			if spec.Required && result.Result == "kok" {
				return fmt.Errorf("missing required output bag %q", name)
			}
			continue
		}
		if err := rejectDuplicateProducedBagIndexes(name, items); err != nil {
			return err
		}
		for _, item := range items {
			members := map[string]bool{}
			for _, member := range item.Members {
				if key := strings.TrimSpace(member.LogicalKey); key != "" {
					members[key] = true
				}
			}
			for _, req := range spec.Members {
				if !req.Required {
					continue
				}
				if !members[req.LogicalKey] {
					return fmt.Errorf("output bag %q missing required member %q", name, req.LogicalKey)
				}
			}
		}
	}
	return nil
}

func versionLogicalKeysByID(bundle core.AgentInputBundle) map[string][]string {
	out := map[string][]string{}
	for _, version := range bundle.Versions {
		if version.ArtifactVersionID == "" || version.LogicalKey == "" {
			continue
		}
		out[version.ArtifactVersionID] = append(out[version.ArtifactVersionID], version.LogicalKey)
	}
	for _, input := range bundle.Inputs {
		if input.ArtifactVersionID == "" || input.LogicalKey == "" {
			continue
		}
		out[input.ArtifactVersionID] = append(out[input.ArtifactVersionID], input.LogicalKey)
	}
	return out
}

func rejectDuplicateBagIndexes(kind, name string, bags []core.AgentInputBag) error {
	seen := map[string]bool{}
	for _, bag := range bags {
		key := bagIndexKey(bag.Indexes)
		if seen[key] {
			return fmt.Errorf("%s bag %q has duplicate indexes %s", kind, name, key)
		}
		seen[key] = true
	}
	return nil
}

func rejectDuplicateProducedBagIndexes(name string, bags []core.ProducedBagManifest) error {
	seen := map[string]bool{}
	for _, bag := range bags {
		key := bagIndexKey(bag.Indexes)
		if seen[key] {
			return fmt.Errorf("output bag %q has duplicate indexes %s", name, key)
		}
		seen[key] = true
	}
	return nil
}

func bagIndexKey(indexes map[string]string) string {
	if len(indexes) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(indexes))
	for key, value := range indexes {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return "{}"
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strings.TrimSpace(indexes[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func validateProducedOutputContent(output core.AgentOutput) error {
	if !strings.EqualFold(strings.TrimSpace(output.ObjectType), "json") {
		return nil
	}
	return schema.ValidateJSONArtifactFile(output.LogicalKey, output.Path)
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
			ContentType:       expected.ContentType,
			Encoding:          expected.Encoding,
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
