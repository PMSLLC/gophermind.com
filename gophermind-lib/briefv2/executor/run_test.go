package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/runner"
)

// goodScript is a good reply for every leaf.
func goodScript(g *rig) Script {
	s := Script{}
	for _, l := range g.plan.Leaves {
		s["implement:"+l.ID] = []step{reply(good(l.ID))}
	}
	return s
}

// fastRun is a run whose checks are scripted (the fake checker replaces the
// real runner once Wave 0 is done). Every leaf passes unless a test scripts
// otherwise; the repository-wide checks pass unless scripted.
func (g *rig) fastChecker() *fakeChecker {
	fc := newFakeChecker(g)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, l := range g.plan.Leaves {
		fc.LeafScript[l.ID] = []runner.Verdict{passVerdict()}
	}
	return fc
}

// doRun wires the script and runs with the fake checker.
func (g *rig) doRun(t *testing.T, script Script, fc *fakeChecker, mod func(*Options)) (Report, error) {
	t.Helper()
	g.wire(script)
	o := g.options()
	if mod != nil {
		mod(&o)
	}
	return run(context.Background(), o, runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
}

func readReportFile(t *testing.T, g *rig) report.Report {
	t.Helper()
	p := filepath.Join(g.runDir, report.FileName)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("no report file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("report.json mode = %o, want 600", fi.Mode().Perm())
	}
	r, err := report.Read(g.runDir)
	if err != nil {
		t.Fatalf("report.json is not readable: %v", err)
	}
	return r
}

func TestRunLandsCommitsOnMain(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "verified" || rep.ExitCode != 0 {
		t.Fatalf("report = %s (%s) exit %d, failures %v", rep.Status, rep.StopReason, rep.ExitCode, rep.Failures)
	}
	subjects := strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%s", "main")), "\n")
	var leaf, wave0, final int
	for _, s := range subjects {
		switch {
		case s == "gm(wave0): tests, types, stubs and go.mod":
			wave0++
		case strings.HasPrefix(s, "gm(run): Greeter built"):
			final++
		case strings.HasPrefix(s, "gm(fn-"):
			leaf++
		}
	}
	if wave0 != 1 || leaf != 5 || final != 1 || len(subjects) != 8 {
		t.Fatalf("main holds %d wave 0, %d leaf, %d final commits of %d: %q", wave0, leaf, final, len(subjects), subjects)
	}
	st, err := LoadState(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := strings.TrimSpace(g.gitCmd("rev-parse", "main")), strings.TrimSpace(g.gitCmd("rev-parse", st.Branch)); a != b {
		t.Errorf("main %s and %s %s differ", a, st.Branch, b)
	}
	if rep.Landing == nil || rep.Landing.MergedInto != "main" || rep.Landing.Branch != st.Branch || rep.Landing.Commit == "" {
		t.Errorf("landing = %+v", rep.Landing)
	}
	if tracked := g.gitCmd("ls-files", ".gophermind"); strings.TrimSpace(tracked) != "" {
		t.Errorf(".gophermind is tracked: %s", tracked)
	}
	if all := g.gitCmd("log", "--all", "--name-only", "--format="); strings.Contains(all, ".gophermind") {
		t.Error(".gophermind is in a commit")
	}
	if out := strings.TrimSpace(g.gitCmd("remote")); out != "" {
		t.Errorf("git remote = %q, want none", out)
	}
	if rd := readReportFile(t, g); rd.Status != "verified" {
		t.Errorf("report.json status = %s", rd.Status)
	}
}

func TestVerifiedImpliesEveryLeafVerified(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("good run = %s, %v", rep.Status, err)
	}
	rows, err := g.board.List(context.Background(), g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	leaves := 0
	for _, r := range rows {
		if g.plan.Leaf(r.NodeID) == nil {
			continue // the planner's rows for the root and the components
		}
		leaves++
		if r.Status != blackboard.StatusVerified {
			t.Errorf("a verified run has %s at %s", r.NodeID, r.Status)
		}
	}
	if leaves != 5 {
		t.Fatalf("%d leaf rows, want 5", leaves)
	}

	// One skipped leaf: never verified.
	g2 := newRig(t, oneShot(0))
	script := goodScript(g2)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	fc := g2.fastChecker()
	fc.LeafScript["fn-greet"] = nil
	failing(fc, "fn-greet", 2)
	g2.wire(script)
	g2.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	rep2, err := run(context.Background(), g2.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep2.Status == "verified" || rep2.Status != "failed" {
		t.Fatalf("a run with a skipped leaf = %s, %v", rep2.Status, err)
	}
}

func TestRunReturnsReportForFailedBuild(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	script := goodScript(g)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = nil
	failing(fc, "fn-greet", 2)
	g.wire(script)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil {
		t.Fatalf("a failed build is a report, got error %v", err)
	}
	if rep.Status != "failed" || rep.ExitCode != 1 {
		t.Fatalf("report = %s exit %d", rep.Status, rep.ExitCode)
	}
	found := false
	for _, f := range rep.Failures {
		if strings.HasPrefix(f, "fn-greet:") && strings.Contains(f, "skipped by human") {
			found = true
		}
	}
	if !found {
		t.Errorf("failures = %q, want fn-greet with skipped by human", rep.Failures)
	}
}

func TestRunErrorOnlyForHarnessFault(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	p := filepath.Join(g.runDir, "dependencies.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true})
	if err == nil {
		t.Fatal("a tampered plan did not return an error")
	}
	if !reflect.DeepEqual(rep, Report{}) {
		t.Errorf("a harness fault returned a report: %+v", rep)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("%d provider calls were made for a plan that does not verify", n)
	}
}

// cancelOn is a sink that cancels the run when the first event of a kind arrives.
type cancelOn struct {
	*events.Collector
	kind   string
	cancel context.CancelFunc
}

func (c *cancelOn) Emit(e events.Event) {
	c.Collector.Emit(e)
	if e.Kind == c.kind {
		c.cancel()
	}
}

func TestExitCodes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		status string
		code   int
		failed bool              // fn-greet fails its leaf test until the gate is asked
		gate   func(*scriptGate) // how the gate answers
		cancel bool              // cancel the run in the first reply
	}{
		{"all good", "verified", 0, false, nil, false},
		{"skip", "failed", 1, true, func(s *scriptGate) { s.queue = []human.Resolution{{Action: human.ActionSkip}} }, false},
		{"gate stop", "escalated", 4, true, nil, false},
		{"gate waiting", "escalated", 3, true, func(s *scriptGate) { s.waiting = true }, false},
		{"cancelled in the first reply", "interrupted", 5, false, nil, true},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var mods []func(*rigOpts)
			if c.failed {
				mods = append(mods, oneShot(0))
			}
			g := newRig(t, mods...)
			script := goodScript(g)
			fc := g.fastChecker()
			if c.failed {
				script["implement:fn-greet"] = distinct("fn-greet", 2)
				fc.LeafScript["fn-greet"] = nil
				failing(fc, "fn-greet", 2)
			}
			ctx := context.Background()
			if c.cancel {
				script["implement:fn-farewell"] = []step{{Delay: 400 * time.Millisecond}}
			}
			g.wire(script)
			if c.gate != nil {
				c.gate(g.gate)
			}
			o := g.options()
			if c.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				o.Sink = &cancelOn{Collector: g.sink, kind: "leaf_started", cancel: cancel}
			}
			rep, err := run(ctx, o, runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
			if err != nil {
				t.Fatalf("run returned an error: %v", err)
			}
			if rep.Status != c.status || rep.ExitCode != c.code {
				t.Fatalf("status %s exit %d (%s), want %s %d", rep.Status, rep.ExitCode, rep.StopReason, c.status, c.code)
			}
			rd := readReportFile(t, g)
			if rd.Status != c.status || rd.ExitCode != c.code {
				t.Errorf("report.json says %s %d", rd.Status, rd.ExitCode)
			}
		})
	}
}

func TestLedgerErrorsMarkReportIncomplete(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), func(o *Options) { o.LedgerErrors = func() int { return 2 } })
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Incomplete || !strings.Contains(rep.Summary(), "Incomplete") {
		t.Errorf("incomplete = %v, summary %q", rep.Incomplete, rep.Summary())
	}
	if rd := readReportFile(t, g); !rd.Incomplete {
		t.Error("report.json is not marked incomplete")
	}
}

func TestDiffOnlyPatch(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: diff_only", 1) }
	})
	before := strings.TrimSpace(g.gitCmd("rev-parse", "HEAD"))
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, failures %v", rep.Status, rep.StopReason, err, rep.Failures)
	}
	raw, err := os.ReadFile(filepath.Join(g.runDir, "changes.patch"))
	if err != nil || len(raw) == 0 {
		t.Fatalf("changes.patch: %d bytes, %v", len(raw), err)
	}
	for _, l := range g.plan.Leaves {
		if !strings.Contains(string(raw), l.File) {
			t.Errorf("the patch lacks %s", l.File)
		}
		if !fileExists(g.realPath(l)) || fileExists(g.stubPath(l)) {
			t.Errorf("the working tree of %s is not the finished leaf", l.ID)
		}
	}
	if after := strings.TrimSpace(g.gitCmd("rev-parse", "HEAD")); after != before {
		t.Errorf("diff_only made a commit: %s -> %s", before, after)
	}
	if br := g.gitCmd("branch", "--list"); strings.Contains(br, "gm/") {
		t.Errorf("diff_only made a branch: %s", br)
	}
	if rep.Landing != nil {
		t.Errorf("landing = %+v, want none", rep.Landing)
	}
	if fi, err := os.Stat(filepath.Join(g.runDir, "changes.patch")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("changes.patch mode: %v %v", fi, err)
	}
}

func TestReportWrittenOnEveryExit(t *testing.T) {
	t.Parallel()
	// A run that stops before its first wave still writes a report: here the
	// Wave 0 build check fails because a leaf's test file does not compile.
	g := newRig(t)
	g.wire(goodScript(g))
	l := g.plan.Leaf("fn-greet")
	tp := filepath.Join(g.repo, filepath.FromSlash(l.TestFile))
	raw, err := os.ReadFile(tp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, append(raw, []byte("\nfunc broken( {\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "failed" {
		t.Fatalf("status = %s", rep.Status)
	}
	if rd := readReportFile(t, g); rd.Status != "failed" || rd.StopReason == "" {
		t.Errorf("report.json = %s (%s)", rd.Status, rd.StopReason)
	}
}

func TestReportHandCountFromRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	script := goodScript(g)
	script["implement:fn-greet"] = []step{reply(variant(bad("fn-greet", 1), 1)), reply(variant(bad("fn-greet", 1), 2)), reply(good("fn-greet"))}
	script["revise:fn-greet"] = []step{reviseHint("check the empty name")}
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = nil
	failing(fc, "fn-greet", 2)
	fc.LeafScript["fn-greet"] = append(fc.LeafScript["fn-greet"], passVerdict())
	rep, err := g.doRun(t, script, fc, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, err, rep.Failures)
	}
	rows, err := g.led.List(context.Background(), g.id, ledger.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := g.led.Summary(context.Background(), g.id)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := map[string]int{}
	var wantTokens int64
	for _, r := range rows {
		wantCalls[r.TaskType]++
		wantTokens += int64(r.PromptTokens + r.CompletionTokens)
	}
	sumCalls := map[string]int{}
	for _, s := range sum {
		sumCalls[s.TaskType] += s.Calls
	}
	gotCalls := map[string]int{}
	var gotTokens int64
	for _, e := range rep.ByTaskType {
		gotCalls[e.TaskType] += e.Calls
		gotTokens += e.PromptTokens + e.CompletionTokens
	}
	if !reflect.DeepEqual(gotCalls, wantCalls) || !reflect.DeepEqual(gotCalls, sumCalls) {
		t.Errorf("report calls %v, ledger rows %v, ledger summary %v", gotCalls, wantCalls, sumCalls)
	}
	if gotTokens != wantTokens || gotTokens == 0 {
		t.Errorf("report tokens %d, ledger %d", gotTokens, wantTokens)
	}
	if wantCalls["implement"] != 7 || wantCalls["revise"] != 1 {
		t.Errorf("ledger calls = %v, want 7 implement (4 good leaves + 3 for fn-greet) and 1 revise", wantCalls)
	}
	var revisions, models int
	for _, e := range rep.ByTaskType {
		revisions += e.RevisionEscalations
		if e.TaskType == "implement" {
			models++
		}
	}
	if revisions != 1 {
		t.Errorf("revision escalations = %d, want 1", revisions)
	}
	if rep.Requirements.Total != 7 || rep.Requirements.Covered != 7 {
		t.Errorf("requirements covered %d of %d, want 7 of 7", rep.Requirements.Covered, rep.Requirements.Total)
	}
	if !strings.Contains(rep.Summary(), "Requirements covered: 7 of 7") {
		t.Errorf("summary lacks the coverage proof line")
	}
	if rep.Nodes.Total != 5 || rep.Nodes.Verified != 5 || rep.Waves != 3 {
		t.Errorf("nodes %+v waves %d", rep.Nodes, rep.Waves)
	}
}

func TestReportEnvironmentAndPlannerWarnings(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	// Two duplicate ids the planner dropped, as the Contract stage records them.
	p := filepath.Join(g.runDir, "_state", "contract.json")
	var doc map[string]any
	if raw, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(raw, &doc)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	doc["ignored_duplicates"] = []string{`function "fn-greet"`, `type "Greeting"`}
	doc["ignored_total"] = 3
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "verified" {
		t.Fatalf("status %s (%s)", rep.Status, rep.StopReason)
	}
	joined := strings.Join(rep.PlannerWarnings, "|")
	if !strings.Contains(joined, `duplicate id ignored: function "fn-greet"`) || !strings.Contains(joined, "3 duplicate emissions ignored in all, 2 listed") {
		t.Errorf("planner warnings = %q", rep.PlannerWarnings)
	}
	env := strings.Join(rep.Environment, "|")
	for _, want := range []string{"sandbox: off", "sandbox-exec ", "binary: ", "go: go"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment %q lacks %q", rep.Environment, want)
		}
	}
	if rep.Sandbox != "off" {
		t.Errorf("sandbox = %q", rep.Sandbox)
	}
	if rep.RepoUsed != g.repo {
		t.Errorf("repo used = %q", rep.RepoUsed)
	}
}

func TestBlockedLeavesReportedInRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	script := goodScript(g)
	script["implement:fn-bye"] = distinct("fn-bye", 2)
	fc := g.fastChecker()
	fc.LeafScript["fn-bye"] = nil
	failing(fc, "fn-bye", 2)
	g.wire(script)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "failed" {
		t.Fatalf("status = %s, want failed", rep.Status)
	}
	if rep.Nodes.Blocked != 1 {
		t.Errorf("blocked = %d, want fn-serve", rep.Nodes.Blocked)
	}
	want := map[string]bool{"fn-bye: skipped by human": false, "fn-serve: blocked by fn-bye": false}
	for _, f := range rep.Failures {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, ok := range want {
		if !ok {
			t.Errorf("failures %q lack %q", rep.Failures, f)
		}
	}
}

func TestReportListsEveryUnverifiedLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	script := goodScript(g)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = nil
	failing(fc, "fn-greet", 2)
	rep, err := g.doRun(t, script, fc, nil) // the default gate answers stop
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "escalated" || rep.StopReason != "human_stop" {
		t.Fatalf("status = %s (%s)", rep.Status, rep.StopReason)
	}
	got := map[string]string{}
	for _, f := range rep.Failures {
		id, reason, _ := strings.Cut(f, ": ")
		got[id] = reason
	}
	if len(got) != 4 || got["fn-hello"] != "blocked by fn-greet" || got["fn-bye"] != "not_run: human_stop" || got["fn-serve"] != "blocked by fn-bye" || got["fn-greet"] == "" {
		t.Fatalf("failures = %q", rep.Failures)
	}
	n := rep.Nodes
	if n.Verified != 1 || n.Escalated != 1 || n.Blocked != 2 || n.Verified+n.Failed+n.Escalated+n.Blocked+1 != n.Total {
		t.Errorf("nodes = %+v (the not_run leaf is the one left over)", n)
	}
}

func TestEveryStageEmitsEvents(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err := g.doRun(t, goodScript(g), g.fastChecker(), nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s, %v", rep.Status, err)
	}
	want := []string{"sandbox", "wave0_commit", "leaf_started", "leaf_verified", "wave_check", "landing", "sandbox"}
	i := 0
	for _, e := range g.sink.Events() {
		if i < len(want) && e.Kind == want[i] {
			i++
		}
		if e.Kind == "" || len(e.Message) > 200 || strings.Contains(e.Message, "package ") || strings.Contains(e.Message, "func ") {
			t.Errorf("event %+v is not an id or a class", e)
		}
	}
	if i != len(want) {
		var kinds []string
		for _, e := range g.sink.Events() {
			kinds = append(kinds, e.Kind)
		}
		t.Fatalf("events %v do not hold %v in order", kinds, want)
	}
}

func TestRunContextReportsCancelled(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	parent, cancel := context.WithCancel(context.Background())
	ctx, cause, stop := rc.runContext(parent)
	defer stop()
	if cause() != "" {
		t.Errorf("cause before the end = %q", cause())
	}
	cancel()
	<-ctx.Done()
	if cause() != "cancelled" {
		t.Errorf("cause = %q, want cancelled", cause())
	}

	g2 := newRig(t)
	g2.wire(goodScript(g2))
	done, cancel2 := context.WithCancel(context.Background())
	cancel2()
	rep, err := run(done, g2.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g2.fastChecker())})
	if err != nil || rep.Status != "interrupted" || rep.ExitCode != 5 {
		t.Fatalf("a cancelled run = %s exit %d, %v", rep.Status, rep.ExitCode, err)
	}
	if rd := readReportFile(t, g2); rd.Status != "interrupted" {
		t.Errorf("report.json = %s", rd.Status)
	}
}

// A plan file that changes during the run stops it failed, before the commit
// it would have guarded (TestPlanFilesImmutable in revise_test covers the
// notes overlay; this is the run-level guard).
func TestPlanChangeStopsRunFailed(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	fc := g.fastChecker()
	fc.Hook = func(string) {
		p := filepath.Join(g.runDir, "contracts.json")
		if raw, err := os.ReadFile(p); err == nil && !strings.HasSuffix(string(raw), " ") {
			_ = os.WriteFile(p, append(raw, ' '), 0o600)
		}
	}
	rep, err := g.doRun(t, goodScript(g), fc, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Status != "failed" || rep.StopReason != "plan_changed" {
		t.Fatalf("status %s (%s), want failed plan_changed", rep.Status, rep.StopReason)
	}
	for _, l := range g.plan.Leaves {
		if g.leafCommits(l.ID) != 0 {
			t.Errorf("%s was committed after the plan changed", l.ID)
		}
	}
}

func TestPlanChangeAtTheEndStopsRunFailed(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	fc := g.fastChecker()
	calls := 0
	fc.RepoHook = func(string) {
		calls++
		if calls == 3 { // the last wave's check: every leaf is verified and committed
			p := filepath.Join(g.runDir, "contracts.json")
			raw, _ := os.ReadFile(p)
			_ = os.WriteFile(p, append(raw, ' '), 0o600)
		}
	}
	rep, err := g.doRun(t, goodScript(g), fc, nil)
	if err != nil || rep.Status != "failed" || rep.StopReason != "plan_changed" {
		t.Fatalf("run = %s (%s), %v, want failed plan_changed", rep.Status, rep.StopReason, err)
	}
	if out := g.gitCmd("log", "--format=%s", "main"); strings.Contains(out, "gm(run):") {
		t.Error("the run landed although the plan changed")
	}
}

// Run on a state with Wave 0 done and some leaves verified continues without a
// model call or a check for a verified leaf, and a later person's answer
// replaces the gate-absent record of the escalation.
func TestResumeContinuesWithoutRepeatingCalls(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	script := goodScript(g)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = nil
	failing(fc, "fn-greet", 2)
	rep, err := g.doRun(t, script, fc, func(o *Options) { o.Gate = nil })
	if err != nil || rep.Status != "escalated" || rep.Resumed {
		t.Fatalf("first run = %s (%s) resumed %v, %v", rep.Status, rep.StopReason, rep.Resumed, err)
	}
	log := readReportFile(t, g).HumanLog
	if len(log) != 1 || log[0].AnsweredBy != human.AnsweredByGateAbsent {
		t.Fatalf("first run escalation log = %+v", log)
	}

	// Second run: fn-farewell must not be called or checked again (an unscripted
	// call fails the test); fn-greet is retried by a person.
	g.wire(Script{
		"implement:fn-greet": {reply(good("fn-greet"))},
		"implement:fn-bye":   {reply(good("fn-bye"))}, "implement:fn-hello": {reply(good("fn-hello"))}, "implement:fn-serve": {reply(good("fn-serve"))},
	})
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry}}
	g.gate.by = human.AnsweredByHuman
	fc2 := newFakeChecker(g)
	fc2.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, id := range []string{"fn-greet", "fn-bye", "fn-hello", "fn-serve"} {
		fc2.LeafScript[id] = []runner.Verdict{passVerdict()}
	}
	rep, err = run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc2)})
	if err != nil || rep.Status != "verified" || !rep.Resumed {
		t.Fatalf("second run = %s (%s) resumed %v, %v, failures %v", rep.Status, rep.StopReason, rep.Resumed, err, rep.Failures)
	}
	if n := len(g.leafCalls("fn-farewell")); n != 0 {
		t.Errorf("a verified leaf was called %d times on resume", n)
	}
	if n := fc2.checks("fn-farewell"); n != 0 {
		t.Errorf("a verified leaf was checked %d times on resume", n)
	}
	log = readReportFile(t, g).HumanLog
	if len(log) != 1 || log[0].AnsweredBy != human.AnsweredByHuman || log[0].Answers != 2 {
		t.Errorf("escalation log after the person answered = %+v, want one record answered by human (2 answers)", log)
	}
}

func TestRunLeavesNoCanaryInAnyStore(t *testing.T) {
	t.Parallel()
	g := newRig(t, repairRig(2))
	script := goodScript(g)
	// A failing first reply holding reply text, a failing test printing output
	// and the secret, and a failing integration check printing compiler output.
	script["implement:fn-greet"] = []step{reply(variant(bad("fn-greet", 1), 1) + "\n// CANARY-REPLY-TEXT\n"), reply(good("fn-greet")), reply(variant(good("fn-greet"), 9))}
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{
		failVerdict("test_fail", "CANARY-TEST-OUTPUT "+canarySecret, "TestA_"+canarySecret),
		passVerdict(), passVerdict(),
	}
	a := g.plan.Leaf("fn-greet")
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 4)}, Out: outputOf("CANARY-COMPILER " + canarySecret)}}
	rep, err := g.doRun(t, script, fc, nil)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, err, rep.Failures)
	}
	if rep.Repairs != 1 {
		t.Errorf("repairs = %d, want 1", rep.Repairs)
	}
	for _, c := range []string{canarySecret, "CANARY-REPLY-TEXT", "CANARY-TEST-OUTPUT", "CANARY-COMPILER"} {
		if hasCanary(t, g, c) {
			t.Errorf("%s reached a store", c)
		}
	}
	if b, err := json.Marshal(rep); err != nil || strings.Contains(string(b), "CANARY") {
		t.Errorf("the report holds a canary: %v", err)
	}
}

// failRun is a checker whose every command exits 1 (the leaf and wave checks
// still pass): no acceptance stage can pass on it.
type failRun struct{ *fakeChecker }

func (failRun) Run(context.Context, runner.Spec) runner.Result { return runner.Result{ExitCode: 1} }

// A finished build whose acceptance cannot run is never verified and never lands.
func TestRunWithoutAcceptanceNeverVerified(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	rep, err := run(context.Background(), g.options(), runFlags{afterStart: useChecker(failRun{g.fastChecker()})})
	if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_build" || rep.ExitCode != 1 {
		t.Fatalf("run = %s (%s) exit %d, %v", rep.Status, rep.StopReason, rep.ExitCode, err)
	}
	if out := g.gitCmd("log", "--format=%s", "main"); strings.Contains(out, "gm(run):") {
		t.Error("a run without acceptance landed a final commit on main")
	}
}

// A fault of the harness in the middle of the run still writes the report.
func TestHarnessFaultMidRunStillWritesReport(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	fc := g.fastChecker()
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassHarness, Err: errors.New("sandbox refused")}}
	rep, err := g.doRun(t, goodScript(g), fc, nil)
	if err == nil {
		t.Fatal("a harness fault returned no error")
	}
	if rep.Status != "failed" || rep.StopReason != "harness_fault" {
		t.Errorf("report = %s (%s)", rep.Status, rep.StopReason)
	}
	if rd := readReportFile(t, g); rd.StopReason != "harness_fault" {
		t.Errorf("report.json = %+v", rd.StopReason)
	}
}

// diff_only commits nothing, so a failed repair puts back the verified file
// from memory.
func TestDiffOnlyFailedRepairKeepsVerifiedFile(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		repairRig(2)(o)
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: diff_only", 1) }
	})
	script := goodScript(g)
	script["implement:fn-greet"] = []step{reply(good("fn-greet")), reply(variant(bad("fn-greet", 1), 1)), reply(variant(bad("fn-greet", 1), 2))}
	fc := g.fastChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), failVerdict("test_fail", "x", "TestA"), failVerdict("test_fail", "x", "TestA")}
	a := g.plan.Leaf("fn-greet")
	fc.RepoScript.Test["./"+a.Dir] = []runner.Verdict{{Class: runner.ClassTestFail, Names: []string{a.TestFunc}}}
	g.wire(script)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(fc)})
	if err != nil || rep.Status != "failed" {
		t.Fatalf("run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	got, err := os.ReadFile(g.realPath(a))
	if err != nil || string(got) != good("fn-greet") {
		t.Fatalf("the verified file was lost after a failed repair in diff_only: %v", err)
	}
	if fileExists(g.stubPath(a)) {
		t.Error("the stub came back over the verified file")
	}
}

// A leaf that was reopened or interrupted (a row past pending) whose
// dependencies are all verified is unfinished work, never "not_run".
func TestUnfinishedInterruptedLeafNotLabelledNotRun(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc := g.newRC(t)
	rows := []blackboard.Row{
		{NodeID: "fn-greet", Status: blackboard.StatusVerified},
		{NodeID: "fn-farewell", Status: blackboard.StatusVerified},
		{NodeID: "fn-hello", Status: blackboard.StatusInProgress, Revision: 1},
		{NodeID: "fn-bye", Status: blackboard.StatusReady},
		{NodeID: "fn-serve", Status: blackboard.StatusPending},
	}
	_, failures := rc.unfinished(rows, "cancelled")
	got := map[string]string{}
	for _, f := range failures {
		id, reason, _ := strings.Cut(f, ": ")
		got[id] = reason
	}
	if strings.HasPrefix(got["fn-hello"], "not_run") || !strings.HasPrefix(got["fn-hello"], "interrupted") {
		t.Errorf("an in-progress leaf is labelled %q", got["fn-hello"])
	}
	if got["fn-bye"] != "not_run: cancelled" {
		t.Errorf("an untouched ready leaf is labelled %q", got["fn-bye"])
	}
}

func TestRunRefusesRepairRoundsBelowOneBeforeAnyCall(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.wire(goodScript(g))
	g.cfg.Executor.RepairRounds = 0
	if _, err := run(context.Background(), g.options(), runFlags{}); err == nil {
		t.Fatal("repair_rounds 0 was accepted")
	}
	if n := len(g.leafCalls("fn-greet")); n != 0 {
		t.Errorf("%d model calls before the refusal", n)
	}
}
