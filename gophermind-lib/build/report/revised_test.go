package report_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/report"
)

func TestReportListsRevisedLeavesAndCountsRewrites(t *testing.T) {
	in := fixture()
	in.Revised = []report.RevisedLeaf{
		{NodeID: "d", Revisions: 2, Changed: []string{"description", "tests"}, TestsRewritten: true, Refusals: []string{"revise_noop"}},
		{NodeID: "a", Flag: "contract_flagged"},
	}
	r, err := report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rewritten != 1 || len(r.Revised) != 2 || r.Revised[0].NodeID != "a" || r.Revised[1].NodeID != "d" {
		t.Fatalf("Rewritten %d Revised %+v: want 1 and the leaves sorted by id", r.Rewritten, r.Revised)
	}
	sum := r.Summary()
	for _, want := range []string{"Rewritten leaves: 1", "  d: revisions 2, changed description tests, tests rewritten true, flag none, refusals revise_noop", "  a: revisions 0, changed none, tests rewritten false, flag contract_flagged, refusals none"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
	// The proof lines stay last.
	lines := strings.Split(strings.TrimRight(sum, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "Acceptance passed:") || !strings.HasPrefix(lines[len(lines)-2], "Requirements covered:") {
		t.Errorf("the proof lines are not last:\n%s", sum)
	}
}

func TestReportWithoutRevisionsIsUnchanged(t *testing.T) {
	r, err := report.Build(fixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "revised_leaves") || strings.Contains(string(raw), "rewritten_leaves") {
		t.Errorf("a run with no revisions carries the new keys: %s", raw)
	}
	if strings.Contains(r.Summary(), "Rewritten") {
		t.Error("a run with no revisions prints a Rewritten line")
	}
}

func TestReportCleansRevisedLeafText(t *testing.T) {
	in := fixture()
	changed := []string{"description\n"}
	in.Revised = []report.RevisedLeaf{{NodeID: "a\x1b[31m", Revisions: 1, Changed: changed, Flag: strings.Repeat("x", 200)}}
	r, err := report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(r.Revised[0].NodeID+r.Revised[0].Changed[0], "\x1b\n") || len(r.Revised[0].Flag) > 40 {
		t.Errorf("revised leaf = %+v", r.Revised[0])
	}
	if changed[0] != "description\n" {
		t.Error("Build changed the caller's slice")
	}
}

func TestReportWithRevisionsRoundTripsAtSchemaThree(t *testing.T) {
	if report.SchemaVersion != 3 {
		t.Fatalf("SchemaVersion = %d: the revised keys are a shape change, so it is 3", report.SchemaVersion)
	}
	in := fixture()
	in.Revised = []report.RevisedLeaf{{NodeID: "d", Revisions: 1, Changed: []string{"description"}}}
	r, err := report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := report.Write(dir, r, opt(dir)); err != nil {
		t.Fatal(err)
	}
	back, err := report.Read(dir)
	if err != nil || back.SchemaVersion != 3 || back.Rewritten != 1 || len(back.Revised) != 1 {
		t.Fatalf("read back = %+v, %v", back, err)
	}
}

func TestReportCapsTheRewrittenLeavesList(t *testing.T) {
	in := fixture()
	for i := 0; i < report.MaxRevisedListed+5; i++ {
		in.Revised = append(in.Revised, report.RevisedLeaf{NodeID: fmt.Sprintf("n%03d", i), Revisions: 1})
	}
	r, err := report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	sum := r.Summary()
	if !strings.Contains(sum, fmt.Sprintf("Rewritten leaves: %d", report.MaxRevisedListed+5)) || !strings.Contains(sum, "  ... 5 more") ||
		strings.Contains(sum, fmt.Sprintf("n%03d:", report.MaxRevisedListed)) || !strings.Contains(sum, "n000:") {
		t.Errorf("summary:\n%s", sum)
	}
	if len(r.Revised) != report.MaxRevisedListed+5 {
		t.Errorf("the JSON list was cut to %d: only the summary is capped", len(r.Revised))
	}
}
