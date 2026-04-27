package common

import (
	"devflow/internal/core"
	"os"
	"path/filepath"
)

func CloneTaskHistory(items []core.TaskMetaData) []core.TaskMetaData {
	if len(items) == 0 {
		return nil
	}
	out := make([]core.TaskMetaData, len(items))
	copy(out, items)
	return out
}

func WriteAgentOutput(workspacePath, relativePath, content string) (string, error) {
	fullPath := filepath.Join(workspacePath, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}

func FeedbackFor(task core.TaskMetaData, runID core.RunID, agentID core.AgentID, outputs []string) core.TaskMetaData {
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      agentID,
		Op:           task.Op,
		ArtifactURIs: outputs,
		Result:       core.TaskResultCodeOK,
	}
}
