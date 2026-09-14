package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteDecodesToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request carries our tool definition.
		var got ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(got.Tools) != 1 || got.Tools[0].Function.Name != "read_file" {
			t.Errorf("tools not forwarded: %+v", got.Tools)
		}
		if got.ToolChoice != "auto" {
			t.Errorf("tool_choice = %q, want auto", got.ToolChoice)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	tools := []Tool{{Type: "function", Function: Function{Name: "read_file"}}}
	msg, _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"main.go"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
}

func TestCompleteFinalMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"all done"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	msg, _, err := c.Complete(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("expected no tool calls")
	}
	if msg.Content != "all done" {
		t.Errorf("content = %q", msg.Content)
	}
}

func TestCompleteSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	_, _, err := c.Complete(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "bad model"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err, want)
	}
}

// TestComplete_TotalTimeoutBound proves that removing http.Client.Timeout
// (Part 1) does not un-bound the non-streaming path: completeOnce must wrap
// each attempt's context with the client's totalTimeout (Part 2), so a
// response body that never finishes still errors out promptly instead of
// hanging forever.
func TestComplete_TotalTimeoutBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold the body open well past the client's totalTimeout, then send it
		// (or just let the handler return; either way the ctx deadline must
		// have already cut the read short).
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"late"}}]}`))
	}))
	defer srv.Close()

	c, err := NewWithTLS(srv.URL, "", "m", 50*time.Millisecond, TLSOptions{})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	c.Retry = RetryPolicy{MaxAttempts: 1} // single attempt: isolate the per-attempt bound

	start := time.Now()
	_, _, err = c.Complete(context.Background(), nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Complete to time out on a body that exceeds totalTimeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Complete took %v, want bounded near totalTimeout (50ms), not the full 300ms body delay", elapsed)
	}
}

func TestAuthHeaderOnlyWhenKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	New(srv.URL, "", "m", time.Second, false).Complete(context.Background(), nil, nil)
	if gotAuth != "" {
		t.Errorf("auth header set with empty key: %q", gotAuth)
	}
	New(srv.URL, "secret", "m", time.Second, false).Complete(context.Background(), nil, nil)
	if gotAuth != "Bearer secret" {
		t.Errorf("auth header = %q, want Bearer secret", gotAuth)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
