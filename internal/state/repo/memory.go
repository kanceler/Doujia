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

