package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// fastClient builds a Client whose retry sleeps are recorded but not actually
// slept, so retry tests run in microseconds and assert on attempt counts and
// backoff bounds deterministically.
func fastClient(url string, attempts int) (*Client, *[]time.Duration) {
	c := New(url, "", "m", 5*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: attempts, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond}
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return c, &slept
}

func TestCompleteRetriesOn429ThenSucceeds(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c, slept := fastClient(srv.URL, 3)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "ok" {
		t.Errorf("content = %q", msg.Content)
	}
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
	if len(*slept) != 1 {
		t.Errorf("backoff sleeps = %d, want 1", len(*slept))
	}
}

func TestCompleteRetriesOn500ThenSucceeds(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"recovered"}}]}`))
	}))
	defer srv.Close()

	c, _ := fastClient(srv.URL, 4)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "recovered" {
		t.Errorf("content = %q", msg.Content)
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Errorf("server hits = %d, want 3", got)
	}
}

func TestCompleteGivesUpAfterMaxAttempts(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	}))
	defer srv.Close()

	c, slept := fastClient(srv.URL, 3)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Errorf("server hits = %d, want 3", got)
	}
	if len(*slept) != 2 {
		t.Errorf("backoff sleeps = %d, want 2 (attempts-1)", len(*slept))
	}
}

func TestCompleteDoesNotRetryOn4xx(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		var n int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&n, 1)
			w.WriteHeader(code)
			w.Write([]byte(`{"error":{"message":"terminal"}}`))
		}))

		c, _ := fastClient(srv.URL, 3)
		_, _, err := c.Complete(context.Background(), nil, nil)
		srv.Close()
		if err == nil {
			t.Errorf("code %d: expected error", code)
		}
		if got := atomic.LoadInt32(&n); got != 1 {
			t.Errorf("code %d: server hits = %d, want 1 (no retry)", code, got)
		}
	}
}

func TestCompleteRetriesOnNetworkError(t *testing.T) {
	// Point at a closed server so Do() returns a connection error; it should be
	// treated as transient and retried up to the attempt cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	c, slept := fastClient(url, 3)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if len(*slept) != 2 {
		t.Errorf("backoff sleeps = %d, want 2", len(*slept))
	}
}

func TestCompleteHonorsRetryAfterSeconds(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Second}
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error { slept = append(slept, d); return nil }

	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(slept) != 1 {
		t.Fatalf("sleeps = %d, want 1", len(slept))
	}
	if slept[0] != 2*time.Second {
		t.Errorf("Retry-After honored as %v, want 2s", slept[0])
	}
}

func TestCompleteCapsAbsurdRetryAfter(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", strconv.Itoa(999999)) // ~11.5 days
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Second}
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error { slept = append(slept, d); return nil }

	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(slept) != 1 {
		t.Fatalf("sleeps = %d, want 1", len(slept))
	}
	if slept[0] > retryAfterCap {
		t.Errorf("absurd Retry-After not capped: slept %v, cap %v", slept[0], retryAfterCap)
	}
}

func TestCompleteContextCancelAbortsBackoff(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: 5, BaseDelay: 1 * time.Millisecond, MaxDelay: time.Second}
	// First backoff cancels the context, so no further attempt should run.
	c.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, _, err := c.Complete(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Errorf("server hits = %d, want 1 (cancel aborts retries)", got)
	}
}

func TestRetryDisabledSingleAttempt(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	for _, attempts := range []int{0, 1} {
		atomic.StoreInt32(&n, 0)
		c, slept := fastClient(srv.URL, attempts)
		_, _, err := c.Complete(context.Background(), nil, nil)
		if err == nil {
			t.Fatalf("attempts=%d: expected error", attempts)
		}
		if got := atomic.LoadInt32(&n); got != 1 {
			t.Errorf("attempts=%d: server hits = %d, want 1", attempts, got)
		}
		if len(*slept) != 0 {
			t.Errorf("attempts=%d: sleeps = %d, want 0", attempts, len(*slept))
		}
	}
}

func TestStreamRetriesInitialNon2xxThenSucceeds(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"hi"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c, slept := fastClient(srv.URL, 3)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "hi" || msg.Content != "hi" {
		t.Errorf("tokens=%q content=%q, want hi", got, msg.Content)
	}
	if hits := atomic.LoadInt32(&n); hits != 2 {
		t.Errorf("server hits = %d, want 2", hits)
	}
	if len(*slept) != 1 {
		t.Errorf("sleeps = %d, want 1", len(*slept))
	}
}

func TestStreamDoesNotReplayAfterTokens(t *testing.T) {
	// Server returns 200 and starts streaming, then the body ends abruptly
	// mid-stream (no [DONE]). The client must NOT retry once tokens have been
	// delivered — it surfaces what it has without re-hitting the server.
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"par"}}]}` + "\n\n"))
		if f != nil {
			f.Flush()
		}
		// Connection closes here with no terminating [DONE]; the scanner just
		// reaches EOF. This must not trigger a retry.
	}))
	defer srv.Close()

	c, slept := fastClient(srv.URL, 3)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream returned error after partial output: %v", err)
	}
	if got != "par" || msg.Content != "par" {
		t.Errorf("partial output = %q / %q, want par", got, msg.Content)
	}
	if hits := atomic.LoadInt32(&n); hits != 1 {
		t.Errorf("server hits = %d, want 1 (no replay after tokens)", hits)
	}
	if len(*slept) != 0 {
		t.Errorf("sleeps = %d, want 0 (no retry mid-stream)", len(*slept))
	}
}
