package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/runview/runviewtest"
)

var stateNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func nodeByID(t *testing.T, st BriefState, id string) BriefNode {
	t.Helper()
	for _, n := range st.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %s in %+v", id, st.Nodes)
	return BriefNode{}
}

func TestBuildBriefStateVerifiedRun(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	if st.SchemaVersion != BriefStateVersion || st.GeneratedAt != "2026-10-23T12:00:00Z" {
		t.Errorf("header %+v", st)
	}
	if st.Run.Status != "verified" || st.Run.Title != "Greeter" || st.Run.Waves != 2 {
		t.Errorf("run %+v", st.Run)
	}
	if len(st.Nodes) != 5 {
		t.Fatalf("%d nodes, want root + component + 3 leaves", len(st.Nodes))
	}
	g := nodeByID(t, st, "fn-greet")
	if g.Status != "verified" || g.RawStatus != "verified" || g.Parent != "greeting" || g.Wave != 0 {
		t.Errorf("fn-greet %+v", g)
	}
	if len(g.Attempts) != 2 || g.Attempts[0].Outcome != "fail" || g.Attempts[1].Outcome != "pass" ||
		g.Attempts[0].Model != "qwen3.6" || g.Attempts[0].Provider != "mini" || g.Attempts[0].FailureReason == "" {
		t.Errorf("fn-greet attempts %+v", g.Attempts)
	}
	if len(g.RequirementIDs) != 1 || g.RequirementIDs[0] != "F1" {
		t.Errorf("requirement ids %v", g.RequirementIDs)
	}
	b := nodeByID(t, st, "fn-bye")
	if b.Wave != 1 || len(b.DependsOn) != 1 || b.DependsOn[0] != "fn-greet" {
		t.Errorf("fn-bye %+v", b)
	}
	if g.Origin != "" || b.Origin != "" {
		t.Errorf("origin must be empty by default: %q %q", g.Origin, b.Origin)
	}
	if nodeByID(t, st, "greeting").Status != "verified" {
		t.Errorf("component %+v", nodeByID(t, st, "greeting"))
	}
	// The root node carries one settled question (the plan's decision record).
	if st.Acceptance != nil {
		t.Errorf("no acceptance.json yet, got %+v", st.Acceptance)
	}
}

func TestBuildBriefStateStatusesPerStage(t *testing.T) {
	cases := []struct {
		stage runviewtest.Stage
		want  map[string]string
	}{
		{runviewtest.Planned, map[string]string{"fn-greet": "pending", "fn-bye": "pending"}},
		{runviewtest.Building, map[string]string{"fn-greet": "verified", "fn-bye": "running", "fn-late": "pending"}},
		{runviewtest.Failed, map[string]string{"fn-greet": "verified", "fn-bye": "failed", "fn-late": "pending"}},
	}
	for _, c := range cases {
		root := t.TempDir()
		f := runviewtest.Build(t, root, c.stage)
		st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
		if err != nil {
			t.Fatal(err)
		}
		for id, want := range c.want {
			if got := nodeByID(t, st, id).Status; got != want {
				t.Errorf("stage %d: %s = %s, want %s", c.stage, id, got, want)
			}
		}
	}
}

func TestMarkBlocked(t *testing.T) {
	nodes := []BriefNode{
		{ID: "a", Status: "failed"},
		{ID: "b", Status: "pending", DependsOn: []string{"a"}},
		{ID: "c", Status: "pending", DependsOn: []string{"b"}},
		{ID: "d", Status: "pending", DependsOn: []string{"e"}},
		{ID: "e", Status: "verified"},
		{ID: "f", Status: "running", DependsOn: []string{"a"}},
	}
	markBlocked(nodes)
	want := map[string]string{"a": "failed", "b": "blocked", "c": "blocked", "d": "pending", "e": "verified", "f": "running"}
	for _, n := range nodes {
		if n.Status != want[n.ID] {
			t.Errorf("%s = %s, want %s", n.ID, n.Status, want[n.ID])
		}
	}
}

func TestBuildBriefStateQuestionsAndAcceptance(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Verified)
	write := func(rel, body string) {
		p := filepath.Join(f.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("_state/clarify/questions.json", `{"calls":1,"round":1,"complete":true,"questions":[
	 {"id":"q1","question":"Which store?","kind":"decision","round":1,"status":"settled","answer":"memory","answered_by":"accepted"},
	 {"id":"q2","question":"Which port?","kind":"decision","round":1,"status":"open"}]}`)
	write("acceptance.json", `{"schema_version":2,"complete":true,"round":1,
	 "acceptance":{"passed":1,"total":2},"constraints_checked":{"passed":1,"total":1},
	 "bullets":[{"requirement":"A1","text":"greets","verdict":"pass"},{"requirement":"A2","text":"leaves","verdict":"fail"}],
	 "constraints":[{"requirement":"C1","text":"no net","verdict":"pass"}]}`)
	write("UNDERSTANDING.md", "# Understanding\n\nA greeter.\n")

	st, err := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Questions) != 2 || st.Questions[0].AnsweredBy != "accepted" || st.Questions[0].Answer != "memory" || st.Questions[1].Status != "open" {
		t.Errorf("questions %+v", st.Questions)
	}
	a := st.Acceptance
	if a == nil || !a.Complete || a.Acceptance.Passed != 1 || a.Acceptance.Total != 2 || len(a.Bullets) != 3 ||
		a.Bullets[1].Verdict != "fail" || a.Bullets[2].Kind != "constraint" {
		t.Errorf("acceptance %+v", a)
	}
	if !strings.Contains(st.Understanding, "A greeter.") {
		t.Errorf("understanding %q", st.Understanding)
	}
}

func TestBuildBriefStateLegacyAnswers(t *testing.T) {
	root := t.TempDir()
	f := runviewtest.Build(t, root, runviewtest.Planned)
	body := `{"answers":[{"id":"q1","question":"Port?","answer":"8080","assumed":true},{"id":"q2","question":"Db?","answer":"none"}]}`
	if err := os.WriteFile(filepath.Join(f.Dir, "answers.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := BuildBriefState(root, f.RunID, nil, false, "", stateNow)
	if len(st.Questions) != 2 || st.Questions[0].AnsweredBy != "unattended-default" || st.Questions[1].AnsweredBy != "human" {
		t.Errorf("questions %+v", st.Questions)
	}
}

func TestBuildBriefStateRecordOnlyAndMissing(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildBriefState(root, "gm-2026-10-23-001", nil, false, "", stateNow); err != ErrBriefNotFound {
		t.Fatalf("missing: %v", err)
	}
	rec := &briefRecord{ID: "gm-2026-10-23-001", Title: "T", Phase: phasePlanning, StartedAt: "2026-10-23T11:00:00Z"}
	st, err := BuildBriefState(root, rec.ID, rec, true, "one\ntwo\n", stateNow)
	if err != nil {
		t.Fatal(err)
	}
	if st.Run.Phase != "planning" || !st.Run.Active || st.Run.Title != "T" || len(st.Nodes) != 0 || len(st.LogTail) != 2 {
		t.Errorf("state %+v", st)
	}
	if st.Nodes == nil || st.Questions == nil {
		t.Error("empty lists must be non-nil so the JSON has [] not null")
	}
}

func TestTailLinesBounds(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("line\n")
	}
	if got := tailLines(b.String()); len(got) != maxLogTailLines {
		t.Errorf("%d lines, want %d", len(got), maxLogTailLines)
	}
	big := strings.Repeat("x", maxLogTailBytes*2)
	got := tailLines(big)
	if len(got) != 1 || len(got[0]) > maxLogTailBytes {
		t.Errorf("byte cap not applied: %d lines, %d bytes", len(got), len(got[0]))
	}
	if len(tailLines("")) != 0 {
		t.Error("empty text gives no lines")
	}
}
