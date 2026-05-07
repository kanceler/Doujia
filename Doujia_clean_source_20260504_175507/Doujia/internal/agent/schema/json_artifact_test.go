package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
)

func TestValidateJSONArtifactBytesRejectsBOM(t *testing.T) {
	t.Parallel()

	body := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"result":"kok"}`)...)
	err := ValidateJSONArtifactBytes(core.LKGlobalTestReport, body)
	if err == nil {
		t.Fatal("ValidateJSONArtifactBytes() error = nil, want BOM rejection")
	}
	if !strings.Contains(err.Error(), "without BOM") {
		t.Fatalf("ValidateJSONArtifactBytes() error = %v, want BOM detail", err)
	}
}

func TestValidateJSONArtifactBytesRejectsMarkdownFence(t *testing.T) {
	t.Parallel()

	err := ValidateJSONArtifactBytes(core.LKFullTestFiles, []byte("```json\n{}\n```"))
	if err == nil {
		t.Fatal("ValidateJSONArtifactBytes() error = nil, want Markdown fence rejection")
	}
	if !strings.Contains(err.Error(), "Markdown") {
		t.Fatalf("ValidateJSONArtifactBytes() error = %v, want Markdown detail", err)
	}
}

func TestValidateJSONArtifactBytesRejectsMalformedKnownArtifact(t *testing.T) {
	t.Parallel()

	err := ValidateJSONArtifactBytes(core.LKGlobalTestReport, []byte(`{"kind":"global_test_report","result":"kok","test_passed":true}`))
	if err == nil {
		t.Fatal("ValidateJSONArtifactBytes() error = nil, want missing tested_commit")
	}
	if !strings.Contains(err.Error(), "tested_commit") {
		t.Fatalf("ValidateJSONArtifactBytes() error = %v, want tested_commit detail", err)
	}
}

func TestValidateJSONArtifactBytesAcceptsModuleSpecPattern(t *testing.T) {
	t.Parallel()

	err := ValidateJSONArtifactBytes("module01_spec", []byte(`{
		"module_id":"module01",
		"module_name":"frontend",
		"module_role":"frontend",
		"implementation_role":"front",
		"branch_name":"feature/module01",
		"worktree_dir":"/workspace/worktrees/module01",
		"owned_paths":["miniprogram/**"],
		"test_command":"npm test",
		"complexity":"high"
	}`))
	if err != nil {
		t.Fatalf("ValidateJSONArtifactBytes() error = %v", err)
	}
}

func TestValidateJSONArtifactFileUsesSameRules(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "global_test_report.json")
	if err := os.WriteFile(path, []byte(`{"kind":"global_test_report","result":"kok","test_passed":true,"tested_commit":"abc123"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := ValidateJSONArtifactFile(core.LKGlobalTestReport, path); err != nil {
		t.Fatalf("ValidateJSONArtifactFile() error = %v", err)
	}
}

func TestJSONArtifactExampleCoversDynamicModuleSpec(t *testing.T) {
	t.Parallel()

	example := JSONArtifactExample("module02_spec")
	for _, want := range []string{"module_id", "implementation_role", "owned_paths", "complexity"} {
		if !strings.Contains(example, want) {
			t.Fatalf("JSONArtifactExample() = %q, missing %q", example, want)
		}
	}
}
