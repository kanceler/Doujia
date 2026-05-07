package app

import (
	"context"
	"fmt"

	agentbootstrap "devflow/internal/agent/bootstrap"
)

func (b *Bootstrap) PluginValidationResult(ctx context.Context, jobID string) (agentbootstrap.ValidationResult, error) {
	if b == nil || b.Internals.PluginRegistry == nil {
		return agentbootstrap.ValidationResult{}, fmt.Errorf("plugin registry is not configured")
	}
	return b.Internals.PluginRegistry.ValidationResult(jobID)
}
