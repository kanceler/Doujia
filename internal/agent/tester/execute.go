package tester

import (
	"context"
	"fmt"
	"path"
	"time"

	"devflow/internal/agent/common"
	"devflow/internal/core"
)

func (a *Agent) failureFeedback(ctx context.Context, task core.TaskMetaData, cause error) core.TaskMetaData {
	content, _ := common.MarshalJSONArtifact(map[string]any{
		"schema_version":      1,
		"kind":                "tester_failure",
		"op":                  task.Op,
		"error":               cause.Error(),
		"input_artifact_uris": task.ArtifactURIs,
		"created_at":          time.Now().UTC().Format(time.RFC3339),
	})
	outputURI := path.Join("projects", string(a.runID), "agents", string(a.agentID), "artifacts", "failures", "tester_not_implemented.md")
	outputs := []string{}
	if a.artifactStore != nil && content != nil {
		if err := a.artifactStore.Write(ctx, outputURI, content); err == nil {
			outputs = append(outputs, outputURI)
		}
	}
	a.logStep(fmt.Sprintf("tester op failed: %v", cause))
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        a.runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      a.agentID,
		Op:           task.Op,
		ArtifactURIs: outputs,
		Result:       core.TaskResultCodeFail,
	}
}
