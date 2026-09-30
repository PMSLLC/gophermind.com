package report_test

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
)

func call(task, stage, node, provider, served string, o ledger.Outcome, pt, ct int, ms int64) ledger.Call {
	return ledger.Call{RunID: "gm-1", TaskType: task, Stage: stage, NodeID: node, Provider: provider,
		ModelRequested: "req", ModelServed: served, Outcome: o, PromptTokens: pt, CompletionTokens: ct, DurationMS: ms}
}

func att(model string, v blackboard.Verdict) blackboard.Attempt {
	return blackboard.Attempt{Model: model, Provider: "mini", Verdict: v}
}

func fixture() report.Input {
	calls := []ledger.Call{
		call("clarify", "clarify", "", "mini", "qwen", ledger.OutcomeOK, 10, 1, 100),
		call("contract", "contract:outline", "", "mini", "qwen", ledger.OutcomeOK, 20, 2, 200),
		call("implement", "implement:a", "a", "mini", "qwen", ledger.OutcomeOK, 100, 10, 1000),
		call("implement", "implement:b", "b", "mini", "qwen", ledger.OutcomeMalformed, 100, 10, 2000),
		call("implement", "implement:b", "b", "mini", "qwen", ledger.OutcomeOK, 100, 10, 3000),
		call("implement", "implement:c", "c", "mini", "qwen", ledger.OutcomeOK, 100, 10, 4000),
		call("implement", "implement:d", "d", "mini", "qwen", ledger.OutcomeOK, 100, 10, 5000),
		call("implement", "implement:c", "c", "kilo", "free", ledger.OutcomeOK, 50, 5, 700),
		call("revise", "revise:d", "d", "mini", "qwen", ledger.OutcomeOK, 30, 3, 300),
	}
	rows := []blackboard.Row{
		{NodeID: "a", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{att("qwen", blackboard.VerdictPass)}},
		{NodeID: "b", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{att("qwen", blackboard.VerdictFail), att("qwen", blackboard.VerdictPass)}},
		{NodeID: "c", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{
			att("qwen", blackboard.VerdictFail),
			{Model: "free", Provider: "kilo", Verdict: blackboard.VerdictPass, Order: 2}}},
		{NodeID: "d", Status: blackboard.StatusEscalated, Attempts: []blackboard.Attempt{att("qwen", blackboard.VerdictFail), att("qwen", blackboard.VerdictFail)}},
	}
	reqs := []planner.Requirement{
		{ID: "C1", Kind: planner.ReqConstraint}, {ID: "A1", Kind: planner.ReqAcceptance}, {ID: "F1", Kind: planner.ReqFeature},
	}
	cov := planner.CoverageFile{
		Covered:   []planner.Covered{{Requirement: "C1"}, {Requirement: "A1"}, {Requirement: "F1"}},
		RootTests: []planner.RootTest{{Requirement: "C1", Name: "t1"}, {Requirement: "A1", Name: "t2"}},
	}
	return report.Input{
		RunID: "gm-1", StartedAt: time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC),
		Status: "escalated", StopReason: "leaf_escalated", Sandbox: "on",
		Calls: calls, Rows: rows, Requirements: reqs, Coverage: cov,
		AcceptancePassed: 2, AcceptanceTotal: 2, ConstraintsPassed: 1, Waves: 3,
		Escalations: []report.Escalation{
			{Kind: "model", TaskType: "implement", Model: "kilo/free", NodeID: "c"},
			{Kind: "revision", TaskType: "revise", Model: "mini/qwen", NodeID: "d"},
			{Kind: "human", TaskType: "implement", Model: "mini/qwen", NodeID: "d"},
		},
		WeakTests: 2, Repairs: 1, Failures: []string{"d: escalated"},
	}
}

func opt(root string) report.WriteOptions { return report.WriteOptions{RepoRoot: root} }

func build(t *testing.T, in report.Input) report.Report {
	t.Helper()
	r, err := report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func find(t *testing.T, r report.Report, task, model string) report.TaskModel {
	t.Helper()
	for _, e := range r.ByTaskType {
		if e.TaskType == task && e.Model == model {
			return e
		}
	}
	t.Fatalf("no entry %s %s", task, model)
	return report.TaskModel{}
}

func TestReportHandCount(t *testing.T) {
	r := build(t, fixture())
	// implement/mini/qwen: calls 5 (a, b, b, c, d); ok 4; malformed 1.
	// retries: (a) row beyond first for (b, implement:b, rev 0, mini) = 1;
	// (b) attempts beyond first: b has 2 mini attempts = 1, d has 2 mini attempts = 1, a and c have 1 = 0;
	// total 1 + 1 + 1 = 3.
	// tokens: prompt 5*100 = 500, completion 5*10 = 50. avg ms (1000+2000+3000+4000+5000)/5 = 3000.
	// escalations: human 1 (implement/mini/qwen, node d), model 0, revision 0.
	im := find(t, r, "implement", "mini/qwen")
	want := report.TaskModel{TaskType: "implement", Model: "mini/qwen", Calls: 5, OK: 4, Malformed: 1, Retries: 3,
		HumanEscalations: 1, PromptTokens: 500, CompletionTokens: 50, AvgDurationMS: 3000}
	if im != want {
		t.Fatalf("implement/mini: got %+v want %+v", im, want)
	}
	// implement/kilo/free: calls 1, ok 1, retries 0 (one row, one attempt), model escalations 1 (c), tokens 50/5, avg 700.
	k := find(t, r, "implement", "kilo/free")
	if k != (report.TaskModel{TaskType: "implement", Model: "kilo/free", Calls: 1, OK: 1, ModelEscalations: 1, PromptTokens: 50, CompletionTokens: 5, AvgDurationMS: 700}) {
		t.Fatalf("kilo: %+v", k)
	}
	// revise/mini/qwen: calls 1, ok 1, revision escalations 1 (d), tokens 30/3, avg 300.
	rv := find(t, r, "revise", "mini/qwen")
	if rv != (report.TaskModel{TaskType: "revise", Model: "mini/qwen", Calls: 1, OK: 1, RevisionEscalations: 1, PromptTokens: 30, CompletionTokens: 3, AvgDurationMS: 300}) {
		t.Fatalf("revise: %+v", rv)
	}
	if len(r.ByTaskType) != 5 { // clarify, contract, implement x2, revise
		t.Fatalf("entries %d", len(r.ByTaskType))
	}
	// Nodes: total 4 rows; verified a, b, c = 3; failed 0; escalated d = 1; blocked 0.
	if r.Nodes != (report.NodeCounts{Total: 4, Verified: 3, Escalated: 1}) {
		t.Fatalf("nodes %+v", r.Nodes)
	}
	if r.WeakTests != 2 || r.Repairs != 1 || r.Waves != 3 {
		t.Fatalf("counts %+v", r)
	}
	// Requirements: 3 requirements, 3 covered. Acceptance 2 of 2. Constraints: one root test on C1 = total 1, passed 1.
	if r.Requirements != (report.Coverage{Covered: 3, Total: 3}) || r.Acceptance != (report.Passed{Passed: 2, Total: 2}) || r.Constraints != (report.Passed{Passed: 1, Total: 1}) {
		t.Fatalf("proof %+v %+v %+v", r.Requirements, r.Acceptance, r.Constraints)
	}
	if r.ExitCode != 4 || r.Status != "escalated" || r.SchemaVersion != 1 {
		t.Fatalf("status %+v", r)
	}
	if r.StartedAt != "2026-09-30T01:00:00Z" {
		t.Fatalf("started %q", r.StartedAt)
	}
}

func TestNodesPartialCounts(t *testing.T) {
	in := fixture()
	in.Rows = append(in.Rows, blackboard.Row{NodeID: "e", Status: blackboard.StatusPending}, blackboard.Row{NodeID: "f", Status: blackboard.StatusFailed})
	in.Blocked = []string{"e"}
	r := build(t, in)
	// total 6; verified 3; failed 1 (f); escalated 1; blocked 1 (reported); pending counts only in total: 3+1+1+1 = 6 here,
	// and with another in-progress row the sum is below total.
	if r.Nodes != (report.NodeCounts{Total: 6, Verified: 3, Failed: 1, Escalated: 1, Blocked: 1}) {
		t.Fatalf("%+v", r.Nodes)
	}
	in.Rows = append(in.Rows, blackboard.Row{NodeID: "g", Status: blackboard.StatusInProgress})
	r = build(t, in)
	n := r.Nodes
	if n.Total-(n.Verified+n.Failed+n.Escalated+n.Blocked) != 1 {
		t.Fatalf("difference %+v", n)
	}
}

func TestAcceptanceTotalFallbackAndConstraintOverride(t *testing.T) {
	in := fixture()
	in.AcceptanceTotal = 0
	in.AcceptancePassed = 0
	in.ConstraintsTotal = 7
	r := build(t, in)
	if r.Acceptance != (report.Passed{Passed: 0, Total: 1}) || r.Constraints.Total != 7 {
		t.Fatalf("%+v %+v", r.Acceptance, r.Constraints)
	}
}

func TestModelPerTaskTypeTable(t *testing.T) {
	in := report.Input{RunID: "r", Status: "verified", Sandbox: "off"}
	in.Calls = []ledger.Call{
		call("revise", "revise:x", "x", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("zeta", "zeta", "", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("implement", "implement:x", "x", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("alpha", "alpha", "", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("testwrite", "testwrite:x", "x", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("coverage", "coverage", "", "kilo", "free", ledger.OutcomeOK, 1, 1, 1),
		call("decompose", "decompose:c", "", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("contract", "contract:o", "", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("clarify", "clarify", "", "mini", "qwen", ledger.OutcomeOK, 1, 1, 1),
		call("implement", "implement:y", "y", "kilo", "free", ledger.OutcomeOK, 1, 1, 1),
		call("implement", "implement:z", "z", "mini", "", ledger.OutcomeOK, 1, 1, 1), // served empty: keys on requested
	}
	in.Escalations = []report.Escalation{{Kind: "model", TaskType: "implement", Model: "ghost/none", NodeID: "x"}}
	r := build(t, in)
	var got []string
	for _, e := range r.ByTaskType {
		got = append(got, e.TaskType+"|"+e.Model)
	}
	want := []string{"clarify|mini/qwen", "contract|mini/qwen", "decompose|mini/qwen", "coverage|kilo/free", "testwrite|mini/qwen",
		"implement|ghost/none", "implement|kilo/free", "implement|mini/qwen", "implement|mini/req", "revise|mini/qwen", "alpha|mini/qwen", "zeta|mini/qwen"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order\n got %v\nwant %v", got, want)
	}
	g := find(t, r, "implement", "ghost/none")
	if g.Calls != 0 || g.ModelEscalations != 1 {
		t.Fatalf("ghost %+v", g)
	}
}

func TestLeafModelPassRate(t *testing.T) {
	r := build(t, fixture())
	// mini/qwen attempts: a 1, b 2, c 1, d 2 = 6; passes a, b = 2; rate 2/6 = 0.3333; first try wins: a only (b failed first) = 1.
	// kilo/free attempts 1, passes 1, rate 1, first try wins 0 (c's first attempt was mini).
	want := []report.LeafModel{
		{Model: "kilo/free", Attempts: 1, Passes: 1, PassRate: 1, FirstTryWins: 0},
		{Model: "mini/qwen", Attempts: 6, Passes: 2, PassRate: 0.3333, FirstTryWins: 1},
	}
	if len(r.Leaves) != 2 || r.Leaves[0] != want[0] || r.Leaves[1] != want[1] {
		t.Fatalf("%+v", r.Leaves)
	}
}

func TestLeafModelSkipsErrorAttempts(t *testing.T) {
	in := report.Input{RunID: "r", Status: "verified", Sandbox: "on", Rows: []blackboard.Row{
		{NodeID: "a", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{att("qwen", blackboard.VerdictError), att("qwen", blackboard.VerdictPass)}},
	}}
	r := build(t, in)
	if len(r.Leaves) != 1 || r.Leaves[0].Attempts != 1 || r.Leaves[0].FirstTryWins != 1 {
		t.Fatalf("%+v", r.Leaves)
	}
	if got := build(t, report.Input{}).Leaves; len(got) != 0 {
		t.Fatal("expected none")
	}
}

func lastLines(s string, n int) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out[len(out)-n:]
}

func TestSummaryEndsWithProofLines(t *testing.T) {
	in := fixture()
	in.Status, in.StopReason = "verified", ""
	r := build(t, in)
	l := lastLines(r.Summary(), 2)
	if l[0] != "Requirements covered: 3 of 3" || l[1] != "Acceptance passed: 2 of 2" {
		t.Fatalf("%q", l)
	}
	if !strings.HasSuffix(r.Summary(), "\n") || strings.HasSuffix(r.Summary(), "\n\n") {
		t.Fatal("must end with exactly one newline")
	}
	in.AcceptancePassed = 1
	in.Status = "failed"
	f := build(t, in)
	l = lastLines(f.Summary(), 2)
	if l[1] != "Acceptance passed: 1 of 2" || strings.Contains(l[1], "2 of 2") {
		t.Fatalf("%q", l)
	}
	s := r.Summary()
	for _, w := range []string{"Run gm-1: verified", "Sandbox: on", "Nodes:", "Waves: 3",
		"implement  mini/qwen  calls 5  ok 4  malformed 1  retries 3  escalations m0/r0/h1  tokens 500/50",
		"Weak tests: 2  Repairs: 1", "Constraints checked: 1 of 1"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
}

func TestSummaryListsFailuresLandingIncomplete(t *testing.T) {
	in := fixture()
	in.Failures = []string{"d: escalated", "e: blocked by d", "f: not_run: leaf_escalated"}
	in.LedgerErrors = 2
	in.Landing = &report.Landing{Branch: "gm/run", Commit: "abc123", MergedInto: "main"}
	s := build(t, in).Summary()
	for _, w := range []string{"d: escalated", "e: blocked by d", "f: not_run: leaf_escalated", "abc123", "Incomplete: ledger writes failed"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q", w)
		}
	}
	l := lastLines(s, 2)
	if !strings.HasPrefix(l[0], "Requirements covered:") {
		t.Fatalf("%q", l)
	}
}

func TestSummaryEscapesControlCharacters(t *testing.T) {
	in := fixture()
	in.RunID = "gm\x1b[31m-1"
	in.Failures = []string{"x\r\ny\x07: escalated "}
	in.Calls[2].Provider = "mi\x1bni"
	s := build(t, in).Summary()
	for _, r := range s {
		if r != '\n' && (r < 0x20 || r == 0x7f || r == 0x2028 || (r >= 0x80 && r < 0xa0)) {
			t.Fatalf("control rune %U survived in\n%q", r, s)
		}
	}
	if !strings.Contains(s, `\x1b`) {
		t.Fatalf("escape not shown: %q", s)
	}
}

func TestExitCodeMapping(t *testing.T) {
	for _, c := range []struct {
		status, stop string
		want         int
	}{{"verified", "", 0}, {"failed", "x", 1}, {"escalated", "x", 4}, {"escalated", "waiting_on_human", 3},
		{"interrupted", "", 5}, {"weird", "", 1}} {
		if got := report.ExitCode(c.status, c.stop); got != c.want {
			t.Errorf("%s/%s: %d want %d", c.status, c.stop, got, c.want)
		}
	}
}

func TestReportWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := build(t, fixture())
	if err := report.Write(dir, r, opt(dir)); err != nil {
		t.Fatal(err)
	}
	got, err := report.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("round trip differs\n%s\n%s", a, b)
	}
	fi, err := os.Stat(filepath.Join(dir, report.FileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	r.Repairs = 9
	if err := report.Write(dir, r, opt(dir)); err != nil {
		t.Fatal(err)
	}
	got, _ = report.Read(dir)
	if got.Repairs != 9 {
		t.Fatal("not replaced")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
}

func TestReportWriteRefusals(t *testing.T) {
	r := build(t, fixture())
	if err := report.Write("", r, opt(t.TempDir())); err == nil {
		t.Fatal("empty dir accepted")
	}
	if err := report.Write(filepath.Join(t.TempDir(), "missing"), r, opt(t.TempDir())); err == nil {
		t.Fatal("missing dir accepted")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.Mkdir(real, 0o700)
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks")
	}
	if err := report.Write(link, r, opt(base)); err == nil {
		t.Fatal("symlinked run dir accepted")
	}
	// A report.json that is a symlink is replaced by a file, never followed.
	victim := filepath.Join(base, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	os.Symlink(victim, filepath.Join(real, report.FileName))
	if err := report.Write(real, r, opt(base)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatal("wrote through a symlink")
	}
}

func TestReadRejectsUnknownVersionAndBadFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := report.Read(dir); err == nil {
		t.Fatal("missing accepted")
	}
	os.WriteFile(filepath.Join(dir, report.FileName), []byte(`{"schema_version":99,"run_id":"x"}`), 0o600)
	if _, err := report.Read(dir); err == nil || strings.Contains(err.Error(), "99x") {
		t.Fatalf("err %v", err)
	}
	os.WriteFile(filepath.Join(dir, report.FileName), []byte(`CANARY-report-text not json`), 0o600)
	if _, err := report.Read(dir); err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err must not quote the file: %v", err)
	}
}

func TestReportCarriesNoText(t *testing.T) {
	in := fixture()
	in.Calls[3].ErrorKind = "CANARY-report-text"
	in.Rows[3].Attempts[0].FailureReason = "CANARY-report-text"
	in.Rows[3].Attempts[0].FailedTests = []string{"CANARY-report-text"}
	in.Failures = []string{"d: test_fail: TestPassedThrough"}
	r := build(t, in)
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "CANARY-report-text") || strings.Contains(r.Summary(), "CANARY-report-text") {
		t.Fatal("canary copied")
	}
	if !strings.Contains(string(raw), "d: test_fail: TestPassedThrough") {
		t.Fatal("caller failure not passed through")
	}
}

func TestRepoFieldsDropCredentials(t *testing.T) {
	in := fixture()
	in.RepoBrief = "https://user:CANARY-secret@example.com/org/repo.git"
	in.RepoUsed = "/home/x/repo"
	r := build(t, in)
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "CANARY-secret") || strings.Contains(string(raw), "user:") {
		t.Fatalf("credentials kept: %s", raw)
	}
	if r.RepoBrief != "https://example.com/org/repo.git" || r.RepoUsed != "/home/x/repo" {
		t.Fatalf("%q %q", r.RepoBrief, r.RepoUsed)
	}
}

func TestBuildDeterministic(t *testing.T) {
	a, _ := json.Marshal(build(t, fixture()))
	for i := 0; i < 20; i++ {
		in := fixture()
		rand.Shuffle(len(in.Calls), func(x, y int) { in.Calls[x], in.Calls[y] = in.Calls[y], in.Calls[x] })
		rand.Shuffle(len(in.Rows), func(x, y int) { in.Rows[x], in.Rows[y] = in.Rows[y], in.Rows[x] })
		rand.Shuffle(len(in.Escalations), func(x, y int) { in.Escalations[x], in.Escalations[y] = in.Escalations[y], in.Escalations[x] })
		b, _ := json.Marshal(build(t, in))
		if string(a) != string(b) {
			t.Fatalf("not deterministic\n%s\n%s", a, b)
		}
	}
	d1, d2 := t.TempDir(), t.TempDir()
	report.Write(d1, build(t, fixture()), opt(d1))
	report.Write(d2, build(t, fixture()), opt(d2))
	x, _ := os.ReadFile(filepath.Join(d1, report.FileName))
	y, _ := os.ReadFile(filepath.Join(d2, report.FileName))
	if string(x) != string(y) {
		t.Fatal("file bytes differ")
	}
}

func TestJSONKeysStable(t *testing.T) {
	raw, _ := json.Marshal(build(t, fixture()))
	var m map[string]json.RawMessage
	json.Unmarshal(raw, &m)
	for _, k := range []string{"schema_version", "run_id", "status", "exit_code", "requirements_covered", "acceptance", "constraints_checked", "nodes", "by_task_type", "leaves", "failures"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if string(m["requirements_covered"]) != `{"covered":3,"total":3}` || string(m["acceptance"]) != `{"passed":2,"total":2}` {
		t.Fatalf("%s %s", m["requirements_covered"], m["acceptance"])
	}
	if !strings.Contains(string(m["nodes"]), `"total":4`) {
		t.Fatal(string(m["nodes"]))
	}
}

func TestStripCredentialsTable(t *testing.T) {
	const tok = "TOKSECRET"
	for _, c := range []struct{ in, want string }{
		{"https://user:" + tok + "@example.com/org/repo.git", "https://example.com/org/repo.git"},
		{"https://" + tok + "@example.com/repo", "https://example.com/repo"},
		{"ssh://git:" + tok + "@host.example:22/org/repo", "ssh://host.example:22/org/repo"},
		{"git+ssh://" + tok + "@host/repo", "git+ssh://host/repo"},
		{"user:" + tok + "@host.example/org/repo", "host.example/org/repo"},
		{tok + "@host.example:org/repo.git", "host.example:org/repo.git"},
		{"https://example.com/repo?token=" + tok, "https://example.com/repo"},
		{"https://example.com/repo#" + tok, "https://example.com/repo"},
		{"https://example.com/repo?e=a@" + tok + "#f", "https://[redacted]"},
		{"https://user%40x:" + tok + "@example.com/r", "https://example.com/r"},
		{"https://user:" + tok + "%40example.com/r", "https://example.com/r"},
		{"https://user:pa/" + tok + "@example.com/r", "https://example.com/r"},
		{"https://ex\tample.com/r\n", "https://example.com/r"},
		{"https://user:" + tok + "@exa\x00mple.com/r", "https://example.com/r"},
		{"/home/a@b/repo", "b/repo"},
		{"/home/x/repo", "/home/x/repo"},
		{"https://example.com/org/repo.git", "https://example.com/org/repo.git"},
		{"", ""},
		{"https://user:pa?ss#x@host/repo", "https://host/repo"},
		{"https://user:pa#ss?x@host/repo", "https://host/repo"},
		{"https://user:p@ss@host/repo", "https://host/repo"},
		{"https://user:p%40ss@host/repo", "https://host/repo"},
		{"https://user:p%3Fss%23x@host/repo", "https://host/repo"},
		{"https://user:p/a/ss@host/repo?x=1", "https://host/repo"},
		{"https://user:p@a@b@host/r#f", "https://host/r"},
		{"user:pa?ss#x@host/repo", "host/repo"},
		{"https://u:p?x@host", "https://[redacted]"},
	} {
		in := strings.NewReplacer("\\t", "\t", "\\n", "\n", "\\x00", "\x00").Replace(c.in)
		got := report.StripCredentials(in)
		if got != c.want {
			t.Errorf("%q: got %q want %q", in, got, c.want)
		}
		if strings.Contains(got, tok) {
			t.Errorf("%q: token survived", in)
		}
	}
}

func TestRepoFieldsNeverCarryPlantedToken(t *testing.T) {
	const tok = "TOKSECRET"
	forms := []string{"https://u:" + tok + "@h/r", "u:" + tok + "@h/r", tok + "@h:r", "https://h/r?t=" + tok, "https://h/r#" + tok, "ssh://" + tok + "@h/r", "https://u%40:" + tok + "@h/r"}
	for _, f := range forms {
		in := fixture()
		in.RepoBrief, in.RepoUsed = f, f
		raw, _ := json.Marshal(build(t, in))
		if strings.Contains(string(raw), tok) || strings.Contains(build(t, in).Summary(), tok) {
			t.Errorf("token in report for %q", f)
		}
	}
}

func completeInput() report.Input {
	in := fixture()
	in.Failures = nil
	in.PlanLeaves = []string{"a", "b", "c", "d", "e", "f"}
	in.LeafDeps = map[string][]string{"e": nil, "f": {"d"}}
	return in
}

func TestEveryPlanLeafVerifiedOrNamed(t *testing.T) {
	r := build(t, completeInput())
	want := []string{"d: escalated: no failure recorded", "e: not_run: no record", "f: blocked by d"}
	if strings.Join(r.Failures, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", r.Failures, want)
	}
	// A leaf already named by the caller is not duplicated.
	in := completeInput()
	in.Failures = []string{"d: test_fail: TestX"}
	r = build(t, in)
	want = []string{"d: test_fail: TestX", "e: not_run: no record", "f: blocked by d"}
	if strings.Join(r.Failures, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v", r.Failures)
	}
}

func TestVerifiedRunHasNoSpuriousFailures(t *testing.T) {
	in := report.Input{RunID: "r", Status: "verified", Sandbox: "on", PlanLeaves: []string{"a", "b"},
		Rows: []blackboard.Row{{NodeID: "a", Status: blackboard.StatusVerified}, {NodeID: "b", Status: blackboard.StatusVerified}}}
	r := build(t, in)
	if len(r.Failures) != 0 {
		t.Fatalf("%v", r.Failures)
	}
	in.PlanLeaves = nil
	in.Rows = in.Rows[:1]
	if r := build(t, in); len(r.Failures) != 0 {
		t.Fatal("nil leaf list must mean rows only")
	}
}

func TestUnknownEscalationKindIsError(t *testing.T) {
	in := fixture()
	in.Escalations = append(in.Escalations, report.Escalation{Kind: "CANARY-kind", TaskType: "implement", Model: "m/x"})
	_, err := report.Build(in)
	if err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err %v", err)
	}
}

func TestFirstTryWinsOrdersByAttemptOrder(t *testing.T) {
	in := report.Input{RunID: "r", Status: "verified", Sandbox: "on", Rows: []blackboard.Row{{NodeID: "c", Status: blackboard.StatusVerified,
		Attempts: []blackboard.Attempt{
			{Model: "free", Provider: "kilo", Verdict: blackboard.VerdictPass, Order: 2},
			{Model: "qwen", Provider: "mini", Verdict: blackboard.VerdictFail, Order: 1},
		}}}}
	r := build(t, in)
	for _, l := range r.Leaves {
		if l.FirstTryWins != 0 {
			t.Fatalf("%+v", r.Leaves)
		}
	}
}

func TestWriteRefusesLinkedComponentUnderGophermind(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.MkdirAll(filepath.Join(real, "gm-1"), 0o700)
	if err := os.Symlink(real, filepath.Join(base, ".gophermind")); err != nil {
		t.Skip("no symlinks")
	}
	if err := report.Write(filepath.Join(base, ".gophermind", "gm-1"), build(t, fixture()), opt(base)); err == nil {
		t.Fatal("linked .gophermind accepted")
	}
	if _, err := os.Stat(filepath.Join(real, "gm-1", report.FileName)); err == nil {
		t.Fatal("wrote through the link")
	}
	// A plain .gophermind/<id> works.
	ok := filepath.Join(t.TempDir(), ".gophermind", "gm-1")
	os.MkdirAll(ok, 0o700)
	if err := report.Write(ok, build(t, fixture()), opt(filepath.Dir(filepath.Dir(ok)))); err != nil {
		t.Fatal(err)
	}
}

func TestReadValidatesShape(t *testing.T) {
	dir := t.TempDir()
	put := func(s string) { os.WriteFile(filepath.Join(dir, report.FileName), []byte(s), 0o600) }
	bad := map[string]string{
		"unknown field": `{"schema_version":1,"status":"verified","exit_code":0,"surprise":1}`,
		"bad exit":      `{"schema_version":1,"status":"verified","exit_code":4}`,
		"deep":          `{"schema_version":1,"status":"verified","exit_code":0,"failures":` + strings.Repeat("[", 500) + strings.Repeat("]", 500) + `}`,
	}
	for name, s := range bad {
		put(s)
		if _, err := report.Read(dir); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	put(`{"schema_version":1,"status":"verified","exit_code":0}`)
	if _, err := report.Read(dir); err != nil {
		t.Fatal(err)
	}
	big := `{"schema_version":1,"status":"verified","exit_code":0,"run_id":"` + strings.Repeat("x", 9<<20) + `"}`
	put(big)
	if _, err := report.Read(dir); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestWriteChecksEveryComponentBelowRepoRoot(t *testing.T) {
	root := t.TempDir()
	r := build(t, fixture())
	// Outside the repo root is refused.
	if err := report.Write(t.TempDir(), r, opt(root)); err == nil {
		t.Fatal("run dir outside RepoRoot accepted")
	}
	if err := report.Write(root, r, report.WriteOptions{}); err == nil {
		t.Fatal("missing RepoRoot accepted")
	}
	if err := report.Write(root+"x", r, opt(root)); err == nil {
		t.Fatal("sibling prefix accepted")
	}
	// A link in the middle of the path below the root is refused, even when
	// the path does not contain .gophermind.
	real := filepath.Join(t.TempDir(), "elsewhere")
	os.MkdirAll(filepath.Join(real, "deep"), 0o700)
	if err := os.Symlink(real, filepath.Join(root, "mid")); err != nil {
		t.Skip("no symlinks")
	}
	if err := report.Write(filepath.Join(root, "mid", "deep"), r, opt(root)); err == nil {
		t.Fatal("linked middle component accepted")
	}
	if _, err := os.Stat(filepath.Join(real, "deep", report.FileName)); err == nil {
		t.Fatal("wrote through link")
	}
	// A real nested dir works.
	ok := filepath.Join(root, "a", "b")
	os.MkdirAll(ok, 0o700)
	if err := report.Write(ok, r, opt(root)); err != nil {
		t.Fatal(err)
	}
	// A root that is reached through an OS alias symlink is tolerated.
	alias := filepath.Join(t.TempDir(), "alias")
	os.Symlink(root, alias)
	if err := report.Write(filepath.Join(alias, "a", "b"), r, opt(alias)); err != nil {
		t.Fatalf("root given as alias: %v", err)
	}
}
