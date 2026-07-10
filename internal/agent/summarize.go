package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/llm"
	"gophermind/internal/tools"
)

// summarizeTool is the tool definition for summarizing large tool results.
var summarizeTool = llm.Tool{
	Type: "function",
	Function: llm.Function{
		Name:        "_gophermind_summarize",
		Description: "Summarize a large tool result into a compact form. Call when a tool result is too large for context.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":       map[string]any{"type": "string", "description": "The text to summarize."},
				"max_tokens": map[string]any{"type": "integer", "description": "Maximum tokens for the summary (default: 500)."},
			},
			"required": []string{"text"},
		},
	},
}

// SummarizeTool returns a tool that summarizes large tool results.
func SummarizeTool() tools.Tool {
	return tools.Tool{
		Name:        "_gophermind_summarize",
		Description: summarizeTool.Function.Description,
		Schema:      summarizeTool.Function.Parameters,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			// This is a no-op tool — the model is expected to summarize itself.
			// It serves as a hint that the model should summarize large results.
			return "Summarize the large result into a compact form.", nil
		},
	}
}

// SummarizeResult checks if a tool result exceeds the summary threshold and
// returns a truncated version with a note. The caller can then use the full
// text on demand.
func SummarizeResult(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes {
		return text, false
	}
	// Truncate to maxBytes and add a note.
	truncated := text[:maxBytes]
	// Find a clean break at a line boundary.
	if idx := strings.LastIndex(truncated, "\n"); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated + fmt.Sprintf("\n\n[truncated: %d bytes dropped — use full result on demand]", len(text)-maxBytes), true
}
