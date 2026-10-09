package executor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// fakeReviser answers Revise from a queue of steps and records the requests.
type fakeReviser struct {
	mu    sync.Mutex
	steps []func(planner.ReviseRequest) (planner.ReviseProposal, error)
	reqs  []planner.ReviseRequest
}

func (f *fakeReviser) Revise(_ context.Context, req planner.ReviseRequest) (planner.ReviseProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if len(f.steps) == 0 {
		return planner.ReviseProposal{}, errors.New("fakeReviser: no step left")
	}
	st := f.steps[0]
	f.steps = f.steps[1:]
	return st(req)
}

func (f *fakeReviser) calls() []planner.ReviseRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]planner.ReviseRequest(nil), f.reqs...)
}

func answer(p planner.ReviseProposal) func(planner.ReviseRequest) (planner.ReviseProposal, error) {
	return func(planner.ReviseRequest) (planner.ReviseProposal, error) { return p, nil }
}

func unusable(class string) func(planner.ReviseRequest) (planner.ReviseProposal, error) {
	return answer(planner.ReviseProposal{Outcome: planner.OutcomeUnusable, Class: class})
}

func logKinds(t *testing.T, g *rig) []string {
	t.Helper()
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	var out []string
	for _, r := range recs {
		out = append(out, string(r.Kind)+":"+r.Class)
	}
	return out
}

func TestRewriteRestartsTheLadderFromTheRevisedNode(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))}}, true)
	fr := &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	rc.rev = fr
	// attempt 1, attempt 2, the red check of the new test against the stub, attempt 3
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), passVerdict()}

	out := runOne(t, rc, id)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified at revision 1", out)
	}
	reqs := fr.calls()
	if len(reqs) != 1 {
		t.Fatalf("Revise calls = %d, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Trigger != planner.TriggerLadderExhausted || r.Revision != 0 || r.Remaining != 2 || r.NodeID != id || len(r.History) != 2 ||
		!strings.Contains(r.History[0], "test_fail") || !strings.Contains(r.LastFailure, "TestGreet") || r.TestSHA256 == "" || r.TestSource == "" || r.Problem != "" {
		t.Fatalf("request = %+v", r)
	}
	calls := g.leafCalls(id)
	if len(calls) != 3 {
		t.Fatalf("implement calls = %v", stagesOf(g.fake))
	}
	last := userText(calls[2])
	for _, want := range []string{"REVISED-SENTENCE for the greeting.", "trims tabs and newlines", "<behaviour>"} {
		if !strings.Contains(last, want) {
			t.Errorf("the first prompt after the rewrite lacks %q", want)
		}
	}
	if first := userText(calls[0]); strings.Contains(first, "<behaviour>") || strings.Contains(first, "REVISED-SENTENCE") {
		t.Error("the prompt before the rewrite carries revised text")
	}
	if row := g.row(t, id); row.Revision != 1 || len(row.Attempts) != 3 || row.Attempts[2].Revision != 1 || row.Attempts[1].Revision != 0 {
		t.Fatalf("row revision %d attempts %+v", row.Revision, row.Attempts)
	}
	if files := fc.files[id]; len(files) != 4 || files[2][0] != rc.plan.Leaf(id).StubFile {
		t.Fatalf("checked files = %v, want the red check on the stub third", files)
	}
}

func TestFlaggedAndSplitLeavesGoToTheGateWithTheProposalAndKeepTheirRevision(t *testing.T) {
	for _, c := range []struct{ class, paragraph string }{
		{planner.ClassContractFlagged, "The function must also return the number of runes."},
		{planner.ClassSplitRequired, "Trimming and formatting are two jobs."},
		{planner.ClassTestsLocked, "The change needs new tests and revise.rewrite_tests is off."},
	} {
		c := c
		t.Run(c.class, func(t *testing.T) {
			t.Parallel()
			g := newRig(t, reviseRewrite(2))
			id := leafID
			rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
			failing(fc, id, 2)
			rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
				answer(planner.ReviseProposal{Outcome: planner.OutcomeFlagged, Class: c.class, Paragraph: c.paragraph}),
			}}
			_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
			asStop(t, err, "escalated", "human_stop")
			seen := g.gate.Escalations()
			if len(seen) != 1 || seen[0].Reason != c.class || seen[0].Proposal != c.paragraph || seen[0].Revisions != 0 {
				t.Fatalf("gate saw %+v", seen)
			}
			if row := g.row(t, id); row.Revision != 0 || row.Status != blackboard.StatusEscalated {
				t.Fatalf("row revision %d status %s: a flag consumes no revision", row.Revision, row.Status)
			}
			if got := logKinds(t, g); len(got) != 1 || got[0] != "flag:"+c.class {
				t.Fatalf("log = %v", got)
			}
			if res, _ := LoadLeafResults(g.runDir); res[id].Reason != c.class {
				t.Fatalf("leaf result = %+v", res[id])
			}
			if se := rc.planIntact(); se != nil {
				t.Fatalf("a flagged leaf changed the plan: %+v", se)
			}
		})
	}
}

func TestRefusalsRepeatTheCallThenEscalateWithTheLastClass(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name           string
		maxRefusals    int
		classes        []string
		wantCalls      int
		wantReason     string
		wantRefusedNow []string // the Refused list of the last request
	}{
		{"two refusals", 2, []string{planner.ClassReviseInvalid, planner.ClassReviseNoop}, 2, planner.ClassReviseNoop, []string{planner.ClassReviseInvalid}},
		{"one is the limit", 1, []string{planner.ClassSecretInReply}, 1, planner.ClassSecretInReply, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, reviseRewrite(2), func(o *rigOpts) {
				prev := o.Settings
				o.Settings = func(cfg *settings.Config) { prev(cfg); cfg.Revise.MaxRefusals = c.maxRefusals }
			})
			id := leafID
			rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
			failing(fc, id, 2)
			fr := &fakeReviser{}
			for _, class := range c.classes {
				fr.steps = append(fr.steps, unusable(class))
			}
			rc.rev = fr
			_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
			asStop(t, err, "escalated", "human_stop")
			reqs := fr.calls()
			if len(reqs) != c.wantCalls || strings.Join(reqs[len(reqs)-1].Refused, ",") != strings.Join(c.wantRefusedNow, ",") {
				t.Fatalf("Revise calls %d (refused list %v), want %d with %v", len(reqs), reqs[len(reqs)-1].Refused, c.wantCalls, c.wantRefusedNow)
			}
			if seen := g.gate.Escalations(); len(seen) != 1 || seen[0].Reason != c.wantReason {
				t.Fatalf("gate saw %+v, want reason %s", seen, c.wantReason)
			}
			if row := g.row(t, id); row.Revision != 0 {
				t.Fatalf("a refusal consumed a revision (%d)", row.Revision)
			}
			if got := logKinds(t, g); len(got) != c.wantCalls || !strings.HasPrefix(got[0], "refused:") {
				t.Fatalf("log = %v", got)
			}
		})
	}
}

func TestARefusedCallThenAGoodRewriteStillVerifies(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))}}, true)
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		unusable(planner.ClassReviseInvalid), answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), passVerdict()}
	if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if got := logKinds(t, g); strings.Join(got, ",") != "refused:revise_invalid,intent:,commit:" {
		t.Fatalf("log = %v", got)
	}
}

func TestMaxRevisionsZeroNeverCallsTheReviserAndARetryGrantsOne(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: append(distinct(id, 4), reply(good(id)))}, true)
	fr := &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	rc.rev = fr
	failing(fc, id, 4)
	fc.LeafScript[id] = append(fc.LeafScript[id], failVerdict("test_fail", "x", "TestGreet"), passVerdict()) // the red check, then the pass
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "mind the tabs"}}

	if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	seen := g.gate.Escalations()
	if len(seen) != 1 || seen[0].Revisions != 0 || len(fr.calls()) != 1 {
		t.Fatalf("gate %+v, Revise calls %d: the first ladder must end at the gate with no rewrite, the retry grants one", seen, len(fr.calls()))
	}
	if row := g.row(t, id); row.Revision != 1 {
		t.Fatalf("revision = %d, want 1", row.Revision)
	}
}

func TestNotesModeNeverCallsTheReviser(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1)) // the rig is pinned to notes mode
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 4), "revise:" + id: {reviseHint("try again")}}, true)
	fr := &fakeReviser{} // any call is an error
	rc.rev = fr
	failing(fc, id, 4)
	_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	if n := len(fr.calls()); n != 0 {
		t.Fatalf("the planner Reviser was called %d time(s) in notes mode", n)
	}
}

func TestARepairRoundUsesNotesWhateverTheMode(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(1))
	rc, _ := g.leafRC(t, Script{}, true)
	if !(&leafRun{rc: rc}).rewriteMode() {
		t.Fatal("a normal leaf must use rewrite mode")
	}
	if (&leafRun{rc: rc, in: leafIn{Repair: 1}}).rewriteMode() {
		t.Fatal("a leaf in an integration repair round has no stub to red-check against: it must use notes")
	}
}

func TestAnIdenticalReplyAfterARewriteIsJudgedAgainstTheNewTest(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(1))
	id := leafID
	same := variant(bad(id, 1), 7)
	rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(same), reply(variant(bad(id, 1), 8)), reply(same)}}, true)
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), passVerdict()}
	if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v: the repeat of an old reply was refused as identical", out)
	}
	if got := verdictsOf(t, g.board, id); got != "fail,fail,pass" {
		t.Fatalf("verdicts = %s", got)
	}
}

func TestACanceledContextDuringReviseIsAnInterruptionNotAFailure(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	ctx, cancel := context.WithCancel(context.Background())
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		func(planner.ReviseRequest) (planner.ReviseProposal, error) {
			cancel()
			return planner.ReviseProposal{}, ctx.Err()
		},
	}}
	out, err := rc.runLeaf(ctx, rc.plan.Leaf(id), leafIn{})
	if err != nil || !out.Interrupted {
		t.Fatalf("outcome %+v err %v, want an interruption", out, err)
	}
	if row := g.row(t, id); row.Status != blackboard.StatusReady || row.Revision != 0 {
		t.Fatalf("row %s revision %d: the claim must be released and nothing charged", row.Status, row.Revision)
	}
}

func TestAReopenedLeafUsesNotesAndARewriteStartsWithNoOldRefusals(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, _ := g.leafRC(t, Script{}, true)
	if (&leafRun{rc: rc, reopened: true}).rewriteMode() {
		t.Fatal("a reopened leaf (a repair round resumed) must keep the notes rung")
	}
	lr, err := rc.newLeafRun(context.Background(), rc.plan.Leaf(id), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	lr.refusals, lr.proposal = []string{planner.ClassReviseInvalid}, "an old paragraph"
	fr := &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(planner.ReviseProposal{Outcome: planner.OutcomeFlagged, Class: planner.ClassSplitRequired, Paragraph: "the new one"}),
	}}
	rc.rev = fr
	_ = lr.rewrite(context.Background(), planner.TriggerLadderExhausted)
	if reqs := fr.calls(); len(reqs) != 1 || len(reqs[0].Refused) != 0 || reqs[0].Revision != 0 {
		t.Fatalf("requests = %+v: the refusals of an earlier escalation must not carry over", reqs)
	}
	if lr.proposal != "the new one" {
		t.Fatalf("proposal = %q", lr.proposal)
	}
}

// seedRowCounter puts the row's revision counter ahead of the node's, as the
// notes rung or a repair round does.
func seedRowCounter(t *testing.T, g *rig, id string, n int) {
	t.Helper()
	if err := g.board.SetRevision(context.Background(), g.id, id, n); err != nil {
		t.Fatal(err)
	}
}

func TestTheRequestCarriesTheNodeRevisionNotTheRowCounter(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2), func(o *rigOpts) {
		prev := o.Settings
		o.Settings = func(cfg *settings.Config) { prev(cfg); cfg.Revise.MaxRefusals = 1 }
	})
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	seedRowCounter(t, g, id, 1) // the node is at revision 0
	failing(fc, id, 2)
	fr := &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){unusable(planner.ClassReviseInvalid)}}
	rc.rev = fr
	_, err := rc.runLeaf(context.Background(), rc.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	reqs := fr.calls()
	if len(reqs) != 1 || reqs[0].Revision != 0 || reqs[0].Remaining != 2 {
		t.Fatalf("requests = %+v, want Revision 0 and Remaining 2 (the node's, not the row counter's)", reqs)
	}
	dir, _ := revlog.NodeDir(g.runDir, id)
	recs, _ := revlog.Read(dir)
	if len(recs) != 1 || recs[0].Kind != revlog.KindRefused || recs[0].N != 1 {
		t.Fatalf("log = %+v, want one refused record numbered 1", recs)
	}
}

func TestAProposalAndRefusalsDoNotSurviveARetry(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	id := leafID
	rc, _ := g.leafRC(t, Script{}, true)
	ctx := context.Background()
	if ok, err := g.board.Claim(ctx, g.id, id, "executor:"+id); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := g.board.SetStatus(ctx, g.id, id, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	lr, err := rc.newLeafRun(ctx, rc.plan.Leaf(id), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	lr.worker = "executor:" + id
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(planner.ReviseProposal{Outcome: planner.OutcomeFlagged, Class: planner.ClassContractFlagged, Paragraph: "the old paragraph"}),
	}}
	var rs *reviseStop
	if err := lr.rewrite(ctx, planner.TriggerLadderExhausted); !errors.As(err, &rs) {
		t.Fatalf("rewrite = %v, want a stop", err)
	}
	lr.refusals = []string{planner.ClassReviseInvalid}
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry}, {Action: human.ActionStop}}
	if _, done, err := lr.toHuman(ctx, rs.Reason); done || err != nil {
		t.Fatalf("toHuman (retry) done=%v err=%v", done, err)
	}
	if len(lr.refusals) != 0 {
		t.Fatalf("refusals = %v after a retry: the budget must be fresh", lr.refusals)
	}
	// the next escalation does not pass through rewrite (a contract problem after the allowance)
	_, _, _ = lr.toHuman(ctx, ClassContractProblem)
	seen := g.gate.Escalations()
	if len(seen) != 2 || seen[0].Proposal != "the old paragraph" || seen[1].Reason != ClassContractProblem || seen[1].Proposal != "" {
		t.Fatalf("gate saw %+v: the second escalation must not carry the first one's paragraph", seen)
	}
}

func TestAnOldReplyAfterARewriteIsAcceptedWhenTheRowCounterWasAhead(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(5))
	id := leafID
	last := variant(bad(id, 1), 8)
	rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(variant(bad(id, 1), 7)), reply(last), reply(last), reply(good(id))}}, true)
	seedRowCounter(t, g, id, 1) // attempts are recorded at revision 1, the revised node will be at 1 too
	rc.rev = &fakeReviser{steps: []func(planner.ReviseRequest) (planner.ReviseProposal, error){
		answer(g.proposal(t, id, "REVISED-SENTENCE for the greeting.", greetTest)),
	}}
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), failVerdict("test_fail", "x", "TestGreet"), passVerdict()}
	if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v: the repeat of a pre-rewrite reply was refused as identical", out)
	}
	if got := verdictsOf(t, g.board, id); got != "fail,fail,fail,pass" {
		t.Fatalf("verdicts = %s", got)
	}
}

func TestARewriteForgetsWhichEntriesWereTooLong(t *testing.T) {
	t.Parallel()
	g := newRig(t, reviseRewrite(2))
	rc, _ := g.leafRC(t, Script{}, true)
	lr, err := rc.newLeafRun(context.Background(), rc.plan.Leaf(leafID), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	lr.tooLong["a/m1"] = true
	if err := lr.afterRewrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(lr.tooLong) != 0 {
		t.Fatalf("tooLong = %v: the revised node may fit now", lr.tooLong)
	}
}
