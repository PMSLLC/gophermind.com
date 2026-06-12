package llm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible /v1/chat/completions endpoint.
type Client struct {
	BaseURL string
	APIKey  string // bearer token; may be empty when reached over VPN
	Model   string
	HTTP    *http.Client
}

// New constructs a Client. timeout bounds a single completion round-trip.
// When insecureTLS is true, server certificate verification is skipped — used
// for self-signed internal endpoints reached over a trusted VPN.
func New(baseURL, apiKey, model string, timeout time.Duration, insecureTLS bool) *Client {
	httpClient := &http.Client{Timeout: timeout}
	if insecureTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    httpClient,
	}
}

// Complete performs one non-streaming chat round-trip and returns the
// assistant message (which may carry tool calls) and the response's token
// usage. Usage is the zero value when the endpoint omits the block.
func (c *Client) Complete(ctx context.Context, msgs []Message, tools []Tool) (Message, Usage, error) {
	reqBody := ChatRequest{
		Model:       c.Model,
		Messages:    msgs,
		Tools:       tools,
		Temperature: 0,
		Stream:      false,
	}
	if len(tools) > 0 {
		reqBody.ToolChoice = "auto"
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("read response: %w", err)
	}

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		// Decode failed; surface the raw body and status for debugging.
		return Message{}, Usage{}, fmt.Errorf("status %d: unmarshal response: %w; body=%s", resp.StatusCode, err, truncate(respBody))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Message{}, Usage{}, fmt.Errorf("provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return Message{}, Usage{}, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(respBody))
	}
	if len(parsed.Choices) == 0 {
		return Message{}, Usage{}, fmt.Errorf("no choices in response; body=%s", truncate(respBody))
	}
	var usage Usage
	if parsed.Usage != nil {
		usage = *parsed.Usage
	}
	return parsed.Choices[0].Message, usage, nil
}

// DiscoverModel queries GET /v1/models and returns the id of the first model
// the endpoint serves. Used when no model is configured.
func (c *Client) DiscoverModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return "", err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("status %d: parse models: %w; body=%s", resp.StatusCode, err, truncate(body))
	}
	if len(parsed.Data) == 0 {
		return "", fmt.Errorf("endpoint served no models")
	}
	return parsed.Data[0].ID, nil
}

func truncate(b []byte) string {
	const max = 2000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "... [truncated]"
}
