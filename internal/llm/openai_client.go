package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultTemperature = 0.2

type OpenAICompatibleClient struct {
	config     Config
	httpClient *http.Client
}

func NewOpenAICompatibleClient(config Config) *OpenAICompatibleClient {
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return &OpenAICompatibleClient{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: 5 * time.Second,
			},
		},
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create chat completion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat completions request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return "", fmt.Errorf("chat completions status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
