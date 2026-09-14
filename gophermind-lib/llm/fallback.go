package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// maxChainModels caps how many models a single request will ever try, across
// the primary plus configured fallbacks. A hostile or fat-fingered env value
// (e.g. thousands of comma-separated names) must not be able to inflate the
// attempt budget — total work is bounded by maxChainModels × per-model retries.
const maxChainModels = 8

// chain returns the ordered list of models to try: the primary Model first,
// then the configured Fallbacks. Entries are trimmed, empties dropped, exact
// duplicates removed (a duplicate would only waste an attempt on the same
// backend), and the result is capped at maxChainModels. The primary is always
// first when non-empty, preserving exact single-model behavior when Fallbacks
// is empty/unset.
func (c *Client) chain() []string {
	out := make([]string, 0, 1+len(c.Fallbacks))
	seen := map[string]struct{}{}
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" {
			return
		}
		if _, dup := seen[m]; dup {
			return
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	add(c.Model)
	for _, m := range c.Fallbacks {
		add(m)
	}
	if len(out) > maxChainModels {
		out = out[:maxChainModels]
	}
	return out
}

// fallbackEligible reports whether, after a model's own retries are exhausted,
// the chain should advance to the next model. The decision is conservative and
// status/error-based — never natural-language refusal detection.
//
// Eligible (try the next model): transport/network errors, 5xx, 429, and
// model-not-found (404 / "model does not exist"). These are conditions where a
// different model on the same endpoint plausibly succeeds.
//
// NOT eligible (terminal — return immediately):
//   - context cancellation/deadline: the caller is done; propagate at once.
//   - auth failures (401/403): all chain models share one endpoint+key here, so
//     another model would fail identically. No point trying.
//   - other terminal errors (400, malformed response, etc.): a different model
//     on the same request would hit the same client-side problem.
//
// classifyComplete/classifyStream feed this a precomputed eligibility derived
// from the actual status/transport result; ctx is checked first so a cancelled
// context always wins regardless of how the per-model attempt happened to fail.
func fallbackEligible(ctx context.Context, perModelEligible bool, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return perModelEligible
}

// statusFallbackEligible classifies an HTTP status (after that model's retries
// are exhausted) for fallback. 5xx and 429 (transient capacity/outage) and 404
// (model not found) justify trying another model; 401/403 (auth) and other 4xx
// are terminal.
func statusFallbackEligible(code int) bool {
	switch {
	case code == http.StatusNotFound:
		return true
	case code == http.StatusTooManyRequests:
		return true
	case code >= 500:
		return true
	default:
		return false
	}
}

// looksLikeModelNotFound matches provider error messages that indicate the named
// model does not exist on the endpoint, so the chain can fall through even when
// the provider reports it as a generic (non-404) JSON error. Kept to concrete,
// model-existence phrasing only — NOT a refusal classifier.
func looksLikeModelNotFound(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "model does not exist") ||
		strings.Contains(m, "model not found") ||
		strings.Contains(m, "no such model") ||
		strings.Contains(m, "unknown model") ||
		strings.Contains(m, "does not exist or you do not have access")
}

// chainError aggregates which models were tried when the whole chain fails. It
// wraps the last error so callers can still errors.Is/As through it. The message
// lists only model names and the (already-truncated) last error — never the API
// key, Authorization header, request body, or full response bodies.
type chainError struct {
	tried []string
	last  error
}

func (e *chainError) Error() string {
	return fmt.Sprintf("all %d model(s) failed [tried: %s]: %v",
		len(e.tried), strings.Join(e.tried, ", "), e.last)
}

func (e *chainError) Unwrap() error { return e.last }

// wrapChainError builds the aggregate error for an exhausted chain. With a
// single model (no real fallback) it returns the last error unwrapped, so
// existing single-model error behavior and messages are unchanged.
func wrapChainError(tried []string, last error) error {
	if len(tried) <= 1 {
		return last
	}
	return &chainError{tried: tried, last: last}
}
