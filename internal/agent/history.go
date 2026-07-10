package agent

import (
	"encoding/json"
	"fmt"
	"io"

	"gophermind/internal/llm"
)

// LoadHistory replaces the conversation with the JSONL message history read from
// r — the exact format ExportJSONL writes — so a persisted session can be
// resumed. The loaded slice becomes the full history including its own system
// message. An empty stream is an error (a session always has at least a system
// turn).
func (a *Agent) LoadHistory(r io.Reader) error {
	dec := json.NewDecoder(r)
	var msgs []llm.Message
	for {
		var m llm.Message
		err := dec.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode session message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 {
		return fmt.Errorf("session history is empty")
	}
	a.msgs = msgs
	return nil
}

// AppendSystemPrompt appends text to the seeded system prompt. It must be called
// before the first Send, as it edits the system message in place. An empty text
// is a no-op.
func (a *Agent) AppendSystemPrompt(text string) {
	if text == "" || len(a.msgs) == 0 || a.msgs[0].Role != "system" {
		return
	}
	a.msgs[0].Content += "\n\n" + text
}

// SetSystemPrompt replaces the entire system prompt. It must be called before
// the first Send. An empty text resets to the default system prompt.
func (a *Agent) SetSystemPrompt(text string) {
	if text == "" {
		text = systemPrompt
	}
	if len(a.msgs) > 0 && a.msgs[0].Role == "system" {
		a.msgs[0].Content = text
	}
}

// SystemPrompt returns the current system prompt.
func (a *Agent) SystemPrompt() string {
	if len(a.msgs) > 0 && a.msgs[0].Role == "system" {
		return a.msgs[0].Content
	}
	return systemPrompt
}
