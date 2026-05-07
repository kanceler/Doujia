package pipeline

import "devflow/internal/core"

const (
	RegistrySchemaVersionV04 = "devflow.pipeline.registry/v0.4"
	PipelineSchemaVersionV04 = "devflow.pipeline/v0.4"
)

type RegistrySpec struct {
	SchemaVersion   string            `json:"schema_version"`
	RegistryID      string            `json:"registry_id"`
	EntryPipelineID core.PipelineID   `json:"entry_pipeline_id"`
	PipelineDefs    []PipelineDefSpec `json:"pipeline_defs"`
}

type PipelineDefSpec struct {
	SchemaVersion     string                                `json:"schema_version"`
	PipelineID        core.PipelineID                       `json:"pipeline_id"`
	Kind              string                                `json:"kind,omitempty"`
	Name              string                                `json:"name,omitempty"`
	Namespace         NamespaceSpec                         `json:"namespace,omitempty"`
	Signature         SignatureSpec                         `json:"signature,omitempty"`
	Handlers          []PipelineHandlerSpec                 `json:"handlers,omitempty"`
	ExceptionHandlers map[string]ExceptionHandlerPolicySpec `json:"exception_handlers,omitempty"`
	StartState        string                                `json:"start_state"`
	DeliveryState     string                                `json:"delivery_state"`
	States            []StateSpec                           `json:"states"`
	Transitions       []TransitionSpec                      `json:"transitions"`
}

type NamespaceSpec struct {
	Agents []SignatureAgentSpec `json:"agents,omitempty"`
	Bags   []BagSpec            `json:"bags,omitempty"`
}

type SignatureSpec struct {
	Params           []string             `json:"params,omitempty"`
	Agents           []SignatureAgentSpec `json:"agents,omitempty"`
	InputBags        []BagSpec            `json:"input_bags,omitempty"`
	OutputBags       []BagSpec            `json:"output_bags,omitempty"`
	Throws           []ExceptionSpec      `json:"throws,omitempty"`
	ExportedHandlers []HandlerExportSpec  `json:"exported_handlers,omitempty"`
}

type SignatureAgentSpec struct {
	Name           string         `json:"name,omitempty"`
	Role           core.AgentRole `json:"role,omitempty"`
	DefaultAgentID core.AgentID   `json:"default_agent_id,omitempty"`
}

type StateSpec struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind,omitempty"`
	Name    string      `json:"name,omitempty"`
	Proof   ProofSpec   `json:"proof,omitempty"`
	Exposes ExposesSpec `json:"exposes,omitempty"`
	Next    *NextSpec   `json:"next,omitempty"`
}

type ProofSpec struct {
	Type        string            `json:"type,omitempty"`
	Transition  string            `json:"transition,omitempty"`
	SourceState string            `json:"source_state,omitempty"`
	Result      string            `json:"result,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	JoinBy      []string          `json:"join_by,omitempty"`
	Sources     []ProofSourceSpec `json:"sources,omitempty"`
	States      []string          `json:"states,omitempty"`
}

type ProofSourceSpec struct {
	State string `json:"state,omitempty"`
	Name  string `json:"name,omitempty"`
}

type ExposesSpec struct {
	Bags []BagSpec `json:"bags,omitempty"`
}

type NextSpec struct {
	Type             string              `json:"type,omitempty"`
	Transitions      []string            `json:"transitions,omitempty"`
	SourceTransition string              `json:"source_transition,omitempty"`
	Cases            map[string]NextCase `json:"cases,omitempty"`
}

type NextCase struct {
	Transitions []string `json:"transitions,omitempty"`
	EnterState  string   `json:"enter_state,omitempty"`
	Action      string   `json:"action,omitempty"`
	Ref         *RefSpec `json:"ref,omitempty"`
}

type RefSpec struct {
	Mode                 string    `json:"mode,omitempty"`
	KeepBags             []BagSpec `json:"keep_bags,omitempty"`
	ReplaceBags          []BagSpec `json:"replace_bags,omitempty"`
	IncludeFailureReport bool      `json:"include_failure_report,omitempty"`
}

type TransitionSpec struct {
	ID                 string               `json:"id"`
	Kind               string               `json:"kind,omitempty"`
	FromState          string               `json:"from_state"`
	ToState            string               `json:"to_state"`
	Agent              *AgentSpec           `json:"agent,omitempty"`
	Op                 string               `json:"op,omitempty"`
	PipelineID         core.PipelineID      `json:"pipeline_id,omitempty"`
	Mode               string               `json:"mode,omitempty"`
	Control            *ControlSpec         `json:"control,omitempty"`
	Foreach            *ForeachSpec         `json:"foreach,omitempty"`
	Bindings           *BindingsSpec        `json:"bindings,omitempty"`
	InputBags          []BagSpec            `json:"input_bags,omitempty"`
	OutputBags         []BagSpec            `json:"output_bags,omitempty"`
	OutputBagsByResult map[string][]BagSpec `json:"output_bags_by_result,omitempty"`
	OutputHandlers     []HandlerBindingSpec `json:"output_handlers,omitempty"`
	Limits             LimitsSpec           `json:"limits,omitempty"`
}

type AgentSpec struct {
	Role  core.AgentRole `json:"role,omitempty"`
	Alias core.AgentID   `json:"alias,omitempty"`
}

type ForeachSpec struct {
	ItemsFrom ForeachItemsFromSpec `json:"items_from"`
	ItemKey   string               `json:"item_key"`
}

type ForeachItemsFromSpec struct {
	State string `json:"state,omitempty"`
	Name  string `json:"name,omitempty"`
}

type BindingsSpec struct {
	Params        map[string]string `json:"params,omitempty"`
	AgentBindings map[string]string `json:"agent_bindings,omitempty"`
	InputBags     map[string]any    `json:"input_bags,omitempty"`
}

type ControlSpec struct {
	Type         string          `json:"type,omitempty"`
	TransitionID string          `json:"transition_id,omitempty"`
	PipelineID   core.PipelineID `json:"pipeline_id,omitempty"`
}

type BagSpec struct {
	Name           string            `json:"name,omitempty"`
	FromReturn     string            `json:"from_return,omitempty"`
	Required       *bool             `json:"required,omitempty"`
	Collection     bool              `json:"collection,omitempty"`
	IndexedBy      []string          `json:"indexed_by,omitempty"`
	FromTransition string            `json:"from_transition,omitempty"`
	FromState      string            `json:"from_state,omitempty"`
	FromStates     []string          `json:"from_states,omitempty"`
	Match          map[string]string `json:"match,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

type ExceptionSpec struct {
	Result string    `json:"result"`
	Bags   []BagSpec `json:"bags,omitempty"`
}

type PipelineHandlerSpec struct {
	Name        string                `json:"name"`
	Handles     []string              `json:"handles,omitempty"`
	Transition  string                `json:"transition"`
	InputBags   []HandlerInputBagSpec `json:"input_bags,omitempty"`
	ReplaceBags []BagSpec             `json:"replace_bags,omitempty"`
	Resume      ResumePolicySpec      `json:"resume,omitempty"`
}

type HandlerInputBagSpec struct {
	Name   string            `json:"name"`
	Source string            `json:"source,omitempty"`
	Match  map[string]string `json:"match,omitempty"`
}

type HandlerExportSpec struct {
	Name      string    `json:"name"`
	Handler   string    `json:"handler"`
	Handles   []string  `json:"handles,omitempty"`
	Replaces  []BagSpec `json:"replaces,omitempty"`
	IndexedBy []string  `json:"indexed_by,omitempty"`
}

type HandlerBindingSpec struct {
	Name        string   `json:"name"`
	FromHandler string   `json:"from_handler"`
	IndexedBy   []string `json:"indexed_by,omitempty"`
}

type ExceptionHandlerPolicySpec struct {
	Select      string           `json:"select,omitempty"`
	Join        string           `json:"join,omitempty"`
	OnUnhandled string           `json:"on_unhandled,omitempty"`
	Resume      ResumePolicySpec `json:"resume,omitempty"`
}

type ResumePolicySpec struct {
	Type string `json:"type,omitempty"`
}

type LimitsSpec struct {
	MaxAttempts int `json:"max_attempts,omitempty"`
}

func (r RegistrySpec) Pipeline(id core.PipelineID) (PipelineDefSpec, bool) {
	for _, def := range r.PipelineDefs {
		if def.PipelineID == id {
			return def, true
		}
	}
	return PipelineDefSpec{}, false
}
