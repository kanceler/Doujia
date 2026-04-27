package app

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
	"fmt"
	"sort"
)

func newTaskHistoryProvider(
	pipelines pipeline.Registry,
	runs repo.RunRepository,
	tasks repo.TaskRepository,
) runtime.TaskHistoryProvider {
	return func(ctx context.Context, runID core.RunID, agentID core.AgentID) ([]core.AgentTaskHistory, error) {
		run, err := runs.Get(ctx, runID)
		if err != nil {
			return nil, err
		}
		spec, err := pipelines.Get(ctx, run.PipelineID)
		if err != nil {
			return nil, err
		}
		records, err := tasks.ListByRun(ctx, runID)
		if err != nil {
			return nil, err
		}

		sort.SliceStable(records, func(i, j int) bool {
			if records[i].CreatedAt.Equal(records[j].CreatedAt) {
				return records[i].ID < records[j].ID
			}
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		})

		stageOps := make(map[core.StageID]string, len(spec.Stages))
		for _, stage := range spec.Stages {
			stageOps[stage.ID] = stage.Op
		}

		history := make([]core.AgentTaskHistory, 0)
		for _, task := range records {
			if task.AgentID != agentID {
				continue
			}
			op, ok := stageOps[task.StageID]
			if !ok {
				return nil, fmt.Errorf("stage %q not found in pipeline %q", task.StageID, spec.ID)
			}
			history = append(history, core.AgentTaskHistory{
				RunID:              task.RunID,
				TaskID:             task.ID,
				AgentID:            task.AgentID,
				Op:                 op,
				Status:             task.Status,
				InputArtifactURIs:  artifactRefsToURIs(task.InputArtifactRefs),
				OutputArtifactURIs: artifactRefsToURIs(task.OutputArtifactRefs),
			})
		}
		return history, nil
	}
}

func artifactRefsToURIs(refs []core.ArtifactRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = string(ref)
	}
	return out
}
