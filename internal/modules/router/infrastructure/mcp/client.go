package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client calls an MCP server over Streamable HTTP (JSON-RPC 2.0).
type Client struct {
	baseURL    string
	httpClient *http.Client
	headers    map[string]string // auth and other fixed request headers
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewClientWithConfig creates a client with auth headers and a custom timeout.
func NewClientWithConfig(baseURL string, headers map[string]string, timeoutSecs int) *Client {
	timeout := time.Duration(timeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
		headers:    headers,
	}
}

// ToolDefinition is the OpenAI-compatible tool schema returned by tools/list.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ListTools calls tools/list on the MCP server and returns tool definitions
// already formatted for inclusion in an OpenAI-compatible tools array.
func (c *Client) ListTools(ctx context.Context) ([]map[string]any, error) {
	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(result.Tools))
	for _, t := range result.Tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	return out, nil
}

// CallTool executes a single tool and returns its string result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	params := map[string]any{"name": name, "arguments": args}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return "", err
	}
	if result.IsError && len(result.Content) > 0 {
		return "", fmt.Errorf("mcp tool error: %s", result.Content[0].Text)
	}
	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}
	return "", nil
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if params != nil {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp call %s: %w", method, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp %s returned %d: %s", method, resp.StatusCode, string(b))
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("mcp error: %s", envelope.Error.Message)
	}
	if out != nil && envelope.Result != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
