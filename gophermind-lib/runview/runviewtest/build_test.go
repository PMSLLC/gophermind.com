package runviewtest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/tree"
)

func TestBuildProducesLoadablePlansAtEveryStage(t *testing.T) {
	for _, st := range []Stage{Planning, Waiting, Planned, Building, Verified, Failed, HarnessFault, PlanFailed} {
		root := t.TempDir()
		f := Build(t, root, st)
		if f.RunID != RunID || filepath.Base(f.Dir) != RunID {
			t.Fatalf("stage %d: fixture = %+v", st, f)
		}
		if _, err := os.Stat(filepath.Join(f.Dir, "brief.md")); err != nil {
			t.Fatalf("stage %d: %v", st, err)
		}
		tr, err := tree.NewStore(f.Dir).Load()
		if err != nil {
			t.Fatalf("stage %d: plan does not load: %v", st, err)
		}
		want := 5 // root, one component, three functions
		if st == Planning || st == Waiting {
			want = 0
		}
		if len(tr.Nodes) != want {
			t.Errorf("stage %d: %d nodes, want %d", st, len(tr.Nodes), want)
		}
	}
}

func TestBuildWritesRuntimeAndArtifacts(t *testing.T) {
	root := t.TempDir()
	f := Build(t, root, Building)
	for _, p := range []string{
		"greeting/fn-greet.runtime.json", "greeting/fn-bye.runtime.json", "greeting/fn-late.runtime.json",
		"_state/events.jsonl", "_state/calls.jsonl", "_state/executor.json", "approval.json",
		"attempts/fn-bye/1/check-output.txt", "attempts/fn-bye/1/attempt.json",
	} {
		if _, err := os.Stat(filepath.Join(f.Dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "report.json")); err == nil {
		t.Error("a building run must not have report.json")
	}
	v := Build(t, t.TempDir(), Verified)
	if _, err := os.Stat(filepath.Join(v.Dir, "report.json")); err != nil {
		t.Errorf("verified run has no report.json: %v", err)
	}
}

func TestDigestSeesAWrite(t *testing.T) {
	f := Build(t, t.TempDir(), Planned)
	before := Digest(t, f.Dir)
	if Digest(t, f.Dir) != before {
		t.Fatal("digest is not stable")
	}
	if err := os.WriteFile(filepath.Join(f.Dir, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Digest(t, f.Dir) == before {
		t.Fatal("digest did not change after a write")
	}
}

func TestPlanFailedHasProjectJSONAndNoApprovalOrReport(t *testing.T) {
	f := Build(t, t.TempDir(), PlanFailed)
	for _, p := range []string{"approval.json", "report.json"} {
		if _, err := os.Stat(filepath.Join(f.Dir, p)); err == nil {
			t.Errorf("%s must be absent", p)
		}
	}
	raw, err := os.ReadFile(filepath.Join(f.Dir, "_state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["status"] != "failed" || m["stop_reason"] != "plan:coverage" {
		t.Errorf("project.json = %v", m)
	}
	if d, _ := m["run_dir"].(string); !filepath.IsAbs(d) {
		t.Errorf("run_dir = %v", m["run_dir"])
	}
	if s, _ := m["secrets"].([]any); len(s) == 0 {
		t.Error("secrets missing")
	}
}

func TestBuildingHasARevisionDecision(t *testing.T) {
	f := Build(t, t.TempDir(), Building)
	raw, err := os.ReadFile(filepath.Join(f.Dir, "_state", "revisions", "decisions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal(raw, &st); err != nil || len(st.Decisions) != 1 {
		t.Fatalf("decisions = %s, err %v", raw, err)
	}
	if st.Decisions[0]["id"] != "rv-fn-bye-1" {
		t.Errorf("id = %v", st.Decisions[0]["id"])
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "decisions", "rv-fn-bye-1.md")); err != nil {
		t.Error(err)
	}
}

func TestVerifiedReportCarriesRevisionAndParallel(t *testing.T) {
	f := Build(t, t.TempDir(), Verified)
	raw, err := os.ReadFile(filepath.Join(f.Dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Revised   []map[string]any `json:"revised_leaves"`
		Rewritten int              `json:"rewritten_leaves"`
		Parallel  struct {
			Workers        int `json:"workers"`
			ParallelLevels int `json:"parallel_levels"`
		} `json:"parallel"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Revised) != 1 || r.Revised[0]["revisions"] != float64(1) || r.Rewritten != 1 || r.Parallel.Workers != 2 || r.Parallel.ParallelLevels != 1 {
		t.Errorf("report = %s", raw)
	}
}

func TestSetRevisionOnABuildingNodeLogsARevisionLine(t *testing.T) {
	f := Build(t, t.TempDir(), Building)
	if err := f.BB.SetRevision(context.Background(), f.RunID, "fn-late", 1); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.Dir, "_state", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, `"revision"`) && strings.Contains(l, "fn-late") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("revision lines = %d in %s", n, raw)
	}
}
