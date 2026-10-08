package projectrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

const gID = "gm-2026-09-29-900"

func plannerTestdata(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "planner", "testdata", name)
}

// fakeExec stands in for executor.Run and records what it was given.
type fakeExec struct {
	calls int
	opts  executor.Options
	rep   executor.Report
	err   error
	fn    func(executor.Options) (executor.Report, error)
}

func (f *fakeExec) run(_ context.Context, o executor.Options) (executor.Report, error) {
	f.calls++
	f.opts = o
	if f.fn != nil {
		return f.fn(o)
	}
	return f.rep, f.err
}

// runRig is the preflight rig plus a brief file, a fixture model, real
// file-backed stores and a fake executor.
type runRig struct {
	*rig
	brief string
	fake  *provider.Fake
	out   *bytes.Buffer
	errb  *bytes.Buffer
	exec  *fakeExec
	board blackboard.Blackboard
	led   ledger.Ledger
	dirs  []string
}

func newRunRig(t *testing.T, fixtureDirs ...string) *runRig {
	t.Helper()
	rr := &runRig{rig: newRig(t), out: &bytes.Buffer{}, errb: &bytes.Buffer{}, dirs: fixtureDirs}
	rr.exec = &fakeExec{rep: executor.Report{RunID: gID, Status: "verified", Sandbox: "off"}}
	rr.writeBrief(nil)
	rr.useModel()
	rr.env.Backends = func() (blackboard.Blackboard, ledger.Ledger) {
		atomic.AddInt32(&rr.backendCalls, 1)
		rr.board, rr.led = blackboard.NewFS(runDirOf), ledger.NewFS(runDirOf)
		return rr.board, rr.led
	}
	rr.env.RunExecutor = rr.exec.run
	rr.o.BriefPath = rr.brief
	rr.o.Out, rr.o.Err = rr.out, rr.errb
	return rr
}

func (rr *runRig) writeBrief(edit func(string) string) {
	rr.t.Helper()
	raw, err := os.ReadFile(filepath.Join(plannerTestdata(rr.t, "greeter"), "brief.md"))
	if err != nil {
		rr.t.Fatal(err)
	}
	text := strings.Replace(string(raw), "REPO_DIR", rr.repo, 1)
	if edit != nil {
		text = edit(text)
	}
	rr.brief = filepath.Join(rr.t.TempDir(), "brief.md")
	if err := os.WriteFile(rr.brief, []byte(text), 0o600); err != nil {
		rr.t.Fatal(err)
	}
	rr.o.BriefPath = rr.brief
}

// useModel builds the fixture provider and wires it through Env.BuildProviders.
func (rr *runRig) useModel() {
	rr.t.Helper()
	fake, err := planner.FixtureProvider(append(append([]string{}, rr.dirs...), plannerTestdata(rr.t, "greeter"))...)
	if err != nil {
		rr.t.Fatal(err)
	}
	rr.setModel(fake)
}

func (rr *runRig) setModel(p *provider.Fake) {
	rr.fake = p
	rr.env.BuildProviders = func(*settings.Config, func(string) (string, error)) (map[string]provider.Provider, error) {
		atomic.AddInt32(&rr.providerCalls, 1)
		return map[string]provider.Provider{"fake": p}, nil
	}
}

func (rr *runRig) runDir() string { return filepath.Join(rr.repo, ".gophermind", gID) }

func (rr *runRig) run() Result {
	rr.t.Helper()
	return Run(context.Background(), rr.o, rr.env)
}

func (rr *runRig) project() ProjectReport {
	rr.t.Helper()
	raw, err := os.ReadFile(filepath.Join(rr.runDir(), "_state", "project.json"))
	if err != nil {
		rr.t.Fatal(err)
	}
	var p ProjectReport
	if err := json.Unmarshal(raw, &p); err != nil {
		rr.t.Fatal(err)
	}
	return p
}

func (rr *runRig) hasProject() bool {
	_, err := os.Stat(filepath.Join(rr.runDir(), "_state", "project.json"))
	return err == nil
}

// withPlanner replaces the planner call for one test.
func withPlanner(t *testing.T, fn func(context.Context, planner.Deps, planner.Options) (planner.Outcome, error)) {
	t.Helper()
	old := runPlanner
	runPlanner = fn
	t.Cleanup(func() { runPlanner = old })
}

func wantResult(t *testing.T, got Result, code int, status, stop string) {
	t.Helper()
	if got.ExitCode != code || got.Status != status || got.StopReason != stop {
		t.Fatalf("Result = %+v, want exit %d status %q stop %q", got, code, status, stop)
	}
}

func headOf(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestRunInvalidBriefExit2(t *testing.T) {
	rr := newRunRig(t)
	if err := os.WriteFile(rr.brief, []byte("this is not a brief\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := rr.run()
	wantResult(t, got, ExitInvalid, "invalid_brief", "invalid_brief")
	if !strings.Contains(rr.errb.String(), "invalid brief: ") {
		t.Errorf("Err = %q", rr.errb.String())
	}
	if rr.providerCalls != 0 || rr.backendCalls != 0 || rr.exec.calls != 0 {
		t.Errorf("work started: providers %d backends %d executor %d", rr.providerCalls, rr.backendCalls, rr.exec.calls)
	}
}

func TestRunUnreadableBriefIsExit1(t *testing.T) {
	rr := newRunRig(t)
	rr.o.BriefPath = filepath.Join(t.TempDir(), "missing.md")
	wantResult(t, rr.run(), ExitFailed, "failed", "brief_unreadable")
}

func TestRunPrintStatePathsOnly(t *testing.T) {
	rr := newRunRig(t)
	rr.o.PrintStatePaths = true
	rr.env.Probe = func(context.Context, *settings.Config) (*settings.Config, []envcheck.ProbeResult) {
		t.Error("probe ran")
		return nil, nil
	}
	got := rr.run()
	wantResult(t, got, ExitVerified, "", "")
	for _, want := range []string{"delete_dir\t", "git_branch_delete\t", "delete_file\t"} {
		if !strings.Contains(rr.out.String(), want) {
			t.Errorf("Out lacks %q: %q", want, rr.out.String())
		}
	}
	if rr.providerCalls != 0 || rr.backendCalls != 0 || rr.exec.calls != 0 {
		t.Errorf("work started")
	}
	if _, err := os.Stat(rr.runDir()); err == nil {
		t.Error("a run folder was made")
	}
}

func breakPreflight(t *testing.T, rr *runRig) {
	t.Helper()
	if err := os.Remove(filepath.Join(rr.cfgDir, "gophermind.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestRunPreflightFailureStopsEverything(t *testing.T) {
	rr := newRunRig(t)
	breakPreflight(t, rr)
	got := rr.run()
	wantResult(t, got, ExitPreflight, "preflight_failed", "preflight")
	if rr.backendCalls != 0 || rr.providerCalls != 0 || rr.exec.calls != 0 || rr.fake.Calls() != 0 {
		t.Errorf("backends %d providers %d executor %d model %d", rr.backendCalls, rr.providerCalls, rr.exec.calls, rr.fake.Calls())
	}
	if rr.hasProject() {
		t.Error("project.json written")
	}
	if !strings.Contains(rr.errb.String(), "  1. ") || !strings.Contains(rr.errb.String(), "Fix: ") {
		t.Errorf("Err is not the numbered list: %q", rr.errb.String())
	}
	if rr.out.Len() != 0 {
		t.Errorf("Out = %q", rr.out.String())
	}
}

func TestPreflightFailureExit6IsNotARun(t *testing.T) {
	rr := newRunRig(t)
	breakPreflight(t, rr)
	before := snapshot(t, rr.repo, rr.cfgDir)
	got := rr.run()
	after := snapshot(t, rr.repo, rr.cfgDir)
	wantResult(t, got, ExitPreflight, "preflight_failed", "preflight")
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("a failed preflight wrote files:\nbefore %v\nafter  %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(rr.repo, ".gophermind")); err == nil {
		t.Error("run folder exists")
	}
	if _, err := os.Stat(filepath.Join(rr.cfgDir, "runs", gID+".json")); err == nil {
		t.Error("run record exists")
	}
}

func TestRunPreflightOnly(t *testing.T) {
	rr := newRunRig(t)
	rr.o.PreflightOnly = true
	wantResult(t, rr.run(), ExitVerified, "", "")
	if !strings.Contains(rr.out.String(), "preflight: ok") {
		t.Errorf("Out = %q", rr.out.String())
	}
	if rr.backendCalls != 0 || rr.providerCalls != 0 {
		t.Error("work started")
	}
}

func TestRunPlanStageFailureSkipsExecutor(t *testing.T) {
	rr := newRunRig(t, plannerTestdata(t, "greeter-stuck"))
	got := rr.run()
	wantResult(t, got, ExitFailed, "failed", "plan:coverage")
	if rr.exec.calls != 0 {
		t.Fatal("the executor ran")
	}
	if !rr.hasProject() {
		t.Fatal("no _state/project.json")
	}
	p := rr.project()
	if len(p.ByNodeClass) != 0 {
		t.Errorf("by_node_class = %v", p.ByNodeClass)
	}
	text := rr.out.String()
	if !strings.Contains(text, "node classes: executor did not run") {
		t.Errorf("text lacks the class line: %q", text)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	n := len(lines)
	if n < 2 || !strings.HasPrefix(lines[n-2], "Requirements covered: ") || !strings.HasPrefix(lines[n-1], "Acceptance passed: 0 of ") {
		t.Errorf("the last two lines are not the proof lines: %q", lines[max(0, n-2):])
	}
	if strings.Contains(text, "resume with --resume") {
		t.Error("a plan failure told the user to resume")
	}
}

func TestRunCallsExecutorWithPlanArtifacts(t *testing.T) {
	rr := newRunRig(t)
	rr.seed(vault.HarnessScope, map[string]string{"FOO_KEY": "canary-value-1"})
	rr.writeBrief(func(s string) string {
		s = strings.Replace(s, "repo: "+rr.repo, "repo: "+t.TempDir(), 1)
		return strings.Replace(s, "on_ambiguity: halt\n", "on_ambiguity: halt\nsecrets:\n  - name: FOO_KEY\n    purpose: test key\n", 1)
	})
	got := rr.run()
	wantResult(t, got, ExitVerified, "verified", "")
	if rr.exec.calls != 1 {
		t.Fatalf("executor calls = %d", rr.exec.calls)
	}
	o := rr.exec.opts
	if o.RunDir != rr.runDir() {
		t.Errorf("RunDir = %q, want %q", o.RunDir, rr.runDir())
	}
	if o.Repo != rr.repo {
		t.Errorf("Repo = %q, want the override %q", o.Repo, rr.repo)
	}
	res, err := o.Gate.Escalate(context.Background(), human.Escalation{NodeID: "x"})
	if err != nil || res.Action != human.ActionStop || res.AnsweredBy != human.AnsweredByUnattended {
		t.Errorf("Escalate = %+v, %v", res, err)
	}
	if o.Secrets == nil {
		t.Error("Secrets is nil though the brief declares one")
	}
	if o.Board != rr.board || o.Ledger != rr.led {
		t.Error("Board or Ledger is not the instance Env.Backends returned")
	}
	if o.Caller == nil || o.LedgerErrors == nil || o.Settings == nil {
		t.Errorf("Caller/LedgerErrors/Settings missing: %+v", o)
	}
	host := strings.TrimPrefix(rr.srv.URL, "http://")
	host = host[:strings.LastIndex(host, ":")]
	if len(o.EnvNotes) == 0 || !strings.Contains(strings.Join(o.EnvNotes, "\n"), host) {
		t.Errorf("EnvNotes = %v, want the answering host %s", o.EnvNotes, host)
	}
	p := rr.project()
	if p.Repo.Path != rr.repo || p.Repo.BriefRepo == rr.repo || p.Repo.HeadAtStart != headOf(t, rr.repo) {
		t.Errorf("repo info = %+v", p.Repo)
	}
	if len(p.Secrets) != 1 || p.Secrets[0].Name != "FOO_KEY" || p.Secrets[0].Source != SourceVault {
		t.Errorf("secrets = %+v", p.Secrets)
	}
	sum := 0
	for _, c := range p.ByNodeClass {
		sum += c.Leaves
	}
	if sum == 0 || sum != p.Plan.Functions || p.Plan.Waves < 1 {
		t.Errorf("class table leaves %d, plan info %+v, classes %+v", sum, p.Plan, p.ByNodeClass)
	}
	if strings.Contains(rr.out.String()+rr.errb.String(), "canary-value-1") {
		t.Error("a secret value was printed")
	}
}

func TestRunNoSecretsMeansNilExecutorSecrets(t *testing.T) {
	rr := newRunRig(t)
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if rr.exec.opts.Secrets != nil {
		t.Errorf("Secrets = %v, want nil", rr.exec.opts.Secrets)
	}
}

func TestRunMapsExecutorStatusesToExitCodes(t *testing.T) {
	rows := []struct {
		name, status, stop string
		code               int
	}{
		{"verified", "verified", "", 0},
		{"failed", "failed", "acceptance_failed", 1},
		{"escalated", "escalated", "human_stop", 4},
		{"waiting", "escalated", "waiting_on_human", 3},
		{"interrupted", "interrupted", "interrupted", 5},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			rr := newRunRig(t)
			rr.exec.rep = executor.Report{RunID: gID, Status: row.status, StopReason: row.stop, Sandbox: "off"}
			wantResult(t, rr.run(), row.code, row.status, row.stop)
			p := rr.project()
			if p.ExitCode != row.code || p.Status != row.status || p.Executor == nil {
				t.Errorf("project.json = %d %s executor %v", p.ExitCode, p.Status, p.Executor)
			}
			if row.status == "interrupted" && !strings.Contains(rr.out.String(), "resume with --resume") {
				t.Errorf("an interrupted run does not say how to resume: %q", rr.out.String())
			}
			if row.status != "interrupted" && strings.Contains(rr.out.String()+rr.errb.String(), "--resume") {
				t.Errorf("a non-interrupted run mentions --resume")
			}
		})
	}
	t.Run("error", func(t *testing.T) {
		rr := newRunRig(t)
		rr.exec.err = errors.New("executor: no repo")
		wantResult(t, rr.run(), ExitFault, "harness_fault", "harness_fault")
		if !strings.Contains(rr.errb.String(), "error: executor: no repo") {
			t.Errorf("Err = %q", rr.errb.String())
		}
		if p := rr.project(); p.ExitCode != ExitFault || p.Executor != nil {
			t.Errorf("project.json = %+v", p)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		rr := newRunRig(t)
		rr.exec.err = &brief.InvalidError{Reason: "x"}
		wantResult(t, rr.run(), ExitInvalid, "invalid_brief", "invalid_brief")
	})
}

func TestRunInterruptedContextExit5(t *testing.T) {
	rr := newRunRig(t)
	started, release := make(chan struct{}, 1), make(chan struct{})
	rr.setModel(provider.NewFake("fake", []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}},
		func(int, provider.Request) (provider.Response, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return provider.Response{}, context.Canceled
		}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() { done <- Run(ctx, rr.o, rr.env) }()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the planner never called the model")
	}
	cancel()
	close(release)
	var got Result
	select {
	case got = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	wantResult(t, got, ExitInterrupted, "interrupted", "interrupted")
	if !strings.Contains(rr.out.String(), "resume with --resume") {
		t.Errorf("Out = %q", rr.out.String())
	}
	if rr.exec.calls != 0 {
		t.Error("the executor ran")
	}
	if !rr.hasProject() {
		t.Error("no project.json after an interrupt")
	}
}

func TestEveryStopConditionHasReasonAndCode(t *testing.T) {
	bad := `[{"id":"q1","question":"Which store?"}]`
	rows := []struct {
		name         string
		setup        func(t *testing.T, rr *runRig)
		fixtures     []string
		status, stop string
		code         int
	}{
		{name: "preflight", setup: func(t *testing.T, rr *runRig) { breakPreflight(t, rr) }, status: "preflight_failed", stop: "preflight", code: 6},
		{name: "invalid brief", setup: func(t *testing.T, rr *runRig) {
			if err := os.WriteFile(rr.brief, []byte("nope\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, status: "invalid_brief", stop: "invalid_brief", code: 2},
		{name: "clarify without recommendations", fixtures: []string{variant(t, map[string]string{"clarify.txt": bad, "clarify.2.txt": bad})},
			status: "failed", stop: "plan:clarify", code: 1},
		{name: "confirm with an open question", setup: func(t *testing.T, rr *runRig) {
			withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
				first := o
				first.StopAfter = "clarify"
				if _, err := planner.New(d).Run(ctx, first); err != nil {
					return "", err
				}
				reopenFirstQuestion(t, rr.runDir())
				return planner.New(d).Run(ctx, planner.Options{RunID: gID, Repo: o.Repo, Unattended: o.Unattended})
			})
		}, status: "failed", stop: "plan:confirm", code: 1},
		{name: "coverage gap", fixtures: []string{plannerTestdata(t, "greeter-stuck")}, status: "failed", stop: "plan:coverage", code: 1},
		{name: "executor failed", setup: func(t *testing.T, rr *runRig) {
			rr.exec.rep = executor.Report{RunID: gID, Status: "failed", StopReason: "acceptance_failed"}
		}, status: "failed", stop: "acceptance_failed", code: 1},
		{name: "executor escalated", setup: func(t *testing.T, rr *runRig) {
			rr.exec.rep = executor.Report{RunID: gID, Status: "escalated", StopReason: "human_stop"}
		}, status: "escalated", stop: "human_stop", code: 4},
		{name: "executor interrupted", setup: func(t *testing.T, rr *runRig) {
			rr.exec.rep = executor.Report{RunID: gID, Status: "interrupted", StopReason: "max_run_minutes"}
		}, status: "interrupted", stop: "max_run_minutes", code: 5},
		{name: "harness fault", setup: func(t *testing.T, rr *runRig) { rr.exec.err = errors.New("executor: plan files changed") },
			status: "harness_fault", stop: "harness_fault", code: 7},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			rr := newRunRig(t, row.fixtures...)
			if row.setup != nil {
				row.setup(t, rr)
			}
			got := rr.run()
			wantResult(t, got, row.code, row.status, row.stop)
			if got.StopReason == "" {
				t.Error("empty StopReason")
			}
			if rr.hasProject() {
				if p := rr.project(); p.ExitCode != row.code || p.StopReason != row.stop {
					t.Errorf("project.json = %d %q", p.ExitCode, p.StopReason)
				}
			}
		})
	}
}

// reopenFirstQuestion marks the first settled clarify question open again and
// drops its answer, the way planner/unattended_test.go does.
func reopenFirstQuestion(t *testing.T, runDir string) {
	t.Helper()
	path := filepath.Join(runDir, "_state", "clarify", "questions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"status": "settled"`, `"status": "open"`, 1)
	if edited == string(raw) {
		edited = strings.Replace(string(raw), `"status":"settled"`, `"status":"open"`, 1)
	}
	if edited == string(raw) {
		t.Fatal("could not reopen a question")
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	var af struct {
		Answers []map[string]any `json:"answers"`
	}
	ans, err := os.ReadFile(filepath.Join(runDir, "answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(ans, &af); err != nil {
		t.Fatal(err)
	}
	af.Answers = af.Answers[1:]
	out, _ := json.Marshal(af)
	if err := os.WriteFile(filepath.Join(runDir, "answers.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// countGate counts the calls an unattended run must never make.
type countGate struct {
	human.Gate
	asks, confirms int
}

func (g *countGate) Ask(ctx context.Context, qs []human.Question) ([]human.Answer, error) {
	g.asks++
	return g.Gate.Ask(ctx, qs)
}

func (g *countGate) Confirm(ctx context.Context, u human.Understanding) (human.Decision, error) {
	g.confirms++
	return g.Gate.Confirm(ctx, u)
}

func TestRunUnattendedPlansOverHaltWithoutAskingTheGate(t *testing.T) {
	rr := newRunRig(t)
	var cg *countGate
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		cg = &countGate{Gate: d.Gate}
		d.Gate = cg
		return planner.New(d).Run(ctx, o)
	})
	in, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := w.WriteString("SENTINEL\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	rr.o.In = in
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if cg.asks != 0 || cg.confirms != 0 {
		t.Errorf("the gate was asked %d questions and %d confirmations", cg.asks, cg.confirms)
	}
	buf := make([]byte, 16)
	if n, _ := in.Read(buf); string(buf[:n]) != "SENTINEL\n" {
		t.Errorf("an unattended run read o.In (left %q)", buf[:n])
	}
	ap, ok, err := planner.ReadApproval(rr.runDir())
	if err != nil || !ok || ap.ApprovedBy != "unattended" || ap.UnderstandingHash == "" {
		t.Fatalf("approval = %+v %v %v", ap, ok, err)
	}
	u, ok, err := planner.ReadUnderstanding(rr.runDir())
	if err != nil || !ok || u.ConfirmedBy != "unattended" || u.Hash != ap.UnderstandingHash {
		t.Fatalf("understanding = %+v %v %v", u, ok, err)
	}
	p := rr.project()
	if !strings.Contains(p.Ambiguity.Effective, "unattended") || p.Ambiguity.BriefSetting != "halt" {
		t.Errorf("ambiguity = %+v", p.Ambiguity)
	}
	if p.Understanding.ConfirmedBy != "unattended" || p.Approval.By != "unattended" || p.Mode != "unattended" {
		t.Errorf("project = %+v %+v %s", p.Understanding, p.Approval, p.Mode)
	}
	if !strings.Contains(rr.out.String(), "understanding: confirmed by unattended") {
		t.Errorf("text lacks the understanding line: %q", rr.out.String())
	}
	if len(p.Ambiguity.ClarifyDefaulted) != 1 || p.Ambiguity.ClarifyDefaulted[0].ID != "q1" || p.Ambiguity.Rounds < 1 {
		t.Errorf("ambiguity counts = %+v", p.Ambiguity)
	}
	if len(p.Stages) == 0 {
		t.Error("no stages recorded")
	}
}

func TestRunAttendedUsesTerminalGate(t *testing.T) {
	rr := newRunRig(t)
	var isTerminal bool
	var cg *countGate
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		_, isTerminal = d.Gate.(*human.Terminal)
		cg = &countGate{Gate: d.Gate}
		d.Gate = cg
		if o.Unattended {
			t.Error("an attended run set Unattended")
		}
		return planner.New(d).Run(ctx, o)
	})
	in, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := w.WriteString("accept\ny\ny\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	rr.o.Attended, rr.o.In = true, in
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if !isTerminal {
		t.Error("Deps.Gate is not the terminal gate")
	}
	if cg.confirms != 1 {
		t.Errorf("Confirm asked %d times, want 1", cg.confirms)
	}
	if _, isT := rr.exec.opts.Gate.(*human.Terminal); !isT {
		t.Errorf("the executor gate is %T", rr.exec.opts.Gate)
	}
	p := rr.project()
	if p.Mode != "attended" || p.Approval.By != "terminal" {
		t.Errorf("mode %s approval %+v", p.Mode, p.Approval)
	}
}

func TestRunResumeSkipsFinishedPlannerStages(t *testing.T) {
	rr := newRunRig(t)
	rr.exec.rep = executor.Report{RunID: gID, Status: "interrupted", StopReason: "interrupted", Sandbox: "off"}
	wantResult(t, rr.run(), ExitInterrupted, "interrupted", "interrupted")
	if rr.project().Resumed {
		t.Error("the first run says resumed")
	}
	callsBefore := rr.fake.Calls()
	if callsBefore == 0 {
		t.Fatal("the first run made no model calls")
	}
	var seen planner.Options
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		seen = o
		return planner.New(d).Run(ctx, o)
	})
	rr.exec.rep = executor.Report{RunID: gID, Status: "verified", Sandbox: "off"}
	rr.out.Reset()
	rr.errb.Reset()
	rr.o.Resume = true
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if n := rr.fake.Calls(); n != callsBefore {
		t.Errorf("the resumed run made %d planner calls", n-callsBefore)
	}
	if seen.RunID != gID || seen.BriefPath != "" {
		t.Errorf("planner options on resume = %+v", seen)
	}
	if rr.exec.calls != 2 {
		t.Errorf("executor calls = %d", rr.exec.calls)
	}
	if !rr.project().Resumed || !strings.Contains(rr.out.String(), "resumed: yes") {
		t.Errorf("resumed not reported: %q", rr.out.String())
	}
}

func TestRunGradedReportsResumedNo(t *testing.T) {
	rr := newRunRig(t)
	var opts []planner.Options
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		opts = append(opts, o)
		return planner.New(d).Run(ctx, o)
	})
	rr.o.Graded, rr.o.ExpectHead = true, headOf(t, rr.repo)
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if len(opts) != 1 || opts[0].RunID != "" || opts[0].BriefPath == "" {
		t.Fatalf("planner runs = %+v, want exactly one from the brief path", opts)
	}
	if !strings.Contains(rr.out.String(), "graded: yes") || !strings.Contains(rr.out.String(), "resumed: no") {
		t.Errorf("Out = %q", rr.out.String())
	}
	if strings.Contains(rr.out.String()+rr.errb.String(), "--resume") {
		t.Error("the healthy path mentions --resume")
	}
	if rr.exec.calls != 1 {
		t.Errorf("executor calls = %d", rr.exec.calls)
	}
}

func TestRunGradedExecutorResumedIsInvalid(t *testing.T) {
	rr := newRunRig(t)
	rr.exec.rep = executor.Report{RunID: gID, Status: "verified", Resumed: true, Sandbox: "off"}
	rr.o.Graded, rr.o.ExpectHead = true, headOf(t, rr.repo)
	rr.run()
	if !strings.Contains(rr.out.String(), "graded: INVALID (the run resumed)") {
		t.Errorf("Out = %q", rr.out.String())
	}
}

func TestGradedRefusesSecondInvocation(t *testing.T) {
	rr := newRunRig(t)
	rr.o.Graded, rr.o.ExpectHead = true, headOf(t, rr.repo)
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	rr.errb.Reset()
	rr.out.Reset()
	got := rr.run()
	wantResult(t, got, ExitPreflight, "preflight_failed", "preflight")
	if !strings.Contains(rr.errb.String(), "stale state") {
		t.Errorf("Err = %q", rr.errb.String())
	}
	if rr.exec.calls != 1 {
		t.Errorf("executor calls = %d", rr.exec.calls)
	}
}

func TestRunReportsAnsweringHostAndCounts(t *testing.T) {
	rr := newRunRig(t)
	probe := rr.env.Probe
	rr.env.Probe = func(ctx context.Context, cfg *settings.Config) (*settings.Config, []envcheck.ProbeResult) {
		c, res := probe(ctx, cfg)
		for i := range res {
			res[i].Host, res[i].Fallback, res[i].Answered = "fallback.example.net", true, true
		}
		return c, res
	}
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		d.Sink.Emit(events.Event{Kind: events.KindWarning, Stage: "decompose", Message: "leaf_defaulted: 3 nodes still failed the leaf checks"})
		d.Sink.Emit(events.Event{Kind: events.KindWarning, Stage: "decompose", Message: "leaf_defaulted: 2 nodes still failed the leaf checks"})
		return planner.New(d).Run(ctx, o)
	})
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if !strings.Contains(rr.out.String(), "answered on fallback.example.net (fallback)") {
		t.Errorf("Out = %q", rr.out.String())
	}
	p := rr.project()
	if len(p.Providers) != 1 || p.Providers[0].Host != "fallback.example.net" || !p.Providers[0].Fallback {
		t.Errorf("providers = %+v", p.Providers)
	}
	if p.Warnings.LeafDefaulted != 5 {
		t.Errorf("planner_warnings = %+v", p.Warnings)
	}
	if !strings.Contains(rr.errb.String(), "warning: leaf_defaulted: 3") {
		t.Errorf("progress did not print the warning: %q", rr.errb.String())
	}
	if !strings.Contains(strings.Join(rr.exec.opts.EnvNotes, "\n"), "fallback.example.net") {
		t.Errorf("EnvNotes = %v", rr.exec.opts.EnvNotes)
	}
}

func TestRunBinaryStampFromEnv(t *testing.T) {
	rr := newRunRig(t)
	rr.env.Executable = func() (string, error) { return "/opt/test/gophermind-dev", nil }
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	p := rr.project()
	if p.Binary.Path != "/opt/test/gophermind-dev" || p.Binary.Version != "v0.0.0-test" || p.Binary.Commit != "abc1234def" {
		t.Errorf("binary = %+v", p.Binary)
	}
	if !strings.HasPrefix(rr.out.String(), "gophermind v0.0.0-test (commit abc1234def") {
		t.Errorf("Out starts %q", rr.out.String()[:60])
	}
}

// blockedWriter never accepts a byte.
type blockedWriter struct{ release chan struct{} }

func (b blockedWriter) Write(p []byte) (int, error) { <-b.release; return len(p), nil }

func TestRunProgressWriterNeverBlocksTheRun(t *testing.T) {
	rr := newRunRig(t)
	bw := blockedWriter{release: make(chan struct{})}
	defer close(bw.release)
	rr.o.Err = bw
	done := make(chan Result, 1)
	go func() { done <- rr.run() }()
	select {
	case got := <-done:
		wantResult(t, got, ExitVerified, "verified", "")
	case <-time.After(60 * time.Second):
		t.Fatal("a stuck progress writer blocked the run")
	}
}

func TestRunGateErrorFromApproveIsNeverApproval(t *testing.T) {
	rr := newRunRig(t)
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		d.Gate = failApprove{d.Gate}
		return planner.New(d).Run(ctx, o)
	})
	got := rr.run()
	if got.ExitCode == ExitVerified || rr.exec.calls != 0 {
		t.Fatalf("Result = %+v, executor calls %d", got, rr.exec.calls)
	}
	wantResult(t, got, ExitFailed, "failed", "plan:approve")
	if _, ok, _ := planner.ReadApproval(rr.runDir()); ok {
		t.Error("approval.json written")
	}
}

type failApprove struct{ human.Gate }

func (failApprove) Approve(context.Context, human.PlanSummary) (human.Decision, error) {
	return human.Decision{}, errors.New("gate: disk on fire")
}

// failList fails List once the executor has run, to prove the class table never fails a run.
type failList struct {
	blackboard.Blackboard
	ran *fakeExec
}

func (f failList) List(ctx context.Context, id string, flt blackboard.Filter) ([]blackboard.Row, error) {
	if f.ran.calls > 0 {
		return nil, errors.New("list: boom")
	}
	return f.Blackboard.List(ctx, id, flt)
}

func TestRunClassTableReadErrorIsAWarning(t *testing.T) {
	rr := newRunRig(t)
	rr.env.Backends = func() (blackboard.Blackboard, ledger.Ledger) {
		rr.board, rr.led = failList{blackboard.NewFS(runDirOf), rr.exec}, ledger.NewFS(runDirOf)
		return rr.board, rr.led
	}
	wantResult(t, rr.run(), ExitVerified, "verified", "")
	if p := rr.project(); len(p.ByNodeClass) != 0 {
		t.Errorf("by_node_class = %v", p.ByNodeClass)
	}
	if !strings.Contains(rr.errb.String(), "warning: ") || !strings.Contains(rr.errb.String(), "node class") {
		t.Errorf("no warning line: %q", rr.errb.String())
	}
}

// variant writes canned model replies into a temp dir to put in front of the
// greeter fixture.
func variant(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
