package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// requestPathFor spins up a server that records the path it was asked for.
func requestPathFor(t *testing.T, chatPath string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.ChatPath = chatPath
	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return got
}

// An empty ChatPath must preserve today's behavior exactly.
func TestChatPathDefaultsToV1(t *testing.T) {
	if got := requestPathFor(t, ""); got != "/v1/chat/completions" {
		t.Errorf("default path = %q, want /v1/chat/completions", got)
	}
}

// A base URL that already carries /v1 sets ChatPath so the path is not doubled.
func TestChatPathOverrideAvoidsDoubledV1(t *testing.T) {
	if got := requestPathFor(t, "/chat/completions"); got != "/chat/completions" {
		t.Errorf("override path = %q, want /chat/completions", got)
	}
}

// streamRequestPathFor spins up an SSE server that records the path it was
// asked for and returns it after a minimal Stream call completes.
func streamRequestPathFor(t *testing.T, chatPath string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"ok"}}]}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.ChatPath = chatPath
	if _, _, err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, func(string) {}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return got
}

// An empty ChatPath must preserve today's streaming behavior exactly.
func TestChatPathStreamDefaultsToV1(t *testing.T) {
	if got := streamRequestPathFor(t, ""); got != "/v1/chat/completions" {
		t.Errorf("default stream path = %q, want /v1/chat/completions", got)
	}
}

// A base URL that already carries /v1 sets ChatPath so the streaming path is
// not doubled either.
func TestChatPathStreamOverrideAvoidsDoubledV1(t *testing.T) {
	if got := streamRequestPathFor(t, "/chat/completions"); got != "/chat/completions" {
		t.Errorf("override stream path = %q, want /chat/completions", got)
	}
}
