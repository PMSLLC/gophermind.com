package usagelog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	t0 := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	if err := Append(path, Record{Time: t0, Model: "m1", PromptTokens: 100, CompletionTokens: 50, CostUSD: 0.01}); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Record{Time: t0.Add(time.Hour), Model: "m2", PromptTokens: 200, CompletionTokens: 80, CostUSD: 0.03}); err != nil {
		t.Fatal(err)
	}
	recs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
}

func TestReportAggregates(t *testing.T) {
	day := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	recs := []Record{
		{Time: day, Model: "m1", PromptTokens: 100, CompletionTokens: 50, CostUSD: 0.01},
		{Time: day.Add(2 * time.Hour), Model: "m1", PromptTokens: 100, CompletionTokens: 50, CostUSD: 0.01},
		{Time: day.Add(24 * time.Hour), Model: "m2", PromptTokens: 200, CompletionTokens: 80, CostUSD: 0.03},
	}
	out := Report(recs)
	if !strings.Contains(out, "2026-07-10") || !strings.Contains(out, "2026-07-11") {
		t.Errorf("report should group by day:\n%s", out)
	}
	if !strings.Contains(out, "m1") || !strings.Contains(out, "m2") {
		t.Errorf("report should break down by model:\n%s", out)
	}
	// Total cost 0.05 should appear.
	if !strings.Contains(out, "0.05") {
		t.Errorf("report should show total cost:\n%s", out)
	}
}

func TestReportEmpty(t *testing.T) {
	if out := Report(nil); !strings.Contains(strings.ToLower(out), "no usage") {
		t.Errorf("empty report should say so: %q", out)
	}
}
