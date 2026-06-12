package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func sse(w http.ResponseWriter, lines ...string) {
	f, _ := w.(http.Flusher)
	for _, l := range lines {
		w.Write([]byte("data: " + l + "\n\n"))
		if f != nil {
			f.Flush()
		}
	}
}

func TestStreamProseAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		)
	}))
	defer srv.Close()

	c := New(srv.URL, "", "m", 5*time.Second, false)
	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "Hello" {
		t.Errorf("tokens = %q, want Hello", got)
	}
	if msg.Content != "Hello" {
		t.Errorf("content = %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" || tc.Function.Arguments != `{"path":"x"}` {
		t.Errorf("reassembled tool call wrong: %+v", tc)
	}
}

// TestStreamCancelMidStreamReturnsPromptly emits a couple of SSE chunks and then
// stalls forever; cancelling the context mid-stream must unblock Stream quickly
// with context.Canceled (not block until the next chunk/EOF), must not panic,
// and must fire no token callbacks after the cancel.
func TestStreamCancelMidStreamReturnsPromptly(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"delta":{"content":"par"}}]}`,
			`{"choices":[{"delta":{"content":"tial"}}]}`,
		)
		// Stall: hold the connection open without sending more frames until the
		// test releases us (a bug that hangs the read can't wedge CI — the test's
		// own deadline below fails fast, and this guard unblocks on cleanup).
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer srv.Close()
	defer close(released)

	c := New(srv.URL, "", "m", 5*time.Second, false)

	ctx, cancel := context.WithCancel(context.Background())

	var tokens int64
	var canceled int32
	gotFirstTokens := make(chan struct{})
	var once int32
	onToken := func(string) {
		// After the cancel fires, no further callbacks may run.
		if atomic.LoadInt32(&canceled) == 1 {
			t.Errorf("token callback fired after cancel")
		}
		if atomic.AddInt64(&tokens, 1) == 2 && atomic.CompareAndSwapInt32(&once, 0, 1) {
			close(gotFirstTokens)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := c.Stream(ctx, nil, nil, onToken)
		done <- err
	}()

	// Wait until the two prose deltas have been delivered, then cancel.
	select {
	case <-gotFirstTokens:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("never received initial tokens")
	}
	atomic.StoreInt32(&canceled, 1)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return promptly after cancel (hung)")
	}
}
