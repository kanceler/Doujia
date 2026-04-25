package app

import "testing"

func TestNewBootstrap(t *testing.T) {
	bootstrap := NewBootstrap(t.TempDir())
	if bootstrap.Modules.RunManager == nil {
		t.Fatalf("RunManager should not be nil")
	}
	if bootstrap.Modules.SessionRuntime == nil {
		t.Fatalf("SessionRuntime should not be nil")
	}
	if bootstrap.Modules.MessageGateway == nil {
		t.Fatalf("MessageGateway should not be nil")
	}
	if bootstrap.Modules.TaskRuntime == nil {
		t.Fatalf("TaskRuntime should not be nil")
	}
	if bootstrap.Internals.Orchestrator == nil {
		t.Fatalf("Orchestrator should not be nil")
	}
}
