package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"doujia/internal/agent/core"
)

type ToolLoop struct {
	adapter  Adapter
	maxTurns int
}

func NewToolLoop(adapter Adapter) *ToolLoop {
	return &ToolLoop{adapter: adapter, maxTurns: 12}
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
	architectTestDataRepairAttempts := 0
	maxTurns := l.maxTurns
	if maxTurns <= 0 {
		maxTurns = 12
	}
	for turn := 0; turn < maxTurns; turn++ {
		resp, err := adapter.Chat(ctx, ChatRequest{Messages: messages, Tools: tools})
		if err != nil {
			return core.AgentResult{}, err
		}
		if len(resp.ToolCalls) == 0 {
			result, err := parseFinalResult(resp.Message.Content)
			if err != nil {
				return core.AgentResult{}, err
			}
			fillProducedOutputPaths(&result, produced)
			return result, nil
		}

		messages = append(messages, Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
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
				return core.AgentResult{}, err
			}
			if call.Name == "task_complete" {
				result, err := agentResultFromToolResponse(toolResp)
				if err != nil {
					return core.AgentResult{}, err
				}
				fillProducedOutputPaths(&result, produced)
				return result, nil
			}
			if call.Name == "artifact_write" {
				if output, ok := outputFromToolResponse(toolResp); ok {
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
					Role:    "tool",
					Name:    "validation_feedback",
					Content: outcome.message,
				})
			}
		}
	}
	return core.AgentResult{}, fmt.Errorf("tool loop exceeded %d turns", maxTurns)
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

func fillProducedOutputPaths(result *core.AgentResult, produced map[string]core.AgentOutput) {
	for i := range result.Outputs {
		producedOutput, ok := produced[result.Outputs[i].LogicalKey]
		if !ok {
			continue
		}
		if result.Outputs[i].ObjectType == "" {
			result.Outputs[i].ObjectType = producedOutput.ObjectType
		}
		if result.Outputs[i].Status == "" {
			result.Outputs[i].Status = producedOutput.Status
		}
		if producedOutput.Path != "" {
			result.Outputs[i].Path = producedOutput.Path
		}
		if producedOutput.ArtifactURI != "" {
			result.Outputs[i].ArtifactURI = producedOutput.ArtifactURI
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
