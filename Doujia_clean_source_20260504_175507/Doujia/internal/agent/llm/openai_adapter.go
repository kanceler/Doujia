package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type OpenAIAdapter struct {
	BaseURL       string
	APIKey        string
	Model         string
	APIStyle      string
	Client        *http.Client
	MaxRetries    int
	RetryDelay    time.Duration
	RetryMaxDelay time.Duration
	Tracef        func(format string, args ...any)
}

type OpenAIAdapterConfig struct {
	BaseURL  string
	APIKey   string
	Model    string
	APIStyle string
}

func NewOpenAIAdapterFromEnv() *OpenAIAdapter {
	return &OpenAIAdapter{
		BaseURL:       os.Getenv("OPENAI_BASE_URL"),
		APIKey:        os.Getenv("OPENAI_API_KEY"),
		Model:         os.Getenv("OPENAI_MODEL"),
		APIStyle:      os.Getenv("OPENAI_API_STYLE"),
		MaxRetries:    envInt("OPENAI_MAX_RETRIES"),
		RetryDelay:    envDuration("OPENAI_RETRY_DELAY"),
		RetryMaxDelay: envDuration("OPENAI_RETRY_MAX_DELAY"),
		Tracef:        log.Printf,
	}
}

func (a *OpenAIAdapter) ApplyConfig(config OpenAIAdapterConfig) {
	if value := strings.TrimSpace(config.BaseURL); value != "" {
		a.BaseURL = value
	}
	if value := strings.TrimSpace(config.APIKey); value != "" {
		a.APIKey = value
	}
	if value := strings.TrimSpace(config.Model); value != "" {
		a.Model = value
	}
	if value := strings.TrimSpace(config.APIStyle); value != "" {
		a.APIStyle = value
	}
}

func (a *OpenAIAdapter) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if a.APIKey == "" {
		return ChatResponse{}, fmt.Errorf("OPENAI_API_KEY is required")
	}
	model := a.Model
	if model == "" {
		return ChatResponse{}, fmt.Errorf("OPENAI_MODEL is required")
	}
	baseURL := strings.TrimRight(a.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	if strings.EqualFold(strings.TrimSpace(a.APIStyle), "responses") {
		return a.responsesChat(ctx, client, baseURL, model, req)
	}

	body := openAIChatRequest{
		Model:    model,
		Messages: toOpenAIMessages(req.Messages),
		Tools:    toOpenAITools(req.Tools),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	respBody, err := a.doChatRequest(ctx, client, chatCompletionsURL(baseURL), payload)
	if err != nil {
		return ChatResponse{}, err
	}

	msg, err := parseOpenAIChatMessage(respBody)
	if err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{
		Message:   Message{Role: msg.Role, Content: msg.Content},
		ToolCalls: fromOpenAIToolCalls(msg.ToolCalls),
	}, nil
}

func (a *OpenAIAdapter) StreamChat(ctx context.Context, req ChatRequest, onDelta func(string) error) (ChatResponse, error) {
	if a.APIKey == "" {
		return ChatResponse{}, fmt.Errorf("OPENAI_API_KEY is required")
	}
	model := a.Model
	if model == "" {
		return ChatResponse{}, fmt.Errorf("OPENAI_MODEL is required")
	}
	baseURL := strings.TrimRight(a.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	if strings.EqualFold(strings.TrimSpace(a.APIStyle), "responses") {
		input, instructions := toOpenAIResponseInput(req.Messages)
		body := openAIResponsesRequest{
			Model:        model,
			Instructions: instructions,
			Input:        input,
			Tools:        toOpenAIResponseTools(req.Tools),
			Stream:       true,
		}
		if len(body.Tools) > 0 {
			body.ToolChoice = "auto"
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return ChatResponse{}, err
		}
		msg, err := a.doStreamingResponsesRequest(ctx, client, responsesURL(baseURL), payload, onDelta)
		if err != nil {
			return ChatResponse{}, err
		}
		return ChatResponse{
			Message:   Message{Role: msg.Role, Content: msg.Content},
			ToolCalls: fromOpenAIToolCalls(msg.ToolCalls),
		}, nil
	}

	body := openAIChatRequest{
		Model:    model,
		Messages: toOpenAIMessages(req.Messages),
		Tools:    toOpenAITools(req.Tools),
		Stream:   true,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	msg, err := a.doStreamingChatRequest(ctx, client, chatCompletionsURL(baseURL), payload, onDelta)
	if err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{
		Message:   Message{Role: msg.Role, Content: msg.Content},
		ToolCalls: fromOpenAIToolCalls(msg.ToolCalls),
	}, nil
}

func (a *OpenAIAdapter) responsesChat(ctx context.Context, client *http.Client, baseURL, model string, req ChatRequest) (ChatResponse, error) {
	input, instructions := toOpenAIResponseInput(req.Messages)
	body := openAIResponsesRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
		Tools:        toOpenAIResponseTools(req.Tools),
	}
	if len(body.Tools) > 0 {
		body.ToolChoice = "auto"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	respBody, err := a.doChatRequest(ctx, client, responsesURL(baseURL), payload)
	if err != nil {
		return ChatResponse{}, err
	}
	msg, err := parseOpenAIResponsesMessage(respBody)
	if err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{
		Message:   Message{Role: msg.Role, Content: msg.Content},
		ToolCalls: fromOpenAIToolCalls(msg.ToolCalls),
	}, nil
}

func (a *OpenAIAdapter) doChatRequest(ctx context.Context, client *http.Client, url string, payload []byte) ([]byte, error) {
	maxRetries := a.MaxRetries
	if maxRetries == 0 {
		maxRetries = 2
	}
	retryDelay := a.RetryDelay
	if retryDelay == 0 {
		retryDelay = 500 * time.Millisecond
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		started := time.Now()
		a.tracef("llm_http_start url=%s attempt=%d retry_count=%d payload_bytes=%d", url, attempt+1, attempt, len(payload))
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
		httpReq.Header.Set("Content-Type", "application/json")

		httpResp, err := client.Do(httpReq)
		if err != nil {
			a.tracef("llm_http_end url=%s attempt=%d retry_count=%d duration=%s error=%q", url, attempt+1, attempt, time.Since(started), err.Error())
			lastErr = err
			if attempt < maxRetries && waitBeforeRetry(ctx, retryDelayForAttempt(retryDelay, a.RetryMaxDelay, attempt, "")) == nil {
				continue
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, lastErr
		}

		respBody, readErr := io.ReadAll(httpResp.Body)
		closeErr := httpResp.Body.Close()
		if readErr != nil {
			a.tracef("llm_http_end url=%s attempt=%d retry_count=%d duration=%s status=%d error=%q", url, attempt+1, attempt, time.Since(started), httpResp.StatusCode, readErr.Error())
			lastErr = readErr
			if attempt < maxRetries && waitBeforeRetry(ctx, retryDelayForAttempt(retryDelay, a.RetryMaxDelay, attempt, "")) == nil {
				continue
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, lastErr
		}
		if closeErr != nil {
			a.tracef("llm_http_end url=%s attempt=%d retry_count=%d duration=%s status=%d error=%q", url, attempt+1, attempt, time.Since(started), httpResp.StatusCode, closeErr.Error())
			return nil, closeErr
		}
		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			a.tracef("llm_http_end url=%s attempt=%d retry_count=%d duration=%s status=%d response_bytes=%d", url, attempt+1, attempt, time.Since(started), httpResp.StatusCode, len(respBody))
			return respBody, nil
		}
		lastErr = fmt.Errorf("openai chat request failed: status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
		a.tracef("llm_http_end url=%s attempt=%d retry_count=%d duration=%s status=%d provider_error_body=%q", url, attempt+1, attempt, time.Since(started), httpResp.StatusCode, truncateTraceBody(respBody, 512))
		if !retryableHTTPStatus(httpResp.StatusCode) || attempt >= maxRetries {
			return nil, lastErr
		}
		if err := waitBeforeRetry(ctx, retryDelayForAttempt(retryDelay, a.RetryMaxDelay, attempt, httpResp.Header.Get("Retry-After"))); err != nil {
			return nil, err
		}
	}
}

func (a *OpenAIAdapter) doStreamingChatRequest(ctx context.Context, client *http.Client, url string, payload []byte, onDelta func(string) error) (openAIMessage, error) {
	started := time.Now()
	a.tracef("llm_http_start url=%s attempt=1 retry_count=0 payload_bytes=%d stream=true", url, len(payload))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return openAIMessage{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s error=%q", url, time.Since(started), err.Error())
		if ctxErr := ctx.Err(); ctxErr != nil {
			return openAIMessage{}, ctxErr
		}
		return openAIMessage{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return openAIMessage{}, readErr
		}
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d provider_error_body=%q", url, time.Since(started), httpResp.StatusCode, truncateTraceBody(respBody, 512))
		return openAIMessage{}, fmt.Errorf("openai chat request failed: status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	msg, err := parseOpenAIChatStreamReader(httpResp.Body, onDelta)
	if err != nil {
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d error=%q", url, time.Since(started), httpResp.StatusCode, err.Error())
		return openAIMessage{}, err
	}
	a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d response_bytes=%d stream=true", url, time.Since(started), httpResp.StatusCode, len(msg.Content))
	return msg, nil
}

func (a *OpenAIAdapter) doStreamingResponsesRequest(ctx context.Context, client *http.Client, url string, payload []byte, onDelta func(string) error) (openAIMessage, error) {
	started := time.Now()
	a.tracef("llm_http_start url=%s attempt=1 retry_count=0 payload_bytes=%d stream=true", url, len(payload))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return openAIMessage{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(httpReq)
	if err != nil {
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s error=%q", url, time.Since(started), err.Error())
		if ctxErr := ctx.Err(); ctxErr != nil {
			return openAIMessage{}, ctxErr
		}
		return openAIMessage{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return openAIMessage{}, readErr
		}
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d provider_error_body=%q", url, time.Since(started), httpResp.StatusCode, truncateTraceBody(respBody, 512))
		return openAIMessage{}, fmt.Errorf("openai chat request failed: status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	msg, err := parseOpenAIResponsesStreamReader(httpResp.Body, onDelta)
	if err != nil {
		a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d error=%q", url, time.Since(started), httpResp.StatusCode, err.Error())
		return openAIMessage{}, err
	}
	a.tracef("llm_http_end url=%s attempt=1 retry_count=0 duration=%s status=%d response_bytes=%d stream=true", url, time.Since(started), httpResp.StatusCode, len(msg.Content))
	return msg, nil
}

func (a *OpenAIAdapter) tracef(format string, args ...any) {
	if a.Tracef == nil {
		return
	}
	a.Tracef(format, args...)
}

func truncateTraceBody(body []byte, limit int) string {
	text := strings.TrimSpace(string(body))
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "...(truncated)"
}

func retryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func retryDelayForAttempt(baseDelay time.Duration, maxDelay time.Duration, attempt int, retryAfter string) time.Duration {
	if retryAfterDelay, ok := parseRetryAfter(retryAfter); ok {
		return retryAfterDelay
	}
	delay := baseDelay
	if attempt > 0 {
		delay = delay * time.Duration(1<<attempt)
	}
	if maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

func waitBeforeRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds < 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			return 0, true
		}
		return delay, true
	}
	return 0, false
}

func envInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func envDuration(key string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func chatCompletionsURL(baseURL string) string {
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v3") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func responsesURL(baseURL string) string {
	if strings.HasSuffix(baseURL, "/responses") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v3") {
		return baseURL + "/responses"
	}
	return baseURL + "/v1/responses"
}

type openAIChatRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []openAITool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream,omitempty"`
}

type openAIResponsesRequest struct {
	Model        string                    `json:"model"`
	Instructions string                    `json:"instructions,omitempty"`
	Input        []openAIResponseInputItem `json:"input"`
	Tools        []openAIResponseTool      `json:"tools,omitempty"`
	ToolChoice   string                    `json:"tool_choice,omitempty"`
	Stream       bool                      `json:"stream,omitempty"`
}

type openAIResponseInputItem struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Args    string `json:"arguments,omitempty"`
	Output  string `json:"output,omitempty"`
}

type openAIResponseTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
}

type openAIResponsesResponse struct {
	Output []openAIResponseOutputItem `json:"output"`
}

type openAIResponsesStreamEvent struct {
	Type      string                    `json:"type"`
	Delta     string                    `json:"delta,omitempty"`
	Text      string                    `json:"text,omitempty"`
	Name      string                    `json:"name,omitempty"`
	Arguments string                    `json:"arguments,omitempty"`
	CallID    string                    `json:"call_id,omitempty"`
	ItemID    string                    `json:"item_id,omitempty"`
	Item      openAIResponseOutputItem  `json:"item,omitempty"`
	Error     *openAIResponsesErrorBody `json:"error,omitempty"`
}

type openAIResponsesErrorBody struct {
	Message string `json:"message,omitempty"`
}

type openAIResponseOutputItem struct {
	Type      string                         `json:"type"`
	Role      string                         `json:"role,omitempty"`
	Content   []openAIResponseOutputContent  `json:"content,omitempty"`
	CallID    string                         `json:"call_id,omitempty"`
	Name      string                         `json:"name,omitempty"`
	Arguments string                         `json:"arguments,omitempty"`
	Function  openAIResponseOutputFunction   `json:"function,omitempty"`
	ToolCalls []openAIResponseOutputToolCall `json:"tool_calls,omitempty"`
}

type openAIResponseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIResponseOutputFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIResponseOutputToolCall struct {
	ID        string                       `json:"id,omitempty"`
	CallID    string                       `json:"call_id,omitempty"`
	Type      string                       `json:"type,omitempty"`
	Name      string                       `json:"name,omitempty"`
	Arguments string                       `json:"arguments,omitempty"`
	Function  openAIResponseOutputFunction `json:"function,omitempty"`
}

type openAIChatStreamChunk struct {
	Choices []struct {
		Delta openAIMessage `json:"delta"`
	} `json:"choices"`
}

func parseOpenAIChatMessage(body []byte) (openAIMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return openAIMessage{}, fmt.Errorf("openai chat response is empty")
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte(":")) {
		return parseOpenAIChatStreamMessage(trimmed)
	}
	var decoded openAIChatResponse
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return openAIMessage{}, err
	}
	if len(decoded.Choices) == 0 {
		return openAIMessage{}, fmt.Errorf("openai chat response has no choices")
	}
	return decoded.Choices[0].Message, nil
}

func parseOpenAIChatStreamMessage(body []byte) (openAIMessage, error) {
	return parseOpenAIChatStreamReader(bytes.NewReader(body), nil)
}

func parseOpenAIChatStreamReader(reader io.Reader, onDelta func(string) error) (openAIMessage, error) {
	var msg openAIMessage
	var sawChoice bool
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk openAIChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return openAIMessage{}, err
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		sawChoice = true
		delta := chunk.Choices[0].Delta
		if strings.TrimSpace(delta.Role) != "" {
			msg.Role = delta.Role
		}
		msg.Content += delta.Content
		if onDelta != nil && delta.Content != "" {
			if err := onDelta(delta.Content); err != nil {
				return openAIMessage{}, err
			}
		}
		if len(delta.ToolCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, delta.ToolCalls...)
		}
	}
	if err := scanner.Err(); err != nil {
		return openAIMessage{}, err
	}
	if !sawChoice {
		return openAIMessage{}, fmt.Errorf("openai chat stream response has no choices")
	}
	if strings.TrimSpace(msg.Role) == "" {
		msg.Role = "assistant"
	}
	return msg, nil
}

func parseOpenAIResponsesMessage(body []byte) (openAIMessage, error) {
	var decoded openAIResponsesResponse
	if err := json.Unmarshal(bytes.TrimSpace(body), &decoded); err != nil {
		return openAIMessage{}, err
	}
	if len(decoded.Output) == 0 {
		return openAIMessage{}, fmt.Errorf("openai responses response has no output")
	}
	msg := openAIMessage{Role: "assistant"}
	for _, item := range decoded.Output {
		switch item.Type {
		case "message":
			if strings.TrimSpace(item.Role) != "" {
				msg.Role = item.Role
			}
			for _, content := range item.Content {
				if content.Type == "" || content.Type == "output_text" || content.Type == "text" {
					msg.Content += content.Text
				}
			}
			for _, call := range item.ToolCalls {
				if toolCall := openAIToolCallFromResponseToolCall(call); strings.TrimSpace(toolCall.ID) != "" || strings.TrimSpace(toolCall.Function.Name) != "" {
					msg.ToolCalls = append(msg.ToolCalls, toolCall)
				}
			}
		case "function_call":
			msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
				ID:   firstNonEmpty(item.CallID, item.Name),
				Type: "function",
				Function: openAIToolFunction{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
		return openAIMessage{}, fmt.Errorf("openai responses response has no assistant content or tool calls")
	}
	return msg, nil
}

func parseOpenAIResponsesStreamReader(reader io.Reader, onDelta func(string) error) (openAIMessage, error) {
	msg := openAIMessage{Role: "assistant"}
	toolCalls := make(map[string]openAIToolCall)

	var eventName string
	var dataLines []string
	handleBlock := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		currentEventName := eventName
		data := strings.Join(dataLines, "\n")
		eventName = ""
		dataLines = nil
		if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
			return nil
		}

		var event openAIResponsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return err
		}
		kind := firstNonEmpty(strings.TrimSpace(event.Type), strings.TrimSpace(currentEventName))
		switch kind {
		case "error":
			if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
				return fmt.Errorf("%s", event.Error.Message)
			}
			return fmt.Errorf("openai responses stream returned an error event")
		case "response.output_text.delta":
			if event.Delta == "" {
				return nil
			}
			msg.Content += event.Delta
			if onDelta != nil {
				if err := onDelta(event.Delta); err != nil {
					return err
				}
			}
		case "response.output_text.done":
			if strings.TrimSpace(msg.Content) == "" && event.Text != "" {
				msg.Content = event.Text
				if onDelta != nil {
					if err := onDelta(event.Text); err != nil {
						return err
					}
				}
			}
		case "response.function_call_arguments.done":
			upsertOpenAIStreamToolCall(toolCalls, openAIToolCall{
				ID:   firstNonEmpty(event.CallID, event.ItemID, event.Name),
				Type: "function",
				Function: openAIToolFunction{
					Name:      event.Name,
					Arguments: event.Arguments,
				},
			})
		case "response.output_item.done":
			collectOpenAIStreamOutputItem(&msg, toolCalls, event.Item)
		case "response.completed":
			return nil
		}
		return nil
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := handleBlock(); err != nil {
				return openAIMessage{}, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return openAIMessage{}, err
	}
	if err := handleBlock(); err != nil {
		return openAIMessage{}, err
	}

	for _, call := range toolCalls {
		msg.ToolCalls = append(msg.ToolCalls, call)
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
		return openAIMessage{}, fmt.Errorf("openai responses stream response has no assistant content or tool calls")
	}
	return msg, nil
}

func openAIToolCallFromResponseToolCall(call openAIResponseOutputToolCall) openAIToolCall {
	name := firstNonEmpty(call.Name, call.Function.Name)
	args := firstNonEmpty(call.Arguments, call.Function.Arguments)
	return openAIToolCall{
		ID:   firstNonEmpty(call.CallID, call.ID, name),
		Type: "function",
		Function: openAIToolFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

func collectOpenAIStreamOutputItem(msg *openAIMessage, toolCalls map[string]openAIToolCall, item openAIResponseOutputItem) {
	switch item.Type {
	case "message":
		if strings.TrimSpace(item.Role) != "" {
			msg.Role = item.Role
		}
		if strings.TrimSpace(msg.Content) == "" {
			for _, content := range item.Content {
				if content.Type == "" || content.Type == "output_text" || content.Type == "text" {
					msg.Content += content.Text
				}
			}
		}
		for _, call := range item.ToolCalls {
			upsertOpenAIStreamToolCall(toolCalls, openAIToolCallFromResponseToolCall(call))
		}
	case "function_call":
		upsertOpenAIStreamToolCall(toolCalls, openAIToolCall{
			ID:   firstNonEmpty(item.CallID, item.Name),
			Type: "function",
			Function: openAIToolFunction{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
}

func upsertOpenAIStreamToolCall(toolCalls map[string]openAIToolCall, call openAIToolCall) {
	key := firstNonEmpty(call.ID, call.Function.Name)
	if key == "" {
		return
	}
	current := toolCalls[key]
	if strings.TrimSpace(call.ID) != "" {
		current.ID = call.ID
	}
	current.Type = "function"
	if strings.TrimSpace(call.Function.Name) != "" {
		current.Function.Name = call.Function.Name
	}
	if strings.TrimSpace(call.Function.Arguments) != "" {
		current.Function.Arguments = call.Function.Arguments
	}
	toolCalls[key] = current
}

func toOpenAIMessages(messages []Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, openAIMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
			ToolCalls:  toOpenAIMessageToolCalls(msg.ToolCalls),
		})
	}
	return out
}

func toOpenAIResponseInput(messages []Message) ([]openAIResponseInputItem, string) {
	out := make([]openAIResponseInputItem, 0, len(messages))
	instructions := make([]string, 0)
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if strings.TrimSpace(msg.Content) != "" {
				instructions = append(instructions, msg.Content)
			}
		case "tool":
			out = append(out, openAIResponseInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
		case "assistant":
			for _, call := range msg.ToolCalls {
				args, _ := json.Marshal(call.Args)
				out = append(out, openAIResponseInputItem{
					Type:   "function_call",
					CallID: call.ID,
					Name:   call.Name,
					Args:   string(args),
				})
			}
			if strings.TrimSpace(msg.Content) != "" || len(msg.ToolCalls) == 0 {
				out = append(out, openAIResponseInputItem{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		default:
			out = append(out, openAIResponseInputItem{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	if len(out) == 0 {
		out = append(out, openAIResponseInputItem{
			Role:    "user",
			Content: "Continue.",
		})
	}
	return out, strings.Join(instructions, "\n\n")
}

func toOpenAITools(tools []Tool) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

func toOpenAIResponseTools(tools []Tool) []openAIResponseTool {
	out := make([]openAIResponseTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAIResponseTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return out
}

func fromOpenAIToolCalls(calls []openAIToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		args := map[string]any{}
		if call.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		out = append(out, ToolCall{ID: call.ID, Name: call.Function.Name, Args: args})
	}
	return out
}

func toOpenAIMessageToolCalls(calls []ToolCall) []openAIToolCall {
	out := make([]openAIToolCall, 0, len(calls))
	for _, call := range calls {
		args, _ := json.Marshal(call.Args)
		out = append(out, openAIToolCall{
			ID:   call.ID,
			Type: "function",
			Function: openAIToolFunction{
				Name:      call.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
