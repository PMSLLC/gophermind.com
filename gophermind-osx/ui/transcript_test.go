package ui

import (
	"sync/atomic"
	"testing"
)

// TestTranscript_UserAndAssistantMessages covers "Chat transcript displays
// user and assistant messages".
func TestTranscript_UserAndAssistantMessages(t *testing.T) {
	tr := NewTranscript()
	tr.AddUserMessage("hello")
	tr.AddAssistantText("hi there")

	msgs := tr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("len(Messages()) = %d, want 2", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[0].Text != "hello" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != RoleAssistant || msgs[1].Text != "hi there" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
}

// TestTranscript_AppendTokenStreams covers "SSE streaming: tokens appear
// in real-time as they arrive" at the model level: each AppendToken call
// extends the same message rather than creating a new one.
func TestTranscript_AppendTokenStreams(t *testing.T) {
	tr := NewTranscript()
	tr.AppendToken("Hel")
	tr.AppendToken("lo")
	tr.AppendToken(", world")

	msgs := tr.Messages()
	if len(msgs) != 1 {
		t.Fatalf("len(Messages()) = %d, want 1 (tokens should accumulate into one message)", len(msgs))
	}
	if msgs[0].Role != RoleAssistant || msgs[0].Text != "Hello, world" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
}

// TestTranscript_AppendTokenAfterUserStartsNewMessage covers the boundary:
// a token arriving after a user message must start a fresh assistant
// message, not append to something else.
func TestTranscript_AppendTokenAfterUserStartsNewMessage(t *testing.T) {
	tr := NewTranscript()
	tr.AddUserMessage("question")
	tr.AppendToken("answer")

	msgs := tr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("len(Messages()) = %d, want 2", len(msgs))
	}
	if msgs[1].Role != RoleAssistant || msgs[1].Text != "answer" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
}

// TestTranscript_BeginAssistantMessageThenAppend covers explicit
// BeginAssistantMessage usage (an empty message a caller wants visible --
// e.g. a "thinking..." placeholder -- before any tokens arrive).
func TestTranscript_BeginAssistantMessageThenAppend(t *testing.T) {
	tr := NewTranscript()
	tr.BeginAssistantMessage()
	if got := tr.Messages(); len(got) != 1 || got[0].Text != "" {
		t.Fatalf("after BeginAssistantMessage: %+v", got)
	}
	tr.AppendToken("now streaming")
	msgs := tr.Messages()
	if len(msgs) != 1 || msgs[0].Text != "now streaming" {
		t.Errorf("msgs = %+v", msgs)
	}
}

// TestTranscript_ToolCallRendering covers "Tool calls rendered with name
// and pretty-printed args".
func TestTranscript_ToolCallRendering(t *testing.T) {
	tr := NewTranscript()
	tr.AddToolCall("run_shell", `{"command":"echo hi"}`)

	msgs := tr.Messages()
	if len(msgs) != 1 {
		t.Fatalf("len(Messages()) = %d, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Role != RoleToolCall || m.ToolName != "run_shell" {
		t.Errorf("msgs[0] = %+v", m)
	}
	want := "{\n  \"command\": \"echo hi\"\n}"
	if m.ToolArgs != want {
		t.Errorf("ToolArgs = %q, want %q (pretty-printed)", m.ToolArgs, want)
	}
}

// TestTranscript_ToolCallMalformedArgsFallsBackToRaw covers the fallback
// path: malformed JSON args must still be shown, not hidden or errored.
func TestTranscript_ToolCallMalformedArgsFallsBackToRaw(t *testing.T) {
	tr := NewTranscript()
	tr.AddToolCall("x", `not json`)
	got := tr.Messages()[0].ToolArgs
	if got != "not json" {
		t.Errorf("ToolArgs = %q, want the raw string unchanged", got)
	}
}

func TestTranscript_ToolResult(t *testing.T) {
	tr := NewTranscript()
	tr.AddToolResult("run_shell", "hi\n")
	m := tr.Messages()[0]
	if m.Role != RoleToolResult || m.ToolName != "run_shell" || m.Text != "hi\n" {
		t.Errorf("msgs[0] = %+v", m)
	}
}

// TestTranscript_OnChangeFiresOnEveryMutation covers the notification
// contract chatview.go's re-rendering depends on.
func TestTranscript_OnChangeFiresOnEveryMutation(t *testing.T) {
	tr := NewTranscript()
	var calls atomic.Int32
	tr.OnChange(func() { calls.Add(1) })

	tr.AddUserMessage("a")
	tr.AppendToken("b")
	tr.AddToolCall("t", "{}")
	tr.AddToolResult("t", "r")
	tr.AddSystem("s")
	tr.AddAssistantText("x")

	if got := calls.Load(); got != 6 {
		t.Errorf("OnChange called %d times, want 6", got)
	}
}

func TestTranscript_MessagesReturnsIndependentCopy(t *testing.T) {
	tr := NewTranscript()
	tr.AddUserMessage("a")
	msgs := tr.Messages()
	msgs[0].Text = "mutated"
	if tr.Messages()[0].Text != "a" {
		t.Error("mutating the returned slice affected the transcript's internal state")
	}
}
