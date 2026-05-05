package runtime

import (
	"context"

	"devflow/internal/core"
)

func CommitAgentFeedbackOutputs(ctx context.Context, committer AgentOutputCommitter, runID core.RunID, agentID core.AgentID, task core.TaskMetaData, feedback core.TaskMetaData) (core.TaskMetaData, error) {
	if committer == nil || feedback.Commit != nil {
		return feedback, nil
	}
	if len(feedback.Outputs) == 0 && len(feedback.ProducedBags) == 0 {
		return feedback, nil
	}
	receipt, err := committer.CommitOutputs(ctx, OutputCommitRequest{
		RunID:        runID,
		AgentID:      agentID,
		TaskID:       task.TaskID,
		Op:           task.Op,
		Result:       feedback.Result,
		Outputs:      append([]core.AgentOutput(nil), feedback.Outputs...),
		ProducedBags: append([]core.ProducedBagManifest(nil), feedback.ProducedBags...),
		Control:      feedback.Control,
	})
	if err != nil {
		return core.TaskMetaData{}, err
	}
	feedback.Commit = &receipt
	return feedback, nil
}
