package ui

import (
	"strings"
	"testing"
)

func TestFuzzyFilter(t *testing.T) {
	cands := []string{"sessions", "serve", "status", "search", "run"}
	got := FuzzyFilter("ss", cands)
	if len(got) == 0 || got[0] != "sessions" {
		t.Errorf("fuzzy 'ss' should rank sessions first, got %v", got)
	}
	// A non-subsequence query matches nothing.
	if len(FuzzyFilter("zzz", cands)) != 0 {
		t.Errorf("no candidate should match 'zzz'")
	}
	// Empty query returns all (order preserved).
	if len(FuzzyFilter("", cands)) != len(cands) {
		t.Errorf("empty query should return all")
	}
}

func TestFuzzyPrefersStartAndConsecutive(t *testing.T) {
	// "run" should beat "forewarn" for query "run" (start + consecutive).
	got := FuzzyFilter("run", []string{"forewarn", "run"})
	if got[0] != "run" {
		t.Errorf("prefix/consecutive match should rank first, got %v", got)
	}
	_ = strings.TrimSpace
}
