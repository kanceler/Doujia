package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"devflow/internal/agent/core"
	"devflow/internal/agent/executor"
	"devflow/internal/agent/schema"
)

const defaultToolLoopMaxTurns = 48

type ToolLoop struct {
	adapter  Adapter
	maxTurns int
}

func NewToolLoop(adapter Adapter) *ToolLoop {
	return &ToolLoop{adapter: adapter, maxTurns: defaultToolLoopMaxTurns}
}

func NewToolLoopFromEnv(adapter Adapter) *ToolLoop {
	loop := NewToolLoop(adapter)
	raw := strings.TrimSpace(os.Getenv("DOUJIA_TOOL_LOOP_MAX_TURNS"))
	if raw == "" {
		return loop
	}
	maxTurns, err := strconv.Atoi(raw)
	if err != nil || maxTurns <= 0 {
		return loop
	}
	return loop.WithMaxTurns(maxTurns)
}

func (l *ToolLoop) WithMaxTurns(maxTurns int) *ToolLoop {
	l.maxTurns = maxTurns
	return l
}

func (l *ToolLoop) Run(ctx context.Context, req core.ToolLoopRequest) (core.AgentResult, error) {
	adapter := l.adapter
	if adapter == nil {
		var ok bool
		adapter, ok = req.LLM.(Adapter)
		if !ok || adapter == nil {
			return core.AgentResult{}, fmt.Errorf("llm adapter is required")
		}
	}
	if req.Handlers == nil {
		return core.AgentResult{}, fmt.Errorf("handler registry is required")
	}
	tools, err := BuildTools(req.OpSpec, req.Handlers)
	if err != nil {
		return core.AgentResult{}, err
	}

	messages := []Message{{Role: "system", Content: req.Prompt}}
	produced := map[string]core.AgentOutput{}
	artifactValidationRepairAttempts := map[string]int{}
	architectTestDataRepairAttempts := 0
	lastTool := ""
	maxTurns := l.maxTurns
	if maxTurns <= 0 {
		maxTurns = defaultToolLoopMaxTurns
	}
	for turn := 0; turn < maxTurns; turn++ {
		resp, err := adapter.Chat(ctx, ChatRequest{Messages: messages, Tools: tools})
		if err != nil {
			return toolLoopFailureResult(req, "llm_chat_failed", err, turn+1, maxTurns, lastTool), nil
		}
		if len(resp.ToolCalls) == 0 {
			result, err := parseFinalResult(resp.Message.Content)
			if err != nil {
				return toolLoopFailureResult(req, "final_json_parse_failed", err, turn+1, maxTurns, lastTool), nil
			}
			normalizeOutputStatusAliases(&result)
			fillProducedOutputPaths(&result, produced)
			return result, nil
		}

		messages = append(messages, Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			lastTool = call.Name
			if hasForbiddenPathArg(call.Args) {
				return core.AgentResult{}, fmt.Errorf("tool %s received forbidden path/output_dir argument", call.Name)
			}
			handler, ok := req.Handlers.Get(call.Name)
			if !ok {
				return core.AgentResult{}, fmt.Errorf("tool %q is not allowed or registered", call.Name)
			}
			toolResp, err := handler.Handle(ctx, core.HandlerRequest{
				Task:   req.Task,
				Bundle: req.Bundle,
				OpSpec: req.OpSpec,
				Args:   call.Args,
			})
			if err != nil {
				messages = append(messages, toolErrorMessage(call, err))
				continue
			}
			if call.Name == "task_complete" {
				result, err := agentResultFromToolResponse(toolResp)
				if err != nil {
					return core.AgentResult{}, err
				}
				normalizeOutputStatusAliases(&result)
				fillProducedOutputPaths(&result, produced)
				if len(result.ProducedBags) == 0 {
					result.ProducedBags = req.OpSpec.ResolveProducedBags(req.Task, req.Bundle, result)
				}
				if err := executor.ValidateOutputs(req.Task, req.Bundle, req.OpSpec, result); err != nil {
					messages = append(messages, toolErrorMessage(
						call,
						fmt.Errorf("AgentResult validation failed: %s. Write or reuse the required outputs, then call task_complete again.", err.Error()),
					))
					continue
				}
				return result, nil
			}
			if call.Name == "artifact_write" {
				if output, ok := outputFromToolResponse(toolResp); ok {
					if err := validateWrittenArtifact(req.Task, output); err != nil {
						artifactValidationRepairAttempts[output.LogicalKey]++
						if artifactValidationRepairAttempts[output.LogicalKey] > 1 {
							return toolLoopFailureResult(req, "artifact_validation_failed", err, turn+1, maxTurns, lastTool), nil
						}
						messages = append(messages, toolErrorMessage(
							call,
							fmt.Errorf("artifact %q failed local validation: %s. Please rewrite only this artifact with artifact_write(logical_key=%q, content=<raw valid JSON>). Do not wrap JSON in Markdown. Required contract: %s. Example:\n%s", output.LogicalKey, err.Error(), output.LogicalKey, schema.JSONArtifactContract(output.LogicalKey), schema.JSONArtifactExample(output.LogicalKey)),
						))
						continue
					}
					produced[output.LogicalKey] = output
				}
			}
			body, err := json.Marshal(toolResp.Data)
			if err != nil {
				return core.AgentResult{}, err
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: string(body)})
		}

		if req.Task.Role == "architect" && req.Task.Op == "test_data" {
			outcome, ready, err := validateArchitectTestData(req.Bundle, produced)
			if err != nil {
				return core.AgentResult{}, err
			}
			if ready {
				if outcome.message == "" {
					continue
				}
				if !outcome.retryable {
					return core.AgentResult{}, errors.New(outcome.message)
				}
				architectTestDataRepairAttempts++
				if architectTestDataRepairAttempts > maxArchitectTestDataRepairAttempts {
					return core.AgentResult{}, fmt.Errorf("validation repair attempts exceeded: %s", outcome.message)
				}
				messages = append(messages, Message{
					Role:    "user",
					Content: outcome.message,
				})
			}
		}
	}
	return toolLoopFailureResult(req, "tool_loop_exceeded", fmt.Errorf("tool loop exceeded max_turns=%d", maxTurns), maxTurns, maxTurns, lastTool), nil
}

func toolLoopFailureResult(req core.ToolLoopRequest, code string, err error, turn int, maxTurns int, lastTool string) core.AgentResult {
	if strings.TrimSpace(lastTool) == "" {
		lastTool = "none"
	}
	message := fmt.Sprintf("%s: role=%s op=%s turn=%d max_turns=%d last_tool=%s error=%s",
		code,
		strings.TrimSpace(req.Task.Role),
		strings.TrimSpace(req.Task.Op),
		turn,
		maxTurns,
		lastTool,
		err.Error(),
	)
	return core.AgentResult{
		Result:  "kfail",
		Message: message,
		Errors:  []core.AgentError{{Code: code, Message: message}},
	}
}

func toolErrorMessage(call ToolCall, err error) Message {
	body, marshalErr := json.Marshal(map[string]any{
		"ok":    false,
		"error": err.Error(),
	})
	if marshalErr != nil {
		body = []byte(`{"ok":false,"error":"tool call failed"}`)
	}
	return Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    string(body),
	}
}

func parseFinalResult(content string) (core.AgentResult, error) {
	candidates := finalResultCandidates(content)
	var lastErr error
	for _, candidate := range candidates {
		var result core.AgentResult
		if err := json.Unmarshal([]byte(candidate), &result); err != nil {
			lastErr = err
			continue
		}
		if result.Result == "" {
			lastErr = fmt.Errorf("final AgentResult JSON missing result")
			continue
		}
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no JSON object found")
	}
	return core.AgentResult{}, fmt.Errorf("final response must be AgentResult JSON: %w", lastErr)
}

func finalResultCandidates(content string) []string {
	seen := map[string]bool{}
	add := func(candidates *[]string, candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		*candidates = append(*candidates, candidate)
	}

	trimmed := strings.TrimSpace(content)
	var candidates []string
	add(&candidates, trimmed)

	unfenced := trimCodeFence(trimmed)
	add(&candidates, unfenced)

	if extracted, ok := extractFirstJSONObject(trimmed); ok {
		add(&candidates, extracted)
	}
	if extracted, ok := extractFirstJSONObject(unfenced); ok {
		add(&candidates, extracted)
	}
	return candidates
}

func trimCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	rest := trimmed[3:]
	if idx := strings.Index(rest, "\n"); idx >= 0 {
		lang := strings.TrimSpace(rest[:idx])
		if lang == "" || isFenceLanguageLabel(lang) {
			body := rest[idx+1:]
			if end := strings.LastIndex(body, "```"); end >= 0 {
				return strings.TrimSpace(body[:end])
			}
		}
	}
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		return strings.TrimSpace(rest[:end])
	}
	return trimmed
}

func isFenceLanguageLabel(label string) bool {
	for _, r := range label {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func extractFirstJSONObject(content string) (string, bool) {
	start := strings.Index(content, "{")
	if start < 0 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		ch := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1], true
			}
		}
	}
	return "", false
}

func hasForbiddenPathArg(args map[string]any) bool {
	_, hasPath := args["path"]
	_, hasOutputDir := args["output_dir"]
	_, hasFileName := args["file_name"]
	return hasPath || hasOutputDir || hasFileName
}

func outputFromToolResponse(resp core.HandlerResponse) (core.AgentOutput, bool) {
	logicalKey, ok := resp.Data["logical_key"].(string)
	if !ok || logicalKey == "" {
		return core.AgentOutput{}, false
	}
	output := core.AgentOutput{LogicalKey: logicalKey}
	if objectType, ok := resp.Data["object_type"].(string); ok {
		output.ObjectType = objectType
	}
	if contentType, ok := resp.Data["content_type"].(string); ok {
		output.ContentType = contentType
	}
	if encoding, ok := resp.Data["encoding"].(string); ok {
		output.Encoding = encoding
	}
	if status, ok := resp.Data["status"].(string); ok {
		output.Status = status
	}
	if path, ok := resp.Data["path"].(string); ok {
		output.Path = path
	}
	if artifactURI, ok := resp.Data["artifact_uri"].(string); ok {
		output.ArtifactURI = artifactURI
	}
	return output, true
}

func validateWrittenArtifact(task core.Task, output core.AgentOutput) error {
	if !strings.EqualFold(strings.TrimSpace(output.ObjectType), "json") {
		return nil
	}
	if strings.TrimSpace(output.Path) == "" {
		return nil
	}
	body, err := os.ReadFile(output.Path)
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	_ = task
	return schema.ValidateJSONArtifactBytes(output.LogicalKey, body)
}

func fillProducedOutputPaths(result *core.AgentResult, produced map[string]core.AgentOutput) {
	seen := make(map[string]bool, len(result.Outputs))
	for i := range result.Outputs {
		producedOutput, ok := produced[result.Outputs[i].LogicalKey]
		seen[result.Outputs[i].LogicalKey] = true
		if !ok {
			continue
		}
		result.Outputs[i].ObjectType = producedOutput.ObjectType
		result.Outputs[i].ContentType = producedOutput.ContentType
		result.Outputs[i].Encoding = producedOutput.Encoding
		result.Outputs[i].Status = producedOutput.Status
		if producedOutput.Path != "" {
			result.Outputs[i].Path = producedOutput.Path
		}
		if producedOutput.ArtifactURI != "" {
			result.Outputs[i].ArtifactURI = producedOutput.ArtifactURI
		}
	}
	for logicalKey, output := range produced {
		if seen[logicalKey] {
			continue
		}
		result.Outputs = append(result.Outputs, output)
	}
}

func normalizeOutputStatusAliases(result *core.AgentResult) {
	for i := range result.Outputs {
		switch strings.TrimSpace(strings.ToLower(result.Outputs[i].Status)) {
		case "success", "ok":
			result.Outputs[i].Status = "produced"
		case "reuse":
			result.Outputs[i].Status = "reused"
		}
	}
}

func agentResultFromToolResponse(resp core.HandlerResponse) (core.AgentResult, error) {
	body, err := json.Marshal(resp.Data)
	if err != nil {
		return core.AgentResult{}, err
	}
	var result core.AgentResult
	if err := json.Unmarshal(body, &result); err != nil {
		return core.AgentResult{}, fmt.Errorf("task_complete payload must be AgentResult-shaped JSON: %w", err)
	}
	if result.Result == "" {
		return core.AgentResult{}, fmt.Errorf("task_complete payload missing result")
	}
	return result, nil
}
