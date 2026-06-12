package agent

import (
	"math"
	"testing"

	"gophermind/internal/llm"
)

// TestMeterAccumulatesAcrossTurns sums usage from multiple responses.
func TestMeterAccumulatesAcrossTurns(t *testing.T) {
	var m UsageMeter
	m.Add(llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	m.Add(llm.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60})

	s := m.Snapshot()
	if s.PromptTokens != 150 || s.CompletionTokens != 30 || s.TotalTokens != 180 {
		t.Errorf("snapshot = %+v, want 150/30/180", s)
	}
}

// TestMeterCostZeroWhenPricesUnset confirms cost is 0 by default.
func TestMeterCostZeroWhenPricesUnset(t *testing.T) {
	var m UsageMeter
	m.Add(llm.Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000})
	if c := m.EstimatedCostUSD(); c != 0 {
		t.Errorf("cost = %v, want 0 when prices unset", c)
	}
}

// TestMeterCostFromPrices computes spend from configured per-1K prices.
func TestMeterCostFromPrices(t *testing.T) {
	m := UsageMeter{InputPricePer1K: 0.50, OutputPricePer1K: 1.50}
	m.Add(llm.Usage{PromptTokens: 2000, CompletionTokens: 1000, TotalTokens: 3000})
	// 2000/1000*0.50 + 1000/1000*1.50 = 1.00 + 1.50 = 2.50
	if c := m.EstimatedCostUSD(); math.Abs(c-2.50) > 1e-9 {
		t.Errorf("cost = %v, want 2.50", c)
	}
}

// TestMeterDerivesTotalWhenMissing fills in total from the parts.
func TestMeterDerivesTotalWhenMissing(t *testing.T) {
	var m UsageMeter
	m.Add(llm.Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 0})
	if got := m.Snapshot().TotalTokens; got != 48 {
		t.Errorf("total = %d, want 48 derived from parts", got)
	}
}

// TestMeterClampsUntrustedCounts drops negatives and clamps absurd values so a
// hostile endpoint cannot corrupt totals or overflow the cost math.
func TestMeterClampsUntrustedCounts(t *testing.T) {
	var m UsageMeter
	m.Add(llm.Usage{PromptTokens: -5, CompletionTokens: -1, TotalTokens: -10})
	s := m.Snapshot()
	if s.PromptTokens != 0 || s.CompletionTokens != 0 || s.TotalTokens != 0 {
		t.Errorf("negative counts not dropped: %+v", s)
	}

	var m2 UsageMeter
	m2.InputPricePer1K = 1
	m2.Add(llm.Usage{PromptTokens: math.MaxInt64, CompletionTokens: 0, TotalTokens: math.MaxInt64})
	s2 := m2.Snapshot()
	if s2.PromptTokens != maxTokensPerTurn {
		t.Errorf("prompt tokens = %d, want clamped to %d", s2.PromptTokens, maxTokensPerTurn)
	}
	if c := m2.EstimatedCostUSD(); math.IsInf(c, 0) || math.IsNaN(c) || c < 0 {
		t.Errorf("cost not finite/non-negative after clamp: %v", c)
	}
}

// TestSnapshotString renders the compact one-line meter.
func TestSnapshotString(t *testing.T) {
	m := UsageMeter{InputPricePer1K: 0.01, OutputPricePer1K: 0.03}
	m.Add(llm.Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000})
	got := m.Snapshot().String()
	want := "tokens: 1000/1000/2000 · ~$0.04"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
