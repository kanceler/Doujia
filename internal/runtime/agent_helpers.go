package runtime

import (
	"devflow/internal/core"
	"os"
	"path/filepath"
)

func appendLineToArtifacts(paths []string, line string) error {
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
				return err
			}
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(line + "\n"); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func writeAgentOutput(workspacePath, relativePath, content string) (string, error) {
	fullPath := filepath.Join(workspacePath, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}

func feedbackFor(task core.TaskMetaData, runID core.RunID, agentID core.AgentID, outputs []string) core.TaskMetaData {
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		AgentID:      agentID,
		Op:           task.Op,
		ArtifactURIs: outputs,
		Result:       core.TaskResultCodeOK,
	}
}

