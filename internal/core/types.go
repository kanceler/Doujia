package core

import "time"

type PipelineID string
type RunID string
type StageID string
type TaskID string
type SessionID string
type AgentID string
type RuntimeID string
type AgentRole string
type ArtifactRef string

type RunStatus string

const (
	RunStatusCreated   RunStatus = "created"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusWaitingExternal TaskStatus = "waiting_external"
	TaskStatusDispatched      TaskStatus = "dispatched"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusDone            TaskStatus = "done"
	TaskStatusBlocked         TaskStatus = "blocked"
	TaskStatusFailed          TaskStatus = "failed"
)

type TaskDirection string

const (
	TaskDirectionDispatch TaskDirection = "dispatch"
	TaskDirectionFeedback TaskDirection = "feedback"
)

type TaskResultCode string

const (
	TaskResultCodeOK   TaskResultCode = "kok"
	TaskResultCodeFail TaskResultCode = "kfail"
)

type ControlType string

const (
	ControlTypeNewCoder  ControlType = "new_coder"
	ControlTypeNewTester ControlType = "new_tester"
)

const (
	AgentRoleCEO       AgentRole = "ceo"
	AgentRolePM        AgentRole = "pm"
	AgentRoleArchitect AgentRole = "architect"
	AgentRoleCoder     AgentRole = "coder"
	AgentRoleTester    AgentRole = "tester"
	AgentRoleReviewer  AgentRole = "reviewer"
)

type Control struct {
	Type         ControlType `json:"type"`
	AgentName    string      `json:"agent_name"`
	ArtifactURIs []string    `json:"artifact_uris"`
}

type TaskMetaData struct {
	Direction    TaskDirection  `json:"direction"`
	RunID        RunID          `json:"run_id"`
	TaskID       TaskID         `json:"task_id"`
	ParentID     *TaskID        `json:"parent_id,omitempty"`
	DependsOn    *TaskID        `json:"depends_on,omitempty"`
	AgentID      AgentID        `json:"agent_id"`
	Op           string         `json:"op"`
	ArtifactURIs []string       `json:"artifact_uris"`
	Result       TaskResultCode `json:"result,omitempty"`
	Control      []Control      `json:"control,omitempty"`
}

type RunConfig struct {
	LLM LLMConfig
}

type LLMConfig struct {
	ProviderType   string
	BaseURL        string
	APIKey         string
	Model          string
	RequestTimeout time.Duration
}

type PipelineRun struct {
	ID          RunID
	PipelineID  PipelineID
	Status      RunStatus
	ProjectDir  string
	SessionID   SessionID
	Config      RunConfig
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Task struct {
	ID                 TaskID
	RunID              RunID
	StageID            StageID
	AgentRole          AgentRole
	AgentID            AgentID
	Status             TaskStatus
	ParentID           *TaskID
	DependsOn          *TaskID
	InputArtifactRefs  []ArtifactRef
	OutputArtifactRefs []ArtifactRef
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

