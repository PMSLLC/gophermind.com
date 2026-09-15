// Package ui implements gophermind-osx's chat UI (.planning/tasks/04-01.json):
// a message transcript, real-time SSE token streaming, tool call/result
// rendering, syntax-highlighted code blocks, and a multi-line input field.
//
// The package is split so the actual rendering/UI-facing logic (transcript
// state, syntax highlighting, the SSE-to-UI async pump, and the Cmd+Enter
// send/newline decision) is plain Go, independent of libui-ng and fully
// unit-tested; only the widget-assembly layer (chatview.go) touches cgo,
// and -- like gophermind-osx/app.go before it -- can only be smoke-tested
// (built and driven without panicking), never visually verified here.
package ui

import (
	"bytes"
	"encoding/json"
	"sync"
)

// Role identifies what kind of transcript entry a Message is.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleSystem     Role = "system"
	RoleToolCall   Role = "tool_call"
	RoleToolResult Role = "tool_result"
)

// Message is one entry in the transcript.
type Message struct {
	Role Role
	Text string
	// ToolName/ToolArgs are set only for RoleToolCall (name + pretty-printed
	// JSON args) and RoleToolResult (name; Text carries the result).
	ToolName string
	ToolArgs string
}

// Transcript is the chat transcript's state: an ordered list of Messages,
// safe for concurrent use (the SSE-reading goroutine in stream.go mutates
// it from a different goroutine than the UI thread that reads it).
type Transcript struct {
	mu       sync.Mutex
	messages []Message
	onChange func()
}

// NewTranscript returns an empty Transcript.
func NewTranscript() *Transcript {
	return &Transcript{}
}

// OnChange registers f to be called after every mutation (AddX, AppendToken).
// f is called synchronously, under the same call that mutated the
// transcript -- it must not block and must not itself call back into
// Transcript (re-entrant locking would deadlock). The real widget layer
// uses this to know when to re-render; wraps its own work in a
// non-blocking dispatch (uiQueueMain) rather than doing UI work here
// directly, keeping this package free of any GUI dependency.
func (t *Transcript) OnChange(f func()) {
	t.mu.Lock()
	t.onChange = f
	t.mu.Unlock()
}

func (t *Transcript) notify() {
	if t.onChange != nil {
		t.onChange()
	}
}

// AddUserMessage appends a user message.
func (t *Transcript) AddUserMessage(text string) {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleUser, Text: text})
	t.mu.Unlock()
	t.notify()
}

// AddSystem appends a system/status message (e.g. an error or connection
// notice), distinct from RoleAssistant so the UI can style it differently.
func (t *Transcript) AddSystem(text string) {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleSystem, Text: text})
	t.mu.Unlock()
	t.notify()
}

// BeginAssistantMessage starts a new, initially-empty assistant message
// that subsequent AppendToken calls extend. Call once per turn before the
// first token arrives (or before the first AppendToken call, which starts
// one automatically if none is open -- see AppendToken).
func (t *Transcript) BeginAssistantMessage() {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleAssistant})
	t.mu.Unlock()
	t.notify()
}

// AppendToken appends text to the transcript's current streaming assistant
// message, starting one first if the last message isn't already an
// assistant message in progress (covers a caller that skips
// BeginAssistantMessage and just starts sending tokens).
func (t *Transcript) AppendToken(text string) {
	t.mu.Lock()
	if len(t.messages) == 0 || t.messages[len(t.messages)-1].Role != RoleAssistant {
		t.messages = append(t.messages, Message{Role: RoleAssistant})
	}
	last := &t.messages[len(t.messages)-1]
	last.Text += text
	t.mu.Unlock()
	t.notify()
}

// AddAssistantText appends a complete (non-streamed) assistant message --
// used for the SSE "assistant" event, which carries intermediate narration
// prose alongside a tool call as one complete chunk, distinct from "token"
// deltas that AppendToken accumulates into an in-progress message (see
// agent.Agent.Send's own distinction between the two, which this mirrors).
func (t *Transcript) AddAssistantText(text string) {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleAssistant, Text: text})
	t.mu.Unlock()
	t.notify()
}

// AddToolCall appends a tool-call entry, pretty-printing argsJSON if it
// parses as JSON (falling back to the raw string otherwise -- malformed
// JSON in a tool call's arguments shouldn't hide the call from the
// transcript, just display it unformatted).
func (t *Transcript) AddToolCall(name, argsJSON string) {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleToolCall, ToolName: name, ToolArgs: prettyJSON(argsJSON)})
	t.mu.Unlock()
	t.notify()
}

// AddToolResult appends a tool-result entry.
func (t *Transcript) AddToolResult(name, text string) {
	t.mu.Lock()
	t.messages = append(t.messages, Message{Role: RoleToolResult, ToolName: name, Text: text})
	t.mu.Unlock()
	t.notify()
}

// Messages returns a snapshot copy of the current transcript.
func (t *Transcript) Messages() []Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Message, len(t.messages))
	copy(out, t.messages)
	return out
}

// prettyJSON re-indents s if it's valid JSON, or returns s unchanged
// otherwise.
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}
