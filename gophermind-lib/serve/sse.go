package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gophermind/gophermind-lib/agent"
)

// writeSSEEvent writes a typed SSE frame to w: an optional "event: <event>\n"
// line (omitted when event is empty), followed by one "data: <line>\n" line
// per line of data (CR/LF normalized, so content can never inject additional
// SSE fields or events), then a terminating blank line. An empty data still
// yields one "data: \n" line, matching the SSE spec. If flusher is non-nil,
// the frame is flushed immediately.
func writeSSEEvent(w io.Writer, flusher http.Flusher, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// SSEFramesForAgentEvent maps an agent.Event to a typed SSE (event, data)
// pair. emit is false for unrecognized event types, so callers can silently
// skip them without special-casing.
func SSEFramesForAgentEvent(ev agent.Event) (event, data string, emit bool) {
	switch ev.Type {
	case "token":
		return "token", ev.Text, true
	case "assistant":
		return "assistant", ev.Text, true
	case "tool_call":
		b, _ := json.Marshal(ToolCallEvent{Name: ev.Name, Args: ev.Text})
		return "tool_call", string(b), true
	case "tool_result":
		b, _ := json.Marshal(ToolResultEvent{Name: ev.Name, Text: ev.Text})
		return "tool_result", string(b), true
	case "usage":
		b, _ := json.Marshal(ev.Usage)
		return "usage", string(b), true
	default:
		return "", "", false
	}
}
