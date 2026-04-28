package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	runGitForTest(t, repoDir, "init", "-b", "main")
	runGitForTest(t, repoDir, "config", "user.name", "DevFlow Test")
	runGitForTest(t, repoDir, "config", "user.email", "devflow@example.local")
	return repoDir
}

func runGitForTest(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	result, err := runGit(context.Background(), repoDir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(result.Stdout)
}

func writeFileForTest(t *testing.T, repoDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
}

func commitAllForTest(t *testing.T, repoDir, message string) string {
	t.Helper()
	runGitForTest(t, repoDir, "add", "-A")
	runGitForTest(t, repoDir, "commit", "-m", message)
	return runGitForTest(t, repoDir, "rev-parse", "HEAD")
}

func TestGitRevParseHEAD(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "main.txt", "base\n")
	want := commitAllForTest(t, repoDir, "base")

	got, err := GitRevParseHEAD(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("GitRevParseHEAD() error = %v", err)
	}
	if got != want {
		t.Fatalf("GitRevParseHEAD() = %q, want %q", got, want)
	}
}

func TestGitIsClean(t *testing.T) {
	t.Run("tracked change", func(t *testing.T) {
		repoDir := initTestRepo(t)
		writeFileForTest(t, repoDir, "main.txt", "base\n")
		commitAllForTest(t, repoDir, "base")
		writeFileForTest(t, repoDir, "main.txt", "changed\n")

		clean, err := GitIsClean(context.Background(), repoDir)
		if err != nil {
			t.Fatalf("GitIsClean() error = %v", err)
		}
		if clean {
			t.Fatal("GitIsClean() = true, want false")
		}
	})

	t.Run("untracked file", func(t *testing.T) {
		repoDir := initTestRepo(t)
		writeFileForTest(t, repoDir, "main.txt", "base\n")
		commitAllForTest(t, repoDir, "base")
		writeFileForTest(t, repoDir, "extra.txt", "extra\n")

		clean, err := GitIsClean(context.Background(), repoDir)
		if err != nil {
			t.Fatalf("GitIsClean() error = %v", err)
		}
		if clean {
			t.Fatal("GitIsClean() = true, want false")
		}
	})
}

func TestGitCherryPickRangeAppliesAllCommits(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "main.txt", "base\n")
	base := commitAllForTest(t, repoDir, "base")

	runGitForTest(t, repoDir, "checkout", "-b", "feature")
	writeFileForTest(t, repoDir, "commit_a.txt", "a\n")
	commitAllForTest(t, repoDir, "commit a")
	writeFileForTest(t, repoDir, "commit_b.txt", "b\n")
	head := commitAllForTest(t, repoDir, "commit b")

	runGitForTest(t, repoDir, "checkout", "main")
	if err := GitCherryPickRange(context.Background(), repoDir, base, head); err != nil {
		t.Fatalf("GitCherryPickRange() error = %v", err)
	}
	for _, name := range []string{"commit_a.txt", "commit_b.txt"} {
		if _, err := os.Stat(filepath.Join(repoDir, name)); err != nil {
			t.Fatalf("expected %s after cherry-pick: %v", name, err)
		}
	}
}

func TestGitCherryPickAbortLeavesNoState(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "main.txt", "line\n")
	base := commitAllForTest(t, repoDir, "base")

	runGitForTest(t, repoDir, "checkout", "-b", "left")
	writeFileForTest(t, repoDir, "main.txt", "left\n")
	leftHead := commitAllForTest(t, repoDir, "left change")

	runGitForTest(t, repoDir, "checkout", "main")
	runGitForTest(t, repoDir, "checkout", "-b", "right", base)
	writeFileForTest(t, repoDir, "main.txt", "right\n")
	rightHead := commitAllForTest(t, repoDir, "right change")

	runGitForTest(t, repoDir, "checkout", "main")
	if err := GitCherryPickRange(context.Background(), repoDir, base, leftHead); err != nil {
		t.Fatalf("apply left branch: %v", err)
	}
	if err := GitCherryPickRange(context.Background(), repoDir, base, rightHead); err == nil {
		t.Fatal("GitCherryPickRange() conflict = nil, want error")
	}
	if err := GitCherryPickAbort(context.Background(), repoDir); err != nil {
		t.Fatalf("GitCherryPickAbort() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git", "CHERRY_PICK_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("CHERRY_PICK_HEAD still present, stat err = %v", err)
	}
}

func TestGitResetHardRestoresCommit(t *testing.T) {
	repoDir := initTestRepo(t)
	writeFileForTest(t, repoDir, "main.txt", "base\n")
	base := commitAllForTest(t, repoDir, "base")
	writeFileForTest(t, repoDir, "main.txt", "updated\n")
	commitAllForTest(t, repoDir, "update")

	if err := GitResetHard(context.Background(), repoDir, base); err != nil {
		t.Fatalf("GitResetHard() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repoDir, "main.txt"))
	if err != nil {
		t.Fatalf("ReadFile(main.txt) error = %v", err)
	}
	if got := strings.ReplaceAll(string(content), "\r\n", "\n"); got != "base\n" {
		t.Fatalf("main.txt = %q, want %q", got, "base\n")
	}
}
