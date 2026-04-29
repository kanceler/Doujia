package agentengine

import "devflow/internal/core"

func BuildFeedback(task core.TaskMetaData, outputURIs []string, output ModelOutput) core.TaskMetaData {
	return core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        task.RunID,
		TaskID:       task.TaskID,
		ParentID:     task.ParentID,
		DependsOn:    task.DependsOn,
		DependsOnIDs: task.DependsOnIDs,
		AgentID:      task.AgentID,
		Op:           task.Op,
		ArtifactURIs: outputURIs,
		Result:       core.TaskResultCodeOK,
		Control:      output.Control,
	}
}
