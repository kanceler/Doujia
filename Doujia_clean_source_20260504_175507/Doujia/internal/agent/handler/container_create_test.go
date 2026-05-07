package handler

import (
	"errors"
	"testing"
)

func TestIsDockerNoSuchContainerError(t *testing.T) {
	t.Parallel()

	if !isDockerNoSuchContainerError(errors.New("docker rm -f foo: Error response from daemon: No such container: foo")) {
		t.Fatal("isDockerNoSuchContainerError() = false, want true for missing container")
	}
	if isDockerNoSuchContainerError(errors.New("docker rm -f foo: permission denied")) {
		t.Fatal("isDockerNoSuchContainerError() = true, want false for unrelated error")
	}
}
