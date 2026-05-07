package pipeline

import (
	"fmt"
	"strings"

	"devflow/internal/core"
)

func NewLegacyRegistryFromSpec(spec RegistrySpec, ids ...core.PipelineID) (*MemoryRegistry, error) {
	spec = NormalizeRegistrySpec(spec)
	if err := ValidateRegistrySpec(spec); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []core.PipelineID{spec.EntryPipelineID}
	}
	legacySpecs := make([]PipelineSpec, 0, len(ids))
	for _, id := range ids {
		def, ok := spec.Pipeline(id)
		if !ok {
			return nil, fmt.Errorf("pipeline %q not found in registry %q", id, spec.RegistryID)
		}
		legacy, err := CompileLegacyPipelineDefPrefix(def)
		if err != nil {
			return nil, err
		}
		legacySpecs = append(legacySpecs, legacy)
	}
	return NewMemoryRegistry(legacySpecs...), nil
}

func CompileLinearPipelineDef(def PipelineDefSpec) (PipelineSpec, error) {
	return CompileLegacyPipelineDef(def)
}

func CompileLegacyPipelineDef(def PipelineDefSpec) (PipelineSpec, error) {
	return compileLegacyPipelineDef(def, false)
}

func CompileLegacyPipelineDefPrefix(def PipelineDefSpec) (PipelineSpec, error) {
	return compileLegacyPipelineDef(def, true)
}

func compileLegacyPipelineDef(def PipelineDefSpec, allowDynamicTail bool) (PipelineSpec, error) {
	def = normalizePipelineDef(def)
	states := make(map[string]StateSpec, len(def.States))
	for _, state := range def.States {
		states[state.ID] = state
	}
	transitions := make(map[string]TransitionSpec, len(def.Transitions))
	for _, transition := range def.Transitions {
		transitions[transition.ID] = transition
	}

	currentStateID := def.StartState
	seenStates := map[string]bool{}
	stages := make([]StageSpec, 0, len(def.Transitions))
	var previousStageID core.StageID

	for {
		if currentStateID == def.DeliveryState {
			break
		}
		if seenStates[currentStateID] {
			return PipelineSpec{}, fmt.Errorf("pipeline %q is not linear: state %q repeats", def.PipelineID, currentStateID)
		}
		seenStates[currentStateID] = true

		state, ok := states[currentStateID]
		if !ok {
			return PipelineSpec{}, fmt.Errorf("pipeline %q state %q not found", def.PipelineID, currentStateID)
		}
		if state.Next == nil {
			if allowDynamicTail && len(stages) > 0 {
				break
			}
			return PipelineSpec{}, fmt.Errorf("pipeline %q state %q has no executable linear task transition", def.PipelineID, currentStateID)
		}
		if state.Next.Type != "all" || len(state.Next.Transitions) != 1 {
			if allowDynamicTail && len(stages) > 0 {
				break
			}
			return PipelineSpec{}, fmt.Errorf("pipeline %q state %q is outside the strict linear legacy subset", def.PipelineID, currentStateID)
		}

		transitionID := state.Next.Transitions[0]
		transition, ok := transitions[transitionID]
		if !ok {
			return PipelineSpec{}, fmt.Errorf("pipeline %q transition %q not found", def.PipelineID, transitionID)
		}
		if transition.Kind != "task" {
			if allowDynamicTail && len(stages) > 0 {
				break
			}
			return PipelineSpec{}, fmt.Errorf("pipeline %q transition %q has unsupported first kind %q for legacy adapter", def.PipelineID, transition.ID, transition.Kind)
		}
		if transition.FromState != currentStateID {
			return PipelineSpec{}, fmt.Errorf("pipeline %q transition %q starts at %q, want %q", def.PipelineID, transition.ID, transition.FromState, currentStateID)
		}
		if transition.Agent == nil {
			return PipelineSpec{}, fmt.Errorf("pipeline %q transition %q has no agent", def.PipelineID, transition.ID)
		}

		stageID := core.StageID(transition.ID)
		stage := StageSpec{
			ID:         stageID,
			Name:       legacyStageName(transition, states[transition.ToState]),
			AgentRole:  transition.Agent.Role,
			AgentAlias: legacyAgentAlias(def.Namespace, transition.Agent.Alias),
			Op:         transition.Op,
			External:   len(stages) == 0 && state.Proof.Type == "external",
			InputBags:  append([]BagSpec(nil), transition.InputBags...),
			OutputBags: append([]BagSpec(nil), transition.OutputBags...),
		}
		if previousStageID != "" {
			stage.DependsOnIDs = []core.StageID{previousStageID}
		}
		stages = append(stages, stage)
		previousStageID = stageID
		currentStateID = transition.ToState
	}

	if len(stages) == 0 {
		return PipelineSpec{}, fmt.Errorf("pipeline %q has no task transitions to compile", def.PipelineID)
	}
	return PipelineSpec{
		ID:     def.PipelineID,
		Name:   def.Name,
		Stages: stages,
	}, nil
}

func legacyAgentAlias(namespace NamespaceSpec, alias core.AgentID) core.AgentID {
	for _, agent := range namespace.Agents {
		if agent.Name == string(alias) && agent.DefaultAgentID != "" {
			return agent.DefaultAgentID
		}
	}
	return alias
}

func legacyStageName(transition TransitionSpec, toState StateSpec) string {
	if strings.TrimSpace(toState.Name) != "" {
		return toState.Name
	}
	return transition.ID
}
