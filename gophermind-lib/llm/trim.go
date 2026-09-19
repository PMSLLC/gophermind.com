package llm

import (
	"encoding/json"
	"strings"
)

// tokenEstimator is a lightweight byte-to-token heuristic. It approximates the
// OpenAI tiktoken counting rules well enough for budgeting: ~4 bytes per token
// for ASCII, with a small overhead for JSON structure. The estimate is always
// an upper bound — the real API count may be lower, never higher.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// Rough heuristic: 1 token ≈ 4 bytes for typical English text.
	// This is conservative (overestimates) so the trimmer errs on the side of
	// keeping more context.
	return (len(s) + 3) / 4
}

// estimateMessageTokens returns the approximate token count for a single
// Message as it would appear in a chat request. It accounts for the role label,
// content, and any tool calls (name + arguments).
func estimateMessageTokens(m Message) int {
	n := estimateTokens(m.Role) + 4 // role label overhead
	n += estimateTokens(m.Content)
	for _, tc := range m.ToolCalls {
		n += estimateTokens(tc.ID)
		n += estimateTokens(tc.Function.Name)
		n += estimateTokens(tc.Function.Arguments)
	}
	return n
}

// EstimateMessagesTokens returns the total approximate token count for a slice
// of messages. It is a fast, allocation-free heuristic used to decide whether
// the conversation fits within a model's context window.
func EstimateMessagesTokens(msgs []Message) int {
	return estimateMessagesTokens(msgs)
}

// estimateMessagesTokens returns the total approximate token count for a slice
// of messages. It is a fast, allocation-free heuristic used to decide whether
// the conversation fits within a model's context window.
func estimateMessagesTokens(msgs []Message) int {
	var total int
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	return total
}

// estimateRequestTokens returns the approximate token count for a chat request
// body: system prompt overhead, all messages, tool definitions, and sampling
// params. This is used to decide whether to trim before sending.
func estimateRequestTokens(msgs []Message, tools []Tool, model string) int {
	n := estimateTokens(model) + 8 // model label overhead
	n += estimateMessagesTokens(msgs)
	for _, t := range tools {
		b, _ := json.Marshal(t)
		n += estimateTokens(string(b))
	}
	return n
}

// elidedToolOutput replaces a tool result that had to be dropped to fit the
// context window. The message itself stays so its assistant tool call is
// still answered, which strict chat templates require.
const elidedToolOutput = "[output elided to fit the context window; re-run the tool if you still need it]"

// TrimToBudget shrinks msgs until the estimated token count is <= maxTokens
// and returns the result with the number of messages changed or removed.
//
// It works oldest first, in two passes, and never touches the system prompt
// (index 0) or the most recent user message and everything after it, which is
// the turn in flight:
//
//  1. Tool results are replaced by a short placeholder. The message stays, so
//     every assistant tool call keeps its answer.
//  2. If that is not enough, whole exchanges older than the latest user
//     message are removed: a plain message alone, or an assistant tool-call
//     message together with all of its tool results.
//
// If the budget is already sufficient, msgs is returned unchanged with 0.
func TrimToBudget(msgs []Message, maxTokens int) ([]Message, int) {
	est := estimateMessagesTokens(msgs)
	if est <= maxTokens {
		return msgs, 0
	}

	lastUser := 0
	for i := len(msgs) - 1; i >= 1; i-- {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}

	out := make([]Message, len(msgs))
	copy(out, msgs)
	changed := 0

	// Pass 1: elide tool output, oldest first.
	placeholder := estimateTokens(elidedToolOutput)
	for i := 1; i < len(out) && est > maxTokens; i++ {
		if out[i].Role != "tool" || out[i].Content == elidedToolOutput {
			continue
		}
		saved := estimateTokens(out[i].Content) - placeholder
		if saved <= 0 {
			continue
		}
		out[i].Content = elidedToolOutput
		est -= saved
		changed++
	}
	if est <= maxTokens {
		return out, changed
	}

	// Pass 2: remove whole exchanges from before the latest user message.
	drop := make(map[int]bool)
	for i := 1; i < lastUser && est > maxTokens; {
		end := i + 1
		if out[i].Role == "assistant" && len(out[i].ToolCalls) > 0 {
			for end < lastUser && out[end].Role == "tool" {
				end++
			}
		}
		for j := i; j < end; j++ {
			est -= estimateMessageTokens(out[j])
			drop[j] = true
			changed++
		}
		i = end
	}

	kept := make([]Message, 0, len(out))
	for i, m := range out {
		if !drop[i] {
			kept = append(kept, m)
		}
	}
	return kept, changed
}

// SummarizeTurns replaces the oldest user/tool turns with a single summary
// message, keeping the total message count manageable while preserving context.
// The summary is a compact description of what was discussed/executed.
//
// Returns the modified messages slice and the number of turns replaced.
func SummarizeTurns(msgs []Message, maxMessages int, summaryPrefix string) ([]Message, int) {
	if len(msgs) <= maxMessages {
		return msgs, 0
	}

	// Keep system prompt and the last N turns; summarize the rest.
	keepFrom := len(msgs) - (maxMessages - 1) // -1 for the summary placeholder
	if keepFrom < 1 {
		keepFrom = 1 // always keep at least one turn after system
	}

	// Build a summary of the dropped turns.
	var parts []string
	for i := 1; i < keepFrom; i++ {
		m := msgs[i]
		switch m.Role {
		case "user":
			// Truncate user content to a short preview.
			preview := m.Content
			if len(preview) > 100 {
				preview = preview[:100] + "…"
			}
			parts = append(parts, "user: "+preview)
		case "tool":
			preview := m.Content
			if len(preview) > 100 {
				preview = preview[:100] + "…"
			}
			parts = append(parts, m.Name+": "+preview)
		}
	}

	summary := summaryPrefix + " " + strings.Join(parts, " | ")
	if len(summary) > 2000 {
		summary = summary[:2000] + "…"
	}

	// Build result: system prompt, summary, then kept turns.
	result := make([]Message, 0, maxMessages+1)
	if len(msgs) > 0 && msgs[0].Role == "system" {
		result = append(result, msgs[0])
	}
	result = append(result, Message{Role: "system", Content: summary})
	result = append(result, msgs[keepFrom:]...)

	return result, keepFrom - 1
}
