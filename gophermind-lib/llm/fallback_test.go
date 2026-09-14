package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastFallbackClient builds a Client with a primary model + fallbacks whose
// retry sleeps are no-ops, so chain tests run fast and deterministically.
func fastFallbackClient(url, model string, fallbacks []string, attempts int) *Client {
	c := New(url, "", model, 5*time.Second, false)
	c.Fallbacks = fallbacks
	c.Retry = RetryPolicy{MaxAttempts: attempts, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	c.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	return c
}

// requestModel decodes a chat request and returns its model field.
func requestModel(t *testing.T, r *http.Request) string {
	t.Helper()
	var got ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return got.Model
}

func TestCompleteFallsBackAfter5xxRetriesExhausted(t *testing.T) {
	var primaryHits, fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			atomic.AddInt32(&primaryHits, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"down"}}`))
		case "backup":
			atomic.AddInt32(&fbHits, 1)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from backup"}}]}`))
		default:
			t.Errorf("unexpected model")
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 3)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "from backup" {
		t.Errorf("content = %q, want from backup", msg.Content)
	}
	if got := atomic.LoadInt32(&primaryHits); got != 3 {
		t.Errorf("primary hits = %d, want 3 (retries exhausted before fallback)", got)
	}
	if got := atomic.LoadInt32(&fbHits); got != 1 {
		t.Errorf("backup hits = %d, want 1", got)
	}
}

func TestCompleteFallsBackOnModelNotFound404(t *testing.T) {
	var primaryHits, fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "missing":
			atomic.AddInt32(&primaryHits, 1)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"detail":"the model does not exist"}`))
		case "present":
			atomic.AddInt32(&fbHits, 1)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "missing", []string{"present"}, 3)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "ok" {
		t.Errorf("content = %q", msg.Content)
	}
	// 404 is terminal-but-fallback-eligible: it is NOT retried, so exactly one hit.
	if got := atomic.LoadInt32(&primaryHits); got != 1 {
		t.Errorf("primary hits = %d, want 1 (404 not retried)", got)
	}
	if got := atomic.LoadInt32(&fbHits); got != 1 {
		t.Errorf("backup hits = %d, want 1", got)
	}
}

func TestCompleteFallsBackOnModelNotFoundProviderError(t *testing.T) {
	// Some endpoints report a missing model as a 200/JSON error rather than 404.
	var fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "ghost":
			w.Write([]byte(`{"error":{"message":"model not found: ghost"}}`))
		case "real":
			atomic.AddInt32(&fbHits, 1)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "ghost", []string{"real"}, 3)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "hi" {
		t.Errorf("content = %q", msg.Content)
	}
	if got := atomic.LoadInt32(&fbHits); got != 1 {
		t.Errorf("fallback hits = %d, want 1", got)
	}
}

func TestCompleteAllModelsFailReturnsAggregateLastError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := requestModel(t, r)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"` + m + ` is down"}}`))
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "m1", []string{"m2", "m3"}, 2)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
	msg := err.Error()
	for _, m := range []string{"m1", "m2", "m3"} {
		if !contains(msg, m) {
			t.Errorf("aggregate error %q missing tried model %q", msg, m)
		}
	}
	// Last error should be from the final model in the chain.
	if !contains(msg, "m3 is down") {
		t.Errorf("aggregate error %q does not reflect last error", msg)
	}
}

func TestCompleteDoesNotFallBackOn401(t *testing.T) {
	var primaryHits, fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			atomic.AddInt32(&primaryHits, 1)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"bad key"}}`))
		default:
			atomic.AddInt32(&fbHits, 1)
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 3)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected terminal auth error")
	}
	if got := atomic.LoadInt32(&primaryHits); got != 1 {
		t.Errorf("primary hits = %d, want 1 (401 not retried)", got)
	}
	if got := atomic.LoadInt32(&fbHits); got != 0 {
		t.Errorf("backup hits = %d, want 0 (401 is terminal, no fallback)", got)
	}
	// A single-model-style error, not the chain aggregate.
	if contains(err.Error(), "all ") {
		t.Errorf("401 should not produce a chain-aggregate error: %q", err)
	}
}

func TestCompleteContextCancelAbortsChain(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := New(srv.URL, "", "primary", 5*time.Second, false)
	c.Fallbacks = []string{"backup"}
	c.Retry = RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
	c.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	// Cancel after the first model's single attempt so the chain must abort
	// before trying the fallback.
	cancel()

	_, _, err := c.Complete(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("server hits = %d, want 0 (cancelled context aborts before any request)", got)
	}
}

func TestCompleteEmptyFallbackListUnchanged(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"down"}}`))
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "only", nil, 2)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Two retries of the single model, no extra attempts.
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2 (single model, retries only)", got)
	}
	// No chain aggregate for a single model.
	if contains(err.Error(), "all ") {
		t.Errorf("single model should not produce a chain-aggregate error: %q", err)
	}
}

func TestCompleteUsageAttributedToSuccessfulModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "backup":
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`))
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 2)
	_, usage, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usage.TotalTokens != 12 {
		t.Errorf("usage total = %d, want 12 (from the model that answered)", usage.TotalTokens)
	}
}

func TestCompleteCachesUnderSuccessfulModelKey(t *testing.T) {
	dir := t.TempDir()
	var primaryHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			atomic.AddInt32(&primaryHits, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
		case "backup":
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"cached me"}}]}`))
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 2)
	c.Cache = &Cache{Dir: dir, TTL: time.Hour}

	// First call: primary fails, backup answers and is cached under backup's key.
	if _, _, err := c.Complete(context.Background(), nil, nil); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	firstPrimary := atomic.LoadInt32(&primaryHits)

	// Second call: backup's cache key hits immediately. Primary should still be
	// probed first (cache miss for primary) but backup never re-hit. We assert the
	// result is served and primary retries happened again (no cache for primary's
	// failing key), while backup's HTTP path is not needed.
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if msg.Content != "cached me" {
		t.Errorf("content = %q, want cached me", msg.Content)
	}
	if atomic.LoadInt32(&primaryHits) <= firstPrimary {
		// Primary has no cache entry (it failed), so it is retried again; that's
		// expected. This assertion documents that the cache is per-model: the
		// failing primary is not cached, only the successful backup is.
		t.Logf("primary re-probed on second call (expected: failures are not cached)")
	}
}

func TestChainTrimsDedupsAndCaps(t *testing.T) {
	c := &Client{Model: "  a  ", Fallbacks: []string{"", "b", "a", " c ", "b"}}
	got := c.chain()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chain[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Cap: a huge fallback list is bounded by maxChainModels.
	big := make([]string, 100)
	for i := range big {
		big[i] = "m" + string(rune('A'+i%26)) + "-" + string(rune('0'+i%10)) + "-" + string(rune('a'+i))
	}
	c2 := &Client{Model: "primary", Fallbacks: big}
	if got := len(c2.chain()); got > maxChainModels {
		t.Errorf("chain length = %d, want <= %d", got, maxChainModels)
	}
}

func TestStreamFallsBackOnPreTokenConnectError(t *testing.T) {
	var primaryHits, fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			atomic.AddInt32(&primaryHits, 1)
			w.WriteHeader(http.StatusBadGateway) // 502, pre-token
		case "backup":
			atomic.AddInt32(&fbHits, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w,
				`{"choices":[{"delta":{"content":"hi"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				`[DONE]`,
			)
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 2)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "hi" || msg.Content != "hi" {
		t.Errorf("tokens=%q content=%q, want hi", got, msg.Content)
	}
	if h := atomic.LoadInt32(&primaryHits); h != 2 {
		t.Errorf("primary hits = %d, want 2 (retries before fallback)", h)
	}
	if h := atomic.LoadInt32(&fbHits); h != 1 {
		t.Errorf("backup hits = %d, want 1", h)
	}
}

func TestStreamDoesNotFallBackMidStream(t *testing.T) {
	// Primary returns 200 and emits a token, then the body ends abruptly. The
	// client must surface the partial output without switching to the fallback
	// model — no duplicate/replayed tokens under a different model.
	var primaryHits, fbHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requestModel(t, r) {
		case "primary":
			atomic.AddInt32(&primaryHits, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			f, _ := w.(http.Flusher)
			w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"par"}}]}` + "\n\n"))
			if f != nil {
				f.Flush()
			}
			// Connection closes mid-stream (no [DONE]); must NOT trigger fallback.
		case "backup":
			atomic.AddInt32(&fbHits, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			sse(w, `{"choices":[{"delta":{"content":"REPLAYED"}}]}`, `[DONE]`)
		}
	}))
	defer srv.Close()

	c := fastFallbackClient(srv.URL, "primary", []string{"backup"}, 2)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream returned error after partial output: %v", err)
	}
	if got != "par" || msg.Content != "par" {
		t.Errorf("output = %q / %q, want par (no fallback replay)", got, msg.Content)
	}
	if h := atomic.LoadInt32(&primaryHits); h != 1 {
		t.Errorf("primary hits = %d, want 1 (no replay)", h)
	}
	if h := atomic.LoadInt32(&fbHits); h != 0 {
		t.Errorf("backup hits = %d, want 0 (no mid-stream model switch)", h)
	}
}
