package handler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doujia/internal/agent/core"
)

func TestArtifactReadPrefersInputsOverPreviousOutputs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := dir + `\input.json`
	previousPath := dir + `\previous.json`
	if err := os.WriteFile(inputPath, []byte("from-input"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(previousPath, []byte("from-previous"), 0o644); err != nil {
		t.Fatalf("write previous: %v", err)
	}

	resp, err := handleArtifactRead(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: "same_key", Path: inputPath},
			},
			PreviousOutputs: []core.PreviousOutputRef{
				{LogicalKey: "same_key", Path: previousPath},
			},
		},
		Args: map[string]any{
			"logical_key": "same_key",
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactRead() error = %v", err)
	}
	if got := resp.Data["content"]; got != "from-input" {
		t.Fatalf("handleArtifactRead() content = %v, want from-input", got)
	}
}

func TestArtifactReadRejectsFileNameArg(t *testing.T) {
	t.Parallel()

	_, err := handleArtifactRead(context.Background(), core.HandlerRequest{
		Args: map[string]any{
			"logical_key": "any",
			"file_name":   "bad.txt",
		},
	})
	if err == nil {
		t.Fatal("handleArtifactRead() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "file_name") {
		t.Fatalf("handleArtifactRead() error = %v, want file_name rejection", err)
	}
}

func TestArtifactWriteRejectsFileNameArg(t *testing.T) {
	t.Parallel()

	_, err := handleArtifactWrite(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			OutputDir: t.TempDir(),
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{
					LogicalKey: "report",
					ObjectType: "json",
					FileName:   "report.json",
					Required:   true,
				},
			},
		},
		Args: map[string]any{
			"logical_key": "report",
			"content":     "{}",
			"file_name":   "bad.json",
		},
	})
	if err == nil {
		t.Fatal("handleArtifactWrite() error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "file_name") {
		t.Fatalf("handleArtifactWrite() error = %v, want file_name rejection", err)
	}
}

func TestArtifactWriteReturnsArtifactURI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	resp, err := handleArtifactWrite(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			OutputDir:     dir,
			OutputURIBase: "projects/run_x/agents/architect01/artifacts/test_code",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{
					LogicalKey: "delivery_guide",
					ObjectType: "markdown",
					FileName:   "delivery_guide.md",
					Required:   true,
				},
			},
		},
		Args: map[string]any{
			"logical_key": "delivery_guide",
			"content":     "# guide\n",
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactWrite() error = %v", err)
	}
	if got := resp.Data["artifact_uri"]; got != "projects/run_x/agents/architect01/artifacts/test_code/delivery_guide.md" {
		t.Fatalf("handleArtifactWrite() artifact_uri = %v", got)
	}
	if got := resp.Data["path"]; got != filepath.Join(dir, "delivery_guide.md") {
		t.Fatalf("handleArtifactWrite() path = %v, want %v", got, filepath.Join(dir, "delivery_guide.md"))
	}
}
