package coder

import "testing"

func TestValidateTestCommandRejectsNaturalLanguageReportText(t *testing.T) {
	command := "python -m compileall todo_app todo.py; PowerShell CLI smoke test covering list/add/list/done/list"

	if err := validateTestCommand(command); err == nil {
		t.Fatal("validateTestCommand accepted a command containing natural-language test notes")
	}
}

func TestValidateTestCommandAcceptsExecutableShellCommand(t *testing.T) {
	command := "python -m compileall todo_app todo.py; python todo.py list"

	if err := validateTestCommand(command); err != nil {
		t.Fatalf("validateTestCommand rejected executable command: %v", err)
	}
}
