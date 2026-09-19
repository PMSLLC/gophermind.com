package plan

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

func TestExcerptsForShowsTheBriefBehindANode(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001.task-002"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "second part text") {
		t.Errorf("ExcerptsFor = %q, want the chunk that produced the task", got)
	}
	if strings.Contains(got, "third part text") {
		t.Errorf("ExcerptsFor leaked another task's brief: %q", got)
	}
}

func TestExcerptsForClimbsToAnAncestor(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	// A step added after the chunk was recorded has no provenance of its own,
	// so the excerpt has to come from the task above it.
	step, err := newSkeleton("phase-001.task-002.step-009", "Late step", "added later", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(step); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001.task-002.step-009"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "second part text") {
		t.Errorf("ExcerptsFor = %q, want the ancestor's chunk", got)
	}
}

func TestExcerptsForIsBoundedAndRuneSafe(t *testing.T) {
	r := plantree.Open(t.TempDir())
	brief := "# One\n" + strings.Repeat("\U0001D11E", 20000) + "\n"
	if _, err := RunPass1(context.Background(), r, brief, &fake{reply: func(int, string) (string, error) {
		return `{"phases":[{"title":"Alpha","digest":"d","objective":"","tasks":[{"title":"T1","digest":"d","objective":"","steps":[{"title":"S1","digest":"d"}]}]}],"overview":"o"}`, nil
	}}, Options{ProjectName: "demo", ChunkBytes: 4000}); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001"}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 900 {
		t.Errorf("ExcerptsFor returned %d bytes, want at most 900", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("ExcerptsFor cut a rune in half")
	}
	if big, err := ExcerptsFor(r, []string{"phase-001"}, 10*ExcerptsForCapBytes); err != nil || len(big) > ExcerptsForCapBytes {
		t.Errorf("an oversize budget must be clamped: %d bytes, %v", len(big), err)
	}
}

func TestExcerptsForWithoutABriefOrNodesSaysNothing(t *testing.T) {
	r := newRepo(t) // no pass 1 ran here, so there is no brief and no state
	for _, ids := range [][]string{nil, {"phase-001"}} {
		got, err := ExcerptsFor(r, ids, 0)
		if err != nil || got != "" {
			t.Errorf("ExcerptsFor(%v) = %q, %v; want no excerpt and no error", ids, got, err)
		}
	}
}
