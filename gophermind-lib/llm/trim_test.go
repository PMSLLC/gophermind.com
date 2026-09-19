package llm

import (
	"strings"
	"testing"
)

func TestEstimateTokensBasic(t *testing.T) {
	// ASCII text: ~4 bytes per token.
	if n := estimateTokens("Hello, world!"); n < 1 {
		t.Errorf("estimateTokens('Hello, world!') = %d, want >= 1", n)
	}
	if estimateTokens("") != 0 {
		t.Error("estimateTokens(\"\") should be 0")
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	m := Message{Role: "user", Content: "Hello"}
	n := estimateMessageTokens(m)
	if n < 1 {
		t.Errorf("estimateMessageTokens(user: Hello) = %d, want >= 1", n)
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}
	n := estimateMessagesTokens(msgs)
	if n < 1 {
		t.Errorf("estimateMessagesTokens(2 msgs) = %d, want >= 1", n)
	}
}

func TestTrimToBudgetNoTrimNeeded(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}
	trimmed, dropped := TrimToBudget(msgs, 10000)
	if dropped != 0 {
		t.Errorf("TrimToBudget(10000): dropped = %d, want 0", dropped)
	}
	if len(trimmed) != len(msgs) {
		t.Errorf("TrimToBudget(10000): len = %d, want %d", len(trimmed), len(msgs))
	}
}

func TestTrimToBudgetDropsOldest(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "First turn with a lot of content that should be trimmed away because the context is getting too large"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Second turn"},
		{Role: "assistant", Content: "Response 2"},
	}
	// Use a very tight budget to force trimming.
	trimmed, dropped := TrimToBudget(msgs, 20)
	if dropped == 0 {
		t.Error("TrimToBudget(20): expected some drops, got 0")
	}
	// System prompt should always be kept.
	if len(trimmed) == 0 || trimmed[0].Role != "system" {
		t.Errorf("TrimToBudget: first message should be system, got %+v", trimmed)
	}
}

func TestTrimToBudgetKeepsLastAssistant(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Turn 1"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Turn 2"},
		{Role: "assistant", Content: "Response 2"},
	}
	trimmed, _ := TrimToBudget(msgs, 50)
	// The last assistant turn should be preserved.
	if len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last.Role != "assistant" {
			t.Errorf("Last message should be assistant, got %s", last.Role)
		}
	}
}

func TestSummarizeTurnsNoSummarizeNeeded(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}
	summarized, replaced := SummarizeTurns(msgs, 10, "Summarized")
	if replaced != 0 {
		t.Errorf("SummarizeTurns(10): replaced = %d, want 0", replaced)
	}
	if len(summarized) != len(msgs) {
		t.Errorf("SummarizeTurns(10): len = %d, want %d", len(summarized), len(msgs))
	}
}

func TestSummarizeTurnsSummarizesOldest(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Turn 1"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Turn 2"},
		{Role: "assistant", Content: "Response 2"},
		{Role: "user", Content: "Turn 3"},
	}
	summarized, replaced := SummarizeTurns(msgs, 4, "Summarized")
	if replaced == 0 {
		t.Error("SummarizeTurns(4): expected some replacements, got 0")
	}
	// Should have system + summary + kept turns.
	if len(summarized) <= 1 {
		t.Errorf("SummarizeTurns(4): got %d messages, want more", len(summarized))
	}
}

// A single in-flight turn: one user prompt, then several tool exchanges whose
// results are large. This is the shape that overflowed a 98k window when a
// model read whole documents one after another.
func inFlightTurn(n, toolBytes int) []Message {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do the task"},
	}
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{{ID: id, Function: FunctionCall{Name: "read_file", Arguments: `{"path":"x"}`}}}},
			Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", toolBytes)},
		)
	}
	return msgs
}

func TestTrimToBudgetFitsAnInFlightToolTurn(t *testing.T) {
	msgs := inFlightTurn(10, 40_000) // ~100k tokens of tool output
	const budget = 30_000
	trimmed, dropped := TrimToBudget(msgs, budget)
	if got := EstimateMessagesTokens(trimmed); got > budget {
		t.Fatalf("trimmed estimate = %d tokens, want <= %d (dropped=%d)", got, budget, dropped)
	}
	if dropped == 0 {
		t.Error("expected trimming to report work done")
	}
}

func TestTrimToBudgetKeepsToolCallsPaired(t *testing.T) {
	trimmed, _ := TrimToBudget(inFlightTurn(10, 40_000), 30_000)
	calls := map[string]bool{}
	for _, m := range trimmed {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}
	}
	for _, m := range trimmed {
		if m.Role == "tool" && !calls[m.ToolCallID] {
			t.Errorf("tool result %q has no matching assistant tool call", m.ToolCallID)
		}
	}
	answered := map[string]bool{}
	for _, m := range trimmed {
		if m.Role == "tool" {
			answered[m.ToolCallID] = true
		}
	}
	for id := range calls {
		if !answered[id] {
			t.Errorf("assistant tool call %q has no tool result", id)
		}
	}
}

func TestTrimToBudgetKeepsTheCurrentUserPrompt(t *testing.T) {
	trimmed, _ := TrimToBudget(inFlightTurn(10, 40_000), 30_000)
	found := false
	for _, m := range trimmed {
		if m.Role == "user" && m.Content == "do the task" {
			found = true
		}
	}
	if !found {
		t.Error("the user prompt that started this turn was dropped")
	}
}
