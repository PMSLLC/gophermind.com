package executor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

const forgedSrc = "package greet\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc init() {\n\tfmt.Println(\"PASS\")\n\tos.Exit(0)\n}\n\nfunc Greet(name string) (string, error) { return \"\", nil }\n"

// A file left on disk is held to the gate a reply passes: the reply gate, the
// import policy and the source scan. Nothing the gate refuses is adopted.
func TestAdoptRefusesWhatTheGateRefuses(t *testing.T) {
	execImport := strings.Replace(good(leafID), `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)", 1)
	execImport = strings.Replace(execImport, "name = strings.TrimSpace(name)", "_ = exec.Command\n\tname = strings.TrimSpace(name)", 1)
	for _, tc := range []struct {
		name, src, sentence string
	}{
		{"forged pass", forgedSrc, "does not satisfy the contract"},
		{"forbidden import", execImport, "Allowed imports:"},
		{"not a signature match", "package greet\n\nfunc Greet(name string) string { return name }\n", "does not satisfy the contract"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
			l := g.plan.Leaf(leafID)
			if err := os.Remove(g.stubPath(l)); err != nil {
				t.Fatal(err)
			}
			write(t, g.realPath(l), tc.src)
			fc.LeafScript[leafID] = []runner.Verdict{passVerdict()} // the checker would pass the planted file
			out := runOne(t, rc, leafID)
			if out.Status != blackboard.StatusVerified {
				t.Fatalf("outcome = %+v, want verified by the model's reply", out)
			}
			if fc.checks(leafID) != 1 {
				t.Fatalf("checks = %d, want 1: the planted file must not be checked", fc.checks(leafID))
			}
			if n := len(g.fake.Requests()); n != 1 {
				t.Fatalf("provider calls = %d, want 1: the planted file was not adopted", n)
			}
			if !strings.Contains(userText(g.leafCalls(leafID)[0]), tc.sentence) {
				t.Errorf("the first prompt does not carry %q", tc.sentence)
			}
			raw, _ := os.ReadFile(g.realPath(l))
			if strings.Contains(string(raw), "os.Exit") || strings.Contains(string(raw), "os/exec") {
				t.Fatal("the planted file was committed")
			}
			if len(g.sink.OfKind("adopt_refused")) != 1 {
				t.Fatalf("adopt_refused events = %d, want 1", len(g.sink.OfKind("adopt_refused")))
			}
		})
	}
}

// The terminal status and its reason survive a restart: they are in
// _state/leaf-results.json, and a run rebuilt from disk has the reason.
func TestTerminalReasonSurvivesRestart(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = 1 } })
	var steps []step
	for i := 1; i <= 4; i++ {
		steps = append(steps, reply(variant(bad(leafID, 1), i)))
	}
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: steps}, true)
	for i := 0; i < 4; i++ {
		fc.LeafScript[leafID] = append(fc.LeafScript[leafID], failVerdict("test_fail", "x", "TestX"))
	}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusFailed {
		t.Fatalf("outcome = %+v", out)
	}
	res, err := LoadLeafResults(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if res[leafID].Status != string(blackboard.StatusFailed) || res[leafID].Reason != "ladder_exhausted" {
		t.Fatalf("persisted result = %+v", res[leafID])
	}
	rc2, err := newRunCtx(context.Background(), g.options())
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.close()
	if got := rc2.rep.reasons[leafID]; got != "ladder_exhausted" {
		t.Fatalf("reason after a restart = %q, want ladder_exhausted", got)
	}
}

// With nothing charged, a revision that ended on the provider or on the
// configuration is an interruption (resumable, claim released), never a leaf
// failure, and it names the true class.
func TestNothingChargedIsNotALeafFailure(t *testing.T) {
	rl := step{Err: provider.ErrRateLimited{}}
	auth := step{Err: provider.ErrAuth{Provider: "x"}}
	to := step{Err: context.DeadlineExceeded}
	prose := reply("Sure, I would do it like this.")
	for _, tc := range []struct {
		name   string
		steps  []step
		reason string // "" means a leaf failure is expected
	}{
		{"cooldown then auth", []step{rl, auth}, "provider_unavailable"},
		{"auth then cooldown", []step{auth, rl}, "provider_unavailable"},
		{"cooldown then malformed", []step{rl, prose, prose}, "provider_unavailable"},
		{"timeout then auth", []step{to, auth}, "provider_unavailable"},
		{"auth on every entry", []step{auth, auth}, "auth"},
		{"auth then malformed", []step{auth, prose, prose}, "auth"},
		{"malformed on every entry", []step{prose, prose, prose, prose}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			rc, _ := g.leafRC(t, Script{"implement:" + leafID: tc.steps}, true)
			out, err := rc.runLeaf(context.Background(), g.plan.Leaf(leafID), leafIn{})
			if err != nil {
				t.Fatalf("runLeaf: %v", err)
			}
			row := g.row(t, leafID)
			if tc.reason == "" {
				if out.Status != blackboard.StatusFailed || out.Interrupted {
					t.Fatalf("outcome = %+v, want a leaf failure", out)
				}
				return
			}
			if !out.Interrupted || out.Reason != tc.reason || out.Status == blackboard.StatusFailed {
				t.Fatalf("outcome = %+v, want Interrupted with reason %s", out, tc.reason)
			}
			if row.Status != blackboard.StatusReady || row.Claim != nil {
				t.Fatalf("row = %s claim %v, want ready and unclaimed", row.Status, row.Claim)
			}
			for _, a := range row.Attempts {
				if a.Verdict == blackboard.VerdictFail {
					t.Fatalf("attempt %+v was charged", a)
				}
			}
			if r := rc.rep.reasons[leafID]; r != "" {
				t.Fatalf("an interrupted leaf got the terminal reason %q", r)
			}
		})
	}
}

// A refused reply that repeats is detected as identical before the gate, so
// it does not burn every fix attempt.
func TestIdenticalRefusedReplyAbandonsEntry(t *testing.T) {
	execImport := strings.Replace(good(leafID), `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)", 1)
	execImport = strings.Replace(execImport, "name = strings.TrimSpace(name)", "_ = exec.Command\n\tname = strings.TrimSpace(name)", 1)
	fileHeader := "// FILE: internal/greet/other.go\npackage greet\n\nfunc NEVERWRITTEN() {}\n"
	for _, tc := range []struct{ name, src, class string }{
		{"forbidden form", fileHeader, "forbidden_write"},
		{"forbidden import", execImport, "import_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(tc.src), reply(tc.src), reply(good(leafID))}}, true)
			fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
			out := runOne(t, rc, leafID)
			if out.Status != blackboard.StatusVerified {
				t.Fatalf("outcome = %+v", out)
			}
			r := reasonsOf(t, g.board, leafID)
			if len(r) != 3 || r[0] != tc.class || r[1] != "identical_reply" {
				t.Fatalf("reasons = %v, want %s then identical_reply then the pass", r, tc.class)
			}
			var onA, onB int
			for _, c := range implementCalls(t, g.led, leafID) {
				if c.Provider == "a" {
					onA++
				} else {
					onB++
				}
			}
			if onA != 2 || onB != 1 {
				t.Fatalf("calls a=%d b=%d, want 2 and 1", onA, onB)
			}
		})
	}
}

// A test file edited after planning stops the leaf with a fixed reason before
// any model call.
func TestTestFileEditedAfterPlanningStopsLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	l := g.plan.Leaf(leafID)
	p := g.repo + "/" + l.TestFile
	raw, _ := os.ReadFile(p)
	write(t, p, string(raw)+"\n// EDITED-SECRET-TEXT\n")
	out, err := rc.runLeaf(context.Background(), l, leafIn{})
	var se *stopError
	if !errors.As(err, &se) || se.Status != "failed" || se.Reason != "test_file_changed" {
		t.Fatalf("err = %v, want a stopError{failed, test_file_changed}", err)
	}
	if strings.Contains(err.Error(), "EDITED-SECRET-TEXT") {
		t.Fatal("the error quotes the test file")
	}
	if out.Status != blackboard.StatusFailed || out.Reason != "test_file_changed" {
		t.Fatalf("outcome = %+v", out)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("provider calls = %d, want 0", n)
	}
	if g.row(t, leafID).Status != blackboard.StatusFailed {
		t.Fatal("the row is not terminal")
	}
}

// The reply gate refuses a go:generate directive and an import "C" before
// anything is written (the source scan behind it is defense in depth).
func TestDirectiveAndCgoRepliesNeverWritten(t *testing.T) {
	gen := strings.Replace(good(leafID), "// Greet trims", "//go:generate echo hi\n// Greet trims", 1)
	cgo := strings.Replace(good(leafID), `import "strings"`, "import \"C\"\nimport \"strings\"", 1)
	for name, src := range map[string]string{"generate": gen, "cgo": cgo} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t)
			rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(src), reply(src), reply(src), reply(src)}}, true)
			out := runOne(t, rc, leafID)
			if out.Status == blackboard.StatusVerified || fc.checks(leafID) != 0 || g.leafCommits(leafID) != 0 {
				t.Fatalf("outcome %+v checks %d commits %d: a forbidden directive was accepted", out, fc.checks(leafID), g.leafCommits(leafID))
			}
			if fileExists(g.realPath(g.plan.Leaf(leafID))) || !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
				t.Fatal("the stub is not in place")
			}
			for _, a := range attemptsOf(t, g.board, leafID) {
				if a.Verdict == blackboard.VerdictPass || !strings.HasPrefix(a.FailureReason, "malformed") {
					t.Errorf("attempt %+v, want a malformed error", a)
				}
			}
		})
	}
}

// Cancel during the provider call, not the checker: no fix attempt is
// consumed, no terminal failed status, the leaf is claimable again.
func TestCancelDuringProviderCall(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {{Delay: 400 * time.Millisecond}}}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	out, err := rc.runLeaf(ctx, g.plan.Leaf(leafID), leafIn{})
	if err != nil || !out.Interrupted {
		t.Fatalf("runLeaf = %+v, %v; want Interrupted", out, err)
	}
	row := g.row(t, leafID)
	if row.Status != blackboard.StatusReady || row.Claim != nil || len(row.Attempts) != 0 {
		t.Fatalf("row = %+v, want ready, unclaimed, no attempt", row)
	}
	if fc.checks(leafID) != 0 || !fileExists(g.stubPath(g.plan.Leaf(leafID))) {
		t.Fatal("the leaf was checked or lost its stub")
	}
	if ok, _ := g.board.Claim(context.Background(), g.id, leafID, "someone"); !ok {
		t.Fatal("the leaf is not claimable again")
	}
}

// A model escalation names only a model that was called, once per model per leaf.
func TestEscalationOnlyForCallsMade(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) {
		o.Settings = func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 600 } // a/m1 cannot hold the prompt
	})
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	escs, _ := LoadEscalations(g.runDir)
	if len(escs) != 1 || escs[0].Model != "b/m2" {
		t.Fatalf("escalations = %+v, want only b/m2 (a/m1 was never called)", escs)
	}
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	e := rc.ladderEntries(g.plan.Leaf(leafID))[1]
	for i := 0; i < 2; i++ {
		if err := lr.noteEscalation(e); err != nil {
			t.Fatal(err)
		}
	}
	if escs, _ = LoadEscalations(g.runDir); len(escs) != 1 {
		t.Fatalf("escalations = %+v, want b/m2 once per leaf however often it is noted", escs)
	}
}

// A truncated reply (after the router's one growth) is labelled truncated,
// not timeout.
func TestProviderReasonIsTheRealOne(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {{Err: provider.ErrTruncated{Provider: "a"}}, {Err: provider.ErrTruncated{Provider: "a"}}, reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if r := reasonsOf(t, g.board, leafID); r[0] != "truncated" {
		t.Fatalf("reasons = %v, want truncated first", r)
	}
}

// With no proxy (an offline run) there is no critical-host evidence: the
// check does not look for any, and the leaf verifies on its merits.
func TestNoProxyMeansNoCriticalHostCheck(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	rc.prox = nil
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	out := runOne(t, rc, leafID)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if rc.criticalSince(leafID, time.Now().Add(-time.Hour)) != nil || rc.warningsSince(leafID, time.Now().Add(-time.Hour)) != nil {
		t.Fatal("a nil proxy reported failures")
	}
}
