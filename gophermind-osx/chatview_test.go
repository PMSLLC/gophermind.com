package main

// This file, like app_test.go, never calls App.Run() -- see that file's
// doc comment for why -- and routes every libui-ng-touching call through
// runOnUIThread for the same reason app_test.go now does (see
// uithread_test.go and app_test.go's doc comment): AppKit requires all
// calls, process-wide, on one consistent OS thread, which go test's
// per-test goroutines don't guarantee on their own.
//
// It exercises the chat UI's widget-assembly layer (chatview.go,
// chatinput.go) far enough to prove it builds real widgets and doesn't
// panic on a representative transcript, including the OnChange ->
// queueMain -> redraw path StreamPump-driven updates rely on. Whether it
// actually LOOKS right needs a human at a real window -- see this
// package's other test files' doc comments for the same limit.
//
// Transcript mutations and StreamPump itself are plain Go (see
// gophermind-osx/ui's own doc comment) with no thread requirement of
// their own -- uiQueueMain, which Transcript.OnChange calls into from
// whatever goroutine mutated it, is specifically designed to be callable
// from any thread (that's its entire purpose: safely handing work to the
// UI thread from elsewhere) -- so only the actual widget-construction
// calls (NewApp, NewChatWindow, app.Close, buildAttributedString,
// recomputeSize) need to run on the pinned UI thread here, not every line
// of every test.
//
// Go's cgo does not support import "C" in _test.go files at all (a
// toolchain restriction, not something specific to this package), so this
// file never spells out any C.xxx type itself; where it needs to touch a
// cgo-derived value (freeing the uiAttributedString buildAttributedString
// returns), it does so through a plain-Go wrapper defined in a non-test
// file (see freeAttributedString in chatview.go) and relies on type
// inference (:=) to carry that value's type without naming it here.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"
	appui "gophermind/gophermind-osx/ui"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// makeFakeStreamFn returns a streamFn (matching RunTurn's signature)
// backed by a fake SSE server that sends one token then "done".
func makeFakeStreamFn(t *testing.T) func(context.Context, string) (*client.EventStream, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: token\ndata: hi\n\nevent: done\ndata: \n\n")
	}))
	t.Cleanup(srv.Close)
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	return c.RunStream
}

func TestNewChatWindow_BuildsWithoutPanic(t *testing.T) {
	var err error
	var transcriptNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		transcriptNil = cw.Transcript == nil
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if transcriptNil {
		t.Fatal("ChatWindow.Transcript is nil")
	}
}

// TestChatWindow_TranscriptUpdatesTriggerRedrawWithoutPanic covers the
// OnChange -> queueMain -> uiAreaQueueRedrawAll/ScrollTo path for every
// message kind, including a code block (exercises buildAttributedString's
// HighlightCode integration). libui-ng's queueMain callback only actually
// runs once uiMain is pumping, so this can't observe the redraw
// happening, only that queuing it and the model mutations underneath
// don't panic.
func TestChatWindow_TranscriptUpdatesTriggerRedrawWithoutPanic(t *testing.T) {
	var err error
	var msgCount int
	var contentH float64
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})

		cw.Transcript.AddUserMessage("hello")
		cw.Transcript.AppendToken("Hi ")   // starts one assistant message ...
		cw.Transcript.AppendToken("there") // ... and this extends the SAME one
		cw.Transcript.AddToolCall("run_shell", `{"command":"ls"}`)
		cw.Transcript.AddToolResult("run_shell", "file1\nfile2")
		cw.Transcript.AddAssistantText("Here:\n```go\nfunc f() {}\n```\ndone")
		cw.Transcript.AddSystem("a system note")
		msgCount = len(cw.Transcript.Messages())

		// Exercise buildAttributedString and recomputeSize directly too --
		// the actual rendering path goChatAreaDraw would take, callable
		// here without a live draw callback from the OS. libui-ng's own
		// leak detector fires (and crashes the process) at uiUninit() if
		// this isn't freed -- caught by this test the first time it ran
		// without the free below.
		as := cw.area.buildAttributedString()
		if as != nil {
			freeAttributedString(as)
		}
		cw.area.recomputeSize()
		contentH = cw.area.contentH
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	// user, assistant(Hi there), tool_call, tool_result, assistant(Here:...), system
	if msgCount != 6 {
		t.Errorf("len(Messages()) = %d, want 6", msgCount)
	}
	if contentH <= 0 {
		t.Error("recomputeSize left contentH <= 0 with a non-empty transcript")
	}
}

// TestChatWindow_RunTurnGoroutineDoesNotPanic exercises RunTurn's actual
// async shape (goroutine + StreamPump) against a real event stream, same
// as gophermind-osx/ui's own stream tests, but through ChatWindow's public
// entry point. RunTurn itself never calls into libui-ng directly (it
// mutates the plain-Go Transcript and starts StreamPump.Run in a
// goroutine; only the OnChange callback that fires from those mutations
// touches uiQueueMain, which is safe from any thread -- see this file's
// top-level doc comment), so only construction needs the pinned UI
// thread.
func TestChatWindow_RunTurnGoroutineDoesNotPanic(t *testing.T) {
	var err error
	var cw *ChatWindow
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		cw = NewChatWindow(app, func(string) {})
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer runOnUIThread(t, func() { cw.App.Close() })

	streamFn := makeFakeStreamFn(t)
	cw.RunTurn(testContext(t), "do something", streamFn)

	deadline := time.Now().Add(5 * time.Second)
	for {
		msgs := cw.Transcript.Messages()
		if len(msgs) >= 2 {
			if msgs[0].Role != appui.RoleUser || msgs[0].Text != "do something" {
				t.Errorf("msgs[0] = %+v, want the user message recorded first", msgs[0])
			}
			if msgs[1].Role != appui.RoleAssistant || msgs[1].Text != "hi" {
				t.Errorf("msgs[1] = %+v, want the streamed token", msgs[1])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream never completed; transcript = %+v", msgs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// makeDroppedStreamFn returns a streamFn backed by a server that sends one
// token frame, flushes it, then hijacks and abruptly closes the raw TCP
// connection mid-response (no chunked terminator, no graceful FIN handling)
// -- a real network drop mid-stream, not a server-sent "error" event and
// not a clean end-of-stream. The client's chunked-transfer reader surfaces
// this as a genuine read error (io.ErrUnexpectedEOF), distinct from the
// io.EOF a normal stream end produces.
func makeDroppedStreamFn(t *testing.T) func(context.Context, string) (*client.EventStream, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: token\ndata: hi\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close() // abrupt: no final chunk, no graceful shutdown
	}))
	t.Cleanup(srv.Close)
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	return c.RunStream
}

// TestChatWindow_RunTurn_StreamErrorIsReportedToTranscript covers 05-05's
// "Network drop mid-stream: client detects and reports error." Before this
// test, RunTurn's goroutine discarded StreamPump.Run's return value
// entirely (`go pump.Run(ctx, stream)`): a genuine network drop mid-stream
// left the transcript silently incomplete, with no indication to the user
// that anything went wrong, unlike a server-sent "error" event (which
// StreamPump.apply already turns into a system message) or the initial
// streamFn failure path just above it (which RunTurn already reports).
func TestChatWindow_RunTurn_StreamErrorIsReportedToTranscript(t *testing.T) {
	var err error
	var cw *ChatWindow
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		cw = NewChatWindow(app, func(string) {})
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer runOnUIThread(t, func() { cw.App.Close() })

	streamFn := makeDroppedStreamFn(t)
	cw.RunTurn(testContext(t), "do something", streamFn)

	deadline := time.Now().Add(5 * time.Second)
	for {
		msgs := cw.Transcript.Messages()
		for _, m := range msgs {
			if m.Role == appui.RoleSystem && strings.Contains(m.Text, "error") {
				return // found the reported error -- test passes
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no system error message reported after a mid-stream drop; transcript = %+v", msgs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
