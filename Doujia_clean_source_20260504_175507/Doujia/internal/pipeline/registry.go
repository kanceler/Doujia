package pipeline

import (
	"context"
	"devflow/internal/core"
	"fmt"
	"sync"
)

type Registry interface {
	Get(ctx context.Context, id core.PipelineID) (PipelineSpec, error)
}

type MemoryRegistry struct {
	mu    sync.RWMutex
	items map[core.PipelineID]PipelineSpec
}

func NewMemoryRegistry(specs ...PipelineSpec) *MemoryRegistry {
	items := make(map[core.PipelineID]PipelineSpec, len(specs))
	for _, spec := range specs {
		items[spec.ID] = spec
	}
	return &MemoryRegistry{items: items}
}

func (r *MemoryRegistry) Get(_ context.Context, id core.PipelineID) (PipelineSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	spec, ok := r.items[id]
	if !ok {
		return PipelineSpec{}, fmt.Errorf("pipeline %q not found", id)
	}
	return spec, nil
}

