package spec_test

import (
	"strings"
	"testing"

	coderspec "devflow/internal/agent/spec/coder"
	frontspec "devflow/internal/agent/spec/front"
)

func TestWriteCodeSpecsTellAgentsToCreateMissingOwnedFiles(t *testing.T) {
	specs := map[string]string{
		"coder.write_code": coderspec.WriteCodeSpec().OpDescription,
		"front.write_code": frontspec.WriteCodeSpec().OpDescription,
	}
	for name, description := range specs {
		for _, want := range []string{
			"Missing files or directories under owned_paths are normal for a new local project",
			"create them with container_write",
			"Do not fail solely because expected owned_paths do not already exist",
		} {
			if !strings.Contains(description, want) {
				t.Fatalf("%s OpDescription missing %q:\n%s", name, want, description)
			}
		}
	}
}
