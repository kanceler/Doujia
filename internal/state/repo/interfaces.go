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

type PipelineInstanceRepository interface {
	Create(ctx context.Context, instance PipelineInstanceRecord) error
	Get(ctx context.Context, runID core.RunID, instanceID core.PipelineInstanceID) (PipelineInstanceRecord, error)
	Update(ctx context.Context, instance PipelineInstanceRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]PipelineInstanceRecord, error)
	ListChildren(ctx context.Context, runID core.RunID, parentID core.PipelineInstanceID) ([]PipelineInstanceRecord, error)
}

type ArtifactRepository interface {
	Create(ctx context.Context, artifact ArtifactRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]ArtifactRecord, error)
	ListByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]ArtifactRecord, error)
}

type EventRepository interface {
	Create(ctx context.Context, event EventRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]EventRecord, error)
	ListByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]EventRecord, error)
}
