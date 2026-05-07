package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	agentcore "devflow/internal/agent/core"
)

func TestSubprocessHandlerRoundTrip(t *testing.T) {
	exe := buildSubprocessHelper(t)
	handler := NewSubprocessHandler(agentcore.HandlerSpec{
		ID:              "echo_handler",
		ExecutionDriver: "subprocess",
		ImplRef:         exe,
	})

	resp, err := handler.Handle(context.Background(), agentcore.HandlerRequest{
		Args: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := resp.Data["text"]; got != "hello" {
		t.Fatalf("response text = %#v, want hello", got)
	}
}

func TestSubprocessRoleRoundTrip(t *testing.T) {
	exe := buildSubprocessHelper(t)
	agent := NewSubprocessRoleAgent(agentcore.RoleSpec{
		ID:              "external_pm",
		ExecutionDriver: "subprocess",
		DriverRef:       exe,
	})

	result, err := agent.Run(context.Background(), agentcore.AgentRunRequest{
		Task: agentcore.Task{Role: "external_pm", Op: "write_plan"},
		Bundle: agentcore.AgentInputBundle{
			OutputDir: t.TempDir(),
		},
		OpSpec: agentcore.OpSpec{Role: "external_pm", Op: "write_plan"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("result = %q, want kok", result.Result)
	}
}

func buildSubprocessHelper(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	exe := filepath.Join(dir, "helper")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(source, []byte(subprocessHelperSource), 0o644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", exe, source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}
	return exe
}

const subprocessHelperSource = `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	var envelope struct {
		Protocol string ` + "`json:\"protocol\"`" + `
		Request struct {
			Args map[string]any ` + "`json:\"args\"`" + `
		} ` + "`json:\"request\"`" + `
	}
	if err := json.NewDecoder(os.Stdin).Decode(&envelope); err != nil {
		fmt.Printf(` + "`" + `{"protocol":"error","error":%q}` + "`" + `, err.Error())
		return
	}
	switch envelope.Protocol {
	case "doujia.agent.handler/v1":
		text, _ := envelope.Request.Args["text"].(string)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"protocol": "doujia.agent.handler_result/v1",
			"response": map[string]any{
				"data": map[string]any{"text": text},
			},
		})
	case "doujia.agent.role/v1":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"protocol": "doujia.agent.role_result/v1",
			"result": map[string]any{
				"result": "kok",
			},
		})
	default:
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"protocol": "error",
			"error": "unsupported protocol",
		})
	}
}
`
