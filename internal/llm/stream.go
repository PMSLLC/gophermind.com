package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrStreamIdle is the sentinel cause when a streaming turn is aborted
// because no SSE frame arrived within the configured idle timeout (see
// SetStreamIdleTimeout). It is distinct from context.Canceled/DeadlineExceeded
// so callers can tell a genuine stall apart from user/parent cancellation.
// Exported so callers can test for it with errors.Is.
var ErrStreamIdle = errors.New("stream idle timeout")

// defaultStreamIdleTimeout is used when a Client has no explicit
// SetStreamIdleTimeout override. It is generous enough to cover slow
// first-token latency from reasoning models (the timer is armed before the
// first frame, not just between later frames).
const defaultStreamIdleTimeout = 300 * time.Second

// streamIdleTimeout returns the configured idle/stall timeout for Stream,
// defaulting to defaultStreamIdleTimeout when unset.
func (c *Client) streamIdleTimeout() time.Duration {
	if c.streamIdleTimeoutOverride > 0 {
		return c.streamIdleTimeoutOverride
	}
	return defaultStreamIdleTimeout
}

// streamChunk is one SSE delta frame from /v1/chat/completions with stream=true.
// The final chunk (when stream_options.include_usage is set) carries usage and
// an empty choices array, so Usage is parsed alongside the deltas.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// Stream performs a streaming chat completion. onToken (if non-nil) is called for
// each prose delta as it arrives. The fully assembled assistant message — including
// tool calls reassembled from fragmented argument deltas — is returned when the
// stream ends, along with the token usage reported in the final chunk (zero value
// when the endpoint omits it).
func (c *Client) Stream(ctx context.Context, msgs []Message, tools []Tool, onToken func(string)) (Message, Usage, error) {
	// streamCtx derives from the parent ctx but can ALSO be cancelled by the
	// idle watchdog below, with a distinct cause (ErrStreamIdle) recoverable
	// via context.Cause. It — not the raw ctx — is what actually governs the
	// request/response body Read, so the watchdog can abort a stalled read.
	// cancelCause(nil) on return is a no-op if the stream finished normally or
	// was already cancelled for another reason.
	streamCtx, cancelCause := context.WithCancelCause(ctx)
	defer cancelCause(nil)

	// Fall back across models ONLY on the initial connect (pre-token). Once a 2xx
	// body is in hand, streaming begins and we never switch models mid-output: a
	// mid-stream break is surfaced rather than re-issued under another model, so
	// no tokens are ever duplicated or replayed.
	resp, err := c.connectStreamChain(streamCtx, msgs, tools)
	if err != nil {
		return Message{}, Usage{}, err
	}
	defer resp.Body.Close()

	// Idle/stall watchdog: armed before the first frame (covers slow
	// first-token/reasoning latency) and reset on every subsequent frame, so it
	// measures the GAP between frames, not the stream's total duration. Only
	// this goroutine (the read loop, below) ever calls timer.Reset; the
	// AfterFunc callback only cancels streamCtx, which is safe to do from any
	// goroutine.
	idle := c.streamIdleTimeout()
	timer := time.AfterFunc(idle, func() { cancelCause(ErrStreamIdle) })
	defer timer.Stop()
	idleErr := func() error {
		return fmt.Errorf("stream idle timeout: no data for %s: %w", idle, ErrStreamIdle)
	}

	var content strings.Builder
	type acc struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*acc{}
	var order []int
	var usage Usage

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		// A frame just arrived: reset the idle window. Only this goroutine calls
		// Reset, so there is no race with the AfterFunc callback (which only
		// cancels streamCtx). Benign boundary case: if a frame arrives at the
		// exact instant the idle timer fires, the AfterFunc callback may already
		// be running and still cancel with ErrStreamIdle despite the reset —
		// harmless, since it only happens at the true stall boundary, not during
		// normal token flow.
		timer.Reset(idle)

		// Idle-cancel is checked FIRST so a stall reported via streamCtx's cause
		// is never mistaken for a bare parent cancellation. Then honor parent
		// cancellation between frames so a mid-stream Ctrl-C aborts promptly with
		// context.Canceled instead of processing buffered chunks or waiting on
		// the next read. The request's context also unblocks the underlying
		// Read, so a stall mid-frame is cut short too; this check makes the abort
		// deterministic and the returned error a clean ctx.Err().
		if errors.Is(context.Cause(streamCtx), ErrStreamIdle) {
			return Message{}, Usage{}, idleErr()
		}
		if err := ctx.Err(); err != nil {
			return Message{}, Usage{}, err
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // tolerate keep-alives / partial frames
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage // final chunk; usually carries no choices
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			if onToken != nil {
				onToken(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			a := calls[tc.Index]
			if a == nil {
				a = &acc{}
				calls[tc.Index] = a
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			a.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		// Idle-cancel is checked FIRST — see the loop-top check above for why —
		// so a stall is always reported as ErrStreamIdle, never a bare ctx error,
		// even though cancelling streamCtx also unblocks the body Read via a
		// transport error that wraps it.
		if errors.Is(context.Cause(streamCtx), ErrStreamIdle) {
			return Message{}, Usage{}, idleErr()
		}
		// A cancelled/expired PARENT context unblocks the body read with a
		// transport error that wraps the ctx error; surface it as the clean
		// ctx.Err() so callers (and the agent loop) can recognize cancellation
		// uniformly and discard the partial turn rather than treating it as a
		// stream fault.
		if cerr := ctx.Err(); cerr != nil {
			return Message{}, Usage{}, cerr
		}
		return Message{}, Usage{}, fmt.Errorf("read stream: %w", err)
	}

	msg := Message{Role: "assistant", Content: content.String()}
	for _, idx := range order {
		a := calls[idx]
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:       a.id,
			Type:     "function",
			Function: FunctionCall{Name: a.name, Arguments: a.args.String()},
		})
	}
	return msg, usage, nil
}

// connectStreamChain establishes the streaming connection, trying each model in
// the chain in order. For each model it exhausts that model's own connect
// retries; if the final connect error is fallback-eligible it advances to the
// next model. It returns the first live 2xx response (body unread, to be
// streamed exactly once), or the aggregated error when the whole chain fails.
// Falling back happens only pre-token, here.
func (c *Client) connectStreamChain(ctx context.Context, msgs []Message, tools []Tool) (*http.Response, error) {
	models := c.chain()
	var lastErr error
	tried := make([]string, 0, len(models))
	for _, model := range models {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tried = append(tried, model)
		resp, eligible, err := c.connectStreamModel(ctx, model, msgs, tools)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !fallbackEligible(ctx, eligible, err) {
			break
		}
	}
	return nil, wrapChainError(tried, lastErr)
}

// connectStreamModel runs one model's connect attempt budget (its own retries)
// and returns a live 2xx response, or the final error plus whether it is
// fallback-eligible (advance to the next model). Retries happen only here,
// before any token is emitted.
func (c *Client) connectStreamModel(ctx context.Context, model string, msgs []Message, tools []Tool) (*http.Response, bool, error) {
	temp, topP, effort := c.sampling()
	reqBody := ChatRequest{
		Model:           model,
		Messages:        msgs,
		Tools:           tools,
		Temperature:     temp,
		TopP:            topP,
		ReasoningEffort: effort,
		Stream:          true,
		StreamOptions:   &StreamOptions{IncludeUsage: true},
	}
	if tc := c.toolChoiceValue(tools); tc != nil {
		reqBody.ToolChoice = tc
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, false, fmt.Errorf("marshal request: %w", err)
	}

	attempts := c.Retry.attempts()
	var lastErr error
	var eligible bool
	for attempt := 0; attempt < attempts; attempt++ {
		resp, retryAfter, retryable, fbEligible, err := c.streamConnectOnce(ctx, body)
		if err == nil {
			return resp, false, nil
		}
		lastErr = err
		eligible = fbEligible
		if !retryable || attempt == attempts-1 {
			break
		}
		if err := c.sleeper()(ctx, c.Retry.backoff(attempt, retryAfter)); err != nil {
			return nil, false, err
		}
	}
	return nil, eligible, lastErr
}

// streamConnectOnce performs a single connect attempt. On a non-2xx it consumes
// and closes the body so the connection can be reused, and reports whether the
// failure is retryable (same model), any Retry-After, and whether it is
// fallback-eligible (advance to the next model). On success it returns the live
// response with its body intact for streaming.
func (c *Client) streamConnectOnce(ctx context.Context, body []byte) (*http.Response, time.Duration, bool, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		live := retryableErr(ctx, err)
		return nil, 0, live, live, fmt.Errorf("perform request: %w", err)
	}
	if resp.StatusCode >= 300 {
		b := readCapped(resp.Body)
		resp.Body.Close()
		retry := retryableStatus(resp.StatusCode)
		var ra time.Duration
		if retry {
			ra = parseRetryAfter(resp.Header)
		}
		return nil, ra, retry, statusFallbackEligible(resp.StatusCode), fmt.Errorf("status %d: %s", resp.StatusCode, truncate(b))
	}
	return resp, 0, false, false, nil
}
