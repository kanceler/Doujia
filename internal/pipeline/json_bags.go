package pipeline

import "strings"

type namespaceBagScope struct {
	byRef map[string]BagSpec
}

func newNamespaceBagScope(bags []BagSpec) namespaceBagScope {
	scope := namespaceBagScope{byRef: make(map[string]BagSpec, len(bags))}
	for _, bag := range bags {
		bag = normalizeBagSpec(bag)
		if bag.Name != "" {
			scope.byRef[bag.Name] = bag
		}
	}
	return scope
}

func (s namespaceBagScope) Lookup(ref string) (BagSpec, bool) {
	if s.byRef == nil {
		return BagSpec{}, false
	}
	bag, ok := s.byRef[strings.TrimSpace(ref)]
	return bag, ok
}

func NormalizeRegistrySpec(spec RegistrySpec) RegistrySpec {
	out := spec
	if len(spec.PipelineDefs) == 0 {
		return out
	}
	out.PipelineDefs = make([]PipelineDefSpec, len(spec.PipelineDefs))
	for i, def := range spec.PipelineDefs {
		out.PipelineDefs[i] = normalizePipelineDef(def)
	}
	return out
}

func normalizePipelineDef(def PipelineDefSpec) PipelineDefSpec {
	out := def
	out.Namespace = normalizeNamespaceSpec(def.Namespace)
	out.Signature = normalizeSignatureSpec(def.Signature)
	out.Handlers = normalizePipelineHandlerSpecs(def.Handlers)
	if len(def.ExceptionHandlers) > 0 {
		out.ExceptionHandlers = make(map[string]ExceptionHandlerPolicySpec, len(def.ExceptionHandlers))
		for result, policy := range def.ExceptionHandlers {
			out.ExceptionHandlers[strings.TrimSpace(result)] = normalizeExceptionHandlerPolicySpec(policy)
		}
	}
	if len(def.States) > 0 {
		out.States = make([]StateSpec, len(def.States))
		for i, state := range def.States {
			out.States[i] = normalizeStateSpec(state)
		}
	}
	if len(def.Transitions) > 0 {
		out.Transitions = make([]TransitionSpec, len(def.Transitions))
		for i, transition := range def.Transitions {
			out.Transitions[i] = normalizeTransitionSpec(transition)
		}
	}
	if len(out.Namespace.Bags) == 0 {
		out.Namespace.Bags = inferNamespaceBags(out)
	}
	return out
}

func normalizeNamespaceSpec(namespace NamespaceSpec) NamespaceSpec {
	out := namespace
	if len(namespace.Bags) > 0 {
		out.Bags = make([]BagSpec, len(namespace.Bags))
		for i, bag := range namespace.Bags {
			out.Bags[i] = normalizeBagSpec(bag)
		}
	}
	return out
}

func normalizeSignatureSpec(signature SignatureSpec) SignatureSpec {
	out := signature
	if len(signature.InputBags) > 0 {
		out.InputBags = make([]BagSpec, len(signature.InputBags))
		for i, bag := range signature.InputBags {
			out.InputBags[i] = normalizeBagSpec(bag)
		}
	}
	if len(signature.OutputBags) > 0 {
		out.OutputBags = make([]BagSpec, len(signature.OutputBags))
		for i, bag := range signature.OutputBags {
			out.OutputBags[i] = normalizeBagSpec(bag)
		}
	}
	if len(signature.Throws) > 0 {
		out.Throws = make([]ExceptionSpec, len(signature.Throws))
		for i, exception := range signature.Throws {
			out.Throws[i] = normalizeExceptionSpec(exception)
		}
	}
	if len(signature.ExportedHandlers) > 0 {
		out.ExportedHandlers = make([]HandlerExportSpec, len(signature.ExportedHandlers))
		for i, handler := range signature.ExportedHandlers {
			out.ExportedHandlers[i] = normalizeHandlerExportSpec(handler)
		}
	}
	return out
}

func normalizeStateSpec(state StateSpec) StateSpec {
	out := state
	if len(state.Proof.Sources) > 0 {
		out.Proof.Sources = make([]ProofSourceSpec, len(state.Proof.Sources))
		for i, source := range state.Proof.Sources {
			out.Proof.Sources[i] = normalizeProofSourceSpec(source)
		}
	}
	if len(state.Exposes.Bags) > 0 {
		out.Exposes.Bags = make([]BagSpec, len(state.Exposes.Bags))
		for i, bag := range state.Exposes.Bags {
			out.Exposes.Bags[i] = normalizeBagSpec(bag)
		}
	}
	if state.Next != nil {
		next := *state.Next
		if len(next.Cases) > 0 {
			next.Cases = make(map[string]NextCase, len(state.Next.Cases))
			for key, nextCase := range state.Next.Cases {
				next.Cases[key] = normalizeNextCase(nextCase)
			}
		}
		out.Next = &next
	}
	return out
}

func normalizeNextCase(nextCase NextCase) NextCase {
	out := nextCase
	if nextCase.Ref != nil {
		ref := *nextCase.Ref
		ref.KeepBags = normalizeBagSpecs(ref.KeepBags)
		ref.ReplaceBags = normalizeBagSpecs(ref.ReplaceBags)
		out.Ref = &ref
	}
	return out
}

func normalizeTransitionSpec(transition TransitionSpec) TransitionSpec {
	out := transition
	out.InputBags = normalizeBagSpecs(transition.InputBags)
	out.OutputBags = normalizeBagSpecs(transition.OutputBags)
	out.OutputHandlers = normalizeHandlerBindingSpecs(transition.OutputHandlers)
	if len(transition.OutputBagsByResult) > 0 {
		out.OutputBagsByResult = make(map[string][]BagSpec, len(transition.OutputBagsByResult))
		for result, bags := range transition.OutputBagsByResult {
			out.OutputBagsByResult[strings.TrimSpace(result)] = normalizeBagSpecs(bags)
		}
	}
	if transition.Foreach != nil {
		foreach := *transition.Foreach
		foreach.ItemsFrom = normalizeForeachItemsFromSpec(foreach.ItemsFrom)
		out.Foreach = &foreach
	}
	return out
}

func normalizePipelineHandlerSpecs(handlers []PipelineHandlerSpec) []PipelineHandlerSpec {
	if len(handlers) == 0 {
		return handlers
	}
	out := make([]PipelineHandlerSpec, len(handlers))
	for i, handler := range handlers {
		out[i] = normalizePipelineHandlerSpec(handler)
	}
	return out
}

func normalizePipelineHandlerSpec(handler PipelineHandlerSpec) PipelineHandlerSpec {
	handler.Name = strings.TrimSpace(handler.Name)
	handler.Transition = strings.TrimSpace(handler.Transition)
	handler.Handles = uniqueNonEmptyStrings(handler.Handles)
	handler.ReplaceBags = normalizeBagSpecs(handler.ReplaceBags)
	if len(handler.InputBags) > 0 {
		inputs := make([]HandlerInputBagSpec, len(handler.InputBags))
		for i, input := range handler.InputBags {
			input.Name = strings.TrimSpace(input.Name)
			input.Source = strings.TrimSpace(input.Source)
			inputs[i] = input
		}
		handler.InputBags = inputs
	}
	handler.Resume = normalizeResumePolicySpec(handler.Resume)
	return handler
}

func normalizeExceptionSpec(exception ExceptionSpec) ExceptionSpec {
	exception.Result = strings.TrimSpace(exception.Result)
	exception.Bags = normalizeBagSpecs(exception.Bags)
	return exception
}

func normalizeHandlerExportSpec(handler HandlerExportSpec) HandlerExportSpec {
	handler.Name = strings.TrimSpace(handler.Name)
	handler.Handler = strings.TrimSpace(handler.Handler)
	handler.Handles = uniqueNonEmptyStrings(handler.Handles)
	handler.Replaces = normalizeBagSpecs(handler.Replaces)
	handler.IndexedBy = uniqueNonEmptyStrings(handler.IndexedBy)
	return handler
}

func normalizeHandlerBindingSpecs(handlers []HandlerBindingSpec) []HandlerBindingSpec {
	if len(handlers) == 0 {
		return handlers
	}
	out := make([]HandlerBindingSpec, len(handlers))
	for i, handler := range handlers {
		handler.Name = strings.TrimSpace(handler.Name)
		handler.FromHandler = strings.TrimSpace(handler.FromHandler)
		handler.IndexedBy = uniqueNonEmptyStrings(handler.IndexedBy)
		out[i] = handler
	}
	return out
}

func normalizeExceptionHandlerPolicySpec(policy ExceptionHandlerPolicySpec) ExceptionHandlerPolicySpec {
	policy.Select = strings.TrimSpace(policy.Select)
	policy.Join = strings.TrimSpace(policy.Join)
	policy.OnUnhandled = strings.TrimSpace(policy.OnUnhandled)
	policy.Resume = normalizeResumePolicySpec(policy.Resume)
	return policy
}

func normalizeResumePolicySpec(policy ResumePolicySpec) ResumePolicySpec {
	policy.Type = strings.TrimSpace(policy.Type)
	return policy
}

func normalizeBagSpecs(bags []BagSpec) []BagSpec {
	if len(bags) == 0 {
		return bags
	}
	out := make([]BagSpec, len(bags))
	for i, bag := range bags {
		out[i] = normalizeBagSpec(bag)
	}
	return out
}

func normalizeBagSpec(bag BagSpec) BagSpec {
	bag.Name = strings.TrimSpace(bag.Name)
	bag.FromReturn = strings.TrimSpace(bag.FromReturn)
	return bag
}

func normalizeProofSourceSpec(source ProofSourceSpec) ProofSourceSpec {
	source.Name = strings.TrimSpace(source.Name)
	return source
}

func normalizeForeachItemsFromSpec(itemsFrom ForeachItemsFromSpec) ForeachItemsFromSpec {
	itemsFrom.Name = strings.TrimSpace(itemsFrom.Name)
	return itemsFrom
}

func canonicalizeSignatureSpecBags(signature SignatureSpec, scope namespaceBagScope) SignatureSpec {
	signature.InputBags = canonicalizeBagSpecs(signature.InputBags, scope)
	signature.OutputBags = canonicalizeBagSpecs(signature.OutputBags, scope)
	return signature
}

func canonicalizeStateSpecBags(state StateSpec, scope namespaceBagScope) StateSpec {
	state.Exposes.Bags = canonicalizeBagSpecs(state.Exposes.Bags, scope)
	if len(state.Proof.Sources) > 0 {
		sources := make([]ProofSourceSpec, len(state.Proof.Sources))
		for i, source := range state.Proof.Sources {
			sources[i] = canonicalizeProofSourceSpec(source, scope)
		}
		state.Proof.Sources = sources
	}
	if state.Next != nil && len(state.Next.Cases) > 0 {
		next := *state.Next
		next.Cases = make(map[string]NextCase, len(state.Next.Cases))
		for key, nextCase := range state.Next.Cases {
			next.Cases[key] = canonicalizeNextCaseBags(nextCase, scope)
		}
		state.Next = &next
	}
	return state
}

func canonicalizeNextCaseBags(nextCase NextCase, scope namespaceBagScope) NextCase {
	if nextCase.Ref == nil {
		return nextCase
	}
	ref := *nextCase.Ref
	ref.KeepBags = canonicalizeBagSpecs(ref.KeepBags, scope)
	ref.ReplaceBags = canonicalizeBagSpecs(ref.ReplaceBags, scope)
	nextCase.Ref = &ref
	return nextCase
}

func canonicalizeTransitionSpecBags(transition TransitionSpec, scope namespaceBagScope) TransitionSpec {
	transition.InputBags = canonicalizeBagSpecs(transition.InputBags, scope)
	transition.OutputBags = canonicalizeBagSpecs(transition.OutputBags, scope)
	if len(transition.OutputBagsByResult) > 0 {
		out := make(map[string][]BagSpec, len(transition.OutputBagsByResult))
		for result, bags := range transition.OutputBagsByResult {
			out[result] = canonicalizeBagSpecs(bags, scope)
		}
		transition.OutputBagsByResult = out
	}
	if transition.Foreach != nil {
		foreach := *transition.Foreach
		foreach.ItemsFrom = canonicalizeForeachItemsFromSpec(foreach.ItemsFrom, scope)
		transition.Foreach = &foreach
	}
	return transition
}

func canonicalizeBagSpecs(bags []BagSpec, scope namespaceBagScope) []BagSpec {
	if len(bags) == 0 {
		return bags
	}
	out := make([]BagSpec, len(bags))
	for i, bag := range bags {
		out[i] = canonicalizeBagSpec(bag, scope)
	}
	return out
}

func canonicalizeBagSpec(bag BagSpec, scope namespaceBagScope) BagSpec {
	bag = normalizeBagSpec(bag)
	if bag.Name == "" {
		return bag
	}
	match, ok := scope.Lookup(bag.Name)
	if !ok {
		return bag
	}
	bag.Name = match.Name
	return bag
}

func canonicalizeProofSourceSpec(source ProofSourceSpec, scope namespaceBagScope) ProofSourceSpec {
	source = normalizeProofSourceSpec(source)
	if source.Name == "" {
		return source
	}
	match, ok := scope.Lookup(source.Name)
	if !ok {
		return source
	}
	source.Name = match.Name
	return source
}

func canonicalizeForeachItemsFromSpec(itemsFrom ForeachItemsFromSpec, scope namespaceBagScope) ForeachItemsFromSpec {
	itemsFrom = normalizeForeachItemsFromSpec(itemsFrom)
	if itemsFrom.Name == "" {
		return itemsFrom
	}
	match, ok := scope.Lookup(itemsFrom.Name)
	if !ok {
		return itemsFrom
	}
	itemsFrom.Name = match.Name
	return itemsFrom
}

func inferNamespaceBags(def PipelineDefSpec) []BagSpec {
	bagsByName := make(map[string]BagSpec)
	add := func(bag BagSpec) {
		bag = normalizeBagSpec(bag)
		primary := inferredNamespaceBagName(bag)
		if primary == "" {
			return
		}
		candidate := BagSpec{
			Name:       primary,
			Collection: bag.Collection,
			IndexedBy:  append([]string(nil), bag.IndexedBy...),
		}
		if existing, ok := bagsByName[primary]; ok {
			bagsByName[primary] = mergeInferredNamespaceBag(existing, candidate)
			return
		}
		bagsByName[primary] = candidate
	}

	for _, bag := range def.Signature.InputBags {
		add(bag)
	}
	for _, bag := range def.Signature.OutputBags {
		add(bag)
	}
	for _, exception := range def.Signature.Throws {
		for _, bag := range exception.Bags {
			add(bag)
		}
	}
	for _, handler := range def.Signature.ExportedHandlers {
		for _, bag := range handler.Replaces {
			add(bag)
		}
	}
	for _, handler := range def.Handlers {
		for _, bag := range handler.ReplaceBags {
			add(bag)
		}
		for _, input := range handler.InputBags {
			add(BagSpec{Name: input.Name})
		}
	}
	for _, state := range def.States {
		for _, bag := range state.Exposes.Bags {
			add(bag)
		}
		if state.Next != nil {
			for _, nextCase := range state.Next.Cases {
				if nextCase.Ref == nil {
					continue
				}
				for _, bag := range nextCase.Ref.KeepBags {
					add(bag)
				}
				for _, bag := range nextCase.Ref.ReplaceBags {
					add(bag)
				}
			}
		}
	}
	for _, transition := range def.Transitions {
		for _, bag := range transition.InputBags {
			add(bag)
		}
		for _, bag := range transition.OutputBags {
			add(bag)
		}
		for _, bags := range transition.OutputBagsByResult {
			for _, bag := range bags {
				add(bag)
			}
		}
	}

	if len(bagsByName) == 0 {
		return nil
	}
	out := make([]BagSpec, 0, len(bagsByName))
	for _, bag := range bagsByName {
		out = append(out, bag)
	}
	return out
}

func mergeInferredNamespaceBag(existing BagSpec, candidate BagSpec) BagSpec {
	existing.Collection = existing.Collection || candidate.Collection
	existing.IndexedBy = uniqueNonEmptyStrings(append(existing.IndexedBy, candidate.IndexedBy...))
	return existing
}

func inferredNamespaceBagName(bag BagSpec) string {
	return bag.Name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
