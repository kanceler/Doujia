package agentengine

import "testing"

func TestParseModelOutputAcceptsExactJSON(t *testing.T) {
	raw := `{"summary":"ok","artifact_outputs":[{"type":"prd","filename":"plan_v1.md","content":"# Plan"}],"control":[]}`
	output, err := ParseModelOutput(raw)
	if err != nil {
		t.Fatalf("ParseModelOutput error = %v", err)
	}
	if output.Summary != "ok" {
		t.Fatalf("summary = %q, want ok", output.Summary)
	}
	if len(output.ArtifactOutputs) != 1 {
		t.Fatalf("artifact count = %d, want 1", len(output.ArtifactOutputs))
	}
}

func TestParseModelOutputAcceptsFencedJSON(t *testing.T) {
	raw := "```json\n{\"summary\":\"ok\",\"artifact_outputs\":[{\"type\":\"design\",\"filename\":\"architecture_v1.md\",\"content\":\"# Architecture\"}],\"control\":[]}\n```"
	output, err := ParseModelOutput(raw)
	if err != nil {
		t.Fatalf("ParseModelOutput error = %v", err)
	}
	if output.ArtifactOutputs[0].Filename != "architecture_v1.md" {
		t.Fatalf("filename = %q, want architecture_v1.md", output.ArtifactOutputs[0].Filename)
	}
}

func TestParseModelOutputAcceptsPrefixedAndSuffixedText(t *testing.T) {
	raw := "Result follows.\n\n{\"summary\":\"ok\",\"artifact_outputs\":[{\"type\":\"prd\",\"filename\":\"plan_v1.md\",\"content\":\"# Plan\"}],\"control\":[]}\n\nExtra note: artifact generated."
	output, err := ParseModelOutput(raw)
	if err != nil {
		t.Fatalf("ParseModelOutput error = %v", err)
	}
	if output.ArtifactOutputs[0].Type != "prd" {
		t.Fatalf("type = %q, want prd", output.ArtifactOutputs[0].Type)
	}
}

func TestParseModelOutputRejectsInvalidFilename(t *testing.T) {
	raw := `{"summary":"ok","artifact_outputs":[{"type":"prd","filename":"nested/plan_v1.md","content":"# Plan"}],"control":[]}`
	_, err := ParseModelOutput(raw)
	if err == nil {
		t.Fatal("ParseModelOutput error = nil, want error")
	}
}
