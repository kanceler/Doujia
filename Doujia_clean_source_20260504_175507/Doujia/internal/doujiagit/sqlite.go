package doujiagit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"devflow/internal/core"
)

type SQLiteRepository struct {
	db *sql.DB
}

var _ Repository = (*SQLiteRepository)(nil)

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) Migrate(ctx context.Context) error {
	for _, stmt := range sqliteSchemaStatements {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := r.ensureColumn(ctx, "refs", "frontier_snapshot_id", "frontier_snapshot_id TEXT"); err != nil {
		return err
	}
	if err := r.ensureColumn(ctx, "frontier_snapshots", "details_json", "details_json TEXT"); err != nil {
		return err
	}
	if err := r.ensureColumn(ctx, "ref_move_events", "details_json", "details_json TEXT"); err != nil {
		return err
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "pipeline_instance_id", definition: "pipeline_instance_id TEXT"},
		{name: "transition_id", definition: "transition_id TEXT"},
		{name: "agent_role", definition: "agent_role TEXT"},
		{name: "agent_id", definition: "agent_id TEXT"},
		{name: "op", definition: "op TEXT"},
		{name: "runtime_context_json", definition: "runtime_context_json TEXT"},
	} {
		if err := r.ensureColumn(ctx, "task_snapshots", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) ensureColumn(ctx context.Context, tableName string, columnName string, definition string) error {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == columnName {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableName, definition))
	return err
}

func (r *SQLiteRepository) UpsertObject(ctx context.Context, obj ArtifactObject) (ArtifactObject, error) {
	obj, err := normalizeObject(obj)
	if err != nil {
		return ArtifactObject{}, err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO artifact_objects (object_id, object_type, storage_uri, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(object_id) DO UPDATE SET
  object_type = excluded.object_type,
  storage_uri = excluded.storage_uri`,
		obj.ObjectID, obj.ObjectType, nullString(obj.StorageURI), formatTime(obj.CreatedAt),
	)
	if err != nil {
		return ArtifactObject{}, err
	}
	return obj, nil
}

func (r *SQLiteRepository) GetObject(ctx context.Context, objectID string) (ArtifactObject, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT object_id, object_type, storage_uri, created_at
FROM artifact_objects WHERE object_id = ?`, strings.TrimSpace(objectID))
	return scanObject(row)
}

func (r *SQLiteRepository) UpsertLogicalArtifact(ctx context.Context, item LogicalArtifact) (LogicalArtifact, error) {
	item, err := normalizeLogicalArtifact(item)
	if err != nil {
		return LogicalArtifact{}, err
	}
	existing, err := r.getLogicalArtifactByKey(ctx, item.RunID, item.Namespace, item.LogicalKey)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return LogicalArtifact{}, err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO logical_artifacts (
  logical_artifact_id, run_id, namespace, logical_key, created_at
) VALUES (?, ?, ?, ?, ?)`,
		item.LogicalArtifactID, item.RunID, item.Namespace, item.LogicalKey, formatTime(item.CreatedAt),
	)
	if err != nil {
		return LogicalArtifact{}, err
	}
	return item, nil
}

func (r *SQLiteRepository) GetLogicalArtifact(ctx context.Context, logicalArtifactID string) (LogicalArtifact, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT logical_artifact_id, run_id, namespace, logical_key, created_at
FROM logical_artifacts WHERE logical_artifact_id = ?`, strings.TrimSpace(logicalArtifactID))
	return scanLogicalArtifact(row)
}

func (r *SQLiteRepository) UpsertArtifactVersion(ctx context.Context, version ArtifactVersion) (ArtifactVersion, error) {
	version, err := normalizeArtifactVersion(version)
	if err != nil {
		return ArtifactVersion{}, err
	}
	if err := r.requireLogicalArtifact(ctx, version.LogicalArtifactID); err != nil {
		return ArtifactVersion{}, err
	}
	for _, objectID := range version.ObjectIDs {
		if err := r.requireObject(ctx, objectID); err != nil {
			return ArtifactVersion{}, err
		}
	}
	objectIDsJSON, err := marshalStringSlice(version.ObjectIDs)
	if err != nil {
		return ArtifactVersion{}, err
	}
	existing, err := r.getArtifactVersionByMembership(ctx, version.LogicalArtifactID, objectIDsJSON)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return ArtifactVersion{}, err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO artifact_versions (
  artifact_version_id, logical_artifact_id, object_ids_json, created_at
) VALUES (?, ?, ?, ?)`,
		version.ArtifactVersionID, version.LogicalArtifactID, objectIDsJSON, formatTime(version.CreatedAt),
	)
	if err != nil {
		return ArtifactVersion{}, err
	}
	return version, nil
}

func (r *SQLiteRepository) GetArtifactVersion(ctx context.Context, versionID string) (ArtifactVersion, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT artifact_version_id, logical_artifact_id, object_ids_json, created_at
FROM artifact_versions WHERE artifact_version_id = ?`, strings.TrimSpace(versionID))
	return scanArtifactVersion(row)
}

func (r *SQLiteRepository) CreateBag(ctx context.Context, bag ArtifactBag) error {
	bag, err := normalizeBag(bag)
	if err != nil {
		return err
	}
	for _, versionID := range bag.ArtifactVersionIDs {
		if err := r.requireVersion(ctx, versionID); err != nil {
			return err
		}
	}
	versionIDsJSON, err := marshalStringSlice(bag.ArtifactVersionIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO artifact_bags (
  bag_id, run_id, artifact_version_ids_json, created_at
) VALUES (?, ?, ?, ?)`,
		bag.BagID, bag.RunID, versionIDsJSON, formatTime(bag.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetBag(ctx context.Context, bagID string) (ArtifactBag, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT bag_id, run_id, artifact_version_ids_json, created_at
FROM artifact_bags WHERE bag_id = ?`, strings.TrimSpace(bagID))
	return scanBag(row)
}

func (r *SQLiteRepository) ListBagsByRun(ctx context.Context, runID core.RunID) ([]ArtifactBag, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT bag_id, run_id, artifact_version_ids_json, created_at
FROM artifact_bags
WHERE run_id = ?
ORDER BY created_at ASC, bag_id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ArtifactBag, 0)
	for rows.Next() {
		item, err := scanBag(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) CreateSnapshot(ctx context.Context, snapshot TaskSnapshot) error {
	snapshot, err := normalizeSnapshot(snapshot)
	if err != nil {
		return err
	}
	for _, bagID := range snapshot.InputBagIDs {
		if err := r.requireBag(ctx, bagID); err != nil {
			return err
		}
	}
	for _, bagID := range snapshot.OutputBagIDs {
		if err := r.requireBag(ctx, bagID); err != nil {
			return err
		}
		if producer, ok, err := r.findProducerOfBag(ctx, bagID); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("artifact bag %q already produced by snapshot %q", bagID, producer)
		}
	}
	inputBagIDsJSON, err := marshalStringSlice(snapshot.InputBagIDs)
	if err != nil {
		return err
	}
	outputBagIDsJSON, err := marshalStringSlice(snapshot.OutputBagIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO task_snapshots (
  snapshot_id, run_id, task_id, pipeline_instance_id, transition_id, agent_role, agent_id, op,
  result, input_bag_ids_json, output_bag_ids_json, diagnostics_json, runtime_context_json,
  created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.SnapshotID, snapshot.RunID, snapshot.TaskID,
		nullString(string(snapshot.PipelineInstanceID)), nullString(string(snapshot.TransitionID)),
		nullString(string(snapshot.AgentRole)), nullString(string(snapshot.AgentID)), nullString(snapshot.Op),
		snapshot.Result, inputBagIDsJSON, outputBagIDsJSON, nullString(snapshot.DiagnosticsJSON), nullString(snapshot.RuntimeContextJSON),
		formatTime(snapshot.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetSnapshot(ctx context.Context, snapshotID string) (TaskSnapshot, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT snapshot_id, run_id, task_id, pipeline_instance_id, transition_id, agent_role, agent_id, op,
       result, input_bag_ids_json, output_bag_ids_json, diagnostics_json, runtime_context_json,
       created_at
FROM task_snapshots WHERE snapshot_id = ?`, strings.TrimSpace(snapshotID))
	return scanSnapshot(row)
}

func (r *SQLiteRepository) ListSnapshotsByRun(ctx context.Context, runID core.RunID) ([]TaskSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT snapshot_id, run_id, task_id, pipeline_instance_id, transition_id, agent_role, agent_id, op,
       result, input_bag_ids_json, output_bag_ids_json, diagnostics_json, runtime_context_json,
       created_at
FROM task_snapshots
WHERE run_id = ?
ORDER BY created_at ASC, snapshot_id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TaskSnapshot, 0)
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) ProducerOfBag(ctx context.Context, bagID string) (string, error) {
	producer, ok, err := r.findProducerOfBag(ctx, bagID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("producer snapshot for artifact bag %q not found", bagID)
	}
	return producer, nil
}

func (r *SQLiteRepository) findProducerOfBag(ctx context.Context, bagID string) (string, bool, error) {
	bagID = strings.TrimSpace(bagID)
	rows, err := r.db.QueryContext(ctx, `
SELECT snapshot_id, output_bag_ids_json
FROM task_snapshots`)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()

	found := ""
	for rows.Next() {
		var snapshotID string
		var outputBagIDsJSON string
		if err := rows.Scan(&snapshotID, &outputBagIDsJSON); err != nil {
			return "", false, err
		}
		var outputBagIDs []string
		if err := json.Unmarshal([]byte(outputBagIDsJSON), &outputBagIDs); err != nil {
			return "", false, fmt.Errorf("unmarshal output bag ids for snapshot %q: %w", snapshotID, err)
		}
		for _, outputBagID := range outputBagIDs {
			if outputBagID != bagID {
				continue
			}
			if found != "" && found != snapshotID {
				return "", false, fmt.Errorf("artifact bag %q has multiple producer snapshots: %q and %q", bagID, found, snapshotID)
			}
			found = snapshotID
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if found == "" {
		return "", false, nil
	}
	return found, true, nil
}

func (r *SQLiteRepository) CreateFrontierSnapshot(ctx context.Context, snapshot FrontierSnapshot) error {
	snapshot, err := normalizeFrontierSnapshot(snapshot)
	if err != nil {
		return err
	}
	for _, taskSnapshotID := range snapshot.TaskSnapshotIDs {
		if err := r.requireSnapshot(ctx, taskSnapshotID); err != nil {
			return err
		}
	}
	for _, parentID := range snapshot.ParentFrontierSnapshotIDs {
		if err := r.requireFrontierSnapshot(ctx, parentID); err != nil {
			return err
		}
	}
	parentIDsJSON, err := marshalStringSlice(snapshot.ParentFrontierSnapshotIDs)
	if err != nil {
		return err
	}
	taskIDsJSON, err := marshalStringSlice(snapshot.TaskSnapshotIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO frontier_snapshots (
  frontier_snapshot_id, run_id, parent_frontier_snapshot_ids_json,
  task_snapshot_ids_json, created_by_mode, created_by_event_id, details_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.FrontierSnapshotID, snapshot.RunID, parentIDsJSON, taskIDsJSON,
		nullString(snapshot.CreatedByMode), nullString(snapshot.CreatedByEventID),
		nullString(snapshot.DetailsJSON), formatTime(snapshot.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetFrontierSnapshot(ctx context.Context, frontierSnapshotID string) (FrontierSnapshot, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT frontier_snapshot_id, run_id, parent_frontier_snapshot_ids_json,
       task_snapshot_ids_json, created_by_mode, created_by_event_id, details_json, created_at
FROM frontier_snapshots WHERE frontier_snapshot_id = ?`, strings.TrimSpace(frontierSnapshotID))
	return scanFrontierSnapshot(row)
}

func (r *SQLiteRepository) ListFrontierSnapshotsByRun(ctx context.Context, runID core.RunID) ([]FrontierSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT frontier_snapshot_id, run_id, parent_frontier_snapshot_ids_json,
       task_snapshot_ids_json, created_by_mode, created_by_event_id, details_json, created_at
FROM frontier_snapshots
WHERE run_id = ?
ORDER BY created_at ASC, frontier_snapshot_id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]FrontierSnapshot, 0)
	for rows.Next() {
		item, err := scanFrontierSnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) UpdateRef(ctx context.Context, ref Ref) error {
	ref, err := normalizeRef(ref)
	if err != nil {
		return err
	}
	if ref.FrontierSnapshotID != "" {
		if err := r.requireFrontierSnapshot(ctx, ref.FrontierSnapshotID); err != nil {
			return err
		}
	}
	for _, snapshotID := range ref.FrontierSnapshotIDs {
		if err := r.requireSnapshot(ctx, snapshotID); err != nil {
			return err
		}
	}
	frontierJSON, err := marshalStringSlice(ref.FrontierSnapshotIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO refs (ref_name, run_id, frontier_snapshot_id, frontier_snapshot_ids_json, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(ref_name, run_id) DO UPDATE SET
  frontier_snapshot_id = excluded.frontier_snapshot_id,
  frontier_snapshot_ids_json = excluded.frontier_snapshot_ids_json,
  updated_at = excluded.updated_at`,
		ref.RefName, ref.RunID, nullString(ref.FrontierSnapshotID), frontierJSON, formatTime(ref.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) MoveRef(ctx context.Context, req MoveRefRequest) (Ref, RefMoveEvent, error) {
	ref, err := normalizeRef(req.Ref)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	event := req.Event
	event.RunID = ref.RunID
	event.RefName = ref.RefName
	if strings.TrimSpace(ref.FrontierSnapshotID) != "" {
		event.ToFrontierSnapshotIDs = []string{ref.FrontierSnapshotID}
	} else {
		event.ToFrontierSnapshotIDs = append([]string(nil), ref.FrontierSnapshotIDs...)
	}
	event, err = normalizeRefMoveEvent(event)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	if ref.FrontierSnapshotID != "" {
		if err := r.requireFrontierSnapshot(ctx, ref.FrontierSnapshotID); err != nil {
			return Ref{}, RefMoveEvent{}, err
		}
	}
	for _, snapshotID := range ref.FrontierSnapshotIDs {
		if err := r.requireSnapshot(ctx, snapshotID); err != nil {
			return Ref{}, RefMoveEvent{}, err
		}
	}
	for _, frontierID := range event.FromFrontierSnapshotIDs {
		if err := r.requireFrontierSnapshot(ctx, frontierID); err != nil {
			return Ref{}, RefMoveEvent{}, err
		}
	}
	for _, frontierID := range event.ToFrontierSnapshotIDs {
		if err := r.requireFrontierSnapshot(ctx, frontierID); err != nil {
			return Ref{}, RefMoveEvent{}, err
		}
	}
	frontierJSON, err := marshalStringSlice(ref.FrontierSnapshotIDs)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	fromJSON, err := marshalStringSlice(event.FromFrontierSnapshotIDs)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	toJSON, err := marshalStringSlice(event.ToFrontierSnapshotIDs)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	defer tx.Rollback()

	var current sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT frontier_snapshot_id
FROM refs
WHERE run_id = ? AND ref_name = ?`, ref.RunID, ref.RefName).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return Ref{}, RefMoveEvent{}, err
	}
	currentID := ""
	if current.Valid {
		currentID = current.String
	}
	if strings.TrimSpace(currentID) != strings.TrimSpace(req.ExpectedFrontierSnapshotID) {
		return Ref{}, RefMoveEvent{}, fmt.Errorf("%w: ref %q in run %q moved from %q to %q",
			ErrRefCASConflict, ref.RefName, ref.RunID, req.ExpectedFrontierSnapshotID, currentID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO refs (ref_name, run_id, frontier_snapshot_id, frontier_snapshot_ids_json, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(ref_name, run_id) DO UPDATE SET
  frontier_snapshot_id = excluded.frontier_snapshot_id,
  frontier_snapshot_ids_json = excluded.frontier_snapshot_ids_json,
  updated_at = excluded.updated_at`,
		ref.RefName, ref.RunID, nullString(ref.FrontierSnapshotID), frontierJSON, formatTime(ref.UpdatedAt),
	); err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO ref_move_events (
  event_id, run_id, ref_name, from_frontier_snapshot_ids_json,
  to_frontier_snapshot_ids_json, mode, reason, details_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.RunID, event.RefName, fromJSON, toJSON,
		event.Mode, nullString(event.Reason), nullString(event.DetailsJSON), formatTime(event.CreatedAt),
	); err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ref{}, RefMoveEvent{}, err
	}
	return ref, event, nil
}

func (r *SQLiteRepository) GetRef(ctx context.Context, runID core.RunID, refName string) (Ref, error) {
	if strings.TrimSpace(refName) == "" {
		refName = DefaultRefName
	}
	row := r.db.QueryRowContext(ctx, `
SELECT ref_name, run_id, frontier_snapshot_id, frontier_snapshot_ids_json, updated_at
FROM refs WHERE run_id = ? AND ref_name = ?`, runID, strings.TrimSpace(refName))
	return scanRef(row)
}

func (r *SQLiteRepository) ListRefsByRun(ctx context.Context, runID core.RunID) ([]Ref, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT ref_name, run_id, frontier_snapshot_id, frontier_snapshot_ids_json, updated_at
FROM refs
WHERE run_id = ?
ORDER BY updated_at DESC, ref_name ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Ref, 0)
	for rows.Next() {
		item, err := scanRef(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) CreateRefMoveEvent(ctx context.Context, event RefMoveEvent) error {
	event, err := normalizeRefMoveEvent(event)
	if err != nil {
		return err
	}
	for _, frontierID := range event.FromFrontierSnapshotIDs {
		if err := r.requireFrontierSnapshot(ctx, frontierID); err != nil {
			return err
		}
	}
	for _, frontierID := range event.ToFrontierSnapshotIDs {
		if err := r.requireFrontierSnapshot(ctx, frontierID); err != nil {
			return err
		}
	}
	fromJSON, err := marshalStringSlice(event.FromFrontierSnapshotIDs)
	if err != nil {
		return err
	}
	toJSON, err := marshalStringSlice(event.ToFrontierSnapshotIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO ref_move_events (
  event_id, run_id, ref_name, from_frontier_snapshot_ids_json,
  to_frontier_snapshot_ids_json, mode, reason, details_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.RunID, event.RefName, fromJSON, toJSON,
		event.Mode, nullString(event.Reason), nullString(event.DetailsJSON), formatTime(event.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) ListRefMoveEvents(ctx context.Context, runID core.RunID, refName string) ([]RefMoveEvent, error) {
	if strings.TrimSpace(refName) == "" {
		refName = DefaultRefName
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT event_id, run_id, ref_name, from_frontier_snapshot_ids_json,
       to_frontier_snapshot_ids_json, mode, reason, details_json, created_at
FROM ref_move_events
WHERE run_id = ? AND ref_name = ?
ORDER BY created_at ASC, event_id ASC`, runID, strings.TrimSpace(refName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RefMoveEvent, 0)
	for rows.Next() {
		item, err := scanRefMoveEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SQLiteRepository) getLogicalArtifactByKey(ctx context.Context, runID core.RunID, namespace string, logicalKey string) (LogicalArtifact, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT logical_artifact_id, run_id, namespace, logical_key, created_at
FROM logical_artifacts
WHERE run_id = ? AND namespace = ? AND logical_key = ?`,
		runID, strings.TrimSpace(namespace), strings.TrimSpace(logicalKey),
	)
	return scanLogicalArtifact(row)
}

func (r *SQLiteRepository) getArtifactVersionByMembership(ctx context.Context, logicalArtifactID string, objectIDsJSON string) (ArtifactVersion, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT artifact_version_id, logical_artifact_id, object_ids_json, created_at
FROM artifact_versions
WHERE logical_artifact_id = ? AND object_ids_json = ?`,
		strings.TrimSpace(logicalArtifactID), objectIDsJSON,
	)
	return scanArtifactVersion(row)
}

func (r *SQLiteRepository) requireLogicalArtifact(ctx context.Context, logicalArtifactID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT logical_artifact_id FROM logical_artifacts WHERE logical_artifact_id = ?`,
		strings.TrimSpace(logicalArtifactID),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("logical artifact %q not found", logicalArtifactID)
	}
	return err
}

func (r *SQLiteRepository) requireObject(ctx context.Context, objectID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT object_id FROM artifact_objects WHERE object_id = ?`,
		strings.TrimSpace(objectID),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("artifact object %q not found", objectID)
	}
	return err
}

func (r *SQLiteRepository) requireVersion(ctx context.Context, versionID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT artifact_version_id FROM artifact_versions WHERE artifact_version_id = ?`,
		strings.TrimSpace(versionID),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("artifact version %q not found", versionID)
	}
	return err
}

func (r *SQLiteRepository) requireBag(ctx context.Context, bagID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT bag_id FROM artifact_bags WHERE bag_id = ?`, strings.TrimSpace(bagID)).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("artifact bag %q not found", bagID)
	}
	return err
}

func (r *SQLiteRepository) requireSnapshot(ctx context.Context, snapshotID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT snapshot_id FROM task_snapshots WHERE snapshot_id = ?`,
		strings.TrimSpace(snapshotID),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task snapshot %q not found", snapshotID)
	}
	return err
}

func (r *SQLiteRepository) requireFrontierSnapshot(ctx context.Context, frontierSnapshotID string) error {
	var id string
	err := r.db.QueryRowContext(ctx, `
SELECT frontier_snapshot_id FROM frontier_snapshots WHERE frontier_snapshot_id = ?`,
		strings.TrimSpace(frontierSnapshotID),
	).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("frontier snapshot %q not found", frontierSnapshotID)
	}
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanObject(row rowScanner) (ArtifactObject, error) {
	var obj ArtifactObject
	var storageURI sql.NullString
	var createdAt string
	if err := row.Scan(&obj.ObjectID, &obj.ObjectType, &storageURI, &createdAt); err != nil {
		return ArtifactObject{}, err
	}
	if storageURI.Valid {
		obj.StorageURI = storageURI.String
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return ArtifactObject{}, err
	}
	obj.CreatedAt = parsed
	return obj, nil
}

func scanLogicalArtifact(row rowScanner) (LogicalArtifact, error) {
	var item LogicalArtifact
	var createdAt string
	if err := row.Scan(&item.LogicalArtifactID, &item.RunID, &item.Namespace, &item.LogicalKey, &createdAt); err != nil {
		return LogicalArtifact{}, err
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return LogicalArtifact{}, err
	}
	item.CreatedAt = parsed
	return item, nil
}

func scanArtifactVersion(row rowScanner) (ArtifactVersion, error) {
	var version ArtifactVersion
	var objectIDsJSON string
	var createdAt string
	if err := row.Scan(&version.ArtifactVersionID, &version.LogicalArtifactID, &objectIDsJSON, &createdAt); err != nil {
		return ArtifactVersion{}, err
	}
	if err := json.Unmarshal([]byte(objectIDsJSON), &version.ObjectIDs); err != nil {
		return ArtifactVersion{}, fmt.Errorf("unmarshal artifact version object ids: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return ArtifactVersion{}, err
	}
	version.CreatedAt = parsed
	return version, nil
}

func scanBag(row rowScanner) (ArtifactBag, error) {
	var bag ArtifactBag
	var versionIDsJSON string
	var createdAt string
	if err := row.Scan(&bag.BagID, &bag.RunID, &versionIDsJSON, &createdAt); err != nil {
		return ArtifactBag{}, err
	}
	if err := json.Unmarshal([]byte(versionIDsJSON), &bag.ArtifactVersionIDs); err != nil {
		return ArtifactBag{}, fmt.Errorf("unmarshal artifact bag version ids: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return ArtifactBag{}, err
	}
	bag.CreatedAt = parsed
	return bag, nil
}

func scanSnapshot(row rowScanner) (TaskSnapshot, error) {
	var snapshot TaskSnapshot
	var inputBagIDsJSON, outputBagIDsJSON string
	var pipelineInstanceID, transitionID, agentRole, agentID, op, diagnostics, runtimeContext sql.NullString
	var createdAt string
	if err := row.Scan(
		&snapshot.SnapshotID, &snapshot.RunID, &snapshot.TaskID,
		&pipelineInstanceID, &transitionID, &agentRole, &agentID, &op,
		&snapshot.Result, &inputBagIDsJSON, &outputBagIDsJSON, &diagnostics, &runtimeContext,
		&createdAt,
	); err != nil {
		return TaskSnapshot{}, err
	}
	if pipelineInstanceID.Valid {
		snapshot.PipelineInstanceID = core.PipelineInstanceID(pipelineInstanceID.String)
	}
	if transitionID.Valid {
		snapshot.TransitionID = core.StageID(transitionID.String)
	}
	if agentRole.Valid {
		snapshot.AgentRole = core.AgentRole(agentRole.String)
	}
	if agentID.Valid {
		snapshot.AgentID = core.AgentID(agentID.String)
	}
	if op.Valid {
		snapshot.Op = op.String
	}
	if err := json.Unmarshal([]byte(inputBagIDsJSON), &snapshot.InputBagIDs); err != nil {
		return TaskSnapshot{}, fmt.Errorf("unmarshal snapshot input bag ids: %w", err)
	}
	if err := json.Unmarshal([]byte(outputBagIDsJSON), &snapshot.OutputBagIDs); err != nil {
		return TaskSnapshot{}, fmt.Errorf("unmarshal snapshot output bag ids: %w", err)
	}
	if diagnostics.Valid {
		snapshot.DiagnosticsJSON = diagnostics.String
	}
	if runtimeContext.Valid {
		snapshot.RuntimeContextJSON = runtimeContext.String
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return TaskSnapshot{}, err
	}
	snapshot.CreatedAt = parsed
	return snapshot, nil
}

func scanFrontierSnapshot(row rowScanner) (FrontierSnapshot, error) {
	var snapshot FrontierSnapshot
	var parentIDsJSON, taskIDsJSON string
	var createdByMode, createdByEventID, details sql.NullString
	var createdAt string
	if err := row.Scan(
		&snapshot.FrontierSnapshotID, &snapshot.RunID, &parentIDsJSON, &taskIDsJSON,
		&createdByMode, &createdByEventID, &details, &createdAt,
	); err != nil {
		return FrontierSnapshot{}, err
	}
	if err := json.Unmarshal([]byte(parentIDsJSON), &snapshot.ParentFrontierSnapshotIDs); err != nil {
		return FrontierSnapshot{}, fmt.Errorf("unmarshal frontier parent ids: %w", err)
	}
	if err := json.Unmarshal([]byte(taskIDsJSON), &snapshot.TaskSnapshotIDs); err != nil {
		return FrontierSnapshot{}, fmt.Errorf("unmarshal frontier task snapshot ids: %w", err)
	}
	if createdByMode.Valid {
		snapshot.CreatedByMode = createdByMode.String
	}
	if createdByEventID.Valid {
		snapshot.CreatedByEventID = createdByEventID.String
	}
	if details.Valid {
		snapshot.DetailsJSON = details.String
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return FrontierSnapshot{}, err
	}
	snapshot.CreatedAt = parsed
	return snapshot, nil
}

func scanRef(row rowScanner) (Ref, error) {
	var ref Ref
	var frontierSnapshotID sql.NullString
	var frontierJSON string
	var updatedAt string
	if err := row.Scan(&ref.RefName, &ref.RunID, &frontierSnapshotID, &frontierJSON, &updatedAt); err != nil {
		return Ref{}, err
	}
	if frontierSnapshotID.Valid {
		ref.FrontierSnapshotID = frontierSnapshotID.String
	}
	if err := json.Unmarshal([]byte(frontierJSON), &ref.FrontierSnapshotIDs); err != nil {
		return Ref{}, fmt.Errorf("unmarshal ref frontier snapshot ids: %w", err)
	}
	parsed, err := parseTime(updatedAt)
	if err != nil {
		return Ref{}, err
	}
	ref.UpdatedAt = parsed
	return ref, nil
}

func scanRefMoveEvent(row rowScanner) (RefMoveEvent, error) {
	var event RefMoveEvent
	var fromJSON, toJSON string
	var reason, details sql.NullString
	var createdAt string
	if err := row.Scan(
		&event.EventID, &event.RunID, &event.RefName, &fromJSON, &toJSON,
		&event.Mode, &reason, &details, &createdAt,
	); err != nil {
		return RefMoveEvent{}, err
	}
	if err := json.Unmarshal([]byte(fromJSON), &event.FromFrontierSnapshotIDs); err != nil {
		return RefMoveEvent{}, fmt.Errorf("unmarshal ref move from frontier ids: %w", err)
	}
	if err := json.Unmarshal([]byte(toJSON), &event.ToFrontierSnapshotIDs); err != nil {
		return RefMoveEvent{}, fmt.Errorf("unmarshal ref move to frontier ids: %w", err)
	}
	if reason.Valid {
		event.Reason = reason.String
	}
	if details.Valid {
		event.DetailsJSON = details.String
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return RefMoveEvent{}, err
	}
	event.CreatedAt = parsed
	return event, nil
}

func marshalStringSlice(items []string) (string, error) {
	raw, err := json.Marshal(NormalizeIDs(items))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func nullString(value string) sql.NullString {
	if strings.TrimSpace(value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

var sqliteSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS artifact_objects (
		object_id TEXT PRIMARY KEY,
		object_type TEXT NOT NULL,
		storage_uri TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS logical_artifacts (
		logical_artifact_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		namespace TEXT NOT NULL,
		logical_key TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE (run_id, namespace, logical_key)
	)`,
	`CREATE TABLE IF NOT EXISTS artifact_versions (
		artifact_version_id TEXT PRIMARY KEY,
		logical_artifact_id TEXT NOT NULL,
		object_ids_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE (logical_artifact_id, object_ids_json),
		FOREIGN KEY (logical_artifact_id) REFERENCES logical_artifacts(logical_artifact_id)
	)`,
	`CREATE TABLE IF NOT EXISTS artifact_bags (
		bag_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		artifact_version_ids_json TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS task_snapshots (
		snapshot_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		task_id TEXT NOT NULL,
		pipeline_instance_id TEXT,
		transition_id TEXT,
		agent_role TEXT,
		agent_id TEXT,
		op TEXT,
		result TEXT NOT NULL,
		input_bag_ids_json TEXT NOT NULL,
		output_bag_ids_json TEXT NOT NULL,
		diagnostics_json TEXT,
		runtime_context_json TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS frontier_snapshots (
		frontier_snapshot_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		parent_frontier_snapshot_ids_json TEXT NOT NULL,
		task_snapshot_ids_json TEXT NOT NULL,
		created_by_mode TEXT,
		created_by_event_id TEXT,
		details_json TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS refs (
		ref_name TEXT NOT NULL,
		run_id TEXT NOT NULL,
		frontier_snapshot_id TEXT,
		frontier_snapshot_ids_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (ref_name, run_id)
	)`,
	`CREATE TABLE IF NOT EXISTS ref_move_events (
		event_id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		ref_name TEXT NOT NULL,
		from_frontier_snapshot_ids_json TEXT NOT NULL,
		to_frontier_snapshot_ids_json TEXT NOT NULL,
		mode TEXT NOT NULL,
		reason TEXT,
		details_json TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_logical_artifacts_run ON logical_artifacts (run_id)`,
	`CREATE INDEX IF NOT EXISTS idx_artifact_versions_logical ON artifact_versions (logical_artifact_id)`,
	`CREATE INDEX IF NOT EXISTS idx_artifact_bags_run ON artifact_bags (run_id)`,
	`CREATE INDEX IF NOT EXISTS idx_snapshots_run_task ON task_snapshots (run_id, task_id)`,
	`CREATE INDEX IF NOT EXISTS idx_frontier_snapshots_run ON frontier_snapshots (run_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_ref_move_events_run_ref ON ref_move_events (run_id, ref_name, created_at)`,
}
