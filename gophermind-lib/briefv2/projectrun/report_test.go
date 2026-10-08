package projectrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/tree"
)

func sampleExecutor() *report.Report {
	return &report.Report{
		SchemaVersion: 2, RunID: "r1", Status: "complete", Sandbox: "off",
		Requirements: report.Coverage{Covered: 7, Total: 7},
		Acceptance:   report.Passed{Passed: 2, Total: 2},
		Waves:        3,
		Nodes:        report.NodeCounts{Total: 5, Verified: 5},
	}
}

func sampleReport() *ProjectReport {
	return &ProjectReport{
		RunID: "r1", Title: "Greeter", RunDir: ".gophermind/r1",
		Binary:    BinaryInfo{Path: "/usr/local/bin/gophermind", Version: "0.9.1", Commit: "abc1234", Date: "2026-10-08"},
		Mode:      "unattended",
		Repo:      RepoInfo{Path: "/work/repo", BriefRepo: "git@example.com:x/y.git", BaseBranch: "main", HeadAtStart: "deadbeef"},
		Preflight: []Check{{Name: "git", OK: true, Detail: "ok"}, {Name: "disk", OK: true, Detail: "ok"}},
		Providers: []ProviderInfo{{Name: "mini", Host: "baby-jesus.local"}},
		Secrets:   []Provisioned{{Name: "DATABASE_URL", Source: SourceVault}, {Name: "JWT_SECRET", Source: SourceHex32}},
		Ambiguity: AmbiguityInfo{
			BriefSetting: "halt", Effective: "unattended", MilestoneApprovals: true,
			ClarifyDefaulted: []ClarifyDefault{{ID: "q1", Question: "CANARY-QUESTION?", Answer: "CANARY-ANSWER"}, {ID: "q2", Question: "other?", Answer: "yes"}},
			ByAnsweredBy:     map[string]int{"unattended-default": 2, "probe": 1},
			Rounds:           2, ClarifyCalls: 3, ConservativeAssumptions: 1,
		},
		Understanding:       UnderstandingInfo{ConfirmedBy: "unattended", Hash: "0123456789abcdef0123"},
		Approval:            ApprovalInfo{By: "unattended", PlanHash: "aaaaaaaaaaaabbbb", UnderstandingHash: "0123456789abcdef0123"},
		Plan:                PlanInfo{Functions: 5, Waves: 3, RequirementsCovered: report.Coverage{Covered: 7, Total: 7}, AcceptanceTotal: 2},
		Warnings:            Counts{LeafDefaulted: 1, DocDefaulted: 2, LeafNormalized: 3, OutlineIDNormalized: 4, DuplicatesIgnored: 5},
		PlannerWarningLines: []string{"duplicate id a/f1 ignored", "5 duplicate ids ignored in all"},
		Stages:              []StageInfo{{Name: "clarify", Status: "done"}},
		ByNodeClass: []ClassStat{
			{Class: "pure", Leaves: 3, Verified: 3, FirstTryWins: 2, Attempts: 4, Passes: 3},
			{Class: "handler", Leaves: 2, Verified: 2, FirstTryWins: 2, Attempts: 2, Passes: 2},
		},
		Executor: sampleExecutor(),
		Status:   "complete", ExitCode: 0,
		StartedAt: "2026-10-08T10:00:00Z", FinishedAt: "2026-10-08T10:30:00Z",
	}
}

func lines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }

func TestReportCarriesBinaryVersion(t *testing.T) {
	p := sampleReport()
	txt := p.Text()
	for _, w := range []string{"gophermind 0.9.1 (commit abc1234, built 2026-10-08)", "binary: /usr/local/bin/gophermind"} {
		if !strings.Contains(txt, w) {
			t.Errorf("text lacks %q", w)
		}
	}
	raw, _ := json.Marshal(p)
	for _, w := range []string{`"version":"0.9.1"`, `"commit":"abc1234"`, `"date":"2026-10-08"`, `"path":"/usr/local/bin/gophermind"`} {
		if !strings.Contains(string(raw), w) {
			t.Errorf("json lacks %q", w)
		}
	}
	if lines(txt)[0] != "gophermind 0.9.1 (commit abc1234, built 2026-10-08)" || lines(txt)[1] != "binary: /usr/local/bin/gophermind" {
		t.Errorf("binary lines are not first: %q", lines(txt)[:2])
	}
}

func TestPrintedReportHasNoModelText(t *testing.T) {
	p := sampleReport()
	raw, _ := json.Marshal(p)
	for _, c := range []string{"CANARY-QUESTION", "CANARY-ANSWER"} {
		if !strings.Contains(string(raw), c) {
			t.Errorf("json lacks %s", c)
		}
		if strings.Contains(p.Text(), c) {
			t.Errorf("text carries %s", c)
		}
	}
	if !strings.Contains(p.Text(), "q1, q2") {
		t.Errorf("clarify ids missing from text:\n%s", p.Text())
	}
}

func TestPrintedReportEndsWithProofLines(t *testing.T) {
	ls := lines(sampleReport().Text())
	n := len(ls)
	if ls[n-2] != "Requirements covered: 7 of 7" || ls[n-1] != "Acceptance passed: 2 of 2" {
		t.Fatalf("tail: %q", ls[n-2:])
	}
}

func TestProofLinesWhenExecutorDidNotRun(t *testing.T) {
	p := sampleReport()
	p.Executor, p.ByNodeClass = nil, nil
	p.Status, p.StopReason, p.ExitCode = "stopped", "plan:coverage", 3
	p.Plan.RequirementsCovered = report.Coverage{Covered: 0, Total: 7}
	p.Plan.AcceptanceTotal = 2
	ls := lines(p.Text())
	n := len(ls)
	if ls[n-2] != "Requirements covered: 0 of 7" || ls[n-1] != "Acceptance passed: 0 of 2" {
		t.Fatalf("tail: %q", ls[n-2:])
	}
	if ls[n-3] != "stopped: stopped plan:coverage" {
		t.Fatalf("stop line: %q", ls[n-3])
	}
}

func TestMilestoneLine(t *testing.T) {
	const want = "milestone_approvals: declared; the executor has no milestone gate; covered by the unattended plan approval"
	p := sampleReport()
	if !strings.Contains(p.Text(), want+"\n") {
		t.Errorf("missing:\n%s", p.Text())
	}
	p.Ambiguity.MilestoneApprovals = false
	if strings.Contains(p.Text(), "milestone_approvals") {
		t.Error("line present when not declared")
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	root, err := tree.ParseNode([]byte(`{"id":"root","kind":"root","children":[],"depends_on":[]}`))
	if err == nil {
		_ = tree.NewStore(dir).Write(root)
	}
	p := sampleReport()
	if err := WriteReport(dir, p); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "_state", "project.json")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "project.json")); err == nil {
		t.Error("project.json at the run root")
	}
	if _, err := tree.NewStore(dir).Load(); err != nil {
		t.Errorf("tree.Store.Load with the report present: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var back ProjectReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&back, p) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", back, *p)
	}
	for _, s := range back.Secrets {
		if s.Name == "" || s.Source == "" {
			t.Errorf("secret entry %+v", s)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "_state", ".tmp-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestReportCarriesNodeClassTable(t *testing.T) {
	p := sampleReport()
	raw, _ := json.Marshal(p)
	if !strings.Contains(string(raw), `"by_node_class":[{"class":"pure"`) {
		t.Errorf("json: %s", raw)
	}
	if !strings.Contains(p.Text(), "node class pure: 3 leaves, 3 verified, 2 first try, 4 attempts\n") {
		t.Errorf("text:\n%s", p.Text())
	}
	p.ByNodeClass = nil
	if !strings.Contains(p.Text(), "node classes: executor did not run\n") {
		t.Errorf("empty case:\n%s", p.Text())
	}
}

func TestPrintedReportNodeClassLines(t *testing.T) {
	p := sampleReport()
	n := 0
	for _, l := range lines(p.Text()) {
		if strings.HasPrefix(l, "node class ") {
			n++
			if strings.Contains(l, "/") {
				t.Errorf("node id in %q", l)
			}
		}
	}
	if n != 2 {
		t.Fatalf("%d class lines", n)
	}
	if !strings.Contains(p.Text(), "node class handler: 2 leaves, 2 verified, 2 first try, 2 attempts\n") {
		t.Error("handler line")
	}
}

func TestReportCarriesUnderstanding(t *testing.T) {
	p := sampleReport()
	if !strings.Contains(p.Text(), "understanding: confirmed by unattended, hash 0123456789ab\n") {
		t.Errorf("text:\n%s", p.Text())
	}
	raw, _ := json.Marshal(p)
	if !strings.Contains(string(raw), `"confirmed_by":"unattended"`) || !strings.Contains(string(raw), `"hash":"0123456789abcdef0123"`) {
		t.Errorf("json: %s", raw)
	}
	if !strings.Contains(p.Text(), "approval: unattended, plan aaaaaaaaaaaa, understanding 0123456789ab\n") {
		t.Errorf("approval line:\n%s", p.Text())
	}
}

func TestReportCarriesPlannerWarnings(t *testing.T) {
	p := sampleReport()
	raw, _ := json.Marshal(p)
	for _, w := range []string{`"duplicates_ignored":5`, `"leaf_defaulted":1`, `"planner_warning_lines":["duplicate id a/f1 ignored"`} {
		if !strings.Contains(string(raw), w) {
			t.Errorf("json lacks %s", w)
		}
	}
	want := "planner warnings: duplicates ignored 5, leaf_defaulted 1, doc_defaulted 2, leaf_normalized 3, outline_id_normalized 4\n"
	if !strings.Contains(p.Text(), want) {
		t.Errorf("text:\n%s", p.Text())
	}
}

func TestReportRecordsAnsweringHost(t *testing.T) {
	p := sampleReport()
	p.Providers = []ProviderInfo{{Name: "mini", Host: "http://user:pw@baby-jesus.local:8080/v1?k=SECRETKEY"}}
	txt := p.Text()
	if !strings.Contains(txt, "model server: mini answered on baby-jesus.local\n") {
		t.Errorf("text:\n%s", txt)
	}
	if strings.Contains(txt, "SECRETKEY") || strings.Contains(txt, "pw@") || strings.Contains(txt, "/v1") {
		t.Errorf("URL parts leaked:\n%s", txt)
	}
	p.Providers = []ProviderInfo{{Name: "mini", Host: "baby-jesus.local"}}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "http") {
		t.Errorf("json holds a URL: %s", raw)
	}
}

func TestGradedInvalidLine(t *testing.T) {
	p := sampleReport()
	p.Graded = true
	if strings.Contains(p.Text(), "graded: INVALID") {
		t.Error("line present without a resume")
	}
	p.Resumed = true
	p.Executor.Resumed = true
	ls := lines(p.Text())
	idx := -1
	for i, l := range ls {
		if l == "graded: INVALID (the run resumed)" {
			idx = i
		}
	}
	if idx < 0 || !strings.HasPrefix(ls[idx+1], "Run r1: complete") {
		t.Fatalf("line missing or not before the executor block:\n%s", p.Text())
	}
	p.Graded = false
	if strings.Contains(p.Text(), "graded: INVALID") {
		t.Error("line present when not graded")
	}
}

func TestAmbiguityLineWithoutDefaultsHasNoParentheses(t *testing.T) {
	p := sampleReport()
	p.Ambiguity.ClarifyDefaulted = nil
	txt := p.Text()
	if !strings.Contains(txt, "0 clarify question(s) answered by their recommendations in 2 round(s)") {
		t.Errorf("text:\n%s", txt)
	}
	if strings.Contains(txt, "recommendations (") || strings.Contains(txt, "()") {
		t.Errorf("empty parentheses:\n%s", txt)
	}
}

func TestProjectJSONFieldsAreCapped(t *testing.T) {
	long := strings.Repeat("é", 5000) + "\nsecond line"
	p := sampleReport()
	p.Title = long
	p.PlannerWarningLines = []string{long}
	p.Ambiguity.ClarifyDefaulted = []ClarifyDefault{{ID: long, Question: long, Answer: long}}
	dir := t.TempDir()
	if err := WriteReport(dir, p); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "_state", "project.json"))
	var back ProjectReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	cd := back.Ambiguity.ClarifyDefaulted[0]
	for name, s := range map[string]string{"title": back.Title, "line": back.PlannerWarningLines[0], "id": cd.ID, "question": cd.Question, "answer": cd.Answer} {
		if n := utf8.RuneCountInString(s); n != maxFieldRunes {
			t.Errorf("%s has %d runes, want %d", name, n, maxFieldRunes)
		}
		if strings.ContainsAny(s, "\r\n") || !utf8.ValidString(s) {
			t.Errorf("%s is not one valid line", name)
		}
	}
	if p.Title != long {
		t.Error("WriteReport changed the caller's report")
	}
	if utf8.RuneCountInString(lines(p.Text())[2]) > len("project: ")+len(p.RunID)+1+maxFieldRunes*2 {
		t.Error("printed title unbounded")
	}
}
