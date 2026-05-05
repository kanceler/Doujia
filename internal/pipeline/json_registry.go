package pipeline

import (
	"context"
	"fmt"
	"sync"

	"devflow/internal/core"
)

type DefinitionRegistry interface {
	GetDef(ctx context.Context, id core.PipelineID) (PipelineDefSpec, error)
	Entry(ctx context.Context) (PipelineDefSpec, error)
	Spec() RegistrySpec
}

type JSONRegistry struct {
	mu   sync.RWMutex
	spec RegistrySpec
	defs map[core.PipelineID]PipelineDefSpec
}

func LoadJSONRegistry(path string) (*JSONRegistry, error) {
	spec, err := LoadRegistrySpec(path)
	if err != nil {
		return nil, err
	}
	return NewJSONRegistry(spec)
}

func NewJSONRegistry(spec RegistrySpec) (*JSONRegistry, error) {
	spec = NormalizeRegistrySpec(spec)
	if err := ValidateRegistrySpec(spec); err != nil {
		return nil, err
	}
	defs := make(map[core.PipelineID]PipelineDefSpec, len(spec.PipelineDefs))
	for _, def := range spec.PipelineDefs {
		defs[def.PipelineID] = def
	}
	return &JSONRegistry{
		spec: spec,
		defs: defs,
	}, nil
}

func (r *JSONRegistry) GetDef(_ context.Context, id core.PipelineID) (PipelineDefSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.defs[id]
	if !ok {
		return PipelineDefSpec{}, fmt.Errorf("pipeline definition %q not found", id)
	}
	return def, nil
}

func (r *JSONRegistry) Entry(ctx context.Context) (PipelineDefSpec, error) {
	r.mu.RLock()
	entryID := r.spec.EntryPipelineID
	r.mu.RUnlock()
	return r.GetDef(ctx, entryID)
}

func (r *JSONRegistry) Spec() RegistrySpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := r.spec
	out.PipelineDefs = append([]PipelineDefSpec(nil), r.spec.PipelineDefs...)
	return out
}
