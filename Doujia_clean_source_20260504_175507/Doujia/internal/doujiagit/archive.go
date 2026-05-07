package doujiagit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devflow/internal/core"
)

const (
	ArchiveSchemaVersion = "devflow.doujiagit.archive/v0.1"
	archiveManifestPath  = "doujiagit_archive.json"
)

type RunArchive struct {
	SchemaVersion     string                `json:"schema_version"`
	ExportedAt        time.Time             `json:"exported_at"`
	SourceRunID       core.RunID            `json:"source_run_id"`
	SourceRefName     string                `json:"source_ref_name"`
	Ref               Ref                   `json:"ref"`
	Objects           []ArtifactObject      `json:"objects"`
	LogicalArtifacts  []LogicalArtifact     `json:"logical_artifacts"`
	ArtifactVersions  []ArtifactVersion     `json:"artifact_versions"`
	Bags              []ArtifactBag         `json:"bags"`
	Snapshots         []TaskSnapshot        `json:"snapshots"`
	FrontierSnapshots []FrontierSnapshot    `json:"frontier_snapshots"`
	RefMoveEvents     []RefMoveEvent        `json:"ref_move_events"`
	ArtifactFiles     []ArchiveArtifactFile `json:"artifact_files,omitempty"`
}

type ArchiveArtifactFile struct {
	ObjectID    string `json:"object_id"`
	StorageURI  string `json:"storage_uri"`
	ArchivePath string `json:"archive_path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type ExportOptions struct {
	RunID                core.RunID
	RefName              string
	ProjectsRoot         string
	IncludeArtifactFiles bool
	ExportedAt           time.Time
}

type ImportOptions struct {
	NewRunID      core.RunID
	TargetRefName string
	ProjectsRoot  string
	ImportedAt    time.Time
}

type ImportResult struct {
	RunID              core.RunID `json:"run_id"`
	RefName            string     `json:"ref_name"`
	FrontierSnapshotID string     `json:"frontier_snapshot_id,omitempty"`
	SnapshotCount      int        `json:"snapshot_count"`
	BagCount           int        `json:"bag_count"`
}

func ExportRunArchive(ctx context.Context, repository Repository, opts ExportOptions) (RunArchive, error) {
	if repository == nil {
		return RunArchive{}, fmt.Errorf("doujia git repository is required")
	}
	if strings.TrimSpace(string(opts.RunID)) == "" {
		return RunArchive{}, fmt.Errorf("run_id is required")
	}
	refName := strings.TrimSpace(opts.RefName)
	if refName == "" {
		refName = DefaultRefName
	}
	exportedAt := opts.ExportedAt
	if exportedAt.IsZero() {
		exportedAt = time.Now().UTC()
	}

	ref, err := repository.GetRef(ctx, opts.RunID, refName)
	if err != nil {
		return RunArchive{}, err
	}
	bags, err := repository.ListBagsByRun(ctx, opts.RunID)
	if err != nil {
		return RunArchive{}, err
	}
	snapshots, err := repository.ListSnapshotsByRun(ctx, opts.RunID)
	if err != nil {
		return RunArchive{}, err
	}
	frontiers, err := repository.ListFrontierSnapshotsByRun(ctx, opts.RunID)
	if err != nil {
		return RunArchive{}, err
	}
	moves, err := repository.ListRefMoveEvents(ctx, opts.RunID, ref.RefName)
	if err != nil {
		return RunArchive{}, err
	}

	versionsByID := make(map[string]ArtifactVersion)
	logicalsByID := make(map[string]LogicalArtifact)
	objectsByID := make(map[string]ArtifactObject)
	for _, bag := range bags {
		for _, versionID := range bag.ArtifactVersionIDs {
			if _, ok := versionsByID[versionID]; ok {
				continue
			}
			version, err := repository.GetArtifactVersion(ctx, versionID)
			if err != nil {
				return RunArchive{}, fmt.Errorf("get artifact version %s: %w", versionID, err)
			}
			versionsByID[version.ArtifactVersionID] = version
			if _, ok := logicalsByID[version.LogicalArtifactID]; !ok {
				logical, err := repository.GetLogicalArtifact(ctx, version.LogicalArtifactID)
				if err != nil {
					return RunArchive{}, fmt.Errorf("get logical artifact %s: %w", version.LogicalArtifactID, err)
				}
				logicalsByID[logical.LogicalArtifactID] = logical
			}
			for _, objectID := range version.ObjectIDs {
				if _, ok := objectsByID[objectID]; ok {
					continue
				}
				object, err := repository.GetObject(ctx, objectID)
				if err != nil {
					return RunArchive{}, fmt.Errorf("get object %s: %w", objectID, err)
				}
				objectsByID[object.ObjectID] = object
			}
		}
	}

	archive := RunArchive{
		SchemaVersion:     ArchiveSchemaVersion,
		ExportedAt:        exportedAt.UTC(),
		SourceRunID:       opts.RunID,
		SourceRefName:     ref.RefName,
		Ref:               ref,
		Objects:           sortedMapValues(objectsByID, func(item ArtifactObject) string { return item.ObjectID }),
		LogicalArtifacts:  sortedMapValues(logicalsByID, func(item LogicalArtifact) string { return item.LogicalArtifactID }),
		ArtifactVersions:  sortedMapValues(versionsByID, func(item ArtifactVersion) string { return item.ArtifactVersionID }),
		Bags:              append([]ArtifactBag(nil), bags...),
		Snapshots:         append([]TaskSnapshot(nil), snapshots...),
		FrontierSnapshots: append([]FrontierSnapshot(nil), frontiers...),
		RefMoveEvents:     append([]RefMoveEvent(nil), moves...),
	}
	sortRunArchive(&archive)
	return archive, nil
}

func WriteRunArchiveZip(ctx context.Context, repository Repository, outputPath string, opts ExportOptions) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path is required")
	}
	archive, err := ExportRunArchive(ctx, repository, opts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(outputPath)), 0o755); err != nil {
		return err
	}
	file, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	defer writer.Close()

	if opts.IncludeArtifactFiles {
		if strings.TrimSpace(opts.ProjectsRoot) == "" {
			return fmt.Errorf("projects root is required when artifact files are included")
		}
		for index := range archive.Objects {
			object := archive.Objects[index]
			if strings.TrimSpace(object.StorageURI) == "" {
				continue
			}
			content, err := readArtifactFile(opts.ProjectsRoot, opts.RunID, object.StorageURI)
			if err != nil {
				return fmt.Errorf("read artifact file for object %s: %w", object.ObjectID, err)
			}
			archivePath := archiveObjectPath(object.ObjectID)
			entry, err := writer.Create(archivePath)
			if err != nil {
				return err
			}
			if _, err := entry.Write(content); err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			archive.ArtifactFiles = append(archive.ArtifactFiles, ArchiveArtifactFile{
				ObjectID:    object.ObjectID,
				StorageURI:  object.StorageURI,
				ArchivePath: archivePath,
				Size:        int64(len(content)),
				SHA256:      hex.EncodeToString(sum[:]),
			})
		}
		sort.Slice(archive.ArtifactFiles, func(i, j int) bool {
			return archive.ArtifactFiles[i].ObjectID < archive.ArtifactFiles[j].ObjectID
		})
	}

	raw, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return err
	}
	manifest, err := writer.Create(archiveManifestPath)
	if err != nil {
		return err
	}
	_, err = manifest.Write(append(raw, '\n'))
	return err
}

func ReadRunArchiveZip(inputPath string) (RunArchive, map[string][]byte, error) {
	if strings.TrimSpace(inputPath) == "" {
		return RunArchive{}, nil, fmt.Errorf("input path is required")
	}
	reader, err := zip.OpenReader(filepath.Clean(inputPath))
	if err != nil {
		return RunArchive{}, nil, err
	}
	defer reader.Close()

	var archive RunArchive
	files := make(map[string][]byte)
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			return RunArchive{}, nil, err
		}
		content, readErr := io.ReadAll(handle)
		closeErr := handle.Close()
		if readErr != nil {
			return RunArchive{}, nil, readErr
		}
		if closeErr != nil {
			return RunArchive{}, nil, closeErr
		}
		if file.Name == archiveManifestPath {
			if err := json.Unmarshal(content, &archive); err != nil {
				return RunArchive{}, nil, fmt.Errorf("unmarshal archive manifest: %w", err)
			}
			continue
		}
		files[file.Name] = content
	}
	if archive.SchemaVersion == "" {
		return RunArchive{}, nil, fmt.Errorf("archive manifest %q not found", archiveManifestPath)
	}
	if archive.SchemaVersion != ArchiveSchemaVersion {
		return RunArchive{}, nil, fmt.Errorf("unsupported archive schema %q", archive.SchemaVersion)
	}
	return archive, files, nil
}

func ImportRunArchiveZip(ctx context.Context, repository Repository, inputPath string, opts ImportOptions) (ImportResult, error) {
	archive, files, err := ReadRunArchiveZip(inputPath)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportRunArchive(ctx, repository, archive, files, opts)
}

func ImportRunArchive(ctx context.Context, repository Repository, archive RunArchive, files map[string][]byte, opts ImportOptions) (ImportResult, error) {
	if repository == nil {
		return ImportResult{}, fmt.Errorf("doujia git repository is required")
	}
	if archive.SchemaVersion != ArchiveSchemaVersion {
		return ImportResult{}, fmt.Errorf("unsupported archive schema %q", archive.SchemaVersion)
	}
	sortRunArchive(&archive)
	if strings.TrimSpace(string(opts.NewRunID)) == "" {
		return ImportResult{}, fmt.Errorf("new run_id is required")
	}
	targetRefName := strings.TrimSpace(opts.TargetRefName)
	if targetRefName == "" {
		targetRefName = archive.Ref.RefName
	}
	if targetRefName == "" {
		targetRefName = DefaultRefName
	}
	importedAt := opts.ImportedAt
	if importedAt.IsZero() {
		importedAt = time.Now().UTC()
	}

	mapping := newArchiveIDMapping()
	if archive.SourceRunID != "" {
		mapping.all[string(archive.SourceRunID)] = string(opts.NewRunID)
	}
	for _, logical := range archive.LogicalArtifacts {
		newID := StableLogicalArtifactID(opts.NewRunID, logical.Namespace, logical.LogicalKey)
		mapping.logical[logical.LogicalArtifactID] = newID
		mapping.all[logical.LogicalArtifactID] = newID
	}
	for _, version := range archive.ArtifactVersions {
		newLogicalID := mapping.logical[version.LogicalArtifactID]
		if newLogicalID == "" {
			return ImportResult{}, fmt.Errorf("logical artifact %q for version %q is missing from archive", version.LogicalArtifactID, version.ArtifactVersionID)
		}
		newID := StableArtifactVersionID(newLogicalID, version.ObjectIDs)
		mapping.version[version.ArtifactVersionID] = newID
		mapping.all[version.ArtifactVersionID] = newID
	}
	for _, bag := range archive.Bags {
		newID := stableImportedID("bag", opts.NewRunID, bag.BagID)
		mapping.bag[bag.BagID] = newID
		mapping.all[bag.BagID] = newID
	}
	for _, snapshot := range archive.Snapshots {
		newID := StableSnapshotID(opts.NewRunID, snapshot.TaskID, snapshot.CreatedAt)
		mapping.snapshot[snapshot.SnapshotID] = newID
		mapping.all[snapshot.SnapshotID] = newID
	}
	for _, frontier := range archive.FrontierSnapshots {
		newTaskSnapshotIDs := rewriteIDs(frontier.TaskSnapshotIDs, mapping.snapshot)
		newParentIDs := rewriteIDs(frontier.ParentFrontierSnapshotIDs, mapping.frontier)
		newID := StableFrontierSnapshotID(opts.NewRunID, newTaskSnapshotIDs, newParentIDs, frontier.CreatedAt)
		mapping.frontier[frontier.FrontierSnapshotID] = newID
		mapping.all[frontier.FrontierSnapshotID] = newID
	}
	for _, event := range archive.RefMoveEvents {
		newFromIDs := rewriteIDs(event.FromFrontierSnapshotIDs, mapping.frontier)
		newToIDs := rewriteIDs(event.ToFrontierSnapshotIDs, mapping.frontier)
		newID := StableRefMoveEventID(opts.NewRunID, targetRefName, newFromIDs, newToIDs, event.Mode, event.CreatedAt)
		mapping.refMove[event.EventID] = newID
		mapping.all[event.EventID] = newID
	}

	fileByObjectID := make(map[string]ArchiveArtifactFile, len(archive.ArtifactFiles))
	for _, file := range archive.ArtifactFiles {
		fileByObjectID[file.ObjectID] = file
	}
	for _, object := range archive.Objects {
		newObject := object
		newObject.StorageURI = rewriteStorageURI(object.StorageURI, archive.SourceRunID, opts.NewRunID)
		if file, ok := fileByObjectID[object.ObjectID]; ok {
			content, ok := files[file.ArchivePath]
			if !ok {
				return ImportResult{}, fmt.Errorf("archive file %q for object %q not found", file.ArchivePath, object.ObjectID)
			}
			if err := verifyArchiveFile(file, content); err != nil {
				return ImportResult{}, err
			}
			if strings.TrimSpace(opts.ProjectsRoot) == "" {
				return ImportResult{}, fmt.Errorf("projects root is required to import artifact files")
			}
			if err := writeArtifactFile(opts.ProjectsRoot, opts.NewRunID, newObject.StorageURI, content); err != nil {
				return ImportResult{}, fmt.Errorf("write artifact file for object %s: %w", object.ObjectID, err)
			}
		}
		if _, err := repository.UpsertObject(ctx, newObject); err != nil {
			return ImportResult{}, err
		}
	}
	for _, logical := range archive.LogicalArtifacts {
		newLogical := logical
		newLogical.RunID = opts.NewRunID
		newLogical.LogicalArtifactID = mapping.logical[logical.LogicalArtifactID]
		if _, err := repository.UpsertLogicalArtifact(ctx, newLogical); err != nil {
			return ImportResult{}, err
		}
	}
	for _, version := range archive.ArtifactVersions {
		newVersion := version
		newVersion.LogicalArtifactID = mapping.logical[version.LogicalArtifactID]
		newVersion.ArtifactVersionID = mapping.version[version.ArtifactVersionID]
		if _, err := repository.UpsertArtifactVersion(ctx, newVersion); err != nil {
			return ImportResult{}, err
		}
	}
	for _, bag := range archive.Bags {
		newBag := bag
		newBag.RunID = opts.NewRunID
		newBag.BagID = mapping.bag[bag.BagID]
		newBag.ArtifactVersionIDs = rewriteIDs(bag.ArtifactVersionIDs, mapping.version)
		if err := repository.CreateBag(ctx, newBag); err != nil {
			return ImportResult{}, err
		}
	}
	for _, snapshot := range archive.Snapshots {
		newSnapshot := snapshot
		newSnapshot.RunID = opts.NewRunID
		newSnapshot.SnapshotID = mapping.snapshot[snapshot.SnapshotID]
		newSnapshot.InputBagIDs = rewriteIDs(snapshot.InputBagIDs, mapping.bag)
		newSnapshot.OutputBagIDs = rewriteIDs(snapshot.OutputBagIDs, mapping.bag)
		newSnapshot.DiagnosticsJSON = replaceKnownIDs(snapshot.DiagnosticsJSON, mapping.all)
		newSnapshot.RuntimeContextJSON = replaceKnownIDs(snapshot.RuntimeContextJSON, mapping.all)
		if err := repository.CreateSnapshot(ctx, newSnapshot); err != nil {
			return ImportResult{}, err
		}
	}
	for _, frontier := range archive.FrontierSnapshots {
		newFrontier := frontier
		newFrontier.RunID = opts.NewRunID
		newFrontier.FrontierSnapshotID = mapping.frontier[frontier.FrontierSnapshotID]
		newFrontier.ParentFrontierSnapshotIDs = rewriteIDs(frontier.ParentFrontierSnapshotIDs, mapping.frontier)
		newFrontier.TaskSnapshotIDs = rewriteIDs(frontier.TaskSnapshotIDs, mapping.snapshot)
		newFrontier.CreatedByEventID = rewriteID(frontier.CreatedByEventID, mapping.refMove)
		newFrontier.DetailsJSON = replaceKnownIDs(frontier.DetailsJSON, mapping.all)
		if err := repository.CreateFrontierSnapshot(ctx, newFrontier); err != nil {
			return ImportResult{}, err
		}
	}

	newRef := archive.Ref
	newRef.RefName = targetRefName
	newRef.RunID = opts.NewRunID
	newRef.FrontierSnapshotID = rewriteID(archive.Ref.FrontierSnapshotID, mapping.frontier)
	newRef.FrontierSnapshotIDs = rewriteIDs(archive.Ref.FrontierSnapshotIDs, mapping.snapshot)
	newRef.UpdatedAt = importedAt.UTC()
	if err := repository.UpdateRef(ctx, newRef); err != nil {
		return ImportResult{}, err
	}
	for _, event := range archive.RefMoveEvents {
		newEvent := event
		newEvent.EventID = mapping.refMove[event.EventID]
		newEvent.RunID = opts.NewRunID
		newEvent.RefName = targetRefName
		newEvent.FromFrontierSnapshotIDs = rewriteIDs(event.FromFrontierSnapshotIDs, mapping.frontier)
		newEvent.ToFrontierSnapshotIDs = rewriteIDs(event.ToFrontierSnapshotIDs, mapping.frontier)
		newEvent.Reason = replaceKnownIDs(event.Reason, mapping.all)
		newEvent.DetailsJSON = replaceKnownIDs(event.DetailsJSON, mapping.all)
		if err := repository.CreateRefMoveEvent(ctx, newEvent); err != nil {
			return ImportResult{}, err
		}
	}

	return ImportResult{
		RunID:              opts.NewRunID,
		RefName:            targetRefName,
		FrontierSnapshotID: newRef.FrontierSnapshotID,
		SnapshotCount:      len(archive.Snapshots),
		BagCount:           len(archive.Bags),
	}, nil
}

type archiveIDMapping struct {
	logical  map[string]string
	version  map[string]string
	bag      map[string]string
	snapshot map[string]string
	frontier map[string]string
	refMove  map[string]string
	all      map[string]string
}

func newArchiveIDMapping() archiveIDMapping {
	return archiveIDMapping{
		logical:  make(map[string]string),
		version:  make(map[string]string),
		bag:      make(map[string]string),
		snapshot: make(map[string]string),
		frontier: make(map[string]string),
		refMove:  make(map[string]string),
		all:      make(map[string]string),
	}
}

func sortedMapValues[T any](items map[string]T, key func(T) string) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return key(out[i]) < key(out[j])
	})
	return out
}

func sortRunArchive(archive *RunArchive) {
	sort.Slice(archive.Objects, func(i, j int) bool { return archive.Objects[i].ObjectID < archive.Objects[j].ObjectID })
	sort.Slice(archive.LogicalArtifacts, func(i, j int) bool {
		return archive.LogicalArtifacts[i].LogicalArtifactID < archive.LogicalArtifacts[j].LogicalArtifactID
	})
	sort.Slice(archive.ArtifactVersions, func(i, j int) bool {
		return archive.ArtifactVersions[i].ArtifactVersionID < archive.ArtifactVersions[j].ArtifactVersionID
	})
	sort.Slice(archive.Bags, func(i, j int) bool { return archive.Bags[i].BagID < archive.Bags[j].BagID })
	sort.Slice(archive.Snapshots, func(i, j int) bool {
		if !archive.Snapshots[i].CreatedAt.Equal(archive.Snapshots[j].CreatedAt) {
			return archive.Snapshots[i].CreatedAt.Before(archive.Snapshots[j].CreatedAt)
		}
		return archive.Snapshots[i].SnapshotID < archive.Snapshots[j].SnapshotID
	})
	sort.Slice(archive.FrontierSnapshots, func(i, j int) bool {
		if !archive.FrontierSnapshots[i].CreatedAt.Equal(archive.FrontierSnapshots[j].CreatedAt) {
			return archive.FrontierSnapshots[i].CreatedAt.Before(archive.FrontierSnapshots[j].CreatedAt)
		}
		return archive.FrontierSnapshots[i].FrontierSnapshotID < archive.FrontierSnapshots[j].FrontierSnapshotID
	})
	sort.Slice(archive.RefMoveEvents, func(i, j int) bool {
		if !archive.RefMoveEvents[i].CreatedAt.Equal(archive.RefMoveEvents[j].CreatedAt) {
			return archive.RefMoveEvents[i].CreatedAt.Before(archive.RefMoveEvents[j].CreatedAt)
		}
		return archive.RefMoveEvents[i].EventID < archive.RefMoveEvents[j].EventID
	})
}

func stableImportedID(kind string, runID core.RunID, oldID string) string {
	raw := strings.Join([]string{kind, string(runID), strings.TrimSpace(oldID)}, "\x00")
	return kind + ":" + shortHash(raw)
}

func rewriteIDs(items []string, mapping map[string]string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, rewriteID(item, mapping))
	}
	return NormalizeIDs(out)
}

func rewriteID(item string, mapping map[string]string) string {
	item = strings.TrimSpace(item)
	if item == "" {
		return ""
	}
	if mapped := mapping[item]; mapped != "" {
		return mapped
	}
	return item
}

func replaceKnownIDs(value string, mapping map[string]string) string {
	if value == "" || len(mapping) == 0 {
		return value
	}
	pairs := make([]string, 0, len(mapping)*2)
	for oldID, newID := range mapping {
		if oldID == "" || newID == "" || oldID == newID {
			continue
		}
		pairs = append(pairs, oldID, newID)
	}
	return strings.NewReplacer(pairs...).Replace(value)
}

func rewriteStorageURI(uri string, oldRunID core.RunID, newRunID core.RunID) string {
	normalized := filepath.ToSlash(strings.TrimSpace(uri))
	oldPrefix := "projects/" + string(oldRunID) + "/"
	if strings.HasPrefix(normalized, oldPrefix) {
		return "projects/" + string(newRunID) + "/" + strings.TrimPrefix(normalized, oldPrefix)
	}
	return normalized
}

func archiveObjectPath(objectID string) string {
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(strings.TrimSpace(objectID))
	return "objects/" + safe + ".blob"
}

func verifyArchiveFile(file ArchiveArtifactFile, content []byte) error {
	if file.Size >= 0 && int64(len(content)) != file.Size {
		return fmt.Errorf("archive file %q size = %d, want %d", file.ArchivePath, len(content), file.Size)
	}
	if file.SHA256 != "" {
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != file.SHA256 {
			return fmt.Errorf("archive file %q sha256 = %s, want %s", file.ArchivePath, got, file.SHA256)
		}
	}
	if file.ObjectID != "" {
		if got := StableObjectID(content); got != file.ObjectID {
			return fmt.Errorf("archive file %q object_id = %s, want %s", file.ArchivePath, got, file.ObjectID)
		}
	}
	return nil
}

func readArtifactFile(projectsRoot string, runID core.RunID, storageURI string) ([]byte, error) {
	fullPath, err := artifactFilePath(projectsRoot, runID, storageURI)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

func writeArtifactFile(projectsRoot string, runID core.RunID, storageURI string, content []byte) error {
	fullPath, err := artifactFilePath(projectsRoot, runID, storageURI)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0o644)
}

func artifactFilePath(projectsRoot string, runID core.RunID, storageURI string) (string, error) {
	projectsRoot = strings.TrimSpace(projectsRoot)
	storageURI = filepath.ToSlash(strings.TrimSpace(storageURI))
	if projectsRoot == "" {
		return "", fmt.Errorf("projects root is required")
	}
	if runID == "" {
		return "", fmt.Errorf("run_id is required")
	}
	if storageURI == "" {
		return "", fmt.Errorf("storage uri is required")
	}
	if filepath.IsAbs(storageURI) {
		return "", fmt.Errorf("storage uri must be relative: %s", storageURI)
	}
	relURI := storageURI
	prefix := "projects/" + string(runID) + "/"
	if strings.HasPrefix(relURI, prefix) {
		relURI = strings.TrimPrefix(relURI, prefix)
	}
	runRoot, err := filepath.Abs(filepath.Join(projectsRoot, string(runID)))
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(runRoot, filepath.FromSlash(relURI)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(runRoot, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage uri escapes run root: %s", storageURI)
	}
	return target, nil
}

func DecodeRunArchive(raw []byte) (RunArchive, error) {
	var archive RunArchive
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archive); err != nil {
		return RunArchive{}, err
	}
	return archive, nil
}
