package common

import (
	"strings"
	"testing"
)

func TestOpenCodeArgsOmitsPromptForFakeOpenCode(t *testing.T) {
	t.Setenv("DEVFLOW_FAKE_OPENCODE", "1")
	prompt := strings.Repeat("long prompt ", 5000)

	args := openCodeArgs(OpenCodeRequest{Prompt: prompt, Model: "gpt-5.5"})

	for _, arg := range args {
		if strings.Contains(arg, "long prompt") {
			t.Fatalf("fake opencode args included prompt; args length=%d", len(strings.Join(args, " ")))
		}
	}
	if len(strings.Join(args, " ")) > 1000 {
		t.Fatalf("fake opencode args are unexpectedly long: %d", len(strings.Join(args, " ")))
	}
}
