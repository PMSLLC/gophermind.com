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
	Retry   RetryPolicy // bounded exponential backoff for transient failures

	// Cache, when non-nil, serves and stores non-streaming Complete results on
	// disk keyed by a hash of the request inputs. Nil (the default) disables
	// caching entirely: no disk I/O, behavior identical to a direct request.
	Cache *Cache

	// sleep performs the backoff wait; injectable so tests stay fast and
	// deterministic. Defaults to ctxSleep (a real, context-aware timer).
	sleep sleepFn
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
		Retry:   DefaultRetryPolicy,
		sleep:   ctxSleep,
	}
}

// sleeper returns the configured sleeper, defaulting to ctxSleep when a Client
// was constructed without New (e.g. a struct literal).
func (c *Client) sleeper() sleepFn {
	if c.sleep != nil {
		return c.sleep
	}
	return ctxSleep
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

	// On a fresh cache hit, return without touching the network. A hit incurred
	// no real token spend, so it reports zero Usage — the session meter then
	// reflects actual spend, not re-counted cached tokens.
	if c.Cache != nil {
		if msg, _, ok := c.Cache.get(reqBody); ok {
			return msg, Usage{}, nil
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("marshal request: %w", err)
	}

	attempts := c.Retry.attempts()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		msg, usage, retryAfter, retryable, err := c.completeOnce(ctx, body)
		if err == nil {
			if c.Cache != nil {
				c.Cache.put(reqBody, msg, usage)
			}
			return msg, usage, nil
		}
		lastErr = err
		// Terminal error, or no attempts left: stop now.
		if !retryable || attempt == attempts-1 {
			break
		}
		if err := c.sleeper()(ctx, c.Retry.backoff(attempt, retryAfter)); err != nil {
			return Message{}, Usage{}, err // context cancelled/deadline during backoff
		}
	}
	return Message{}, Usage{}, lastErr
}

// completeOnce performs a single non-streaming round-trip. It returns the parsed
// result on success, or an error plus whether that error is retryable and any
// server-supplied Retry-After to honor on the next attempt.
func (c *Client) completeOnce(ctx context.Context, body []byte) (Message, Usage, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, 0, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Transport-level failure: retryable unless the context is done.
		return Message{}, Usage{}, 0, retryableErr(ctx, err), fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	// Retryable status: drain a bounded slice of the body for the error message
	// (don't grow memory on a hostile/huge error body) and report it.
	if retryableStatus(resp.StatusCode) {
		errBody := readCapped(resp.Body)
		return Message{}, Usage{}, parseRetryAfter(resp.Header), true,
			fmt.Errorf("status %d: %s", resp.StatusCode, truncate(errBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// Reading the body failed mid-flight: a transient read error, retryable.
		return Message{}, Usage{}, 0, retryableErr(ctx, err), fmt.Errorf("read response: %w", err)
	}

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		// Decode failed; surface the raw body and status for debugging. Not retried.
		return Message{}, Usage{}, 0, false, fmt.Errorf("status %d: unmarshal response: %w; body=%s", resp.StatusCode, err, truncate(respBody))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Message{}, Usage{}, 0, false, fmt.Errorf("provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		// A non-retryable >=300 (e.g. 4xx other than 429): terminal.
		return Message{}, Usage{}, 0, false, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(respBody))
	}
	if len(parsed.Choices) == 0 {
		return Message{}, Usage{}, 0, false, fmt.Errorf("no choices in response; body=%s", truncate(respBody))
	}
	var usage Usage
	if parsed.Usage != nil {
		usage = *parsed.Usage
	}
	return parsed.Choices[0].Message, usage, 0, false, nil
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

// readCapped reads at most a bounded prefix of r, so a hostile server cannot
// make a retried error response exhaust memory. The cap matches truncate's
// display limit (extra byte lets truncate flag that it was cut).
func readCapped(r io.Reader) []byte {
	const cap = 2001
	b, _ := io.ReadAll(io.LimitReader(r, cap))
	return b
}

func truncate(b []byte) string {
	const max = 2000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "... [truncated]"
}
