package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultTemperature = 0.2
const chatCompletionMaxAttempts = 3

var chatCompletionRetryDelay = func(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

type OpenAICompatibleClient struct {
	config     Config
	httpClient *http.Client
}

func NewOpenAICompatibleClient(config Config) *OpenAICompatibleClient {
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   20 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 20 * time.Second,
		},
	}
	if config.RequestTimeout > 0 {
		httpClient.Timeout = config.RequestTimeout
	}
	return &OpenAICompatibleClient{
		config:     config,
		httpClient: httpClient,
	}
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openai-compatible client is required")
	}
	if strings.TrimSpace(c.config.BaseURL) == "" {
		return "", fmt.Errorf("openai-compatible base url is required")
	}
	if strings.TrimSpace(c.config.APIKey) == "" {
		return "", fmt.Errorf("openai-compatible api key is required")
	}
	if strings.TrimSpace(c.config.Model) == "" {
		return "", fmt.Errorf("openai-compatible model is required")
	}

	reqBody := chatCompletionRequest{
		Model: c.config.Model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
		Temperature: defaultTemperature,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal chat completion request: %w", err)
	}

	endpoint := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	var lastErr error
	for attempt := 1; attempt <= chatCompletionMaxAttempts; attempt++ {
		content, err := c.completeOnce(ctx, endpoint, payload)
		if err == nil {
			return content, nil
		}
		lastErr = err
		retryable := isRetryableChatCompletionError(err)
		if attempt == chatCompletionMaxAttempts || !retryable {
			if retryable && attempt > 1 {
				return "", fmt.Errorf("chat completions failed after %d attempts: %w", attempt, err)
			}
			return "", err
		}
		if err := sleepContext(ctx, chatCompletionRetryDelay(attempt)); err != nil {
			return "", fmt.Errorf("chat completions retry canceled after attempt %d: %w", attempt, lastErr)
		}
	}
	return "", lastErr
}

func (c *OpenAICompatibleClient) completeOnce(ctx context.Context, endpoint string, payload []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create chat completion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &chatCompletionError{
			message:   fmt.Sprintf("chat completions request: %v", err),
			cause:     err,
			retryable: isRetryableNetworkError(err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		bodyText := strings.TrimSpace(string(body))
		return "", &chatCompletionError{
			message:   fmt.Sprintf("chat completions status %d: %s", resp.StatusCode, bodyText),
			retryable: isRetryableStatus(resp.StatusCode, bodyText),
		}
	}

	var parsed chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode chat completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("chat completion response choices is empty")
	}
	content := parsed.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("chat completion response content is empty")
	}
	return content, nil
}

type chatCompletionError struct {
	message   string
	cause     error
	retryable bool
}

func (e *chatCompletionError) Error() string {
	return e.message
}

func (e *chatCompletionError) Unwrap() error {
	return e.cause
}

func isRetryableChatCompletionError(err error) bool {
	var chatErr *chatCompletionError
	return errors.As(err, &chatErr) && chatErr.retryable
}

func isRetryableNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "temporary failure") ||
		strings.Contains(lower, "unexpected eof")
}

func isRetryableStatus(statusCode int, body string) bool {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "quota_exhausted") || strings.Contains(lower, "insufficient_quota") {
		return false
	}
	return statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= 500
}

func sleepContext(ctx context.Context, delay time.Duration) error {
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

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}
