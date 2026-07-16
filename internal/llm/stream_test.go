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
		if errors.Is(err, ErrStreamIdle) {
			t.Errorf("err = %v, must NOT be ErrStreamIdle (this was a parent cancel, not a stall)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return promptly after cancel (hung)")
	}
}

// TestStream_SlowFramesUnderIdle_CompletesDespiteOldTotalTimeout is the core
// regression test: a stream whose TOTAL duration exceeds what used to be
// http.Client.Timeout, but whose inter-frame gaps all stay comfortably under
// the idle timeout, must complete successfully. Before the fix, the shared
// http.Client's total Timeout would have killed this mid-stream even though
// tokens were arriving steadily.
func TestStream_SlowFramesUnderIdle_CompletesDespiteOldTotalTimeout(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"content":"a"}}]}`,
		`{"choices":[{"delta":{"content":"b"}}]}`,
		`{"choices":[{"delta":{"content":"c"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		for _, l := range frames {
			w.Write([]byte("data: " + l + "\n\n"))
			if f != nil {
				f.Flush()
			}
			time.Sleep(60 * time.Millisecond) // gap << idle timeout
		}
	}))
	defer srv.Close()

	// "Old" total timeout (300ms) is far shorter than the stream's total
	// duration (~5*60ms = 300ms+ across 5 frames, i.e. it would have tripped a
	// Client.Timeout of 300ms). Timeout: 0 on the shared client (Part 1) means
	// this totalTimeout only bounds Complete, never Stream.
	c, err := NewWithTLS(srv.URL, "", "m", 300*time.Millisecond, TLSOptions{})
	if err != nil {
		t.Fatalf("NewWithTLS: %v", err)
	}
	c.SetStreamIdleTimeout(500 * time.Millisecond) // >> the 60ms inter-frame gap

	var got string
	msg, _, err := c.Stream(context.Background(), nil, nil, func(tok string) { got += tok })
	if err != nil {
		t.Fatalf("Stream: %v (should complete despite exceeding the old 300ms total timeout)", err)
	}
	if got != "abc" {
		t.Errorf("tokens = %q, want abc", got)
	}
	if msg.Content != "abc" {
		t.Errorf("content = %q, want abc", msg.Content)
	}
}

// TestStream_IdleStall_ReturnsErrStreamIdle: a server that sends one frame and
// then stalls (keeping the connection open, no more data) must cause Stream to
// return an error that IS ErrStreamIdle, within roughly the idle duration —
// not hang, and not surface as a bare context error.
func TestStream_IdleStall_ReturnsErrStreamIdle(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"delta":{"content":"hi"}}]}`)
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer srv.Close()
	defer close(released)

	c := New(srv.URL, "", "m", 5*time.Second, false)
	c.SetStreamIdleTimeout(100 * time.Millisecond)

	start := time.Now()
	_, _, err := c.Stream(context.Background(), nil, nil, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("err = %v, want ErrStreamIdle (via errors.Is)", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, must NOT also read as context.Canceled (parent was never cancelled)", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v, want close to the 100ms idle timeout", elapsed)
	}
}

// TestStream_DefaultIdleTimeout_UsedWhenUnset ensures a Client built without an
// explicit SetStreamIdleTimeout call still gets a sane (300s) default rather
// than firing immediately (zero value) or never firing (unbounded).
func TestStream_DefaultIdleTimeout_UsedWhenUnset(t *testing.T) {
	c := New("http://example.invalid", "", "m", 5*time.Second, false)
	if got := c.streamIdleTimeout(); got != 300*time.Second {
		t.Errorf("default streamIdleTimeout = %v, want 300s", got)
	}
}
