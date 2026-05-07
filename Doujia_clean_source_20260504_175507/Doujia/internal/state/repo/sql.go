package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"devflow/internal/core"
)

type SQLStore struct {
	db *sql.DB
}

var _ RunRepository = (*SQLRunRepository)(nil)
var _ ProjectRepository = (*SQLProjectRepository)(nil)
var _ TaskRepository = (*SQLTaskRepository)(nil)
var _ PipelineInstanceRepository = (*SQLPipelineInstanceRepository)(nil)
var _ ArtifactRepository = (*SQLArtifactRepository)(nil)
var _ EventRepository = (*SQLEventRepository)(nil)
var _ RunIterationRepository = (*SQLRunIterationRepository)(nil)
var _ SessionMessageRepository = (*SQLSessionMessageRepository)(nil)
var _ SessionArtifactRepository = (*SQLSessionArtifactRepository)(nil)
var _ ImprovementItemRepository = (*SQLImprovementItemRepository)(nil)

type SQLRepositories struct {
	Store            *SQLStore
	Runs             RunRepository
	Projects         ProjectRepository
	Tasks            TaskRepository
	Instances        PipelineInstanceRepository
	Artifacts        ArtifactRepository
	Events           EventRepository
	Iterations       RunIterationRepository
	SessionMessages  SessionMessageRepository
	SessionArtifacts SessionArtifactRepository
	ImprovementItems ImprovementItemRepository
}

func NewSQLRepositories(db *sql.DB) SQLRepositories {
	store := NewSQLStore(db)
	return SQLRepositories{
		Store:            store,
		Runs:             NewSQLRunRepository(store),
		Projects:         NewSQLProjectRepository(store),
		Tasks:            NewSQLTaskRepository(store),
		Instances:        NewSQLPipelineInstanceRepository(store),
		Artifacts:        NewSQLArtifactRepository(store),
		Events:           NewSQLEventRepository(store),
		Iterations:       NewSQLRunIterationRepository(store),
		SessionMessages:  NewSQLSessionMessageRepository(store),
		SessionArtifacts: NewSQLSessionArtifactRepository(store),
		ImprovementItems: NewSQLImprovementItemRepository(store),
	}
}

func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) Migrate(ctx context.Context) error {
	for _, stmt := range sqlSchemaStatements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureTaskColumns(ctx); err != nil {
		return err
	}
	return s.ensureRunColumns(ctx)
}

func (s *SQLStore) ensureTaskColumns(ctx context.Context) error {
	columns, err := s.tableColumns(ctx, "tasks")
	if err != nil {
		return err
	}
	for column, typ := range map[string]string{
		"pipeline_instance_id": "TEXT",
		"input_bag_ids_json":   "TEXT",
		"input_bags_json":      "TEXT",
		"output_bag_ids_json":  "TEXT",
		"exception_frame_id":   "TEXT",
	} {
		if columns[column] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE tasks ADD COLUMN "+column+" "+typ); err != nil {
			return err
		}
	}
	instanceColumns, err := s.tableColumns(ctx, "pipeline_instances")
	if err != nil {
		return err
	}
	for column, typ := range map[string]string{
		"input_bag_id_lists_json":  "TEXT",
		"output_bag_id_lists_json": "TEXT",
		"handler_bindings_json":    "TEXT",
		"exception_frames_json":    "TEXT",
	} {
		if instanceColumns[column] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE pipeline_instances ADD COLUMN "+column+" "+typ); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) ensureRunColumns(ctx context.Context) error {
	columns, err := s.tableColumns(ctx, "runs")
	if err != nil {
		return err
	}
	for column, typ := range map[string]string{
		"project_id":                           "TEXT",
		"current_iteration_no":                 "INTEGER NOT NULL DEFAULT 0",
		"latest_delivery_frontier_id":          "TEXT",
		"latest_acceptance_checkpoint_task_id": "TEXT",
	} {
		if columns[column] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE runs ADD COLUMN "+column+" "+typ); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func (s *SQLStore) Create(ctx context.Context, run RunRecord) error {
	configJSON, err := marshalJSON(run.Config)
	if err != nil {
		return fmt.Errorf("marshal run config: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO runs (
	id, pipeline_id, status, project_id, project_dir, session_id, current_iteration_no, latest_delivery_frontier_id, latest_acceptance_checkpoint_task_id, config_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.PipelineID, run.Status, nullString(run.ProjectID), run.ProjectDir,
		nullString(string(run.SessionID)), run.CurrentIterationNo,
		nullString(run.LatestDeliveryFrontierID), nullString(string(run.LatestAcceptanceCheckpointTaskID)), nullString(configJSON),
		formatTime(run.CreatedAt), formatTime(run.UpdatedAt),
	)
	return err
}

func (s *SQLStore) Get(ctx context.Context, runID core.RunID) (RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, pipeline_id, status, project_id, project_dir, session_id, current_iteration_no, latest_delivery_frontier_id, latest_acceptance_checkpoint_task_id, config_json, created_at, updated_at
FROM runs WHERE id = ?`, runID)
	return scanRun(row)
}

func (s *SQLStore) List(ctx context.Context) ([]RunRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, pipeline_id, status, project_id, project_dir, session_id, current_iteration_no, latest_delivery_frontier_id, latest_acceptance_checkpoint_task_id, config_json, created_at, updated_at
FROM runs ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RunRecord, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *SQLStore) Update(ctx context.Context, run RunRecord) error {
	configJSON, err := marshalJSON(run.Config)
	if err != nil {
		return fmt.Errorf("marshal run config: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE runs
SET pipeline_id = ?, status = ?, project_id = ?, project_dir = ?, session_id = ?, current_iteration_no = ?, latest_delivery_frontier_id = ?, latest_acceptance_checkpoint_task_id = ?, config_json = ?, updated_at = ?
WHERE id = ?`,
		run.PipelineID, run.Status, nullString(run.ProjectID), run.ProjectDir, nullString(string(run.SessionID)),
		run.CurrentIterationNo, nullString(run.LatestDeliveryFrontierID), nullString(string(run.LatestAcceptanceCheckpointTaskID)),
		nullString(configJSON), formatTime(run.UpdatedAt), run.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "run", string(run.ID))
}

func (s *SQLStore) CreateTask(ctx context.Context, task TaskRecord) error {
	return s.insertTask(ctx, task)
}

func (s *SQLStore) GetTask(ctx context.Context, runID core.RunID, taskID core.TaskID) (TaskRecord, error) {
	row := s.db.QueryRowContext(ctx, taskSelectSQL+" WHERE run_id = ? AND id = ?", runID, taskID)
	return scanTask(row)
}

func (s *SQLStore) UpdateTask(ctx context.Context, task TaskRecord) error {
	dependsOnIDsJSON, inputRefsJSON, outputRefsJSON, inputBagIDsJSON, inputBagsJSON, outputBagIDsJSON, err := marshalTaskJSONFields(task)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET stage_id = ?, op = ?, agent_role = ?, agent_id = ?, status = ?, result = ?, error_message = ?,
    pipeline_instance_id = ?, parent_id = ?, depends_on = ?, depends_on_ids_json = ?, input_artifact_uris_json = ?,
    output_artifact_uris_json = ?, input_bag_ids_json = ?, input_bags_json = ?, output_bag_ids_json = ?, exception_frame_id = ?, updated_at = ?
WHERE run_id = ? AND id = ?`,
		task.StageID, task.Op, task.AgentRole, task.AgentID, task.Status,
		nullString(string(task.Result)), nullString(task.ErrorMessage),
		nullString(string(task.PipelineInstanceID)), nullTaskID(task.ParentID), legacyDependsOn(task.DependsOnIDs),
		nullString(dependsOnIDsJSON), nullString(inputRefsJSON), nullString(outputRefsJSON),
		nullString(inputBagIDsJSON), nullString(inputBagsJSON), nullString(outputBagIDsJSON),
		nullString(string(task.ExceptionFrameID)), formatTime(task.UpdatedAt),
		task.RunID, task.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "task", string(task.ID))
}

func (s *SQLStore) ListTasksByRun(ctx context.Context, runID core.RunID) ([]TaskRecord, error) {
	rows, err := s.db.QueryContext(ctx, taskSelectSQL+" WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TaskRecord, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (s *SQLStore) CreatePipelineInstance(ctx context.Context, instance PipelineInstanceRecord) error {
	paramsJSON, agentBindingsJSON, inputBagIDsJSON, inputBagIDListsJSON, outputBagIDsJSON, outputBagIDListsJSON, handlerBindingsJSON, exceptionFramesJSON, err := marshalPipelineInstanceJSONFields(instance)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO pipeline_instances (
	id, run_id, pipeline_id, parent_id, parent_transition_id, instance_key, status,
	params_json, agent_bindings_json, input_bag_ids_json, input_bag_id_lists_json,
	output_bag_ids_json, output_bag_id_lists_json, handler_bindings_json, exception_frames_json,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		instance.ID, instance.RunID, instance.PipelineID, nullPipelineInstanceID(instance.ParentID),
		nullString(instance.ParentTransitionID), nullString(instance.InstanceKey), instance.Status,
		nullString(paramsJSON), nullString(agentBindingsJSON), nullString(inputBagIDsJSON), nullString(inputBagIDListsJSON),
		nullString(outputBagIDsJSON), nullString(outputBagIDListsJSON), nullString(handlerBindingsJSON), nullString(exceptionFramesJSON),
		formatTime(instance.CreatedAt), formatTime(instance.UpdatedAt),
	)
	return err
}

func (s *SQLStore) GetPipelineInstance(ctx context.Context, runID core.RunID, instanceID core.PipelineInstanceID) (PipelineInstanceRecord, error) {
	row := s.db.QueryRowContext(ctx, pipelineInstanceSelectSQL+" WHERE run_id = ? AND id = ?", runID, instanceID)
	return scanPipelineInstance(row)
}

func (s *SQLStore) UpdatePipelineInstance(ctx context.Context, instance PipelineInstanceRecord) error {
	paramsJSON, agentBindingsJSON, inputBagIDsJSON, inputBagIDListsJSON, outputBagIDsJSON, outputBagIDListsJSON, handlerBindingsJSON, exceptionFramesJSON, err := marshalPipelineInstanceJSONFields(instance)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE pipeline_instances
SET pipeline_id = ?, parent_id = ?, parent_transition_id = ?, instance_key = ?, status = ?,
    params_json = ?, agent_bindings_json = ?, input_bag_ids_json = ?, input_bag_id_lists_json = ?,
    output_bag_ids_json = ?, output_bag_id_lists_json = ?, handler_bindings_json = ?, exception_frames_json = ?, updated_at = ?
WHERE run_id = ? AND id = ?`,
		instance.PipelineID, nullPipelineInstanceID(instance.ParentID), nullString(instance.ParentTransitionID),
		nullString(instance.InstanceKey), instance.Status, nullString(paramsJSON), nullString(agentBindingsJSON),
		nullString(inputBagIDsJSON), nullString(inputBagIDListsJSON), nullString(outputBagIDsJSON),
		nullString(outputBagIDListsJSON), nullString(handlerBindingsJSON), nullString(exceptionFramesJSON), formatTime(instance.UpdatedAt),
		instance.RunID, instance.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "pipeline instance", string(instance.ID))
}

func (s *SQLStore) ListPipelineInstancesByRun(ctx context.Context, runID core.RunID) ([]PipelineInstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, pipelineInstanceSelectSQL+" WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPipelineInstances(rows)
}

func (s *SQLStore) ListPipelineInstanceChildren(ctx context.Context, runID core.RunID, parentID core.PipelineInstanceID) ([]PipelineInstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, pipelineInstanceSelectSQL+" WHERE run_id = ? AND parent_id = ? ORDER BY created_at, id", runID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPipelineInstances(rows)
}

func (s *SQLStore) CreateArtifact(ctx context.Context, artifact ArtifactRecord) error {
	if artifact.ID == "" {
		artifact.ID = string(artifact.RunID) + ":" + artifact.URI
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (id, run_id, task_id, agent_id, kind, uri, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, uri) DO UPDATE SET
	task_id = excluded.task_id,
	agent_id = excluded.agent_id,
	kind = excluded.kind,
	created_at = excluded.created_at`,
		artifact.ID, artifact.RunID, artifact.TaskID, artifact.AgentID,
		artifact.Kind, artifact.URI, formatTime(artifact.CreatedAt),
	)
	return err
}

func (s *SQLStore) ListArtifactsByRun(ctx context.Context, runID core.RunID) ([]ArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, artifactSelectSQL+" WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func (s *SQLStore) ListArtifactsByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]ArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, artifactSelectSQL+" WHERE run_id = ? AND task_id = ? ORDER BY created_at, id", runID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func (s *SQLStore) CreateEvent(ctx context.Context, event EventRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (id, run_id, task_id, agent_id, type, message, payload_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.RunID, nullString(string(event.TaskID)), nullString(string(event.AgentID)),
		event.Type, event.Message, nullString(event.PayloadJSON), formatTime(event.CreatedAt),
	)
	return err
}

func (s *SQLStore) CreateRunIteration(ctx context.Context, iteration RunIterationRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_iterations (
	run_id, iteration_no, start_frontier_id, delivery_frontier_id, acceptance_checkpoint_task_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		iteration.RunID, iteration.IterationNo, nullString(iteration.StartFrontierID), nullString(iteration.DeliveryFrontierID),
		nullString(string(iteration.AcceptanceCheckpointTaskID)), iteration.Status, formatTime(iteration.CreatedAt), formatTime(iteration.UpdatedAt),
	)
	return err
}

func (s *SQLStore) UpdateRunIteration(ctx context.Context, iteration RunIterationRecord) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE run_iterations
SET start_frontier_id = ?, delivery_frontier_id = ?, acceptance_checkpoint_task_id = ?, status = ?, updated_at = ?
WHERE run_id = ? AND iteration_no = ?`,
		nullString(iteration.StartFrontierID),
		nullString(iteration.DeliveryFrontierID),
		nullString(string(iteration.AcceptanceCheckpointTaskID)),
		iteration.Status,
		formatTime(iteration.UpdatedAt),
		iteration.RunID,
		iteration.IterationNo,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "run iteration", fmt.Sprintf("%s/%d", iteration.RunID, iteration.IterationNo))
}

func (s *SQLStore) ListRunIterationsByRun(ctx context.Context, runID core.RunID) ([]RunIterationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, iteration_no, start_frontier_id, delivery_frontier_id, acceptance_checkpoint_task_id, status, created_at, updated_at
FROM run_iterations WHERE run_id = ? ORDER BY iteration_no`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RunIterationRecord, 0)
	for rows.Next() {
		record, err := scanRunIteration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLStore) CreateSessionMessage(ctx context.Context, message SessionMessageRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_messages (id, run_id, iteration_no, role, message_type, content, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.RunID, message.IterationNo, message.Role, message.MessageType, message.Content, formatTime(message.CreatedAt),
	)
	return err
}

func (s *SQLStore) ListSessionMessagesByRun(ctx context.Context, runID core.RunID) ([]SessionMessageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, iteration_no, role, message_type, content, created_at
FROM session_messages WHERE run_id = ? ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SessionMessageRecord, 0)
	for rows.Next() {
		record, err := scanSessionMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLStore) CreateSessionArtifact(ctx context.Context, artifact SessionArtifactRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_artifacts (id, run_id, iteration_no, kind, title, content, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.RunID, artifact.IterationNo, artifact.Kind, artifact.Title, artifact.Content, formatTime(artifact.CreatedAt),
	)
	return err
}

func (s *SQLStore) ListSessionArtifactsByRun(ctx context.Context, runID core.RunID) ([]SessionArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, iteration_no, kind, title, content, created_at
FROM session_artifacts WHERE run_id = ? ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SessionArtifactRecord, 0)
	for rows.Next() {
		record, err := scanSessionArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLStore) CreateImprovementItem(ctx context.Context, item ImprovementItemRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO improvement_items (item_id, run_id, iteration_no, title, detail, source, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ItemID, item.RunID, item.IterationNo, item.Title, item.Detail, item.Source, item.Status, formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	return err
}

func (s *SQLStore) UpdateImprovementItem(ctx context.Context, item ImprovementItemRecord) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE improvement_items
SET iteration_no = ?, title = ?, detail = ?, source = ?, status = ?, updated_at = ?
WHERE run_id = ? AND item_id = ?`,
		item.IterationNo, item.Title, item.Detail, item.Source, item.Status, formatTime(item.UpdatedAt), item.RunID, item.ItemID,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "improvement item", item.ItemID)
}

func (s *SQLStore) ListImprovementItemsByRun(ctx context.Context, runID core.RunID) ([]ImprovementItemRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT item_id, run_id, iteration_no, title, detail, source, status, created_at, updated_at
FROM improvement_items WHERE run_id = ? ORDER BY created_at, item_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ImprovementItemRecord, 0)
	for rows.Next() {
		record, err := scanImprovementItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListEventsByRun(ctx context.Context, runID core.RunID) ([]EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, eventSelectSQL+" WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *SQLStore) ListEventsByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, eventSelectSQL+" WHERE run_id = ? AND task_id = ? ORDER BY created_at, id", runID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

type SQLRunRepository struct{ store *SQLStore }

func NewSQLRunRepository(store *SQLStore) *SQLRunRepository { return &SQLRunRepository{store: store} }
func (r *SQLRunRepository) Create(ctx context.Context, run RunRecord) error {
	return r.store.Create(ctx, run)
}
func (r *SQLRunRepository) Get(ctx context.Context, runID core.RunID) (RunRecord, error) {
	return r.store.Get(ctx, runID)
}
func (r *SQLRunRepository) List(ctx context.Context) ([]RunRecord, error) {
	return r.store.List(ctx)
}
func (r *SQLRunRepository) Update(ctx context.Context, run RunRecord) error {
	return r.store.Update(ctx, run)
}

type SQLTaskRepository struct{ store *SQLStore }

func NewSQLTaskRepository(store *SQLStore) *SQLTaskRepository {
	return &SQLTaskRepository{store: store}
}
func (r *SQLTaskRepository) Create(ctx context.Context, task TaskRecord) error {
	return r.store.CreateTask(ctx, task)
}
func (r *SQLTaskRepository) Get(ctx context.Context, runID core.RunID, taskID core.TaskID) (TaskRecord, error) {
	return r.store.GetTask(ctx, runID, taskID)
}
func (r *SQLTaskRepository) Update(ctx context.Context, task TaskRecord) error {
	return r.store.UpdateTask(ctx, task)
}
func (r *SQLTaskRepository) ListByRun(ctx context.Context, runID core.RunID) ([]TaskRecord, error) {
	return r.store.ListTasksByRun(ctx, runID)
}

type SQLPipelineInstanceRepository struct{ store *SQLStore }

func NewSQLPipelineInstanceRepository(store *SQLStore) *SQLPipelineInstanceRepository {
	return &SQLPipelineInstanceRepository{store: store}
}
func (r *SQLPipelineInstanceRepository) Create(ctx context.Context, instance PipelineInstanceRecord) error {
	return r.store.CreatePipelineInstance(ctx, instance)
}
func (r *SQLPipelineInstanceRepository) Get(ctx context.Context, runID core.RunID, instanceID core.PipelineInstanceID) (PipelineInstanceRecord, error) {
	return r.store.GetPipelineInstance(ctx, runID, instanceID)
}
func (r *SQLPipelineInstanceRepository) Update(ctx context.Context, instance PipelineInstanceRecord) error {
	return r.store.UpdatePipelineInstance(ctx, instance)
}
func (r *SQLPipelineInstanceRepository) ListByRun(ctx context.Context, runID core.RunID) ([]PipelineInstanceRecord, error) {
	return r.store.ListPipelineInstancesByRun(ctx, runID)
}
func (r *SQLPipelineInstanceRepository) ListChildren(ctx context.Context, runID core.RunID, parentID core.PipelineInstanceID) ([]PipelineInstanceRecord, error) {
	return r.store.ListPipelineInstanceChildren(ctx, runID, parentID)
}

type SQLArtifactRepository struct{ store *SQLStore }

func NewSQLArtifactRepository(store *SQLStore) *SQLArtifactRepository {
	return &SQLArtifactRepository{store: store}
}
func (r *SQLArtifactRepository) Create(ctx context.Context, artifact ArtifactRecord) error {
	return r.store.CreateArtifact(ctx, artifact)
}
func (r *SQLArtifactRepository) ListByRun(ctx context.Context, runID core.RunID) ([]ArtifactRecord, error) {
	return r.store.ListArtifactsByRun(ctx, runID)
}
func (r *SQLArtifactRepository) ListByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]ArtifactRecord, error) {
	return r.store.ListArtifactsByTask(ctx, runID, taskID)
}

type SQLEventRepository struct{ store *SQLStore }

func NewSQLEventRepository(store *SQLStore) *SQLEventRepository {
	return &SQLEventRepository{store: store}
}
func (r *SQLEventRepository) Create(ctx context.Context, event EventRecord) error {
	return r.store.CreateEvent(ctx, event)
}
func (r *SQLEventRepository) ListByRun(ctx context.Context, runID core.RunID) ([]EventRecord, error) {
	return r.store.ListEventsByRun(ctx, runID)
}
func (r *SQLEventRepository) ListByTask(ctx context.Context, runID core.RunID, taskID core.TaskID) ([]EventRecord, error) {
	return r.store.ListEventsByTask(ctx, runID, taskID)
}

type SQLRunIterationRepository struct{ store *SQLStore }

func NewSQLRunIterationRepository(store *SQLStore) *SQLRunIterationRepository {
	return &SQLRunIterationRepository{store: store}
}

func (r *SQLRunIterationRepository) Create(ctx context.Context, iteration RunIterationRecord) error {
	return r.store.CreateRunIteration(ctx, iteration)
}

func (r *SQLRunIterationRepository) Update(ctx context.Context, iteration RunIterationRecord) error {
	return r.store.UpdateRunIteration(ctx, iteration)
}

func (r *SQLRunIterationRepository) ListByRun(ctx context.Context, runID core.RunID) ([]RunIterationRecord, error) {
	return r.store.ListRunIterationsByRun(ctx, runID)
}

type SQLSessionMessageRepository struct{ store *SQLStore }

func NewSQLSessionMessageRepository(store *SQLStore) *SQLSessionMessageRepository {
	return &SQLSessionMessageRepository{store: store}
}

func (r *SQLSessionMessageRepository) Create(ctx context.Context, message SessionMessageRecord) error {
	return r.store.CreateSessionMessage(ctx, message)
}

func (r *SQLSessionMessageRepository) ListByRun(ctx context.Context, runID core.RunID) ([]SessionMessageRecord, error) {
	return r.store.ListSessionMessagesByRun(ctx, runID)
}

type SQLSessionArtifactRepository struct{ store *SQLStore }

func NewSQLSessionArtifactRepository(store *SQLStore) *SQLSessionArtifactRepository {
	return &SQLSessionArtifactRepository{store: store}
}

func (r *SQLSessionArtifactRepository) Create(ctx context.Context, artifact SessionArtifactRecord) error {
	return r.store.CreateSessionArtifact(ctx, artifact)
}

func (r *SQLSessionArtifactRepository) ListByRun(ctx context.Context, runID core.RunID) ([]SessionArtifactRecord, error) {
	return r.store.ListSessionArtifactsByRun(ctx, runID)
}

type SQLImprovementItemRepository struct{ store *SQLStore }

func NewSQLImprovementItemRepository(store *SQLStore) *SQLImprovementItemRepository {
	return &SQLImprovementItemRepository{store: store}
}

func (r *SQLImprovementItemRepository) Create(ctx context.Context, item ImprovementItemRecord) error {
	return r.store.CreateImprovementItem(ctx, item)
}

func (r *SQLImprovementItemRepository) Update(ctx context.Context, item ImprovementItemRecord) error {
	return r.store.UpdateImprovementItem(ctx, item)
}

func (r *SQLImprovementItemRepository) ListByRun(ctx context.Context, runID core.RunID) ([]ImprovementItemRecord, error) {
	return r.store.ListImprovementItemsByRun(ctx, runID)
}

func (s *SQLStore) insertTask(ctx context.Context, task TaskRecord) error {
	dependsOnIDsJSON, inputRefsJSON, outputRefsJSON, inputBagIDsJSON, inputBagsJSON, outputBagIDsJSON, err := marshalTaskJSONFields(task)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO tasks (
	id, run_id, stage_id, op, agent_role, agent_id, status, result, error_message,
	pipeline_instance_id, parent_id, depends_on, depends_on_ids_json, input_artifact_uris_json,
	output_artifact_uris_json, input_bag_ids_json, input_bags_json, output_bag_ids_json, exception_frame_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.RunID, task.StageID, task.Op, task.AgentRole, task.AgentID, task.Status,
		nullString(string(task.Result)), nullString(task.ErrorMessage), nullString(string(task.PipelineInstanceID)),
		nullTaskID(task.ParentID), legacyDependsOn(task.DependsOnIDs), nullString(dependsOnIDsJSON),
		nullString(inputRefsJSON), nullString(outputRefsJSON), nullString(inputBagIDsJSON),
		nullString(inputBagsJSON), nullString(outputBagIDsJSON), nullString(string(task.ExceptionFrameID)),
		formatTime(task.CreatedAt), formatTime(task.UpdatedAt),
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (RunRecord, error) {
	var run RunRecord
	var projectID, sessionID, latestDeliveryFrontierID, latestAcceptanceCheckpointTaskID, configJSON sql.NullString
	var currentIterationNo int
	var createdAt, updatedAt string
	if err := row.Scan(&run.ID, &run.PipelineID, &run.Status, &projectID, &run.ProjectDir, &sessionID, &currentIterationNo, &latestDeliveryFrontierID, &latestAcceptanceCheckpointTaskID, &configJSON, &createdAt, &updatedAt); err != nil {
		return RunRecord{}, err
	}
	if projectID.Valid {
		run.ProjectID = projectID.String
	}
	if sessionID.Valid {
		run.SessionID = core.SessionID(sessionID.String)
	}
	run.CurrentIterationNo = currentIterationNo
	if latestDeliveryFrontierID.Valid {
		run.LatestDeliveryFrontierID = latestDeliveryFrontierID.String
	}
	if latestAcceptanceCheckpointTaskID.Valid {
		run.LatestAcceptanceCheckpointTaskID = core.TaskID(latestAcceptanceCheckpointTaskID.String)
	}
	if configJSON.Valid && strings.TrimSpace(configJSON.String) != "" {
		if err := json.Unmarshal([]byte(configJSON.String), &run.Config); err != nil {
			return RunRecord{}, fmt.Errorf("unmarshal run config: %w", err)
		}
	}
	var err error
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	return run, err
}

type SQLProjectRepository struct{ store *SQLStore }

func NewSQLProjectRepository(store *SQLStore) *SQLProjectRepository {
	return &SQLProjectRepository{store: store}
}

func (r *SQLProjectRepository) Create(ctx context.Context, project ProjectRecord) error {
	llmJSON, err := marshalJSON(project.LLM)
	if err != nil {
		return fmt.Errorf("marshal project llm config: %w", err)
	}
	_, err = r.store.db.ExecContext(ctx, `
INSERT INTO projects (project_id, name, llm_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)`,
		project.ProjectID, project.Name, nullString(llmJSON), formatTime(project.CreatedAt), formatTime(project.UpdatedAt),
	)
	return err
}

func (r *SQLProjectRepository) Get(ctx context.Context, projectID string) (ProjectRecord, error) {
	row := r.store.db.QueryRowContext(ctx, `
SELECT project_id, name, llm_json, created_at, updated_at
FROM projects WHERE project_id = ?`, projectID)
	return scanProject(row)
}

func (r *SQLProjectRepository) List(ctx context.Context) ([]ProjectRecord, error) {
	rows, err := r.store.db.QueryContext(ctx, `
SELECT project_id, name, llm_json, created_at, updated_at
FROM projects ORDER BY created_at DESC, project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProjectRecord, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	return out, rows.Err()
}

func (r *SQLProjectRepository) Update(ctx context.Context, project ProjectRecord) error {
	llmJSON, err := marshalJSON(project.LLM)
	if err != nil {
		return fmt.Errorf("marshal project llm config: %w", err)
	}
	result, err := r.store.db.ExecContext(ctx, `
UPDATE projects
SET name = ?, llm_json = ?, updated_at = ?
WHERE project_id = ?`,
		project.Name, nullString(llmJSON), formatTime(project.UpdatedAt), project.ProjectID,
	)
	if err != nil {
		return err
	}
	return requireAffected(result, "project", project.ProjectID)
}

func scanProject(row rowScanner) (ProjectRecord, error) {
	var project ProjectRecord
	var llmJSON sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&project.ProjectID, &project.Name, &llmJSON, &createdAt, &updatedAt); err != nil {
		return ProjectRecord{}, err
	}
	if llmJSON.Valid && strings.TrimSpace(llmJSON.String) != "" {
		if err := json.Unmarshal([]byte(llmJSON.String), &project.LLM); err != nil {
			return ProjectRecord{}, fmt.Errorf("unmarshal project llm config: %w", err)
		}
	}
	var err error
	project.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ProjectRecord{}, err
	}
	project.UpdatedAt, err = parseTime(updatedAt)
	return project, err
}

func scanRunIteration(row rowScanner) (RunIterationRecord, error) {
	var record RunIterationRecord
	var startFrontierID, deliveryFrontierID, acceptanceCheckpointTaskID sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&record.RunID, &record.IterationNo, &startFrontierID, &deliveryFrontierID, &acceptanceCheckpointTaskID, &record.Status, &createdAt, &updatedAt); err != nil {
		return RunIterationRecord{}, err
	}
	if startFrontierID.Valid {
		record.StartFrontierID = startFrontierID.String
	}
	if deliveryFrontierID.Valid {
		record.DeliveryFrontierID = deliveryFrontierID.String
	}
	if acceptanceCheckpointTaskID.Valid {
		record.AcceptanceCheckpointTaskID = core.TaskID(acceptanceCheckpointTaskID.String)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return RunIterationRecord{}, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	return record, err
}

func scanSessionMessage(row rowScanner) (SessionMessageRecord, error) {
	var record SessionMessageRecord
	var createdAt string
	if err := row.Scan(&record.ID, &record.RunID, &record.IterationNo, &record.Role, &record.MessageType, &record.Content, &createdAt); err != nil {
		return SessionMessageRecord{}, err
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return SessionMessageRecord{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}

func scanSessionArtifact(row rowScanner) (SessionArtifactRecord, error) {
	var record SessionArtifactRecord
	var createdAt string
	if err := row.Scan(&record.ID, &record.RunID, &record.IterationNo, &record.Kind, &record.Title, &record.Content, &createdAt); err != nil {
		return SessionArtifactRecord{}, err
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return SessionArtifactRecord{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}

func scanImprovementItem(row rowScanner) (ImprovementItemRecord, error) {
	var record ImprovementItemRecord
	var createdAt, updatedAt string
	if err := row.Scan(&record.ItemID, &record.RunID, &record.IterationNo, &record.Title, &record.Detail, &record.Source, &record.Status, &createdAt, &updatedAt); err != nil {
		return ImprovementItemRecord{}, err
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return ImprovementItemRecord{}, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	return record, err
}

func scanTask(row rowScanner) (TaskRecord, error) {
	var task TaskRecord
	var result, errorMessage, pipelineInstanceID, parentID, dependsOn, dependsOnIDsJSON, inputRefsJSON, outputRefsJSON sql.NullString
	var inputBagIDsJSON, inputBagsJSON, outputBagIDsJSON sql.NullString
	var exceptionFrameID sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&task.ID, &task.RunID, &task.StageID, &task.Op, &task.AgentRole, &task.AgentID, &task.Status,
		&result, &errorMessage, &pipelineInstanceID, &parentID, &dependsOn, &dependsOnIDsJSON,
		&inputRefsJSON, &outputRefsJSON, &inputBagIDsJSON, &inputBagsJSON, &outputBagIDsJSON, &exceptionFrameID,
		&createdAt, &updatedAt,
	); err != nil {
		return TaskRecord{}, err
	}
	if result.Valid {
		task.Result = core.TaskResultCode(result.String)
	}
	if errorMessage.Valid {
		task.ErrorMessage = errorMessage.String
	}
	if pipelineInstanceID.Valid {
		task.PipelineInstanceID = core.PipelineInstanceID(pipelineInstanceID.String)
	}
	if parentID.Valid {
		id := core.TaskID(parentID.String)
		task.ParentID = &id
	}
	if err := unmarshalJSONField(dependsOnIDsJSON, &task.DependsOnIDs); err != nil {
		return TaskRecord{}, err
	}
	if len(task.DependsOnIDs) == 0 && dependsOn.Valid && strings.TrimSpace(dependsOn.String) != "" {
		task.DependsOnIDs = []core.TaskID{core.TaskID(dependsOn.String)}
	}
	if err := unmarshalJSONField(inputRefsJSON, &task.InputArtifactRefs); err != nil {
		return TaskRecord{}, err
	}
	if err := unmarshalJSONField(outputRefsJSON, &task.OutputArtifactRefs); err != nil {
		return TaskRecord{}, err
	}
	if err := unmarshalJSONField(inputBagIDsJSON, &task.InputBagIDs); err != nil {
		return TaskRecord{}, err
	}
	if err := unmarshalJSONField(inputBagsJSON, &task.InputBags); err != nil {
		return TaskRecord{}, err
	}
	if err := unmarshalJSONField(outputBagIDsJSON, &task.OutputBagIDs); err != nil {
		return TaskRecord{}, err
	}
	if exceptionFrameID.Valid {
		task.ExceptionFrameID = core.ExceptionFrameID(exceptionFrameID.String)
	}
	var err error
	task.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return TaskRecord{}, err
	}
	task.UpdatedAt, err = parseTime(updatedAt)
	return task, err
}

func scanPipelineInstance(row rowScanner) (PipelineInstanceRecord, error) {
	var instance PipelineInstanceRecord
	var parentID, parentTransitionID, instanceKey sql.NullString
	var paramsJSON, agentBindingsJSON, inputBagIDsJSON, inputBagIDListsJSON, outputBagIDsJSON, outputBagIDListsJSON sql.NullString
	var handlerBindingsJSON, exceptionFramesJSON sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&instance.ID, &instance.RunID, &instance.PipelineID, &parentID, &parentTransitionID, &instanceKey, &instance.Status,
		&paramsJSON, &agentBindingsJSON, &inputBagIDsJSON, &inputBagIDListsJSON, &outputBagIDsJSON, &outputBagIDListsJSON,
		&handlerBindingsJSON, &exceptionFramesJSON, &createdAt, &updatedAt,
	); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if parentID.Valid {
		id := core.PipelineInstanceID(parentID.String)
		instance.ParentID = &id
	}
	if parentTransitionID.Valid {
		instance.ParentTransitionID = parentTransitionID.String
	}
	if instanceKey.Valid {
		instance.InstanceKey = instanceKey.String
	}
	if err := unmarshalJSONField(paramsJSON, &instance.Params); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(agentBindingsJSON, &instance.AgentBindings); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(inputBagIDsJSON, &instance.InputBagIDs); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(inputBagIDListsJSON, &instance.InputBagIDLists); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(outputBagIDsJSON, &instance.OutputBagIDs); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(outputBagIDListsJSON, &instance.OutputBagIDLists); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(handlerBindingsJSON, &instance.HandlerBindings); err != nil {
		return PipelineInstanceRecord{}, err
	}
	if err := unmarshalJSONField(exceptionFramesJSON, &instance.ExceptionFrames); err != nil {
		return PipelineInstanceRecord{}, err
	}
	var err error
	instance.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return PipelineInstanceRecord{}, err
	}
	instance.UpdatedAt, err = parseTime(updatedAt)
	return instance, err
}

func scanPipelineInstances(rows *sql.Rows) ([]PipelineInstanceRecord, error) {
	out := make([]PipelineInstanceRecord, 0)
	for rows.Next() {
		instance, err := scanPipelineInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, instance)
	}
	return out, rows.Err()
}

func scanArtifacts(rows *sql.Rows) ([]ArtifactRecord, error) {
	out := make([]ArtifactRecord, 0)
	for rows.Next() {
		var artifact ArtifactRecord
		var createdAt string
		if err := rows.Scan(&artifact.ID, &artifact.RunID, &artifact.TaskID, &artifact.AgentID, &artifact.Kind, &artifact.URI, &createdAt); err != nil {
			return nil, err
		}
		parsed, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		artifact.CreatedAt = parsed
		out = append(out, artifact)
	}
	return out, rows.Err()
}

func scanEvents(rows *sql.Rows) ([]EventRecord, error) {
	out := make([]EventRecord, 0)
	for rows.Next() {
		var event EventRecord
		var taskID, agentID, payloadJSON sql.NullString
		var createdAt string
		if err := rows.Scan(&event.ID, &event.RunID, &taskID, &agentID, &event.Type, &event.Message, &payloadJSON, &createdAt); err != nil {
			return nil, err
		}
		if taskID.Valid {
			event.TaskID = core.TaskID(taskID.String)
		}
		if agentID.Valid {
			event.AgentID = core.AgentID(agentID.String)
		}
		if payloadJSON.Valid {
			event.PayloadJSON = payloadJSON.String
		}
		parsed, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		event.CreatedAt = parsed
		out = append(out, event)
	}
	return out, rows.Err()
}

func marshalTaskJSONFields(task TaskRecord) (string, string, string, string, string, string, error) {
	dependsOnIDsJSON, err := marshalJSON(task.DependsOnIDs)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal depends_on_ids: %w", err)
	}
	inputRefsJSON, err := marshalJSON(task.InputArtifactRefs)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal input artifact refs: %w", err)
	}
	outputRefsJSON, err := marshalJSON(task.OutputArtifactRefs)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal output artifact refs: %w", err)
	}
	inputBagIDsJSON, err := marshalJSON(task.InputBagIDs)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal input bag ids: %w", err)
	}
	inputBagsJSON, err := marshalJSON(task.InputBags)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal input bags: %w", err)
	}
	outputBagIDsJSON, err := marshalJSON(task.OutputBagIDs)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal output bag ids: %w", err)
	}
	return dependsOnIDsJSON, inputRefsJSON, outputRefsJSON, inputBagIDsJSON, inputBagsJSON, outputBagIDsJSON, nil
}

func marshalPipelineInstanceJSONFields(instance PipelineInstanceRecord) (string, string, string, string, string, string, string, string, error) {
	paramsJSON, err := marshalJSON(instance.Params)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance params: %w", err)
	}
	agentBindingsJSON, err := marshalJSON(instance.AgentBindings)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance agent bindings: %w", err)
	}
	inputBagIDsJSON, err := marshalJSON(instance.InputBagIDs)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance input bags: %w", err)
	}
	inputBagIDListsJSON, err := marshalJSON(instance.InputBagIDLists)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance input bag lists: %w", err)
	}
	outputBagIDsJSON, err := marshalJSON(instance.OutputBagIDs)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance output bags: %w", err)
	}
	outputBagIDListsJSON, err := marshalJSON(instance.OutputBagIDLists)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance output bag lists: %w", err)
	}
	handlerBindingsJSON, err := marshalJSON(instance.HandlerBindings)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance handler bindings: %w", err)
	}
	exceptionFramesJSON, err := marshalJSON(instance.ExceptionFrames)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("marshal pipeline instance exception frames: %w", err)
	}
	return paramsJSON, agentBindingsJSON, inputBagIDsJSON, inputBagIDListsJSON, outputBagIDsJSON, outputBagIDListsJSON, handlerBindingsJSON, exceptionFramesJSON, nil
}

func marshalJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalJSONField(raw sql.NullString, dest any) error {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw.String), dest)
}

func nullString(value string) sql.NullString {
	if strings.TrimSpace(value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullTaskID(value *core.TaskID) sql.NullString {
	if value == nil || strings.TrimSpace(string(*value)) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*value), Valid: true}
}

func legacyDependsOn(values []core.TaskID) sql.NullString {
	if len(values) == 0 {
		return sql.NullString{}
	}
	value := strings.TrimSpace(string(values[len(values)-1]))
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullPipelineInstanceID(value *core.PipelineInstanceID) sql.NullString {
	if value == nil || strings.TrimSpace(string(*value)) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*value), Valid: true}
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

func requireAffected(result sql.Result, kind string, id string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return nil
	}
	if affected == 0 {
		return fmt.Errorf("%s %q not found", kind, id)
	}
	return nil
}

const taskSelectSQL = `
SELECT id, run_id, stage_id, op, agent_role, agent_id, status, result, error_message,
       pipeline_instance_id, parent_id, depends_on, depends_on_ids_json, input_artifact_uris_json,
       output_artifact_uris_json, input_bag_ids_json, input_bags_json, output_bag_ids_json, exception_frame_id,
       created_at, updated_at
FROM tasks`

const pipelineInstanceSelectSQL = `
SELECT id, run_id, pipeline_id, parent_id, parent_transition_id, instance_key, status,
       params_json, agent_bindings_json, input_bag_ids_json, input_bag_id_lists_json,
       output_bag_ids_json, output_bag_id_lists_json, handler_bindings_json, exception_frames_json,
       created_at, updated_at
FROM pipeline_instances`

const artifactSelectSQL = `SELECT id, run_id, task_id, agent_id, kind, uri, created_at FROM artifacts`
const eventSelectSQL = `SELECT id, run_id, task_id, agent_id, type, message, payload_json, created_at FROM events`

var sqlSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		pipeline_id TEXT NOT NULL,
		status TEXT NOT NULL,
		project_id TEXT,
		project_dir TEXT NOT NULL,
		session_id TEXT,
		current_iteration_no INTEGER NOT NULL DEFAULT 0,
		latest_delivery_frontier_id TEXT,
		latest_acceptance_checkpoint_task_id TEXT,
		config_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS projects (
		project_id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		llm_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS run_iterations (
		run_id TEXT NOT NULL,
		iteration_no INTEGER NOT NULL,
		start_frontier_id TEXT,
		delivery_frontier_id TEXT,
		acceptance_checkpoint_task_id TEXT,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, iteration_no),
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS session_messages (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		iteration_no INTEGER NOT NULL,
		role TEXT NOT NULL,
		message_type TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS session_artifacts (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		iteration_no INTEGER NOT NULL,
		kind TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS improvement_items (
		item_id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		iteration_no INTEGER NOT NULL,
		title TEXT NOT NULL,
		detail TEXT NOT NULL,
		source TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, item_id),
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		pipeline_instance_id TEXT,
		stage_id TEXT NOT NULL,
		op TEXT NOT NULL,
		agent_role TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		status TEXT NOT NULL,
		parent_id TEXT,
		depends_on TEXT,
		depends_on_ids_json TEXT,
		input_artifact_uris_json TEXT,
		output_artifact_uris_json TEXT,
		input_bag_ids_json TEXT,
		input_bags_json TEXT,
		output_bag_ids_json TEXT,
		exception_frame_id TEXT,
		result TEXT,
		error_message TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, id),
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS pipeline_instances (
		id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		pipeline_id TEXT NOT NULL,
		parent_id TEXT,
		parent_transition_id TEXT,
		instance_key TEXT,
		status TEXT NOT NULL,
		params_json TEXT,
		agent_bindings_json TEXT,
		input_bag_ids_json TEXT,
		input_bag_id_lists_json TEXT,
		output_bag_ids_json TEXT,
		output_bag_id_lists_json TEXT,
		handler_bindings_json TEXT,
		exception_frames_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, id),
		FOREIGN KEY (run_id) REFERENCES runs(id),
		FOREIGN KEY (run_id, parent_id) REFERENCES pipeline_instances(run_id, id)
	)`,
	`CREATE TABLE IF NOT EXISTS artifacts (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		task_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		uri TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE (run_id, uri),
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		run_id TEXT NOT NULL,
		task_id TEXT,
		agent_id TEXT,
		type TEXT NOT NULL,
		message TEXT NOT NULL,
		payload_json TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY (run_id) REFERENCES runs(id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_run_status ON tasks (run_id, status)`,
	`CREATE INDEX IF NOT EXISTS idx_run_iterations_run_iteration ON run_iterations (run_id, iteration_no)`,
	`CREATE INDEX IF NOT EXISTS idx_session_messages_run_created ON session_messages (run_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_session_artifacts_run_created ON session_artifacts (run_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_improvement_items_run_created ON improvement_items (run_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_run_agent ON tasks (run_id, agent_id)`,
	`CREATE INDEX IF NOT EXISTS idx_pipeline_instances_run_status ON pipeline_instances (run_id, status)`,
	`CREATE INDEX IF NOT EXISTS idx_pipeline_instances_parent ON pipeline_instances (run_id, parent_id)`,
	`CREATE INDEX IF NOT EXISTS idx_artifacts_run_task ON artifacts (run_id, task_id)`,
	`CREATE INDEX IF NOT EXISTS idx_artifacts_run_kind ON artifacts (run_id, kind)`,
	`CREATE INDEX IF NOT EXISTS idx_events_run_created ON events (run_id, created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_events_task_created ON events (run_id, task_id, created_at)`,
}
