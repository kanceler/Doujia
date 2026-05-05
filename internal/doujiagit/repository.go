package doujiagit

import (
	"context"

	"devflow/internal/core"
)

type Repository interface {
	UpsertObject(ctx context.Context, obj ArtifactObject) (ArtifactObject, error)
	GetObject(ctx context.Context, objectID string) (ArtifactObject, error)

	UpsertLogicalArtifact(ctx context.Context, item LogicalArtifact) (LogicalArtifact, error)
	GetLogicalArtifact(ctx context.Context, logicalArtifactID string) (LogicalArtifact, error)
	UpsertArtifactVersion(ctx context.Context, version ArtifactVersion) (ArtifactVersion, error)
	GetArtifactVersion(ctx context.Context, versionID string) (ArtifactVersion, error)

	CreateBag(ctx context.Context, bag ArtifactBag) error
	GetBag(ctx context.Context, bagID string) (ArtifactBag, error)
	ListBagsByRun(ctx context.Context, runID core.RunID) ([]ArtifactBag, error)

	CreateSnapshot(ctx context.Context, snapshot TaskSnapshot) error
	GetSnapshot(ctx context.Context, snapshotID string) (TaskSnapshot, error)
	ListSnapshotsByRun(ctx context.Context, runID core.RunID) ([]TaskSnapshot, error)
	ProducerOfBag(ctx context.Context, bagID string) (string, error)

	CreateFrontierSnapshot(ctx context.Context, snapshot FrontierSnapshot) error
	GetFrontierSnapshot(ctx context.Context, frontierSnapshotID string) (FrontierSnapshot, error)
	ListFrontierSnapshotsByRun(ctx context.Context, runID core.RunID) ([]FrontierSnapshot, error)

	UpdateRef(ctx context.Context, ref Ref) error
	MoveRef(ctx context.Context, req MoveRefRequest) (Ref, RefMoveEvent, error)
	GetRef(ctx context.Context, runID core.RunID, refName string) (Ref, error)
	ListRefsByRun(ctx context.Context, runID core.RunID) ([]Ref, error)
	CreateRefMoveEvent(ctx context.Context, event RefMoveEvent) error
	ListRefMoveEvents(ctx context.Context, runID core.RunID, refName string) ([]RefMoveEvent, error)
}
