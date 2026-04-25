package agentengine

import (
	"devflow/internal/core"
	"fmt"
	"path/filepath"
	"strings"
)

func ParseTask(task core.TaskMetaData) (TaskEnvelope, error) {
	if task.Direction != core.TaskDirectionDispatch {
		return TaskEnvelope{}, fmt.Errorf("unsupported direction %q: only dispatch can be executed", task.Direction)
	}
	if strings.TrimSpace(string(task.TaskID)) == "" {
		return TaskEnvelope{}, fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(string(task.AgentID)) == "" {
		return TaskEnvelope{}, fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(task.Op) == "" {
		return TaskEnvelope{}, fmt.Errorf("op is required")
	}
	refs := make([]ArtifactRef, 0, len(task.ArtifactURIs))
	for _, uri := range task.ArtifactURIs {
		uri = strings.TrimSpace(filepath.ToSlash(uri))
		if uri == "" {
			continue
		}
		refs = append(refs, ArtifactRef{URI: uri, Kind: InferArtifactKind(uri)})
	}
	return TaskEnvelope{
		TaskID:         task.TaskID,
		ParentID:       task.ParentID,
		AgentID:        task.AgentID,
		Op:             task.Op,
		InputArtifacts: refs,
		Control:        task.Control,
	}, nil
}

func InferArtifactKind(uri string) string {
	lower := strings.ToLower(filepath.ToSlash(uri))
	switch {
	case strings.Contains(lower, "/requirement/"):
		return "requirement"
	case strings.Contains(lower, "/prd/"), strings.Contains(lower, "/plan/"):
		return "prd"
	case strings.Contains(lower, "/design/"), strings.Contains(lower, "/architecture/"):
		return "design"
	default:
		return "unknown"
	}
}

func ValidateTaskInputs(task TaskEnvelope, recipe Recipe) error {
	for _, kind := range recipe.RequiredInputKinds {
		found := false
		for _, ref := range task.InputArtifacts {
			if strings.EqualFold(ref.Kind, kind) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s requires %s artifact", recipe.Op, kind)
		}
	}
	return nil
}
