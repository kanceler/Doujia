package repo

import (
	"context"
	"devflow/internal/core"
	"fmt"
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

func (r *MemoryRunRepository) Update(_ context.Context, run RunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.ID]; !exists {
		return fmt.Errorf("run %q not found", run.ID)
	}
	r.runs[run.ID] = run
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
