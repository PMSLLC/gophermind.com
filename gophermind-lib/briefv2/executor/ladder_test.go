package executor

import (
	"context"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
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
