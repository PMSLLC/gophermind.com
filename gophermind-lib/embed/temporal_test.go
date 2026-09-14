package embed

import (
	"testing"
	"time"
)

func TestTopKSkipsRetiredVectors(t *testing.T) {
	query := []float32{1, 0}
	items := []Vector{
		{ID: "retired", Values: []float32{1, 0}, ValidUntil: "2026-09-08T09:00:00Z"},
		{ID: "live", Values: []float32{0.9, 0.1}},
	}
	hits := TopK(query, items, 2)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	if hits[0].ID != "live" {
		t.Errorf("hit = %q, want live", hits[0].ID)
	}
}

func TestTopKKeepsVectorsWithoutAWindow(t *testing.T) {
	// Vectors written before validity windows existed carry neither field and
	// must keep ranking exactly as they did.
	query := []float32{1, 0}
	items := []Vector{{ID: "legacy", Values: []float32{1, 0}}}
	hits := TopK(query, items, 1)
	if len(hits) != 1 || hits[0].ID != "legacy" {
		t.Fatalf("legacy vector dropped: %+v", hits)
	}
}

func TestSupersedeRetiresNearDuplicates(t *testing.T) {
	at := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	idx := &Index{Vectors: []Vector{
		{ID: "old", Text: "deploy target is box A", Values: []float32{0.99, 0.1}},
		{ID: "other", Text: "the linter runs on push", Values: []float32{0, 1}},
	}}

	retired := idx.Supersede([]float32{1, 0}, at, 0.93)

	if len(retired) != 1 || retired[0] != "deploy target is box A" {
		t.Fatalf("retired = %v, want the near-duplicate fact", retired)
	}
	if got := idx.Vectors[0].ValidUntil; got != "2026-09-08T09:00:00Z" {
		t.Errorf("ValidUntil = %q, want the supersede time", got)
	}
	if got := idx.Vectors[1].ValidUntil; got != "" {
		t.Errorf("unrelated fact retired with ValidUntil %q", got)
	}
}

func TestSupersedeLeavesSimilarButDistinctFactsLive(t *testing.T) {
	// cosine 0.9 — related, below the 0.93 threshold, so it stays true.
	idx := &Index{Vectors: []Vector{
		{ID: "related", Text: "the deploy script lives in scripts/", Values: []float32{0.9, 0.436}},
	}}

	retired := idx.Supersede([]float32{1, 0}, time.Now(), 0.93)

	if len(retired) != 0 {
		t.Fatalf("retired = %v, want nothing below threshold", retired)
	}
	if idx.Vectors[0].ValidUntil != "" {
		t.Errorf("below-threshold fact was retired")
	}
}

func TestSupersedeIgnoresAlreadyRetiredFacts(t *testing.T) {
	idx := &Index{Vectors: []Vector{
		{ID: "old", Text: "deploy target is box A", Values: []float32{1, 0}, ValidUntil: "2026-07-01T00:00:00Z"},
	}}

	retired := idx.Supersede([]float32{1, 0}, time.Now(), 0.93)

	if len(retired) != 0 {
		t.Fatalf("retired = %v, want nothing (already retired)", retired)
	}
	if got := idx.Vectors[0].ValidUntil; got != "2026-07-01T00:00:00Z" {
		t.Errorf("ValidUntil overwritten to %q, want the original retirement time", got)
	}
}
