package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(1, 3) // 1 token/sec, burst 3
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !rl.Allow("a") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if rl.Allow("a") {
		t.Error("4th request should be blocked (burst exhausted)")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(1, 1)
	rl.now = func() time.Time { return now }

	if !rl.Allow("a") {
		t.Fatal("first request should pass")
	}
	if rl.Allow("a") {
		t.Fatal("second immediate request should block")
	}
	now = now.Add(time.Second) // one token refilled
	if !rl.Allow("a") {
		t.Error("after 1s a token should be available")
	}
}

func TestRateLimiterPerKey(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(1, 1)
	rl.now = func() time.Time { return now }

	if !rl.Allow("a") || !rl.Allow("b") {
		t.Error("different keys have independent buckets")
	}
	if rl.Allow("a") {
		t.Error("key a should now be exhausted")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(1, 1)
	rl.now = func() time.Time { return now }
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := rateLimitMiddleware(base, rl, func(r *http.Request) string { return "k" })

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/run", nil))
	if rr.Code != 200 {
		t.Fatalf("first request = %d, want 200", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/run", nil))
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("second request = %d, want 429", rr.Code)
	}
}
