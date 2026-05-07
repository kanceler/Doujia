package repo

import (
	"context"
	"devflow/internal/core"
	"fmt"
	"sort"
	"sync"
)

type MemoryRunRepository struct {
	mu   sync.RWMutex
	runs map[core.RunID]RunRecord
}

func NewMemoryRunRepository() *MemoryRunRepository {
	return &MemoryRunRepository{runs: make(map[core.RunID]RunRecord)}
}

func (r *MemoryRunRepository) Create(_ context.Context, run RunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.ID]; exists {
		return fmt.Errorf("run %q already exists", run.ID)
	}
	r.runs[run.ID] = run
	return nil
}

func (r *MemoryRunRepository) Get(_ context.Context, runID core.RunID) (RunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[runID]
	if !ok {
		return RunRecord{}, fmt.Errorf("run %q not found", runID)
	}
	return run, nil
}

func (r *MemoryRunRepository) List(_ context.Context) ([]RunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RunRecord, 0, len(r.runs))
	for _, run := range r.runs {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRunRepository) Update(_ context.Context, run RunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.ID]; !exists {
		return fmt.Errorf("run %q not found", run.ID)
	}
	r.runs[run.ID] = run
	return nil
}

type MemoryProjectRepository struct {
	mu       sync.RWMutex
	projects map[string]ProjectRecord
}

func NewMemoryProjectRepository() *MemoryProjectRepository {
	return &MemoryProjectRepository{projects: make(map[string]ProjectRecord)}
}

func (r *MemoryProjectRepository) Create(_ context.Context, project ProjectRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.projects[project.ProjectID]; exists {
		return fmt.Errorf("project %q already exists", project.ProjectID)
	}
	r.projects[project.ProjectID] = project
	return nil
}

func (r *MemoryProjectRepository) Get(_ context.Context, projectID string) (ProjectRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	project, ok := r.projects[projectID]
	if !ok {
		return ProjectRecord{}, fmt.Errorf("project %q not found", projectID)
	}
	return project, nil
}

func (r *MemoryProjectRepository) List(_ context.Context) ([]ProjectRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProjectRecord, 0, len(r.projects))
	for _, project := range r.projects {
		out = append(out, project)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ProjectID < out[j].ProjectID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryProjectRepository) Update(_ context.Context, project ProjectRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.projects[project.ProjectID]; !exists {
		return fmt.Errorf("project %q not found", project.ProjectID)
	}
	r.projects[project.ProjectID] = project
	return nil
}

type MemoryTaskRepository struct {
	mu    sync.RWMutex
	tasks map[taskKey]TaskRecord
}

func NewMemoryTaskRepository() *MemoryTaskRepository {
	return &MemoryTaskRepository{tasks: make(map[taskKey]TaskRecord)}
}

func (r *MemoryTaskRepository) Create(_ context.Context, task TaskRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newTaskKey(task.RunID, task.ID)
	if _, exists := r.tasks[key]; exists {
		return fmt.Errorf("task %q already exists in run %q", task.ID, task.RunID)
	}
	r.tasks[key] = task
	return nil
}

func (r *MemoryTaskRepository) Get(_ context.Context, runID core.RunID, taskID core.TaskID) (TaskRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[newTaskKey(runID, taskID)]
	if !ok {
		return TaskRecord{}, fmt.Errorf("task %q not found in run %q", taskID, runID)
	}
	return task, nil
}

func (r *MemoryTaskRepository) Update(_ context.Context, task TaskRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newTaskKey(task.RunID, task.ID)
	if _, exists := r.tasks[key]; !exists {
		return fmt.Errorf("task %q not found in run %q", task.ID, task.RunID)
	}
	r.tasks[key] = task
	return nil
}

func (r *MemoryTaskRepository) ListByRun(_ context.Context, runID core.RunID) ([]TaskRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TaskRecord, 0)
	for _, task := range r.tasks {
		if task.RunID == runID {
			out = append(out, task)
		}
	}
	return out, nil
}

type taskKey struct {
	runID  core.RunID
	taskID core.TaskID
}

func newTaskKey(runID core.RunID, taskID core.TaskID) taskKey {
	return taskKey{runID: runID, taskID: taskID}
}

type MemoryPipelineInstanceRepository struct {
	mu        sync.RWMutex
	instances map[pipelineInstanceKey]PipelineInstanceRecord
}

func NewMemoryPipelineInstanceRepository() *MemoryPipelineInstanceRepository {
	return &MemoryPipelineInstanceRepository{instances: make(map[pipelineInstanceKey]PipelineInstanceRecord)}
}

func (r *MemoryPipelineInstanceRepository) Create(_ context.Context, instance PipelineInstanceRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newPipelineInstanceKey(instance.RunID, instance.ID)
	if _, exists := r.instances[key]; exists {
		return fmt.Errorf("pipeline instance %q already exists in run %q", instance.ID, instance.RunID)
	}
	r.instances[key] = clonePipelineInstanceRecord(instance)
	return nil
}

func (r *MemoryPipelineInstanceRepository) Get(_ context.Context, runID core.RunID, instanceID core.PipelineInstanceID) (PipelineInstanceRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	instance, ok := r.instances[newPipelineInstanceKey(runID, instanceID)]
	if !ok {
		return PipelineInstanceRecord{}, fmt.Errorf("pipeline instance %q not found in run %q", instanceID, runID)
	}
	return clonePipelineInstanceRecord(instance), nil
}

func (r *MemoryPipelineInstanceRepository) Update(_ context.Context, instance PipelineInstanceRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newPipelineInstanceKey(instance.RunID, instance.ID)
	if _, exists := r.instances[key]; !exists {
		return fmt.Errorf("pipeline instance %q not found in run %q", instance.ID, instance.RunID)
	}
	r.instances[key] = clonePipelineInstanceRecord(instance)
	return nil
}

func (r *MemoryPipelineInstanceRepository) ListByRun(_ context.Context, runID core.RunID) ([]PipelineInstanceRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PipelineInstanceRecord, 0)
	for _, instance := range r.instances {
		if instance.RunID == runID {
			out = append(out, clonePipelineInstanceRecord(instance))
		}
	}
	return out, nil
}

func (r *MemoryPipelineInstanceRepository) ListChildren(_ context.Context, runID core.RunID, parentID core.PipelineInstanceID) ([]PipelineInstanceRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PipelineInstanceRecord, 0)
	for _, instance := range r.instances {
		if instance.RunID == runID && instance.ParentID != nil && *instance.ParentID == parentID {
			out = append(out, clonePipelineInstanceRecord(instance))
		}
	}
	return out, nil
}

type pipelineInstanceKey struct {
	runID      core.RunID
	instanceID core.PipelineInstanceID
}

func newPipelineInstanceKey(runID core.RunID, instanceID core.PipelineInstanceID) pipelineInstanceKey {
	return pipelineInstanceKey{runID: runID, instanceID: instanceID}
}

func clonePipelineInstanceRecord(instance PipelineInstanceRecord) PipelineInstanceRecord {
	instance.Params = cloneStringMap(instance.Params)
	instance.AgentBindings = cloneAgentBindingMap(instance.AgentBindings)
	instance.InputBagIDs = cloneStringMap(instance.InputBagIDs)
	instance.InputBagIDLists = cloneStringSliceMap(instance.InputBagIDLists)
	instance.OutputBagIDs = cloneStringMap(instance.OutputBagIDs)
	instance.OutputBagIDLists = cloneStringSliceMap(instance.OutputBagIDLists)
	instance.HandlerBindings = cloneHandlerBindingRefs(instance.HandlerBindings)
	instance.ExceptionFrames = cloneExceptionFrames(instance.ExceptionFrames)
	return instance
}

func cloneHandlerBindingRefs(in []core.HandlerBindingRef) []core.HandlerBindingRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.HandlerBindingRef, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Handles = append([]string(nil), item.Handles...)
		out[i].Replaces = cloneBagBindingRefs(item.Replaces)
		out[i].Indexes = cloneStringMap(item.Indexes)
	}
	return out
}

func cloneExceptionFrames(in []core.ExceptionFrame) []core.ExceptionFrame {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.ExceptionFrame, len(in))
	for i, item := range in {
		out[i] = item
		out[i].FailedInputBags = cloneBagBindingRefs(item.FailedInputBags)
		out[i].FailureBags = cloneBagBindingRefs(item.FailureBags)
		out[i].SelectedHandlers = cloneHandlerBindingRefs(item.SelectedHandlers)
	}
	return out
}

func cloneBagBindingRefs(in []core.BagBindingRef) []core.BagBindingRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.BagBindingRef, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Indexes = cloneStringMap(item.Indexes)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, value := range in {
		out[key] = append([]string(nil), value...)
	}
	return out
}

func cloneAgentBindingMap(in map[string]core.AgentID) map[string]core.AgentID {
	if in == nil {
		return nil
	}
	out := make(map[string]core.AgentID, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type MemoryArtifactRepository struct {
	mu        sync.RWMutex
	artifacts map[artifactKey]ArtifactRecord
}

func NewMemoryArtifactRepository() *MemoryArtifactRepository {
	return &MemoryArtifactRepository{artifacts: make(map[artifactKey]ArtifactRecord)}
}

func (r *MemoryArtifactRepository) Create(_ context.Context, artifact ArtifactRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if artifact.RunID == "" {
		return fmt.Errorf("artifact run_id is required")
	}
	if artifact.URI == "" {
		return fmt.Errorf("artifact uri is required")
	}
	key := newArtifactKey(artifact.RunID, artifact.URI)
	if artifact.ID == "" {
		artifact.ID = string(artifact.RunID) + ":" + artifact.URI
	}
	r.artifacts[key] = artifact
	return nil
}

func (r *MemoryArtifactRepository) ListByRun(_ context.Context, runID core.RunID) ([]ArtifactRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ArtifactRecord, 0)
	for _, artifact := range r.artifacts {
		if artifact.RunID == runID {
			out = append(out, artifact)
		}
	}
	return out, nil
}

func (r *MemoryArtifactRepository) ListByTask(_ context.Context, runID core.RunID, taskID core.TaskID) ([]ArtifactRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ArtifactRecord, 0)
	for _, artifact := range r.artifacts {
		if artifact.RunID == runID && artifact.TaskID == taskID {
			out = append(out, artifact)
		}
	}
	return out, nil
}

type artifactKey struct {
	runID core.RunID
	uri   string
}

func newArtifactKey(runID core.RunID, uri string) artifactKey {
	return artifactKey{runID: runID, uri: uri}
}

type MemoryEventRepository struct {
	mu     sync.RWMutex
	events map[string]EventRecord
}

func NewMemoryEventRepository() *MemoryEventRepository {
	return &MemoryEventRepository{events: make(map[string]EventRecord)}
}

func (r *MemoryEventRepository) Create(_ context.Context, event EventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.ID == "" {
		return fmt.Errorf("event id is required")
	}
	if event.RunID == "" {
		return fmt.Errorf("event run_id is required")
	}
	if _, exists := r.events[event.ID]; exists {
		return fmt.Errorf("event %q already exists", event.ID)
	}
	r.events[event.ID] = event
	return nil
}

func (r *MemoryEventRepository) ListByRun(_ context.Context, runID core.RunID) ([]EventRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EventRecord, 0)
	for _, event := range r.events {
		if event.RunID == runID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (r *MemoryEventRepository) ListByTask(_ context.Context, runID core.RunID, taskID core.TaskID) ([]EventRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EventRecord, 0)
	for _, event := range r.events {
		if event.RunID == runID && event.TaskID == taskID {
			out = append(out, event)
		}
	}
	return out, nil
}

type MemoryRunIterationRepository struct {
	mu         sync.RWMutex
	iterations map[runIterationKey]RunIterationRecord
}

func NewMemoryRunIterationRepository() *MemoryRunIterationRepository {
	return &MemoryRunIterationRepository{iterations: make(map[runIterationKey]RunIterationRecord)}
}

func (r *MemoryRunIterationRepository) Create(_ context.Context, iteration RunIterationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newRunIterationKey(iteration.RunID, iteration.IterationNo)
	if _, exists := r.iterations[key]; exists {
		return fmt.Errorf("run iteration %q/%d already exists", iteration.RunID, iteration.IterationNo)
	}
	r.iterations[key] = iteration
	return nil
}

func (r *MemoryRunIterationRepository) Update(_ context.Context, iteration RunIterationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := newRunIterationKey(iteration.RunID, iteration.IterationNo)
	if _, exists := r.iterations[key]; !exists {
		return fmt.Errorf("run iteration %q/%d not found", iteration.RunID, iteration.IterationNo)
	}
	r.iterations[key] = iteration
	return nil
}

func (r *MemoryRunIterationRepository) ListByRun(_ context.Context, runID core.RunID) ([]RunIterationRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RunIterationRecord, 0)
	for _, iteration := range r.iterations {
		if iteration.RunID == runID {
			out = append(out, iteration)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].IterationNo < out[j].IterationNo
	})
	return out, nil
}

type runIterationKey struct {
	runID       core.RunID
	iterationNo int
}

func newRunIterationKey(runID core.RunID, iterationNo int) runIterationKey {
	return runIterationKey{runID: runID, iterationNo: iterationNo}
}

type MemorySessionMessageRepository struct {
	mu       sync.RWMutex
	messages map[string]SessionMessageRecord
}

func NewMemorySessionMessageRepository() *MemorySessionMessageRepository {
	return &MemorySessionMessageRepository{messages: make(map[string]SessionMessageRecord)}
}

func (r *MemorySessionMessageRepository) Create(_ context.Context, message SessionMessageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if message.ID == "" {
		return fmt.Errorf("session message id is required")
	}
	if _, exists := r.messages[message.ID]; exists {
		return fmt.Errorf("session message %q already exists", message.ID)
	}
	r.messages[message.ID] = message
	return nil
}

func (r *MemorySessionMessageRepository) ListByRun(_ context.Context, runID core.RunID) ([]SessionMessageRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SessionMessageRecord, 0)
	for _, message := range r.messages {
		if message.RunID == runID {
			out = append(out, message)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

type MemorySessionArtifactRepository struct {
	mu        sync.RWMutex
	artifacts map[string]SessionArtifactRecord
}

func NewMemorySessionArtifactRepository() *MemorySessionArtifactRepository {
	return &MemorySessionArtifactRepository{artifacts: make(map[string]SessionArtifactRecord)}
}

func (r *MemorySessionArtifactRepository) Create(_ context.Context, artifact SessionArtifactRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if artifact.ID == "" {
		return fmt.Errorf("session artifact id is required")
	}
	if _, exists := r.artifacts[artifact.ID]; exists {
		return fmt.Errorf("session artifact %q already exists", artifact.ID)
	}
	r.artifacts[artifact.ID] = artifact
	return nil
}

func (r *MemorySessionArtifactRepository) ListByRun(_ context.Context, runID core.RunID) ([]SessionArtifactRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SessionArtifactRecord, 0)
	for _, artifact := range r.artifacts {
		if artifact.RunID == runID {
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

type MemoryImprovementItemRepository struct {
	mu    sync.RWMutex
	items map[string]ImprovementItemRecord
}

func NewMemoryImprovementItemRepository() *MemoryImprovementItemRepository {
	return &MemoryImprovementItemRepository{items: make(map[string]ImprovementItemRecord)}
}

func (r *MemoryImprovementItemRepository) Create(_ context.Context, item ImprovementItemRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ItemID == "" {
		return fmt.Errorf("improvement item id is required")
	}
	if _, exists := r.items[item.ItemID]; exists {
		return fmt.Errorf("improvement item %q already exists", item.ItemID)
	}
	r.items[item.ItemID] = item
	return nil
}

func (r *MemoryImprovementItemRepository) Update(_ context.Context, item ImprovementItemRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[item.ItemID]; !exists {
		return fmt.Errorf("improvement item %q not found", item.ItemID)
	}
	r.items[item.ItemID] = item
	return nil
}

func (r *MemoryImprovementItemRepository) ListByRun(_ context.Context, runID core.RunID) ([]ImprovementItemRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ImprovementItemRecord, 0)
	for _, item := range r.items {
		if item.RunID == runID {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ItemID < out[j].ItemID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
