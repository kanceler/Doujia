package llm

import (
	"testing"

	"devflow/internal/agent/core"
	"devflow/internal/agent/handler"
)

func TestBuildToolsHidesArtifactWriteContentBase64FromLLM(t *testing.T) {
	tools, err := BuildTools(core.OpSpec{
		AllowedTools: []core.ToolSpec{{Name: "artifact_write"}},
	}, testHandlerRegistry{
		"artifact_write": handler.NewArtifactWriteHandler(),
	})
	if err != nil {
		t.Fatalf("BuildTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	properties, ok := tools[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("artifact_write properties missing or wrong type: %#v", tools[0].Parameters["properties"])
	}
	if _, ok := properties["content_base64"]; ok {
		t.Fatalf("artifact_write schema exposed content_base64 to LLM: %#v", properties)
	}
}

type testHandlerRegistry map[string]core.Handler

func (r testHandlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r[name]
	return handler, ok
}
