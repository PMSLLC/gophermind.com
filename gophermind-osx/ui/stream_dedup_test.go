package ui

import (
	"strings"
	"testing"

	"gophermind/gophermind-osx/client"
)

// TestAssistantEchoOfStreamedTokensIsNotRepeated pins that prose which
// arrived as tokens is rendered once.
//
// agent/budget.go streams every token and then, when the same reply also
// carries tool calls, emits an "assistant" event holding the identical
// reply.Content. Rendering both put the whole answer on screen twice.
func TestAssistantEchoOfStreamedTokensIsNotRepeated(t *testing.T) {
	tr := NewTranscript()
	p := &StreamPump{Transcript: tr}

	const answer = "Here is the plan, and I will look at the tree."
	for _, tok := range strings.SplitAfter(answer, " ") {
		p.apply(client.Event{Type: "token", Data: tok})
	}
	p.apply(client.Event{Type: "assistant", Data: answer})

	var assistant []string
	for _, m := range tr.Messages() {
		if m.Role == RoleAssistant {
			assistant = append(assistant, m.Text)
		}
	}
	if len(assistant) != 1 {
		t.Fatalf("assistant messages = %d (%q), want 1", len(assistant), assistant)
	}
	if assistant[0] != answer {
		t.Errorf("text = %q, want %q", assistant[0], answer)
	}
}

// TestAssistantNarrationDistinctFromTokensStillRenders guards the other
// side: an "assistant" event that is not an echo -- the agent's own budget
// and debate notices are emitted this way -- must still appear.
func TestAssistantNarrationDistinctFromTokensStillRenders(t *testing.T) {
	tr := NewTranscript()
	p := &StreamPump{Transcript: tr}

	p.apply(client.Event{Type: "token", Data: "working on it"})
	p.apply(client.Event{Type: "assistant", Data: "tool call budget reached"})

	var texts []string
	for _, m := range tr.Messages() {
		if m.Role == RoleAssistant {
			texts = append(texts, m.Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("assistant messages = %d (%q), want 2", len(texts), texts)
	}
}

// TestAssistantEchoAfterToolBoundary pins that the comparison resets at a
// message boundary, so a later echo is still caught rather than matched
// against stale text.
func TestAssistantEchoAfterToolBoundary(t *testing.T) {
	tr := NewTranscript()
	p := &StreamPump{Transcript: tr}

	p.apply(client.Event{Type: "token", Data: "first answer"})
	p.apply(client.Event{Type: "assistant", Data: "first answer"})
	p.apply(client.Event{Type: "tool_call", Data: `{"name":"read_file","args":"{}"}`})
	p.apply(client.Event{Type: "tool_result", Data: `{"name":"read_file","text":"ok"}`})
	p.apply(client.Event{Type: "token", Data: "second answer"})
	p.apply(client.Event{Type: "assistant", Data: "second answer"})

	var texts []string
	for _, m := range tr.Messages() {
		if m.Role == RoleAssistant {
			texts = append(texts, m.Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("assistant messages = %d (%q), want 2 (one per turn)", len(texts), texts)
	}
}
