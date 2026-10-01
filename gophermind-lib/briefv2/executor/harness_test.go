package executor

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/vault"
)

func TestScriptedProviderPopsPerStage(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	Loop   bool               // repeat the last step of a stage when it is exhausted
	Before func(stage string) // runs when a call for the stage arrives, before its step answers (never under the lock)

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
	before := sp.Before
	sp.mu.Unlock()
	if before != nil {
		before(stage)
	}
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
	mu      sync.Mutex
	queue   []human.Resolution
	seen    []human.Escalation
	waiting bool   // answer every escalation with human.ErrWaiting (a file gate with no answer yet)
	by      string // AnsweredBy of every resolution (an auto-answering gate says "unattended-default")
	after   func() // runs once the answer is chosen, before it is returned
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
	if s.waiting {
		return human.Resolution{}, human.ErrWaiting
	}
	if len(s.queue) == 0 {
		return human.Resolution{Action: human.ActionStop}, nil
	}
	r := s.queue[0]
	s.queue = s.queue[1:]
	r.AnsweredBy = s.by
	if s.after != nil {
		s.after()
	}
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
		noRedCheck:   true, // the red check has its own tests (acceptance_red_test.go)
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

// makeModuleProxy writes one module version into a new GOPROXY directory
// (<dir>/<module>/@v/{list,<version>.info,.mod,.zip}) and returns its file://
// URL. The files map holds the module's source files by relative name; a
// go.mod is added when it has none.
func makeModuleProxy(t *testing.T, module, version string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	addModule(t, dir, module, version, files)
	return "file://" + dir
}

// addModule adds one more module version to a proxy directory made by
// makeModuleProxy (taken from its URL).
func addModule(t *testing.T, dir, module, version string, files map[string]string) {
	t.Helper()
	dir = strings.TrimPrefix(dir, "file://")
	vdir := filepath.Join(dir, filepath.FromSlash(module), "@v")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	all := map[string]string{}
	for k, v := range files {
		all[k] = v
	}
	if _, ok := all["go.mod"]; !ok {
		all["go.mod"] = "module " + module + "\n\ngo 1.20\n"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(all[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"list":            []byte(version + "\n"),
		version + ".info": []byte(`{"Version":"` + version + `","Time":"2024-01-01T00:00:00Z"}`),
		version + ".mod":  []byte(all["go.mod"]),
		version + ".zip":  buf.Bytes(),
	} {
		if err := os.WriteFile(filepath.Join(vdir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// useModuleProxy points the deps step at a file:// proxy with the checksum
// database off, restored when the test ends.
func useModuleProxy(t *testing.T, url string) {
	t.Helper()
	old := testHooks
	testHooks = depsHooks{GoProxy: url, GoSumDB: "off"}
	t.Cleanup(func() { testHooks = old })
}

// ---- Task 11a support: a scripted checker and readers for the stores ----

// outputOf makes a runner.Output holding text. Output has no constructor, so a
// real shell prints the text once and the runner captures it.
func outputOf(text string) runner.Output {
	r := runner.New(runner.Config{})
	res := r.Run(context.Background(), runner.Spec{
		Dir: os.TempDir(), Argv: []string{"/bin/sh", "-c", `printf '%s' "$1"`, "sh", text}, Env: []string{"PATH=/usr/bin:/bin"},
	})
	if res.Err != nil {
		panic("harness: outputOf: " + res.Err.Error())
	}
	return res.Out
}

// failVerdict is a failing verdict of the class with names and output text.
func failVerdict(class string, out string, names ...string) runner.Verdict {
	return runner.Verdict{Class: class, Names: names, Events: 1, Out: outputOf(out)}
}

// passVerdict is a pass with one test event.
func passVerdict() runner.Verdict { return runner.Verdict{Events: 1} }

// fakeChecker is the Checker of the leaf-loop tests. CheckLeaf pops the next
// scripted verdict of the leaf (found by its test function) and runs Hook, if
// set, while the "check" is under way. A call with nothing scripted fails the
// test. Run, BuildVet and Test are not used by a leaf loop; Task 12a adds a
// RepoScript for the repository-wide ones.
type fakeChecker struct {
	t          *testing.T
	mu         sync.Mutex
	LeafScript map[string][]runner.Verdict
	Hook       func(repo string)
	byFunc     map[string]string
	calls      map[string]int
	files      map[string][][]string

	// RepoScript scripts the repository-wide checks of a wave. While it is nil
	// BuildVet and Test are errors (a leaf loop must not call them); once set, a
	// call with nothing scripted passes. RepoHook runs inside every BuildVet.
	RepoScript *repoScript
	RepoHook   func(repo string)
	buildVets  int
	testCalls  []repoCall
}

// repoScript holds the verdicts of BuildVet (in order) and of Test, keyed by
// TestSet.Pkg (in order per package).
type repoScript struct {
	BuildVet []runner.Verdict
	Test     map[string][]runner.Verdict
}

// repoCall is what one Test call was asked.
type repoCall struct {
	Pkg   string
	Funcs []string
	Race  bool
}

func newFakeChecker(g *rig) *fakeChecker {
	f := &fakeChecker{t: g.t, LeafScript: map[string][]runner.Verdict{}, byFunc: map[string]string{}, calls: map[string]int{}, files: map[string][][]string{}}
	for _, l := range g.plan.Leaves {
		f.byFunc[l.TestFunc] = l.ID
	}
	return f
}

var _ Checker = (*fakeChecker)(nil)

func (f *fakeChecker) CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict {
	f.mu.Lock()
	id := f.byFunc[c.TestFunc]
	f.calls[id]++
	f.files[id] = append(f.files[id], append([]string(nil), c.Files...))
	queue := f.LeafScript[id]
	var v runner.Verdict
	ok := len(queue) > 0
	if ok {
		v, f.LeafScript[id] = queue[0], queue[1:]
	}
	hook := f.Hook
	f.mu.Unlock()
	if hook != nil {
		hook(c.Repo)
	}
	if !ok {
		f.t.Errorf("fakeChecker: no verdict scripted for leaf %q", id)
		return runner.Verdict{Class: runner.ClassHarness, Err: errors.New("fakeChecker: unscripted call")}
	}
	return v
}

func (f *fakeChecker) Run(context.Context, runner.Spec) runner.Result {
	f.mu.Lock()
	scripted := f.RepoScript != nil
	f.mu.Unlock()
	if scripted {
		return runner.Result{} // go mod verify and the like pass once the repository-wide checks are scripted
	}
	f.t.Error("fakeChecker.Run: not scripted")
	return runner.Result{ExitCode: -1, Err: errors.New("fakeChecker: unscripted call")}
}

func (f *fakeChecker) BuildVet(ctx context.Context, repo string, env []string) runner.Verdict {
	f.mu.Lock()
	rs, hook := f.RepoScript, f.RepoHook
	if rs == nil {
		f.mu.Unlock()
		f.t.Error("fakeChecker.BuildVet: not scripted")
		return runner.Verdict{Class: runner.ClassHarness, Err: errors.New("fakeChecker: unscripted call")}
	}
	f.buildVets++
	v := runner.Verdict{}
	if len(rs.BuildVet) > 0 {
		v, rs.BuildVet = rs.BuildVet[0], rs.BuildVet[1:]
	}
	f.mu.Unlock()
	if hook != nil {
		hook(repo)
	}
	return v
}

func (f *fakeChecker) Test(ctx context.Context, t runner.TestSet) runner.Verdict {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.RepoScript == nil {
		f.t.Error("fakeChecker.Test: not scripted")
		return runner.Verdict{Class: runner.ClassHarness, Err: errors.New("fakeChecker: unscripted call")}
	}
	f.testCalls = append(f.testCalls, repoCall{Pkg: t.Pkg, Funcs: append([]string(nil), t.Funcs...), Race: t.Race})
	q := f.RepoScript.Test[t.Pkg]
	if len(q) == 0 {
		return passVerdict()
	}
	v := q[0]
	f.RepoScript.Test[t.Pkg] = q[1:]
	return v
}

// checks is how many times the leaf was checked.
func (f *fakeChecker) checks(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

// leafRC is a started run (Wave 0 done, real runner) over the scripted
// providers, with every leaf ready to claim. With fake set, the checker is a
// fakeChecker and the rig's rc.chk is replaced by it.
func (g *rig) leafRC(t *testing.T, script Script, fake bool) (*runCtx, *fakeChecker) {
	t.Helper()
	g.wire(script)
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
	var fc *fakeChecker
	if fake {
		fc = newFakeChecker(g)
		rc.chk = fc
	}
	for _, l := range g.plan.Leaves {
		if err := g.board.SetStatus(context.Background(), g.id, l.ID, blackboard.StatusPending, blackboard.StatusReady); err != nil {
			t.Fatal(err)
		}
	}
	return rc, fc
}

func (g *rig) row(t *testing.T, id string) blackboard.Row {
	t.Helper()
	r, err := g.board.Get(context.Background(), g.id, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// attemptsOf is the attempts recorded on the blackboard for the node.
func attemptsOf(t *testing.T, board blackboard.Blackboard, id string) []blackboard.Attempt {
	t.Helper()
	r, err := board.Get(context.Background(), greeterID, id)
	if err != nil {
		t.Fatal(err)
	}
	return r.Attempts
}

// verdictsOf is "fail,fail,pass" for the attempts of the node.
func verdictsOf(t *testing.T, board blackboard.Blackboard, id string) string {
	t.Helper()
	var out []string
	for _, a := range attemptsOf(t, board, id) {
		out = append(out, string(a.Verdict))
	}
	return strings.Join(out, ",")
}

// reasonsOf is the failure reasons of the attempts, in order.
func reasonsOf(t *testing.T, board blackboard.Blackboard, id string) []string {
	t.Helper()
	var out []string
	for _, a := range attemptsOf(t, board, id) {
		out = append(out, a.FailureReason)
	}
	return out
}

// implementCalls is the ledger rows of the leaf's implement stage, oldest first.
func implementCalls(t *testing.T, led ledger.Ledger, id string) []ledger.Call {
	t.Helper()
	rows, err := led.List(context.Background(), greeterID, ledger.Filter{Stage: "implement:" + id})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// stagesOf is the stage of every request the scripted providers received.
func stagesOf(fake *scriptedProvider) []string {
	var out []string
	for _, r := range fake.Requests() {
		out = append(out, stageOf(r))
	}
	return out
}

// userText is the last user message of a request: the packed prompt.
func userText(r provider.Request) string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == provider.RoleUser {
			return r.Messages[i].Content
		}
	}
	return ""
}

// leafCommits is how many commits of the leaf the work branch holds.
func (g *rig) leafCommits(id string) int {
	g.t.Helper()
	out := strings.TrimSpace(g.gitCmd("log", "--format=%s", "--grep=^gm("+id+"): "))
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

// variant makes a reply that differs from src but behaves the same, so a
// script can hold many distinct failing replies.
func variant(src string, n int) string { return src + fmt.Sprintf("\n// variant %d\n", n) }

// hasCanary reports whether any store the run writes holds canary: the SQLite
// file (and its WAL), the events, every file of the run folder, the ledger
// rows, the blackboard rows and the commit messages of the work branch. The
// repository's source files are not a store: a passing reply is committed.
func hasCanary(t *testing.T, g *rig, canary string) bool {
	t.Helper()
	found := false
	check := func(where string, b []byte) {
		if bytes.Contains(b, []byte(canary)) {
			t.Logf("canary %q found in %s", canary, where)
			found = true
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if b, err := os.ReadFile(g.dbPath + suffix); err == nil {
			check("sqlite"+suffix, b)
		}
	}
	for _, e := range g.sink.Events() {
		check("an event", []byte(fmt.Sprintf("%+v", e)))
	}
	_ = filepath.WalkDir(g.runDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, rerr := os.ReadFile(p); rerr == nil {
			check(p, b)
		}
		return nil
	})
	ctx := context.Background()
	if rows, err := g.led.List(ctx, g.id, ledger.Filter{}); err == nil {
		check("the ledger", []byte(fmt.Sprintf("%+v", rows)))
	}
	if rows, err := g.board.List(ctx, g.id, blackboard.Filter{}); err == nil {
		check("the blackboard", []byte(fmt.Sprintf("%+v", rows)))
	}
	check("commit messages", []byte(g.gitCmd("log", "--all", "--format=%B")))
	return found
}

// stubPath is the absolute path of the leaf's stub file; realPath of its file.
func (g *rig) stubPath(l *Leaf) string { return filepath.Join(g.repo, filepath.FromSlash(l.StubFile)) }
func (g *rig) realPath(l *Leaf) string { return filepath.Join(g.repo, filepath.FromSlash(l.File)) }

// traceBoard records every claim and status change the executor asks the real
// blackboard for, so a test can assert the sequence of transitions.
type traceBoard struct {
	blackboard.Blackboard
	mu  sync.Mutex
	log []string
	// onStatus, when set, runs after each accepted status change.
	onStatus func(node string, to blackboard.Status)
}

func (b *traceBoard) note(s string) {
	b.mu.Lock()
	b.log = append(b.log, s)
	b.mu.Unlock()
}

func (b *traceBoard) Claim(ctx context.Context, runID, nodeID, worker string) (bool, error) {
	b.note("claim " + nodeID)
	return b.Blackboard.Claim(ctx, runID, nodeID, worker)
}

func (b *traceBoard) SetStatus(ctx context.Context, runID, nodeID string, from, to blackboard.Status) error {
	err := b.Blackboard.SetStatus(ctx, runID, nodeID, from, to)
	if err == nil {
		b.note(fmt.Sprintf("%s %s->%s", nodeID, from, to))
		if b.onStatus != nil {
			b.onStatus(nodeID, to)
		}
	}
	return err
}

// Trace is the log so far, one entry per claim or accepted status change.
func (b *traceBoard) Trace() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.log...)
}

// traced puts a traceBoard between the run and the real blackboard.
func traced(rc *runCtx) *traceBoard {
	tb := &traceBoard{Blackboard: rc.o.Board}
	rc.o.Board = tb
	return tb
}

// useChecker is the runFlags.afterStart that replaces the run's checker.
func useChecker(c Checker) func(*runCtx) { return func(rc *runCtx) { rc.chk = c } }

// TestRigInReopens: a rig over a directory the caller owns can be closed and
// rebuilt from the directory alone (what a second process does): the same
// plan, the same blackboard rows, the same secret, and no second planning.
func TestRigInReopens(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := newRigIn(t, dir, Script{})
	ctx := context.Background()
	if err := a.board.InitRun(ctx, a.id, []string{"fn-greet"}, map[string]int{"fn-greet": 0}); err != nil {
		t.Fatal(err)
	}
	rowsA, err := a.board.List(ctx, a.id, blackboard.Filter{})
	if err != nil || len(rowsA) == 0 {
		t.Fatalf("rows = %d, %v", len(rowsA), err)
	}
	approvalA, err := os.ReadFile(filepath.Join(a.runDir, "approval.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.close()

	b := newRigIn(t, dir, Script{})
	if b.repo != a.repo || b.runDir != a.runDir || b.id != a.id {
		t.Fatalf("paths differ: %s %s vs %s %s", b.repo, b.runDir, a.repo, a.runDir)
	}
	rowsB, err := b.board.List(ctx, b.id, blackboard.Filter{})
	if err != nil || len(rowsB) != len(rowsA) {
		t.Fatalf("rebuilt rows = %d, %v; want %d", len(rowsB), err, len(rowsA))
	}
	for i := range rowsA {
		if rowsA[i].NodeID != rowsB[i].NodeID || rowsA[i].Status != rowsB[i].Status {
			t.Errorf("row %d differs: %v vs %v", i, rowsA[i], rowsB[i])
		}
	}
	approvalB, _ := os.ReadFile(filepath.Join(b.runDir, "approval.json"))
	if !bytes.Equal(approvalA, approvalB) {
		t.Error("the plan was made again")
	}
	if len(b.plan.Leaves) != len(a.plan.Leaves) || len(b.plan.Leaves) != 5 {
		t.Errorf("leaves = %d, want 5", len(b.plan.Leaves))
	}
	if v, ok := b.secrets.Get(vault.RunScope(b.id), greeterSecret); !ok || v != canarySecret {
		t.Error("the rebuilt rig does not hold the run's secret")
	}
	if b.fake == nil || b.router == nil || b.gate == nil {
		t.Error("newRigIn must wire the script")
	}
}
