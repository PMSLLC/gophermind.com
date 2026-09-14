package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// The behaviour the user asked for: a 429 moves to the next model rather than
// failing the turn. This drives a real server that refuses the first model and
// answers the second.
func TestRateLimitedModelFallsBackToTheNext(t *testing.T) {
	var mu sync.Mutex
	var tried []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		tried = append(tried, body.Model)
		mu.Unlock()

		if body.Model == "busy-model" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answered by ` + body.Model + `"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "busy-model", 30*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: 1} // no retries, so the test measures fallback not backoff
	c.Fallbacks = []string{"spare-model"}

	msg, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("a 429 on the first model failed the whole request: %v", err)
	}
	if msg.Content != "answered by spare-model" {
		t.Errorf("content = %q, want the fallback's answer", msg.Content)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(tried) != 2 || tried[0] != "busy-model" || tried[1] != "spare-model" {
		t.Errorf("models tried = %v, want [busy-model spare-model]", tried)
	}
}

// With no fallbacks configured, which is what the desktop did before, the same
// 429 simply fails. This pins what was actually broken.
func TestRateLimitedWithNoFallbacksFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "busy-model", 30*time.Second, false)
	c.Retry = RetryPolicy{MaxAttempts: 1}

	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("a 429 with no fallbacks returned no error")
	}
}
