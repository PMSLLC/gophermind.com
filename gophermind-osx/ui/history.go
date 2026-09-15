package ui

import "gophermind/gophermind-lib/llm"

// ApplyHistory replays msgs (as gophermind-osx/client.SessionMessages
// returns, decoded into llm.Message -- the exact wire format
// agent.Agent.ExportJSONL writes) into t, so resuming a saved session
// covers "resume: click session to attach, chat transcript loads"
// (.planning/tasks/04-05.json).
//
// The seeded system message is skipped: it's the base prompt this app (or
// its mode) builds internally, never something the user typed or should
// see re-rendered as a chat entry. An assistant turn's ToolCalls become one
// RoleToolCall entry per call (an assistant message can request several
// tools in one turn); a "tool" message becomes one RoleToolResult entry,
// using its Name field for the tool call's identity (ToolCallID exists to
// match a result back to its call, but the transcript only ever displays
// tool calls/results in order, not paired by ID -- same rendering ==
// wire-order assumption chatview.go's own display already makes).
func ApplyHistory(t *Transcript, msgs []llm.Message) {
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "user":
			t.AddUserMessage(m.Content)
		case "assistant":
			if m.Content != "" {
				t.AddAssistantText(m.Content)
			}
			for _, tc := range m.ToolCalls {
				t.AddToolCall(tc.Function.Name, tc.Function.Arguments)
			}
		case "tool":
			t.AddToolResult(m.Name, m.Content)
		}
	}
}
