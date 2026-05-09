package real

import (
	"testing"

	agentcore "devflow/internal/agent/core"
)

func TestInferLogicalKeyFromURIRecognizesEnvironmentSpecBeforeArchitecturePath(t *testing.T) {
	uri := "projects/run/agents/architect01/artifacts/architect_write_architecture/environment_spec.json"

	if got := inferLogicalKeyFromURI(uri); got != agentcore.LKEnvironmentSpec {
		t.Fatalf("inferLogicalKeyFromURI(%q) = %q, want %q", uri, got, agentcore.LKEnvironmentSpec)
	}
}
