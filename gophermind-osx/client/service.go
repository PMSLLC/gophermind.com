// Package client is gophermind-osx's HTTP/SSE service client for
// gophermind-server: one method per route in the contract
// gophermind-lib/serve.NewMux's doc comment defines (.planning/tasks/
// 03-02.json). Every request carries the configured bearer token; POST/GET/
// PATCH/DELETE calls retry transient failures (network errors, 5xx) with
// exponential backoff, while SSE streams (Stream, RunStream, PipelineEvents)
// are not retried -- a partially-consumed event stream can't be safely
// replayed, so a stream error is returned to the caller to restart if it
// wants to.
//
// Two routes from the contract have no method here: POST /devices (S4 APNs
// push registration -- gophermind-server's buildDeps leaves Deps.Devices
// nil, so the route is never even registered server-side; nothing to call)
// and anything under "backends"/"backend-status" the task description
// mentions -- no such route exists anywhere in gophermind-lib/serve's
// contract (see webhook.go's NewMux doc comment, the authoritative route
// table). Documented rather than invented: adding client methods for
// server routes that don't exist would be worse than not having them.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"gophermind/gophermind-lib/modelcat"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/session"
	"gophermind/gophermind-lib/skills"
)

// DefaultTimeout is used when Config.Timeout is zero.
const DefaultTimeout = 30 * time.Second

// RetryPolicy controls Client's retry-with-backoff behavior for non-stream
// requests. The zero value is not usable directly -- use DefaultRetryPolicy.
type RetryPolicy struct {
	MaxAttempts int           // total tries, including the first; 1 disables retrying
	BaseDelay   time.Duration // first backoff interval
	MaxDelay    time.Duration // backoff is capped here regardless of attempt count
}

// DefaultRetryPolicy retries transient failures up to 3 additional times
// (4 attempts total) with backoff from 250ms up to 4s.
var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 4, BaseDelay: 250 * time.Millisecond, MaxDelay: 4 * time.Second}

// backoff returns the delay before attempt (1-indexed: attempt 1 is the
// first retry, after the initial try) -- exponential with full jitter
// (a random delay in [0, computed)), capped at MaxDelay. Full jitter avoids
// every failing client retrying in lockstep against a recovering server.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	d := p.BaseDelay << uint(attempt-1)
	if d > p.MaxDelay || d <= 0 { // d<=0 catches overflow from a large attempt count
		d = p.MaxDelay
	}
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d)))
}

// Config configures a new Client.
type Config struct {
	BaseURL string // e.g. "https://10.66.0.1:8090" (no trailing slash required)
	Token   string // bearer token sent with every request
	Timeout time.Duration
	Retry   RetryPolicy // zero value means DefaultRetryPolicy
	// Transport overrides the underlying http.Client's RoundTripper. Nil
	// uses Go's default transport (a direct connection) -- the right choice
	// for local mode. Remote mode (plan 03-03's connection manager) sets
	// this to a *wireguard.Client's HTTPClient().Transport, so every
	// request this Client makes routes through the WireGuard tunnel
	// instead of the host's real network stack.
	Transport http.RoundTripper
}

// Client is a gophermind-server HTTP/SSE client.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	retry   RetryPolicy
}

// New returns a Client for cfg. Config.Timeout <= 0 uses DefaultTimeout;
// a zero-value Config.Retry uses DefaultRetryPolicy.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	retry := cfg.Retry
	if retry.MaxAttempts <= 0 {
		retry = DefaultRetryPolicy
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: timeout, Transport: cfg.Transport},
		retry:   retry,
	}
}

// StatusError is returned when a request completes but the server responds
// with a non-2xx status. Exported so callers can inspect StatusCode
// (e.g. to distinguish 401 from 404) with errors.As.
type StatusError struct {
	StatusCode int
	Body       string // response body, truncated; never logged with the token
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("gophermind-server: status %d: %s", e.StatusCode, e.Body)
}

// isRetryableStatus reports whether a response status is worth retrying:
// 5xx (server-side, plausibly transient) but not 4xx (the request itself is
// wrong; retrying it unchanged will fail identically).
func isRetryableStatus(code int) bool {
	return code >= 500 && code <= 599
}

// do executes one request with retry-with-backoff for transient failures
// (network errors, 5xx). method/path/body build the request fresh on each
// attempt (a request's Body can only be read once). Returns the response
// body already read into memory and closed -- every current endpoint
// method's response is small JSON, so this keeps every caller simpler than
// threading io.ReadCloser lifetime through each of them; Stream/RunStream/
// PipelineEvents (SSE) bypass do entirely and manage the body themselves.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retry.backoff(attempt - 1)):
			}
		}

		respBody, status, err := c.attempt(ctx, method, path, body)
		if err == nil && !isRetryableStatus(status) {
			if status < 200 || status >= 300 {
				return nil, &StatusError{StatusCode: status, Body: string(respBody)}
			}
			return respBody, nil
		}
		if err == nil {
			lastErr = &StatusError{StatusCode: status, Body: string(respBody)}
		} else {
			lastErr = err
			if ctx.Err() != nil {
				return nil, lastErr // context cancelled/deadline: don't keep retrying
			}
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", c.retry.MaxAttempts, lastErr)
}

// attempt performs exactly one HTTP round trip. Returns (body, status, nil)
// for any response actually received (including a non-2xx one -- that's
// the caller's decision to retry or surface), or (nil, 0, err) for a
// network-level failure (err is always the retry-worthy case here; a
// non-network error, like a bad request build, would be a caller bug and
// currently can't happen given how these requests are constructed).
func (c *Client) attempt(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	c.authorize(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, 0, err
	}
	return respBody, resp.StatusCode, nil
}

// authorize applies the configured bearer token to req. Called on every
// request this client makes, streams included.
func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func decodeJSON[T any](body []byte) (T, error) {
	var v T
	err := json.Unmarshal(body, &v)
	return v, err
}

// --- Sessions ---

// CreateSessionOptions are optional fields for CreateSession; the zero
// value creates an anonymous session with server defaults.
type CreateSessionOptions struct {
	ID      string
	Model   string
	Mode    string
	Profile string
	Root    string
}

// CreateSession backs POST /session and returns the created session's id
// (server-generated when opts.ID is empty).
func (c *Client) CreateSession(ctx context.Context, opts CreateSessionOptions) (string, error) {
	body, _ := json.Marshal(struct {
		ID      string `json:"id,omitempty"`
		Model   string `json:"model,omitempty"`
		Mode    string `json:"mode,omitempty"`
		Profile string `json:"profile,omitempty"`
		Root    string `json:"root,omitempty"`
	}{opts.ID, opts.Model, opts.Mode, opts.Profile, opts.Root})
	respBody, err := c.do(ctx, http.MethodPost, "/session", body)
	if err != nil {
		return "", err
	}
	resp, err := decodeJSON[struct {
		ID string `json:"id"`
	}](respBody)
	return resp.ID, err
}

// ListSessions backs GET /session.
func (c *Client) ListSessions(ctx context.Context) ([]session.Info, error) {
	body, err := c.do(ctx, http.MethodGet, "/session", nil)
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]session.Info](body)
}

// DeleteSession backs DELETE /session/{id}.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/session/"+id, nil)
	return err
}

// RenameSession backs PATCH /session/{id}.
func (c *Client) RenameSession(ctx context.Context, id, name string) error {
	body, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	_, err := c.do(ctx, http.MethodPatch, "/session/"+id, body)
	return err
}

// SessionMessages backs GET /session/{id}/messages.
func (c *Client) SessionMessages(ctx context.Context, id string) ([]json.RawMessage, error) {
	body, err := c.do(ctx, http.MethodGet, "/session/"+id+"/messages", nil)
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]json.RawMessage](body)
}

// Approve backs POST /session/{id}/approve, resolving a pending
// "approval-needed" gate raised on that session's Stream.
func (c *Client) Approve(ctx context.Context, sessionID, approvalID string, approved bool) error {
	body, _ := json.Marshal(struct {
		ApprovalID string `json:"approval_id"`
		Approved   bool   `json:"approved"`
	}{approvalID, approved})
	_, err := c.do(ctx, http.MethodPost, "/session/"+sessionID+"/approve", body)
	return err
}

// Stream backs POST /session/{id}/stream: runs task in sessionID and
// returns a live SSE event stream. The caller must Close the returned
// *EventStream when done (also cancelling ctx has the same effect, by
// closing the underlying response body). Not retried -- see the package
// doc comment.
func (c *Client) Stream(ctx context.Context, sessionID, task string) (*EventStream, error) {
	return c.openSSE(ctx, http.MethodPost, "/session/"+sessionID+"/stream", strings.NewReader(task))
}

// --- Run (non-session) ---

// Run backs POST /run: one-shot, non-session task execution.
func (c *Client) Run(ctx context.Context, task string) (string, error) {
	body, err := c.do(ctx, http.MethodPost, "/run", []byte(task))
	return string(body), err
}

// RunStream backs POST /run/stream: the streaming sibling of Run.
func (c *Client) RunStream(ctx context.Context, task string) (*EventStream, error) {
	return c.openSSE(ctx, http.MethodPost, "/run/stream", strings.NewReader(task))
}

// --- Models ---

// ListModels backs GET /models.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	body, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]string](body)
}

// Catalogue backs GET /models/catalogue.
func (c *Client) Catalogue(ctx context.Context) ([]modelcat.Entry, error) {
	body, err := c.do(ctx, http.MethodGet, "/models/catalogue", nil)
	if err != nil {
		return nil, err
	}
	resp, err := decodeJSON[struct {
		Entries []modelcat.Entry `json:"entries"`
	}](body)
	return resp.Entries, err
}

// ModelSettings backs GET /models/settings.
func (c *Client) ModelSettings(ctx context.Context) (modelcat.Settings, error) {
	body, err := c.do(ctx, http.MethodGet, "/models/settings", nil)
	if err != nil {
		return modelcat.Settings{}, err
	}
	return decodeJSON[modelcat.Settings](body)
}

// PatchModelSettings backs PATCH /models/settings. patch is sent as-is
// (the server merges it into stored settings), letting callers update just
// the fields they changed without first fetching the whole settings object.
func (c *Client) PatchModelSettings(ctx context.Context, patch map[string]any) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPatch, "/models/settings", body)
	return err
}

// --- Skills ---

// SkillsList backs GET /skills.
func (c *Client) SkillsList(ctx context.Context) (sources []skills.Source, list []skills.Skill, err error) {
	body, err := c.do(ctx, http.MethodGet, "/skills", nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := decodeJSON[struct {
		Sources []skills.Source `json:"sources"`
		Skills  []skills.Skill  `json:"skills"`
	}](body)
	return resp.Sources, resp.Skills, err
}

// SetSkillEnabled backs PATCH /skills.
func (c *Client) SetSkillEnabled(ctx context.Context, key string, enabled bool) error {
	body, _ := json.Marshal(struct {
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}{key, enabled})
	_, err := c.do(ctx, http.MethodPatch, "/skills", body)
	return err
}

// AddSkillSource backs POST /skills/sources.
func (c *Client) AddSkillSource(ctx context.Context, url, ref string) error {
	body, _ := json.Marshal(struct {
		URL string `json:"url"`
		Ref string `json:"ref"`
	}{url, ref})
	_, err := c.do(ctx, http.MethodPost, "/skills/sources", body)
	return err
}

// RemoveSkillSource backs DELETE /skills/sources/{id}.
func (c *Client) RemoveSkillSource(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/skills/sources/"+id, nil)
	return err
}

// --- Pipeline ---

// PipelineState backs GET /pipeline/state.
func (c *Client) PipelineState(ctx context.Context) ([]phaseflow.Task, time.Time, error) {
	body, err := c.do(ctx, http.MethodGet, "/pipeline/state", nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	resp, err := decodeJSON[struct {
		Tasks       []phaseflow.Task `json:"tasks"`
		GeneratedAt time.Time        `json:"generated_at"`
	}](body)
	return resp.Tasks, resp.GeneratedAt, err
}

// PipelineReport backs GET /pipeline/report.
func (c *Client) PipelineReport(ctx context.Context) (phaseflow.RunReport, error) {
	body, err := c.do(ctx, http.MethodGet, "/pipeline/report", nil)
	if err != nil {
		return phaseflow.RunReport{}, err
	}
	return decodeJSON[phaseflow.RunReport](body)
}

// PipelineEvents backs GET /pipeline/events: the live SSE feed of
// task-status/task-attempt/wave-changed/run-report events. Not retried --
// see the package doc comment.
func (c *Client) PipelineEvents(ctx context.Context) (*EventStream, error) {
	return c.openSSE(ctx, http.MethodGet, "/pipeline/events", nil)
}

// --- Health (unauthenticated; no retry needed, these are meant to be cheap
// and frequent, e.g. for a connection-status indicator) ---

// Healthy reports whether GET /healthz returns 200.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
