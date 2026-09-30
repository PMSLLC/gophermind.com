package executor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/runner"
)

// planUntouched fails the test unless every node file and contracts.json still
// hash to what Plan.Hashes recorded at load.
func planUntouched(t *testing.T, g *rig, rc *runCtx) {
	t.Helper()
	fresh, err := LoadPlan(g.runDir, g.repo)
	if err != nil {
		t.Fatalf("the plan no longer loads: %v", err)
	}
	if !reflect.DeepEqual(fresh.Hashes, rc.plan.Hashes) {
		t.Fatalf("plan files changed:\n now %v\n was %v", fresh.Hashes, rc.plan.Hashes)
	}
}

// oneShot is one entry, one fix (two attempts per revision, the least the
// settings allow) and rev revisions.
func oneShot(rev int) func(*rigOpts) { return oneEntryRevisions(1, rev) }

func TestReviseNotesOnlyContractsUntouched(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(2))
	id := leafID
	script := Script{
		"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))},
		// The parser allows 5 notes at most; the router repairs a reply with 6.
		"revise:" + id: {
			reply(`{"notes":["h1","h2","h3","h4","h5","h6"]}`),
			reply(`{"notes":["n2","n3","n4","n5","n6"]}`),
		},
	}
	rc, fc := g.leafRC(t, script, true)
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestA"), failVerdict("test_fail", "x", "TestA"), passVerdict()}
	if err := AddNote(g.runDir, id, "older human hint"); err != nil {
		t.Fatal(err)
	}

	out := runOne(t, rc, id)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified after one revision", out)
	}
	notes, err := LoadNotes(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"n2", "n3", "n4", "n5", "n6"}; !reflect.DeepEqual(notes[id], want) {
		t.Fatalf("notes = %v, want the last 5 %v", notes[id], want)
	}
	calls := g.leafCalls(id)
	if len(calls) != 5 { // implement x2, revise (malformed), revise (repair), implement
		t.Fatalf("calls = %v", stagesOf(g.fake))
	}
	last := userText(calls[len(calls)-1])
	for _, n := range []string{"n2", "n6"} {
		if !strings.Contains(last, n) {
			t.Errorf("the next implement prompt lacks note %s", n)
		}
	}
	if strings.Contains(last, "older human hint") {
		t.Error("a note beyond the last 5 is still in the prompt")
	}
	if r := g.row(t, id); r.Revision != 1 {
		t.Fatalf("revision = %d, want 1", r.Revision)
	}
	escs, err := LoadEscalations(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	var rev int
	for _, e := range escs {
		if e.Kind == "revision" {
			rev++
			if e.TaskType != "revise" || e.NodeID != id || e.Model != "a/m1" {
				t.Errorf("revision escalation = %+v", e)
			}
		}
	}
	if rev != 1 {
		t.Fatalf("revision escalations = %d, want 1", rev)
	}
	planUntouched(t, g, rc)
}

func TestPlanFilesImmutable(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: distinct(id, 4),
		"revise:" + id:    {reviseHint("try again")},
	}, true)
	failing(fc, id, 4)
	_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	planUntouched(t, g, rc)
}

func TestContractProblemOnce(t *testing.T) {
	t.Parallel()
	id := leafID
	cp := reply("CONTRACT_PROBLEM: the signature cannot return that")
	t.Run("implement then a good revise", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, oneShot(2))
		rc, fc := g.leafRC(t, Script{
			"implement:" + id: {cp, reply(good(id))},
			"revise:" + id:    {reviseHint("use the signature as given")},
		}, true)
		fc.LeafScript[id] = []runner.Verdict{passVerdict()}
		if out := runOne(t, rc, id); out.Status != blackboard.StatusVerified {
			t.Fatalf("outcome = %+v", out)
		}
		if got := stagesOf(g.fake); strings.Join(got, " ") != strings.Join([]string{"implement:" + id, "revise:" + id, "implement:" + id}, " ") {
			t.Fatalf("stages = %v", got)
		}
		if reasons := reasonsOf(t, g.board, id); !strings.HasPrefix(reasons[0], ClassContractProblem) {
			t.Fatalf("reasons = %v", reasons)
		}
	})
	t.Run("implement then revise both say contract problem", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, oneShot(2))
		rc, _ := g.leafRC(t, Script{
			"implement:" + id: {cp},
			"revise:" + id:    {reply("CONTRACT_PROBLEM: still impossible")},
		}, true)
		_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
		asStop(t, err, "escalated", "human_stop")
		if got := stagesOf(g.fake); strings.Join(got, " ") != "implement:"+id+" revise:"+id {
			t.Fatalf("stages = %v, want one implement and one revise, nothing after", got)
		}
		if escs := g.gate.Escalations(); len(escs) != 1 || escs[0].Reason != ClassContractProblem {
			t.Fatalf("gate escalations = %+v", escs)
		}
		// The sentence of the model is in memory only.
		if hasCanary(t, g, "still impossible") {
			t.Error("the revise contract problem sentence was persisted")
		}
	})
}

func TestGateRetryAddsNoteAndRaisesAllowance(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: append(distinct(id, 4), reply(good(id))),
		"revise:" + id:    {reviseHint("model hint")},
	}, true)
	failing(fc, id, 4)
	fc.LeafScript[id] = append(fc.LeafScript[id], passVerdict())
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "loop over the runes"}}
	tb := traced(rc)

	out := runOne(t, rc, id)
	if out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, want verified", out)
	}
	st, err := LoadState(g.runDir)
	if err != nil || st.ExtraRevisions[id] != 1 {
		t.Fatalf("extra_revisions = %v, %v; want 1 for %s", st.ExtraRevisions, err, id)
	}
	trace := strings.Join(tb.Trace(), ",")
	want := id + " in_progress->needs_revision," + id + " needs_revision->escalated," + id + " escalated->ready,claim " + id + "," + id + " claimed->in_progress"
	if !strings.Contains(trace, want) {
		t.Fatalf("trace = %s\nwant it to contain %s", trace, want)
	}
	stages := stagesOf(g.fake)
	if strings.Join(stages, " ") != strings.Join([]string{"implement:" + id, "implement:" + id, "implement:" + id, "implement:" + id, "revise:" + id, "implement:" + id}, " ") {
		t.Fatalf("stages = %v, want the ladder again after the retry, then one revise and a last pass", stages)
	}
	calls := g.leafCalls(id)
	if strings.Contains(userText(calls[1]), "loop over the runes") || !strings.Contains(userText(calls[2]), "loop over the runes") {
		t.Error("the human note is not in the prompt after the retry")
	}
	if r := g.row(t, id); r.Status != blackboard.StatusVerified || r.Revision != 1 {
		t.Fatalf("row = %s revision %d", r.Status, r.Revision)
	}
	escs, _ := LoadEscalations(g.runDir)
	var human, revision int
	for _, e := range escs {
		switch e.Kind {
		case "human":
			human++
		case "revision":
			revision++
		}
	}
	if human != 1 || revision != 1 {
		t.Fatalf("escalations human %d revision %d, want 1 and 1", human, revision)
	}
}

func TestGateSkipFailsRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	if err != nil || out.Status != blackboard.StatusFailed || out.Reason != "skipped by human" {
		t.Fatalf("runLeaf = %+v, %v; want failed, skipped by human", out, err)
	}
	if r := g.row(t, id); r.Status != blackboard.StatusFailed {
		t.Fatalf("row = %s", r.Status)
	}
	res, err := LoadLeafResults(g.runDir)
	if err != nil || res[id].Status != "failed" || res[id].Reason != "skipped by human" {
		t.Fatalf("persisted result = %+v, %v", res[id], err)
	}
	if rc.rep.reasons[id] != "skipped by human" {
		t.Fatalf("report reason = %q", rc.rep.reasons[id])
	}
	if !fileExists(g.stubPath(g.plan.Leaf(id))) {
		t.Error("the stub was not restored for a skipped leaf")
	}
}

func TestNilGateStops(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	rc.o.Gate = nil
	_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	if r := g.row(t, id); r.Status != blackboard.StatusEscalated {
		t.Fatalf("row = %s, want escalated", r.Status)
	}
	if res, _ := LoadLeafResults(g.runDir); res[id].Status != "escalated" || res[id].Reason == "" {
		t.Fatalf("persisted result = %+v", res[id])
	}
}

func TestGateWaitingExit3(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 2)}, true)
	failing(fc, id, 2)
	g.gate.waiting = true
	_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "waiting_on_human")
	if r := g.row(t, id); r.Status != blackboard.StatusEscalated || r.Claim != nil {
		t.Fatalf("row = %+v, want escalated and unclaimed", r)
	}
}

// The gate is given ids, a class and class-and-name history lines: never reply
// text, command output or a secret. No store holds them either.
func TestEscalationCarriesNoReplyTextOrSecret(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	id := leafID
	replyCanary := variant(bad(id, 1), 1) + "\n// CANARY-reply-text\n"
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: {reply(replyCanary), reply(variant(replyCanary, 2)), reply(variant(replyCanary, 3)), reply(variant(replyCanary, 4))},
		"revise:" + id:    {reviseHint("try again")},
	}, true)
	fc.LeafScript[id] = []runner.Verdict{
		failVerdict("test_fail", "CANARY-output "+canarySecret, "TestA", "TestB_"+canarySecret),
		failVerdict("test_fail", "CANARY-output "+canarySecret, "TestA"),
		failVerdict("test_fail", "CANARY-output "+canarySecret, "TestA"),
		failVerdict("test_fail", "CANARY-output "+canarySecret, "TestA"),
	}
	_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")
	escs := g.gate.Escalations()
	if len(escs) != 1 {
		t.Fatalf("escalations = %d", len(escs))
	}
	blob := escs[0].NodeID + escs[0].Reason + strings.Join(escs[0].History, "\n")
	for _, c := range []string{"CANARY-reply-text", "CANARY-output", canarySecret} {
		if strings.Contains(blob, c) {
			t.Errorf("the gate was given %q", c)
		}
	}
	for _, r := range g.fake.Requests() {
		if strings.Contains(userText(r), canarySecret) {
			t.Error("a prompt holds the secret value")
		}
	}
	for _, c := range []string{"CANARY-reply-text", "CANARY-output"} {
		if hasCanary(t, g, c) {
			t.Errorf("%s reached a persisted store", c)
		}
	}
}

// A revise call sees node scope only, through the strong tier and the real
// task type, and the models asked are the router's choice (no Only).
func TestReviseCallShape(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: {reply(variant(bad(id, 1), 1)), reply(variant(bad(id, 1), 2)), reply(good(id))},
		"revise:" + id:    {reviseHint("x")},
	}, true)
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestA"), failVerdict("test_fail", "x", "TestA"), passVerdict()}
	runOne(t, rc, id)
	rows, err := g.led.List(context.Background(), g.id, ledger.Filter{Stage: "revise:" + id})
	if err != nil || len(rows) != 1 {
		t.Fatalf("revise rows = %d, %v", len(rows), err)
	}
	r := rows[0]
	if r.TaskType != "revise" || r.Scope != "node" || r.Tier != "strong" || r.NodeID != id {
		t.Fatalf("revise row = %+v", r)
	}
}
