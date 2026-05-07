package core

import (
	"context"
	"strings"

	appcore "devflow/internal/core"
)

const (
	ExecutionModeNormal  = "normal"
	ExecutionModeRepair  = "repair"
	ExecutionModeRewrite = "rewrite"
	ExecutionModeReuse   = "reuse"
)

func NormalizeExecutionMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return ExecutionModeNormal
	}
	return mode
}

type Task struct {
	TaskID        string `json:"task_id"`
	RunID         string `json:"run_id,omitempty"`
	StageID       string `json:"stage_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	Role          string `json:"role"`
	Op            string `json:"op"`
	OpID          string `json:"op_id,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty"`
}

type AgentInputBundle struct {
	InputDir        string              `json:"input_dir"`
	OutputDir       string              `json:"output_dir"`
	OutputURIBase   string              `json:"output_uri_base,omitempty"`
	Inputs          []InputArtifact     `json:"inputs"`
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
	ProducerSnapshotID string            `json:"producer_snapshot_id,omitempty"`
	ArtifactVersionIDs []string          `json:"artifact_version_ids"`
}

type AgentInputVersion struct {
	ArtifactVersionID string   `json:"artifact_version_id"`
	LogicalArtifactID string   `json:"logical_artifact_id"`
	LogicalKey        string   `json:"logical_key,omitempty"`
	ObjectType        string   `json:"object_type,omitempty"`
	ContentType       string   `json:"content_type,omitempty"`
	Encoding          string   `json:"encoding,omitempty"`
	ObjectIDs         []string `json:"object_ids,omitempty"`
	StorageURI        string   `json:"storage_uri,omitempty"`
	LocalPath         string   `json:"local_path,omitempty"`
}

type ProducedBagsResolver func(Task, AgentInputBundle, AgentResult) []appcore.ProducedBagManifest

type BagBindingRef = appcore.BagBindingRef
type BagMemberRequirement = appcore.BagMemberRequirement
type InputBagSpec = appcore.InputBagSpec
type OutputBagSpec = appcore.OutputBagSpec
type ProducedBagManifest = appcore.ProducedBagManifest
type ProducedBagMember = appcore.ProducedBagMember

type OpSpec struct {
	Role                    string                                       `json:"role"`
	Op                      string                                       `json:"op"`
	RoleDescription         string                                       `json:"role_description"`
	OpDescription           string                                       `json:"op_description"`
	InputBags               []InputBagSpec                               `json:"input_bags,omitempty"`
	BaseRequiredInputs      []InputRequirement                           `json:"base_required_inputs"`
	BaseOptionalInputs      []InputRequirement                           `json:"base_optional_inputs"`
	ModeInputRules          map[string]ModeInputRule                     `json:"mode_input_rules"`
	OutputBags              []OutputBagSpec                              `json:"output_bags,omitempty"`
	ExpectedOutputs         []OutputSpec                                 `json:"expected_outputs"`
	ExpectedOutputsResolver func(AgentInputBundle) ([]OutputSpec, error) `json:"-"`
	ProducedBagsResolver    ProducedBagsResolver                         `json:"-"`
	PreflightHandlers       []string                                     `json:"preflight_handlers,omitempty"`
	AllowedTools            []ToolSpec                                   `json:"allowed_tools"`
	PromptTemplateID        string                                       `json:"prompt_template_id"`
}

type InputRequirement struct {
	LogicalKey  string `json:"logical_key"`
	Description string `json:"description,omitempty"`
}

type ModeInputRule struct {
	ExtraRequiredInputs []InputRequirement `json:"extra_required_inputs,omitempty"`
	ExtraOptionalInputs []InputRequirement `json:"extra_optional_inputs,omitempty"`
}

type OutputSpec struct {
	LogicalKey  string `json:"logical_key"`
	ObjectType  string `json:"object_type"`
	ContentType string `json:"content_type,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	FileName    string `json:"file_name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

func (s OpSpec) FindOutput(logicalKey string) (OutputSpec, bool) {
	for _, output := range s.ExpectedOutputs {
		if output.LogicalKey == logicalKey {
			return output, true
		}
	}
	return OutputSpec{}, false
}

func (s OpSpec) ResolveExpectedOutputs(bundle AgentInputBundle) ([]OutputSpec, error) {
	if s.ExpectedOutputsResolver != nil {
		return s.ExpectedOutputsResolver(bundle)
	}
	return s.ExpectedOutputs, nil
}

func (s OpSpec) ResolveProducedBags(task Task, bundle AgentInputBundle, result AgentResult) []appcore.ProducedBagManifest {
	switch strings.TrimSpace(result.Result) {
	case "kok", string(appcore.TaskResultCodeBug):
	default:
		return nil
	}
	if s.ProducedBagsResolver != nil {
		return s.ProducedBagsResolver(task, bundle, result)
	}
	return nil
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type AgentResult struct {
	Result       string                        `json:"result"`
	Message      string                        `json:"message,omitempty"`
	Outputs      []AgentOutput                 `json:"outputs"`
	Errors       []AgentError                  `json:"errors,omitempty"`
	ProducedBags []appcore.ProducedBagManifest `json:"produced_bags,omitempty"`
	Control      []appcore.Control             `json:"control,omitempty"`
	Controls     []appcore.Control             `json:"controls,omitempty"`
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

type Agent interface {
	Role() string
	Run(ctx context.Context, req AgentRunRequest) (AgentResult, error)
}

type HandlerRegistryLike interface {
	Get(name string) (Handler, bool)
}

type AgentRunRequest struct {
	Task     Task
	Bundle   AgentInputBundle
	OpSpec   OpSpec
	Prompt   string
	Handlers HandlerRegistryLike
	LLM      LLMClientLike
	ToolLoop ToolLoopLike
}

type Handler interface {
	Name() string
	Description() string
	ToolSpec() ToolSpec
	Handle(ctx context.Context, req HandlerRequest) (HandlerResponse, error)
}

type ScopedHandler interface {
	WithScope(bundle AgentInputBundle) Handler
}

type HandlerRequest struct {
	Task   Task             `json:"task"`
	Bundle AgentInputBundle `json:"bundle"`
	OpSpec OpSpec           `json:"op_spec"`
	Args   map[string]any   `json:"args"`
}

type HandlerResponse struct {
	Data map[string]any `json:"data"`
}

type LLMClientLike interface{}

type ToolLoopRequest struct {
	Task     Task
	Bundle   AgentInputBundle
	OpSpec   OpSpec
	Prompt   string
	Handlers HandlerRegistryLike
	LLM      LLMClientLike
}

type ToolLoopLike interface {
	Run(ctx context.Context, req ToolLoopRequest) (AgentResult, error)
}
