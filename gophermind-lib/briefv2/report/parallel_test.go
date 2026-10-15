package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/report"
)

func TestParallelSectionIsAdditive(t *testing.T) {
	r, err := report.Build(fixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), `"parallel"`) || strings.Contains(r.Summary(), "Parallel:") {
		t.Fatalf("a run with no parallel level carries the new key or line: %s", raw)
	}
	in := fixture()
	in.Parallel = &report.Parallel{Workers: 3, ParallelLevels: 2, LandingConflicts: 1}
	r, err = report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(r)
	if !strings.Contains(string(raw), `"parallel":{"workers":3,"parallel_levels":2,"landing_conflicts":1}`) {
		t.Fatalf("parallel = %s", raw)
	}
	if !strings.Contains(r.Summary(), "Parallel: workers 3, levels 2, landing conflicts 1") || r.SchemaVersion != 3 {
		t.Fatalf("summary or schema: %d\n%s", r.SchemaVersion, r.Summary())
	}
	in.Parallel = &report.Parallel{Workers: 3}
	if r, _ := report.Build(in); r.Parallel != nil {
		t.Fatal("a run with zero parallel levels reports a parallel object")
	}
}
