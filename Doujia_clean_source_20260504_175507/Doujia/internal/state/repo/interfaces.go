package repo

import (
	"context"
	"devflow/internal/core"
)

type RunRepository interface {
	Create(ctx context.Context, run RunRecord) error
	Get(ctx context.Context, runID core.RunID) (RunRecord, error)
	List(ctx context.Context) ([]RunRecord, error)
	Update(ctx context.Context, run RunRecord) error
}

type ProjectRepository interface {
	Create(ctx context.Context, project ProjectRecord) error
	Get(ctx context.Context, projectID string) (ProjectRecord, error)
	List(ctx context.Context) ([]ProjectRecord, error)
	Update(ctx context.Context, project ProjectRecord) error
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

type RunIterationRepository interface {
	Create(ctx context.Context, iteration RunIterationRecord) error
	Update(ctx context.Context, iteration RunIterationRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]RunIterationRecord, error)
}

type SessionMessageRepository interface {
	Create(ctx context.Context, message SessionMessageRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]SessionMessageRecord, error)
}

type SessionArtifactRepository interface {
	Create(ctx context.Context, artifact SessionArtifactRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]SessionArtifactRecord, error)
}

type ImprovementItemRepository interface {
	Create(ctx context.Context, item ImprovementItemRecord) error
	Update(ctx context.Context, item ImprovementItemRecord) error
	ListByRun(ctx context.Context, runID core.RunID) ([]ImprovementItemRecord, error)
}
