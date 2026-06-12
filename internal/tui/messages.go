// Package tui is the Bubble Tea interactive session. It is a thin rendering and
// key-routing layer over the headless agent engine, which runs in a goroutine
// and reports activity as tea.Msgs over a channel.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
)

type tokenMsg string
type assistantMsg string
type toolCallMsg struct{ name, args string }
type toolResultMsg struct{ name, text string }
type approvalMsg struct {
	tool, args string
	reply      chan bool
}
type doneMsg struct{ answer string }
type errMsg struct{ err error }

// eventToMsg converts an agent Event into the corresponding tea.Msg. It returns
// nil for event types the TUI does not render.
func eventToMsg(e agent.Event) tea.Msg {
	switch e.Type {
	case "token":
		return tokenMsg(e.Text)
	case "assistant":
		return assistantMsg(e.Text)
	case "tool_call":
		return toolCallMsg{name: e.Name, args: e.Text}
	case "tool_result":
		return toolResultMsg{name: e.Name, text: e.Text}
	default:
		return nil
	}
}

// waitFor reads the next message off the bridge channel. It is re-issued after
// each agent message so the loop keeps draining engine activity. Exactly one
// waitFor is ever in flight.
func waitFor(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-sub }
}
