package common

import "devflow/internal/core"

func NewTaskHistoryItem(task core.TaskMetaData, feedback core.TaskMetaData, agentID core.AgentID, err error) core.AgentTaskHistory {
	status := core.TaskStatusDone
	if err != nil || feedback.Result == core.TaskResultCodeFail {
		status = core.TaskStatusFailed
	}

	return core.AgentTaskHistory{
		RunID:              task.RunID,
		TaskID:             task.TaskID,
		AgentID:            agentID,
		Op:                 task.Op,
		Status:             status,
		InputArtifactURIs:  append([]string(nil), task.ArtifactURIs...),
		OutputArtifactURIs: append([]string(nil), feedback.ArtifactURIs...),
	}
}
