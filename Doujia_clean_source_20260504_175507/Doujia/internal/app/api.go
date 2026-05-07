package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agentbootstrap "devflow/internal/agent/bootstrap"
	agentcore "devflow/internal/agent/core"
	"devflow/internal/agent/llm"
	"devflow/internal/artifact"
	"devflow/internal/core"
	"devflow/internal/doujiagit"
	"devflow/internal/pipeline"
	"devflow/internal/runtime"
	"devflow/internal/state/repo"
)

type runView struct {
	RunID                            core.RunID      `json:"run_id"`
	PipelineID                       core.PipelineID `json:"pipeline_id"`
	Status                           core.RunStatus  `json:"status"`
	ProjectID                        string          `json:"project_id,omitempty"`
	ProjectDir                       string          `json:"project_dir"`
	SessionID                        core.SessionID  `json:"session_id,omitempty"`
	CurrentIterationNo               int             `json:"current_iteration_no"`
	LatestDeliveryFrontierID         string          `json:"latest_delivery_frontier_id,omitempty"`
	LatestAcceptanceCheckpointTaskID core.TaskID     `json:"latest_acceptance_checkpoint_task_id,omitempty"`
	Config                           core.RunConfig  `json:"config"`
	CreatedAt                        time.Time       `json:"created_at"`
	UpdatedAt                        time.Time       `json:"updated_at"`
}

type sessionMessageView struct {
	MessageID   string         `json:"message_id"`
	RunID       core.RunID     `json:"run_id"`
	IterationNo int            `json:"iteration_no"`
	Role        string         `json:"role"`
	MessageType string         `json:"message_type"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type doujiaSessionReply struct {
	Reply                        string `json:"reply"`
	ShouldEmitRequirementSummary bool   `json:"should_emit_requirement_summary"`
	SummaryTitle                 string `json:"summary_title,omitempty"`
	SummaryDetail                string `json:"summary_detail,omitempty"`
}

type projectView struct {
	ProjectID string            `json:"project_id"`
	Name      string            `json:"name"`
	LLM       core.LLMRunConfig `json:"llm"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type improvementItemView struct {
	ItemID      string     `json:"item_id"`
	RunID       core.RunID `json:"run_id"`
	IterationNo int        `json:"iteration_no"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type acceptanceCheckpointView struct {
	RunID                    core.RunID     `json:"run_id"`
	CheckpointTaskID         core.TaskID    `json:"checkpoint_task_id"`
	CurrentIterationNo       int            `json:"current_iteration_no"`
	LatestDeliveryFrontierID string         `json:"latest_delivery_frontier_id,omitempty"`
	Status                   core.RunStatus `json:"status"`
}

type runIterationView struct {
	RunID                      core.RunID     `json:"run_id"`
	IterationNo                int            `json:"iteration_no"`
	StartFrontierID            string         `json:"start_frontier_id,omitempty"`
	DeliveryFrontierID         string         `json:"delivery_frontier_id,omitempty"`
	AcceptanceCheckpointTaskID core.TaskID    `json:"acceptance_checkpoint_task_id,omitempty"`
	Status                     core.RunStatus `json:"status"`
	CreatedAt                  time.Time      `json:"created_at"`
	UpdatedAt                  time.Time      `json:"updated_at"`
}

type sessionArtifactView struct {
	ArtifactID  string     `json:"artifact_id"`
	RunID       core.RunID `json:"run_id"`
	IterationNo int        `json:"iteration_no"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	CreatedAt   time.Time  `json:"created_at"`
}

type workspaceOverviewView struct {
	Run                  runView                  `json:"run"`
	CurrentIterationNo   int                      `json:"current_iteration_no"`
	AcceptanceCheckpoint acceptanceCheckpointView `json:"acceptance_checkpoint"`
	Iterations           []runIterationView       `json:"iterations"`
	SessionArtifacts     []sessionArtifactView    `json:"session_artifacts"`
	OpenImprovementItems []improvementItemView    `json:"open_improvement_items"`
}

type pipelineWorkspaceView struct {
	RunID               core.RunID                      `json:"run_id"`
	Status              core.RunStatus                  `json:"status"`
	CurrentIterationNo  int                             `json:"current_iteration_no"`
	FocusInstanceID     string                          `json:"focus_instance_id,omitempty"`
	FocusPipelineID     string                          `json:"focus_pipeline_id,omitempty"`
	Iterations          []runIterationView              `json:"iterations"`
	Instances           []pipelineWorkspaceInstanceView `json:"instances"`
	Edges               []pipelineWorkspaceEdgeView     `json:"edges"`
	MainPipelineNodes   []pipelineWorkspaceMainNodeView `json:"main_pipeline_nodes,omitempty"`
	MainPipelineEdges   []pipelineWorkspaceMainEdgeView `json:"main_pipeline_edges,omitempty"`
	CollapsedChildNodes []pipelineWorkspaceMainNodeView `json:"collapsed_child_pipeline_nodes,omitempty"`
}

type gitBranchesView struct {
	RunID          core.RunID               `json:"run_id"`
	BaseBranch     string                   `json:"base_branch,omitempty"`
	BaseCommit     string                   `json:"base_commit,omitempty"`
	ContainerID    string                   `json:"container_id,omitempty"`
	ModuleBranches []gitModuleBranchView    `json:"module_branches"`
	Merge          *gitMergeView            `json:"merge,omitempty"`
	Warnings       []gitBranchesWarningView `json:"warnings,omitempty"`
}

type gitModuleBranchView struct {
	TaskID       core.TaskID  `json:"task_id,omitempty"`
	AgentID      core.AgentID `json:"agent_id,omitempty"`
	SnapshotID   string       `json:"snapshot_id,omitempty"`
	BagID        string       `json:"bag_id,omitempty"`
	LogicalKey   string       `json:"logical_key,omitempty"`
	CreatedAt    time.Time    `json:"created_at,omitempty"`
	ModuleID     string       `json:"module_id,omitempty"`
	ModuleName   string       `json:"module_name,omitempty"`
	Branch       string       `json:"branch,omitempty"`
	Commit       string       `json:"commit,omitempty"`
	BaseBranch   string       `json:"base_branch,omitempty"`
	BaseCommit   string       `json:"base_commit,omitempty"`
	ContainerID  string       `json:"container_id,omitempty"`
	ChangedFiles []string     `json:"changed_files,omitempty"`
	Result       string       `json:"result,omitempty"`
	TestPassed   *bool        `json:"test_passed,omitempty"`
}

type gitMergeView struct {
	TaskID         core.TaskID           `json:"task_id,omitempty"`
	AgentID        core.AgentID          `json:"agent_id,omitempty"`
	SnapshotID     string                `json:"snapshot_id,omitempty"`
	BagID          string                `json:"bag_id,omitempty"`
	LogicalKey     string                `json:"logical_key,omitempty"`
	CreatedAt      time.Time             `json:"created_at,omitempty"`
	BaseBranch     string                `json:"base_branch,omitempty"`
	BaseCommit     string                `json:"base_commit,omitempty"`
	ContainerID    string                `json:"container_id,omitempty"`
	MergedCommit   string                `json:"merged_commit,omitempty"`
	AppliedCommits []string              `json:"applied_commits,omitempty"`
	Modules        []gitModuleBranchView `json:"modules,omitempty"`
	Result         string                `json:"result,omitempty"`
}

type gitBranchesWarningView struct {
	TaskID     core.TaskID `json:"task_id,omitempty"`
	BagID      string      `json:"bag_id,omitempty"`
	LogicalKey string      `json:"logical_key,omitempty"`
	Message    string      `json:"message"`
}

type coderBranchArtifact struct {
	Kind         string   `json:"kind"`
	ModuleID     string   `json:"module_id"`
	ModuleName   string   `json:"module_name"`
	ContainerID  string   `json:"container_id"`
	RepoDir      string   `json:"repo_dir"`
	BaseBranch   string   `json:"base_branch"`
	BaseCommit   string   `json:"base_commit"`
	Branch       string   `json:"branch"`
	Commit       string   `json:"commit"`
	Worktree     string   `json:"worktree"`
	ChangedFiles []string `json:"changed_files"`
	Result       string   `json:"result"`
	TestPassed   *bool    `json:"test_passed"`
}

type mergedMainBranchArtifact struct {
	Kind           string                `json:"kind"`
	BaseBranch     string                `json:"base_branch"`
	BaseCommit     string                `json:"base_commit"`
	ContainerID    string                `json:"container_id"`
	MergedCommit   string                `json:"merged_commit"`
	AppliedCommits []string              `json:"applied_commits"`
	Modules        []coderBranchArtifact `json:"modules"`
	RepoDir        string                `json:"repo_dir"`
	Result         string                `json:"result"`
	SchemaVersion  int                   `json:"schema_version"`
}

type pipelineWorkspaceInstanceView struct {
	InstanceID         core.PipelineInstanceID     `json:"instance_id"`
	PipelineID         core.PipelineID             `json:"pipeline_id"`
	ParentInstanceID   *core.PipelineInstanceID    `json:"parent_instance_id,omitempty"`
	ParentTransitionID string                      `json:"parent_transition_id,omitempty"`
	InstanceKey        string                      `json:"instance_key,omitempty"`
	Status             string                      `json:"status"`
	DefaultCollapsed   bool                        `json:"default_collapsed"`
	Tasks              []pipelineWorkspaceTaskView `json:"tasks"`
}

type pipelineWorkspaceTaskView struct {
	TaskID  core.TaskID  `json:"task_id"`
	StageID core.StageID `json:"stage_id"`
	Op      string       `json:"op"`
	Status  string       `json:"status"`
}

type pipelineWorkspaceEdgeView struct {
	FromInstanceID core.PipelineInstanceID `json:"from_instance_id"`
	ToInstanceID   core.PipelineInstanceID `json:"to_instance_id"`
	Kind           string                  `json:"kind"`
	Label          string                  `json:"label,omitempty"`
}

type pipelineWorkspaceMainNodeView struct {
	NodeID                  string      `json:"node_id"`
	NodeKind                string      `json:"node_kind,omitempty"`
	StageID                 string      `json:"stage_id"`
	Label                   string      `json:"label"`
	Status                  string      `json:"status"`
	TaskID                  core.TaskID `json:"task_id,omitempty"`
	ChildPipelineInstanceID string      `json:"child_pipeline_instance_id,omitempty"`
}

type pipelineWorkspaceMainEdgeView struct {
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
	Kind       string `json:"kind"`
	Label      string `json:"label,omitempty"`
}

type pluginRegistryStateView struct {
	Handlers  []pluginHandlerStateView  `json:"handlers"`
	Ops       []pluginOpStateView       `json:"ops"`
	Roles     []pluginRoleStateView     `json:"roles"`
	Pipelines []pluginPipelineStateView `json:"pipelines"`
}

type pluginHandlerStateView struct {
	HandlerID       string `json:"handler_id"`
	ExecutionDriver string `json:"execution_driver,omitempty"`
	ImplRef         string `json:"impl_ref,omitempty"`
}

type pluginOpStateView struct {
	OpID    string `json:"op_id"`
	Role    string `json:"role"`
	Op      string `json:"op"`
	ImplRef string `json:"impl_ref,omitempty"`
}

type pluginRoleStateView struct {
	RoleID          string `json:"role_id"`
	ExecutionDriver string `json:"execution_driver,omitempty"`
	DriverRef       string `json:"driver_ref,omitempty"`
	InteractionMode string `json:"interaction_mode,omitempty"`
}

type pluginPipelineStateView struct {
	PipelineID string `json:"pipeline_id"`
	Name       string `json:"name,omitempty"`
}

type pluginValidationResultView struct {
	JobID              string    `json:"job_id"`
	SourceType         string    `json:"source_type"`
	Status             string    `json:"status"`
	FileName           string    `json:"file_name"`
	Errors             []string  `json:"errors,omitempty"`
	ActivatedHandlers  []string  `json:"activated_handlers,omitempty"`
	ActivatedOps       []string  `json:"activated_ops,omitempty"`
	ActivatedRoles     []string  `json:"activated_roles,omitempty"`
	ActivatedPipelines []string  `json:"activated_pipelines,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type createRunRequest struct {
	RunID            core.RunID      `json:"run_id,omitempty"`
	DemandSummary    string          `json:"demand_summary"`
	ProjectID        string          `json:"project_id,omitempty"`
	PipelineID       core.PipelineID `json:"pipeline_id,omitempty"`
	TargetRepo       string          `json:"target_repo,omitempty"`
	MainBranch       string          `json:"main_branch,omitempty"`
	BaseRef          string          `json:"base_ref,omitempty"`
	ModelProvider    string          `json:"model_provider,omitempty"`
	ModelName        string          `json:"model_name,omitempty"`
	APIKey           string          `json:"api_key,omitempty"`
	BaseURL          string          `json:"base_url,omitempty"`
	APIStyle         string          `json:"api_style,omitempty"`
	StartImmediately bool            `json:"start_immediately,omitempty"`
}

type taskView struct {
	TaskID             core.TaskID             `json:"task_id"`
	RunID              core.RunID              `json:"run_id"`
	PipelineInstanceID core.PipelineInstanceID `json:"pipeline_instance_id,omitempty"`
	StageID            core.StageID            `json:"stage_id"`
	AgentRole          core.AgentRole          `json:"agent_role"`
	AgentID            core.AgentID            `json:"agent_id"`
	Op                 string                  `json:"op"`
	Status             core.TaskStatus         `json:"status"`
	Result             core.TaskResultCode     `json:"result,omitempty"`
	ErrorMessage       string                  `json:"error_message,omitempty"`
	InputBagIDs        []string                `json:"input_bag_ids,omitempty"`
	OutputBagIDs       []string                `json:"output_bag_ids,omitempty"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

type eventView struct {
	EventID     string       `json:"event_id"`
	RunID       core.RunID   `json:"run_id"`
	TaskID      core.TaskID  `json:"task_id,omitempty"`
	AgentID     core.AgentID `json:"agent_id,omitempty"`
	Type        string       `json:"type"`
	Message     string       `json:"message"`
	PayloadJSON string       `json:"payload_json,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

type artifactView struct {
	ID        string       `json:"id"`
	RunID     core.RunID   `json:"run_id"`
	TaskID    core.TaskID  `json:"task_id"`
	AgentID   core.AgentID `json:"agent_id"`
	Kind      string       `json:"kind"`
	URI       string       `json:"uri"`
	CreatedAt time.Time    `json:"created_at"`
}

type nodeDetailView struct {
	Task      taskView                `json:"task"`
	Snapshot  *doujiagit.SnapshotView `json:"snapshot,omitempty"`
	Events    []eventView             `json:"events"`
	Artifacts []artifactView          `json:"artifacts"`
}

type artifactContentView struct {
	ArtifactID  string `json:"artifact_id,omitempty"`
	URI         string `json:"uri"`
	ContentType string `json:"content_type"`
	Text        string `json:"text,omitempty"`
	Size        int    `json:"size"`
	Truncated   bool   `json:"truncated"`
}

type checkpointView struct {
	CheckpointID string              `json:"checkpoint_id"`
	TaskID       core.TaskID         `json:"task_id"`
	RunID        core.RunID          `json:"run_id"`
	StageID      core.StageID        `json:"stage_id"`
	Op           string              `json:"op"`
	Status       core.TaskStatus     `json:"status"`
	Result       core.TaskResultCode `json:"result,omitempty"`
	AgentRole    core.AgentRole      `json:"agent_role"`
	AgentID      core.AgentID        `json:"agent_id"`
	Title        string              `json:"title"`
	Summary      string              `json:"summary,omitempty"`
	Artifacts    []artifactView      `json:"artifact_refs,omitempty"`
	InputBagIDs  []string            `json:"input_bag_ids,omitempty"`
	OutputBagIDs []string            `json:"output_bag_ids,omitempty"`
	CanApprove   bool                `json:"can_approve"`
	CanReject    bool                `json:"can_reject"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type taskFeedbackRequest struct {
	Result       core.TaskResultCode        `json:"result"`
	Message      string                     `json:"message,omitempty"`
	AgentID      core.AgentID               `json:"agent_id,omitempty"`
	Op           string                     `json:"op,omitempty"`
	ArtifactURIs []string                   `json:"artifact_uris,omitempty"`
	InputBagIDs  []string                   `json:"input_bag_ids,omitempty"`
	OutputBagIDs []string                   `json:"output_bag_ids,omitempty"`
	Outputs      []core.AgentOutput         `json:"outputs,omitempty"`
	ProducedBags []core.ProducedBagManifest `json:"produced_bags,omitempty"`
	Control      []core.Control             `json:"control,omitempty"`
	Commit       *core.CommitReceipt        `json:"commit,omitempty"`
}

type approveCheckpointRequest struct {
	Comment string `json:"comment,omitempty"`
}

type rejectCheckpointRequest struct {
	Reason string `json:"reason"`
	Mode   string `json:"mode,omitempty"`
}

type resumeFromRefRequest struct {
	RefName string `json:"ref_name,omitempty"`
}

type previewHandlerRequest struct {
	Args finalResultOpenRequest `json:"args"`
}

type finalResultOpenRequest struct {
	BagID       string `json:"bag_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
}

type finalResultOpenResult struct {
	Status      string `json:"status"`
	PreviewURL  string `json:"preview_url"`
	ContainerID string `json:"container_id,omitempty"`
	BagID       string `json:"bag_id,omitempty"`
	Message     string `json:"message,omitempty"`
}

type finalResultAsset struct {
	ContainerID string
	Path        string
}

type continueAcceptanceRequest struct {
	SelectedItemIDs []string `json:"selected_item_ids,omitempty"`
	FreeformText    string   `json:"freeform_text,omitempty"`
}

type postSessionMessageRequest struct {
	Content     string `json:"content"`
	MessageType string `json:"message_type,omitempty"`
}

type createProjectRequest struct {
	Name string `json:"name"`
	LLM  struct {
		Provider string `json:"provider,omitempty"`
		Model    string `json:"model,omitempty"`
		APIKey   string `json:"api_key,omitempty"`
		BaseURL  string `json:"base_url,omitempty"`
		APIStyle string `json:"api_style,omitempty"`
	} `json:"llm"`
}

type requirementSummaryConfirmRequest struct {
	Action string `json:"action"`
}

type confirmRequirementsRequest struct{}

type createImprovementItemRequest struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Source string `json:"source,omitempty"`
}

type patchImprovementItemRequest struct {
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
	Source string `json:"source,omitempty"`
	Status string `json:"status,omitempty"`
}

func (b *Bootstrap) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/", b.handleAPI)
}

func (b *Bootstrap) handleAPI(w http.ResponseWriter, r *http.Request) {
	setAPIHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	parts := splitAPIPath(path)

	if len(parts) == 1 && parts[0] == "health" {
		requireMethod(w, r, http.MethodGet, func() {
			writeAPIJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		return
	}
	if len(parts) == 1 && parts[0] == "runs" {
		switch r.Method {
		case http.MethodGet:
			b.handleListRuns(w, r)
		case http.MethodPost:
			b.handleCreateRun(w, r)
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 1 && parts[0] == "projects" {
		switch r.Method {
		case http.MethodGet:
			b.handleListProjects(w, r)
		case http.MethodPost:
			b.handleCreateProject(w, r)
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 1 && parts[0] == "demo-runs" {
		requireMethod(w, r, http.MethodGet, func() {
			b.handleListRuns(w, r)
		})
		return
	}
	if len(parts) == 1 && parts[0] == "openapi.json" {
		requireMethod(w, r, http.MethodGet, func() {
			b.handleOpenAPI(w, r)
		})
		return
	}
	if len(parts) >= 2 && parts[0] == "runs" {
		runID := core.RunID(parts[1])
		switch {
		case len(parts) == 2:
			requireMethod(w, r, http.MethodGet, func() {
				b.handleGetRun(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "start":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleStartRun(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "tasks":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleListTasks(w, r, runID)
			})
		case len(parts) == 4 && parts[2] == "session" && parts[3] == "messages":
			switch r.Method {
			case http.MethodGet:
				b.handleListSessionMessages(w, r, runID)
			case http.MethodPost:
				b.handlePostSessionMessage(w, r, runID)
			default:
				writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case len(parts) == 5 && parts[2] == "session" && parts[3] == "messages" && parts[4] == "stream":
			requireMethod(w, r, http.MethodPost, func() {
				b.handlePostSessionMessageStream(w, r, runID)
			})
		case len(parts) == 6 && parts[2] == "session" && parts[3] == "messages" && parts[5] == "confirm":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleConfirmRequirementSummary(w, r, runID, parts[4])
			})
		case len(parts) == 4 && parts[2] == "requirements" && parts[3] == "confirm":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleConfirmRequirements(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "improvement-items":
			switch r.Method {
			case http.MethodGet:
				b.handleListImprovementItems(w, r, runID)
			case http.MethodPost:
				b.handleCreateImprovementItem(w, r, runID)
			default:
				writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case len(parts) == 4 && parts[2] == "improvement-items":
			switch r.Method {
			case http.MethodPatch:
				b.handlePatchImprovementItem(w, r, runID, parts[3])
			case http.MethodDelete:
				b.handleDeleteImprovementItem(w, r, runID, parts[3])
			default:
				writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case len(parts) == 3 && parts[2] == "iterations":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleListIterations(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "workspace-overview":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleWorkspaceOverview(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "pipeline-workspace":
			requireMethod(w, r, http.MethodGet, func() {
				b.handlePipelineWorkspace(w, r, runID)
			})
		case len(parts) == 5 && parts[2] == "preview" && parts[3] == "handlers":
			requireMethod(w, r, http.MethodPost, func() {
				b.handlePreviewHandler(w, r, runID, parts[4])
			})
		case len(parts) >= 5 && parts[2] == "preview" && parts[3] == "final-result":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleFinalResultAsset(w, r, runID, parts[4:])
			})
		case len(parts) == 3 && parts[2] == "git-branches":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleGitBranches(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "acceptance-checkpoint":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleGetAcceptanceCheckpoint(w, r, runID)
			})
		case len(parts) == 4 && parts[2] == "acceptance-checkpoint" && parts[3] == "approve":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleApproveAcceptanceCheckpoint(w, r, runID)
			})
		case len(parts) == 4 && parts[2] == "acceptance-checkpoint" && parts[3] == "continue":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleContinueAcceptanceCheckpoint(w, r, runID)
			})
		case len(parts) == 5 && parts[2] == "tasks" && parts[4] == "feedback":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleTaskFeedback(w, r, runID, core.TaskID(parts[3]))
			})
		case len(parts) == 3 && parts[2] == "checkpoints":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleListCheckpoints(w, r, runID)
			})
		case len(parts) == 4 && parts[2] == "checkpoints":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleGetCheckpoint(w, r, runID, core.TaskID(parts[3]))
			})
		case len(parts) == 5 && parts[2] == "checkpoints" && parts[4] == "approve":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleApproveCheckpoint(w, r, runID, core.TaskID(parts[3]))
			})
		case len(parts) == 5 && parts[2] == "checkpoints" && parts[4] == "reject":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleRejectCheckpoint(w, r, runID, core.TaskID(parts[3]))
			})
		case len(parts) == 3 && parts[2] == "resume-from-ref":
			requireMethod(w, r, http.MethodPost, func() {
				b.handleResumeFromRef(w, r, runID)
			})
		case len(parts) == 5 && parts[2] == "artifacts" && parts[4] == "content":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleArtifactContent(w, r, runID, parts[3])
			})
		case len(parts) == 3 && parts[2] == "events":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleListEvents(w, r, runID)
			})
		case len(parts) == 3 && parts[2] == "pipeline-graph":
			requireMethod(w, r, http.MethodGet, func() {
				b.handlePipelineGraph(w, r, runID)
			})
		case len(parts) == 4 && parts[2] == "nodes":
			requireMethod(w, r, http.MethodGet, func() {
				b.handleNodeDetail(w, r, runID, core.TaskID(parts[3]))
			})
		default:
			writeAPIError(w, http.StatusNotFound, "not found")
		}
		return
	}
	if len(parts) >= 2 && parts[0] == "plugins" {
		switch {
		case len(parts) == 2 && parts[1] == "registry-state":
			requireMethod(w, r, http.MethodGet, func() {
				b.handlePluginRegistryState(w, r)
			})
		case len(parts) == 2 && parts[1] == "upload-pack":
			requireMethod(w, r, http.MethodPost, func() {
				b.handlePluginUploadPack(w, r)
			})
		case len(parts) == 2 && parts[1] == "upload-pipeline":
			requireMethod(w, r, http.MethodPost, func() {
				b.handlePluginUploadPipeline(w, r)
			})
		case len(parts) == 3 && parts[1] == "validations":
			requireMethod(w, r, http.MethodGet, func() {
				b.handlePluginValidationResult(w, r, parts[2])
			})
		default:
			writeAPIError(w, http.StatusNotFound, "not found")
		}
		return
	}
	writeAPIError(w, http.StatusNotFound, "not found")
}

func (b *Bootstrap) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := b.Internals.RunRepository.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]runView, 0, len(runs))
	for _, run := range runs {
		items = append(items, toRunView(run))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if b.Internals.ProjectRepository == nil {
		writeAPIJSON(w, http.StatusOK, map[string]any{"items": []projectView{}})
		return
	}
	projects, err := b.Internals.ProjectRepository.List(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]projectView, 0, len(projects))
	for _, project := range projects {
		items = append(items, toProjectView(project))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if b.Internals.ProjectRepository == nil {
		writeAPIError(w, http.StatusNotImplemented, "project repository is unavailable")
		return
	}
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "project name is required")
		return
	}
	now := time.Now().UTC()
	project := repo.ProjectRecord{
		ProjectID: "project_" + randomHex(6),
		Name:      name,
		LLM: core.LLMRunConfig{
			Provider: strings.TrimSpace(req.LLM.Provider),
			Model:    strings.TrimSpace(req.LLM.Model),
			APIKey:   strings.TrimSpace(req.LLM.APIKey),
			BaseURL:  strings.TrimSpace(req.LLM.BaseURL),
			APIStyle: strings.TrimSpace(req.LLM.APIStyle),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := b.Internals.ProjectRepository.Create(r.Context(), project); err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusCreated, toProjectView(project))
}

func (b *Bootstrap) handleGetRun(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, toRunView(run))
}

func (b *Bootstrap) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(string(req.RunID)) == "" {
		req.RunID = core.RunID("run_" + randomHex(6))
	}
	if strings.TrimSpace(string(req.PipelineID)) == "" {
		req.PipelineID = pipeline.PipelineIDPhaseTwo
	}
	var project repo.ProjectRecord
	var hasProject bool
	if strings.TrimSpace(req.ProjectID) != "" {
		if b.Internals.ProjectRepository == nil {
			writeAPIError(w, http.StatusNotImplemented, "project repository is unavailable")
			return
		}
		var err error
		project, err = b.Internals.ProjectRepository.Get(r.Context(), strings.TrimSpace(req.ProjectID))
		if err != nil {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		hasProject = true
	}
	config := core.RunConfig{Delivery: core.DeliveryConfig{
		MaxCoderAgents:           2,
		MaxTesterAgents:          2,
		RequireTesterPerModule:   true,
		AllowParallelWork:        true,
		GlobalTestTimeoutSeconds: 60,
		Git: core.GitRunConfig{
			RepoURL:    req.TargetRepo,
			MainBranch: defaultString(req.MainBranch, "main"),
			BaseRef:    req.BaseRef,
		},
	}}
	if hasProject {
		config.LLM = project.LLM
	}
	if hasLLMCreateConfig(req) {
		config.LLM = core.LLMRunConfig{
			Provider: defaultString(req.ModelProvider, "openai"),
			Model:    req.ModelName,
			APIKey:   req.APIKey,
			BaseURL:  req.BaseURL,
			APIStyle: req.APIStyle,
		}
	}
	if err := b.Modules.RunManager.CreateRun(r.Context(), req.RunID, req.PipelineID, config); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hasProject {
		run, err := b.Internals.RunRepository.Get(r.Context(), req.RunID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		run.ProjectID = project.ProjectID
		run.UpdatedAt = time.Now().UTC()
		if err := b.Internals.RunRepository.Update(r.Context(), run); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.StartImmediately {
		if err := b.Modules.RunManager.StartRun(r.Context(), req.RunID); err != nil {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), req.RunID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusCreated, toRunView(run))
}

func (b *Bootstrap) seedInitialRequirement(ctx context.Context, runID core.RunID, demandSummary string) error {
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return err
	}
	task, err := b.Internals.TaskRepository.Get(ctx, runID, "ceo_write_requirement")
	if err != nil {
		return err
	}
	if task.Status == core.TaskStatusDone || task.Status == core.TaskStatusFailed {
		return nil
	}

	outputRef := filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "ceo", "artifacts", "requirement", "requirement_v1.md"))
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: filepath.Join(run.ProjectDir, "agents", "ceo"),
	}
	if err := store.Write(ctx, outputRef, []byte(strings.TrimSpace(demandSummary)+"\n")); err != nil {
		return err
	}
	committer := runtime.NewDoujiaGitOutputCommitter(b.Internals.DoujiaGitRepository, store)
	feedback, err := runtime.CommitAgentFeedbackOutputs(ctx, committer, runID, "ceo", core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    "ceo_write_requirement",
		AgentID:   "ceo",
		Op:        core.TaskOpWritePlan,
	}, core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       "ceo_write_requirement",
		AgentID:      "ceo",
		Op:           core.TaskOpWritePlan,
		ArtifactURIs: []string{outputRef},
		Result:       core.TaskResultCodeOK,
		Outputs: []core.AgentOutput{
			{LogicalKey: "requirement", ObjectType: "markdown", Status: "produced", ArtifactURI: outputRef},
		},
		ProducedBags: []core.ProducedBagManifest{
			{
				Name: "requirement",
				Members: []core.ProducedBagMember{
					{LogicalKey: "requirement"},
				},
			},
		},
	})
	if err != nil {
		return err
	}
	return b.Internals.Orchestrator.OnFeedback(ctx, feedback)
}

func (b *Bootstrap) handleStartRun(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	if err := b.Modules.RunManager.StartRun(r.Context(), runID); err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	b.handleGetRun(w, r, runID)
}

func (b *Bootstrap) handleListTasks(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	tasks, err := b.Internals.TaskRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]taskView, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, toTaskView(task))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handleListSessionMessages(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	if b.Internals.SessionMessageRepository == nil {
		writeAPIJSON(w, http.StatusOK, map[string]any{"items": []sessionMessageView{}})
		return
	}
	messages, err := b.Internals.SessionMessageRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]sessionMessageView, 0, len(messages))
	for _, message := range messages {
		items = append(items, toSessionMessageView(message))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handlePostSessionMessage(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req postSessionMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeAPIError(w, http.StatusBadRequest, "content is required")
		return
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	project, err := b.projectForRun(r.Context(), run)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	messageType := defaultString(strings.TrimSpace(req.MessageType), "chat")
	userMessage := repo.SessionMessageRecord{
		ID:          "message_" + randomHex(6),
		RunID:       runID,
		IterationNo: max(1, run.CurrentIterationNo),
		Role:        "user",
		MessageType: messageType,
		Content:     content,
		CreatedAt:   now,
	}
	if err := b.Internals.SessionMessageRepository.Create(r.Context(), userMessage); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := b.Internals.SessionMessageRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var improvementItems []repo.ImprovementItemRecord
	if b.Internals.ImprovementItemRepository != nil {
		improvementItems, err = b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	progress := currentRunProgress(run, r.Context(), b.Internals.TaskRepository)
	memorySummary := latestConversationMemorySummary(r.Context(), runID, b.Internals.SessionArtifactRepository)
	responseMessages, artifacts, err := b.generateDoujiaSessionResponse(r.Context(), run, project, content, now, history, improvementItems, progress, memorySummary)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	if memoryArtifact := buildConversationMemoryArtifact(run, history, improvementItems, progress, now); memoryArtifact.ID != "" {
		artifacts = append(artifacts, memoryArtifact)
	}
	for _, response := range responseMessages {
		if err := b.Internals.SessionMessageRepository.Create(r.Context(), response); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if b.Internals.SessionArtifactRepository != nil {
		for _, artifact := range artifacts {
			_ = b.Internals.SessionArtifactRepository.Create(r.Context(), artifact)
		}
	}
	writeAPIJSON(w, http.StatusAccepted, map[string]any{
		"accepted":     true,
		"message_id":   userMessage.ID,
		"iteration_no": userMessage.IterationNo,
		"message_type": userMessage.MessageType,
	})
}

func (b *Bootstrap) handlePostSessionMessageStream(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req postSessionMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeAPIError(w, http.StatusBadRequest, "content is required")
		return
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	project, err := b.projectForRun(r.Context(), run)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	now := time.Now().UTC()
	messageType := defaultString(strings.TrimSpace(req.MessageType), "chat")
	userMessage := repo.SessionMessageRecord{
		ID:          "message_" + randomHex(6),
		RunID:       runID,
		IterationNo: max(1, run.CurrentIterationNo),
		Role:        "user",
		MessageType: messageType,
		Content:     content,
		CreatedAt:   now,
	}
	if err := b.Internals.SessionMessageRepository.Create(r.Context(), userMessage); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := b.Internals.SessionMessageRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var improvementItems []repo.ImprovementItemRecord
	if b.Internals.ImprovementItemRepository != nil {
		improvementItems, err = b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	progress := currentRunProgress(run, r.Context(), b.Internals.TaskRepository)
	memorySummary := latestConversationMemorySummary(r.Context(), runID, b.Internals.SessionArtifactRepository)
	adapter := b.doujiaLLMFactory(project.LLM)
	if adapter == nil {
		writeAPIError(w, http.StatusBadGateway, "llm adapter is unavailable")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	request := buildDoujiaSessionChatRequest(run, content, history, improvementItems, progress, memorySummary)
	var raw strings.Builder
	emittedReply := ""
	onRawDelta := func(delta string) error {
		raw.WriteString(delta)
		reply, ok := extractJSONStringField(raw.String(), "reply")
		if !ok || len(reply) <= len(emittedReply) {
			return nil
		}
		next := reply[len(emittedReply):]
		emittedReply = reply
		return writeSSEEvent(w, flusher, "delta", map[string]string{"delta": next})
	}

	var resp llm.ChatResponse
	if streaming, ok := adapter.(llm.StreamingAdapter); ok {
		resp, err = streaming.StreamChat(r.Context(), request, onRawDelta)
	} else {
		resp, err = adapter.Chat(r.Context(), request)
		if err == nil {
			raw.WriteString(resp.Message.Content)
		}
	}
	if err != nil {
		_ = writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}
	rawContent := strings.TrimSpace(firstNonEmpty(resp.Message.Content, raw.String()))
	reply, summary, err := parseDoujiaSessionReply(rawContent)
	if err != nil {
		_ = writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}
	if emittedReply == "" && strings.TrimSpace(reply) != "" {
		emittedReply = reply
		if err := writeSSEEvent(w, flusher, "delta", map[string]string{"delta": reply}); err != nil {
			return
		}
	}
	responseMessages, artifacts := buildDoujiaSessionRecords(run, now, reply, summary)
	if memoryArtifact := buildConversationMemoryArtifact(run, history, improvementItems, progress, now); memoryArtifact.ID != "" {
		artifacts = append(artifacts, memoryArtifact)
	}
	for _, response := range responseMessages {
		if err := b.Internals.SessionMessageRepository.Create(r.Context(), response); err != nil {
			_ = writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
			return
		}
	}
	if b.Internals.SessionArtifactRepository != nil {
		for _, artifact := range artifacts {
			_ = b.Internals.SessionArtifactRepository.Create(r.Context(), artifact)
		}
	}
	_ = writeSSEEvent(w, flusher, "done", map[string]any{
		"accepted":     true,
		"message_id":   userMessage.ID,
		"iteration_no": userMessage.IterationNo,
		"message_type": userMessage.MessageType,
	})
}

func (b *Bootstrap) handleConfirmRequirementSummary(w http.ResponseWriter, r *http.Request, runID core.RunID, messageID string) {
	var req requirementSummaryConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(strings.ToLower(req.Action)) != "accept" {
		writeAPIError(w, http.StatusBadRequest, "only accept action is supported")
		return
	}
	if b.Internals.SessionMessageRepository == nil || b.Internals.ImprovementItemRepository == nil {
		writeAPIError(w, http.StatusNotImplemented, "session repositories are unavailable")
		return
	}
	messages, err := b.Internals.SessionMessageRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var summary *repo.SessionMessageRecord
	for _, message := range messages {
		if message.ID == messageID && message.MessageType == "requirement_summary" {
			copy := message
			summary = &copy
			break
		}
	}
	if summary == nil {
		writeAPIError(w, http.StatusNotFound, "requirement summary message not found")
		return
	}
	now := time.Now().UTC()
	existingItems, err := b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range existingItems {
		if item.Source != "requirement_pool_confirmed" {
			continue
		}
		if strings.TrimSpace(item.Detail) != strings.TrimSpace(summary.Content) {
			continue
		}
		if item.Status == "removed" {
			item.Status = "confirmed"
			item.UpdatedAt = now
			if err := b.Internals.ImprovementItemRepository.Update(r.Context(), item); err != nil {
				writeAPIError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{
			"confirmed": true,
			"item_id":   item.ItemID,
			"item":      toImprovementItemView(item),
		})
		return
	}
	item := repo.ImprovementItemRecord{
		ItemID:      "item_" + randomHex(6),
		RunID:       runID,
		IterationNo: summary.IterationNo,
		Title:       summarizeImprovementTitle(summary.Content),
		Detail:      summary.Content,
		Source:      "requirement_pool_confirmed",
		Status:      "confirmed",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := b.Internals.ImprovementItemRepository.Create(r.Context(), item); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	confirmMessage := repo.SessionMessageRecord{
		ID:          "message_doujia_" + randomHex(6),
		RunID:       runID,
		IterationNo: summary.IterationNo,
		Role:        "ceo",
		MessageType: "requirement_confirm",
		Content:     "已确认，这条需求已加入需求队列。",
		CreatedAt:   now,
	}
	if err := b.Internals.SessionMessageRepository.Create(r.Context(), confirmMessage); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"confirmed": true,
		"item_id":   item.ItemID,
		"item":      toImprovementItemView(item),
	})
}

func (b *Bootstrap) handleConfirmRequirements(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req confirmRequirementsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	task, err := b.Internals.TaskRepository.Get(r.Context(), runID, "ceo_write_requirement")
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	if task.Status != core.TaskStatusWaitingExternal {
		writeAPIError(w, http.StatusConflict, "ceo_write_requirement is not waiting for requirement confirmation")
		return
	}
	if b.Internals.ImprovementItemRepository == nil {
		writeAPIError(w, http.StatusNotImplemented, "improvement item repository is unavailable")
		return
	}
	items, err := b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pool := confirmedRequirementPool(items)
	if len(pool) == 0 {
		writeAPIError(w, http.StatusBadRequest, "at least one confirmed requirement item is required")
		return
	}
	history, err := b.Internals.SessionMessageRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	requirementPath := filepath.Join(run.ProjectDir, "ceo_requirement_context.md")
	requirementContent := buildRequirementDocumentFromPool(run, pool, history)
	if err := os.WriteFile(requirementPath, []byte(requirementContent), 0o644); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dispatch := core.TaskMetaData{
		Direction: core.TaskDirectionDispatch,
		RunID:     runID,
		TaskID:    task.ID,
		AgentID:   task.AgentID,
		Op:        task.Op,
		InputBundle: &core.AgentInputBundle{
			InputDir:      run.ProjectDir,
			OutputDir:     filepath.Join(run.ProjectDir, "agents", "ceo", "artifacts", "requirement"),
			OutputURIBase: filepath.ToSlash(filepath.Join("projects", string(runID), "agents", "ceo", "artifacts", "requirement")),
			Inputs: []core.InputArtifact{
				{
					LogicalKey:  agentcore.LKRequirement,
					Path:        requirementPath,
					ObjectType:  "markdown",
					ContentType: "text/markdown",
					Description: "Confirmed requirement context for ceo.write_plan.",
				},
			},
		},
		ArtifactURIs: []string{"ceo_requirement_context.md"},
	}
	if err := b.Modules.SessionRuntime.DispatchToSession(r.Context(), dispatch); err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	updatedRun, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, toRunView(updatedRun))
}

func (b *Bootstrap) handleListImprovementItems(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	if b.Internals.ImprovementItemRepository == nil {
		writeAPIJSON(w, http.StatusOK, map[string]any{"items": []improvementItemView{}})
		return
	}
	items, err := b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]improvementItemView, 0, len(items))
	for _, item := range items {
		views = append(views, improvementItemView{
			ItemID:      item.ItemID,
			RunID:       item.RunID,
			IterationNo: item.IterationNo,
			Title:       item.Title,
			Detail:      item.Detail,
			Source:      item.Source,
			Status:      item.Status,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (b *Bootstrap) handleCreateImprovementItem(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req createImprovementItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeAPIError(w, http.StatusBadRequest, "title is required")
		return
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	now := time.Now().UTC()
	record := repo.ImprovementItemRecord{
		ItemID:      "item_" + randomHex(6),
		RunID:       runID,
		IterationNo: max(1, run.CurrentIterationNo),
		Title:       strings.TrimSpace(req.Title),
		Detail:      strings.TrimSpace(req.Detail),
		Source:      defaultString(strings.TrimSpace(req.Source), "user"),
		Status:      "open",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := b.Internals.ImprovementItemRepository.Create(r.Context(), record); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusCreated, toImprovementItemView(record))
}

func (b *Bootstrap) handlePatchImprovementItem(w http.ResponseWriter, r *http.Request, runID core.RunID, itemID string) {
	var req patchImprovementItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	items, err := b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		if item.ItemID != itemID {
			continue
		}
		if strings.TrimSpace(req.Title) != "" {
			item.Title = strings.TrimSpace(req.Title)
		}
		if req.Detail != "" {
			item.Detail = strings.TrimSpace(req.Detail)
		}
		if strings.TrimSpace(req.Source) != "" {
			item.Source = strings.TrimSpace(req.Source)
		}
		if strings.TrimSpace(req.Status) != "" {
			item.Status = strings.TrimSpace(req.Status)
		}
		item.UpdatedAt = time.Now().UTC()
		if err := b.Internals.ImprovementItemRepository.Update(r.Context(), item); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, toImprovementItemView(item))
		return
	}
	writeAPIError(w, http.StatusNotFound, "improvement item not found")
}

func (b *Bootstrap) handleDeleteImprovementItem(w http.ResponseWriter, r *http.Request, runID core.RunID, itemID string) {
	if b.Internals.ImprovementItemRepository == nil {
		writeAPIError(w, http.StatusNotImplemented, "improvement item repository is unavailable")
		return
	}
	items, err := b.Internals.ImprovementItemRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		if item.ItemID != itemID {
			continue
		}
		if item.Source != "requirement_pool_confirmed" {
			writeAPIError(w, http.StatusConflict, "only confirmed requirement items can be removed from the requirement pool")
			return
		}
		item.Status = "removed"
		item.UpdatedAt = time.Now().UTC()
		if err := b.Internals.ImprovementItemRepository.Update(r.Context(), item); err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, map[string]any{
			"removed": true,
			"item":    toImprovementItemView(item),
		})
		return
	}
	writeAPIError(w, http.StatusNotFound, "improvement item not found")
}

func (b *Bootstrap) handleListIterations(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	iterations, err := b.Internals.RunIterationRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]runIterationView, 0, len(iterations))
	for _, iteration := range iterations {
		items = append(items, toRunIterationView(iteration))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handleWorkspaceOverview(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	view, err := b.buildWorkspaceOverview(r.Context(), runID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, view)
}

func (b *Bootstrap) handlePipelineWorkspace(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	view, err := b.buildPipelineWorkspace(r.Context(), runID, strings.TrimSpace(r.URL.Query().Get("focus_instance_id")))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, view)
}

func (b *Bootstrap) handlePreviewHandler(w http.ResponseWriter, r *http.Request, runID core.RunID, handlerName string) {
	switch handlerName {
	case "final_result_open":
		b.handleFinalResultOpen(w, r, runID)
	default:
		writeAPIError(w, http.StatusNotFound, "preview handler not found")
	}
}

func (b *Bootstrap) handleFinalResultOpen(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req previewHandlerRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	args := req.Args
	args.BagID = strings.TrimSpace(args.BagID)
	args.ContainerID = strings.TrimSpace(args.ContainerID)
	args.PreviewURL = strings.TrimSpace(args.PreviewURL)
	if args.PreviewURL != "" {
		writeAPIJSON(w, http.StatusOK, finalResultOpenResult{
			Status:      "ready",
			PreviewURL:  args.PreviewURL,
			ContainerID: args.ContainerID,
			BagID:       args.BagID,
		})
		return
	}
	if args.ContainerID == "" && args.BagID != "" {
		containerID, err := b.findContainerIDForBag(r.Context(), runID, args.BagID)
		if err != nil {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		args.ContainerID = containerID
	}
	if args.ContainerID == "" {
		writeAPIError(w, http.StatusBadRequest, "container_id or preview_url is required")
		return
	}
	previewURL, err := openDockerContainerPreviewURL(r.Context(), args.ContainerID)
	if err != nil {
		previewURL = finalResultAssetURLWithVersion(runID, args.ContainerID, "index.html", finalResultContainerVersion(r.Context(), args.ContainerID))
	}
	writeAPIJSON(w, http.StatusOK, finalResultOpenResult{
		Status:      "ready",
		PreviewURL:  previewURL,
		ContainerID: args.ContainerID,
		BagID:       args.BagID,
	})
}

func (b *Bootstrap) handleFinalResultAsset(w http.ResponseWriter, r *http.Request, runID core.RunID, parts []string) {
	asset, err := finalResultAssetFromPath(parts)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := runDockerCommand(r.Context(), "start", asset.ContainerID); err != nil {
		writeAPIError(w, http.StatusConflict, fmt.Sprintf("container_start_failed: %v", err))
		return
	}
	content, err := readContainerFinalResultAsset(r.Context(), asset)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(pathpkg.Ext(asset.Path)))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	if asset.Path == "index.html" {
		content = []byte(rewriteFinalResultHTML(string(content), runID, asset.ContainerID))
		contentType = "text/html; charset=utf-8"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (b *Bootstrap) handleGetAcceptanceCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, acceptanceCheckpointView{
		RunID:                    run.ID,
		CheckpointTaskID:         run.LatestAcceptanceCheckpointTaskID,
		CurrentIterationNo:       run.CurrentIterationNo,
		LatestDeliveryFrontierID: run.LatestDeliveryFrontierID,
		Status:                   run.Status,
	})
}

func (b *Bootstrap) handlePluginRegistryState(w http.ResponseWriter, r *http.Request) {
	if b.Internals.PluginRegistry == nil {
		writeAPIJSON(w, http.StatusOK, pluginRegistryStateView{})
		return
	}
	state := b.Internals.PluginRegistry.State()
	writeAPIJSON(w, http.StatusOK, toPluginRegistryStateView(state))
}

func (b *Bootstrap) handlePluginUploadPack(w http.ResponseWriter, r *http.Request) {
	fileName, raw, err := parseUploadFile(r, "file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := b.Internals.PluginRegistry.UploadPluginPack(r.Context(), fileName, raw)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusAccepted, map[string]any{"job_id": result.JobID})
}

func (b *Bootstrap) handlePluginUploadPipeline(w http.ResponseWriter, r *http.Request) {
	fileName, raw, err := parseUploadFile(r, "file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := b.Internals.PluginRegistry.UploadPipelineJSON(r.Context(), fileName, raw)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusAccepted, map[string]any{"job_id": result.JobID})
}

func (b *Bootstrap) handlePluginValidationResult(w http.ResponseWriter, r *http.Request, jobID string) {
	result, err := b.PluginValidationResult(r.Context(), jobID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, toPluginValidationResultView(result))
}

func (b *Bootstrap) handleApproveAcceptanceCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req approveCheckpointRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	run, err := b.Modules.RunManager.ApproveAcceptance(r.Context(), runID, req.Comment)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, toRunView(run))
}

func (b *Bootstrap) handleContinueAcceptanceCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req continueAcceptanceRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	run, err := b.Modules.RunManager.ContinueIteration(r.Context(), runID, req.SelectedItemIDs, req.FreeformText)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, toRunView(run))
}

func (b *Bootstrap) handleListCheckpoints(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	tasks, err := b.Internals.TaskRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]checkpointView, 0)
	for _, task := range tasks {
		if !isCheckpointTask(task) {
			continue
		}
		checkpoint, err := b.toCheckpointView(r.Context(), task)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, checkpoint)
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *Bootstrap) handleGetCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID) {
	task, err := b.Internals.TaskRepository.Get(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	if !isCheckpointTask(task) {
		writeAPIError(w, http.StatusNotFound, "checkpoint not found")
		return
	}
	checkpoint, err := b.toCheckpointView(r.Context(), task)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, checkpoint)
}

func (b *Bootstrap) handleTaskFeedback(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID) {
	var req taskFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	checkpoint, ok := b.applyTaskFeedback(w, r, runID, taskID, req)
	if !ok {
		return
	}
	writeAPIJSON(w, http.StatusOK, checkpoint)
}

func (b *Bootstrap) handleApproveCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID) {
	var req approveCheckpointRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	checkpoint, ok := b.applyTaskFeedback(w, r, runID, taskID, taskFeedbackRequest{
		Result:  core.TaskResultCodeOK,
		Message: req.Comment,
	})
	if !ok {
		return
	}
	writeAPIJSON(w, http.StatusOK, checkpoint)
}

func (b *Bootstrap) handleRejectCheckpoint(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID) {
	var req rejectCheckpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeAPIError(w, http.StatusBadRequest, "reject reason is required")
		return
	}
	checkpoint, ok := b.applyTaskFeedback(w, r, runID, taskID, taskFeedbackRequest{
		Result:  rejectModeResult(req.Mode),
		Message: req.Reason,
	})
	if !ok {
		return
	}
	writeAPIJSON(w, http.StatusOK, checkpoint)
}

func (b *Bootstrap) handleResumeFromRef(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	var req resumeFromRefRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := b.Internals.Orchestrator.ResumeFromRef(r.Context(), runID, req.RefName)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, result)
}

func (b *Bootstrap) handleArtifactContent(w http.ResponseWriter, r *http.Request, runID core.RunID, artifactID string) {
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	artifacts, err := b.Internals.ArtifactRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var found repo.ArtifactRecord
	for _, item := range artifacts {
		if item.ID == artifactID {
			found = item
			break
		}
	}
	if found.ID == "" {
		writeAPIError(w, http.StatusNotFound, "artifact not found")
		return
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	raw, err := store.Read(r.Context(), found.URI)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	const maxTextBytes = 64 * 1024
	truncated := len(raw) > maxTextBytes
	text := raw
	if truncated {
		text = text[:maxTextBytes]
	}
	contentType := http.DetectContentType(raw)
	if strings.HasSuffix(strings.ToLower(found.URI), ".md") {
		contentType = "text/markdown; charset=utf-8"
	}
	writeAPIJSON(w, http.StatusOK, artifactContentView{
		ArtifactID:  found.ID,
		URI:         found.URI,
		ContentType: contentType,
		Text:        string(text),
		Size:        len(raw),
		Truncated:   truncated,
	})
}

func (b *Bootstrap) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"openapi": "3.0.3",
		"info": map[string]string{
			"title":   "Doujia DevFlow API",
			"version": "0.2.0",
		},
		"paths": map[string]any{
			"/api/health":                                       map[string]any{"get": map[string]string{"summary": "Health check"}},
			"/api/runs":                                         map[string]any{"get": map[string]string{"summary": "List runs"}, "post": map[string]string{"summary": "Create run"}},
			"/api/demo-runs":                                    map[string]any{"get": map[string]string{"summary": "List demo runs"}},
			"/api/runs/{run_id}":                                map[string]any{"get": map[string]string{"summary": "Get run"}},
			"/api/runs/{run_id}/start":                          map[string]any{"post": map[string]string{"summary": "Start run"}},
			"/api/runs/{run_id}/tasks":                          map[string]any{"get": map[string]string{"summary": "List tasks"}},
			"/api/runs/{run_id}/session/messages":               map[string]any{"get": map[string]string{"summary": "List session messages"}, "post": map[string]string{"summary": "Post session message"}},
			"/api/runs/{run_id}/improvement-items":              map[string]any{"get": map[string]string{"summary": "List improvement items"}, "post": map[string]string{"summary": "Create improvement item"}},
			"/api/runs/{run_id}/improvement-items/{item_id}":    map[string]any{"patch": map[string]string{"summary": "Patch improvement item"}},
			"/api/runs/{run_id}/iterations":                     map[string]any{"get": map[string]string{"summary": "List run iterations"}},
			"/api/runs/{run_id}/workspace-overview":             map[string]any{"get": map[string]string{"summary": "Get workspace overview"}},
			"/api/runs/{run_id}/pipeline-workspace":             map[string]any{"get": map[string]string{"summary": "Get pipeline workspace"}},
			"/api/runs/{run_id}/acceptance-checkpoint":          map[string]any{"get": map[string]string{"summary": "Get acceptance checkpoint"}},
			"/api/runs/{run_id}/acceptance-checkpoint/approve":  map[string]any{"post": map[string]string{"summary": "Approve acceptance checkpoint"}},
			"/api/runs/{run_id}/acceptance-checkpoint/continue": map[string]any{"post": map[string]string{"summary": "Continue to next iteration"}},
			"/api/runs/{run_id}/tasks/{task_id}/feedback":       map[string]any{"post": map[string]string{"summary": "Send task feedback"}},
			"/api/runs/{run_id}/events":                         map[string]any{"get": map[string]string{"summary": "List events"}},
			"/api/runs/{run_id}/pipeline-graph":                 map[string]any{"get": map[string]string{"summary": "Get DoujiaGit graph"}},
			"/api/runs/{run_id}/nodes/{task_id}":                map[string]any{"get": map[string]string{"summary": "Get node detail"}},
			"/api/runs/{run_id}/checkpoints":                    map[string]any{"get": map[string]string{"summary": "List checkpoints"}},
			"/api/runs/{run_id}/checkpoints/{task_id}":          map[string]any{"get": map[string]string{"summary": "Get checkpoint"}},
			"/api/runs/{run_id}/checkpoints/{task_id}/approve":  map[string]any{"post": map[string]string{"summary": "Approve checkpoint"}},
			"/api/runs/{run_id}/checkpoints/{task_id}/reject":   map[string]any{"post": map[string]string{"summary": "Reject checkpoint"}},
			"/api/runs/{run_id}/resume-from-ref":                map[string]any{"post": map[string]string{"summary": "Resume from DoujiaGit ref"}},
			"/api/runs/{run_id}/artifacts/{artifact_id}/content": map[string]any{
				"get": map[string]string{"summary": "Read artifact content"},
			},
			"/api/plugins/registry-state":       map[string]any{"get": map[string]string{"summary": "Get plugin registry state"}},
			"/api/plugins/upload-pack":          map[string]any{"post": map[string]string{"summary": "Upload plugin pack"}},
			"/api/plugins/upload-pipeline":      map[string]any{"post": map[string]string{"summary": "Upload standalone pipeline JSON"}},
			"/api/plugins/validations/{job_id}": map[string]any{"get": map[string]string{"summary": "Get plugin validation result"}},
		},
	})
}

func (b *Bootstrap) handleListEvents(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	events, err := b.Internals.EventRepository.ListByRun(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"items": toEventViews(events)})
}

func (b *Bootstrap) handlePipelineGraph(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	refName := strings.TrimSpace(r.URL.Query().Get("ref"))
	if refName == "" {
		refName = doujiagit.DefaultRefName
	}
	run, err := b.Internals.RunRepository.Get(r.Context(), runID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	graph, err := doujiagit.BuildRunGraph(r.Context(), b.Internals.DoujiaGitRepository, runID, refName)
	if err != nil {
		if isMissingDoujiaGitRef(err) {
			writeAPIJSON(w, http.StatusOK, emptyPipelineGraph(runID, refName, run.UpdatedAt))
			return
		}
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, graph)
}

func (b *Bootstrap) handleGitBranches(w http.ResponseWriter, r *http.Request, runID core.RunID) {
	view, err := b.buildGitBranchesView(r.Context(), runID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAPIJSON(w, http.StatusOK, view)
}

func (b *Bootstrap) buildGitBranchesView(ctx context.Context, runID core.RunID) (gitBranchesView, error) {
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return gitBranchesView{}, err
	}
	tasks, err := b.Internals.TaskRepository.ListByRun(ctx, runID)
	if err != nil {
		return gitBranchesView{}, err
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	view := gitBranchesView{
		RunID:          runID,
		ModuleBranches: make([]gitModuleBranchView, 0),
		Warnings:       make([]gitBranchesWarningView, 0),
	}
	for _, task := range tasks {
		snapshot, err := doujiagit.BuildTaskSnapshotDetail(ctx, b.Internals.DoujiaGitRepository, runID, task.ID)
		if err != nil {
			continue
		}
		for _, bag := range snapshot.OutputBags {
			for _, version := range bag.Versions {
				logicalKey := strings.TrimSpace(version.LogicalArtifact.LogicalKey)
				if logicalKey != "coder_branch" && logicalKey != "merged_main_branch" {
					continue
				}
				for _, object := range version.Objects {
					if strings.TrimSpace(object.StorageURI) == "" {
						view.Warnings = append(view.Warnings, gitBranchesWarningView{
							TaskID:     snapshot.TaskID,
							BagID:      bag.BagID,
							LogicalKey: logicalKey,
							Message:    "artifact object has no storage_uri",
						})
						continue
					}
					content, err := store.Read(ctx, object.StorageURI)
					if err != nil {
						view.Warnings = append(view.Warnings, gitBranchesWarningView{
							TaskID:     snapshot.TaskID,
							BagID:      bag.BagID,
							LogicalKey: logicalKey,
							Message:    err.Error(),
						})
						continue
					}
					switch logicalKey {
					case "coder_branch":
						var payload coderBranchArtifact
						if err := json.Unmarshal(content, &payload); err != nil {
							view.Warnings = append(view.Warnings, gitBranchesWarningView{TaskID: snapshot.TaskID, BagID: bag.BagID, LogicalKey: logicalKey, Message: err.Error()})
							continue
						}
						row := gitModuleBranchView{
							TaskID:       snapshot.TaskID,
							AgentID:      snapshot.AgentID,
							SnapshotID:   snapshot.SnapshotID,
							BagID:        bag.BagID,
							LogicalKey:   logicalKey,
							CreatedAt:    snapshot.CreatedAt,
							ModuleID:     payload.ModuleID,
							ModuleName:   payload.ModuleName,
							Branch:       payload.Branch,
							Commit:       payload.Commit,
							BaseBranch:   payload.BaseBranch,
							BaseCommit:   payload.BaseCommit,
							ContainerID:  payload.ContainerID,
							ChangedFiles: append([]string(nil), payload.ChangedFiles...),
							Result:       payload.Result,
							TestPassed:   payload.TestPassed,
						}
						view.ModuleBranches = append(view.ModuleBranches, row)
						if view.BaseBranch == "" {
							view.BaseBranch = row.BaseBranch
						}
						if view.BaseCommit == "" {
							view.BaseCommit = row.BaseCommit
						}
						if view.ContainerID == "" {
							view.ContainerID = row.ContainerID
						}
					case "merged_main_branch":
						var payload mergedMainBranchArtifact
						if err := json.Unmarshal(content, &payload); err != nil {
							view.Warnings = append(view.Warnings, gitBranchesWarningView{TaskID: snapshot.TaskID, BagID: bag.BagID, LogicalKey: logicalKey, Message: err.Error()})
							continue
						}
						merge := gitMergeView{
							TaskID:         snapshot.TaskID,
							AgentID:        snapshot.AgentID,
							SnapshotID:     snapshot.SnapshotID,
							BagID:          bag.BagID,
							LogicalKey:     logicalKey,
							CreatedAt:      snapshot.CreatedAt,
							BaseBranch:     payload.BaseBranch,
							BaseCommit:     payload.BaseCommit,
							ContainerID:    payload.ContainerID,
							MergedCommit:   payload.MergedCommit,
							AppliedCommits: append([]string(nil), payload.AppliedCommits...),
							Result:         payload.Result,
							Modules:        make([]gitModuleBranchView, 0, len(payload.Modules)),
						}
						for _, module := range payload.Modules {
							merge.Modules = append(merge.Modules, gitModuleBranchView{
								TaskID:      snapshot.TaskID,
								AgentID:     snapshot.AgentID,
								SnapshotID:  snapshot.SnapshotID,
								BagID:       bag.BagID,
								LogicalKey:  logicalKey,
								CreatedAt:   snapshot.CreatedAt,
								ModuleID:    module.ModuleID,
								ModuleName:  module.ModuleName,
								Branch:      module.Branch,
								Commit:      module.Commit,
								BaseBranch:  module.BaseBranch,
								BaseCommit:  module.BaseCommit,
								ContainerID: payload.ContainerID,
							})
						}
						view.Merge = &merge
						if view.BaseBranch == "" {
							view.BaseBranch = merge.BaseBranch
						}
						if view.BaseCommit == "" {
							view.BaseCommit = merge.BaseCommit
						}
						if view.ContainerID == "" {
							view.ContainerID = merge.ContainerID
						}
					}
				}
			}
		}
	}
	return view, nil
}

func (b *Bootstrap) handleNodeDetail(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID) {
	task, err := b.Internals.TaskRepository.Get(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}
	events, err := b.Internals.EventRepository.ListByTask(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	artifacts, err := b.Internals.ArtifactRepository.ListByTask(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var snapshot *doujiagit.SnapshotView
	view, err := doujiagit.BuildTaskSnapshotDetail(r.Context(), b.Internals.DoujiaGitRepository, runID, taskID)
	if err == nil {
		snapshot = &view
	}
	writeAPIJSON(w, http.StatusOK, nodeDetailView{
		Task:      toTaskView(task),
		Snapshot:  snapshot,
		Events:    toEventViews(events),
		Artifacts: toArtifactViews(artifacts),
	})
}

func toRunView(run repo.RunRecord) runView {
	return runView{
		RunID:                            run.ID,
		PipelineID:                       run.PipelineID,
		Status:                           run.Status,
		ProjectID:                        run.ProjectID,
		ProjectDir:                       run.ProjectDir,
		SessionID:                        run.SessionID,
		CurrentIterationNo:               run.CurrentIterationNo,
		LatestDeliveryFrontierID:         run.LatestDeliveryFrontierID,
		LatestAcceptanceCheckpointTaskID: run.LatestAcceptanceCheckpointTaskID,
		Config:                           run.Config,
		CreatedAt:                        run.CreatedAt,
		UpdatedAt:                        run.UpdatedAt,
	}
}

func toProjectView(project repo.ProjectRecord) projectView {
	return projectView{
		ProjectID: project.ProjectID,
		Name:      project.Name,
		LLM:       project.LLM,
		CreatedAt: project.CreatedAt,
		UpdatedAt: project.UpdatedAt,
	}
}

func toSessionMessageView(message repo.SessionMessageRecord) sessionMessageView {
	view := sessionMessageView{
		MessageID:   message.ID,
		RunID:       message.RunID,
		IterationNo: message.IterationNo,
		Role:        message.Role,
		MessageType: message.MessageType,
		Content:     message.Content,
		CreatedAt:   message.CreatedAt,
	}
	if message.MessageType == "requirement_summary" {
		view.Metadata = map[string]any{
			"kind":                 "requirement_summary",
			"confirm_action_path":  fmt.Sprintf("/api/runs/%s/session/messages/%s/confirm", message.RunID, message.ID),
			"confirm_action_label": "确认加入需求池",
			"revise_action_label":  "继续补充",
			"summary_key":          strings.TrimSpace(message.Content),
		}
	}
	return view
}

func toRunIterationView(iteration repo.RunIterationRecord) runIterationView {
	return runIterationView{
		RunID:                      iteration.RunID,
		IterationNo:                iteration.IterationNo,
		StartFrontierID:            iteration.StartFrontierID,
		DeliveryFrontierID:         iteration.DeliveryFrontierID,
		AcceptanceCheckpointTaskID: iteration.AcceptanceCheckpointTaskID,
		Status:                     iteration.Status,
		CreatedAt:                  iteration.CreatedAt,
		UpdatedAt:                  iteration.UpdatedAt,
	}
}

func toSessionArtifactView(artifact repo.SessionArtifactRecord) sessionArtifactView {
	return sessionArtifactView{
		ArtifactID:  artifact.ID,
		RunID:       artifact.RunID,
		IterationNo: artifact.IterationNo,
		Kind:        artifact.Kind,
		Title:       artifact.Title,
		Content:     artifact.Content,
		CreatedAt:   artifact.CreatedAt,
	}
}

func toImprovementItemView(item repo.ImprovementItemRecord) improvementItemView {
	return improvementItemView{
		ItemID:      item.ItemID,
		RunID:       item.RunID,
		IterationNo: item.IterationNo,
		Title:       item.Title,
		Detail:      item.Detail,
		Source:      item.Source,
		Status:      item.Status,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func toPluginRegistryStateView(state agentbootstrap.RegistryState) pluginRegistryStateView {
	view := pluginRegistryStateView{
		Handlers:  make([]pluginHandlerStateView, 0, len(state.Handlers)),
		Ops:       make([]pluginOpStateView, 0, len(state.Ops)),
		Roles:     make([]pluginRoleStateView, 0, len(state.Roles)),
		Pipelines: make([]pluginPipelineStateView, 0, len(state.Pipelines)),
	}
	for _, handler := range state.Handlers {
		view.Handlers = append(view.Handlers, pluginHandlerStateView{
			HandlerID:       handler.ID,
			ExecutionDriver: handler.ExecutionDriver,
			ImplRef:         handler.ImplRef,
		})
	}
	for _, op := range state.Ops {
		view.Ops = append(view.Ops, pluginOpStateView{
			OpID:    op.ID,
			Role:    op.Spec.Role,
			Op:      op.Spec.Op,
			ImplRef: op.ImplRef,
		})
	}
	for _, role := range state.Roles {
		view.Roles = append(view.Roles, pluginRoleStateView{
			RoleID:          role.ID,
			ExecutionDriver: role.ExecutionDriver,
			DriverRef:       role.DriverRef,
			InteractionMode: role.InteractionMode,
		})
	}
	for _, def := range state.Pipelines {
		view.Pipelines = append(view.Pipelines, pluginPipelineStateView{
			PipelineID: string(def.PipelineID),
			Name:       def.Name,
		})
	}
	return view
}

func toPluginValidationResultView(result agentbootstrap.ValidationResult) pluginValidationResultView {
	return pluginValidationResultView{
		JobID:              result.JobID,
		SourceType:         string(result.SourceType),
		Status:             string(result.Status),
		FileName:           result.FileName,
		Errors:             append([]string(nil), result.Errors...),
		ActivatedHandlers:  append([]string(nil), result.ActivatedHandlers...),
		ActivatedOps:       append([]string(nil), result.ActivatedOps...),
		ActivatedRoles:     append([]string(nil), result.ActivatedRoles...),
		ActivatedPipelines: append([]string(nil), result.ActivatedPipelines...),
		CreatedAt:          result.CreatedAt,
		UpdatedAt:          result.UpdatedAt,
	}
}

func (b *Bootstrap) applyTaskFeedback(w http.ResponseWriter, r *http.Request, runID core.RunID, taskID core.TaskID, req taskFeedbackRequest) (checkpointView, bool) {
	if req.Result == "" {
		writeAPIError(w, http.StatusBadRequest, "feedback result is required")
		return checkpointView{}, false
	}
	task, err := b.Internals.TaskRepository.Get(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return checkpointView{}, false
	}
	if task.Status == core.TaskStatusDone || task.Status == core.TaskStatusFailed {
		writeAPIError(w, http.StatusConflict, "task is already terminal")
		return checkpointView{}, false
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = task.AgentID
	}
	op := strings.TrimSpace(req.Op)
	if op == "" {
		op = task.Op
	}
	feedback := core.TaskMetaData{
		Direction:    core.TaskDirectionFeedback,
		RunID:        runID,
		TaskID:       taskID,
		AgentID:      agentID,
		Op:           op,
		ArtifactURIs: append([]string(nil), req.ArtifactURIs...),
		InputBagIDs:  append([]string(nil), req.InputBagIDs...),
		Result:       req.Result,
		Outputs:      append([]core.AgentOutput(nil), req.Outputs...),
		ProducedBags: append([]core.ProducedBagManifest(nil), req.ProducedBags...),
		Control:      append([]core.Control(nil), req.Control...),
		Commit:       req.Commit,
	}
	if err := b.Internals.Orchestrator.OnFeedback(r.Context(), feedback); err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return checkpointView{}, false
	}
	updated, err := b.Internals.TaskRepository.Get(r.Context(), runID, taskID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return checkpointView{}, false
	}
	checkpoint, err := b.toCheckpointView(r.Context(), updated)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return checkpointView{}, false
	}
	return checkpoint, true
}

func (b *Bootstrap) toCheckpointView(ctx context.Context, task repo.TaskRecord) (checkpointView, error) {
	artifacts, err := b.Internals.ArtifactRepository.ListByTask(ctx, task.RunID, task.ID)
	if err != nil {
		return checkpointView{}, err
	}
	actionable := task.Status == core.TaskStatusWaitingExternal
	title := strings.TrimSpace(task.Op)
	if title == "" {
		title = string(task.StageID)
	}
	return checkpointView{
		CheckpointID: string(task.ID),
		TaskID:       task.ID,
		RunID:        task.RunID,
		StageID:      task.StageID,
		Op:           task.Op,
		Status:       task.Status,
		Result:       task.Result,
		AgentRole:    task.AgentRole,
		AgentID:      task.AgentID,
		Title:        title,
		Summary:      checkpointSummary(task),
		Artifacts:    toArtifactViews(artifacts),
		InputBagIDs:  append([]string(nil), task.InputBagIDs...),
		OutputBagIDs: append([]string(nil), task.OutputBagIDs...),
		CanApprove:   actionable,
		CanReject:    actionable,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}, nil
}

func toTaskView(task repo.TaskRecord) taskView {
	return taskView{
		TaskID:             task.ID,
		RunID:              task.RunID,
		PipelineInstanceID: task.PipelineInstanceID,
		StageID:            task.StageID,
		AgentRole:          task.AgentRole,
		AgentID:            task.AgentID,
		Op:                 task.Op,
		Status:             task.Status,
		Result:             task.Result,
		ErrorMessage:       task.ErrorMessage,
		InputBagIDs:        append([]string(nil), task.InputBagIDs...),
		OutputBagIDs:       append([]string(nil), task.OutputBagIDs...),
		CreatedAt:          task.CreatedAt,
		UpdatedAt:          task.UpdatedAt,
	}
}

func toEventViews(events []repo.EventRecord) []eventView {
	items := make([]eventView, 0, len(events))
	for _, event := range events {
		items = append(items, eventView{
			EventID:     event.ID,
			RunID:       event.RunID,
			TaskID:      event.TaskID,
			AgentID:     event.AgentID,
			Type:        event.Type,
			Message:     event.Message,
			PayloadJSON: event.PayloadJSON,
			CreatedAt:   event.CreatedAt,
		})
	}
	return items
}

func toArtifactViews(artifacts []repo.ArtifactRecord) []artifactView {
	items := make([]artifactView, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, artifactView{
			ID:        artifact.ID,
			RunID:     artifact.RunID,
			TaskID:    artifact.TaskID,
			AgentID:   artifact.AgentID,
			Kind:      artifact.Kind,
			URI:       artifact.URI,
			CreatedAt: artifact.CreatedAt,
		})
	}
	return items
}

func isCheckpointTask(task repo.TaskRecord) bool {
	op := strings.ToLower(strings.TrimSpace(task.Op))
	if task.Status == core.TaskStatusWaitingExternal {
		return true
	}
	return strings.Contains(op, "review") || strings.Contains(op, "confirm") || strings.Contains(op, "approve")
}

func checkpointSummary(task repo.TaskRecord) string {
	switch {
	case task.Status == core.TaskStatusWaitingExternal:
		return "waiting for human checkpoint feedback"
	case task.Result != "":
		return "checkpoint feedback result: " + string(task.Result)
	default:
		return ""
	}
}

func rejectModeResult(mode string) core.TaskResultCode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "replan":
		return core.TaskResultCodeReplan
	case "bugfix", "bug":
		return core.TaskResultCodeBug
	default:
		return core.TaskResultCodeRewrite
	}
}

func emptyPipelineGraph(runID core.RunID, refName string, updatedAt time.Time) doujiagit.RunGraph {
	if strings.TrimSpace(refName) == "" {
		refName = doujiagit.DefaultRefName
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return doujiagit.RunGraph{
		RunID: runID,
		Ref: doujiagit.RefView{
			RefName:             refName,
			FrontierSnapshotIDs: []string{},
			UpdatedAt:           updatedAt,
		},
		Refs:              []doujiagit.RefView{},
		FrontierSnapshots: []doujiagit.FrontierSnapshotView{},
		RefMoveEvents:     []doujiagit.RefMoveEventView{},
		Snapshots:         []doujiagit.SnapshotView{},
		Bags:              []doujiagit.BagView{},
		HistorySnapshots:  []doujiagit.SnapshotView{},
		HistoryBags:       []doujiagit.BagView{},
		HistoryFrontiers:  []doujiagit.FrontierSnapshotView{},
	}
}

func isMissingDoujiaGitRef(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "ref ") && strings.Contains(message, " not found")
}

func decodeOptionalJSON(r *http.Request, out any) error {
	err := json.NewDecoder(r.Body).Decode(out)
	if err == nil || err == io.EOF {
		return nil
	}
	return err
}

func (b *Bootstrap) findContainerIDForBag(ctx context.Context, runID core.RunID, bagID string) (string, error) {
	bagID = strings.TrimSpace(bagID)
	if bagID == "" {
		return "", fmt.Errorf("bag_id is required")
	}
	run, err := b.Internals.RunRepository.Get(ctx, runID)
	if err != nil {
		return "", err
	}
	bag, err := b.Internals.DoujiaGitRepository.GetBag(ctx, bagID)
	if err != nil {
		return "", err
	}
	if bag.RunID != runID {
		return "", fmt.Errorf("bag %q does not belong to run %q", bagID, runID)
	}
	store := &artifact.ScopedLocalStore{
		RunRoot:       run.ProjectDir,
		WorkspaceRoot: run.ProjectDir,
	}
	builder := newFinalResultBagReader(b.Internals.DoujiaGitRepository, store)
	for _, versionID := range bag.ArtifactVersionIDs {
		containerID, err := builder.containerIDFromVersion(ctx, versionID)
		if err != nil {
			return "", err
		}
		if containerID != "" {
			return containerID, nil
		}
	}
	return "", fmt.Errorf("container_id not found in bag %q", bagID)
}

type finalResultBagReader struct {
	repository doujiagit.Repository
	store      artifact.Store
}

func newFinalResultBagReader(repository doujiagit.Repository, store artifact.Store) finalResultBagReader {
	return finalResultBagReader{repository: repository, store: store}
}

func (r finalResultBagReader) containerIDFromVersion(ctx context.Context, versionID string) (string, error) {
	version, err := r.repository.GetArtifactVersion(ctx, versionID)
	if err != nil {
		return "", err
	}
	for _, objectID := range version.ObjectIDs {
		object, err := r.repository.GetObject(ctx, objectID)
		if err != nil {
			return "", err
		}
		if containerID := parseContainerIDFromText(object.StorageURI); containerID != "" {
			return containerID, nil
		}
		if r.store != nil && strings.TrimSpace(object.StorageURI) != "" {
			content, err := r.store.Read(ctx, object.StorageURI)
			if err != nil {
				return "", err
			}
			if containerID := parseContainerIDFromText(string(content)); containerID != "" {
				return containerID, nil
			}
		}
	}
	return "", nil
}

func openDockerContainerPreviewURL(ctx context.Context, containerID string) (string, error) {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("container_id is required")
	}
	if err := runDockerCommand(ctx, "start", containerID); err != nil {
		return "", fmt.Errorf("container_start_failed: %w", err)
	}
	output, err := dockerCommandOutput(ctx, "port", containerID)
	if err != nil {
		return "", fmt.Errorf("container_preview_unavailable: %w", err)
	}
	previewURL := previewURLFromDockerPortOutput(output)
	if previewURL == "" {
		return "", fmt.Errorf("container_preview_unavailable: container %q has no published HTTP port; recreate it with a published port or pass preview_url", containerID)
	}
	return previewURL, nil
}

func finalResultAssetURL(runID core.RunID, containerID string, assetPath string) string {
	return "/api/runs/" + url.PathEscape(string(runID)) + "/preview/final-result/" + url.PathEscape(containerID) + "/" + strings.TrimPrefix(assetPath, "/")
}

func finalResultAssetURLWithVersion(runID core.RunID, containerID string, assetPath string, version string) string {
	previewURL := finalResultAssetURL(runID, containerID, assetPath)
	version = strings.TrimSpace(version)
	if version == "" {
		return previewURL
	}
	return previewURL + "?v=" + url.QueryEscape(version)
}

func finalResultContainerVersion(ctx context.Context, containerID string) string {
	output, err := dockerCommandOutput(ctx, "inspect", "-f", "{{.State.StartedAt}}", containerID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

func finalResultAssetFromPath(parts []string) (finalResultAsset, error) {
	var asset finalResultAsset
	if len(parts) == 0 {
		return asset, fmt.Errorf("container_id is required")
	}
	asset.ContainerID = strings.TrimSpace(parts[0])
	if asset.ContainerID == "" {
		return asset, fmt.Errorf("container_id is required")
	}
	assetPath := strings.Join(parts[1:], "/")
	if strings.TrimSpace(assetPath) == "" {
		assetPath = "index.html"
	}
	assetPath = pathpkg.Clean("/" + strings.ReplaceAll(assetPath, "\\", "/"))
	assetPath = strings.TrimPrefix(assetPath, "/")
	if assetPath == "." || strings.HasPrefix(assetPath, "../") || strings.Contains(assetPath, "/../") {
		return asset, fmt.Errorf("invalid final result asset path")
	}
	asset.Path = assetPath
	return asset, nil
}

func readContainerFinalResultAsset(ctx context.Context, asset finalResultAsset) ([]byte, error) {
	containerPath := pathpkg.Join("/workspace/repo", asset.Path)
	output, err := dockerCommandOutput(ctx, "exec", asset.ContainerID, "cat", containerPath)
	if err != nil {
		return nil, fmt.Errorf("final_result_asset_unavailable: %w", err)
	}
	return []byte(output), nil
}

func rewriteFinalResultHTML(html string, runID core.RunID, containerID string) string {
	baseHref := finalResultAssetURL(runID, containerID, "")
	if !strings.HasSuffix(baseHref, "/") {
		baseHref += "/"
	}
	headTag := regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	if headTag.MatchString(html) {
		return headTag.ReplaceAllStringFunc(html, func(match string) string {
			return match + `<base href="` + baseHref + `">`
		})
	}
	return `<base href="` + baseHref + `">` + html
}

func previewURLFromDockerPortOutput(output string) string {
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if url := previewURLFromDockerPortLine(line); url != "" {
			return url
		}
	}
	return ""
}

func previewURLFromDockerPortLine(line string) string {
	fields := strings.Split(line, "->")
	if len(fields) != 2 {
		return ""
	}
	host := strings.TrimSpace(fields[1])
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, ":::") {
		host = "127.0.0.1:" + host[strings.LastIndex(host, ":")+1:]
	}
	if !strings.Contains(host, ":") {
		return ""
	}
	port := host[strings.LastIndex(host, ":")+1:]
	if strings.TrimSpace(port) == "" {
		return ""
	}
	return "http://127.0.0.1:" + port + "/"
}

func parseContainerIDFromText(text string) string {
	matches := regexp.MustCompile(`"container_id"\s*:\s*"([^"]+)"`).FindStringSubmatch(text)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func runDockerCommand(ctx context.Context, args ...string) error {
	_, err := dockerCommandOutput(ctx, args...)
	return err
}

func dockerCommandOutput(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseUploadFile(r *http.Request, fieldName string) (string, []byte, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return "", nil, fmt.Errorf("invalid multipart form")
	}
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", nil, fmt.Errorf("missing upload file")
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", nil, err
	}
	return header.Filename, body, nil
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string, next func()) {
	if r.Method != method {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	next()
}

func writeAPIJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}
	writeAPIJSON(w, status, map[string]string{"error": message})
}

func setAPIHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func splitAPIPath(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw := strings.Split(path, "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (b *Bootstrap) projectForRun(ctx context.Context, run repo.RunRecord) (repo.ProjectRecord, error) {
	if strings.TrimSpace(run.ProjectID) == "" {
		return repo.ProjectRecord{}, fmt.Errorf("please select an llm model first")
	}
	if b.Internals.ProjectRepository == nil {
		return repo.ProjectRecord{}, fmt.Errorf("project repository is unavailable")
	}
	project, err := b.Internals.ProjectRepository.Get(ctx, run.ProjectID)
	if err != nil {
		return repo.ProjectRecord{}, err
	}
	if !hasRunLLMConfig(project.LLM) {
		return repo.ProjectRecord{}, fmt.Errorf("please select an llm model first")
	}
	return project, nil
}

func (b *Bootstrap) generateDoujiaSessionResponse(ctx context.Context, run repo.RunRecord, project repo.ProjectRecord, content string, now time.Time, history []repo.SessionMessageRecord, pool []repo.ImprovementItemRecord, progress string, memorySummary string) ([]repo.SessionMessageRecord, []repo.SessionArtifactRecord, error) {
	adapter := b.doujiaLLMFactory(project.LLM)
	if adapter == nil {
		return nil, nil, fmt.Errorf("llm adapter is unavailable")
	}
	resp, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{
				Role: "system",
				Content: strings.Join([]string{
					"You are Doujia, a collaborative product assistant.",
					"Reply in Chinese.",
					"Keep replies very short, usually one or two sentences.",
					"Only emit a requirement summary when you truly understand the user's request, can compress it into one very short requirement item, and no longer need to ask clarifying questions.",
					"If the request is still vague, keep clarifying and do not trigger a requirement summary.",
					"Return JSON with fields reply, should_emit_requirement_summary, summary_title, and summary_detail.",
					"Keep summary_title and summary_detail extremely short, like a single requirement item.",
					"Do not write explanations, questions, or analysis in the summary.",
				}, " "),
			},
			{
				Role:    "user",
				Content: buildDoujiaRequirementPrompt(run, content, history, pool, progress, memorySummary),
			},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	reply, summary, err := parseDoujiaSessionReply(resp.Message.Content)
	if err != nil {
		return nil, nil, err
	}
	summary = normalizeRequirementSummary(summary)
	iterationNo := max(1, run.CurrentIterationNo)
	messages := []repo.SessionMessageRecord{
		{
			ID:          "message_doujia_" + randomHex(6),
			RunID:       run.ID,
			IterationNo: iterationNo,
			Role:        "ceo",
			MessageType: "chat",
			Content:     firstNonEmpty(reply, "我先帮你把需求收敛成一条简短结论。"),
			CreatedAt:   now.Add(time.Millisecond),
		},
	}
	var artifacts []repo.SessionArtifactRecord
	if summary != "" {
		messages = append(messages, repo.SessionMessageRecord{
			ID:          "message_doujia_summary_" + randomHex(6),
			RunID:       run.ID,
			IterationNo: iterationNo,
			Role:        "ceo",
			MessageType: "requirement_summary",
			Content:     summary,
			CreatedAt:   now.Add(2 * time.Millisecond),
		})
		artifacts = []repo.SessionArtifactRecord{
			{
				ID:          "artifact_requirement_summary_" + randomHex(6),
				RunID:       run.ID,
				IterationNo: iterationNo,
				Kind:        "requirement_summary",
				Title:       summarizeImprovementTitle(summary),
				Content:     summary,
				CreatedAt:   now.Add(3 * time.Millisecond),
			},
		}
	}
	return messages, artifacts, nil
}

func buildDoujiaSessionChatRequest(run repo.RunRecord, content string, history []repo.SessionMessageRecord, pool []repo.ImprovementItemRecord, progress string, memorySummary string) llm.ChatRequest {
	return llm.ChatRequest{
		Messages: []llm.Message{
			{
				Role: "system",
				Content: strings.Join([]string{
					"You are Doujia, a collaborative product assistant.",
					"Reply in Chinese.",
					"Keep replies very short, usually one or two sentences.",
					"Only emit a requirement summary when you truly understand the user's request, can compress it into one very short requirement item, and no longer need to ask clarifying questions.",
					"If the request is still vague, keep clarifying and do not trigger a requirement summary.",
					"Return JSON with fields reply, should_emit_requirement_summary, summary_title, and summary_detail.",
					"Keep summary_title and summary_detail extremely short, like a single requirement item.",
					"Do not write explanations, questions, or analysis in the summary.",
				}, " "),
			},
			{
				Role:    "user",
				Content: buildDoujiaRequirementPrompt(run, content, history, pool, progress, memorySummary),
			},
		},
	}
}

func buildDoujiaSessionRecords(run repo.RunRecord, now time.Time, reply string, summary string) ([]repo.SessionMessageRecord, []repo.SessionArtifactRecord) {
	summary = normalizeRequirementSummary(summary)
	iterationNo := max(1, run.CurrentIterationNo)
	messages := []repo.SessionMessageRecord{
		{
			ID:          "message_doujia_" + randomHex(6),
			RunID:       run.ID,
			IterationNo: iterationNo,
			Role:        "ceo",
			MessageType: "chat",
			Content:     firstNonEmpty(reply, "I will help refine this requirement."),
			CreatedAt:   now.Add(time.Millisecond),
		},
	}
	var artifacts []repo.SessionArtifactRecord
	if summary != "" {
		messages = append(messages, repo.SessionMessageRecord{
			ID:          "message_doujia_summary_" + randomHex(6),
			RunID:       run.ID,
			IterationNo: iterationNo,
			Role:        "ceo",
			MessageType: "requirement_summary",
			Content:     summary,
			CreatedAt:   now.Add(2 * time.Millisecond),
		})
		artifacts = []repo.SessionArtifactRecord{
			{
				ID:          "artifact_requirement_summary_" + randomHex(6),
				RunID:       run.ID,
				IterationNo: iterationNo,
				Kind:        "requirement_summary",
				Title:       summarizeImprovementTitle(summary),
				Content:     summary,
				CreatedAt:   now.Add(3 * time.Millisecond),
			},
		}
	}
	return messages, artifacts
}

func buildDoujiaRequirementPrompt(run repo.RunRecord, content string, history []repo.SessionMessageRecord, pool []repo.ImprovementItemRecord, progress string, memorySummary string) string {
	lines := []string{
		"Current run context:",
		fmt.Sprintf("- run_id: %s", run.ID),
		fmt.Sprintf("- iteration_no: %d", max(1, run.CurrentIterationNo)),
	}
	if strings.TrimSpace(memorySummary) != "" {
		lines = append(lines, "", "Compressed memory summary:", strings.TrimSpace(memorySummary))
	}
	if historyText := formatConversationHistory(history); historyText != "" {
		lines = append(lines, "", "Conversation history:", historyText)
	}
	if poolText := formatConfirmedRequirementPool(pool); poolText != "" {
		lines = append(lines, "", "Confirmed requirement pool:", poolText)
	}
	if strings.TrimSpace(progress) != "" {
		lines = append(lines, "", "Current progress:", strings.TrimSpace(progress))
	}
	lines = append(lines, "", "User message:", strings.TrimSpace(content), "", "Return one JSON object only.")
	return strings.Join(lines, "\n")
}

func latestConversationMemorySummary(ctx context.Context, runID core.RunID, repository repo.SessionArtifactRepository) string {
	if repository == nil {
		return ""
	}
	artifacts, err := repository.ListByRun(ctx, runID)
	if err != nil || len(artifacts) == 0 {
		return ""
	}
	for i := len(artifacts) - 1; i >= 0; i-- {
		if artifacts[i].Kind != "conversation_memory" {
			continue
		}
		return strings.TrimSpace(artifacts[i].Content)
	}
	return ""
}

func buildConversationMemoryArtifact(run repo.RunRecord, history []repo.SessionMessageRecord, pool []repo.ImprovementItemRecord, progress string, now time.Time) repo.SessionArtifactRecord {
	summary := buildConversationMemorySummary(history, pool, progress)
	if summary == "" {
		return repo.SessionArtifactRecord{}
	}
	iterationNo := max(1, run.CurrentIterationNo)
	return repo.SessionArtifactRecord{
		ID:          "artifact_conversation_memory_" + randomHex(6),
		RunID:       run.ID,
		IterationNo: iterationNo,
		Kind:        "conversation_memory",
		Title:       "Doujia Memory",
		Content:     summary,
		CreatedAt:   now.Add(4 * time.Millisecond),
	}
}

func buildConversationMemorySummary(history []repo.SessionMessageRecord, pool []repo.ImprovementItemRecord, progress string) string {
	parts := make([]string, 0, 3)
	if historyText := summarizeConversationMemoryHistory(history); historyText != "" {
		parts = append(parts, "Recent notes:\n"+historyText)
	}
	if poolText := formatConfirmedRequirementPool(pool); poolText != "" {
		parts = append(parts, "Confirmed requirements:\n"+poolText)
	}
	if strings.TrimSpace(progress) != "" {
		parts = append(parts, "Progress:\n- "+strings.TrimSpace(progress))
	}
	summary := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if summary == "" {
		return ""
	}
	runes := []rune(summary)
	if len(runes) > 600 {
		summary = strings.TrimSpace(string(runes[:600]))
	}
	return summary
}

func summarizeConversationMemoryHistory(history []repo.SessionMessageRecord) string {
	if len(history) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(history))
	lines := make([]string, 0, minInt(len(history), 6))
	for _, message := range history {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if _, exists := seen[content]; exists {
			continue
		}
		seen[content] = struct{}{}
		lines = append(lines, "- "+content)
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

func parseDoujiaSessionReply(content string) (string, string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", "", nil
	}
	var structured doujiaSessionReply
	if err := json.Unmarshal([]byte(trimmed), &structured); err == nil {
		reply := strings.TrimSpace(structured.Reply)
		if reply == "" {
			reply = firstNonEmpty(structured.SummaryTitle, "我先帮你整理一下需求。")
		}
		if len([]rune(reply)) > 120 {
			reply = strings.TrimSpace(string([]rune(reply)[:120]))
		}
		if !structured.ShouldEmitRequirementSummary {
			return reply, "", nil
		}
		summary := buildRequirementSummaryContent(structured.SummaryTitle, structured.SummaryDetail)
		return reply, summary, nil
	}
	if reply, ok := extractJSONStringField(trimmed, "reply"); ok {
		reply = strings.TrimSpace(reply)
		if len([]rune(reply)) > 120 {
			reply = strings.TrimSpace(string([]rune(reply)[:120]))
		}
		return reply, "", nil
	}
	if len([]rune(trimmed)) > 120 {
		trimmed = strings.TrimSpace(string([]rune(trimmed)[:120]))
	}
	return trimmed, "", nil
}

func extractJSONStringField(content string, field string) (string, bool) {
	key := `"` + field + `"`
	index := strings.Index(content, key)
	if index < 0 {
		return "", false
	}
	rest := content[index+len(key):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return "", false
	}
	rest = strings.TrimLeft(rest[colon+1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	var out strings.Builder
	escaped := false
	for _, r := range rest {
		if escaped {
			switch r {
			case '"', '\\', '/':
				out.WriteRune(r)
			case 'n':
				out.WriteRune('\n')
			case 'r':
				out.WriteRune('\r')
			case 't':
				out.WriteRune('\t')
			default:
				out.WriteRune(r)
			}
			escaped = false
			continue
		}
		switch r {
		case '\\':
			escaped = true
		case '"':
			return out.String(), true
		default:
			out.WriteRune(r)
		}
	}
	return out.String(), out.Len() > 0
}

func buildRequirementSummaryContent(title string, detail string) string {
	title = strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	parts := make([]string, 0, 2)
	if title != "" {
		parts = append(parts, title)
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	content := strings.TrimSpace(strings.Join(parts, "\n"))
	if content == "" {
		return "需求确认卡片"
	}
	if len([]rune(content)) > 80 {
		content = strings.TrimSpace(string([]rune(content)[:80]))
	}
	return content
}

func normalizeRequirementSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if len([]rune(summary)) > 60 {
		return ""
	}
	lower := strings.ToLower(summary)
	blocked := []string{
		"你更偏向",
		"还想确认",
		"如果你愿意",
		"请告诉我",
		"你想",
		"吗？",
		"吗?",
		"为什么",
		"怎么",
		"是否",
		"可以先",
		"继续补充",
		"先告诉我",
		"你好",
		"可以",
		"方向合理",
	}
	for _, phrase := range blocked {
		if strings.Contains(summary, phrase) || strings.Contains(lower, strings.ToLower(phrase)) {
			return ""
		}
	}
	return summary
}

func formatConversationHistory(history []repo.SessionMessageRecord) string {
	if len(history) == 0 {
		return ""
	}
	lines := make([]string, 0, minInt(len(history), 8))
	start := 0
	if len(history) > 8 {
		start = len(history) - 8
	}
	for _, message := range history[start:] {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s/%s: %s", message.Role, message.MessageType, content))
	}
	return strings.Join(lines, "\n")
}

func formatConfirmedRequirementPool(items []repo.ImprovementItemRecord) string {
	confirmed := confirmedRequirementPool(items)
	if len(confirmed) == 0 {
		return ""
	}
	lines := make([]string, 0, len(confirmed))
	for _, item := range confirmed {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			detail = strings.TrimSpace(item.Title)
		}
		if detail == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s", detail))
	}
	return strings.Join(lines, "\n")
}

func confirmedRequirementPool(items []repo.ImprovementItemRecord) []repo.ImprovementItemRecord {
	out := make([]repo.ImprovementItemRecord, 0, len(items))
	for _, item := range items {
		if item.Source != "requirement_pool_confirmed" {
			continue
		}
		if item.Status != "confirmed" && item.Status != "selected" && item.Status != "open" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func currentRunProgress(run repo.RunRecord, ctx context.Context, tasks repo.TaskRepository) string {
	if tasks == nil {
		return ""
	}
	items, err := tasks.ListByRun(ctx, run.ID)
	if err != nil {
		return ""
	}
	if len(items) == 0 {
		return fmt.Sprintf("run status %s", run.Status)
	}
	for _, task := range items {
		if task.Status == core.TaskStatusWaitingExternal || task.Status == core.TaskStatusRunning || task.Status == core.TaskStatusDispatched {
			return fmt.Sprintf("%s %s", task.ID, task.Status)
		}
	}
	last := items[len(items)-1]
	return fmt.Sprintf("%s %s", last.ID, last.Status)
}

func buildRequirementDocumentFromPool(run repo.RunRecord, pool []repo.ImprovementItemRecord, history []repo.SessionMessageRecord) string {
	lines := []string{
		"# Requirement Document",
		"",
		"## Run Context",
		"",
		fmt.Sprintf("- run_id: %s", run.ID),
		fmt.Sprintf("- iteration_no: %d", max(1, run.CurrentIterationNo)),
		"",
		"## Confirmed Requirements",
		"",
	}
	for _, item := range pool {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			detail = strings.TrimSpace(item.Title)
		}
		if detail == "" {
			continue
		}
		lines = append(lines, "- "+detail)
	}
	if historyText := formatConversationHistory(history); historyText != "" {
		lines = append(lines, "", "## Conversation Notes", "", historyText)
	}
	return strings.Join(lines, "\n") + "\n"
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func hasRunLLMConfig(config core.LLMRunConfig) bool {
	return firstNonEmpty(config.Provider, config.Model, config.APIKey, config.BaseURL, config.APIStyle) != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hasLLMCreateConfig(req createRunRequest) bool {
	return strings.TrimSpace(req.ModelProvider) != "" ||
		strings.TrimSpace(req.ModelName) != "" ||
		strings.TrimSpace(req.APIKey) != "" ||
		strings.TrimSpace(req.BaseURL) != "" ||
		strings.TrimSpace(req.APIStyle) != ""
}

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
