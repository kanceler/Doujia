package doujiagit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"devflow/internal/core"
)

func TestRunArchiveZipExportImportRewritesRunScopedIDsAndArtifacts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	sourceRunID := core.RunID("run_archive_source")
	targetRunID := core.RunID("run_archive_restored")
	sourceProjectsRoot := filepath.Join(t.TempDir(), "source_projects")
	targetProjectsRoot := filepath.Join(t.TempDir(), "target_projects")
	sourceRepository := NewMemoryRepository()
	content := []byte("hello from archived artifact\n")
	storageURI := "projects/run_archive_source/agents/pm01/artifacts/prd/plan_v1.md"
	fullPath := filepath.Join(sourceProjectsRoot, string(sourceRunID), "agents", "pm01", "artifacts", "prd", "plan_v1.md")
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	object, err := sourceRepository.UpsertObject(ctx, ArtifactObject{
		ObjectID:   StableObjectID(content),
		ObjectType: ObjectTypeBlob,
		StorageURI: storageURI,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertObject() error = %v", err)
	}
	logical, err := sourceRepository.UpsertLogicalArtifact(ctx, LogicalArtifact{
		RunID:      sourceRunID,
		Namespace:  "pm01",
		LogicalKey: "prd.plan",
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("UpsertLogicalArtifact() error = %v", err)
	}
	version, err := sourceRepository.UpsertArtifactVersion(ctx, ArtifactVersion{
		LogicalArtifactID: logical.LogicalArtifactID,
		ObjectIDs:         []string{object.ObjectID},
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertArtifactVersion() error = %v", err)
	}
	bagID := StableBagID(sourceRunID, "snapshot_pm_plan", "prd")
	if err := sourceRepository.CreateBag(ctx, ArtifactBag{
		BagID:              bagID,
		RunID:              sourceRunID,
		ArtifactVersionIDs: []string{version.ArtifactVersionID},
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("CreateBag() error = %v", err)
	}
	snapshotID := StableSnapshotID(sourceRunID, "task_02", now.Add(time.Second))
	if err := sourceRepository.CreateSnapshot(ctx, TaskSnapshot{
		SnapshotID:      snapshotID,
		RunID:           sourceRunID,
		TaskID:          "task_02",
		Result:          core.TaskResultCodeOK,
		OutputBagIDs:    []string{bagID},
		DiagnosticsJSON: `{"bag":"` + bagID + `"}`,
		CreatedAt:       now.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	frontierID := StableFrontierSnapshotID(sourceRunID, []string{snapshotID}, nil, now.Add(2*time.Second))
	if err := sourceRepository.CreateFrontierSnapshot(ctx, FrontierSnapshot{
		FrontierSnapshotID: frontierID,
		RunID:              sourceRunID,
		TaskSnapshotIDs:    []string{snapshotID},
		CreatedByMode:      RefMoveModeAdvance,
		DetailsJSON:        `{"frontier":"` + frontierID + `","bag":"` + bagID + `"}`,
		CreatedAt:          now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("CreateFrontierSnapshot() error = %v", err)
	}
	if err := sourceRepository.UpdateRef(ctx, Ref{
		RefName:             DefaultRefName,
		RunID:               sourceRunID,
		FrontierSnapshotID:  frontierID,
		FrontierSnapshotIDs: []string{snapshotID},
		UpdatedAt:           now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("UpdateRef() error = %v", err)
	}
	if err := sourceRepository.CreateRefMoveEvent(ctx, RefMoveEvent{
		EventID:               StableRefMoveEventID(sourceRunID, DefaultRefName, nil, []string{frontierID}, RefMoveModeAdvance, now.Add(4*time.Second)),
		RunID:                 sourceRunID,
		RefName:               DefaultRefName,
		ToFrontierSnapshotIDs: []string{frontierID},
		Mode:                  RefMoveModeAdvance,
		Reason:                "bag " + bagID + " committed",
		DetailsJSON:           `{"snapshot":"` + snapshotID + `"}`,
		CreatedAt:             now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("CreateRefMoveEvent() error = %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "run_archive.zip")
	if err := WriteRunArchiveZip(ctx, sourceRepository, archivePath, ExportOptions{
		RunID:                sourceRunID,
		RefName:              DefaultRefName,
		ProjectsRoot:         sourceProjectsRoot,
		IncludeArtifactFiles: true,
		ExportedAt:           now.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("WriteRunArchiveZip() error = %v", err)
	}

	targetRepository := NewMemoryRepository()
	result, err := ImportRunArchiveZip(ctx, targetRepository, archivePath, ImportOptions{
		NewRunID:      targetRunID,
		TargetRefName: "restored",
		ProjectsRoot:  targetProjectsRoot,
		ImportedAt:    now.Add(6 * time.Second),
	})
	if err != nil {
		t.Fatalf("ImportRunArchiveZip() error = %v", err)
	}
	if result.RunID != targetRunID || result.RefName != "restored" || result.SnapshotCount != 1 || result.BagCount != 1 {
		t.Fatalf("import result = %+v", result)
	}

	graph, err := BuildRunGraph(ctx, targetRepository, targetRunID, "restored")
	if err != nil {
		t.Fatalf("BuildRunGraph(imported) error = %v", err)
	}
	if graph.RunID != targetRunID || graph.Ref.RefName != "restored" {
		t.Fatalf("graph run/ref = %s/%s", graph.RunID, graph.Ref.RefName)
	}
	if len(graph.Snapshots) != 1 || graph.Snapshots[0].SnapshotID == snapshotID {
		t.Fatalf("imported snapshots = %+v, want one remapped snapshot", graph.Snapshots)
	}
	if len(graph.Bags) != 1 || graph.Bags[0].BagID == bagID {
		t.Fatalf("imported bags = %+v, want one remapped bag", graph.Bags)
	}
	if graph.Bags[0].RunID != targetRunID {
		t.Fatalf("imported bag run_id = %s", graph.Bags[0].RunID)
	}
	gotVersion := graph.Bags[0].Versions[0]
	if gotVersion.ArtifactVersionID == version.ArtifactVersionID {
		t.Fatalf("version id was not remapped")
	}
	if gotVersion.LogicalArtifact.RunID != targetRunID {
		t.Fatalf("logical run_id = %s", gotVersion.LogicalArtifact.RunID)
	}
	if gotVersion.Objects[0].ObjectID != object.ObjectID {
		t.Fatalf("object id = %s, want %s", gotVersion.Objects[0].ObjectID, object.ObjectID)
	}
	wantStorageURI := "projects/run_archive_restored/agents/pm01/artifacts/prd/plan_v1.md"
	if gotVersion.Objects[0].StorageURI != wantStorageURI {
		t.Fatalf("storage uri = %q, want %q", gotVersion.Objects[0].StorageURI, wantStorageURI)
	}
	importedFile := filepath.Join(targetProjectsRoot, string(targetRunID), "agents", "pm01", "artifacts", "prd", "plan_v1.md")
	gotContent, err := os.ReadFile(importedFile)
	if err != nil {
		t.Fatalf("ReadFile(imported artifact) error = %v", err)
	}
	if !reflect.DeepEqual(gotContent, content) {
		t.Fatalf("imported content = %q, want %q", gotContent, content)
	}
	if strings.Contains(graph.Snapshots[0].DiagnosticsJSON, bagID) {
		t.Fatalf("diagnostics_json still contains source bag id: %s", graph.Snapshots[0].DiagnosticsJSON)
	}
	moves, err := targetRepository.ListRefMoveEvents(ctx, targetRunID, "restored")
	if err != nil {
		t.Fatalf("ListRefMoveEvents(imported) error = %v", err)
	}
	if len(moves) != 1 || moves[0].EventID == "" || strings.Contains(moves[0].Reason, bagID) {
		t.Fatalf("imported moves = %+v", moves)
	}
}
