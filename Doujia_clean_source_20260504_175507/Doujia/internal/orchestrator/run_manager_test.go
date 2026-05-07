package orchestrator

import (
	"testing"

	"devflow/internal/core"
)

func TestRunManagerNormalizeDeliveryConfigSetsGlobalTestTimeoutOnly(t *testing.T) {
	config := normalizeDeliveryConfig(core.DeliveryConfig{})
	if config.GlobalTestTimeoutSeconds != 60 {
		t.Fatalf("GlobalTestTimeoutSeconds = %d, want 60", config.GlobalTestTimeoutSeconds)
	}
	if len(config.GlobalVerifyCommands) != 0 {
		t.Fatalf("GlobalVerifyCommands = %v, want empty", config.GlobalVerifyCommands)
	}
}
