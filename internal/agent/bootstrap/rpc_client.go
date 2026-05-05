package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type rpcClient struct {
	baseURL string
	path    string
	timeout time.Duration
	client  *http.Client
}

func newRPCClient(baseURL, path string, timeout time.Duration) rpcClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return rpcClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		path:    path,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c rpcClient) postJSON(ctx context.Context, in any, out any) error {
	if strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("rpc driver requires driver_ref URL")
	}
	endpoint, err := url.JoinPath(c.baseURL, strings.TrimLeft(c.path, "/"))
	if err != nil {
		return fmt.Errorf("build rpc endpoint: %w", err)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode rpc request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post rpc request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("rpc endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode rpc response: %w", err)
	}
	return nil
}
