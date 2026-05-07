package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devflow/internal/agent/core"
	appcore "devflow/internal/core"
)

func TestHasForbiddenPathArgRejectsFileName(t *testing.T) {
	t.Parallel()

	if !hasForbiddenPathArg(map[string]any{"file_name": "bad.json"}) {
		t.Fatal("hasForbiddenPathArg() = false, want true for file_name")
	}
}

func TestOutputFromToolResponseIncludesArtifactURI(t *testing.T) {
	t.Parallel()

	output, ok := outputFromToolResponse(core.HandlerResponse{
		Data: map[string]any{
			"logical_key":  "delivery_guide",
			"object_type":  "markdown",
			"status":       "produced",
			"path":         `C:\tmp\delivery_guide.md`,
			"artifact_uri": "projects/run_x/agents/architect01/artifacts/test_code/delivery_guide.md",
		},
	})
	if !ok {
		t.Fatal("outputFromToolResponse() ok = false, want true")
	}
	if output.ArtifactURI == "" {
		t.Fatal("outputFromToolResponse() artifact_uri = empty, want populated")
	}
}

func TestFillProducedOutputPathsTreatsArtifactWritesAsCanonical(t *testing.T) {
	t.Parallel()

	result := core.AgentResult{
		Result: "kok",
		Outputs: []core.AgentOutput{
			{LogicalKey: "expanded_test_data", ObjectType: "markdown", Status: "success"},
		},
	}
	fillProducedOutputPaths(&result, map[string]core.AgentOutput{
		"expanded_test_data": {
			LogicalKey:  "expanded_test_data",
			ObjectType:  "markdown",
			Status:      "produced",
			Path:        "expanded_test_data.md",
			ArtifactURI: "projects/run/expanded_test_data.md",
		},
		"boundary_tests": {
			LogicalKey:  "boundary_tests",
			ObjectType:  "json",
			Status:      "produced",
			Path:        "boundary_tests.json",
			ArtifactURI: "projects/run/boundary_tests.json",
		},
	})

	if got := result.Outputs[0].Status; got != "produced" {
		t.Fatalf("status = %q, want produced", got)
	}
	if got := result.Outputs[0].ArtifactURI; got == "" {
		t.Fatal("artifact_uri = empty, want produced artifact uri")
	}
	if len(result.Outputs) != 2 {
		t.Fatalf("outputs len = %d, want appended produced output", len(result.Outputs))
	}
}

func TestNewToolLoopFromEnvUsesPositiveMaxTurns(t *testing.T) {
	t.Setenv("DOUJIA_TOOL_LOOP_MAX_TURNS", "5")

	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{ToolCalls: []ToolCall{{ID: "call_1", Name: "noop_tool"}}},
			{ToolCalls: []ToolCall{{ID: "call_2", Name: "noop_tool"}}},
			{ToolCalls: []ToolCall{{ID: "call_3", Name: "noop_tool"}}},
			{ToolCalls: []ToolCall{{ID: "call_4", Name: "noop_tool"}}},
			{ToolCalls: []ToolCall{{ID: "call_5", Name: "noop_tool"}}},
		},
	}
	loop := NewToolLoopFromEnv(adapter)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task:   core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		OpSpec: core.OpSpec{AllowedTools: []core.ToolSpec{{Name: "noop_tool"}}},
		Handlers: handlerRegistry{
			"noop_tool": staticHandler{name: "noop_tool", data: map[string]any{"ok": true}},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want structured kfail result", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || result.Errors[0].Code != "tool_loop_exceeded" || !strings.Contains(result.Errors[0].Message, "max_turns=5") {
		t.Fatalf("Run() errors = %+v, want max turn diagnostics", result.Errors)
	}
}

func TestToolLoopDirectFinalNormalizesStatusAliases(t *testing.T) {
	t.Parallel()

	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{Message: Message{Role: "assistant", Content: `{"result":"kok","outputs":[{"logical_key":"code_bag","object_type":"json","status":"success"},{"logical_key":"notes","object_type":"markdown","status":"ok"},{"logical_key":"previous","object_type":"json","status":"reuse"}]}`}},
		},
	}

	result, err := NewToolLoop(adapter).Run(context.Background(), core.ToolLoopRequest{
		Task:     core.Task{Role: "coder", Op: "write_code", ExecutionMode: "normal"},
		OpSpec:   core.OpSpec{},
		Handlers: handlerRegistry{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := []string{result.Outputs[0].Status, result.Outputs[1].Status, result.Outputs[2].Status}; got[0] != "produced" || got[1] != "produced" || got[2] != "reused" {
		t.Fatalf("output statuses = %#v, want produced/produced/reused", got)
	}
}

func TestToolLoopNonJSONFinalReturnsStructuredKfail(t *testing.T) {
	t.Parallel()

	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{Message: Message{Role: "assistant", Content: "done, all good"}},
		},
	}

	result, err := NewToolLoop(adapter).Run(context.Background(), core.ToolLoopRequest{
		Task:     core.Task{Role: "tester", Op: "test_code", ExecutionMode: "normal"},
		OpSpec:   core.OpSpec{},
		Handlers: handlerRegistry{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want structured kfail result", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(result.Errors) == 0 || result.Errors[0].Code != "final_json_parse_failed" {
		t.Fatalf("Run() errors = %+v, want final_json_parse_failed", result.Errors)
	}
	if !strings.Contains(result.Errors[0].Message, "role=tester") || !strings.Contains(result.Errors[0].Message, "op=test_code") || !strings.Contains(result.Errors[0].Message, "turn=1") {
		t.Fatalf("Run() error message = %q, want role/op/turn diagnostics", result.Errors[0].Message)
	}
}

func TestToolLoopTaskCompleteValidationRequestsRepair(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "artifact_write", Args: map[string]any{"logical_key": "expanded_test_data", "content": "# Expanded\n"}},
					{ID: "call_2", Name: "task_complete", Args: map[string]any{
						"result": "kok",
						"outputs": []any{
							map[string]any{"logical_key": "expanded_test_data", "object_type": "markdown", "status": "success"},
							map[string]any{"logical_key": "boundary_tests", "object_type": "json", "status": "success"},
						},
					}},
				},
			},
			{
				ToolCalls: []ToolCall{
					{ID: "call_3", Name: "artifact_write", Args: map[string]any{"logical_key": "boundary_tests", "content": "{}"}},
					{ID: "call_4", Name: "artifact_write", Args: map[string]any{"logical_key": "full_test_files", "content": `{"kind":"full_test_files","test_command":"npm test","files":[]}`}},
					{ID: "call_5", Name: "task_complete", Args: map[string]any{
						"result": "kok",
						"outputs": []any{
							map[string]any{"logical_key": "expanded_test_data", "object_type": "markdown", "status": "produced"},
							map[string]any{"logical_key": "boundary_tests", "object_type": "json", "status": "produced"},
							map[string]any{"logical_key": "full_test_files", "object_type": "json", "status": "produced"},
						},
					}},
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(4)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task: core.Task{Role: "tester", Op: "test_data", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir:     outputDir,
			OutputURIBase: "projects/run/agents/tester01/artifacts/test_data",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "expanded_test_data", ObjectType: "markdown", FileName: "expanded_test_data.md", Required: true},
				{LogicalKey: "boundary_tests", ObjectType: "json", FileName: "boundary_tests.json", Required: true},
				{LogicalKey: "full_test_files", ObjectType: "json", FileName: "full_test_files.json", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_write"},
				{Name: "task_complete"},
			},
		},
		Handlers: handlerRegistry{
			"artifact_write": artifactWriteHandler{},
			"task_complete":  taskCompleteEchoHandler{},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(adapter.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(adapter.requests))
	}
	secondMessages := adapter.requests[1].Messages
	if len(secondMessages) == 0 {
		t.Fatal("second request messages = empty, want validation feedback")
	}
	last := secondMessages[len(secondMessages)-1]
	if last.Role != "tool" || last.Name != "task_complete" || last.ToolCallID != "call_2" {
		t.Fatalf("last message = %+v, want task_complete tool error message", last)
	}
	if !strings.Contains(last.Content, "boundary_tests") {
		t.Fatalf("validation feedback = %q, want boundary_tests detail", last.Content)
	}
}

func TestToolLoopRequestsRewriteForInvalidJSONArtifactWrite(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "artifact_write", Args: map[string]any{"logical_key": core.LKFullTestFiles, "content": `{"files":[]}}`}},
				},
			},
			{
				ToolCalls: []ToolCall{
					{ID: "call_2", Name: "artifact_write", Args: map[string]any{"logical_key": core.LKFullTestFiles, "content": `{"kind":"full_test_files","test_command":"npm test","files":[]}`}},
					{ID: "call_3", Name: "task_complete", Args: map[string]any{
						"result": "kok",
						"outputs": []any{
							map[string]any{"logical_key": core.LKFullTestFiles, "object_type": "json", "status": "produced"},
						},
					}},
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(4)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task: core.Task{Role: "tester", Op: "test_data", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir:     outputDir,
			OutputURIBase: "projects/run/agents/tester01/artifacts/test_data",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: core.LKFullTestFiles, ObjectType: "json", FileName: "full_test_files.json", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_write"},
				{Name: "task_complete"},
			},
		},
		Handlers: handlerRegistry{
			"artifact_write": artifactWriteHandler{},
			"task_complete":  taskCompleteEchoHandler{},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(adapter.requests) != 2 {
		t.Fatalf("chat calls = %d, want rewrite feedback then retry", len(adapter.requests))
	}
	last := adapter.requests[1].Messages[len(adapter.requests[1].Messages)-1]
	if last.Role != "tool" || last.ToolCallID != "call_1" {
		t.Fatalf("last feedback message = %+v, want tool feedback for first artifact_write", last)
	}
	for _, want := range []string{"valid JSON", core.LKFullTestFiles, "rewrite", "Required contract", "Example", "test_command"} {
		if !strings.Contains(last.Content, want) {
			t.Fatalf("rewrite feedback = %q, want %q", last.Content, want)
		}
	}
}

func TestToolLoopTaskCompleteCanOmitOutputsWhenArtifactsAreAlreadyWritten(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "artifact_write", Args: map[string]any{"logical_key": "expanded_test_data", "content": "# Expanded\n"}}},
			},
			{
				ToolCalls: []ToolCall{
					{ID: "call_2", Name: "task_complete", Args: map[string]any{
						"result":  "kok",
						"message": "done",
					}},
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(3)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task: core.Task{Role: "tester", Op: "test_data", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir:     outputDir,
			OutputURIBase: "projects/run/agents/tester01/artifacts/test_data",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "expanded_test_data", ObjectType: "markdown", FileName: "expanded_test_data.md", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_write"},
				{Name: "task_complete"},
			},
		},
		Handlers: handlerRegistry{
			"artifact_write": artifactWriteHandler{},
			"task_complete":  taskCompleteEchoHandler{},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kok" {
		t.Fatalf("Run() result = %q, want kok", result.Result)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].LogicalKey != "expanded_test_data" {
		t.Fatalf("Run() outputs = %#v, want inferred expanded_test_data", result.Outputs)
	}
}

func TestToolLoopTaskCompleteInfersProducedBagsFromWrittenArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "artifact_write", Args: map[string]any{"logical_key": "pm_plan", "content": "# Plan\n"}},
				},
			},
			{
				ToolCalls: []ToolCall{
					{ID: "call_2", Name: "task_complete", Args: map[string]any{
						"result":  "kok",
						"message": "done",
					}},
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(3)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task: core.Task{Role: "pm", Op: "write_plan", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir:     outputDir,
			OutputURIBase: "projects/run/agents/pm01/artifacts/pm_write_plan",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "pm_plan", ObjectType: "markdown", FileName: "plan_v1.md", Required: true},
			},
			OutputBags: []core.OutputBagSpec{
				{Name: "product_plan", Required: true, Members: []core.BagMemberRequirement{{LogicalKey: "pm_plan", Required: true}}},
			},
			ProducedBagsResolver: func(core.Task, core.AgentInputBundle, core.AgentResult) []appcore.ProducedBagManifest {
				return []appcore.ProducedBagManifest{{
					Name:    "product_plan",
					Members: []appcore.ProducedBagMember{{LogicalKey: "pm_plan"}},
				}}
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_write"},
				{Name: "task_complete"},
			},
		},
		Handlers: handlerRegistry{
			"artifact_write": artifactWriteHandler{},
			"task_complete":  taskCompleteEchoHandler{},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := len(result.ProducedBags), 1; got != want {
		t.Fatalf("Run() produced bags len = %d, want %d", got, want)
	}
	if got := result.ProducedBags[0].Name; got != "product_plan" {
		t.Fatalf("Run() produced bag name = %q, want product_plan", got)
	}
}

func TestToolLoopArchitectValidationFeedbackUsesUserRole(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "artifact_write", Args: map[string]any{"logical_key": "global_test_data", "content": "# Global test data\n"}},
					{ID: "call_2", Name: "artifact_write", Args: map[string]any{"logical_key": "global_acceptance_tests", "content": "{\"kind\":\"global_acceptance_tests\",\"target\":\"merged_main_branch\",\"scenarios\":[{\"name\":\"smoke\"}]}"}},
					{ID: "call_3", Name: "artifact_write", Args: map[string]any{"logical_key": "global_test_commands", "content": "{\"kind\":\"global_test_commands\",\"commands\":[]}"}},
				},
			},
			{
				Message: Message{
					Role:    "assistant",
					Content: `{"result":"kfail","outputs":[],"errors":[{"code":"validation_feedback_seen","message":"saw validation feedback"}]}`,
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(3)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task: core.Task{Role: "architect", Op: "test_data", ExecutionMode: "normal"},
		Bundle: core.AgentInputBundle{
			OutputDir:     outputDir,
			OutputURIBase: "projects/run/agents/architect01/artifacts/test_data",
		},
		OpSpec: core.OpSpec{
			ExpectedOutputs: []core.OutputSpec{
				{LogicalKey: "global_test_data", ObjectType: "markdown", FileName: "global_test_data.md", Required: true},
				{LogicalKey: "global_acceptance_tests", ObjectType: "json", FileName: "global_acceptance_tests.json", Required: true},
				{LogicalKey: "global_test_commands", ObjectType: "json", FileName: "global_test_commands.json", Required: true},
			},
			AllowedTools: []core.ToolSpec{
				{Name: "artifact_write"},
			},
		},
		Handlers: handlerRegistry{
			"artifact_write": artifactWriteHandler{},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(adapter.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(adapter.requests))
	}
	secondMessages := adapter.requests[1].Messages
	if len(secondMessages) == 0 {
		t.Fatal("second request messages = empty, want validation feedback")
	}
	last := secondMessages[len(secondMessages)-1]
	if last.Role != "tool" {
		t.Fatalf("last message role = %q, want tool", last.Role)
	}
	if last.ToolCallID != "call_3" {
		t.Fatalf("last message tool_call_id = %q, want call_3", last.ToolCallID)
	}
	if !strings.Contains(last.Content, "global_test_commands.commands must be a non-empty array") {
		t.Fatalf("validation feedback = %q, want global_test_commands detail", last.Content)
	}
	if !strings.Contains(last.Content, "Required contract") || !strings.Contains(last.Content, "Example") {
		t.Fatalf("validation feedback = %q, want contract and example", last.Content)
	}
}

func TestToolLoopReturnsHandlerErrorsToModel(t *testing.T) {
	t.Parallel()

	adapter := &recordingAdapter{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "unstable_tool", Args: map[string]any{"value": ""}},
				},
			},
			{
				Message: Message{
					Role:    "assistant",
					Content: `{"result":"kfail","outputs":[],"errors":[{"code":"tool_arg_repaired","message":"saw tool error"}]}`,
				},
			},
		},
	}
	loop := NewToolLoop(adapter).WithMaxTurns(3)

	result, err := loop.Run(context.Background(), core.ToolLoopRequest{
		Task:   core.Task{Role: "architect", Op: "write_plan", ExecutionMode: "normal"},
		OpSpec: core.OpSpec{AllowedTools: []core.ToolSpec{{Name: "unstable_tool"}}},
		Handlers: handlerRegistry{
			"unstable_tool": errorHandler{name: "unstable_tool", err: fmt.Errorf("arg \"content\" must be a non-empty string")},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Result != "kfail" {
		t.Fatalf("Run() result = %q, want kfail", result.Result)
	}
	if len(adapter.requests) < 2 {
		t.Fatalf("Chat calls = %d, want at least 2", len(adapter.requests))
	}
	lastMessages := adapter.requests[1].Messages
	if len(lastMessages) == 0 || lastMessages[len(lastMessages)-1].Role != "tool" {
		t.Fatalf("last message before retry = %+v, want tool error", lastMessages)
	}
	if !strings.Contains(lastMessages[len(lastMessages)-1].Content, "non-empty string") {
		t.Fatalf("tool error content = %s, want handler error", lastMessages[len(lastMessages)-1].Content)
	}
}

type recordingAdapter struct {
	responses []ChatResponse
	requests  []ChatRequest
}

func (a *recordingAdapter) Chat(_ context.Context, req ChatRequest) (ChatResponse, error) {
	a.requests = append(a.requests, req)
	if len(a.requests) > len(a.responses) {
		return ChatResponse{}, fmt.Errorf("unexpected chat call %d", len(a.requests))
	}
	return a.responses[len(a.requests)-1], nil
}

type handlerRegistry map[string]core.Handler

func (r handlerRegistry) Get(name string) (core.Handler, bool) {
	handler, ok := r[name]
	return handler, ok
}

type errorHandler struct {
	name string
	err  error
}

func (h errorHandler) Name() string {
	return h.name
}

func (h errorHandler) Description() string {
	return "test handler"
}

func (h errorHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name: h.name,
		Parameters: map[string]any{
			"type": "object",
		},
	}
}

func (h errorHandler) Handle(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
	return core.HandlerResponse{}, h.err
}

type staticHandler struct {
	name string
	data map[string]any
}

func (h staticHandler) Name() string {
	return h.name
}

func (h staticHandler) Description() string {
	return "test handler"
}

func (h staticHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{
		Name: h.name,
		Parameters: map[string]any{
			"type": "object",
		},
	}
}

func (h staticHandler) Handle(context.Context, core.HandlerRequest) (core.HandlerResponse, error) {
	return core.HandlerResponse{Data: h.data}, nil
}

type artifactWriteHandler struct{}

func (h artifactWriteHandler) Name() string { return "artifact_write" }

func (h artifactWriteHandler) Description() string { return "test artifact writer" }

func (h artifactWriteHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{Name: "artifact_write", Parameters: map[string]any{"type": "object"}}
}

func (h artifactWriteHandler) Handle(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	logicalKey, _ := req.Args["logical_key"].(string)
	content, _ := req.Args["content"].(string)
	output, ok := req.OpSpec.FindOutput(logicalKey)
	if !ok {
		return core.HandlerResponse{}, fmt.Errorf("unknown output %q", logicalKey)
	}
	path := filepath.Join(req.Bundle.OutputDir, output.FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return core.HandlerResponse{}, err
	}
	artifactURI := strings.TrimRight(req.Bundle.OutputURIBase, "/")
	if artifactURI != "" {
		artifactURI += "/" + output.FileName
	}
	return core.HandlerResponse{Data: map[string]any{
		"logical_key":  logicalKey,
		"object_type":  output.ObjectType,
		"status":       "produced",
		"path":         path,
		"artifact_uri": artifactURI,
	}}, nil
}

type taskCompleteEchoHandler struct{}

func (h taskCompleteEchoHandler) Name() string { return "task_complete" }

func (h taskCompleteEchoHandler) Description() string { return "test task complete" }

func (h taskCompleteEchoHandler) ToolSpec() core.ToolSpec {
	return core.ToolSpec{Name: "task_complete", Parameters: map[string]any{"type": "object"}}
}

func (h taskCompleteEchoHandler) Handle(_ context.Context, req core.HandlerRequest) (core.HandlerResponse, error) {
	return core.HandlerResponse{Data: req.Args}, nil
}
