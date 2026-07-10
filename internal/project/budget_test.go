package project

import (
	"strings"
	"testing"
)

func TestCapContextUnderBudget(t *testing.T) {
	in := "short context"
	if got := CapContext(in, 100); got != in {
		t.Errorf("under-budget text should be unchanged, got %q", got)
	}
}

func TestCapContextTruncatesOverBudget(t *testing.T) {
	in := strings.Repeat("word ", 1000) // ~5000 bytes ≈ 1250 tokens
	out := CapContext(in, 50)           // 50 tokens ≈ 200 bytes
	if len(out) >= len(in) {
		t.Errorf("over-budget text should be truncated: in=%d out=%d", len(in), len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("truncated context should note the truncation: %q", out[len(out)-40:])
	}
}

func TestCapContextZeroBudgetIsNoop(t *testing.T) {
	in := "anything at all"
	if got := CapContext(in, 0); got != in {
		t.Errorf("zero/negative budget should not cap, got %q", got)
	}
}

func TestCapContextEmpty(t *testing.T) {
	if got := CapContext("", 10); got != "" {
		t.Errorf("empty stays empty, got %q", got)
	}
}
