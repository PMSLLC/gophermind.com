package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// jsonChatResponse is a minimal valid non-streaming completion body.
const jsonChatResponse = `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`

// newTestClient builds a Client pointed at srv with retries disabled (one
// attempt) so a hook-abort error surfaces immediately and deterministically.
func newTestClient(t *testing.T, srv *httptest.Server, apiKey string) *Client {
	t.Helper()
	c, err := NewWithTLS(srv.URL, apiKey, "test-model", 5*time.Second, TLSOptions{})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	c.Retry = RetryPolicy{MaxAttempts: 1, BaseDelay: 0, MaxDelay: 0}
	return c
}

// TestHeaderInjectionMiddleware asserts the injected header reaches the server.
func TestHeaderInjectionMiddleware(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Tenant-ID")
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "")
	c.Use(HeaderInjector("X-Tenant-ID", "acme-42"))

	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "acme-42" {
		t.Fatalf("server saw X-Tenant-ID=%q, want %q", got, "acme-42")
	}
}

// TestLoggingMiddlewareRedactsAuthorization asserts the log line records
// method/URL/status but NEVER contains the bearer token value.
func TestLoggingMiddlewareRedactsAuthorization(t *testing.T) {
	const secret = "sk-supersecret-bearer-token-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c := newTestClient(t, srv, secret)
	c.Use(LoggingMiddleware(&buf))

	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("log output leaked the bearer token: %q", out)
	}
	for _, want := range []string{"POST", "/v1/chat/completions", "200"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q; got %q", want, out)
		}
	}
}

// recordHook is a request hook that appends a label to a shared slice, used to
// observe execution order.
func recordHook(mu *sync.Mutex, order *[]string, label string) Middleware {
	return HookMiddleware(func(*http.Request) error {
		mu.Lock()
		*order = append(*order, label)
		mu.Unlock()
		return nil
	}, nil)
}

// TestMiddlewareOrder asserts middlewares run in declared order (index 0 first).
func TestMiddlewareOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	var mu sync.Mutex
	var order []string
	c := newTestClient(t, srv, "")
	c.Use(
		recordHook(&mu, &order, "first"),
		recordHook(&mu, &order, "second"),
		recordHook(&mu, &order, "third"),
	)

	if _, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := []string{"first", "second", "third"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
}

// TestRequestHookErrorAbortsRequest asserts a request-hook error fails the
// request WITHOUT hitting the server.
func TestRequestHookErrorAbortsRequest(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	sentinel := errors.New("denied by policy")
	c := newTestClient(t, srv, "")
	c.Use(HookMiddleware(func(*http.Request) error { return sentinel }, nil))

	_, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error from aborting request hook, got nil")
	}
	if hit {
		t.Fatal("server was hit despite request hook abort")
	}
	if !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("error did not surface hook cause: %v", err)
	}
}

// TestNoMiddlewareUnchanged asserts a client with no middleware works and uses
// the base transport directly (no wrapping).
func TestNoMiddlewareUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "")
	msg, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "ok" {
		t.Fatalf("content = %q, want ok", msg.Content)
	}
	// With no middleware registered the transport must be exactly the captured
	// base (which is nil here -> default transport path), not a wrapped chain.
	if c.HTTP.Transport != c.baseTransport {
		t.Fatalf("transport was wrapped despite no middleware registered")
	}
}

// TestStreamingThroughMiddlewareStillStreams asserts SSE deltas arrive
// incrementally through the middleware chain (the response body is not buffered)
// AND that a request hook which reads+restores the body does not send an empty
// body.
func TestStreamingThroughMiddlewareStillStreams(t *testing.T) {
	var gotBodyLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBodyLen = len(b)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("server response writer is not a Flusher")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit three deltas with a flush between each so a buffering wrapper would
		// be observable as all-at-once delivery.
		for _, tok := range []string{"a", "b", "c"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tok)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "")

	// A request hook that READS the body then RESTORES it via GetBody, to prove
	// the actual request still carries a non-empty body after a hook reads it.
	c.Use(HookMiddleware(func(req *http.Request) error {
		if req.Body == nil {
			return nil
		}
		_, _ = io.ReadAll(req.Body) // consume
		if req.GetBody != nil {
			nb, err := req.GetBody()
			if err != nil {
				return err
			}
			req.Body = nb // restore so the server sees the real body
		}
		return nil
	}, nil))
	// Also add a logging middleware to confirm streaming survives multiple wraps.
	var logBuf bytes.Buffer
	c.Use(LoggingMiddleware(&logBuf))

	var tokens []string
	var times []time.Time
	msg, _, err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, func(tok string) {
		tokens = append(tokens, tok)
		times = append(times, time.Now())
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotBodyLen == 0 {
		t.Fatal("server received an EMPTY request body: hook destroyed it")
	}
	if got := strings.Join(tokens, ""); got != "abc" {
		t.Fatalf("assembled tokens = %q, want abc", got)
	}
	if msg.Content != "abc" {
		t.Fatalf("final content = %q, want abc", msg.Content)
	}
	if len(times) < 3 {
		t.Fatalf("expected 3 incremental tokens, got %d", len(times))
	}
	// Incremental delivery: the gap between first and last token should reflect
	// the server's inter-flush sleeps. If a wrapper buffered the whole body, all
	// tokens would arrive together (near-zero spread).
	spread := times[len(times)-1].Sub(times[0])
	if spread < 5*time.Millisecond {
		t.Fatalf("tokens arrived nearly simultaneously (spread=%s): stream was buffered", spread)
	}
}

// TestResponseHookErrorFailsRequest asserts a response-hook error surfaces as an
// error and the response body is closed.
func TestResponseHookErrorFailsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "")
	c.Use(HookMiddleware(nil, func(*http.Response) error { return errors.New("bad response") }))

	_, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "bad response") {
		t.Fatalf("expected response-hook error to surface, got %v", err)
	}
}

// TestPanicInHookBecomesError asserts a panicking hook is recovered and turned
// into a transport error rather than crashing.
func TestPanicInHookBecomesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, jsonChatResponse)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "")
	c.Use(HookMiddleware(func(*http.Request) error { panic("boom") }, nil))

	_, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "panic in request hook") {
		t.Fatalf("expected recovered panic error, got %v", err)
	}
}

// TestRedactHeaders covers the sensitive-header matching directly.
func TestRedactHeaders(t *testing.T) {
	h := http.Header{
		"Authorization":   {"Bearer secret"},
		"X-Api-Key":       {"key-123"},
		"X-Session-Token": {"tok-456"},
		"Cookie":          {"sid=abc"},
		"Content-Type":    {"application/json"},
	}
	red := redactHeaders(h)
	for _, name := range []string{"Authorization", "X-Api-Key", "X-Session-Token", "Cookie"} {
		if got := red.Get(name); got != "<redacted>" {
			t.Errorf("%s = %q, want <redacted>", name, got)
		}
	}
	if got := red.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type was altered: %q", got)
	}
}
