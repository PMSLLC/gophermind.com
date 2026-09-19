package plan

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func newRepo(t *testing.T) *plantree.Repo {
	t.Helper()
	r := plantree.Open(t.TempDir())
	root := plantree.Node{
		SchemaVersion: plantree.SchemaVersion, ID: plantree.RootID, Title: "demo", NodeRevision: 1,
		ContextDigest: "Plan for demo.", DependsOn: []string{}, Planning: plantree.Planning{Stage: plantree.StageSkeleton},
	}
	if err := r.Init(root); err != nil {
		t.Fatal(err)
	}
	return r
}

func ids(t *testing.T, r *plantree.Repo) []string {
	t.Helper()
	var out []string
	if err := r.Walk(func(n plantree.Node) error { out = append(out, n.ID+" "+n.Title); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func sampleOut() Pass1Output {
	return Pass1Output{
		Overview: "o",
		Phases: []PhaseOut{{
			Title: "Foundation", Digest: "base", Objective: "set up",
			Tasks: []TaskOut{{
				Title: "Repo layout", Digest: "where code lives",
				Steps: []StepOut{{Title: "Create module", Digest: "needed to compile"}, {Title: "Add CI", Digest: "catch breakage"}},
			}},
		}},
	}
}

func TestMergeBuildsSkeletonTree(t *testing.T) {
	r := newRepo(t)
	c, err := Merge(r, sampleOut())
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{Phases: 1, Tasks: 1, Steps: 2}) {
		t.Errorf("Created = %+v", c)
	}
	want := []string{
		"plan demo", "phase-001 Foundation", "phase-001.task-001 Repo layout",
		"phase-001.task-001.step-001 Create module", "phase-001.task-001.step-002 Add CI",
	}
	if got := ids(t, r); !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	step, _ := r.Get("phase-001.task-001.step-001")
	if step.Status != plantree.StatusUntouched || step.Planning.Stage != plantree.StageSkeleton || step.ContextDigest != "needed to compile" {
		t.Errorf("step = %+v", step)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	before := ids(t, r)
	c, err := Merge(r, sampleOut())
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{}) {
		t.Errorf("replay created %+v, want nothing", c)
	}
	if after := ids(t, r); !reflect.DeepEqual(before, after) {
		t.Errorf("replay changed the tree:\n%v\n%v", before, after)
	}
}

func TestMergeMatchesTitlesIgnoringCaseAndSpacing(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	again := sampleOut()
	again.Phases[0].Title = "  foundation "
	again.Phases[0].Tasks[0].Steps = append(again.Phases[0].Tasks[0].Steps, StepOut{Title: "Add lint", Digest: "style"})
	c, err := Merge(r, again)
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{Steps: 1}) {
		t.Errorf("Created = %+v, want one new step under the existing task", c)
	}
	if got, _ := r.Get("phase-001.task-001.step-003"); got.Title != "Add lint" {
		t.Errorf("new step = %+v", got)
	}
}

func TestMergeNumbersAfterTheHighestSibling(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "One", Digest: "d"}, {Title: "Two", Digest: "d"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "Three", Digest: "d"}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get("phase-003"); err != nil || got.Title != "Three" {
		t.Errorf("phase-003 = %+v, %v", got, err)
	}
}

func TestMergeCollapsesWhitespaceInTitlesAndDigests(t *testing.T) {
	r := newRepo(t)
	out := Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "Two\nline   title", Digest: "why\n it exists"}}}
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	n, _ := r.Get("phase-001")
	if n.Title != "Two line title" || strings.Contains(n.ContextDigest, "\n") {
		t.Errorf("node = %+v", n)
	}
}

func TestMergeReportsTheSiblingLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("creates 999 nodes")
	}
	r := newRepo(t)
	for i := 1; i <= 999; i++ {
		id, _ := plantree.ChildID(plantree.RootID, i)
		n, err := newSkeleton(id, fmt.Sprintf("phase %d", i), "d", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "one too many", Digest: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "one too many") {
		t.Errorf("Merge past 999 siblings = %v, want an error naming the title", err)
	}
}
