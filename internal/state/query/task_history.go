package query

import (
	"context"
	"devflow/internal/core"
	"devflow/internal/pipeline"
	"devflow/internal/state/repo"
	"sort"
)

type TaskHistoryQuery interface {
	ListAgentHistory(ctx context.Context, runID core.RunID, agentID core.AgentID) ([]core.AgentTaskHistory, error)
}

type taskHistoryQuery struct {
	pipelines pipeline.Registry
	runs      repo.RunRepository
	tasks     repo.TaskRepository
}

func NewTaskHistoryQuery(
	pipelines pipeline.Registry,
	runs repo.RunRepository,
	tasks repo.TaskRepository,
) TaskHistoryQuery {
	return &taskHistoryQuery{
		pipelines: pipelines,
		runs:      runs,
		tasks:     tasks,
	}
}

func (q *taskHistoryQuery) ListAgentHistory(ctx context.Context, runID core.RunID, agentID core.AgentID) ([]core.AgentTaskHistory, error) {
	run, err := q.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	spec, err := q.pipelines.Get(ctx, run.PipelineID)
	if err != nil {
		return nil, err
	}
	records, err := q.tasks.ListByRun(ctx, runID)
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
		stageOp := stageOps[task.StageID]
		history = append(history, core.AgentTaskHistory{
			RunID:              task.RunID,
			TaskID:             task.ID,
			AgentID:            task.AgentID,
			Op:                 inferTaskOp(task, stageOp),
			Status:             task.Status,
			InputArtifactURIs:  artifactRefsToURIs(task.InputArtifactRefs),
			OutputArtifactURIs: artifactRefsToURIs(task.OutputArtifactRefs),
		})
	}
	return history, nil
}

func inferTaskOp(task core.Task, stageOp string) string {
	if task.AgentRole == core.AgentRoleCoder {
		return core.TaskOpWriteCode
	}
	if task.AgentRole == core.AgentRoleTester {
		id := string(task.ID)
		if hasSuffix(id, "_test_code") {
			return core.TaskOpTestCode
		}
		return core.TaskOpTestData
	}
	if task.AgentRole == core.AgentRoleArchitect {
		id := string(task.ID)
		switch {
		case hasSuffix(id, "_merge_code"):
			return core.TaskOpMergeCode
		case hasSuffix(id, "_global_test_data"):
			return core.TaskOpTestData
		case hasSuffix(id, "_global_test_code"):
			return core.TaskOpTestCode
		}
	}
	return stageOp
}

func hasSuffix(value, suffix string) bool {
	return len(value) >= len(suffix) && value[len(value)-len(suffix):] == suffix
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
