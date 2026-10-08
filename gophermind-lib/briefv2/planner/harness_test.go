package planner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

const (
	greeter   = "testdata/greeter"
	greeterID = "gm-2026-09-29-900"
)

// scriptGate is a human gate that answers at once from a script and records
// what it was asked. A nil answer function answers every question "yes".
type scriptGate struct {
	mu       sync.Mutex
	answer   func(q human.Question) string
	decision human.Decision
	// approveErr is returned by Approve alone (a gate that confirms but has not approved yet).
	approveErr error
	err        error // returned by every call when set (human.ErrWaiting for a gate nobody answers)
	asked      []human.Question
	plans      []human.PlanSummary
	rounds     [][]string // the question ids of each Ask call, in order

	understandings []human.Understanding
	confirm        *human.Decision
}

var _ human.Gate = (*scriptGate)(nil)

func (g *scriptGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, qs...)
	ids := make([]string, len(qs))
	for i, q := range qs {
		ids[i] = q.ID
	}
	g.rounds = append(g.rounds, ids)
	if g.err != nil {
		return nil, g.err
	}
	out := make([]human.Answer, len(qs))
	for i, q := range qs {
		text := "yes"
		if g.answer != nil {
			text = g.answer(q)
		}
		out[i] = human.Answer{ID: q.ID, Text: text}
	}
	return out, nil
}

func (g *scriptGate) Approve(ctx context.Context, plan human.PlanSummary) (human.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.plans = append(g.plans, plan)
	if g.approveErr != nil {
		return human.Decision{}, g.approveErr
	}
	if g.err != nil {
		return human.Decision{}, g.err
	}
	return g.decision, nil
}

func (g *scriptGate) Confirm(ctx context.Context, u human.Understanding) (human.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.understandings = append(g.understandings, u)
	if g.err != nil {
		return human.Decision{}, g.err
	}
	if g.confirm != nil {
		return *g.confirm, nil
	}
	return human.Decision{Approved: true, By: "test"}, nil
}

func (g *scriptGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	return human.Resolution{Action: human.ActionStop}, nil
}

// approving is a gate that answers "yes" and approves the plan.
func approving() *scriptGate {
	return &scriptGate{decision: human.Decision{Approved: true, By: "test"}}
}

// rig is one target repo, one config dir and one run folder, with a planner
// wired over a real router and the fixture provider.
type rig struct {
	t         *testing.T
	repo      string // the target repository (a temp dir)
	runDir    string // <repo>/.gophermind/<id>
	briefPath string
	led       ledger.Ledger
	board     blackboard.Blackboard
	sink      *events.Collector
	gate      human.Gate
	fake      *provider.Fake
	router    *router.Router
	cfg       *settings.Config
	deps      planner.Deps
}

// newRig copies the greeter brief into a temp dir with its repo pointed at a
// fresh temp repository, and wires a planner whose model is the fixture
// provider over dirs (testdata/greeter is always searched last).
func newRig(t *testing.T, gate human.Gate, dirs ...string) *rig {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	g := &rig{t: t, repo: t.TempDir(), gate: gate}
	if resolved, err := filepath.EvalSymlinks(g.repo); err == nil {
		g.repo = resolved
	}
	g.runDir = filepath.Join(g.repo, ".gophermind", greeterID)
	g.briefPath = writeBrief(t, g.repo, nil)
	// The planner creates the run folder and writes the run record; the file
	// backends find the folder through it, as the commands do.
	resolve := func(id string) (string, error) {
		rec, err := planner.LookupRun(id)
		return rec.RunDir, err
	}
	g.led, g.board = ledger.NewFS(resolve), blackboard.NewFS(resolve)
	g.wire(dirs...)
	return g
}

// writeBrief writes the greeter brief with repo filled in and returns its
// path. edit, when given, changes the text first.
func writeBrief(t *testing.T, repo string, edit func(string) string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(greeter, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "REPO_DIR", repo, 1)
	if edit != nil {
		text = edit(text)
	}
	path := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// wire builds a fresh provider, router and dependency set, the way a new
// process would on `resume`. The repo and config dir stay.
func (g *rig) wire(dirs ...string) {
	g.t.Helper()
	fake, err := planner.FixtureProvider(append(dirs, greeter)...)
	if err != nil {
		g.t.Fatal(err)
	}
	g.fake = fake
	g.cfg = planner.FixtureSettings()
	g.sink = events.NewCollector()
	g.router = router.New(g.cfg, map[string]provider.Provider{"fake": fake}, g.led, g.sink)
	g.deps = planner.Deps{Caller: g.router, Gate: g.gate, Sink: g.sink, Board: g.board, Settings: g.cfg,
		LedgerErrors: g.router.LedgerErrors}
}

func (g *rig) plan(o planner.Options) (planner.Outcome, error) {
	if o.BriefPath == "" && o.RunID == "" {
		o.BriefPath = g.briefPath
	}
	return planner.New(g.deps).Run(context.Background(), o)
}

func (g *rig) mustPlan(o planner.Options) {
	g.t.Helper()
	out, err := g.plan(o)
	if err != nil || out != planner.Done {
		g.t.Fatalf("Run = %q, %v; want done", out, err)
	}
}

// stagesCalled lists the stage of every request the fixture provider got, in order.
func (g *rig) stagesCalled() []string {
	var out []string
	for _, r := range g.fake.Requests() {
		out = append(out, planner.StageOf(r))
	}
	return out
}

func (g *rig) read(name string) []byte {
	g.t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.runDir, filepath.FromSlash(name)))
	if err != nil {
		g.t.Fatal(err)
	}
	return raw
}

func (g *rig) has(name string) bool {
	_, err := os.Stat(filepath.Join(g.runDir, filepath.FromSlash(name)))
	return err == nil
}

// variant writes canned replies into a temp dir to put in front of the
// greeter fixture: files maps a file name to its content.
func variant(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	// "contract.outline.txt" is a whole outline: the shared pass keeps its types
	// component and the one batch of the greeter brief brings the rest.
	if body, ok := files["contract.outline.txt"]; ok {
		moved := map[string]string{"contract.outline.1.txt": body}
		for k, v := range files {
			if k != "contract.outline.txt" {
				moved[k] = v
			}
		}
		if _, ok := moved["contract.outline.2.txt"]; !ok {
			moved["contract.outline.2.txt"] = body
		}
		files = moved
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func count(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}
