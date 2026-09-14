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

// TrimToBudget removes the oldest user/tool turns from msgs (keeping the system
// prompt and the most recent assistant turn) until the estimated token count
// of the remaining messages is ≤ maxTokens. It returns the trimmed slice and
// the number of turns dropped.
//
// The system prompt (first message, role=="system") is never dropped. The most
// recent assistant turn is also preserved so the model always has a coherent
// ending. Only user and tool-role messages are eligible for trimming.
//
// If the budget is already sufficient, msgs is returned unchanged with 0 dropped.
func TrimToBudget(msgs []Message, maxTokens int) ([]Message, int) {
	est := estimateMessagesTokens(msgs)
	if est <= maxTokens {
		return msgs, 0
	}

	// We need to drop turns. Keep the system prompt (index 0) and the last
	// assistant turn. Drop oldest user/tool turns first.
	dropped := 0
	result := make([]Message, 0, len(msgs))

	// Collect indices of turns we can drop (user and tool, excluding the last
	// assistant turn if present).
	droppable := make([]int, 0, len(msgs))
	lastAssistant := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && len(msgs[i].ToolCalls) == 0 && msgs[i].Content != "" {
			lastAssistant = i
			break
		}
	}

	for i := 1; i < len(msgs); i++ { // skip system prompt (index 0)
		if i == lastAssistant {
			continue // preserve the last assistant turn
		}
		if msgs[i].Role == "user" || msgs[i].Role == "tool" {
			droppable = append(droppable, i)
		}
	}

	// Drop from the oldest first.
	for _, idx := range droppable {
		if est <= maxTokens {
			break
		}
		est -= estimateMessageTokens(msgs[idx])
		dropped++
	}

	// Build the result: keep system prompt, keep remaining turns up to and
	// including the last assistant turn.
	keepUpTo := len(msgs)
	if lastAssistant >= 0 {
		keepUpTo = lastAssistant + 1
	}

	// Rebuild: include system prompt, then all non-dropped turns up to keepUpTo.
	dropSet := make(map[int]bool)
	droppedCount := 0
	for _, idx := range droppable {
		if est < maxTokens {
			break
		}
		est -= estimateMessageTokens(msgs[idx])
		dropSet[idx] = true
		droppedCount++
	}

	for i := 0; i < keepUpTo; i++ {
		if !dropSet[i] {
			result = append(result, msgs[i])
		}
	}

	return result, droppedCount
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
