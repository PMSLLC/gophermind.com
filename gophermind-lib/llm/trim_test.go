package llm

import (
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
