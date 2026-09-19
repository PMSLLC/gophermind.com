package plan

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func TestReadBriefAndBriefChunksMatchTheRun(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	brief, err := ReadBrief(r)
	if err != nil || brief != threePartBrief {
		t.Fatalf("ReadBrief = %q, %v", brief, err)
	}
	chunks, err := BriefChunks(r)
	if err != nil || len(chunks) != 3 || !strings.Contains(chunks[1].Text, "second part text") {
		t.Fatalf("BriefChunks = %v, %v", chunks, err)
	}
	if joined(chunks) != threePartBrief {
		t.Error("the chunks must reproduce the brief")
	}
}

func TestBriefChunksWithoutARunWrapsNotExist(t *testing.T) {
	r := newRepo(t)
	if _, err := BriefChunks(r); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestExcerpts(t *testing.T) {
	chunks := []Chunk{
		{Index: 0, Text: "zero text\n"},
		{Index: 1, Text: "one text\n"},
		{Index: 2, Text: "two text\n"},
	}
	got := Excerpts(chunks, []int{0, 2}, 10000)
	if !strings.Contains(got, "[part 1 of 3]") || !strings.Contains(got, "zero text") ||
		!strings.Contains(got, "[part 3 of 3]") || strings.Contains(got, "one text") {
		t.Errorf("Excerpts = %q", got)
	}
	if got := Excerpts(chunks, nil, 10000); got != "" {
		t.Errorf("no indexes: %q", got)
	}
	if got := Excerpts(chunks, []int{-1, 9, 1}, 10000); !strings.Contains(got, "one text") || strings.Contains(got, "zero") {
		t.Errorf("out-of-range indexes must be skipped: %q", got)
	}
}

func TestExcerptsCutsTheChunkThatDoesNotFitAndStaysInBudget(t *testing.T) {
	long := strings.Repeat("line of brief text\n\n", 40) // 800 bytes
	chunks := []Chunk{{Index: 0, Text: "short\n"}, {Index: 1, Text: long}, {Index: 2, Text: "never reached\n"}}
	got := Excerpts(chunks, []int{0, 1, 2}, 300)
	if len(got) > 300 || !strings.Contains(got, "short") || !strings.Contains(got, "[excerpt cut to fit the budget]") || strings.Contains(got, "never reached") {
		t.Errorf("len=%d %q", len(got), got)
	}
	if got := Excerpts(chunks, []int{1}, 50); got != "" {
		t.Errorf("a budget too small to be useful must give nothing, got %q", got)
	}
}
