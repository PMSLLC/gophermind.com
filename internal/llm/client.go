package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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

	// sampleMu guards temperature/topP, which can be mutated at runtime (the TUI
	// /temp and /topp commands) while the agent goroutine reads them per request.
	sampleMu    sync.RWMutex
	temperature float64  // sent with every request; 0 by default (deterministic)
	topP        *float64 // sent only when non-nil; nil (default) omits top_p
	// reasoningEffort is the OpenAI-compatible reasoning-effort level sent with
	// each request; empty (the default) omits it. Guarded by sampleMu. Set via
	// --think.
	reasoningEffort string

	// capCache memoizes probed model capabilities per endpoint+model so a
	// session re-uses one probe. Guarded by capCacheMu; lazily allocated.
	capCache map[string]Capabilities

	// baseTransport is the TLS-configured RoundTripper assembled at construction,
	// captured so Use() can re-wrap it with the registered middleware chain
	// without losing the TLS settings. nil means "http.DefaultTransport" was in
	// effect (the plain-client path); Use() resolves that lazily.
	baseTransport http.RoundTripper

	// middlewares is the ordered list of registered HTTP middlewares. Index 0 is
	// the OUTERMOST wrapper (its request hook runs first). Empty (the default)
	// means the base transport is used directly, with zero wrapping overhead.
	middlewares []Middleware

	// toolChoice is the default tool_choice to send with each request. When nil
	// (the default), the client sends "auto" when tools are present and omits
	// the field entirely when tools are absent. Set to a non-nil ToolChoiceConfig
	// to override: force a specific tool, require tool calls, or forbid them.
	toolChoice *ToolChoiceConfig

	// totalTimeout is the per-attempt bound for the NON-streaming path
	// (Complete), applied as a context deadline in completeOnce. The shared
	// HTTP client itself has no total-request cap (see httpClientFor); this
	// field is what preserves Complete's old "whole round-trip" timeout
	// semantics now that Client.Timeout is 0. Zero disables the bound.
	totalTimeout time.Duration

	// streamIdleTimeoutOverride is the configured idle/stall timeout for
	// Stream, set via SetStreamIdleTimeout. Zero (the default) means "use the
	// package default" — see streamIdleTimeout().
	streamIdleTimeoutOverride time.Duration
}

// New constructs a Client. timeout bounds a single completion round-trip.
// When insecureTLS is true, server certificate verification is skipped — used
// for self-signed internal endpoints reached over a trusted VPN.
//
// New is the simple, backward-compatible constructor and cannot fail. For
// optional client-certificate (mutual TLS) auth or a custom CA bundle — the
// SECURE alternative to insecureTLS — use NewWithTLS, which validates the cert
// material and returns a config-time error.
func New(baseURL, apiKey, model string, timeout time.Duration, insecureTLS bool) *Client {
	// The bool-only path never has cert/key/CA, so buildTLSConfig cannot error;
	// NewWithTLS is the same code with validation surfaced.
	c, _ := NewWithTLS(baseURL, apiKey, model, timeout, TLSOptions{InsecureSkipVerify: insecureTLS})
	return c
}

// NewWithTLS constructs a Client with explicit TLS options, supporting optional
// mutual-TLS (client certificate) and a custom CA bundle for verifying the
// server — the secure way to reach internal endpoints without disabling
// verification. It returns a config-time error if the cert/key/CA material is
// missing, unreadable, malformed, or only one of cert/key is supplied. The
// private key is loaded via tls.LoadX509KeyPair and never logged.
func NewWithTLS(baseURL, apiKey, model string, timeout time.Duration, tlsOpts TLSOptions) (*Client, error) {
	httpClient, err := httpClientFor(timeout, tlsOpts)
	if err != nil {
		return nil, err
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    httpClient,
		Retry:   DefaultRetryPolicy,
		sleep:   ctxSleep,
		// Capture the TLS-configured base transport (nil here means the default
		// transport, which Use() resolves lazily) so middleware can wrap it
		// without discarding the TLS configuration.
		baseTransport: httpClient.Transport,
		// totalTimeout preserves timeout's old meaning (the whole non-streaming
		// round-trip) now that the shared HTTP client itself has no cap.
		totalTimeout: timeout,
	}, nil
}

// SetStreamIdleTimeout sets the idle/stall timeout used by Stream: if no SSE
// frame arrives within this duration (measured between frames, and before the
// first), the stream is aborted with an error wrapping ErrStreamIdle. Zero
// restores the package default (300s). Concurrency-safe to call before
// issuing requests; not intended to be changed concurrently with an in-flight
// Stream call.
func (c *Client) SetStreamIdleTimeout(d time.Duration) {
	c.streamIdleTimeoutOverride = d
}

// currentBase resolves the base transport, treating nil as the default.
func (c *Client) currentBase() http.RoundTripper {
	if c.baseTransport != nil {
		return c.baseTransport
	}
	return http.DefaultTransport
}

// rebuildTransport re-chains any registered middleware on top of the (possibly
// updated) base transport.
func (c *Client) rebuildTransport() {
	if c.HTTP == nil {
		c.HTTP = &http.Client{}
	}
	c.HTTP.Transport = chainMiddleware(c.baseTransport, c.middlewares)
}

// EnableRecording wraps the client's base transport with a recorder that appends
// each real interaction to the cassette at path, so a session can be captured
// and later replayed offline. Non-streaming (Complete) use only.
func (c *Client) EnableRecording(path string) {
	c.baseTransport = NewRecorder(c.currentBase(), path)
	c.rebuildTransport()
}

// EnableReplay replaces the client's base transport with a replayer that serves
// responses from the cassette at path, never touching the network — for
// hermetic, deterministic agent tests.
func (c *Client) EnableReplay(path string) error {
	rp, err := NewReplayer(path)
	if err != nil {
		return err
	}
	c.baseTransport = rp
	c.rebuildTransport()
	return nil
}

// Use registers one or more HTTP middlewares on the client and rebuilds the
// transport so subsequent requests — both Complete and Stream, which share this
// *http.Client — flow through them. Middlewares are appended in order; the
// FIRST registered is the OUTERMOST wrapper (its request hook runs first on the
// way out, its response hook runs last on the way back). Calling Use multiple
// times accumulates the chain in registration order.
//
// When no middleware has ever been registered, the client uses the base
// transport directly with zero wrapping overhead — behavior identical to today.
//
// Use is NOT safe to call concurrently with in-flight requests; register
// middleware at setup time, before issuing requests.
func (c *Client) Use(mws ...Middleware) {
	if len(mws) == 0 {
		return
	}
	c.middlewares = append(c.middlewares, mws...)
	if c.HTTP == nil {
		c.HTTP = &http.Client{}
	}
	// Resolve the base transport once: a nil baseTransport means the plain client
	// path where http.DefaultTransport applies. Capture and reuse it so repeated
	// Use() calls keep wrapping the SAME base, not a previously-wrapped chain.
	if c.baseTransport == nil {
		c.baseTransport = http.DefaultTransport
	}
	c.HTTP.Transport = chainMiddleware(c.baseTransport, c.middlewares)
}

// sleeper returns the configured sleeper, defaulting to ctxSleep when a Client
// was constructed without New (e.g. a struct literal).
func (c *Client) sleeper() sleepFn {
	if c.sleep != nil {
		return c.sleep
	}
	return ctxSleep
}

// SetTemperature sets the sampling temperature sent with subsequent requests.
// It is safe to call concurrently with in-flight requests; the new value takes
// effect on the next request. The caller is responsible for validating the
// range (see config.ValidateTemperature).
func (c *Client) SetTemperature(t float64) {
	c.sampleMu.Lock()
	c.temperature = t
	c.sampleMu.Unlock()
}

// SetTopP sets the nucleus-sampling top_p sent with subsequent requests. A nil
// argument unsets top_p (it is then omitted from requests). Concurrency-safe;
// effective on the next request. Range validation is the caller's job (see
// config.ValidateTopP).
func (c *Client) SetTopP(p *float64) {
	c.sampleMu.Lock()
	if p == nil {
		c.topP = nil
	} else {
		v := *p // copy so the caller can't mutate our stored value later
		c.topP = &v
	}
	c.sampleMu.Unlock()
}

// SetReasoningEffort sets the reasoning-effort hint (low|medium|high) sent with
// subsequent requests. An empty string unsets it (then omitted from requests).
// Concurrency-safe; effective on the next request.
func (c *Client) SetReasoningEffort(level string) {
	c.sampleMu.Lock()
	c.reasoningEffort = level
	c.sampleMu.Unlock()
}

// ReasoningEffort returns the current reasoning-effort level ("" when unset).
func (c *Client) ReasoningEffort() string {
	c.sampleMu.RLock()
	defer c.sampleMu.RUnlock()
	return c.reasoningEffort
}

// Temperature returns the current sampling temperature.
func (c *Client) Temperature() float64 {
	c.sampleMu.RLock()
	defer c.sampleMu.RUnlock()
	return c.temperature
}

// TopP returns the current top_p (nil when unset).
func (c *Client) TopP() *float64 {
	c.sampleMu.RLock()
	defer c.sampleMu.RUnlock()
	if c.topP == nil {
		return nil
	}
	v := *c.topP
	return &v
}

// SetToolChoice sets the default tool_choice for subsequent requests. Pass nil
// to restore the default behavior (auto when tools are present, omitted otherwise).
func (c *Client) SetToolChoice(tc *ToolChoiceConfig) {
	c.toolChoice = tc
}

// toolChoiceValue returns the tool_choice value to embed in a request body.
// When c.toolChoice is set it is used verbatim; otherwise it falls back to
// "auto" when tools are present.
func (c *Client) toolChoiceValue(tools []Tool) any {
	if c.toolChoice != nil {
		return c.toolChoice.toolChoiceString()
	}
	if len(tools) > 0 {
		return "auto"
	}
	return nil
}

// sampling reads the current temperature and top_p under one lock, for building
// a request body. The returned top_p is a copy, safe to embed in a request.
func (c *Client) sampling() (float64, *float64, string) {
	c.sampleMu.RLock()
	defer c.sampleMu.RUnlock()
	if c.topP == nil {
		return c.temperature, nil, c.reasoningEffort
	}
	v := *c.topP
	return c.temperature, &v, c.reasoningEffort
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
	temp, topP, effort := c.sampling()
	reqBody := ChatRequest{
		Model:           model,
		Messages:        msgs,
		Tools:           tools,
		Temperature:     temp,
		TopP:            topP,
		ReasoningEffort: effort,
		Stream:          false,
	}
	if tc := c.toolChoiceValue(tools); tc != nil {
		reqBody.ToolChoice = tc
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
	// Bound this attempt's whole round-trip (connect + headers + body read) via
	// context, since the shared HTTP client no longer has a total Timeout.
	// Per-attempt (not per-chain/per-model-loop) matches the old Client.Timeout
	// semantics, which applied per c.HTTP.Do call.
	if c.totalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.totalTimeout)
		defer cancel()
	}
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

// ListModels queries GET /v1/models and returns the ids of every model the
// endpoint serves, in the order returned (empty ids are skipped). It backs both
// auto-discovery and startup validation of a configured model, so a typo can be
// reported against the actual list of what the endpoint offers.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("status %d: parse models: %w; body=%s", resp.StatusCode, err, truncate(body))
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// DiscoverModel queries GET /v1/models and returns the id of the first model
// the endpoint serves. Used when no model is configured.
func (c *Client) DiscoverModel(ctx context.Context) (string, error) {
	ids, err := c.ListModels(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("endpoint served no models")
	}
	return ids[0], nil
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
