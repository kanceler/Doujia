package llm

import (
	"testing"

	"doujia/internal/agent/core"
)

func TestHasForbiddenPathArgRejectsFileName(t *testing.T) {
	t.Parallel()

	if !hasForbiddenPathArg(map[string]any{"file_name": "bad.json"}) {
		t.Fatal("hasForbiddenPathArg() = false, want true for file_name")
	}
}

func TestOutputFromToolResponseIncludesArtifactURI(t *testing.T) {
	t.Parallel()

	output, ok := outputFromToolResponse(core.HandlerResponse{
		Data: map[string]any{
			"logical_key":  "delivery_guide",
			"object_type":  "markdown",
			"status":       "produced",
			"path":         `C:\tmp\delivery_guide.md`,
			"artifact_uri": "projects/run_x/agents/architect01/artifacts/test_code/delivery_guide.md",
		},
	})
	if !ok {
		t.Fatal("outputFromToolResponse() ok = false, want true")
	}
	if output.ArtifactURI == "" {
		t.Fatal("outputFromToolResponse() artifact_uri = empty, want populated")
	}
}
