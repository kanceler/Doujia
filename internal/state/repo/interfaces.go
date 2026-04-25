package repo

import (
	"context"
	"devflow/internal/core"
)

type RunRepository interface {
	Create(ctx context.Context, run RunRecord) error
	Get(ctx context.Context, runID core.RunID) (RunRecord, error)
	Update(ctx context.Context, run RunRecord) error
}

type TaskRepository interface {
	Create(ctx context.Context, task TaskRecord) error
	Get(ctx context.Context, runID core.RunID, taskID core.TaskID) (TaskRecord, error)
	Update(ctx context.Context, task TaskRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]TaskRecord, error)
}

