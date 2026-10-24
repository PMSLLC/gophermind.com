package serve

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/report"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func checksOf(st BriefState) map[int]string {
	out := map[int]string{}
	for _, w := range st.WaveChecks {
		out[w.Wave] = w.Check
	}
	return out
}

func TestWaveChecksPerStage(t *testing.T) {
	cases := []struct {
		name    string
		stage   runviewtest.Stage
		want    map[int]string
		failing []string // failing leaves of the last wave
	}{
		{"verified", runviewtest.Verified, map[int]string{0: "pass", 1: "pass"}, nil},
		{"building", runviewtest.Building, map[int]string{0: "pass", 1: "running"}, nil},
		{"failed", runviewtest.Failed, map[int]string{0: "pass", 1: "fail"}, []string{"fn-bye"}},
		{"planned", runviewtest.Planned, map[int]string{0: "pending", 1: "pending"}, nil},
	}
	for _, c := range cases {
		root := t.TempDir()
		f := runviewtest.Build(t, root, c.stage)
		st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
		if err != nil {
			t.Fatal(err)
		}
		if got := checksOf(st); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: checks %v, want %v", c.name, got, c.want)
		}
		last := st.WaveChecks[len(st.WaveChecks)-1]
		if len(last.FailingLeaves) != len(c.failing) || (len(c.failing) > 0 && last.FailingLeaves[0] != c.failing[0]) {
			t.Errorf("%s: failing leaves %v, want %v", c.name, last.FailingLeaves, c.failing)
		}
		if len(st.Repairs.Open) != 0 {
			t.Errorf("%s: repairs %+v", c.name, st.Repairs)
		}
	}
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	st, _ := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if w := st.WaveChecks[1]; !reflect.DeepEqual(w.Leaves, []string{"fn-bye", "fn-late"}) || w.Verified != 2 {
		t.Errorf("wave 1 %+v", w)
	}
}

func TestWaveCheckRepairingAndRepairs(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	// Wave 0's integration check failed after wave 1 had started: fn-greet was
	// reopened (the executor records the reason in leaf-results.json).
	if err := f.BB.SetStatus(context.Background(), f.RunID, "fn-greet", blackboard.StatusVerified, blackboard.StatusNeedsRevision); err != nil {
		t.Fatal(err)
	}
	lr := `{"fn-greet":{"status":"needs_revision","reason":"integration: build"}}`
	if err := os.WriteFile(filepath.Join(f.Dir, "_state", "leaf-results.json"), []byte(lr), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	w0 := st.WaveChecks[0]
	if w0.Check != "repairing" || !reflect.DeepEqual(w0.FailingLeaves, []string{"fn-greet"}) {
		t.Errorf("wave 0 %+v", w0)
	}
	if len(st.Repairs.Open) != 1 {
		t.Fatalf("repairs %+v", st.Repairs)
	}
	r := st.Repairs.Open[0]
	if r.Leaf != "fn-greet" || r.Wave != 0 || r.Attempts != 2 || r.Reason != "integration: build" || r.Status != "needs_revision" {
		t.Errorf("repair %+v", r)
	}
}

func TestRepairTotalFromReportAndStopOnIntegration(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	rep, err := report.Read(f.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rep.Repairs = 2
	if err := report.Write(f.Dir, rep, report.WriteOptions{RepoRoot: root}); err != nil {
		t.Fatal(err)
	}
	st, _ := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if st.Repairs.Total != 2 || st.Repairs.Open == nil {
		t.Errorf("repairs %+v", st.Repairs)
	}

	// A run that stopped on a wave's integration check with every leaf of the
	// wave verified: that wave's check failed.
	root = t.TempDir()
	f = runviewtest.Build(t, root, runviewtest.Verified)
	runviewtest.WriteReport(t, f, "failed", "repair_failed", 1, report.NodeCounts{Total: 3, Verified: 3})
	st, _ = BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if got := checksOf(st); !reflect.DeepEqual(got, map[int]string{0: "pass", 1: "fail"}) {
		t.Errorf("checks %v", got)
	}
}
