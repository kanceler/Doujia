package doujiagit

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"devflow/internal/core"

	_ "modernc.org/sqlite"
)

func TestMemoryRepositoryRoundTrip(t *testing.T) {
	runRepositoryContract(t, NewMemoryRepository())
}

func TestSQLiteRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "doujia_git.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	repository := NewSQLiteRepository(db)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	runRepositoryContract(t, repository)
}

func runRepositoryContract(t *testing.T, repository Repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	runID := core.RunID("run_doujia_git")

	object, err := repository.UpsertObject(ctx, ArtifactObject{
		ObjectID:   StableObjectID([]byte("hello")),
		ObjectType: ObjectTypeBlob,
		StorageURI: "objects/sha256/hello",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}

	logical, err := repository.UpsertLogicalArtifact(ctx, LogicalArtifact{
		RunID:      runID,
		Namespace:  "pm01",
		LogicalKey: "plan",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	if logical.LogicalArtifactID == "" {
		t.Fatalf("logical artifact id should be filled")
	}
	gotLogical, err := repository.GetLogicalArtifact(ctx, logical.LogicalArtifactID)
	if err != nil {
		t.Fatalf("GetLogicalArtifact() error = %v", err)
	}
	if gotLogical.LogicalKey != "plan" || gotLogical.Namespace != "pm01" {
		t.Fatalf("logical artifact = %+v, want pm01/plan", gotLogical)
	}

	version, err := repository.UpsertArtifactVersion(ctx, ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	gotObject, err := repository.GetObject(ctx, object.ObjectID)
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	if gotObject.StorageURI != object.StorageURI {
		t.Fatalf("object storage uri = %q, want %q", gotObject.StorageURI, object.StorageURI)
	}
	gotVersion, err := repository.GetArtifactVersion(ctx, version.ArtifactVersionID)
	if err != nil {
		t.Fatalf("GetArtifactVersion() error = %v", err)
	}
	if !reflect.DeepEqual(gotVersion.ObjectIDs, []string{object.ObjectID}) {
		t.Fatalf("version objects = %v, want [%s]", gotVersion.ObjectIDs, object.ObjectID)
	}
	reusedVersion, err := repository.UpsertArtifactVersion(ctx, ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion(reuse) error = %v", err)
	}
	if reusedVersion.ArtifactVersionID != version.ArtifactVersionID {
		t.Fatalf("reused version id = %q, want %q", reusedVersion.ArtifactVersionID, version.ArtifactVersionID)
	}

	outputBagID := "bag_task_01_default"
	if err := repository.CreateBag(ctx, ArtifactBag{
		BagID:              outputBagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag(output) error = %v", err)
	}

	snapshotID := "snapshot_task_01"
	if err := repository.CreateSnapshot(ctx, TaskSnapshot{
		SnapshotID:   snapshotID,
		RunID:        runID,
		TaskID:       "task_01",
		Result:       core.TaskResultCodeOK,
		OutputBagIDs: []string{outputBagID},
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateSnapshot(task_01) error = %v", err)
	}

	producer, err := repository.ProducerOfBag(ctx, outputBagID)
	if err != nil {
		t.Fatalf("ProducerOfBag() error = %v", err)
	}
	if producer != snapshotID {
		t.Fatalf("producer = %q, want %q", producer, snapshotID)
	}

	nextBagID := "bag_task_02_default"
	if err := repository.CreateBag(ctx, ArtifactBag{
		BagID:              nextBagID,
		RunID:              runID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateBag(next) error = %v", err)
	}
	nextSnapshotID := "snapshot_task_02"
	if err := repository.CreateSnapshot(ctx, TaskSnapshot{
		SnapshotID:   nextSnapshotID,
		RunID:        runID,
		TaskID:       "task_02",
		Result:       core.TaskResultCodeOK,
		InputBagIDs:  []string{outputBagID},
		OutputBagIDs: []string{nextBagID},
		CreatedAt:    now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("CreateSnapshot(task_02) error = %v", err)
	}

	gotSnapshot, err := repository.GetSnapshot(ctx, nextSnapshotID)
	if err != nil {
		t.Fatalf("GetSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(gotSnapshot.InputBagIDs, []string{outputBagID}) {
		t.Fatalf("snapshot inputs = %v, want [%s]", gotSnapshot.InputBagIDs, outputBagID)
	}

	frontierID := "frontier_task_02"
	parentFrontierID := "frontier_task_01"
	if err := repository.CreateFrontierSnapshot(ctx, FrontierSnapshot{
		FrontierSnapshotID: parentFrontierID,
		RunID:              runID,
		TaskSnapshotIDs:    []string{snapshotID},
		CreatedByMode:      RefMoveModeAdvance,
		CreatedAt:          now.Add(2500 * time.Millisecond),
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot(parent) error = %v", err)
	}
	if err := repository.CreateFrontierSnapshot(ctx, FrontierSnapshot{
		FrontierSnapshotID:        frontierID,
		RunID:                     runID,
		ParentFrontierSnapshotIDs: []string{parentFrontierID},
		TaskSnapshotIDs:           []string{nextSnapshotID},
		CreatedByMode:             RefMoveModeAdvance,
		CreatedAt:                 now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	gotFrontier, err := repository.GetFrontierSnapshot(ctx, frontierID)
	if err != nil {
		t.Fatalf("GetFrontierSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(gotFrontier.TaskSnapshotIDs, []string{nextSnapshotID}) {
		t.Fatalf("frontier task snapshots = %v, want [%s]", gotFrontier.TaskSnapshotIDs, nextSnapshotID)
	}

	if err := repository.UpdateRef(ctx, Ref{
		RefName:             DefaultRefName,
		RunID:               runID,
		FrontierSnapshotID:  frontierID,
		FrontierSnapshotIDs: []string{nextSnapshotID},
		UpdatedAt:           now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	gotRef, err := repository.GetRef(ctx, runID, DefaultRefName)
	if err != nil {
		t.Fatalf("GetRef() error = %v", err)
	}
	if !reflect.DeepEqual(gotRef.FrontierSnapshotIDs, []string{nextSnapshotID}) {
		t.Fatalf("ref frontier = %v, want [%s]", gotRef.FrontierSnapshotIDs, nextSnapshotID)
	}
	if gotRef.FrontierSnapshotID != frontierID {
		t.Fatalf("ref current frontier = %q, want %q", gotRef.FrontierSnapshotID, frontierID)
	}
	movedFrontierID := "frontier_task_02_checkout"
	if err := repository.CreateFrontierSnapshot(ctx, FrontierSnapshot{
		FrontierSnapshotID:        movedFrontierID,
		RunID:                     runID,
		ParentFrontierSnapshotIDs: []string{frontierID},
		TaskSnapshotIDs:           []string{nextSnapshotID},
		CreatedByMode:             RefMoveModeCheckout,
		CreatedAt:                 now.Add(4500 * time.Millisecond),
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot(checkout) error = %v", err)
	}
	movedRef, moveEvent, err := repository.MoveRef(ctx, MoveRefRequest{
		Ref: Ref{
			RefName:             DefaultRefName,
			RunID:               runID,
			FrontierSnapshotID:  movedFrontierID,
			FrontierSnapshotIDs: []string{nextSnapshotID},
			UpdatedAt:           now.Add(4600 * time.Millisecond),
		},
		ExpectedFrontierSnapshotID: frontierID,
		Event: RefMoveEvent{
			EventID:                 "refmove_checkout",
			RunID:                   runID,
			RefName:                 DefaultRefName,
			FromFrontierSnapshotIDs: []string{frontierID},
			ToFrontierSnapshotIDs:   []string{movedFrontierID},
			Mode:                    RefMoveModeCheckout,
			Reason:                  "checkout in contract",
			CreatedAt:               now.Add(4600 * time.Millisecond),
		},
	})
	if err != nil {
		t.Fatalf("MoveRef() error = %v", err)
	}
	if movedRef.FrontierSnapshotID != movedFrontierID || moveEvent.Mode != RefMoveModeCheckout {
		t.Fatalf("MoveRef() = ref %+v event %+v, want checkout to %s", movedRef, moveEvent, movedFrontierID)
	}
	_, _, err = repository.MoveRef(ctx, MoveRefRequest{
		Ref: Ref{
			RefName:             DefaultRefName,
			RunID:               runID,
			FrontierSnapshotID:  frontierID,
			FrontierSnapshotIDs: []string{nextSnapshotID},
			UpdatedAt:           now.Add(4700 * time.Millisecond),
		},
		ExpectedFrontierSnapshotID: frontierID,
		Event: RefMoveEvent{
			EventID:               "refmove_stale",
			RunID:                 runID,
			RefName:               DefaultRefName,
			ToFrontierSnapshotIDs: []string{frontierID},
			Mode:                  RefMoveModeCheckout,
			CreatedAt:             now.Add(4700 * time.Millisecond),
		},
	})
	if err == nil {
		t.Fatalf("MoveRef(stale) succeeded, want CAS conflict")
	}

	if err := repository.CreateRefMoveEvent(ctx, RefMoveEvent{
		EventID:               "refmove_task_02",
		RunID:                 runID,
		RefName:               DefaultRefName,
		ToFrontierSnapshotIDs: []string{frontierID},
		Mode:                  RefMoveModeAdvance,
		Reason:                "task_02 committed",
		CreatedAt:             now.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("CreateRefMoveEvent() error = %v", err)
	}
	moves, err := repository.ListRefMoveEvents(ctx, runID, DefaultRefName)
	if err != nil {
		t.Fatalf("ListRefMoveEvents() error = %v", err)
	}
	if got, want := len(moves), 2; got != want {
		t.Fatalf("ref move count = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(moves[0].ToFrontierSnapshotIDs, []string{movedFrontierID}) || moves[0].Mode != RefMoveModeCheckout {
		t.Fatalf("ref move = %+v, want checkout to %s", moves[0], movedFrontierID)
	}

	gotBag, err := repository.GetBag(ctx, outputBagID)
	if err != nil {
		t.Fatalf("GetBag() error = %v", err)
	}
	if !reflect.DeepEqual(gotBag.ArtifactVersionIDs, []string{version.ArtifactVersionID}) {
		t.Fatalf("bag versions = %v, want [%s]", gotBag.ArtifactVersionIDs, version.ArtifactVersionID)
	}

	snapshots, err := repository.ListSnapshotsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSnapshotsByRun() error = %v", err)
	}
	if got, want := len(snapshots), 2; got != want {
		t.Fatalf("snapshot count = %d, want %d", got, want)
	}
	if snapshots[0].SnapshotID != snapshotID || snapshots[1].SnapshotID != nextSnapshotID {
		t.Fatalf("snapshot order = [%s %s], want [%s %s]", snapshots[0].SnapshotID, snapshots[1].SnapshotID, snapshotID, nextSnapshotID)
	}

	bags, err := repository.ListBagsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListBagsByRun() error = %v", err)
	}
	if got, want := len(bags), 2; got != want {
		t.Fatalf("bag count = %d, want %d", got, want)
	}

	graph, err := BuildRunGraph(ctx, repository, runID, DefaultRefName)
	if err != nil {
		t.Fatalf("BuildRunGraph() error = %v", err)
	}
	if graph.Ref.RefName != DefaultRefName || !reflect.DeepEqual(graph.Ref.FrontierSnapshotIDs, []string{nextSnapshotID}) {
		t.Fatalf("graph ref = %+v, want %s -> [%s]", graph.Ref, DefaultRefName, nextSnapshotID)
	}
	if got, want := len(graph.Snapshots), 2; got != want {
		t.Fatalf("graph snapshot count = %d, want %d", got, want)
	}
	if got, want := len(graph.HistorySnapshots), 2; got != want {
		t.Fatalf("graph history snapshot count = %d, want %d", got, want)
	}
	if graph.Ref.FrontierSnapshotID != movedFrontierID {
		t.Fatalf("graph current frontier = %q, want %q", graph.Ref.FrontierSnapshotID, movedFrontierID)
	}
	if got, want := graph.Snapshots[1].InputBags[0].Versions[0].Objects[0].StorageURI, object.StorageURI; got != want {
		t.Fatalf("graph input object uri = %q, want %q", got, want)
	}
	if got, want := graph.Snapshots[0].OutputBags[0].ConsumerSnapshotIDs, []string{nextSnapshotID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph consumers = %v, want %v", got, want)
	}

	detail, err := BuildTaskSnapshotDetail(ctx, repository, runID, "task_02")
	if err != nil {
		t.Fatalf("BuildTaskSnapshotDetail() error = %v", err)
	}
	if detail.SnapshotID != nextSnapshotID || len(detail.InputBags) != 1 || len(detail.OutputBags) != 1 {
		t.Fatalf("task detail = %+v, want task_02 with one input and one output bag", detail)
	}
}
