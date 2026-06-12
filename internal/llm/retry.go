package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy bounds automatic retries for Complete and the initial connect of
// Stream. The zero value (MaxAttempts 0/1) disables retries: a single attempt
// is always made. Backoff is exponential with jitter, capped at MaxDelay.
type RetryPolicy struct {
	MaxAttempts int           // total tries; <=1 means no retries
	BaseDelay   time.Duration // first backoff interval
	MaxDelay    time.Duration // cap on any single backoff (and on Retry-After)
}

// DefaultRetryPolicy is used when a Client is built without an explicit policy.
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 3,
	BaseDelay:   250 * time.Millisecond,
	MaxDelay:    30 * time.Second,
}

// retryAfterCap bounds how long we will honor a server-supplied Retry-After,
// independent of the per-attempt MaxDelay, so a hostile or buggy server cannot
// make the client hang for minutes/days. Backoff is additionally bounded by the
// caller's context deadline inside the sleeper.
const retryAfterCap = 60 * time.Second

// attempts returns the effective number of tries (always >= 1).
func (p RetryPolicy) attempts() int {
	if p.MaxAttempts < 1 {
		return 1
	}
	return p.MaxAttempts
}

// backoff computes the sleep before the next try for a given zero-based attempt
// index. retryAfter (>0) overrides the computed delay but is itself capped.
func (p RetryPolicy) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > retryAfterCap {
			return retryAfterCap
		}
		return retryAfter
	}
	base := p.BaseDelay
	if base <= 0 {
		base = DefaultRetryPolicy.BaseDelay
	}
	maxD := p.MaxDelay
	if maxD <= 0 {
		maxD = DefaultRetryPolicy.MaxDelay
	}
	// Exponential growth: base * 2^attempt, guarding against overflow by
	// clamping to maxD before applying jitter.
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d <= 0 || d > maxD { // overflow or past cap
			d = maxD
			break
		}
	}
	if d > maxD {
		d = maxD
	}
	// Full jitter in [d/2, d]: keeps a floor so retries still space out, while
	// spreading load to avoid thundering herds.
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// retryableStatus reports whether an HTTP status code is worth retrying.
// 429 and 5xx are transient; every other 4xx is terminal.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// retryableErr reports whether a transport error from http.Client.Do is worth
// retrying. Context cancellation/deadline is never retried; genuine network
// faults (connection refused/reset, timeouts, temporary errors) are.
func retryableErr(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Anything else reaching here is a transport-level failure (DNS, dial,
	// connection reset, read/write timeout). Treat it as transient.
	return true
}

// parseRetryAfter parses an untrusted Retry-After header. Only the delta-seconds
// form is honored (the HTTP-date form is ignored as not worth the surface area).
// Negative, zero, garbage, and absurd values yield 0 (fall back to computed
// backoff); positive values are returned as-is and capped later.
func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// sleepFn pauses for d unless ctx is cancelled first, in which case it returns
// the context error immediately. It is a field on Client so tests can inject a
// fast/no-op sleeper.
type sleepFn func(ctx context.Context, d time.Duration) error

// ctxSleep is the production sleeper: it never sleeps past the context deadline.
func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
