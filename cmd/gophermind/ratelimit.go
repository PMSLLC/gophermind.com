package main

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bucket is a single caller's token-bucket state.
type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is an in-memory per-key token-bucket limiter for the serve
// webhook, so a single caller can't monopolize the agent.
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens refilled per second
	burst   float64 // bucket capacity
	buckets map[string]*bucket
	now     func() time.Time
}

// newRateLimiter builds a limiter refilling ratePerSec tokens/second up to burst.
func newRateLimiter(ratePerSec, burst float64) *rateLimiter {
	return &rateLimiter{
		rate:    ratePerSec,
		burst:   burst,
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// Allow reports whether a request keyed by key may proceed, consuming a token.
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	} else {
		elapsed := now.Sub(b.last).Seconds()
		b.tokens += elapsed * rl.rate
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// rateLimitMiddleware rejects requests over the limit for their key with 429.
func rateLimitMiddleware(next http.Handler, rl *rateLimiter, keyOf func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(keyOf(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serveRateLimiter builds the limiter from GOPHERMIND_SERVE_RATE (requests per
// minute, burst = that many). Returns nil when unset/invalid (limiting off).
func serveRateLimiter() *rateLimiter {
	v := strings.TrimSpace(os.Getenv("GOPHERMIND_SERVE_RATE"))
	if v == "" {
		return nil
	}
	perMin, err := strconv.ParseFloat(v, 64)
	if err != nil || perMin <= 0 {
		return nil
	}
	return newRateLimiter(perMin/60.0, perMin)
}
