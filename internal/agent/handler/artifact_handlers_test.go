package handler

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
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

func TestArtifactReadPrefersLatestDuplicateInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	oldPath := filepath.Join(dir, "coder_branch_old.json")
	newPath := filepath.Join(dir, "coder_branch_new.json")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old input: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new input: %v", err)
	}

	resp, err := handleArtifactRead(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{LogicalKey: core.LKCoderBranch, Path: oldPath},
				{LogicalKey: core.LKCoderBranch, Path: newPath},
			},
		},
		Args: map[string]any{
			"logical_key": core.LKCoderBranch,
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactRead() error = %v", err)
	}
	if got := resp.Data["content"]; got != "new" {
		t.Fatalf("handleArtifactRead() content = %v, want latest input", got)
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

func TestArtifactWriteSupportsDeclaredHTMLArtifactMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	resp, err := handleArtifactWrite(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			OutputDir:     dir,
			OutputURIBase: "projects/run_x/agents/front01/artifacts/preview",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{
					LogicalKey:  "preview_html",
					ObjectType:  "html",
					ContentType: "text/html; charset=utf-8",
					Encoding:    "utf-8",
					FileName:    "preview/index.html",
					Required:    true,
				},
			},
		},
		Args: map[string]any{
			"logical_key": "preview_html",
			"content":     "<!doctype html><title>Preview</title>",
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactWrite() error = %v", err)
	}
	if got := resp.Data["content_type"]; got != "text/html; charset=utf-8" {
		t.Fatalf("content_type = %v, want text/html; charset=utf-8", got)
	}
	if got := resp.Data["encoding"]; got != "utf-8" {
		t.Fatalf("encoding = %v, want utf-8", got)
	}
	target := filepath.Join(dir, "preview", "index.html")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", target, err)
	}
	if string(body) != "<!doctype html><title>Preview</title>" {
		t.Fatalf("html body = %q", string(body))
	}
}

func TestArtifactWriteAndReadSupportBase64BinaryArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	raw := []byte{0x00, 0x01, 0x02, 0xff}
	resp, err := handleArtifactWrite(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			OutputDir: dir,
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{
					LogicalKey:  "preview_image",
					ObjectType:  "binary",
					ContentType: "application/octet-stream",
					Encoding:    "base64",
					FileName:    "preview/image.bin",
					Required:    true,
				},
			},
		},
		Args: map[string]any{
			"logical_key":    "preview_image",
			"content_base64": base64.StdEncoding.EncodeToString(raw),
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactWrite() error = %v", err)
	}
	if got := resp.Data["encoding"]; got != "base64" {
		t.Fatalf("write encoding = %v, want base64", got)
	}
	path, ok := resp.Data["path"].(string)
	if !ok || path == "" {
		t.Fatalf("write path = %#v, want non-empty string", resp.Data["path"])
	}
	readResp, err := handleArtifactRead(context.Background(), core.HandlerRequest{
		Bundle: core.AgentInputBundle{
			Inputs: []core.InputArtifact{
				{
					LogicalKey:  "preview_image",
					Path:        path,
					ObjectType:  "binary",
					ContentType: "application/octet-stream",
					Encoding:    "base64",
				},
			},
		},
		Args: map[string]any{
			"logical_key": "preview_image",
		},
	})
	if err != nil {
		t.Fatalf("handleArtifactRead() error = %v", err)
	}
	if got := readResp.Data["content_base64"]; got != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("content_base64 = %v, want encoded binary", got)
	}
	if _, exists := readResp.Data["content"]; exists {
		t.Fatalf("read response has content for binary artifact: %#v", readResp.Data)
	}
	if got := readResp.Data["content_type"]; got != "application/octet-stream" {
		t.Fatalf("read content_type = %v, want application/octet-stream", got)
	}
}
