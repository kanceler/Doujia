package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"devflow/internal/core"
)

func ValidateRegistrySpec(spec RegistrySpec) error {
	validator := registrySpecValidator{}
	validator.validate(spec)
	if len(validator.errs) > 0 {
		return fmt.Errorf("invalid pipeline registry: %s", strings.Join(validator.errs, "; "))
	}
	return nil
}

type registrySpecValidator struct {
	errs []string
}

func (v *registrySpecValidator) validate(spec RegistrySpec) {
	if strings.TrimSpace(spec.SchemaVersion) != RegistrySchemaVersionV04 {
		v.errorf("schema_version must be %q", RegistrySchemaVersionV04)
	}
	if strings.TrimSpace(spec.RegistryID) == "" {
		v.errorf("registry_id is required")
	}
	if strings.TrimSpace(string(spec.EntryPipelineID)) == "" {
		v.errorf("entry_pipeline_id is required")
	}

	pipelines := make(map[core.PipelineID]PipelineDefSpec, len(spec.PipelineDefs))
	for i, def := range spec.PipelineDefs {
		if strings.TrimSpace(string(def.PipelineID)) == "" {
			v.errorf("pipeline_defs[%d].pipeline_id is required", i)
			continue
		}
		if _, exists := pipelines[def.PipelineID]; exists {
			v.errorf("pipeline_id %q is duplicated", def.PipelineID)
			continue
		}
		pipelines[def.PipelineID] = def
	}
	if _, ok := pipelines[spec.EntryPipelineID]; spec.EntryPipelineID != "" && !ok {
		v.errorf("entry_pipeline_id %q does not exist in pipeline_defs", spec.EntryPipelineID)
	}
	for _, def := range spec.PipelineDefs {
		v.validatePipeline(def, pipelines)
	}
	v.validateAgentScopes(spec, pipelines)
}

func (v *registrySpecValidator) validatePipeline(def PipelineDefSpec, pipelines map[core.PipelineID]PipelineDefSpec) {
	prefix := fmt.Sprintf("pipeline %q", def.PipelineID)
	if strings.TrimSpace(def.SchemaVersion) != PipelineSchemaVersionV04 {
		v.errorf("%s schema_version must be %q", prefix, PipelineSchemaVersionV04)
	}
	if def.Kind != "" && def.Kind != "entry" && def.Kind != "subpipeline" {
		v.errorf("%s kind must be entry or subpipeline", prefix)
	}
	if strings.TrimSpace(def.StartState) == "" {
		v.errorf("%s start_state is required", prefix)
	}
	if strings.TrimSpace(def.DeliveryState) == "" {
		v.errorf("%s delivery_state is required", prefix)
	}
	if len(def.States) == 0 {
		v.errorf("%s states is required", prefix)
	}
	if def.Transitions == nil {
		v.errorf("%s transitions is required", prefix)
	}
	scope := newNamespaceBagScope(def.Namespace.Bags)
	v.validateNamespace(prefix+".namespace", def.Namespace)
	v.validateSignature(prefix, def.Signature, scope)
	v.validateSignatureAgentsDeclared(prefix, def)

	states := make(map[string]StateSpec, len(def.States))
	for i, state := range def.States {
		if strings.TrimSpace(state.ID) == "" {
			v.errorf("%s states[%d].id is required", prefix, i)
			continue
		}
		if _, exists := states[state.ID]; exists {
			v.errorf("%s state.id %q is duplicated", prefix, state.ID)
			continue
		}
		states[state.ID] = state
	}
	if _, ok := states[def.StartState]; def.StartState != "" && !ok {
		v.errorf("%s start_state %q does not exist", prefix, def.StartState)
	}
	if _, ok := states[def.DeliveryState]; def.DeliveryState != "" && !ok {
		v.errorf("%s delivery_state %q does not exist", prefix, def.DeliveryState)
	}

	transitions := make(map[string]TransitionSpec, len(def.Transitions))
	for i, transition := range def.Transitions {
		if strings.TrimSpace(transition.ID) == "" {
			v.errorf("%s transitions[%d].id is required", prefix, i)
			continue
		}
		if _, exists := transitions[transition.ID]; exists {
			v.errorf("%s transition.id %q is duplicated", prefix, transition.ID)
			continue
		}
		transitions[transition.ID] = transition
	}

	for _, state := range def.States {
		v.validateState(prefix, state, states, transitions, scope)
	}
	v.validatePipelineHandlers(prefix, def, transitions, scope)
	v.validatePipelineReturnContract(prefix, def, states)
	for _, transition := range def.Transitions {
		v.validateTransition(prefix, def, transition, states, transitions, pipelines, scope)
	}
	v.validateBoundedCycles(prefix, def, states)
}

func (v *registrySpecValidator) validateSignatureAgentsDeclared(prefix string, def PipelineDefSpec) {
	if len(def.Signature.Agents) == 0 {
		return
	}
	declared := make(map[string]bool, len(def.Namespace.Agents))
	for _, agent := range def.Namespace.Agents {
		declared[agent.Name] = true
	}
	for _, agent := range def.Signature.Agents {
		if !declared[agent.Name] {
			v.errorf("%s signature.agents.%s must be declared in namespace.agents", prefix, agent.Name)
		}
	}
}

func (v *registrySpecValidator) validateSignature(prefix string, signature SignatureSpec, scope namespaceBagScope) {
	params := make(map[string]bool, len(signature.Params))
	for i, param := range signature.Params {
		if strings.TrimSpace(param) == "" {
			v.errorf("%s signature.params[%d] is required", prefix, i)
			continue
		}
		if params[param] {
			v.errorf("%s signature.params %q is duplicated", prefix, param)
		}
		params[param] = true
	}
	v.validateSignatureAgents(prefix+".signature.agents", signature.Agents)
	v.validateSignatureBags(prefix+".signature.input_bags", signature.InputBags, scope)
	v.validateSignatureBags(prefix+".signature.output_bags", signature.OutputBags, scope)
	v.validateSignatureThrows(prefix+".signature.throws", signature.Throws, scope)
	v.validateSignatureExportedHandlers(prefix+".signature.exported_handlers", signature.ExportedHandlers, nil, scope)
}

func (v *registrySpecValidator) validateNamespace(prefix string, namespace NamespaceSpec) {
	v.validateSignatureAgents(prefix+".agents", namespace.Agents)
	v.validateNamespaceBags(prefix+".bags", namespace.Bags)
}

func (v *registrySpecValidator) validateSignatureAgents(prefix string, agents []SignatureAgentSpec) {
	names := make(map[string]bool, len(agents))
	for i, agent := range agents {
		agentPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(agent.Name) == "" {
			v.errorf("%s name is required", agentPrefix)
		} else if names[agent.Name] {
			v.errorf("%s name %q is duplicated", prefix, agent.Name)
		}
		names[agent.Name] = true
		if strings.TrimSpace(string(agent.Role)) == "" {
			v.errorf("%s role is required", agentPrefix)
		}
	}
}

func (v *registrySpecValidator) validateNamespaceBags(prefix string, bags []BagSpec) {
	names := make(map[string]bool, len(bags))
	for i, bag := range bags {
		bagPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(bag.Name) == "" {
			v.errorf("%s name is required", bagPrefix)
		} else if names[bag.Name] {
			v.errorf("%s name %q is duplicated", prefix, bag.Name)
		}
		names[bag.Name] = true
	}
}

func (v *registrySpecValidator) validateSignatureBags(prefix string, bags []BagSpec, scope namespaceBagScope) {
	names := make(map[string]bool, len(bags))
	for i, bag := range bags {
		bagPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(bag.Name) == "" {
			v.errorf("%s name is required", bagPrefix)
		} else if names[bag.Name] {
			v.errorf("%s name %q is duplicated", prefix, bag.Name)
		}
		names[bag.Name] = true
		v.validateBagReference(bagPrefix, bag, scope)
	}
}

func (v *registrySpecValidator) validateSignatureThrows(prefix string, exceptions []ExceptionSpec, scope namespaceBagScope) {
	seenResults := make(map[string]bool, len(exceptions))
	for i, exception := range exceptions {
		exceptionPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		result := strings.TrimSpace(exception.Result)
		if result == "" {
			v.errorf("%s result is required", exceptionPrefix)
		} else if seenResults[result] {
			v.errorf("%s result %q is duplicated", prefix, result)
		}
		seenResults[result] = true
		v.validateSignatureBags(exceptionPrefix+".bags", exception.Bags, scope)
	}
}

func (v *registrySpecValidator) validateSignatureExportedHandlers(prefix string, exports []HandlerExportSpec, handlers map[string]PipelineHandlerSpec, scope namespaceBagScope) {
	names := make(map[string]bool, len(exports))
	for i, export := range exports {
		exportPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(export.Name) == "" {
			v.errorf("%s name is required", exportPrefix)
		} else if names[export.Name] {
			v.errorf("%s name %q is duplicated", prefix, export.Name)
		}
		names[export.Name] = true
		if strings.TrimSpace(export.Handler) == "" {
			v.errorf("%s handler is required", exportPrefix)
		} else if handlers != nil {
			if _, ok := handlers[export.Handler]; !ok {
				v.errorf("%s handler %q does not exist in pipeline handlers", exportPrefix, export.Handler)
			}
		}
		if len(export.Handles) == 0 {
			v.errorf("%s handles is required", exportPrefix)
		}
		v.validateSignatureBags(exportPrefix+".replaces", export.Replaces, scope)
	}
}

func (v *registrySpecValidator) validatePipelineHandlers(prefix string, def PipelineDefSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	handlers := make(map[string]PipelineHandlerSpec, len(def.Handlers))
	for i, handler := range def.Handlers {
		handlerPrefix := fmt.Sprintf("%s handlers[%d]", prefix, i)
		if strings.TrimSpace(handler.Name) == "" {
			v.errorf("%s name is required", handlerPrefix)
		} else if _, exists := handlers[handler.Name]; exists {
			v.errorf("%s handlers name %q is duplicated", prefix, handler.Name)
		}
		handlers[handler.Name] = handler
		if len(handler.Handles) == 0 {
			v.errorf("%s handles is required", handlerPrefix)
		}
		transitionID := strings.TrimSpace(handler.Transition)
		if transitionID == "" {
			v.errorf("%s transition is required", handlerPrefix)
		} else {
			transition, ok := transitions[transitionID]
			if !ok {
				v.errorf("%s transition %q does not exist", handlerPrefix, transitionID)
			} else {
				if transition.Kind != "task" {
					v.errorf("%s transition %q must be a task transition", handlerPrefix, transitionID)
				}
				if transition.Limits.MaxAttempts <= 0 {
					v.errorf("%s transition %q must declare limits.max_attempts", handlerPrefix, transitionID)
				}
			}
		}
		v.validateHandlerInputBags(handlerPrefix+".input_bags", handler.InputBags, scope)
		v.validateBags(handlerPrefix+".replace_bags", handler.ReplaceBags, nil, transitions, scope)
		v.validateResumePolicy(handlerPrefix+".resume", handler.Resume)
	}
	v.validateSignatureExportedHandlers(prefix+".signature.exported_handlers", def.Signature.ExportedHandlers, handlers, scope)
	v.validateExceptionHandlerPolicies(prefix+".exception_handlers", def.ExceptionHandlers, handlers)
}

func (v *registrySpecValidator) validateHandlerInputBags(prefix string, bags []HandlerInputBagSpec, scope namespaceBagScope) {
	for i, bag := range bags {
		bagPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		if strings.TrimSpace(bag.Name) == "" {
			v.errorf("%s name is required", bagPrefix)
		} else if _, ok := scope.Lookup(bag.Name); !ok {
			v.errorf("%s references unknown namespace bag %q", bagPrefix, bag.Name)
		}
		switch strings.TrimSpace(bag.Source) {
		case "", "exception", "owner_input", "owner_output":
		default:
			v.errorf("%s source %q is unsupported", bagPrefix, bag.Source)
		}
	}
}

func (v *registrySpecValidator) validateExceptionHandlerPolicies(prefix string, policies map[string]ExceptionHandlerPolicySpec, handlers map[string]PipelineHandlerSpec) {
	handledResults := make(map[string]bool)
	for _, handler := range handlers {
		for _, result := range handler.Handles {
			handledResults[result] = true
		}
	}
	for result, policy := range policies {
		resultPrefix := fmt.Sprintf("%s[%q]", prefix, result)
		if strings.TrimSpace(result) == "" {
			v.errorf("%s result is required", prefix)
			continue
		}
		if !handledResults[result] {
			v.errorf("%s has no handler that handles %q", resultPrefix, result)
		}
		switch policy.Select {
		case "", "nearest_matching", "all_matching":
		default:
			v.errorf("%s select %q is unsupported", resultPrefix, policy.Select)
		}
		switch policy.Join {
		case "", "all_success":
		default:
			v.errorf("%s join %q is unsupported", resultPrefix, policy.Join)
		}
		switch policy.OnUnhandled {
		case "", "bubble", "fail_run":
		default:
			v.errorf("%s on_unhandled %q is unsupported", resultPrefix, policy.OnUnhandled)
		}
		v.validateResumePolicy(resultPrefix+".resume", policy.Resume)
	}
}

func (v *registrySpecValidator) validateResumePolicy(prefix string, policy ResumePolicySpec) {
	switch policy.Type {
	case "", "retry_failed_transition":
	default:
		v.errorf("%s type %q is unsupported", prefix, policy.Type)
	}
}

func (v *registrySpecValidator) validateState(prefix string, state StateSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	statePrefix := fmt.Sprintf("%s state %q", prefix, state.ID)
	v.validateProof(statePrefix, state, states, transitions, scope)
	v.validateBags(statePrefix+".exposes.bags", state.Exposes.Bags, states, transitions, scope)
	if state.Next != nil {
		v.validateNext(statePrefix, *state.Next, states, transitions, scope)
	}
}

func (v *registrySpecValidator) validateProof(prefix string, state StateSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	proof := state.Proof
	switch proof.Type {
	case "":
		return
	case "external":
		return
	case "transition_result":
		if strings.TrimSpace(proof.Transition) == "" {
			v.errorf("%s proof.transition is required", prefix)
			return
		}
		if _, ok := transitions[proof.Transition]; !ok {
			v.errorf("%s proof.transition %q does not exist", prefix, proof.Transition)
		}
	case "aggregate":
		v.validateAggregateProof(prefix, state, states, scope)
	case "accepted_result":
		if strings.TrimSpace(proof.SourceState) == "" {
			v.errorf("%s proof.source_state is required", prefix)
		} else if _, ok := states[proof.SourceState]; !ok {
			v.errorf("%s proof.source_state %q does not exist", prefix, proof.SourceState)
		}
		if strings.TrimSpace(proof.Result) == "" {
			v.errorf("%s proof.result is required", prefix)
		}
	default:
		v.errorf("%s proof.type %q is unsupported", prefix, proof.Type)
	}
}

func (v *registrySpecValidator) validateAggregateProof(prefix string, state StateSpec, states map[string]StateSpec, scope namespaceBagScope) {
	proof := state.Proof
	switch proof.Mode {
	case "all":
		if len(proof.States) == 0 {
			v.errorf("%s aggregate proof.states is required", prefix)
		}
		for _, stateID := range proof.States {
			if _, ok := states[stateID]; !ok {
				v.errorf("%s aggregate proof state %q does not exist", prefix, stateID)
			}
		}
	case "join_bags":
		if len(proof.JoinBy) == 0 {
			v.errorf("%s aggregate join_bags.join_by is required", prefix)
		}
		if len(proof.Sources) == 0 {
			v.errorf("%s aggregate join_bags.sources is required", prefix)
		}
		for _, source := range proof.Sources {
			sourceState, ok := states[source.State]
			if !ok {
				v.errorf("%s aggregate source state %q does not exist", prefix, source.State)
				continue
			}
			sourceRef := proofSourceRef(source)
			if sourceRef == "" {
				v.errorf("%s aggregate source bag reference is required", prefix)
				continue
			}
			if _, ok := scope.Lookup(sourceRef); !ok {
				v.errorf("%s aggregate source %s:%s is not declared in namespace.bags", prefix, source.State, sourceRef)
				continue
			}
			bag, ok := exposedBagByRef(sourceState, sourceRef)
			if !ok {
				v.errorf("%s aggregate source %s:%s is not exposed", prefix, source.State, sourceRef)
				continue
			}
			for _, key := range proof.JoinBy {
				if !containsString(bag.IndexedBy, key) {
					v.errorf("%s aggregate join_by %q is missing from %s:%s indexed_by", prefix, key, source.State, sourceRef)
				}
			}
		}
	default:
		v.errorf("%s aggregate proof.mode %q is unsupported", prefix, proof.Mode)
	}
}

func (v *registrySpecValidator) validateNext(prefix string, next NextSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	switch next.Type {
	case "all":
		if len(next.Transitions) == 0 {
			v.errorf("%s next.transitions is required", prefix)
		}
		for _, transitionID := range next.Transitions {
			if _, ok := transitions[transitionID]; !ok {
				v.errorf("%s next transition %q does not exist", prefix, transitionID)
			}
		}
	case "by_result", "by_results":
		if next.Type == "by_result" {
			if strings.TrimSpace(next.SourceTransition) == "" {
				v.errorf("%s next.source_transition is required", prefix)
			} else if _, ok := transitions[next.SourceTransition]; !ok {
				v.errorf("%s next.source_transition %q does not exist", prefix, next.SourceTransition)
			}
		}
		if len(next.Cases) == 0 {
			v.errorf("%s next.cases is required", prefix)
		}
		for result, nextCase := range next.Cases {
			casePrefix := fmt.Sprintf("%s next.cases[%q]", prefix, result)
			if len(nextCase.Transitions) == 0 && strings.TrimSpace(nextCase.EnterState) == "" && strings.TrimSpace(nextCase.Action) == "" {
				v.errorf("%s must declare transitions, enter_state, or action", casePrefix)
			}
			for _, transitionID := range nextCase.Transitions {
				transition, ok := transitions[transitionID]
				if !ok {
					v.errorf("%s transition %q does not exist", casePrefix, transitionID)
					continue
				}
				if nextCase.Ref != nil && nextCase.Ref.Mode == "recover" && transition.Limits.MaxAttempts <= 0 {
					v.errorf("%s recover transition %q must declare limits.max_attempts", casePrefix, transitionID)
				}
			}
			if strings.TrimSpace(nextCase.EnterState) != "" {
				if _, ok := states[nextCase.EnterState]; !ok {
					v.errorf("%s enter_state %q does not exist", casePrefix, nextCase.EnterState)
				}
			}
			if nextCase.Ref != nil {
				v.validateRef(casePrefix+".ref", *nextCase.Ref, states, transitions, scope)
			}
		}
	default:
		v.errorf("%s next.type %q is unsupported", prefix, next.Type)
	}
}

func (v *registrySpecValidator) validateRef(prefix string, ref RefSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	switch ref.Mode {
	case "advance", "recover", "fork", "fail_run", "agent_directed":
	case "":
		v.errorf("%s mode is required", prefix)
	default:
		v.errorf("%s mode %q is unsupported", prefix, ref.Mode)
	}
	v.validateBags(prefix+".keep_bags", ref.KeepBags, states, transitions, scope)
	v.validateBags(prefix+".replace_bags", ref.ReplaceBags, states, transitions, scope)
}

func (v *registrySpecValidator) validateTransition(prefix string, def PipelineDefSpec, transition TransitionSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, pipelines map[core.PipelineID]PipelineDefSpec, scope namespaceBagScope) {
	transitionPrefix := fmt.Sprintf("%s transition %q", prefix, transition.ID)
	if _, ok := states[transition.FromState]; !ok {
		v.errorf("%s from_state %q does not exist", transitionPrefix, transition.FromState)
	}
	if _, ok := states[transition.ToState]; !ok {
		v.errorf("%s to_state %q does not exist", transitionPrefix, transition.ToState)
	}

	switch transition.Kind {
	case "task":
		if transition.Agent == nil {
			v.errorf("%s task.agent is required", transitionPrefix)
		} else {
			if strings.TrimSpace(string(transition.Agent.Role)) == "" {
				v.errorf("%s task.agent.role is required", transitionPrefix)
			}
			if strings.TrimSpace(string(transition.Agent.Alias)) == "" {
				v.errorf("%s task.agent.alias is required", transitionPrefix)
			}
		}
		if strings.TrimSpace(transition.Op) == "" {
			v.errorf("%s task.op is required", transitionPrefix)
		}
	case "call":
		v.validateCallTransition(transitionPrefix, def, transition, states, pipelines, scope)
	case "gate":
	case "":
		v.errorf("%s kind is required", transitionPrefix)
	default:
		v.errorf("%s kind %q is unsupported", transitionPrefix, transition.Kind)
	}

	v.validateBags(transitionPrefix+".input_bags", transition.InputBags, states, transitions, scope)
	v.validateBags(transitionPrefix+".output_bags", transition.OutputBags, states, transitions, scope)
	v.validateOutputBagsByResult(transitionPrefix+".output_bags_by_result", transition.OutputBagsByResult, states, transitions, scope)
}

func (v *registrySpecValidator) validateOutputBagsByResult(prefix string, bagsByResult map[string][]BagSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	for result, bags := range bagsByResult {
		result = strings.TrimSpace(result)
		resultPrefix := fmt.Sprintf("%s[%q]", prefix, result)
		if result == "" {
			v.errorf("%s result is required", prefix)
			continue
		}
		v.validateBags(resultPrefix, bags, states, transitions, scope)
	}
}

func (v *registrySpecValidator) validateCallTransition(prefix string, def PipelineDefSpec, transition TransitionSpec, states map[string]StateSpec, pipelines map[core.PipelineID]PipelineDefSpec, scope namespaceBagScope) {
	called, ok := pipelines[transition.PipelineID]
	if strings.TrimSpace(string(transition.PipelineID)) == "" {
		v.errorf("%s call.pipeline_id is required", prefix)
	} else if !ok {
		v.errorf("%s call.pipeline_id %q does not exist", prefix, transition.PipelineID)
	}

	switch transition.Mode {
	case "single":
	case "foreach":
		v.validateForeach(prefix, transition, states, scope)
	case "from_control":
		v.validateFromControl(prefix, transition)
	case "":
		v.errorf("%s call.mode is required", prefix)
	default:
		v.errorf("%s call.mode %q is unsupported", prefix, transition.Mode)
	}
	if ok {
		v.validateCallBindings(prefix, transition, called, scope)
		v.validateCallReturnBindings(prefix, transition, called)
		v.validateCallOutputHandlerBindings(prefix, transition, called)
		v.validateCallThrowsHandling(prefix, def, transition, called)
	}
}

func (v *registrySpecValidator) validatePipelineReturnContract(prefix string, def PipelineDefSpec, states map[string]StateSpec) {
	if strings.TrimSpace(def.DeliveryState) == "" {
		return
	}
	delivery, ok := states[def.DeliveryState]
	if !ok {
		return
	}
	signatureOutputs := bagSpecsByName(def.Signature.OutputBags)
	deliveryOutputs := bagSpecsByName(delivery.Exposes.Bags)
	for name, signatureBag := range signatureOutputs {
		deliveryBag, ok := deliveryOutputs[name]
		if !ok {
			v.errorf("%s delivery_state.exposes.bags missing signature.output_bags %q", prefix, name)
			continue
		}
		v.validateReturnIndexCompatibility(prefix+" delivery_state.exposes.bags", name, signatureBag, deliveryBag)
	}
	for name := range deliveryOutputs {
		if _, ok := signatureOutputs[name]; !ok {
			v.errorf("%s delivery_state.exposes.bags %q is not declared in signature.output_bags", prefix, name)
		}
	}
}

func (v *registrySpecValidator) validateCallReturnBindings(prefix string, transition TransitionSpec, called PipelineDefSpec) {
	signatureOutputs := bagSpecsByName(called.Signature.OutputBags)
	receivedOutputs := make(map[string]BagSpec, len(transition.OutputBags))
	for _, bag := range transition.OutputBags {
		returnName := callOutputReturnName(bag)
		if returnName == "" {
			continue
		}
		signatureBag, ok := signatureOutputs[returnName]
		if !ok {
			declared := sortedBagSpecNames(called.Signature.OutputBags)
			v.errorf("%s transition.output_bags %q receives undeclared return %q from called pipeline %q; declared returns: [%s]", prefix, bag.Name, returnName, called.PipelineID, strings.Join(declared, ","))
			continue
		}
		receivedOutputs[returnName] = bag
		v.validateReturnIndexCompatibility(prefix+".transition.output_bags", returnName, signatureBag, bag)
	}
	for name := range signatureOutputs {
		if _, ok := receivedOutputs[name]; !ok {
			v.errorf("%s transition.output_bags missing return %q declared by called pipeline %q", prefix, name, called.PipelineID)
		}
	}
}

func (v *registrySpecValidator) validateCallOutputHandlerBindings(prefix string, transition TransitionSpec, called PipelineDefSpec) {
	exported := handlerExportsByName(called.Signature.ExportedHandlers)
	received := make(map[string]bool, len(transition.OutputHandlers))
	for i, binding := range transition.OutputHandlers {
		bindingPrefix := fmt.Sprintf("%s.output_handlers[%d]", prefix, i)
		if strings.TrimSpace(binding.Name) == "" {
			v.errorf("%s name is required", bindingPrefix)
		}
		fromHandler := strings.TrimSpace(binding.FromHandler)
		if fromHandler == "" {
			v.errorf("%s from_handler is required", bindingPrefix)
			continue
		}
		export, ok := exported[fromHandler]
		if !ok {
			declared := sortedHandlerExportNames(called.Signature.ExportedHandlers)
			v.errorf("%s from_handler %q is not exported by called pipeline %q; exported handlers: [%s]", bindingPrefix, fromHandler, called.PipelineID, strings.Join(declared, ","))
			continue
		}
		received[fromHandler] = true
		for _, key := range export.IndexedBy {
			if !containsString(binding.IndexedBy, key) {
				v.errorf("%s %q indexed_by is missing handler index %q", bindingPrefix, fromHandler, key)
			}
		}
	}
	_ = received
}

func (v *registrySpecValidator) validateCallThrowsHandling(prefix string, def PipelineDefSpec, transition TransitionSpec, called PipelineDefSpec) {
	if len(called.Signature.Throws) == 0 {
		return
	}
	for _, exception := range called.Signature.Throws {
		result := strings.TrimSpace(exception.Result)
		if result == "" {
			continue
		}
		if pipelineHandlesResult(def, result) {
			continue
		}
		if pipelineRethrowsResult(def, result) {
			continue
		}
		v.errorf("%s called pipeline %q throws %q but parent neither handles nor rethrows it", prefix, called.PipelineID, result)
	}
}

func pipelineHandlesResult(def PipelineDefSpec, result string) bool {
	if _, ok := def.ExceptionHandlers[result]; ok {
		return true
	}
	for _, handler := range def.Handlers {
		if containsString(handler.Handles, result) {
			return true
		}
	}
	return false
}

func pipelineRethrowsResult(def PipelineDefSpec, result string) bool {
	for _, exception := range def.Signature.Throws {
		if strings.TrimSpace(exception.Result) == result {
			return true
		}
	}
	return false
}

func (v *registrySpecValidator) validateReturnIndexCompatibility(prefix string, name string, expected BagSpec, actual BagSpec) {
	for _, key := range expected.IndexedBy {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if !containsString(actual.IndexedBy, key) {
			v.errorf("%s %q indexed_by is missing return index %q", prefix, name, key)
		}
	}
}

func (v *registrySpecValidator) validateFromControl(prefix string, transition TransitionSpec) {
	if transition.Control == nil {
		v.errorf("%s control is required when mode is from_control", prefix)
		return
	}
	if strings.TrimSpace(transition.Control.Type) == "" {
		v.errorf("%s control.type is required", prefix)
	}
	if strings.TrimSpace(transition.Control.TransitionID) == "" {
		v.errorf("%s control.transition_id is required", prefix)
	} else if transition.Control.TransitionID != transition.ID {
		v.errorf("%s control.transition_id %q must match transition id %q", prefix, transition.Control.TransitionID, transition.ID)
	}
	if strings.TrimSpace(string(transition.Control.PipelineID)) == "" {
		v.errorf("%s control.pipeline_id is required", prefix)
	} else if transition.Control.PipelineID != transition.PipelineID {
		v.errorf("%s control.pipeline_id %q must match call.pipeline_id %q", prefix, transition.Control.PipelineID, transition.PipelineID)
	}
}

type agentScope map[string]SignatureAgentSpec

func (v *registrySpecValidator) validateAgentScopes(spec RegistrySpec, pipelines map[core.PipelineID]PipelineDefSpec) {
	entry, ok := pipelines[spec.EntryPipelineID]
	if !ok {
		return
	}
	v.validatePipelineAgentScope(entry, nil, pipelines, map[string]bool{})
}

func (v *registrySpecValidator) validatePipelineAgentScope(def PipelineDefSpec, inherited agentScope, pipelines map[core.PipelineID]PipelineDefSpec, visited map[string]bool) {
	scope := mergeAgentScope(inherited, def.Namespace.Agents)
	visitKey := string(def.PipelineID) + "|" + agentScopeKey(scope)
	if visited[visitKey] {
		return
	}
	visited[visitKey] = true

	prefix := fmt.Sprintf("pipeline %q", def.PipelineID)
	for _, transition := range def.Transitions {
		transitionPrefix := fmt.Sprintf("%s transition %q", prefix, transition.ID)
		switch transition.Kind {
		case "task":
			if transition.Agent == nil {
				continue
			}
			alias := strings.TrimSpace(string(transition.Agent.Alias))
			if alias == "" || strings.HasPrefix(alias, "${") {
				continue
			}
			agent, ok := scope[alias]
			if !ok {
				v.errorf("%s task.agent.alias %q is not declared in current namespace or inherited agent scope", transitionPrefix, alias)
				continue
			}
			if agent.Role != "" && transition.Agent.Role != "" && agent.Role != transition.Agent.Role {
				v.errorf("%s task.agent.alias %q has role %q, want %q", transitionPrefix, alias, agent.Role, transition.Agent.Role)
			}
		case "call":
			if transition.Bindings != nil {
				v.validateAgentBindingReferences(transitionPrefix, transition.Bindings.AgentBindings, scope)
			}
			called, ok := pipelines[transition.PipelineID]
			if !ok {
				continue
			}
			v.validatePipelineAgentScope(called, scope, pipelines, visited)
		}
	}
}

func (v *registrySpecValidator) validateAgentBindingReferences(prefix string, bindings map[string]string, scope agentScope) {
	for name, expr := range bindings {
		ref, ok := inheritedAgentReference(expr)
		if !ok {
			continue
		}
		if _, exists := scope[ref]; !exists {
			v.errorf("%s bindings.agent_bindings.%s references unknown inherited agent %q", prefix, name, ref)
		}
	}
}

func inheritedAgentReference(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "${agents.") || !strings.HasSuffix(expr, "}") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(expr, "${agents."), "}")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	return name, true
}

func mergeAgentScope(inherited agentScope, local []SignatureAgentSpec) agentScope {
	scope := make(agentScope, len(inherited)+len(local))
	for name, agent := range inherited {
		scope[name] = agent
	}
	for _, agent := range local {
		name := strings.TrimSpace(agent.Name)
		if name == "" {
			continue
		}
		scope[name] = agent
	}
	return scope
}

func agentScopeKey(scope agentScope) string {
	names := make([]string, 0, len(scope))
	for name := range scope {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func (v *registrySpecValidator) validateForeach(prefix string, transition TransitionSpec, states map[string]StateSpec, scope namespaceBagScope) {
	if transition.Foreach == nil {
		v.errorf("%s foreach is required when mode is foreach", prefix)
		return
	}
	itemsFrom := transition.Foreach.ItemsFrom
	if strings.TrimSpace(itemsFrom.State) == "" {
		v.errorf("%s foreach.items_from.state is required", prefix)
		return
	}
	sourceState, ok := states[itemsFrom.State]
	if !ok {
		v.errorf("%s foreach.items_from.state %q does not exist", prefix, itemsFrom.State)
		return
	}
	if strings.TrimSpace(itemsFrom.Name) == "" {
		v.errorf("%s foreach.items_from.name is required", prefix)
	}
	sourceRef := foreachItemsFromRef(itemsFrom)
	if sourceRef == "" {
		return
	}
	if _, ok := scope.Lookup(sourceRef); !ok {
		v.errorf("%s foreach source %s:%s is not declared in namespace.bags", prefix, itemsFrom.State, sourceRef)
		return
	}
	bag, ok := exposedBagByRef(sourceState, sourceRef)
	if !ok {
		v.errorf("%s foreach source %s:%s is not exposed", prefix, itemsFrom.State, sourceRef)
		return
	}
	if !bag.Collection {
		v.errorf("%s foreach source %s:%s must be a collection", prefix, itemsFrom.State, sourceRef)
	}
	if strings.TrimSpace(transition.Foreach.ItemKey) == "" {
		v.errorf("%s foreach.item_key is required", prefix)
		return
	}
	if !containsString(bag.IndexedBy, transition.Foreach.ItemKey) {
		v.errorf("%s foreach.item_key %q is missing from source indexed_by", prefix, transition.Foreach.ItemKey)
	}
}

func (v *registrySpecValidator) validateCallBindings(prefix string, transition TransitionSpec, called PipelineDefSpec, scope namespaceBagScope) {
	bindings := transition.Bindings
	if bindings == nil {
		if len(called.Signature.Params) > 0 || len(called.Signature.Agents) > 0 || len(called.Signature.InputBags) > 0 {
			v.errorf("%s bindings is required by called pipeline %q", prefix, called.PipelineID)
		}
		return
	}

	requiredParams := stringSet(called.Signature.Params)
	for _, param := range called.Signature.Params {
		if strings.TrimSpace(bindings.Params[param]) == "" {
			v.errorf("%s bindings.params.%s is required by called pipeline %q", prefix, param, called.PipelineID)
		}
	}
	for param := range bindings.Params {
		if !requiredParams[param] {
			v.errorf("%s bindings.params.%s is not declared by called pipeline %q", prefix, param, called.PipelineID)
		}
	}

	requiredAgents := make(map[string]bool, len(called.Signature.Agents))
	for _, agent := range called.Signature.Agents {
		requiredAgents[agent.Name] = true
		if strings.TrimSpace(bindings.AgentBindings[agent.Name]) == "" {
			v.errorf("%s bindings.agent_bindings.%s is required by called pipeline %q", prefix, agent.Name, called.PipelineID)
		}
	}
	for name := range bindings.AgentBindings {
		if !requiredAgents[name] {
			v.errorf("%s bindings.agent_bindings.%s is not declared by called pipeline %q", prefix, name, called.PipelineID)
		}
	}

	requiredInputBags := make(map[string]bool, len(called.Signature.InputBags))
	for _, bag := range called.Signature.InputBags {
		requiredInputBags[bag.Name] = true
		if _, ok := bindings.InputBags[bag.Name]; !ok {
			v.errorf("%s bindings.input_bags.%s is required by called pipeline %q", prefix, bag.Name, called.PipelineID)
		}
	}
	for name := range bindings.InputBags {
		if !requiredInputBags[name] {
			v.errorf("%s bindings.input_bags.%s is not declared by called pipeline %q", prefix, name, called.PipelineID)
		}
	}
	v.validateBindingInputBagReferences(prefix, bindings.InputBags, scope)
}

func (v *registrySpecValidator) validateBindingInputBagReferences(prefix string, bindings map[string]any, scope namespaceBagScope) {
	for name, binding := range bindings {
		typed, ok := binding.(map[string]any)
		if !ok {
			continue
		}
		ref := anyMapString(typed, "name")
		if ref == "" {
			continue
		}
		if _, ok := scope.Lookup(ref); !ok {
			v.errorf("%s bindings.input_bags.%s references unknown namespace bag %q", prefix, name, ref)
		}
	}
}

func (v *registrySpecValidator) validateBags(prefix string, bags []BagSpec, states map[string]StateSpec, transitions map[string]TransitionSpec, scope namespaceBagScope) {
	for i, bag := range bags {
		bagPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		v.validateBagReference(bagPrefix, bag, scope)
		if strings.TrimSpace(bag.FromTransition) != "" {
			if _, ok := transitions[bag.FromTransition]; !ok {
				v.errorf("%s from_transition %q does not exist", bagPrefix, bag.FromTransition)
			}
		}
		if strings.TrimSpace(bag.FromState) != "" {
			if _, ok := states[bag.FromState]; !ok {
				v.errorf("%s from_state %q does not exist", bagPrefix, bag.FromState)
			}
		}
		for _, stateID := range bag.FromStates {
			if _, ok := states[stateID]; !ok {
				v.errorf("%s from_states %q does not exist", bagPrefix, stateID)
			}
		}
	}
}

func (v *registrySpecValidator) validateBagReference(prefix string, bag BagSpec, scope namespaceBagScope) {
	ref := bagRef(bag)
	if ref == "" {
		v.errorf("%s name is required", prefix)
		return
	}
	if _, ok := scope.Lookup(ref); !ok {
		v.errorf("%s references unknown namespace bag %q", prefix, ref)
	}
}

type validationEdge struct {
	To      string
	Bounded bool
}

func (v *registrySpecValidator) validateBoundedCycles(prefix string, def PipelineDefSpec, states map[string]StateSpec) {
	graph := make(map[string][]validationEdge, len(states))
	for _, state := range def.States {
		graph[state.ID] = nil
	}
	for _, transition := range def.Transitions {
		if states[transition.FromState].ID == "" || states[transition.ToState].ID == "" {
			continue
		}
		graph[transition.FromState] = append(graph[transition.FromState], validationEdge{
			To:      transition.ToState,
			Bounded: transition.Limits.MaxAttempts > 0,
		})
	}
	for _, state := range def.States {
		if state.Next != nil {
			for _, nextCase := range state.Next.Cases {
				if nextCase.EnterState != "" {
					graph[state.ID] = append(graph[state.ID], validationEdge{To: nextCase.EnterState})
				}
			}
		}
		switch state.Proof.Type {
		case "aggregate":
			for _, source := range state.Proof.Sources {
				if source.State != "" {
					graph[source.State] = append(graph[source.State], validationEdge{To: state.ID})
				}
			}
			for _, sourceState := range state.Proof.States {
				graph[sourceState] = append(graph[sourceState], validationEdge{To: state.ID})
			}
		case "accepted_result":
			if state.Proof.SourceState != "" {
				graph[state.Proof.SourceState] = append(graph[state.Proof.SourceState], validationEdge{To: state.ID})
			}
		}
	}

	for _, component := range stronglyConnectedComponents(graph) {
		if len(component) == 1 && !hasSelfLoop(graph, component[0]) {
			continue
		}
		if !componentHasBoundedEdge(graph, component) {
			v.errorf("%s has an unbounded static cycle involving states [%s]", prefix, strings.Join(component, ","))
		}
	}
}

func stronglyConnectedComponents(graph map[string][]validationEdge) [][]string {
	index := 0
	stack := make([]string, 0)
	onStack := make(map[string]bool, len(graph))
	indexes := make(map[string]int, len(graph))
	lowlinks := make(map[string]int, len(graph))
	components := make([][]string, 0)

	var visit func(string)
	visit = func(node string) {
		indexes[node] = index
		lowlinks[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true

		for _, edge := range graph[node] {
			if _, ok := graph[edge.To]; !ok {
				continue
			}
			if _, seen := indexes[edge.To]; !seen {
				visit(edge.To)
				if lowlinks[edge.To] < lowlinks[node] {
					lowlinks[node] = lowlinks[edge.To]
				}
			} else if onStack[edge.To] && indexes[edge.To] < lowlinks[node] {
				lowlinks[node] = indexes[edge.To]
			}
		}

		if lowlinks[node] != indexes[node] {
			return
		}
		component := make([]string, 0)
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == node {
				break
			}
		}
		components = append(components, component)
	}

	for node := range graph {
		if _, seen := indexes[node]; !seen {
			visit(node)
		}
	}
	return components
}

func hasSelfLoop(graph map[string][]validationEdge, node string) bool {
	for _, edge := range graph[node] {
		if edge.To == node {
			return true
		}
	}
	return false
}

func componentHasBoundedEdge(graph map[string][]validationEdge, component []string) bool {
	inComponent := stringSet(component)
	for _, node := range component {
		for _, edge := range graph[node] {
			if inComponent[edge.To] && edge.Bounded {
				return true
			}
		}
	}
	return false
}

func exposedBagByRef(state StateSpec, ref string) (BagSpec, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return BagSpec{}, false
	}
	for _, bag := range state.Exposes.Bags {
		if bag.Name == ref {
			return bag, true
		}
	}
	return BagSpec{}, false
}

func bagSpecsByName(bags []BagSpec) map[string]BagSpec {
	out := make(map[string]BagSpec, len(bags))
	for _, bag := range bags {
		name := strings.TrimSpace(bag.Name)
		if name == "" {
			continue
		}
		out[name] = bag
	}
	return out
}

func handlerExportsByName(exports []HandlerExportSpec) map[string]HandlerExportSpec {
	out := make(map[string]HandlerExportSpec, len(exports))
	for _, export := range exports {
		name := strings.TrimSpace(export.Name)
		if name == "" {
			continue
		}
		out[name] = export
	}
	return out
}

func callOutputReturnName(bag BagSpec) string {
	if name := strings.TrimSpace(bag.FromReturn); name != "" {
		return name
	}
	return strings.TrimSpace(bag.Name)
}

func sortedBagSpecNames(bags []BagSpec) []string {
	out := make([]string, 0, len(bags))
	for _, bag := range bags {
		if name := strings.TrimSpace(bag.Name); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func sortedHandlerExportNames(exports []HandlerExportSpec) []string {
	out := make([]string, 0, len(exports))
	for _, export := range exports {
		if name := strings.TrimSpace(export.Name); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func bagRef(bag BagSpec) string {
	return strings.TrimSpace(bag.Name)
}

func proofSourceRef(source ProofSourceSpec) string {
	return strings.TrimSpace(source.Name)
}

func foreachItemsFromRef(itemsFrom ForeachItemsFromSpec) string {
	return strings.TrimSpace(itemsFrom.Name)
}

func anyMapString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func stringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (v *registrySpecValidator) errorf(format string, args ...any) {
	v.errs = append(v.errs, fmt.Sprintf(format, args...))
}
