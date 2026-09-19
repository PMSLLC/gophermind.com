package plan

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func TestProvenanceRoundTripAndReplace(t *testing.T) {
	r := newRepo(t)
	if p, err := loadProvenance(r); err != nil || len(p.Chunks) != 0 {
		t.Fatalf("missing file: %+v, %v", p, err)
	}
	if err := recordProvenance(r, 0, []string{"phase-001", "phase-001.task-001"}); err != nil {
		t.Fatal(err)
	}
	if err := recordProvenance(r, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := recordProvenance(r, 0, []string{"phase-002"}); err != nil { // a replayed chunk replaces its list
		t.Fatal(err)
	}
	p, err := loadProvenance(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"0": {"phase-002"}, "1": {}}
	if !reflect.DeepEqual(p.Chunks, want) {
		t.Errorf("Chunks = %v, want %v", p.Chunks, want)
	}
}

func TestProvenanceCorruptFileSaysHowToRecover(t *testing.T) {
	r := newRepo(t)
	if err := os.MkdirAll(r.Dir()+"/_state", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provenancePath(r), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProvenance(r); err == nil || !strings.Contains(err.Error(), "delete that file") {
		t.Errorf("err = %v", err)
	}
}

func TestChunksForReturnsBriefOrder(t *testing.T) {
	p := provenance{Chunks: map[string][]string{"2": {"x"}, "0": {"y", "x"}, "5": {"z"}, "10": {"x"}, "bad": {"x"}}}
	if got := p.chunksFor([]string{"x"}); !reflect.DeepEqual(got, []int{0, 2, 10}) {
		t.Errorf("chunksFor = %v, want [0 2 10]", got)
	}
	if got := p.chunksFor([]string{"nothing"}); len(got) != 0 {
		t.Errorf("chunksFor(nothing) = %v", got)
	}
}

func TestRunPass1RecordsWhichNodesEachChunkProduced(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	p, err := loadProvenance(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"0": {"phase-001", "phase-001.task-001", "phase-001.task-001.step-001"},
		"1": {"phase-001", "phase-001.task-002", "phase-001.task-002.step-001"},
		"2": {"phase-002", "phase-002.task-001", "phase-002.task-001.step-001"},
	}
	if !reflect.DeepEqual(p.Chunks, want) {
		t.Errorf("Chunks = %v", p.Chunks)
	}
}
