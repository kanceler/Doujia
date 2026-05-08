package core

import "time"

type PipelineID string
type PipelineInstanceID string
type RunID string
type StageID string
type TaskID string
type SessionID string
type AgentID string
type RuntimeID string
type AgentRole string
type ArtifactRef string
type ExceptionFrameID string

type RunStatus string

const (
	RunStatusCreated            RunStatus = "created"
	RunStatusRunning            RunStatus = "running"
	RunStatusBlocked            RunStatus = "blocked"
	RunStatusAwaitingAcceptance RunStatus = "awaiting_acceptance"
	RunStatusCompleted          RunStatus = "completed"
	RunStatusFailed             RunStatus = "failed"
)

type PipelineInstanceStatus string

const (
	PipelineInstanceStatusCreated   PipelineInstanceStatus = "created"
	PipelineInstanceStatusRunning   PipelineInstanceStatus = "running"
	PipelineInstanceStatusCompleted PipelineInstanceStatus = "completed"
	PipelineInstanceStatusFailed    PipelineInstanceStatus = "failed"
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

type ExecutionMode string

const (
	ExecutionModeNormal  ExecutionMode = "normal"
	ExecutionModeRepair  ExecutionMode = "repair"
	ExecutionModeRewrite ExecutionMode = "rewrite"
	ExecutionModeReuse   ExecutionMode = "reuse"
)

const (
	TaskOpWritePlan       = "write_plan"
	TaskOpReviewPlan      = "review_plan"
	TaskOpRewrite         = "rewrite"
	TaskOpReplan          = "replan"
	TaskOpCreateContainer = "create_container"
	TaskOpSplitModule     = "split_module"
	TaskOpResplitModule   = "resplit_module"
	TaskOpWriteCode       = "write_code"
	TaskOpTestData        = "test_data"
	TaskOpTestCode        = "test_code"
	TaskOpDebug           = "debug"
	TaskOpMergeCode       = "merge_code"
)

type ControlType string

const (
	ControlTypeNewCoder      ControlType = "new_coder"
	ControlTypeNewTester     ControlType = "new_tester"
	ControlTypeStartPipeline ControlType = "start_pipeline"
)

const (
	AgentRoleCEO       AgentRole = "ceo"
	AgentRolePM        AgentRole = "pm"
	AgentRoleArchitect AgentRole = "architect"
	AgentRoleCoder     AgentRole = "coder"
	AgentRoleTester    AgentRole = "tester"
)

type Control struct {
	Type          ControlType        `json:"type"`
	AgentName     string             `json:"agent_name,omitempty"`
	ArtifactURIs  []string           `json:"artifact_uris,omitempty"`
	TransitionID  string             `json:"transition_id,omitempty"`
	PipelineID    PipelineID         `json:"pipeline_id,omitempty"`
	InstanceKey   string             `json:"instance_key,omitempty"`
	Params        map[string]string  `json:"params,omitempty"`
	AgentBindings map[string]AgentID `json:"agent_bindings,omitempty"`
	InputBags     map[string]string  `json:"input_bags,omitempty"`
	OutputBags    map[string]string  `json:"output_bags,omitempty"`
}

type TaskMetaData struct {
	Direction     TaskDirection         `json:"direction"`
	RunID         RunID                 `json:"run_id"`
	TaskID        TaskID                `json:"task_id"`
	ParentID      *TaskID               `json:"parent_id,omitempty"`
	DependsOnIDs  []TaskID              `json:"depends_on_ids,omitempty"`
	AgentID       AgentID               `json:"agent_id"`
	Op            string                `json:"op"`
	ArtifactURIs  []string              `json:"artifact_uris"`
	InputBagIDs   []string              `json:"input_bag_ids,omitempty"`
	InputBags     []BagBindingRef       `json:"input_bags,omitempty"`
	InputBundle   *AgentInputBundle     `json:"input_bundle,omitempty"`
	ExecutionMode ExecutionMode         `json:"execution_mode,omitempty"`
	Result        TaskResultCode        `json:"result,omitempty"`
	Outputs       []AgentOutput         `json:"outputs,omitempty"`
	ProducedBags  []ProducedBagManifest `json:"produced_bags,omitempty"`
	Control       []Control             `json:"control,omitempty"`
	Commit        *CommitReceipt        `json:"commit_receipt,omitempty"`
}

type AgentInputBundle struct {
	InputDir        string              `json:"input_dir,omitempty"`
	OutputDir       string              `json:"output_dir,omitempty"`
	OutputURIBase   string              `json:"output_uri_base,omitempty"`
	Inputs          []InputArtifact     `json:"inputs,omitempty"`
	PreviousOutputs []PreviousOutputRef `json:"previous_outputs,omitempty"`
	Bags            []AgentInputBag     `json:"bags,omitempty"`
	Versions        []AgentInputVersion `json:"versions,omitempty"`
}

type InputArtifact struct {
	LogicalKey        string `json:"logical_key"`
	Path              string `json:"path"`
	ArtifactVersionID string `json:"artifact_version_id"`
	LogicalArtifactID string `json:"logical_artifact_id,omitempty"`
	ObjectType        string `json:"object_type,omitempty"`
	ContentType       string `json:"content_type,omitempty"`
	Encoding          string `json:"encoding,omitempty"`
	Description       string `json:"description,omitempty"`
}

type PreviousOutputRef struct {
	LogicalKey        string `json:"logical_key"`
	ArtifactVersionID string `json:"artifact_version_id"`
	ArtifactURI       string `json:"artifact_uri,omitempty"`
	LogicalArtifactID string `json:"logical_artifact_id,omitempty"`
	ObjectType        string `json:"object_type,omitempty"`
	ContentType       string `json:"content_type,omitempty"`
	Encoding          string `json:"encoding,omitempty"`
	Path              string `json:"path,omitempty"`
	Description       string `json:"description,omitempty"`
}

type AgentInputBag struct {
	Name               string            `json:"name,omitempty"`
	BagID              string            `json:"bag_id"`
	Indexes            map[string]string `json:"indexes,omitempty"`
	ArtifactVersionIDs []string          `json:"artifact_version_ids"`
}

type AgentInputVersion struct {
	ArtifactVersionID string             `json:"artifact_version_id"`
	LogicalArtifactID string             `json:"logical_artifact_id"`
	LogicalKey        string             `json:"logical_key,omitempty"`
	ObjectType        string             `json:"object_type,omitempty"`
	ContentType       string             `json:"content_type,omitempty"`
	Encoding          string             `json:"encoding,omitempty"`
	StorageURI        string             `json:"storage_uri,omitempty"`
	LocalPath         string             `json:"local_path,omitempty"`
	ObjectIDs         []string           `json:"object_ids"`
	Objects           []AgentInputObject `json:"objects,omitempty"`
}

type AgentInputObject struct {
	ObjectID   string `json:"object_id"`
	ObjectType string `json:"object_type"`
	StorageURI string `json:"storage_uri,omitempty"`
}

type ProducedBagMember struct {
	LogicalKey        string `json:"logical_key,omitempty"`
	OutputRef         string `json:"output_ref,omitempty"`
	ArtifactVersionID string `json:"artifact_version_id,omitempty"`
}

type ProducedBagManifest struct {
	Name    string              `json:"name"`
	Indexes map[string]string   `json:"indexes,omitempty"`
	Members []ProducedBagMember `json:"members,omitempty"`
}

type CommittedBagDef struct {
	Name               string            `json:"name"`
	Indexes            map[string]string `json:"indexes,omitempty"`
	ArtifactVersionIDs []string          `json:"artifact_version_ids"`
	MemberLogicalKeys  []string          `json:"member_logical_keys,omitempty"`
}

type BagMemberRequirement struct {
	LogicalKey string `json:"logical_key"`
	Required   bool   `json:"required,omitempty"`
}

type InputBagSpec struct {
	Name       string                 `json:"name"`
	Required   bool                   `json:"required,omitempty"`
	Collection bool                   `json:"collection,omitempty"`
	Members    []BagMemberRequirement `json:"members,omitempty"`
}

type OutputBagSpec struct {
	Name       string                 `json:"name"`
	Required   bool                   `json:"required,omitempty"`
	Collection bool                   `json:"collection,omitempty"`
	Members    []BagMemberRequirement `json:"members,omitempty"`
}

type BagBindingRef struct {
	Name    string            `json:"name,omitempty"`
	BagID   string            `json:"bag_id"`
	Indexes map[string]string `json:"indexes,omitempty"`
}

type ExceptionFrame struct {
	ID                       ExceptionFrameID    `json:"id"`
	RunID                    RunID               `json:"run_id"`
	Result                   TaskResultCode      `json:"result"`
	OriginTaskID             TaskID              `json:"origin_task_id"`
	OriginPipelineInstanceID PipelineInstanceID  `json:"origin_pipeline_instance_id"`
	FailedTransitionID       string              `json:"failed_transition_id,omitempty"`
	FailedInputBags          []BagBindingRef     `json:"failed_input_bags,omitempty"`
	FailureBags              []BagBindingRef     `json:"failure_bags,omitempty"`
	SelectedHandlers         []HandlerBindingRef `json:"selected_handlers,omitempty"`
	ResumeTransitionID       string              `json:"resume_transition_id,omitempty"`
	CreatedAt                time.Time           `json:"created_at"`
}

type HandlerBindingRef struct {
	Name               string             `json:"name"`
	FromHandler        string             `json:"from_handler,omitempty"`
	OwnerInstanceID    PipelineInstanceID `json:"owner_instance_id"`
	ParentTransitionID string             `json:"parent_transition_id,omitempty"`
	Handles            []string           `json:"handles,omitempty"`
	Replaces           []BagBindingRef    `json:"replaces,omitempty"`
	Indexes            map[string]string  `json:"indexes,omitempty"`
}

type AgentOutput struct {
	LogicalKey        string `json:"logical_key"`
	ObjectType        string `json:"object_type"`
	ContentType       string `json:"content_type,omitempty"`
	Encoding          string `json:"encoding,omitempty"`
	Status            string `json:"status"`
	Path              string `json:"path,omitempty"`
	ArtifactURI       string `json:"artifact_uri,omitempty"`
	ArtifactVersionID string `json:"artifact_version_id,omitempty"`
	Description       string `json:"description,omitempty"`
}

type AgentError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AgentResult struct {
	Result  TaskResultCode `json:"result"`
	Message string         `json:"message,omitempty"`
	Outputs []AgentOutput  `json:"outputs"`
	Errors  []AgentError   `json:"errors,omitempty"`
	Control []Control      `json:"control,omitempty"`
}

type CommitReceipt struct {
	Result                 TaskResultCode    `json:"result"`
	ProducedBags           []CommittedBagDef `json:"produced_bags,omitempty"`
	MaterializedOutputRefs []string          `json:"materialized_output_refs,omitempty"`
	Control                []Control         `json:"control,omitempty"`
	DiagnosticsJSON        string            `json:"diagnostics_json,omitempty"`
}

func (r CommitReceipt) EffectiveCommittedBags() []CommittedBagDef {
	return append([]CommittedBagDef(nil), r.ProducedBags...)
}

type RunConfig struct {
	Delivery DeliveryConfig `json:"delivery,omitempty"`
	LLM      LLMRunConfig   `json:"llm,omitempty"`
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

type LLMRunConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIStyle string `json:"api_style,omitempty"`
}

type PipelineRun struct {
	ID                               RunID
	PipelineID                       PipelineID
	Status                           RunStatus
	ProjectID                        string
	ProjectDir                       string
	SessionID                        SessionID
	CurrentIterationNo               int
	LatestDeliveryFrontierID         string
	LatestAcceptanceCheckpointTaskID TaskID
	Config                           RunConfig
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
}

type PipelineInstance struct {
	ID                 PipelineInstanceID
	RunID              RunID
	PipelineID         PipelineID
	ParentID           *PipelineInstanceID
	ParentTransitionID string
	InstanceKey        string
	Status             PipelineInstanceStatus
	Params             map[string]string
	AgentBindings      map[string]AgentID
	InputBagIDs        map[string]string
	InputBagIDLists    map[string][]string
	OutputBagIDs       map[string]string
	OutputBagIDLists   map[string][]string
	HandlerBindings    []HandlerBindingRef
	ExceptionFrames    []ExceptionFrame
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Task struct {
	ID                 TaskID
	RunID              RunID
	PipelineInstanceID PipelineInstanceID
	StageID            StageID
	AgentRole          AgentRole
	AgentID            AgentID
	Op                 string
	Status             TaskStatus
	Result             TaskResultCode
	ErrorMessage       string
	ParentID           *TaskID
	DependsOnIDs       []TaskID
	InputArtifactRefs  []ArtifactRef
	OutputArtifactRefs []ArtifactRef
	InputBagIDs        []string
	InputBags          []BagBindingRef
	OutputBagIDs       []string
	ExecutionMode      ExecutionMode
	ExceptionFrameID   ExceptionFrameID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
