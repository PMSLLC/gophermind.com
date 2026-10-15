package runview_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview"
	"gophermind/gophermind-lib/runview/runviewtest"
)

func TestRunsStatusPerStage(t *testing.T) {
	cases := []struct {
		stage          runviewtest.Stage
		status, reason string
	}{
		{runviewtest.Planning, "planning", ""},
		{runviewtest.Waiting, "waiting", ""},
		{runviewtest.Planned, "planned", ""},
		{runviewtest.Building, "building", ""},
		{runviewtest.Verified, "verified", ""},
		{runviewtest.Failed, "failed", "leaf_failed"},
		{runviewtest.HarnessFault, "harness_fault", "harness_fault"},
	}
	for _, c := range cases {
		root := t.TempDir()
		runviewtest.Build(t, root, c.stage)
		runs, err := runview.Runs(root)
		if err != nil || len(runs) != 1 {
			t.Fatalf("stage %d: %v, %d runs", c.stage, err, len(runs))
		}
		if runs[0].Status != c.status || runs[0].StopReason != c.reason {
			t.Errorf("stage %d: status %q reason %q, want %q %q", c.stage, runs[0].Status, runs[0].StopReason, c.status, c.reason)
		}
	}
}

func TestRunsNewestFirstAndEmpty(t *testing.T) {
	root := t.TempDir()
	runs, err := runview.Runs(root)
	if err != nil || runs == nil || len(runs) != 0 {
		t.Fatalf("no .gophermind folder: %v, %v (want an empty, non-nil list)", runs, err)
	}
	for _, id := range []string{"gm-2026-10-08-001", "gm-2026-10-09-001", "gm-2026-10-08-002"} {
		runviewtest.BuildID(t, root, id, runviewtest.Planned)
	}
	// Equal update times: the run id decides, highest first.
	for _, id := range []string{"gm-2026-10-08-001", "gm-2026-10-09-001", "gm-2026-10-08-002"} {
		touchRun(t, root, id, runviewtest.T0)
	}
	// Folders that are not runs are ignored: a stray directory and a symlink.
	_ = os.MkdirAll(filepath.Join(root, ".gophermind", "notes"), 0o700)
	_ = os.Symlink(filepath.Join(root, ".gophermind", "gm-2026-10-08-001"), filepath.Join(root, ".gophermind", "gm-2026-10-10-001"))
	runs, err = runview.Runs(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range runs {
		got = append(got, r.RunID)
	}
	want := "gm-2026-10-09-001,gm-2026-10-08-002,gm-2026-10-08-001"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

func TestSummaryOfABuildingRun(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	s := v.Summary()
	if s.Title != "Greeter" || s.RepoName != filepath.Base(root) || s.Status != "building" {
		t.Errorf("summary = %+v", s)
	}
	if s.Waves != 2 || s.Counts.Nodes != 3 {
		t.Errorf("waves %d nodes %d, want 2 and 3", s.Waves, s.Counts.Nodes)
	}
	if s.Counts.ByStatus["verified"] != 1 || s.Counts.ByStatus["in_progress"] != 1 || s.Counts.ByStatus["pending"] != 1 {
		t.Errorf("by_status = %v", s.Counts.ByStatus)
	}
	if s.Coverage.Covered != 1 || s.Coverage.Total != 1 {
		t.Errorf("coverage = %+v", s.Coverage)
	}
	if s.PlannedAt == "" || s.StartedAt == "" || s.UpdatedAt == "" {
		t.Errorf("times = %q %q %q", s.PlannedAt, s.StartedAt, s.UpdatedAt)
	}
	if strings.Contains(s.RepoName, string(filepath.Separator)) {
		t.Errorf("repo_name %q must be a folder name, not a path", s.RepoName)
	}
}

func TestStateNodesInPlanOrder(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	st := v.State()
	if st.SchemaVersion != 1 || st.Run == nil || st.GeneratedAt == "" {
		t.Fatalf("state = %+v", st)
	}
	var ids []string
	byID := map[string]runview.NodeSummary{}
	for _, n := range st.Nodes {
		ids = append(ids, n.ID)
		byID[n.ID] = n
	}
	if got := strings.Join(ids, ","); got != runviewtest.RunID+",greeting,fn-greet,fn-bye,fn-late" {
		t.Errorf("node order = %s", got)
	}
	g := byID["fn-greet"]
	if g.Status != "verified" || g.AttemptCount != 2 || g.LastAttempt == nil || g.LastAttempt.Verdict != "pass" ||
		g.Result == nil || g.Result.Commit != "abc1234" || g.ModelTier != "standard" || g.NodeClass != "pure" ||
		len(g.RequirementIDs) != 1 || g.RequirementIDs[0] != "F1" || g.Tests.Total != 1 {
		t.Errorf("fn-greet = %+v", g)
	}
	b := byID["fn-bye"]
	if b.Status != "in_progress" || b.ClaimedBy != "worker-1" || b.AttemptCount != 1 || b.LastAttempt == nil ||
		b.LastAttempt.Verdict != "fail" || b.LastAttempt.FailureReason == "" || len(b.DependsOn) != 1 || b.Wave != 1 {
		t.Errorf("fn-bye = %+v", b)
	}
	l := byID["fn-late"]
	if l.Status != "pending" || l.LastAttempt != nil || l.Result != nil || l.DependsOn == nil || l.RequirementIDs == nil {
		t.Errorf("fn-late = %+v (lists must be empty, never null)", l)
	}
	raw, _ := os.ReadFile(filepath.Join(f.Dir, "_state", "events.jsonl"))
	if st.EventSeq != int64(bytes.Count(raw, []byte("\n"))) || st.EventSeq == 0 {
		t.Errorf("event_seq = %d, log has %d lines", st.EventSeq, bytes.Count(raw, []byte("\n")))
	}
}

func TestStateOfAPlanWithoutRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	v, _ := runview.Open(root, runviewtest.RunID)
	st := v.State()
	if st.EventSeq != 0 || len(st.Nodes) != 5 {
		t.Fatalf("event_seq %d, %d nodes", st.EventSeq, len(st.Nodes))
	}
	for _, n := range st.Nodes {
		if n.Status != "pending" {
			t.Errorf("%s status %q, want the plan file's pending", n.ID, n.Status)
		}
	}
}

func TestStateSurvivesDamagedFiles(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	if err := os.WriteFile(filepath.Join(f.Dir, "greeting", "fn-bye.runtime.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	st := v.State()
	var bye runview.NodeSummary
	for _, n := range st.Nodes {
		if n.ID == "fn-bye" {
			bye = n
		}
	}
	if bye.Status != "pending" {
		t.Errorf("a damaged runtime file falls back to the plan status, got %q", bye.Status)
	}
	if len(st.Warnings) == 0 || !strings.Contains(strings.Join(st.Warnings, "|"), "fn-bye") {
		t.Errorf("warnings = %v, want one naming fn-bye", st.Warnings)
	}
	// A damaged plan file leaves an empty node list and a warning, not a failure.
	if err := os.WriteFile(filepath.Join(f.Dir, "greeting", "fn-late.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	st = v.State()
	if len(st.Nodes) != 0 || !strings.Contains(strings.Join(st.Warnings, "|"), "plan") {
		t.Errorf("nodes %d, warnings %v", len(st.Nodes), st.Warnings)
	}
}

func TestUnreadableReportIsFailedNotAPanic(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	if err := os.WriteFile(filepath.Join(f.Dir, "report.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	s := v.Summary()
	if s.Status != "failed" || s.StopReason != "report_unreadable" {
		t.Errorf("summary = %q %q", s.Status, s.StopReason)
	}
}

func TestStateAndRunsNeverWrite(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Building)
	before := runviewtest.Digest(t, root)
	if _, err := runview.Runs(root); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	_ = v.State()
	_ = v.Summary()
	_ = v.EventSeq()
	if runviewtest.Digest(t, root) != before {
		t.Fatal("a reader changed the run folder")
	}
}

func TestOpenRefusesBadIDs(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	for _, id := range []string{"..", "../gm-2026-10-08-001", "gm-2026-10-08-001/../..", "x"} {
		if _, err := runview.Open(root, id); err == nil {
			t.Errorf("Open(%q) succeeded", id)
		}
	}
}

// touchRun sets every file the view stats to at (a test may write; the view may not).
func touchRun(t *testing.T, root, id string, at time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".gophermind", id)
	for _, rel := range []string{"brief.md", "approval.json", "report.json", "_state/events.jsonl", "_state/status.json", "_state/executor.json"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Lstat(p); err == nil {
			if err := os.Chtimes(p, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNewestRunIsLatestUpdatedNotHighestID(t *testing.T) {
	root := t.TempDir()
	if got := runview.NewestRunIDForTest(root); got != "" {
		t.Errorf("no runs: newest = %q", got)
	}
	runviewtest.BuildID(t, root, "gm-2026-10-09-001", runviewtest.Planned)
	runviewtest.BuildID(t, root, "gm-2026-09-29-002", runviewtest.Planned)
	touchRun(t, root, "gm-2026-10-09-001", runviewtest.T0)
	touchRun(t, root, "gm-2026-09-29-002", runviewtest.T0.Add(time.Hour))
	runs, err := runview.Runs(root)
	if err != nil || len(runs) != 2 || runs[0].RunID != "gm-2026-09-29-002" {
		t.Fatalf("runs = %+v, %v: the lower id updated later must be first", runs, err)
	}
	if got := runview.NewestRunIDForTest(root); got != "gm-2026-09-29-002" {
		t.Errorf("newest = %q", got)
	}
}

func TestProjectRunStoppedInPlanning(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.PlanFailed)
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	s := v.Summary()
	if s.Status != "failed" || s.StopReason != "plan:coverage" {
		t.Errorf("summary = %q %q", s.Status, s.StopReason)
	}
	raw, _ := json.Marshal(v.State())
	if strings.Contains(string(raw), "FAKE_SECRET") || strings.Contains(string(raw), root) {
		t.Errorf("state leaks project.json content: %s", raw)
	}
}

func TestGroupNodeStatusIsDerivedFromChildren(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Building)
	status := func() map[string]string {
		v, _ := runview.Open(root, runviewtest.RunID)
		m := map[string]string{}
		for _, n := range v.State().Nodes {
			m[n.ID] = n.Status
		}
		return m
	}
	m := status()
	if m["greeting"] != "in_progress" || m[runviewtest.RunID] != "in_progress" {
		t.Errorf("verified+in_progress+pending: component %q root %q, want in_progress", m["greeting"], m[runviewtest.RunID])
	}
	runviewtest.Verify(t, f, "fn-late")
	if m := status(); m["greeting"] != "in_progress" {
		t.Errorf("fn-bye still in progress: component %q", m["greeting"])
	}
}

func TestGroupStatusAllVerifiedAndWorstChild(t *testing.T) {
	for _, c := range []struct {
		stage runviewtest.Stage
		want  string
	}{{runviewtest.Verified, "verified"}, {runviewtest.Failed, "failed"}} {
		root := t.TempDir()
		runviewtest.Build(t, root, c.stage)
		v, _ := runview.Open(root, runviewtest.RunID)
		for _, n := range v.State().Nodes {
			if (n.Kind == "component" || n.Kind == "root") && n.Status != c.want {
				t.Errorf("stage %d: %s status %q, want %q", c.stage, n.ID, n.Status, c.want)
			}
		}
	}
}

func TestResumedRunIgnoresStalePlanFailure(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.PlanFailed)
	p := filepath.Join(root, ".gophermind", runviewtest.RunID, "_state", "executor.json")
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if s := v.Summary(); s.Status == "failed" || s.StopReason == "plan:coverage" {
		t.Errorf("summary = %q %q, want the stale planning failure ignored", s.Status, s.StopReason)
	}
}

func TestStampIgnoresSymlinkedFiles(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.Planned)
	touchRun(t, root, runviewtest.RunID, runviewtest.T0)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := runviewtest.T0.Add(48 * time.Hour)
	if err := os.Chtimes(target, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".gophermind", runviewtest.RunID, "report.json")); err != nil {
		t.Fatal(err)
	}
	v, err := runview.Open(root, runviewtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Summary().UpdatedAt; strings.Contains(got, future.Format("2006-01-02")) {
		t.Errorf("updated_at = %s, leaked the symlink target mtime", got)
	}
}

func TestPlanFailedStampsProjectJSON(t *testing.T) {
	root := t.TempDir()
	runviewtest.Build(t, root, runviewtest.PlanFailed)
	touchRun(t, root, runviewtest.RunID, runviewtest.T0)
	later := runviewtest.T0.Add(time.Hour)
	p := filepath.Join(root, ".gophermind", runviewtest.RunID, "_state", "project.json")
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	v, _ := runview.Open(root, runviewtest.RunID)
	if got, want := v.Summary().UpdatedAt, later.UTC().Format(time.RFC3339); !strings.HasPrefix(got, want[:16]) {
		t.Errorf("updated_at = %s, want about %s", got, want)
	}
}

func TestRunsErrorHasNoPath(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, ".gophermind")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(base, 0o700)
	if _, err := os.ReadDir(base); err == nil {
		t.Skip("directory permissions are not enforced here")
	}
	_, err := runview.Runs(root)
	if err == nil {
		t.Fatal("want an error for an unreadable run folder")
	}
	if strings.Contains(err.Error(), root) {
		t.Errorf("error leaks a path: %v", err)
	}
}
