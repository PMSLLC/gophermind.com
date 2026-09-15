package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
)

// sseBody writes raw SSE frames matching gophermind-lib/serve's
// writeSSEEvent output, for a fake backend this package's tests dial via
// the real client.Client/EventStream (the same integration-style approach
// gophermind-osx/client's own tests use, rather than mocking EventStream,
// which has no exported way to construct directly).
func sseBody(frames ...[2]string) string {
	var b strings.Builder
	for _, f := range frames {
		event, data := f[0], f[1]
		b.WriteString("event: " + event + "\n")
		for _, line := range strings.Split(data, "\n") {
			b.WriteString("data: " + line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func openTestStream(t *testing.T, body string) *client.EventStream {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	return stream
}

// TestStreamPump_TokensAccumulateIntoOneMessage covers real-time token
// streaming end to end: a fake SSE backend, the real client.EventStream
// parser, and StreamPump all wired together.
func TestStreamPump_TokensAccumulateIntoOneMessage(t *testing.T) {
	body := sseBody(
		[2]string{"token", "Hel"},
		[2]string{"token", "lo"},
		[2]string{"usage", `{"PromptTokens":1}`},
		[2]string{"done", ""},
	)
	stream := openTestStream(t, body)
	tr := NewTranscript()
	pump := &StreamPump{Transcript: tr}

	if err := pump.Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := tr.Messages()
	if len(msgs) != 1 || msgs[0].Text != "Hello" {
		t.Errorf("msgs = %+v, want one assistant message \"Hello\"", msgs)
	}
}

// TestStreamPump_ToolCallAndResult covers tool call/result rendering
// driven through a real stream.
func TestStreamPump_ToolCallAndResult(t *testing.T) {
	body := sseBody(
		[2]string{"assistant", "checking..."},
		[2]string{"tool_call", `{"name":"run_shell","args":"{\"command\":\"echo hi\"}"}`},
		[2]string{"tool_result", `{"name":"run_shell","text":"hi\n"}`},
		[2]string{"token", "done"},
		[2]string{"done", ""},
	)
	stream := openTestStream(t, body)
	tr := NewTranscript()
	pump := &StreamPump{Transcript: tr}

	if err := pump.Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := tr.Messages()
	if len(msgs) != 4 {
		t.Fatalf("len(msgs) = %d, want 4: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != RoleAssistant || msgs[0].Text != "checking..." {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != RoleToolCall || msgs[1].ToolName != "run_shell" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	if msgs[2].Role != RoleToolResult || msgs[2].Text != "hi\n" {
		t.Errorf("msgs[2] = %+v", msgs[2])
	}
	if msgs[3].Role != RoleAssistant || msgs[3].Text != "done" {
		t.Errorf("msgs[3] = %+v", msgs[3])
	}
}

// TestStreamPump_ApprovalNeededCallsBack covers surfacing "approval-needed"
// without the pump trying to resolve it itself.
func TestStreamPump_ApprovalNeededCallsBack(t *testing.T) {
	body := sseBody(
		[2]string{"approval-needed", `{"approval_id":"a1","tool":"run_shell","args":"{}"}`},
		[2]string{"done", ""},
	)
	stream := openTestStream(t, body)
	tr := NewTranscript()

	var got client.Event
	var called bool
	pump := &StreamPump{Transcript: tr, OnApprovalNeeded: func(ev client.Event) {
		called = true
		got = ev
	}}
	if err := pump.Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("OnApprovalNeeded was never called")
	}
	an, err := got.ApprovalNeeded()
	if err != nil || an.ApprovalID != "a1" {
		t.Errorf("ApprovalNeeded() = %+v, err %v", an, err)
	}
	// approval-needed must not itself appear as a transcript message --
	// the caller (via OnApprovalNeeded) owns how it's surfaced to the user.
	if len(tr.Messages()) != 0 {
		t.Errorf("transcript = %+v, want no messages from approval-needed alone", tr.Messages())
	}
}

func TestStreamPump_ErrorEventAddedAsSystemMessage(t *testing.T) {
	body := sseBody([2]string{"error", "something broke"})
	stream := openTestStream(t, body)
	tr := NewTranscript()
	pump := &StreamPump{Transcript: tr}
	pump.Run(context.Background(), stream)

	msgs := tr.Messages()
	if len(msgs) != 1 || msgs[0].Role != RoleSystem || !strings.Contains(msgs[0].Text, "something broke") {
		t.Errorf("msgs = %+v", msgs)
	}
}

// TestStreamPump_ContextCancellationStopsPromptly covers "No UI freezes":
// Run must return promptly when ctx is cancelled, even against a stream
// that never ends on its own (a long-lived connection, like
// PipelineEvents in practice).
func TestStreamPump_ContextCancellationStopsPromptly(t *testing.T) {
	// A handler that sends one frame then blocks (simulating a long-lived
	// stream with no more events yet, not a closed connection).
	blockCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sseBody([2]string{"token", "x"}))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-blockCh // hold the connection open until the test closes it
	}))
	defer srv.Close()
	defer close(blockCh)

	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.RunStream(context.Background(), "go")
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr := NewTranscript()
	pump := &StreamPump{Transcript: tr}

	done := make(chan error, 1)
	go func() { done <- pump.Run(ctx, stream) }()

	// Let the first token land, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Run returned nil after cancellation, want ctx.Err()")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return promptly after ctx was cancelled")
	}
}
