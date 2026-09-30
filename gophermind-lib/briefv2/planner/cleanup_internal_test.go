package planner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
)

func ids(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("type %q", fmt.Sprintf("%s%d", prefix, i))
	}
	return out
}

// The stored id list is capped, the total is exact, and the warning reports the
// running total, not only this pass.
func TestNoteIgnoredKeepsAnExactRunningTotal(t *testing.T) {
	col := events.NewCollector()
	p := New(Deps{Sink: col})
	var st contractState
	p.noteIgnored(&st, "contract", ids("a", 150))
	p.noteIgnored(&st, "contract", ids("b", 150))
	if st.IgnoredTotal != 300 {
		t.Errorf("total = %d, want 300", st.IgnoredTotal)
	}
	if len(st.IgnoredDuplicates) != maxIgnoredRecorded || !st.IgnoredTruncated {
		t.Errorf("stored %d truncated %v, want %d and true", len(st.IgnoredDuplicates), st.IgnoredTruncated, maxIgnoredRecorded)
	}
	w := col.OfKind(events.KindWarning)
	if len(w) != 2 || !strings.Contains(w[1].Message, "300") || !strings.Contains(w[1].Message, "150 ") {
		t.Errorf("warnings = %v, want the second to carry this pass (150) and the running total (300)", w)
	}
	var small contractState
	p.noteIgnored(&small, "contract", ids("c", 3))
	if small.IgnoredTotal != 3 || small.IgnoredTruncated {
		t.Errorf("small state total %d truncated %v", small.IgnoredTotal, small.IgnoredTruncated)
	}
}

func TestIgnoredDuplicatesReader(t *testing.T) {
	dir := t.TempDir()
	if got, total, trunc, err := IgnoredDuplicates(dir); err != nil || len(got) != 0 || total != 0 || trunc {
		t.Errorf("absent state: %v %d %v %v, want empty", got, total, trunc, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "_state"), 0o755); err != nil {
		t.Fatal(err)
	}
	st := contractState{IgnoredDuplicates: []string{`type "a"`}, IgnoredTotal: 250, IgnoredTruncated: true}
	if err := writeJSON((&run{dir: dir}).path(stateContract), st); err != nil {
		t.Fatal(err)
	}
	got, total, trunc, err := IgnoredDuplicates(dir)
	if err != nil || len(got) != 1 || total != 250 || !trunc {
		t.Errorf("got %v %d %v %v", got, total, trunc, err)
	}
	// A state written before the total existed: the stored list is the total.
	old := contractState{IgnoredDuplicates: []string{`type "a"`, `type "b"`}}
	if err := writeJSON((&run{dir: dir}).path(stateContract), old); err != nil {
		t.Fatal(err)
	}
	if _, total, trunc, err := IgnoredDuplicates(dir); err != nil || total != 2 || trunc {
		t.Errorf("old state: total %d trunc %v err %v, want 2 false", total, trunc, err)
	}
}

// Only a model-caused failure is a repair attempt.
func TestFailedAttemptCountsOnlyModelCausedFailures(t *testing.T) {
	ctx := context.Background()
	gone, cancel := context.WithCancel(ctx)
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"no error", ctx, nil, false},
		{"unusable reply", ctx, &router.ChainExhausted{ParseErr: errors.New("bad json")}, true},
		{"unusable reply, wrapped", ctx, fmt.Errorf("x: %w", &router.ChainExhausted{ParseErr: errors.New("bad json")}), true},
		{"chain failed with no bad reply", ctx, &router.ChainExhausted{Reasons: []router.EntryReason{{Kind: router.ReasonFailed}}}, false},
		{"privacy", ctx, &router.ChainExhausted{Reasons: []router.EntryReason{{Kind: router.ReasonPrivacy}}}, false},
		{"ledger or budget error", ctx, errors.New("ledger: disk full"), false},
		{"caller gave up", gone, &router.ChainExhausted{ParseErr: errors.New("bad json")}, false},
	}
	for _, c := range cases {
		if got := failedAttempt(c.ctx, c.err); got != c.want {
			t.Errorf("%s: failedAttempt = %v, want %v", c.name, got, c.want)
		}
	}
}

// One id namespace: the first emission wins across components, types and
// functions, with a warning entry, never a validation error.
// fnOutline is okOutline with a type and a component whose ids a function id
// can equal (a function id starts with fn-, and so may a type or a component id).
var fnOutline = strings.NewReplacer(`"id": "name-error"`, `"id": "fn-a"`, `{"id": "types", "package": "x"}`, `{"id": "fn-types", "package": "x"}`).Replace(okOutline)

func TestMergePassDropsAFunctionWhoseIDIsATypeID(t *testing.T) {
	base, _, err := parseOutline(fnOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"types": [], "functions": [
 {"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []},
 {"id": "fn-ok", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []},
 {"id": "fn-types", "package": "x", "file": "internal/x/b.go", "signature": "func B()", "doc": "d", "uses": []}]}`
	doc, _, ignored, err := mergePass(base, "greeting", reply, testRunID)
	if err != nil {
		t.Fatalf("a function id that repeats a type or component id is noise: %v", err)
	}
	if len(objects(doc["functions"])) != 1 || len(objects(doc["types"])) != 1 {
		t.Errorf("functions %d types %d, want 1 and 1", len(objects(doc["functions"])), len(objects(doc["types"])))
	}
	want := `function "fn-a"|function "fn-types"`
	if strings.Join(ignored, "|") != want {
		t.Errorf("ignored = %v, want %s", ignored, want)
	}
}

func TestMergePassDropsATypeWhoseIDIsAFunctionID(t *testing.T) {
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	first := `{"types": [], "functions": [{"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`
	doc, _, _, err := mergePass(base, "greeting", first, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	second := `{"types": [{"id": "fn-a", "package": "x", "file": "internal/x/t.go", "decl": "type T int"}], "functions": []}`
	doc, _, ignored, err := mergePass(doc, "greeting", second, testRunID)
	if err != nil {
		t.Fatalf("a type id that repeats a function id is noise: %v", err)
	}
	if len(objects(doc["types"])) != 1 || len(ignored) != 1 || ignored[0] != `type "fn-a"` {
		t.Errorf("types %d ignored %v", len(objects(doc["types"])), ignored)
	}
}

func TestMergeOutlineDropsATypeWhoseIDIsAComponentID(t *testing.T) {
	text := strings.Replace(okOutline, `"types": [{"id": "name-error"`, `"types": [{"id": "greeting", "package": "x", "file": "internal/x/g.go", "decl": "type G int"}, {"id": "name-error"`, 1)
	doc, _, _, ign, err := mergeOutline(nil, nil, text, testRunID, false)
	if err != nil {
		t.Fatalf("a type id that repeats a component id is noise: %v", err)
	}
	if len(objects(doc["types"])) != 1 || len(ign) != 1 || ign[0] != `type "greeting"` {
		t.Errorf("types %d ignored %v", len(objects(doc["types"])), ign)
	}
}

func TestMergeRepairDropsAFunctionWhoseIDIsATypeID(t *testing.T) {
	base, _, err := parseOutline(fnOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"functions": [{"id": "fn-a", "component": "greeting", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`
	doc, ignored, err := mergeRepair(base, reply, testRunID)
	if err != nil {
		t.Fatalf("noise, not an error: %v", err)
	}
	if len(objects(doc["functions"])) != 0 || len(ignored) != 1 {
		t.Errorf("functions %d ignored %v", len(objects(doc["functions"])), ignored)
	}
}

func TestMergeRepairDropsAnIDTakenByAnotherListBeforeItNeedsAComponent(t *testing.T) {
	base, _, err := parseOutline(fnOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"functions": [{"id": "fn-a", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`
	if _, ignored, err := mergeRepair(base, reply, testRunID); err != nil || len(ignored) != 1 {
		t.Errorf("ignored %v err %v, want the repeat dropped with no component error", ignored, err)
	}
}

func writeState(t *testing.T, dir string, st contractState) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_state"), 0o755); err != nil {
		return err
	}
	return writeJSON((&run{dir: dir}).path(stateContract), st)
}
