package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeTestFilesWritesSafeRelativeFiles(t *testing.T) {
	worktree := t.TempDir()
	bundle := TestFileBundle{
		SchemaVersion: 1,
		Kind:          "seed_tests",
		ModuleID:      "module01",
		TestCommand:   "node test/module01.seed.test.js",
		TestFiles: []TestFile{
			{
				Path:    "test/module01.seed.test.js",
				Content: "console.log('seed ok');\n",
			},
		},
	}

	parsed, err := ParseTestFileBundle([]byte(`{"schema_version":1,"kind":"seed_tests","module_id":"module01","test_command":"node test/module01.seed.test.js","test_files":[{"path":"test/module01.seed.test.js","content":"console.log('seed ok');\n"}]}`))
	if err != nil {
		t.Fatalf("ParseTestFileBundle returned error: %v", err)
	}
	if parsed.Kind != bundle.Kind || parsed.ModuleID != bundle.ModuleID {
		t.Fatalf("parsed bundle = %+v, want kind/module_id from json", parsed)
	}
	if err := ValidateTestFileBundle(parsed, "seed_tests"); err != nil {
		t.Fatalf("ValidateTestFileBundle returned error: %v", err)
	}

	written, err := MaterializeTestFiles(worktree, parsed)
	if err != nil {
		t.Fatalf("MaterializeTestFiles returned error: %v", err)
	}
	if got, want := len(written), 1; got != want {
		t.Fatalf("written count = %d, want %d", got, want)
	}
	content, err := os.ReadFile(filepath.Join(worktree, "test", "module01.seed.test.js"))
	if err != nil {
		t.Fatalf("seed test was not written: %v", err)
	}
	if string(content) != bundle.TestFiles[0].Content {
		t.Fatalf("seed content = %q, want %q", string(content), bundle.TestFiles[0].Content)
	}
	if got, want := BundleTestCommand(parsed), "node test/module01.seed.test.js"; got != want {
		t.Fatalf("BundleTestCommand = %q, want %q", got, want)
	}
}

func TestMaterializeTestFilesRejectsUnsafePaths(t *testing.T) {
	worktree := t.TempDir()
	unsafePaths := []string{
		"",
		"../outside.test.js",
		"test/../outside.test.js",
		"C:/absolute/test.js",
		".git/hooks/pre-commit",
		".devflow/result.json",
	}

	for _, unsafePath := range unsafePaths {
		t.Run(strings.ReplaceAll(unsafePath, "/", "_"), func(t *testing.T) {
			bundle := TestFileBundle{
				SchemaVersion: 1,
				Kind:          "full_test_files",
				ModuleID:      "module01",
				TestCommand:   "node test/module01.full.test.js",
				TestFiles: []TestFile{
					{Path: unsafePath, Content: "console.log('unsafe');\n"},
				},
			}
			if _, err := MaterializeTestFiles(worktree, bundle); err == nil {
				t.Fatalf("MaterializeTestFiles accepted unsafe path %q", unsafePath)
			}
		})
	}
}
