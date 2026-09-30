package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
)

func TestScriptedProviderPopsPerStage(t *testing.T) {
	g := newRig(t)
	g.wire(Script{
		"implement:fn-greet":    {{Text: "one"}, {Text: "two"}},
		"implement:fn-farewell": {{Text: "other"}},
	})
	ctx := context.Background()
	call := func(stage, node string) (string, error) {
		res, err := g.router.CallParsed(ctx, router.CallInfo{RunID: g.id, Stage: stage, NodeID: node, Tier: router.TierStandard, Scope: router.ScopeNode, TaskType: "implement"},
			provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: packer.SystemPrefix + stage}, {Role: provider.RoleUser, Content: "p"}}},
			func(string) error { return nil })
		return res.Text, err
	}
	for _, w := range []struct{ stage, node, want string }{
		{"implement:fn-greet", "fn-greet", "one"},
		{"implement:fn-farewell", "fn-farewell", "other"},
		{"implement:fn-greet", "fn-greet", "two"},
	} {
		if got, err := call(w.stage, w.node); err != nil || got != w.want {
			t.Fatalf("%s = %q, %v; want %q", w.stage, got, err, w.want)
		}
	}
	if n := len(g.leafCalls("fn-greet")); n != 2 {
		t.Fatalf("leafCalls(fn-greet) = %d, want 2", n)
	}
	if n := len(g.fake.Requests()); n != 3 {
		t.Fatalf("Requests = %d, want 3", n)
	}
	// The real router wrote real ledger rows, with sizes and hashes only.
	rows, err := g.led.List(ctx, g.id, ledgerFilterNone)
	if err != nil {
		t.Fatal(err)
	}
	var implement int
	for _, r := range rows {
		if r.Stage == "implement:fn-greet" || r.Stage == "implement:fn-farewell" {
			implement++
		}
	}
	if implement != 3 {
		t.Fatalf("ledger implement rows = %d, want 3", implement)
	}
}

func TestScriptedProviderExhaustedFailsWithStage(t *testing.T) {
	var msgs []string
	sp := newScriptedProvider(Script{"implement:fn-x": {{Text: "only"}}}, func(f string, a ...any) { msgs = append(msgs, fmt.Sprintf(f, a...)) })
	p := sp.provider("a", []provider.ModelInfo{{ID: "m1", ContextTokens: 1000}})
	req := provider.Request{Model: "m1", Messages: []provider.Message{{Role: provider.RoleSystem, Content: packer.SystemPrefix + "implement:fn-x"}}}
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(context.Background(), req); err == nil {
		t.Fatal("an exhausted script answered")
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "implement:fn-x") {
		t.Fatalf("failure messages = %q, want one naming the stage", msgs)
	}

	sp2 := newScriptedProvider(Script{"implement:fn-x": {{Text: "last"}}}, func(f string, a ...any) { t.Errorf(f, a...) })
	sp2.Loop = true
	p2 := sp2.provider("a", []provider.ModelInfo{{ID: "m1", ContextTokens: 1000}})
	for i := 0; i < 3; i++ {
		r, err := p2.Complete(context.Background(), req)
		if err != nil || r.Text != "last" {
			t.Fatalf("loop call %d = %q, %v", i, r.Text, err)
		}
	}
}

func TestScriptedProviderInjections(t *testing.T) {
	sp := newScriptedProvider(Script{"s": {
		{Err: provider.ErrTruncated{Provider: "a"}},
		{Err: provider.ErrEmptyReply{Provider: "a"}},
		{Text: ""},
		{Delay: time.Hour},
	}}, func(f string, a ...any) { t.Errorf(f, a...) })
	p := sp.provider("a", []provider.ModelInfo{{ID: "m1", ContextTokens: 1000}})
	req := provider.Request{Model: "m1", Messages: []provider.Message{{Role: provider.RoleSystem, Content: packer.SystemPrefix + "s"}}}
	if _, err := p.Complete(context.Background(), req); !errors.As(err, new(provider.ErrTruncated)) {
		t.Fatalf("step 1 err = %v, want ErrTruncated", err)
	}
	if _, err := p.Complete(context.Background(), req); !errors.As(err, new(provider.ErrEmptyReply)) {
		t.Fatalf("step 2 err = %v, want ErrEmptyReply", err)
	}
	if r, err := p.Complete(context.Background(), req); err != nil || r.Text != "" {
		t.Fatalf("step 3 = %q, %v; want empty content", r.Text, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := p.Complete(ctx, req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delayed step err = %v, want a deadline", err)
	}
}

func TestScriptedReplyTextStaysOutOfLedger(t *testing.T) {
	g := newRig(t)
	g.wire(Script{"implement:fn-greet": {{Text: "REPLY-" + canarySecret}, {Text: "REPLY-" + canarySecret}, {Text: "ok"}, {Text: "ok"}}})
	ctx := context.Background()
	_, err := g.router.CallParsed(ctx, router.CallInfo{RunID: g.id, Stage: "implement:fn-greet", NodeID: "fn-greet", Tier: router.TierStandard, Scope: router.ScopeNode, TaskType: "implement"},
		provider.Request{Messages: []provider.Message{{Role: provider.RoleSystem, Content: packer.SystemPrefix + "implement:fn-greet"}, {Role: provider.RoleUser, Content: "PROMPT-" + canarySecret}}},
		func(text string) error {
			if strings.HasPrefix(text, "REPLY-") {
				return errors.New("malformed")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("CallParsed: %v", err)
	}
	rows, err := g.led.List(ctx, g.id, ledgerFilterNone)
	if err != nil {
		t.Fatal(err)
	}
	if containsCanary(fmt.Sprintf("%+v", rows)) {
		t.Fatal("reply or prompt text reached a ledger row")
	}
	for _, e := range g.sink.Events() {
		if containsCanary(fmt.Sprintf("%+v", e)) {
			t.Fatal("reply or prompt text reached an event")
		}
	}
}

func TestScriptGateRecordsAndDefaultsToStop(t *testing.T) {
	g := newRig(t)
	g.wire(Script{})
	res, err := g.gate.Escalate(context.Background(), human.Escalation{NodeID: "fn-greet", Reason: "r"})
	if err != nil || res.Action != human.ActionStop {
		t.Fatalf("default = %v, %v; want stop", res, err)
	}
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "again"}}
	res, _ = g.gate.Escalate(context.Background(), human.Escalation{NodeID: "fn-bye"})
	if res.Action != human.ActionRetry {
		t.Fatalf("queued = %v, want retry", res)
	}
	if len(g.gate.Escalations()) != 2 {
		t.Fatalf("recorded %d escalations, want 2", len(g.gate.Escalations()))
	}
}

func TestOptionsUsesRealCollaborators(t *testing.T) {
	g := newRig(t)
	g.wire(Script{})
	o := g.options()
	if err := o.validate(); err != nil {
		t.Fatalf("options do not validate: %v", err)
	}
	if o.Caller != Caller(g.router) || o.Board != g.board || o.Ledger != g.led || o.Gate == nil {
		t.Fatal("options must hold the real router, board and ledger")
	}
}

func TestGoodAndBadReadFixtures(t *testing.T) {
	if !strings.Contains(good("fn-greet"), "func Greet") {
		t.Fatal("good(fn-greet) is not the fixture")
	}
	if strings.TrimSpace(bad("fn-greet", 1)) == "" {
		t.Fatal("bad(fn-greet, 1) is empty")
	}
}

// harness_test helpers used by several files follow.

func TestWave0HelperCommitsTestsAndStubs(t *testing.T) {
	g := newRig(t)
	g.startWave0()
	dirty, err := g.git.Dirty()
	if err != nil || len(dirty) != 0 {
		t.Fatalf("after wave 0 dirty = %v, %v; want clean", dirty, err)
	}
	for _, l := range g.plan.Leaves {
		if !fileExists(filepath.Join(g.repo, filepath.FromSlash(l.StubFile))) {
			t.Errorf("stub of %s is missing", l.ID)
		}
	}
}

var ledgerFilterNone = ledger.Filter{}

// maxScriptDelay is the longest a scripted Delay waits; a longer one ends as a timeout.
const maxScriptDelay = 500 * time.Millisecond

// step is one scripted reply: text, a provider error, or a delay (which ends
// early with the context's error, so a timeout can be scripted).
type step struct {
	Text  string
	Err   error
	Delay time.Duration
}

// Script maps a stage ("implement:fn-greet") to its replies, popped in order.
type Script map[string][]step

// scriptedProvider is the only fake in the executor tests. Every provider it
// hands out draws from the same script, so "fails twice, then passes" does not
// depend on which chain entry served the call. It knows stages from the system
// message (packer.SystemPrefix) and keeps every request.
type scriptedProvider struct {
	Loop bool // repeat the last step of a stage when it is exhausted

	mu     sync.Mutex
	script map[string][]step
	used   map[string]int
	reqs   []provider.Request
	fail   func(format string, args ...any)
}

func newScriptedProvider(s Script, fail func(format string, args ...any)) *scriptedProvider {
	cp := map[string][]step{}
	for k, v := range s {
		cp[k] = append([]step(nil), v...)
	}
	return &scriptedProvider{script: cp, used: map[string]int{}, fail: fail}
}

// stageOf is the stage of a request, "" when its system message has none.
func stageOf(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem && strings.HasPrefix(m.Content, packer.SystemPrefix) {
			return strings.TrimPrefix(m.Content, packer.SystemPrefix)
		}
	}
	return ""
}

func (sp *scriptedProvider) next(req provider.Request) (step, error) {
	stage := stageOf(req)
	sp.mu.Lock()
	sp.reqs = append(sp.reqs, req)
	steps := sp.script[stage]
	i := sp.used[stage]
	var st step
	switch {
	case i < len(steps):
		st = steps[i]
		sp.used[stage] = i + 1
	case sp.Loop && len(steps) > 0:
		st = steps[len(steps)-1]
	default:
		sp.mu.Unlock()
		sp.fail("scripted provider: no reply left for stage %q", stage)
		return step{}, fmt.Errorf("scripted provider: no reply left for stage %q", stage)
	}
	sp.mu.Unlock()
	return st, nil
}

// scriptedProv is one named provider over the shared script. It is not a
// provider.Fake because a Fake's function never sees the context, and a
// scripted timeout has to end when the caller's context does.
type scriptedProv struct {
	sp     *scriptedProvider
	name   string
	models []provider.ModelInfo
}

var _ provider.Provider = (*scriptedProv)(nil)

func (p *scriptedProv) Name() string                 { return p.name }
func (p *scriptedProv) Models() []provider.ModelInfo { return p.models }

func (p *scriptedProv) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	if err := ctx.Err(); err != nil {
		return provider.Response{}, err
	}
	st, err := p.sp.next(req)
	if err != nil {
		return provider.Response{}, err
	}
	return st.answer(ctx, req)
}

// provider returns one provider.Provider named name that draws from the script.
func (sp *scriptedProvider) provider(name string, models []provider.ModelInfo) *scriptedProv {
	return &scriptedProv{sp: sp, name: name, models: models}
}

func (st step) answer(ctx context.Context, req provider.Request) (provider.Response, error) {
	if st.Delay > 0 {
		// A delay is capped so a script with no deadline on the context cannot hang a test.
		d := st.Delay
		if d > maxScriptDelay {
			select {
			case <-time.After(maxScriptDelay):
				return provider.Response{}, context.DeadlineExceeded
			case <-ctx.Done():
				return provider.Response{}, ctx.Err()
			}
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	}
	if st.Err != nil {
		return provider.Response{}, st.Err
	}
	return provider.Response{Text: st.Text, Model: req.Model, Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

// Requests is every request received, oldest first.
func (sp *scriptedProvider) Requests() []provider.Request {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return append([]provider.Request(nil), sp.reqs...)
}

// scriptGate is the human gate of the tests: it records every escalation and
// answers from a queue, stop when the queue is empty.
type scriptGate struct {
	mu    sync.Mutex
	queue []human.Resolution
	seen  []human.Escalation
}

func (s *scriptGate) Ask(context.Context, []human.Question) ([]human.Answer, error) {
	return nil, errors.New("scriptGate: Ask is not used by the executor")
}

func (s *scriptGate) Approve(context.Context, human.PlanSummary) (human.Decision, error) {
	return human.Decision{}, errors.New("scriptGate: Approve is not used by the executor")
}

func (s *scriptGate) Escalate(_ context.Context, e human.Escalation) (human.Resolution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, e)
	if len(s.queue) == 0 {
		return human.Resolution{Action: human.ActionStop}, nil
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	return r, nil
}

// Escalations is every escalation asked so far.
func (s *scriptGate) Escalations() []human.Escalation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]human.Escalation(nil), s.seen...)
}

var _ human.Gate = (*scriptGate)(nil)

// wire builds the scripted providers a and b, the real router over the rig's
// settings, ledger and sink, and the gate. The sandbox is off in the rig.
func (g *rig) wire(script Script) {
	g.t.Helper()
	g.fake = newScriptedProvider(script, func(f string, a ...any) { g.t.Errorf(f, a...) })
	provs := map[string]provider.Provider{}
	for _, pc := range g.cfg.Providers {
		var infos []provider.ModelInfo
		for _, m := range pc.Models {
			infos = append(infos, provider.ModelInfo{ID: m.ID, ContextTokens: m.ContextTokens})
		}
		provs[pc.Name] = g.fake.provider(pc.Name, infos)
	}
	instant := func(context.Context, time.Duration) error { return nil }
	g.router = router.New(g.cfg, provs, g.led, g.sink, router.WithSleep(instant))
	g.gate = &scriptGate{}
}

// options is the Options for Run: the real router, board, ledger and sink.
func (g *rig) options() Options {
	return Options{
		RunDir: g.runDir, Repo: g.repo, Caller: g.router, Board: g.board, Ledger: g.led,
		Gate: g.gate, Sink: g.sink, Settings: g.cfg, Secrets: g.secrets, Git: g.git,
		LedgerErrors: g.router.LedgerErrors,
	}
}

// leafCalls is every request whose stage belongs to the leaf (implement,
// revise or testwrite), oldest first.
func (g *rig) leafCalls(id string) []provider.Request {
	var out []provider.Request
	for _, r := range g.fake.Requests() {
		if strings.HasSuffix(stageOf(r), ":"+id) {
			out = append(out, r)
		}
	}
	return out
}

// good is the fixture reply that passes the leaf's test; bad(id, k) is the
// k-th reply that fails it.
func good(id string) string { return fixtureReply("good." + id + ".txt") }

func bad(id string, k int) string { return fixtureReply(fmt.Sprintf("bad.%s.%d.txt", id, k)) }

func fixtureReply(name string) string {
	raw, err := os.ReadFile(filepath.Join(greeterDir, "impl", name))
	if err != nil {
		panic("harness: missing fixture " + name)
	}
	return string(raw)
}

// startWave0 starts the work branch (the Test-writer's files are the allowed
// dirt), writes the contract types and every leaf's stub, and commits them with
// the tests, as Wave 0 does.
func (g *rig) startWave0() {
	g.t.Helper()
	var paths []string
	seen := map[string]bool{}
	for _, l := range g.plan.Leaves {
		if !seen[l.TestFile] {
			seen[l.TestFile] = true
			paths = append(paths, l.TestFile)
		}
	}
	if err := g.git.Start("main", "gm/work", paths); err != nil {
		g.t.Fatal(err)
	}
	typePaths, err := WriteTypes(g.repo, g.plan.Contracts, g.plan.Policy())
	if err != nil {
		g.t.Fatal(err)
	}
	paths = append(paths, typePaths...)
	for _, l := range g.plan.Leaves {
		src, err := stubFor(g.plan.Contracts, g.plan.Policy(), l)
		if err != nil {
			g.t.Fatal(err)
		}
		if err := pathsafe.Replace(g.repo, l.StubFile, src); err != nil {
			g.t.Fatal(err)
		}
		paths = append(paths, l.StubFile)
	}
	if _, err := g.git.CommitWave0(paths); err != nil {
		g.t.Fatal(err)
	}
}

func TestScriptedDelayEndsWithoutDeadline(t *testing.T) {
	sp := newScriptedProvider(Script{"s": {{Delay: time.Hour}}}, func(f string, a ...any) { t.Errorf(f, a...) })
	p := sp.provider("a", []provider.ModelInfo{{ID: "m1", ContextTokens: 1000}})
	req := provider.Request{Model: "m1", Messages: []provider.Message{{Role: provider.RoleSystem, Content: packer.SystemPrefix + "s"}}}
	done := make(chan error, 1)
	go func() { _, err := p.Complete(context.Background(), req); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an hour-long delay answered")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a delay with no deadline on the context never ended")
	}
}
