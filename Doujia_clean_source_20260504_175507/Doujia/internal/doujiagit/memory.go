package doujiagit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"devflow/internal/core"
)

type MemoryRepository struct {
	mu sync.RWMutex

	objects map[string]ArtifactObject

	logicalByID  map[string]LogicalArtifact
	logicalByKey map[logicalKey]string

	versions            map[string]ArtifactVersion
	versionByMembership map[versionKey]string

	bags map[string]ArtifactBag

	snapshots   map[string]TaskSnapshot
	bagProducer map[string]string

	frontiers map[string]FrontierSnapshot
	refs      map[refKey]Ref
	refMoves  map[string]RefMoveEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		objects:             make(map[string]ArtifactObject),
		logicalByID:         make(map[string]LogicalArtifact),
		logicalByKey:        make(map[logicalKey]string),
		versions:            make(map[string]ArtifactVersion),
		versionByMembership: make(map[versionKey]string),
		bags:                make(map[string]ArtifactBag),
		snapshots:           make(map[string]TaskSnapshot),
		bagProducer:         make(map[string]string),
		frontiers:           make(map[string]FrontierSnapshot),
		refs:                make(map[refKey]Ref),
		refMoves:            make(map[string]RefMoveEvent),
	}
}

var _ Repository = (*MemoryRepository)(nil)

func (r *MemoryRepository) UpsertObject(_ context.Context, obj ArtifactObject) (ArtifactObject, error) {
	obj, err := normalizeObject(obj)
	if err != nil {
		return ArtifactObject{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.objects[obj.ObjectID]; ok {
		if obj.CreatedAt.IsZero() {
			obj.CreatedAt = existing.CreatedAt
		}
		r.objects[obj.ObjectID] = obj
		return r.objects[obj.ObjectID], nil
	}
	r.objects[obj.ObjectID] = obj
	return obj, nil
}

func (r *MemoryRepository) GetObject(_ context.Context, objectID string) (ArtifactObject, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	obj, ok := r.objects[strings.TrimSpace(objectID)]
	if !ok {
		return ArtifactObject{}, fmt.Errorf("artifact object %q not found", objectID)
	}
	return obj, nil
}

func (r *MemoryRepository) UpsertLogicalArtifact(_ context.Context, item LogicalArtifact) (LogicalArtifact, error) {
	item, err := normalizeLogicalArtifact(item)
	if err != nil {
		return LogicalArtifact{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	key := newLogicalKey(item.RunID, item.Namespace, item.LogicalKey)
	if existingID, ok := r.logicalByKey[key]; ok {
		return r.logicalByID[existingID], nil
	}
	if existing, ok := r.logicalByID[item.LogicalArtifactID]; ok {
		if newLogicalKey(existing.RunID, existing.Namespace, existing.LogicalKey) != key {
			return LogicalArtifact{}, fmt.Errorf("logical artifact id %q already belongs to a different key", item.LogicalArtifactID)
		}
		return existing, nil
	}
	r.logicalByID[item.LogicalArtifactID] = item
	r.logicalByKey[key] = item.LogicalArtifactID
	return item, nil
}

func (r *MemoryRepository) GetLogicalArtifact(_ context.Context, logicalArtifactID string) (LogicalArtifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.logicalByID[strings.TrimSpace(logicalArtifactID)]
	if !ok {
		return LogicalArtifact{}, fmt.Errorf("logical artifact %q not found", logicalArtifactID)
	}
	return item, nil
}

func (r *MemoryRepository) UpsertArtifactVersion(_ context.Context, version ArtifactVersion) (ArtifactVersion, error) {
	version, err := normalizeArtifactVersion(version)
	if err != nil {
		return ArtifactVersion{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.logicalByID[version.LogicalArtifactID]; !ok {
		return ArtifactVersion{}, fmt.Errorf("logical artifact %q not found", version.LogicalArtifactID)
	}
	for _, objectID := range version.ObjectIDs {
		if _, ok := r.objects[objectID]; !ok {
			return ArtifactVersion{}, fmt.Errorf("artifact object %q not found", objectID)
		}
	}

	key := newVersionKey(version.LogicalArtifactID, version.ObjectIDs)
	if existingID, ok := r.versionByMembership[key]; ok {
		return r.versions[existingID], nil
	}
	if existing, ok := r.versions[version.ArtifactVersionID]; ok {
		if newVersionKey(existing.LogicalArtifactID, existing.ObjectIDs) != key {
			return ArtifactVersion{}, fmt.Errorf("artifact version id %q already belongs to a different membership", version.ArtifactVersionID)
		}
		return existing, nil
	}
	r.versions[version.ArtifactVersionID] = version
	r.versionByMembership[key] = version.ArtifactVersionID
	return version, nil
}

func (r *MemoryRepository) GetArtifactVersion(_ context.Context, versionID string) (ArtifactVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version, ok := r.versions[strings.TrimSpace(versionID)]
	if !ok {
		return ArtifactVersion{}, fmt.Errorf("artifact version %q not found", versionID)
	}
	version.ObjectIDs = append([]string(nil), version.ObjectIDs...)
	return version, nil
}

func (r *MemoryRepository) CreateBag(_ context.Context, bag ArtifactBag) error {
	bag, err := normalizeBag(bag)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bags[bag.BagID]; ok {
		return fmt.Errorf("artifact bag %q already exists", bag.BagID)
	}
	for _, versionID := range bag.ArtifactVersionIDs {
		if _, ok := r.versions[versionID]; !ok {
			return fmt.Errorf("artifact version %q not found", versionID)
		}
	}
	r.bags[bag.BagID] = bag
	return nil
}

func (r *MemoryRepository) GetBag(_ context.Context, bagID string) (ArtifactBag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bag, ok := r.bags[strings.TrimSpace(bagID)]
	if !ok {
		return ArtifactBag{}, fmt.Errorf("artifact bag %q not found", bagID)
	}
	return cloneBag(bag), nil
}

func (r *MemoryRepository) ListBagsByRun(_ context.Context, runID core.RunID) ([]ArtifactBag, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]ArtifactBag, 0)
	for _, bag := range r.bags {
		if bag.RunID != runID {
			continue
		}
		items = append(items, cloneBag(bag))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].BagID < items[j].BagID
	})
	return items, nil
}

func (r *MemoryRepository) CreateSnapshot(_ context.Context, snapshot TaskSnapshot) error {
	snapshot, err := normalizeSnapshot(snapshot)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.snapshots[snapshot.SnapshotID]; ok {
		return fmt.Errorf("task snapshot %q already exists", snapshot.SnapshotID)
	}
	for _, bagID := range snapshot.InputBagIDs {
		if _, ok := r.bags[bagID]; !ok {
			return fmt.Errorf("input artifact bag %q not found", bagID)
		}
	}
	for _, bagID := range snapshot.OutputBagIDs {
		if _, ok := r.bags[bagID]; !ok {
			return fmt.Errorf("output artifact bag %q not found", bagID)
		}
		if producer, ok := r.bagProducer[bagID]; ok {
			return fmt.Errorf("artifact bag %q already produced by snapshot %q", bagID, producer)
		}
	}
	r.snapshots[snapshot.SnapshotID] = snapshot
	for _, bagID := range snapshot.OutputBagIDs {
		r.bagProducer[bagID] = snapshot.SnapshotID
	}
	return nil
}

func (r *MemoryRepository) GetSnapshot(_ context.Context, snapshotID string) (TaskSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.snapshots[strings.TrimSpace(snapshotID)]
	if !ok {
		return TaskSnapshot{}, fmt.Errorf("task snapshot %q not found", snapshotID)
	}
	return cloneSnapshot(snapshot), nil
}

func (r *MemoryRepository) ListSnapshotsByRun(_ context.Context, runID core.RunID) ([]TaskSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]TaskSnapshot, 0)
	for _, snapshot := range r.snapshots {
		if snapshot.RunID != runID {
			continue
		}
		items = append(items, cloneSnapshot(snapshot))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].SnapshotID < items[j].SnapshotID
	})
	return items, nil
}

func (r *MemoryRepository) ProducerOfBag(_ context.Context, bagID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshotID, ok := r.bagProducer[strings.TrimSpace(bagID)]
	if !ok {
		return "", fmt.Errorf("producer snapshot for artifact bag %q not found", bagID)
	}
	return snapshotID, nil
}

func (r *MemoryRepository) CreateFrontierSnapshot(_ context.Context, snapshot FrontierSnapshot) error {
	snapshot, err := normalizeFrontierSnapshot(snapshot)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.frontiers[snapshot.FrontierSnapshotID]; ok {
		return fmt.Errorf("frontier snapshot %q already exists", snapshot.FrontierSnapshotID)
	}
	for _, taskSnapshotID := range snapshot.TaskSnapshotIDs {
		if _, ok := r.snapshots[taskSnapshotID]; !ok {
			return fmt.Errorf("task snapshot %q not found", taskSnapshotID)
		}
	}
	for _, parentID := range snapshot.ParentFrontierSnapshotIDs {
		if _, ok := r.frontiers[parentID]; !ok {
			return fmt.Errorf("parent frontier snapshot %q not found", parentID)
		}
	}
	r.frontiers[snapshot.FrontierSnapshotID] = snapshot
	return nil
}

func (r *MemoryRepository) GetFrontierSnapshot(_ context.Context, frontierSnapshotID string) (FrontierSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.frontiers[strings.TrimSpace(frontierSnapshotID)]
	if !ok {
		return FrontierSnapshot{}, fmt.Errorf("frontier snapshot %q not found", frontierSnapshotID)
	}
	return cloneFrontierSnapshot(snapshot), nil
}

func (r *MemoryRepository) ListFrontierSnapshotsByRun(_ context.Context, runID core.RunID) ([]FrontierSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]FrontierSnapshot, 0)
	for _, snapshot := range r.frontiers {
		if snapshot.RunID != runID {
			continue
		}
		items = append(items, cloneFrontierSnapshot(snapshot))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].FrontierSnapshotID < items[j].FrontierSnapshotID
	})
	return items, nil
}

func (r *MemoryRepository) UpdateRef(_ context.Context, ref Ref) error {
	ref, err := normalizeRef(ref)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if ref.FrontierSnapshotID != "" {
		if _, ok := r.frontiers[ref.FrontierSnapshotID]; !ok {
			return fmt.Errorf("frontier snapshot %q not found", ref.FrontierSnapshotID)
		}
	}
	for _, snapshotID := range ref.FrontierSnapshotIDs {
		if _, ok := r.snapshots[snapshotID]; !ok {
			return fmt.Errorf("frontier snapshot %q not found", snapshotID)
		}
	}
	r.refs[newRefKey(ref.RunID, ref.RefName)] = ref
	return nil
}

func (r *MemoryRepository) MoveRef(_ context.Context, req MoveRefRequest) (Ref, RefMoveEvent, error) {
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

	r.mu.Lock()
	defer r.mu.Unlock()
	if ref.FrontierSnapshotID != "" {
		if _, ok := r.frontiers[ref.FrontierSnapshotID]; !ok {
			return Ref{}, RefMoveEvent{}, fmt.Errorf("frontier snapshot %q not found", ref.FrontierSnapshotID)
		}
	}
	for _, snapshotID := range ref.FrontierSnapshotIDs {
		if _, ok := r.snapshots[snapshotID]; !ok {
			return Ref{}, RefMoveEvent{}, fmt.Errorf("task snapshot %q not found", snapshotID)
		}
	}
	for _, frontierID := range event.FromFrontierSnapshotIDs {
		if _, ok := r.frontiers[frontierID]; !ok {
			return Ref{}, RefMoveEvent{}, fmt.Errorf("from frontier snapshot %q not found", frontierID)
		}
	}
	for _, frontierID := range event.ToFrontierSnapshotIDs {
		if _, ok := r.frontiers[frontierID]; !ok {
			return Ref{}, RefMoveEvent{}, fmt.Errorf("to frontier snapshot %q not found", frontierID)
		}
	}
	key := newRefKey(ref.RunID, ref.RefName)
	current := r.refs[key]
	if strings.TrimSpace(current.FrontierSnapshotID) != strings.TrimSpace(req.ExpectedFrontierSnapshotID) {
		return Ref{}, RefMoveEvent{}, fmt.Errorf("%w: ref %q in run %q moved from %q to %q",
			ErrRefCASConflict, ref.RefName, ref.RunID, req.ExpectedFrontierSnapshotID, current.FrontierSnapshotID)
	}
	if _, ok := r.refMoves[event.EventID]; ok {
		return Ref{}, RefMoveEvent{}, fmt.Errorf("ref move event %q already exists", event.EventID)
	}
	r.refs[key] = ref
	r.refMoves[event.EventID] = event
	return cloneRef(ref), cloneRefMoveEvent(event), nil
}

func (r *MemoryRepository) GetRef(_ context.Context, runID core.RunID, refName string) (Ref, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ref, ok := r.refs[newRefKey(runID, refName)]
	if !ok {
		return Ref{}, fmt.Errorf("ref %q not found in run %q", refName, runID)
	}
	return cloneRef(ref), nil
}

func (r *MemoryRepository) ListRefsByRun(_ context.Context, runID core.RunID) ([]Ref, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Ref, 0)
	for _, ref := range r.refs {
		if ref.RunID != runID {
			continue
		}
		items = append(items, cloneRef(ref))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].RefName < items[j].RefName
	})
	return items, nil
}

func (r *MemoryRepository) CreateRefMoveEvent(_ context.Context, event RefMoveEvent) error {
	event, err := normalizeRefMoveEvent(event)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.refMoves[event.EventID]; ok {
		return fmt.Errorf("ref move event %q already exists", event.EventID)
	}
	for _, frontierID := range event.FromFrontierSnapshotIDs {
		if _, ok := r.frontiers[frontierID]; !ok {
			return fmt.Errorf("from frontier snapshot %q not found", frontierID)
		}
	}
	for _, frontierID := range event.ToFrontierSnapshotIDs {
		if _, ok := r.frontiers[frontierID]; !ok {
			return fmt.Errorf("to frontier snapshot %q not found", frontierID)
		}
	}
	r.refMoves[event.EventID] = event
	return nil
}

func (r *MemoryRepository) ListRefMoveEvents(_ context.Context, runID core.RunID, refName string) ([]RefMoveEvent, error) {
	if strings.TrimSpace(refName) == "" {
		refName = DefaultRefName
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]RefMoveEvent, 0)
	for _, event := range r.refMoves {
		if event.RunID != runID || event.RefName != strings.TrimSpace(refName) {
			continue
		}
		items = append(items, cloneRefMoveEvent(event))
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].EventID < items[j].EventID
	})
	return items, nil
}

func normalizeObject(obj ArtifactObject) (ArtifactObject, error) {
	obj.ObjectID = strings.TrimSpace(obj.ObjectID)
	obj.ObjectType = strings.TrimSpace(obj.ObjectType)
	obj.StorageURI = strings.TrimSpace(obj.StorageURI)
	if obj.ObjectID == "" {
		return ArtifactObject{}, fmt.Errorf("artifact object id is required")
	}
	if obj.ObjectType == "" {
		obj.ObjectType = ObjectTypeBlob
	}
	if obj.CreatedAt.IsZero() {
		obj.CreatedAt = time.Now().UTC()
	}
	return obj, nil
}

func normalizeLogicalArtifact(item LogicalArtifact) (LogicalArtifact, error) {
	item.Namespace = strings.TrimSpace(item.Namespace)
	item.LogicalKey = strings.TrimSpace(item.LogicalKey)
	item.LogicalArtifactID = strings.TrimSpace(item.LogicalArtifactID)
	if item.RunID == "" {
		return LogicalArtifact{}, fmt.Errorf("logical artifact run_id is required")
	}
	if item.Namespace == "" {
		return LogicalArtifact{}, fmt.Errorf("logical artifact namespace is required")
	}
	if item.LogicalKey == "" {
		return LogicalArtifact{}, fmt.Errorf("logical artifact logical_key is required")
	}
	if item.LogicalArtifactID == "" {
		item.LogicalArtifactID = StableLogicalArtifactID(item.RunID, item.Namespace, item.LogicalKey)
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	return item, nil
}

func normalizeArtifactVersion(version ArtifactVersion) (ArtifactVersion, error) {
	version.LogicalArtifactID = strings.TrimSpace(version.LogicalArtifactID)
	version.ArtifactVersionID = strings.TrimSpace(version.ArtifactVersionID)
	version.ObjectIDs = NormalizeIDs(version.ObjectIDs)
	if version.LogicalArtifactID == "" {
		return ArtifactVersion{}, fmt.Errorf("artifact version logical_artifact_id is required")
	}
	if len(version.ObjectIDs) == 0 {
		return ArtifactVersion{}, fmt.Errorf("artifact version object_ids is required")
	}
	if version.ArtifactVersionID == "" {
		version.ArtifactVersionID = StableArtifactVersionID(version.LogicalArtifactID, version.ObjectIDs)
	}
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	}
	return version, nil
}

func normalizeBag(bag ArtifactBag) (ArtifactBag, error) {
	bag.BagID = strings.TrimSpace(bag.BagID)
	bag.ArtifactVersionIDs = NormalizeIDs(bag.ArtifactVersionIDs)
	if bag.BagID == "" {
		return ArtifactBag{}, fmt.Errorf("artifact bag id is required")
	}
	if bag.RunID == "" {
		return ArtifactBag{}, fmt.Errorf("artifact bag run_id is required")
	}
	if len(bag.ArtifactVersionIDs) == 0 {
		return ArtifactBag{}, fmt.Errorf("artifact bag artifact_version_ids is required")
	}
	if bag.CreatedAt.IsZero() {
		bag.CreatedAt = time.Now().UTC()
	}
	return bag, nil
}

func normalizeSnapshot(snapshot TaskSnapshot) (TaskSnapshot, error) {
	snapshot.SnapshotID = strings.TrimSpace(snapshot.SnapshotID)
	snapshot.PipelineInstanceID = core.PipelineInstanceID(strings.TrimSpace(string(snapshot.PipelineInstanceID)))
	snapshot.TransitionID = core.StageID(strings.TrimSpace(string(snapshot.TransitionID)))
	snapshot.AgentRole = core.AgentRole(strings.TrimSpace(string(snapshot.AgentRole)))
	snapshot.AgentID = core.AgentID(strings.TrimSpace(string(snapshot.AgentID)))
	snapshot.Op = strings.TrimSpace(snapshot.Op)
	snapshot.DiagnosticsJSON = strings.TrimSpace(snapshot.DiagnosticsJSON)
	snapshot.RuntimeContextJSON = strings.TrimSpace(snapshot.RuntimeContextJSON)
	snapshot.InputBagIDs = NormalizeIDs(snapshot.InputBagIDs)
	snapshot.OutputBagIDs = NormalizeIDs(snapshot.OutputBagIDs)
	if snapshot.SnapshotID == "" {
		if snapshot.RunID == "" || snapshot.TaskID == "" {
			return TaskSnapshot{}, fmt.Errorf("task snapshot id is required")
		}
		if snapshot.CreatedAt.IsZero() {
			snapshot.CreatedAt = time.Now().UTC()
		}
		snapshot.SnapshotID = StableSnapshotID(snapshot.RunID, snapshot.TaskID, snapshot.CreatedAt)
	}
	if snapshot.RunID == "" {
		return TaskSnapshot{}, fmt.Errorf("task snapshot run_id is required")
	}
	if snapshot.TaskID == "" {
		return TaskSnapshot{}, fmt.Errorf("task snapshot task_id is required")
	}
	if snapshot.Result == "" {
		return TaskSnapshot{}, fmt.Errorf("task snapshot result is required")
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	return snapshot, nil
}

func normalizeFrontierSnapshot(snapshot FrontierSnapshot) (FrontierSnapshot, error) {
	snapshot.FrontierSnapshotID = strings.TrimSpace(snapshot.FrontierSnapshotID)
	snapshot.ParentFrontierSnapshotIDs = NormalizeIDs(snapshot.ParentFrontierSnapshotIDs)
	snapshot.TaskSnapshotIDs = NormalizeIDs(snapshot.TaskSnapshotIDs)
	snapshot.CreatedByMode = strings.TrimSpace(snapshot.CreatedByMode)
	snapshot.CreatedByEventID = strings.TrimSpace(snapshot.CreatedByEventID)
	snapshot.DetailsJSON = strings.TrimSpace(snapshot.DetailsJSON)
	if snapshot.RunID == "" {
		return FrontierSnapshot{}, fmt.Errorf("frontier snapshot run_id is required")
	}
	if len(snapshot.TaskSnapshotIDs) == 0 {
		return FrontierSnapshot{}, fmt.Errorf("frontier snapshot task_snapshot_ids is required")
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	if snapshot.FrontierSnapshotID == "" {
		snapshot.FrontierSnapshotID = StableFrontierSnapshotID(snapshot.RunID, snapshot.TaskSnapshotIDs, snapshot.ParentFrontierSnapshotIDs, snapshot.CreatedAt)
	}
	return snapshot, nil
}

func normalizeRef(ref Ref) (Ref, error) {
	ref.RefName = strings.TrimSpace(ref.RefName)
	ref.FrontierSnapshotID = strings.TrimSpace(ref.FrontierSnapshotID)
	ref.FrontierSnapshotIDs = NormalizeIDs(ref.FrontierSnapshotIDs)
	if ref.RefName == "" {
		ref.RefName = DefaultRefName
	}
	if ref.RunID == "" {
		return Ref{}, fmt.Errorf("ref run_id is required")
	}
	if ref.FrontierSnapshotID == "" && len(ref.FrontierSnapshotIDs) == 0 {
		return Ref{}, fmt.Errorf("ref frontier snapshot is required")
	}
	if ref.UpdatedAt.IsZero() {
		ref.UpdatedAt = time.Now().UTC()
	}
	return ref, nil
}

func normalizeRefMoveEvent(event RefMoveEvent) (RefMoveEvent, error) {
	event.EventID = strings.TrimSpace(event.EventID)
	event.RefName = strings.TrimSpace(event.RefName)
	event.FromFrontierSnapshotIDs = NormalizeIDs(event.FromFrontierSnapshotIDs)
	event.ToFrontierSnapshotIDs = NormalizeIDs(event.ToFrontierSnapshotIDs)
	event.Mode = strings.TrimSpace(event.Mode)
	event.Reason = strings.TrimSpace(event.Reason)
	event.DetailsJSON = strings.TrimSpace(event.DetailsJSON)
	if event.RefName == "" {
		event.RefName = DefaultRefName
	}
	if event.RunID == "" {
		return RefMoveEvent{}, fmt.Errorf("ref move event run_id is required")
	}
	if len(event.ToFrontierSnapshotIDs) == 0 {
		return RefMoveEvent{}, fmt.Errorf("ref move event to_frontier_snapshot_ids is required")
	}
	if event.Mode == "" {
		event.Mode = RefMoveModeAdvance
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.EventID == "" {
		event.EventID = StableRefMoveEventID(event.RunID, event.RefName, event.FromFrontierSnapshotIDs, event.ToFrontierSnapshotIDs, event.Mode, event.CreatedAt)
	}
	return event, nil
}

func cloneBag(bag ArtifactBag) ArtifactBag {
	bag.ArtifactVersionIDs = append([]string(nil), bag.ArtifactVersionIDs...)
	return bag
}

func cloneSnapshot(snapshot TaskSnapshot) TaskSnapshot {
	snapshot.InputBagIDs = append([]string(nil), snapshot.InputBagIDs...)
	snapshot.OutputBagIDs = append([]string(nil), snapshot.OutputBagIDs...)
	return snapshot
}

func cloneFrontierSnapshot(snapshot FrontierSnapshot) FrontierSnapshot {
	snapshot.ParentFrontierSnapshotIDs = append([]string(nil), snapshot.ParentFrontierSnapshotIDs...)
	snapshot.TaskSnapshotIDs = append([]string(nil), snapshot.TaskSnapshotIDs...)
	return snapshot
}

func cloneRef(ref Ref) Ref {
	ref.FrontierSnapshotIDs = append([]string(nil), ref.FrontierSnapshotIDs...)
	return ref
}

func cloneRefMoveEvent(event RefMoveEvent) RefMoveEvent {
	event.FromFrontierSnapshotIDs = append([]string(nil), event.FromFrontierSnapshotIDs...)
	event.ToFrontierSnapshotIDs = append([]string(nil), event.ToFrontierSnapshotIDs...)
	return event
}

type logicalKey struct {
	runID      core.RunID
	namespace  string
	logicalKey string
}

func newLogicalKey(runID core.RunID, namespace string, key string) logicalKey {
	return logicalKey{
		runID:      runID,
		namespace:  strings.TrimSpace(namespace),
		logicalKey: strings.TrimSpace(key),
	}
}

type versionKey struct {
	logicalArtifactID string
	objectIDs         string
}

func newVersionKey(logicalArtifactID string, objectIDs []string) versionKey {
	return versionKey{
		logicalArtifactID: strings.TrimSpace(logicalArtifactID),
		objectIDs:         strings.Join(NormalizeIDs(objectIDs), "\x00"),
	}
}

type refKey struct {
	runID   core.RunID
	refName string
}

func newRefKey(runID core.RunID, refName string) refKey {
	refName = strings.TrimSpace(refName)
	if refName == "" {
		refName = DefaultRefName
	}
	return refKey{runID: runID, refName: refName}
}
