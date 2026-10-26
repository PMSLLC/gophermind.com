package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRunFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentPlanStageIsTheOpenOne(t *testing.T) {
	log := "load: started\nload: done\ncontract: started\ncontract: done\ndecompose: started\n"
	if got := currentPlanStage(log); got != "decompose" {
		t.Errorf("got %q", got)
	}
	if got := currentPlanStage("load: started\nload: done\n"); got != "" {
		t.Errorf("all done: got %q", got)
	}
}

func TestProgressCountsComponentsDecomposedAndEnriched(t *testing.T) {
	dir := t.TempDir()
	writeRunFile(t, dir, "contracts.json", `{"components":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"}]}`)
	writeRunFile(t, dir, "_state/decomposed.json", `{"components":{"a":[],"b":[],"c":[]}}`)
	st := &BriefState{Run: BriefRun{Phase: phasePlanning}}
	p := progressOf(dir, st, "decompose: started\n")
	if p == nil || p.Step != "decompose" || p.Done != 3 || p.Total != 4 || p.Percent != 75 {
		t.Errorf("decompose %+v", p)
	}
	writeRunFile(t, dir, "_state/enriched.json", `{"components":{"a":{}}}`)
	p = progressOf(dir, st, "decompose: started\ndecompose: done\nenrich: started\n")
	if p == nil || p.Step != "enrich" || p.Done != 1 || p.Total != 4 || p.Percent != 25 {
		t.Errorf("enrich %+v", p)
	}
}

func TestProgressOfABuildIsVerifiedNodes(t *testing.T) {
	st := &BriefState{Run: BriefRun{Phase: phaseRunning}}
	st.Run.Counts.Nodes = 8
	st.Run.Counts.ByStatus = map[string]int{"verified": 2}
	p := progressOf(t.TempDir(), st, "")
	if p == nil || p.Step != "build" || p.Done != 2 || p.Total != 8 || p.Percent != 25 {
		t.Errorf("build %+v", p)
	}
}

func TestNoProgressWhenNoStepIsOpen(t *testing.T) {
	st := &BriefState{Run: BriefRun{Phase: phasePlanning}}
	if p := progressOf(t.TempDir(), st, "load: started\nload: done\n"); p != nil {
		t.Errorf("got %+v", p)
	}
}

func TestRailHasACollapseToggle(t *testing.T) {
	h := railHTML("runs")
	for _, want := range []string{`id="railtoggle"`, `aria-expanded="true"`, `gm-rail`} {
		if !strings.Contains(h, want) {
			t.Errorf("rail lacks %q", want)
		}
	}
}
