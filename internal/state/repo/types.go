package repo

import (
	"devflow/internal/core"
	"time"
)

type RunRecord = core.PipelineRun
type PipelineInstanceRecord = core.PipelineInstance
type TaskRecord = core.Task

type ArtifactRecord struct {
	ID        string
	RunID     core.RunID
	TaskID    core.TaskID
	AgentID   core.AgentID
	Kind      string
	URI       string
	CreatedAt time.Time
}

type EventRecord struct {
	ID          string
	RunID       core.RunID
	TaskID      core.TaskID
	AgentID     core.AgentID
	Type        string
	Message     string
	PayloadJSON string
	CreatedAt   time.Time
}
