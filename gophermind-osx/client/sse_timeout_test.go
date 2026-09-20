package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestStreamOutlivesRequestTimeout pins that an SSE stream is not killed by
// Config.Timeout. http.Client.Timeout covers the whole exchange including
// reading the body, so a stream opened with the same client dies mid-flight
// once the deadline passes -- surfacing as "context deadline exceeded
// (Client.Timeout or context cancellation while reading body)" partway
// through a model turn, which is far shorter than any real one.
//
// The timeout here is deliberately tiny and the second event deliberately
// late, so the test fails in about the time a working stream takes to
// deliver two events, rather than waiting out a realistic deadline.
func TestStreamOutlivesRequestTimeout(t *testing.T) {
	const timeout = 150 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server response is not flushable")
			return
		}
		fmt.Fprint(w, "event: token\ndata: first\n\n")
		flusher.Flush()
		// Cross the deadline before the second event.
		time.Sleep(3 * timeout)
		fmt.Fprint(w, "event: done\ndata: second\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Timeout: timeout})
	stream, err := c.Stream(context.Background(), "sess-1", "task")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if first.Type != "token" {
		t.Errorf("first event = %q, want token", first.Type)
	}

	// The event that lands after Config.Timeout has elapsed is the one that
	// used to fail.
	second, err := stream.Next()
	if err != nil {
		t.Fatalf("second Next after %v: %v", timeout, err)
	}
	if second.Type != "done" {
		t.Errorf("second event = %q, want done", second.Type)
	}
}

// TestNonStreamingStillHonoursTimeout guards the other side of the split:
// ordinary request/response calls must keep their deadline, so a wedged
// server cannot hang the UI forever.
func TestNonStreamingStillHonoursTimeout(t *testing.T) {
	const timeout = 100 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(4 * timeout)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Token: "t", Timeout: timeout, Retry: RetryPolicy{MaxAttempts: 1}})
	if _, err := c.ListSessions(context.Background()); err == nil {
		t.Fatal("ListSessions against a wedged server returned nil error; the timeout is gone")
	}
}
