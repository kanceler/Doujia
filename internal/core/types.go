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
	TaskResultCodeOK             TaskResultCode = "kok"
	TaskResultCodeFail           TaskResultCode = "kfail"
	TaskResultCodeRewrite        TaskResultCode = "krewrite"
	TaskResultCodeReplan         TaskResultCode = "kreplan"
	TaskResultCodeBug            TaskResultCode = "kbug"
	TaskResultCodeControlInvalid TaskResultCode = "kcontrol_invalid"
)

const (
	TaskOpWritePlan     = "write_plan"
	TaskOpReviewPlan    = "review_plan"
	TaskOpRewrite       = "rewrite"
	TaskOpReplan        = "replan"
	TaskOpSplitModule   = "split_module"
	TaskOpResplitModule = "resplit_module"
	TaskOpWriteCode     = "write_code"
	TaskOpTestData      = "test_data"
	TaskOpTestCode      = "test_code"
	TaskOpDebug         = "debug"
	TaskOpMergeCode     = "merge_code"
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
	DependsOnIDs []TaskID       `json:"depends_on_ids,omitempty"`
	AgentID      AgentID        `json:"agent_id"`
	Op           string         `json:"op"`
	ArtifactURIs []string       `json:"artifact_uris"`
	Result       TaskResultCode `json:"result,omitempty"`
	Control      []Control      `json:"control,omitempty"`
}

type AgentTaskHistory struct {
	RunID              RunID      `json:"run_id"`
	TaskID             TaskID     `json:"task_id"`
	AgentID            AgentID    `json:"agent_id"`
	Op                 string     `json:"op"`
	Status             TaskStatus `json:"status"`
	InputArtifactURIs  []string   `json:"input_artifact_uris,omitempty"`
	OutputArtifactURIs []string   `json:"output_artifact_uris,omitempty"`
}

type RunConfig struct {
	LLM      LLMConfig      `json:"llm"`
	Delivery DeliveryConfig `json:"delivery,omitempty"`
}

type LLMConfig struct {
	ProviderType   string        `json:"provider_type"`
	BaseURL        string        `json:"base_url,omitempty"`
	APIKey         string        `json:"api_key,omitempty"`
	Model          string        `json:"model,omitempty"`
	RequestTimeout time.Duration `json:"request_timeout,omitempty"`
}

type DeliveryConfig struct {
	MaxCoderAgents           int          `json:"max_coder_agents"`
	MaxTesterAgents          int          `json:"max_tester_agents"`
	RequireTesterPerModule   bool         `json:"require_tester_per_module"`
	AllowParallelWork        bool         `json:"allow_parallel_work"`
	GlobalVerifyCommands     []string     `json:"global_verify_commands,omitempty"`
	GlobalTestTimeoutSeconds int          `json:"global_test_timeout_seconds,omitempty"`
	Git                      GitRunConfig `json:"git"`
}

type GitRunConfig struct {
	RepoURL    string `json:"repo_url,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
	BaseRef    string `json:"base_ref,omitempty"`
}

type PipelineRun struct {
	ID         RunID
	PipelineID PipelineID
	Status     RunStatus
	ProjectDir string
	SessionID  SessionID
	Config     RunConfig
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	DependsOnIDs       []TaskID
	InputArtifactRefs  []ArtifactRef
	OutputArtifactRefs []ArtifactRef
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
