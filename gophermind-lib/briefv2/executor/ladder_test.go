package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

func TestLadderEntries(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	planned := g.plan.Leaf("fn-greet")
	l := &Leaf{}
	*l = *planned
	l.Tier = "standard"

	// A leaf planned for the any tier starts on that chain.
	anyLeaf := *planned
	anyLeaf.Tier = "any"
	if got := rc.ladderEntries(&anyLeaf); len(got) == 0 || got[0].Tier != router.TierAny {
		t.Fatalf("any-tier leaf entries = %+v, want the any chain first", got)
	}

	// Standard chain in order, then the strong chain's entries not already present.
	rc.cfg.Models = map[string][]string{
		"standard": {"a/m1"},
		"strong":   {"b/m2", "a/m1", "ghost/m9"}, // ghost is not a configured provider
		"any":      {"a/m1"},
	}
	got := rc.ladderEntries(l)
	want := []ladderEntry{
		{Name: "a/m1", ContextTokens: 32768, Tier: router.TierStandard, Pos: 1},
		{Name: "b/m2", ContextTokens: 32768, Tier: router.TierStrong, Pos: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("ladderEntries = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// Deterministic.
	again := rc.ladderEntries(l)
	for i := range got {
		if got[i] != again[i] {
			t.Fatal("ladderEntries is not deterministic")
		}
	}

	// A strong leaf starts on the strong chain and adds nothing twice.
	strong := *l
	strong.Tier = "strong"
	got = rc.ladderEntries(&strong)
	if len(got) != 2 || got[0].Name != "b/m2" || got[0].Tier != router.TierStrong || got[1].Name != "a/m1" || got[1].Pos != 2 {
		t.Fatalf("strong leaf entries = %+v", got)
	}

	// An unset tier is standard; an entry whose provider is not configured is dropped.
	blank := *l
	blank.Tier = ""
	rc.cfg.Models["standard"] = []string{"ghost/m9", "a/m1"}
	got = rc.ladderEntries(&blank)
	if len(got) != 2 || got[0].Name != "a/m1" || got[0].Pos != 1 || got[0].Tier != router.TierStandard {
		t.Fatalf("blank tier entries = %+v", got)
	}

	// Identical chains give one entry per model.
	rc.cfg.Models = map[string][]string{"standard": {"a/m1", "b/m2"}, "strong": {"a/m1", "b/m2"}, "any": {"a/m1"}}
	if got = rc.ladderEntries(l); len(got) != 2 {
		t.Fatalf("identical chains gave %+v, want 2 entries", got)
	}
}

func TestEscalationAlongChain(t *testing.T) {
	t.Parallel()
	g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = 1 } })
	id := "fn-greet"
	script := Script{"implement:" + id: {{Text: bad(id, 1)}, {Text: variant(bad(id, 1), 2)}, {Text: good(id)}}}
	rc, fc := g.leafRC(t, script, true)
	fc.LeafScript[id] = append(fc.LeafScript[id], failVerdict("test_fail", "x", "TestA"), failVerdict("test_fail", "x", "TestA"), passVerdict())

	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("runLeaf = %+v, %v; want verified", out, err)
	}
	as := attemptsOf(t, g.board, id)
	if len(as) != 3 {
		t.Fatalf("attempts = %d, want 3 (a twice, b once)", len(as))
	}
	wantOrder := []int{1, 1, 2}
	wantProv := []string{"a", "a", "b"}
	for i, a := range as {
		if a.Order != wantOrder[i] || a.Provider != wantProv[i] {
			t.Errorf("attempt %d: order %d provider %s, want %d %s", i, a.Order, a.Provider, wantOrder[i], wantProv[i])
		}
	}
	escs, err := LoadEscalations(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 1 || escs[0].Kind != "model" || escs[0].TaskType != "implement" || escs[0].Model != "b/m2" || escs[0].NodeID != id {
		t.Fatalf("escalations = %+v, want one model escalation naming b/m2", escs)
	}
}

func TestIdenticalReplyAbandonsEntry(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	id := "fn-greet"
	same := bad(id, 1)
	script := Script{"implement:" + id: {{Text: same}, {Text: same}, {Text: good(id)}}}
	rc, fc := g.leafRC(t, script, true)
	fc.LeafScript[id] = []runner.Verdict{failVerdict("test_fail", "x", "TestA"), passVerdict()}

	out, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("runLeaf = %+v, %v; want verified", out, err)
	}
	// Exactly two calls to a, then b.
	var onA, onB int
	for _, c := range implementCalls(t, g.led, id) {
		switch c.Provider {
		case "a":
			onA++
		case "b":
			onB++
		}
	}
	if onA != 2 || onB != 1 {
		t.Fatalf("calls: a=%d b=%d, want 2 and 1", onA, onB)
	}
	if fc.checks(id) != 2 {
		t.Fatalf("checks = %d, want 2 (the identical reply is never checked)", fc.checks(id))
	}
	as := attemptsOf(t, g.board, id)
	if len(as) != 3 || as[1].Verdict != blackboard.VerdictFail || as[1].FailureReason != ClassIdentical || as[2].Verdict != blackboard.VerdictPass {
		t.Fatalf("attempts = %+v", as)
	}
	if as[0].ReplySHA256 == "" || as[0].ReplySHA256 != as[1].ReplySHA256 || as[2].ReplySHA256 == as[0].ReplySHA256 {
		t.Fatalf("reply hashes = %q %q %q", as[0].ReplySHA256, as[1].ReplySHA256, as[2].ReplySHA256)
	}
	if as[0].ReplySHA256 != sumHex([]byte(same)) {
		t.Fatal("ReplySHA256 is not the SHA-256 of the reply text")
	}

	// A new leaf run over the same blackboard (memory empty, as after a restart)
	// reads the last reply hash of the entry from the blackboard.
	lr, err := rc.newLeafRun(context.Background(), g.plan.Leaf(id), leafIn{})
	if err != nil {
		t.Fatal(err)
	}
	entries := rc.ladderEntries(g.plan.Leaf(id))
	if got, err := lr.previousSHA(context.Background(), entries[0]); err != nil || got != sumHex([]byte(same)) {
		t.Fatalf("previousSHA(a) = %q, want the hash of the last reply on a", got)
	}
	if got, err := lr.previousSHA(context.Background(), entries[1]); err != nil || got != as[2].ReplySHA256 {
		t.Fatalf("previousSHA(b) = %q, want the hash of the reply on b", got)
	}
}

// distinct is n failing implement replies that all differ, so no entry is
// abandoned as an identical reply.
func distinct(id string, n int) []step {
	var out []step
	for i := 1; i <= n; i++ {
		out = append(out, reply(variant(bad(id, 1), i)))
	}
	return out
}

// failing scripts n failing test verdicts for the leaf.
func failing(fc *fakeChecker, id string, n int) {
	for i := 0; i < n; i++ {
		fc.LeafScript[id] = append(fc.LeafScript[id], failVerdict("test_fail", "x", "TestA"))
	}
}

// reviseHint is a revise reply with one hint.
func reviseHint(text string) step { return reply(`{"notes":["` + text + `"]}`) }

// callSeq is the ledger rows of the leaf in order as "stage@provider", router
// repair rows (outcome malformed) left out.
func callSeq(t *testing.T, g *rig, id string) []string {
	t.Helper()
	rows, err := g.led.List(context.Background(), g.id, ledger.Filter{NodeID: id})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		kind := strings.SplitN(r.Stage, ":", 2)[0]
		if r.Outcome == ledger.OutcomeMalformed || (kind != "implement" && kind != "revise") {
			continue
		}
		out = append(out, kind+"@"+r.Provider)
	}
	return out
}

func countPrefix(seq []string, prefix string) int {
	n := 0
	for _, s := range seq {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

func twoEntryRevisions(fix, rev int) func(*rigOpts) {
	return func(o *rigOpts) {
		o.Settings = func(c *settings.Config) { c.Executor.FixAttempts = fix; c.Defaults.MaxRevisions = rev }
	}
}

func oneEntryRevisions(fix, rev int) func(*rigOpts) {
	return func(o *rigOpts) {
		o.Settings = func(c *settings.Config) {
			c.Executor.FixAttempts = fix
			c.Defaults.MaxRevisions = rev
			c.Models = map[string][]string{"strong": {"a/m1"}, "standard": {"a/m1"}, "any": {"a/m1"}}
		}
	}
}

func asStop(t *testing.T, err error, status, reason string) {
	t.Helper()
	var se *stopError
	if !errors.As(err, &se) || se.Status != status || se.Reason != reason {
		t.Fatalf("error = %v, want a stopError %s/%s", err, status, reason)
	}
}

func TestLadderOrderAndEscalation(t *testing.T) {
	t.Parallel()
	g := newRig(t, twoEntryRevisions(2, 2))
	id := leafID
	rc, fc := g.leafRC(t, Script{
		"implement:" + id: distinct(id, 18),
		"revise:" + id:    {reviseHint("one"), reviseHint("two")},
	}, true)
	failing(fc, id, 18)
	tb := traced(rc)

	_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	asStop(t, err, "escalated", "human_stop")

	var want []string
	for r := 0; r < 3; r++ {
		for _, p := range []string{"a", "b"} {
			for k := 0; k < 3; k++ {
				want = append(want, "implement@"+p)
			}
		}
		if r < 2 {
			want = append(want, "revise@a")
		}
	}
	if got := callSeq(t, g, id); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("calls:\n got %v\nwant %v", got, want)
	}
	escs := g.gate.Escalations()
	if len(escs) != 1 || escs[0].NodeID != id || escs[0].Reason != "test_fail" {
		t.Fatalf("gate escalations = %+v, want one for %s", escs, id)
	}
	if len(escs[0].History) != 18 || !strings.HasPrefix(escs[0].History[0], "attempt 1 a/m1 fail") {
		t.Fatalf("history = %d lines, first %q; want 18 attempt lines", len(escs[0].History), escs[0].History[0])
	}
	trace := strings.Join(tb.Trace(), ",")
	if !strings.Contains(trace, id+" in_progress->needs_revision,"+id+" needs_revision->escalated") {
		t.Fatalf("status trace = %s", trace)
	}
	if r := g.row(t, id); r.Status != blackboard.StatusEscalated || r.Revision != 2 {
		t.Fatalf("row = %s revision %d, want escalated at revision 2", r.Status, r.Revision)
	}
}

func TestLeafCallBound(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mod           func(*rigOpts)
		entries       int
		wantImplement int
	}{
		{"two entries", twoEntryRevisions(2, 2), 2, 18},
		{"one entry", oneEntryRevisions(2, 2), 1, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t, tc.mod)
			id := leafID
			rc, fc := g.leafRC(t, Script{
				"implement:" + id: distinct(id, 30),
				"revise:" + id:    {reviseHint("one"), reviseHint("two"), reviseHint("three")},
			}, true)
			failing(fc, id, 30)
			_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
			asStop(t, err, "escalated", "human_stop")
			seq := callSeq(t, g, id)
			impl, rev := countPrefix(seq, "implement@"), countPrefix(seq, "revise@")
			bound := tc.entries * (1 + 2) * (2 + 1)
			if impl != tc.wantImplement || impl > bound || rev > 2 {
				t.Fatalf("implement %d (want %d, bound %d), revise %d (bound 2)", impl, tc.wantImplement, bound, rev)
			}
			if tc.entries == 1 && impl+rev > 11 {
				t.Fatalf("total %d, want at most 11", impl+rev)
			}
		})
	}
}

// With one entry in both tiers the ladder has no next model to try: after the
// fixes of the entry the next request is the revise call, never a repeat.
func TestSingleEntryChainNoWastedCall(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneEntryRevisions(2, 2))
	id := leafID
	rc, fc := g.leafRC(t, Script{"implement:" + id: distinct(id, 9), "revise:" + id: {reviseHint("one"), reviseHint("two")}}, true)
	failing(fc, id, 9)
	_, _ = rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
	stages := stagesOf(g.fake)
	if len(stages) < 4 || stages[3] != "revise:"+id {
		t.Fatalf("stages = %v, want the 4th request to be revise:%s", stages, id)
	}
}

func TestContextTooLongTriesNextEntryThenEscalates(t *testing.T) {
	t.Parallel()
	id := leafID
	t.Run("first entry too small", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, func(o *rigOpts) {
			o.Settings = func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 600 }
		})
		rc, fc := g.leafRC(t, Script{"implement:" + id: {reply(good(id))}}, true)
		fc.LeafScript[id] = []runner.Verdict{passVerdict()}
		out := runOne(t, rc, id)
		if out.Status != blackboard.StatusVerified {
			t.Fatalf("outcome = %+v", out)
		}
		if got := callSeq(t, g, id); strings.Join(got, " ") != "implement@b" {
			t.Fatalf("calls = %v, want only b", got)
		}
	})
	t.Run("every entry too small", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, func(o *rigOpts) {
			o.Settings = func(c *settings.Config) {
				c.Providers[0].Models[0].ContextTokens = 600
				c.Providers[1].Models[0].ContextTokens = 600
			}
		})
		rc, _ := g.leafRC(t, Script{}, true)
		_, err := rc.runLeaf(context.Background(), g.plan.Leaf(id), leafIn{})
		asStop(t, err, "escalated", "human_stop")
		if n := len(g.fake.Requests()); n != 0 {
			t.Fatalf("provider calls = %d, want 0 (no implement and no revise call)", n)
		}
		escs := g.gate.Escalations()
		if len(escs) != 1 || escs[0].Reason != ClassContextTooLong {
			t.Fatalf("gate escalations = %+v, want one with reason context_too_long", escs)
		}
	})
}

// The floor of the prompt is larger than the whole window of the only entry:
// nothing is sent, and a human is asked.
func TestPackFloorOverBudgetNoCall(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneEntryRevisions(2, 2))
	id := leafID
	rc, _ := g.leafRC(t, Script{}, true)
	// Constraints are part of the floor: the packer drops every other optional part first.
	l := *g.plan.Leaf(id)
	for i := 0; i < 40; i++ {
		l.Constraints = append(l.Constraints, strings.Repeat("a long constraint ", 250))
	}
	rc.plan.byID[id] = &l
	_, err := rc.runLeaf(context.Background(), &l, leafIn{})
	asStop(t, err, "escalated", "human_stop")
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("provider calls = %d, want 0", n)
	}
	if escs := g.gate.Escalations(); len(escs) != 1 || escs[0].Reason != ClassContextTooLong {
		t.Fatalf("gate escalations = %+v", escs)
	}
}
