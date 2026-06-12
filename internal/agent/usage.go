package agent

import (
	"fmt"

	"gophermind/internal/llm"
)

// maxTokensPerTurn bounds a single response's reported token counts before they
// are accumulated. The usage block comes from an untrusted endpoint, so a
// malformed, negative, or absurd value must not corrupt the running total or
// overflow the cost arithmetic. 100M tokens is far beyond any real context
// window, so anything larger is treated as garbage and clamped.
const maxTokensPerTurn = 100_000_000

// UsageMeter is a per-session token and cost accumulator. It sums the token
// usage reported by each model response and estimates spend from configurable
// per-1K-token prices. Prices default to 0, so EstimatedCostUSD reports 0 until
// they are set. A meter is owned by a single Agent and is not safe for
// concurrent use.
type UsageMeter struct {
	// InputPricePer1K and OutputPricePer1K are USD prices per 1,000 prompt and
	// completion tokens respectively. Zero (the default) yields a zero cost.
	InputPricePer1K  float64
	OutputPricePer1K float64

	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// UsageSnapshot is an immutable view of the meter's running totals, suitable for
// display. Cost is in USD.
type UsageSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
}

// Add folds one response's usage into the running totals. Reported counts are
// validated: negatives are dropped to 0 and absurd values are clamped, so a
// hostile or buggy endpoint cannot drive the totals negative or overflow the
// cost math. When the endpoint reports no total, it is derived from the parts.
func (m *UsageMeter) Add(u llm.Usage) {
	p := clampTokens(u.PromptTokens)
	c := clampTokens(u.CompletionTokens)
	t := clampTokens(u.TotalTokens)
	if t == 0 {
		t = p + c
	}
	m.PromptTokens += p
	m.CompletionTokens += c
	m.TotalTokens += t
}

// Snapshot returns the current totals and estimated cost.
func (m *UsageMeter) Snapshot() UsageSnapshot {
	return UsageSnapshot{
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		TotalTokens:      m.TotalTokens,
		CostUSD:          m.EstimatedCostUSD(),
	}
}

// EstimatedCostUSD computes spend from the cumulative tokens and the configured
// per-1K prices. Returns 0 when prices are unset.
func (m *UsageMeter) EstimatedCostUSD() float64 {
	return float64(m.PromptTokens)/1000*m.InputPricePer1K +
		float64(m.CompletionTokens)/1000*m.OutputPricePer1K
}

// String renders a snapshot as a compact one-line meter, e.g.
// "tokens: 1200/340/1540 · ~$0.02" (prompt/completion/total · estimated cost).
func (s UsageSnapshot) String() string {
	return fmt.Sprintf("tokens: %d/%d/%d · ~$%.2f",
		s.PromptTokens, s.CompletionTokens, s.TotalTokens, s.CostUSD)
}

// clampTokens normalizes one untrusted token count into [0, maxTokensPerTurn].
func clampTokens(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxTokensPerTurn {
		return maxTokensPerTurn
	}
	return n
}
