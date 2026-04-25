package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScopedLocalStoreAllowsReadWithinRunRoot(t *testing.T) {
	runRoot := t.TempDir()
	workspaceRoot := filepath.Join(runRoot, "agents", "pm01")
	if err := os.MkdirAll(filepath.Join(runRoot, "shared"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(runRoot, "shared", "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := &ScopedLocalStore{
		RunRoot:       runRoot,
		WorkspaceRoot: workspaceRoot,
	}
	got, err := store.Read(context.Background(), "shared/note.txt")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("Read() = %q, want %q", string(got), "hello")
	}
}

func TestScopedLocalStoreRejectsWriteOutsideWorkspace(t *testing.T) {
	runRoot := t.TempDir()
	workspaceRoot := filepath.Join(runRoot, "agents", "pm01")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	store := &ScopedLocalStore{
		RunRoot:       runRoot,
		WorkspaceRoot: workspaceRoot,
	}
	if err := store.Write(context.Background(), "../ceo/note.txt", []byte("bad")); err == nil {
		t.Fatalf("Write() error = nil, want non-nil")
	}
}
