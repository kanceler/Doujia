package repo

import (
	"devflow/internal/core"
	"time"
)

type RunRecord = core.PipelineRun
type PipelineInstanceRecord = core.PipelineInstance
type TaskRecord = core.Task

type RunIterationRecord struct {
	RunID                      core.RunID
	IterationNo                int
	StartFrontierID            string
	DeliveryFrontierID         string
	AcceptanceCheckpointTaskID core.TaskID
	Status                     core.RunStatus
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type SessionMessageRecord struct {
	ID          string
	RunID       core.RunID
	IterationNo int
	Role        string
	MessageType string
	Content     string
	CreatedAt   time.Time
}

type SessionArtifactRecord struct {
	ID          string
	RunID       core.RunID
	IterationNo int
	Kind        string
	Title       string
	Content     string
	CreatedAt   time.Time
}

type ImprovementItemRecord struct {
	ItemID      string
	RunID       core.RunID
	IterationNo int
	Title       string
	Detail      string
	Source      string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProjectRecord struct {
	ProjectID string
	Name      string
	LLM       core.LLMRunConfig
	CreatedAt time.Time
	UpdatedAt time.Time
}

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
