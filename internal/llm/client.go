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

	// Fallbacks is an optional ordered list of models tried, in order, after the
	// primary Model when a request fails with a fallback-eligible error (after
	// that model's own retries are exhausted) — graceful degradation when the
	// primary is down or missing. Empty (the default) means single-model behavior
	// exactly as before: no fallback. The effective try-order is Model first,
	// then these; see chain() for trimming/dedup/cap rules.
	Fallbacks []string

	// Cache, when non-nil, serves and stores non-streaming Complete results on
	// disk keyed by a hash of the request inputs. Nil (the default) disables
	// caching entirely: no disk I/O, behavior identical to a direct request.
	Cache *Cache

	// sleep performs the backoff wait; injectable so tests stay fast and
	// deterministic. Defaults to ctxSleep (a real, context-aware timer).
	sleep sleepFn

	// capCache memoizes probed model capabilities per endpoint+model so a
	// session re-uses one probe. Guarded by capCacheMu; lazily allocated.
	capCache map[string]Capabilities
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
	models := c.chain()
	var lastErr error
	tried := make([]string, 0, len(models))
	for _, model := range models {
		// Respect the overall deadline across the whole chain: a context already
		// done must not start another model's attempts.
		if err := ctx.Err(); err != nil {
			return Message{}, Usage{}, err
		}
		tried = append(tried, model)
		msg, usage, eligible, err := c.completeModel(ctx, model, msgs, tools)
		if err == nil {
			return msg, usage, nil
		}
		lastErr = err
		// A cancelled/deadline-exceeded context aborts the chain immediately.
		if !fallbackEligible(ctx, eligible, err) {
			break
		}
	}
	return Message{}, Usage{}, wrapChainError(tried, lastErr)
}

// completeModel performs one model's full attempt budget (its own retries) for a
// non-streaming completion. It returns the result on success, or the final error
// plus whether that error is fallback-eligible (i.e. the chain should advance to
// the next model). Cache lookup and store are keyed by THIS model, so a result
// produced under a fallback is cached under the fallback's key and usage is
// attributed to the model that actually answered.
func (c *Client) completeModel(ctx context.Context, model string, msgs []Message, tools []Tool) (Message, Usage, bool, error) {
	reqBody := ChatRequest{
		Model:       model,
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
			return msg, Usage{}, false, nil
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, Usage{}, false, fmt.Errorf("marshal request: %w", err)
	}

	attempts := c.Retry.attempts()
	var lastErr error
	var eligible bool
	for attempt := 0; attempt < attempts; attempt++ {
		msg, usage, retryAfter, retryable, fbEligible, err := c.completeOnce(ctx, body)
		if err == nil {
			if c.Cache != nil {
				c.Cache.put(reqBody, msg, usage)
			}
			return msg, usage, false, nil
		}
		lastErr = err
		eligible = fbEligible
		// Terminal error, or no attempts left: stop retrying this model.
		if !retryable || attempt == attempts-1 {
			break
		}
		if err := c.sleeper()(ctx, c.Retry.backoff(attempt, retryAfter)); err != nil {
			// Context cancelled/deadline during backoff: propagate; not eligible.
			return Message{}, Usage{}, false, err
		}
	}
	return Message{}, Usage{}, eligible, lastErr
}

// completeOnce performs a single non-streaming round-trip. It returns the parsed
// result on success, or an error plus: whether that error is retryable (same
// model), any server-supplied Retry-After, and whether the error is
// fallback-eligible (advance to the next model). retryable and fbEligible are
// independent: a 5xx is both (retry this model, then fall back); a 404 is
// fallback-eligible but not retried; a 401/400 is neither.
func (c *Client) completeOnce(ctx context.Context, body []byte) (msg Message, usage Usage, retryAfter time.Duration, retryable, fbEligible bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, 0, false, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Transport-level failure: retryable, and fallback-eligible, unless the
		// context is done (then neither).
		live := retryableErr(ctx, err)
		return Message{}, Usage{}, 0, live, live, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	// Retryable status (429/5xx): drain a bounded slice of the body for the error
	// message (don't grow memory on a hostile/huge error body) and report it.
	// These are also fallback-eligible once this model's retries are exhausted.
	if retryableStatus(resp.StatusCode) {
		errBody := readCapped(resp.Body)
		return Message{}, Usage{}, parseRetryAfter(resp.Header), true, true,
			fmt.Errorf("status %d: %s", resp.StatusCode, truncate(errBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// Reading the body failed mid-flight: a transient read error, retryable
		// and fallback-eligible (unless the context is done).
		live := retryableErr(ctx, err)
		return Message{}, Usage{}, 0, live, live, fmt.Errorf("read response: %w", err)
	}

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		// Decode failed; surface the raw body and status for debugging. Not retried.
		// On a >=300 status the body is an error page, not a real completion — a 404
		// (model not found) or 5xx there is still fallback-eligible even when the
		// body isn't JSON; a 2xx decode failure is a client-side problem (same for
		// any model), so it is terminal.
		fb := resp.StatusCode >= 300 && statusFallbackEligible(resp.StatusCode)
		return Message{}, Usage{}, 0, false, fb, fmt.Errorf("status %d: unmarshal response: %w; body=%s", resp.StatusCode, err, truncate(respBody))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		// A model-not-found provider error justifies trying the next model even
		// when the endpoint reported it with a non-404 status.
		return Message{}, Usage{}, 0, false, looksLikeModelNotFound(parsed.Error.Message),
			fmt.Errorf("provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		// A non-retryable >=300 (e.g. 4xx other than 429): terminal for this model;
		// 404 (model not found) is fallback-eligible, 401/403/400 are not.
		return Message{}, Usage{}, 0, false, statusFallbackEligible(resp.StatusCode),
			fmt.Errorf("status %d: %s", resp.StatusCode, truncate(respBody))
	}
	if len(parsed.Choices) == 0 {
		return Message{}, Usage{}, 0, false, false, fmt.Errorf("no choices in response; body=%s", truncate(respBody))
	}
	var u Usage
	if parsed.Usage != nil {
		u = *parsed.Usage
	}
	return parsed.Choices[0].Message, u, 0, false, false, nil
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
