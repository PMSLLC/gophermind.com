package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/tree"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/gitenv"
)

const cliCanary = "CANARY-CLI-SECRET"

var greeterExecDir = func() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "gophermind-lib", "briefv2", "executor", "testdata", "greeter")
}()

const execGreeterID = "gm-2026-09-30-901"

// A git hook (the pre-push gate runs these tests) exports GIT_DIR,
// GIT_WORK_TREE, GIT_INDEX_FILE and friends. Left in the environment they make
// every `git init` and `git commit` below, and the gitland calls the executor
// makes under test, act on the real repository. No test of this package may
// inherit them.
func init() {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
}

// insideTemp reports whether dir is under the OS temp directory.
func insideTemp(t *testing.T, dir string) bool {
	t.Helper()
	d, err1 := filepath.EvalSymlinks(dir)
	tmp, err2 := filepath.EvalSymlinks(os.TempDir())
	return err1 == nil && err2 == nil && strings.HasPrefix(d+string(filepath.Separator), tmp+string(filepath.Separator))
}

// gitOut runs git in dir, which must be a temporary directory, with a clean
// environment: no inherited GIT_* variable, no user or system configuration, a
// throwaway HOME, fixed author and committer.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if !insideTemp(t, dir) {
		t.Fatalf("refusing to run git in %q: not a temporary directory", dir)
	}
	bin := os.Getenv("GITLAND_TEST_GIT")
	if bin == "" {
		bin = "git"
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(gitenv.SanitizedEnv(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=Cli", "GIT_AUTHOR_EMAIL=cli@example.com", "GIT_COMMITTER_NAME=Cli", "GIT_COMMITTER_EMAIL=cli@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", args[0], err, out)
	}
	return string(out)
}

func gitIn(t *testing.T, dir string, args ...string) { t.Helper(); gitOut(t, dir, args...) }

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// newGitRepo makes a repository holding the greeter's seed files and one commit.
func newGitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	copyDir(t, filepath.Join(greeterExecDir, "repo"), repo)
	for name, text := range map[string]string{"go.mod": "module example.com/greeter\n\ngo 1.22\n", ".gitignore": ".gophermind/\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "config", "user.email", "cli@example.com")
	gitIn(t, repo, "config", "user.name", "Cli")
	gitIn(t, repo, "config", "commit.gpgsign", "false")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "seed")
	if top := strings.TrimSpace(gitOut(t, repo, "rev-parse", "--show-toplevel")); !sameDir(top, repo) {
		t.Fatalf("the test repository resolves to %q, not %q", top, repo)
	}
	return repo
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// The guard: even with a hook's GIT_* variables set, the helper builds its
// repository in the temp dir and touches nothing else.
func TestGitHelpersIgnoreInheritedGitVariables(t *testing.T) {
	decoy := t.TempDir()
	t.Setenv("GIT_DIR", filepath.Join(decoy, "decoy.git"))
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, "decoy-index"))
	repo := newGitRepo(t)
	if top := strings.TrimSpace(gitOut(t, repo, "rev-parse", "--show-toplevel")); !sameDir(top, repo) {
		t.Fatalf("toplevel = %q, want %q", top, repo)
	}
	if entries, _ := os.ReadDir(decoy); len(entries) != 0 {
		t.Errorf("the decoy %q was written to: %d entries", decoy, len(entries))
	}
	if out := gitOut(t, repo, "log", "--format=%s"); strings.TrimSpace(out) != "seed" {
		t.Errorf("log = %q", out)
	}
	if !insideTemp(t, repo) || insideTemp(t, "/") {
		t.Error("insideTemp is wrong")
	}
}

// With a hook's variables inherited by the process, the production preflight
// still reads the repository it was given.
func TestPreflightGitIgnoresInheritedGitVariables(t *testing.T) {
	repo := newGitRepo(t)
	other := newGitRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := dirtyOutsideTests(repo, t.TempDir())
	if err != nil || n != 1 {
		t.Errorf("dirty = %d, %v; want 1 (the repository given, not GIT_DIR's)", n, err)
	}
}

// plannedGreeter plans the executor greeter offline into a fresh config dir and
// repository (landing: commit) and returns the run id and the repository. The
// vault holds GREETER_TOKEN = cliCanary.
func plannedGreeter(t *testing.T) (id, repo string) {
	t.Helper()
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	old := vaultOptions
	vaultOptions = vault.Options{WorkFactor: 10}
	t.Cleanup(func() { vaultOptions = old })
	vpath := filepath.Join(t.TempDir(), "vault.age")
	t.Setenv("GOPHERMIND_VAULT_PATH", vpath)
	t.Setenv(vault.PassphraseEnv, "pw")
	v, err := vault.Open(vpath, "pw", vaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(vault.HarnessScope, "GREETER_TOKEN", cliCanary); err != nil {
		t.Fatal(err)
	}
	repo = newGitRepo(t)
	raw, err := os.ReadFile(filepath.Join(greeterExecDir, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	briefPath := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(briefPath, []byte(strings.Replace(string(raw), "REPO_DIR", repo, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runBriefCmd(t, "", "plan", briefPath, "--yes", "--fake", filepath.Join(greeterExecDir, "planner"))
	if code != 0 {
		t.Fatalf("plan: code=%d out=%q err=%q", code, out, errs)
	}
	return execGreeterID, repo
}

// stagedProvider answers every executor call from fn, which gets the stage
// (for example implement:fn-greet) and the 1-based number of the call for it.
type stagedProvider struct {
	fn    func(ctx context.Context, stage string, k int) (string, error)
	mu    sync.Mutex
	n     map[string]int
	total int
}

func (p *stagedProvider) Name() string { return "fake" }
func (p *stagedProvider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{{ID: "fixture", ContextTokens: 1 << 20}}
}
func (p *stagedProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}
func (p *stagedProvider) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	stage := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem && strings.HasPrefix(m.Content, packer.SystemPrefix) {
			stage = strings.TrimPrefix(m.Content, packer.SystemPrefix)
		}
	}
	p.mu.Lock()
	if p.n == nil {
		p.n = map[string]int{}
	}
	p.n[stage]++
	k := p.n[stage]
	p.total++
	p.mu.Unlock()
	text, err := p.fn(ctx, stage, k)
	if err != nil {
		return provider.Response{}, err
	}
	return provider.Response{Text: text, Model: "fixture", Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

func implReply(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(greeterExecDir, "impl", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// goodReplies answers every leaf correctly; badLeaf, when set, always gets a
// wrong reply instead.
func goodReplies(t *testing.T, badLeaf string) func(context.Context, string, int) (string, error) {
	return func(_ context.Context, stage string, k int) (string, error) {
		_, id, _ := strings.Cut(stage, ":")
		if id == badLeaf {
			return implReply(t, fmt.Sprintf("bad.%s.%d.txt", id, (k-1)%3+1)), nil
		}
		return implReply(t, "good."+id+".txt"), nil
	}
}

// useRun installs runHook: the fixture settings, with the sandbox off when this
// machine cannot run one, a private module cache and the toolchain of this go.
func useRun(t *testing.T, fn func(context.Context, string, int) (string, error)) *stagedProvider {
	t.Helper()
	sp := &stagedProvider{fn: fn}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	mod := filepath.Join(t.TempDir(), "gomodcache")
	t.Cleanup(func() { makeWritable(filepath.Dir(mod)) })
	runHook = func() (*settings.Config, map[string]provider.Provider, error) {
		cfg := planner.FixtureSettings()
		cfg.Executor.GoModCache = mod
		cfg.Toolchain = map[string]string{"PATH": filepath.Dir(goBin) + ":/usr/bin:/bin"}
		if err := sandbox.Preflight(context.Background()); err != nil {
			cfg.Executor.Sandbox = "off"
		}
		return cfg, map[string]provider.Provider{"fake": sp}, nil
	}
	t.Cleanup(func() { runHook = nil })
	return sp
}

// makeWritable lets the temp dir removal delete a module cache the go tool made read-only.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink == 0 {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
}

func lastLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) < n {
		return lines
	}
	return lines[len(lines)-n:]
}

func runDirOf(repo, id string) string { return filepath.Join(repo, ".gophermind", id) }

func requireProofLines(t *testing.T, out string) {
	t.Helper()
	last := lastLines(out, 2)
	if len(last) != 2 || !strings.HasPrefix(last[0], "Requirements covered: ") || !strings.HasPrefix(last[1], "Acceptance passed: ") ||
		!strings.Contains(last[0], "7 of 7") || !strings.Contains(last[1], " of ") {
		t.Fatalf("stdout does not end with the two proof lines, it ends %q", last)
	}
	var a, b int
	if _, err := fmt.Sscanf(last[1], "Acceptance passed: %d of %d", &a, &b); err != nil || a != b || a == 0 {
		t.Errorf("acceptance proof line %q is not N of N", last[1])
	}
}

// ---- exit codes ----

func TestBriefRunExitCodes(t *testing.T) {
	t.Run("verified is 0 and the proof lines are last", func(t *testing.T) {
		id, _ := plannedGreeter(t)
		useRun(t, goodReplies(t, ""))
		code, out, errs := runBriefCmd(t, "", "run", id)
		if code != 0 {
			t.Fatalf("code=%d out=%q err=%q", code, out, errs)
		}
		requireProofLines(t, out)
		if !strings.Contains(out, "verified") || !strings.Contains(errs, "fn-farewell: verified") || !strings.Contains(errs, "wave 1 checked: pass") || !strings.Contains(errs, "Acceptance passed: ") {
			t.Errorf("out=%q err=%q", out, errs)
		}
	})
	t.Run("an escalated leaf is 4", func(t *testing.T) {
		id, _ := plannedGreeter(t)
		useRun(t, goodReplies(t, "fn-greet"))
		code, out, errs := runBriefCmd(t, "", "run", id)
		if code != exitEscalated || !strings.Contains(out, "escalated") {
			t.Fatalf("code=%d out=%q err=%q", code, out, errs)
		}
		requireProofLinesAny(t, out)
	})
	t.Run("a file gate waiting is 3 and names the run folder", func(t *testing.T) {
		id, repo := plannedGreeter(t)
		useRun(t, goodReplies(t, "fn-greet"))
		code, out, errs := runBriefCmd(t, "", "run", id, "--gate", "file")
		if code != exitWaiting || !strings.Contains(errs, "waiting: answer in "+runDirOf(repo, id)) || !strings.Contains(errs, "gophermind brief run "+id) {
			t.Fatalf("code=%d out=%q err=%q", code, out, errs)
		}
		requireProofLinesAny(t, out)
	})
	t.Run("SIGINT ends the run interrupted with exit 5 and a report", func(t *testing.T) {
		id, repo := plannedGreeter(t)
		var once sync.Once
		useRun(t, func(ctx context.Context, stage string, k int) (string, error) {
			once.Do(func() { _ = syscall.Kill(os.Getpid(), syscall.SIGINT) })
			<-ctx.Done()
			return "", ctx.Err()
		})
		code, out, errs := runBriefCmd(t, "", "run", id)
		if code != exitInterrupted || !strings.Contains(out, "interrupted") {
			t.Fatalf("code=%d out=%q err=%q", code, out, errs)
		}
		requireProofLinesAny(t, out)
		rep, err := report.Read(runDirOf(repo, id))
		if err != nil || rep.Status != "interrupted" || rep.ExitCode != 5 {
			t.Errorf("report = %+v, %v", rep, err)
		}
		if !strings.Contains(errs, "report: "+filepath.Join(runDirOf(repo, id), "report.json")) {
			t.Errorf("the report path is not printed: %q", errs)
		}
		// R11: the same command resumes and finishes the run.
		useRun(t, goodReplies(t, ""))
		code, out, errs = runBriefCmd(t, "", "run", id)
		if code != 0 {
			t.Fatalf("resume: code=%d out=%q err=%q", code, out, errs)
		}
		requireProofLines(t, out)
		if rep, err := report.Read(runDirOf(repo, id)); err != nil || !rep.Resumed || rep.Status != "verified" {
			t.Errorf("resumed report = %+v, %v", rep, err)
		}
	})
	t.Run("an unknown run id is 1", func(t *testing.T) {
		t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
		code, _, errs := runBriefCmd(t, "", "run", "gm-2026-01-01-001")
		if code != 1 || !strings.Contains(errs, "no run gm-2026-01-01-001") {
			t.Errorf("code=%d err=%q", code, errs)
		}
	})
	t.Run("an unapproved plan is 1 with no model call", func(t *testing.T) {
		id, repo := plannedGreeter(t)
		sp := useRun(t, goodReplies(t, ""))
		ap := filepath.Join(runDirOf(repo, id), "approval.json")
		if filepath.Base(ap) != "approval.json" || !strings.HasPrefix(ap, repo) {
			t.Fatal("refusing to delete an unexpected path")
		}
		if err := os.Remove(ap); err != nil {
			t.Fatal(err)
		}
		code, out, errs := runBriefCmd(t, "", "run", id)
		if code != 1 || !strings.Contains(errs, "approval") || sp.Calls() != 0 || out != "" {
			t.Errorf("code=%d out=%q err=%q calls=%d", code, out, errs, sp.Calls())
		}
		if _, err := os.Stat(filepath.Join(runDirOf(repo, id), "report.json")); err == nil {
			t.Error("a report was written for a run that never started")
		}
	})
	for _, n := range []string{"0", "2"} {
		t.Run("--workers "+n+" is 1 with no model call", func(t *testing.T) {
			id, _ := plannedGreeter(t)
			sp := useRun(t, goodReplies(t, ""))
			code, _, errs := runBriefCmd(t, "", "run", id, "--workers", n)
			if code != 1 || !strings.Contains(errs, "error: --workers must be 1 until leaf-isolated trees exist") || sp.Calls() != 0 {
				t.Errorf("code=%d err=%q calls=%d", code, errs, sp.Calls())
			}
		})
	}
	t.Run("an unknown gate is 1", func(t *testing.T) {
		id, _ := plannedGreeter(t)
		sp := useRun(t, goodReplies(t, ""))
		code, _, errs := runBriefCmd(t, "", "run", id, "--gate", "nope")
		if code != 1 || !strings.Contains(errs, `unknown gate "nope"`) || sp.Calls() != 0 {
			t.Errorf("code=%d err=%q", code, errs)
		}
	})
	t.Run("an invalid brief stays 2 for plan", func(t *testing.T) {
		_, briefPath := planEnv(t)
		raw, _ := os.ReadFile(briefPath)
		bad := filepath.Join(t.TempDir(), "b.md")
		_ = os.WriteFile(bad, []byte(strings.Replace(string(raw), "spec_version: \"2.0\"\n", "", 1)), 0o600)
		if code, _, _ := runBriefCmd(t, "", "plan", bad, "--fake", greeterFixture); code != exitInvalid || exitInvalid != 2 {
			t.Errorf("code=%d", code)
		}
	})
}

// requireProofLinesAny: the summary of a run that did not verify still ends
// with the two proof lines.
func requireProofLinesAny(t *testing.T, out string) {
	t.Helper()
	last := lastLines(out, 2)
	if len(last) != 2 || !strings.HasPrefix(last[0], "Requirements covered: ") || !strings.HasPrefix(last[1], "Acceptance passed: ") {
		t.Fatalf("stdout does not end with the two proof lines, it ends %q", last)
	}
}

func TestExitCodes(t *testing.T) {
	cases := []struct {
		status, stop string
		want         int
	}{
		{"verified", "", 0},
		{"failed", "", 1},
		{"failed", "harness_fault", 1},
		{"failed", "integration_skipped", 1},
		{"escalated", "waiting_on_human", 3},
		{"escalated", "human_stop", 4},
		{"escalated", "", 4},
		{"interrupted", "signal", 5},
		{"interrupted", "max_run_minutes", 5},
		{"interrupted", "cancelled", 5},
	}
	for _, c := range cases {
		r := executor.Report{Status: c.status, StopReason: c.stop}
		if got := exitFor(r); got != c.want {
			t.Errorf("exitFor(%s, %q) = %d, want %d", c.status, c.stop, got, c.want)
		}
	}
	for name, got := range map[string]int{"done": exitDone, "error": exitError, "invalid": exitInvalid, "waiting": exitWaiting, "escalated": exitEscalated, "interrupted": exitInterrupted, "preflight": exitPreflight} {
		want := map[string]int{"done": 0, "error": 1, "invalid": 2, "waiting": 3, "escalated": 4, "interrupted": 5, "preflight": 6}[name]
		if got != want {
			t.Errorf("exit%s = %d, want %d", name, got, want)
		}
	}
}

// ---- the executor seam: every exit path prints what it has ----

func summaryReport(id, status, stop string) executor.Report {
	r := executor.Report{SchemaVersion: report.SchemaVersion, RunID: id, Status: status, StopReason: stop, Sandbox: "off"}
	r.Requirements = report.Coverage{Covered: 7, Total: 7}
	r.Acceptance = report.Passed{Passed: 2, Total: 2}
	r.ExitCode = report.ExitCode(status, stop)
	return r
}

func TestBriefRunPrintsSummaryAndPathOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name string
		rep  executor.Report
		err  error
		code int
		note string
	}{
		{"verified", summaryReport(execGreeterID, "verified", ""), nil, 0, ""},
		{"failed", summaryReport(execGreeterID, "failed", "acceptance_failed"), nil, 1, ""},
		{"escalated", summaryReport(execGreeterID, "escalated", "human_stop"), nil, 4, ""},
		{"waiting", summaryReport(execGreeterID, "escalated", "waiting_on_human"), nil, 3, "waiting: answer in "},
		{"interrupted", summaryReport(execGreeterID, "interrupted", "signal"), nil, 5, "interrupted; continue with `gophermind brief run "},
		{"harness fault with a report", summaryReport(execGreeterID, "failed", "harness_fault"), errors.New("executor: reading the ledger for the report failed"), 1, "error: executor: reading the ledger"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, repo := plannedGreeter(t)
			useRun(t, goodReplies(t, ""))
			stubExecutor(t, func(_ context.Context, o executor.Options) (executor.Report, error) { return c.rep, c.err })
			code, out, errs := runBriefCmd(t, "", "run", id)
			if code != c.code {
				t.Fatalf("code=%d out=%q err=%q, want %d", code, out, errs, c.code)
			}
			requireProofLinesAny(t, out)
			if !strings.Contains(errs, "report: "+filepath.Join(runDirOf(repo, id), "report.json")) || !strings.Contains(errs, c.note) {
				t.Errorf("err = %q", errs)
			}
			if !strings.Contains(out, "Sandbox: off") || !strings.Contains(errs, "sandbox: off") {
				t.Errorf("sandbox off is not announced: out=%q err=%q", out, errs)
			}
		})
	}
}

func TestBriefRunHarnessErrorsWithoutAReportAreNonZero(t *testing.T) {
	for _, c := range []struct{ err, want string }{
		{"executor: plan files changed since the run started", "error: the plan changed since the run started; nothing was run"},
		{"executor: node fn-greet is held by a live worker", "error: a leaf is held by a live worker; wait for its claim to go stale and run again"},
		{"executor: preflight: go not found on PATH", "error: executor: preflight: go not found on PATH"},
	} {
		id, repo := plannedGreeter(t)
		useRun(t, goodReplies(t, ""))
		stubExecutor(t, func(context.Context, executor.Options) (executor.Report, error) {
			return executor.Report{}, errors.New(c.err)
		})
		code, out, errs := runBriefCmd(t, "", "run", id)
		if code != exitError || !strings.Contains(errs, c.want) || strings.Contains(out, "Requirements covered") {
			t.Errorf("%q: code=%d out=%q err=%q", c.err, code, out, errs)
		}
		if _, err := os.Stat(filepath.Join(runDirOf(repo, id), "report.json")); err == nil {
			t.Error("a report appeared")
		}
	}
}

func stubExecutor(t *testing.T, fn func(context.Context, executor.Options) (executor.Report, error)) {
	t.Helper()
	old := runExecutor
	runExecutor = fn
	t.Cleanup(func() { runExecutor = old })
}

func TestBriefRunRepoOverride(t *testing.T) {
	id, repo := plannedGreeter(t)
	useRun(t, goodReplies(t, ""))
	var got executor.Options
	stubExecutor(t, func(_ context.Context, o executor.Options) (executor.Report, error) {
		got = o
		return summaryReport(id, "verified", ""), nil
	})
	if code, _, errs := runBriefCmd(t, "", "run", id); code != 0 || got.Repo != repo {
		t.Fatalf("default: code=%d err=%q repo=%q want %q", code, errs, got.Repo, repo)
	}
	if got.RunDir != runDirOf(repo, id) || got.Gate == nil || got.Secrets == nil || got.Settings.Executor.Workers != 1 {
		t.Errorf("options: %s", got)
	}
	other := newGitRepo(t)
	if code, _, errs := runBriefCmd(t, "", "run", id, "--repo", other); code != 0 || got.Repo != other {
		t.Fatalf("override: code=%d err=%q repo=%q want %q", code, errs, got.Repo, other)
	}
	plain := t.TempDir()
	for _, bad := range []string{plain, filepath.Join(plain, "missing")} {
		code, _, errs := runBriefCmd(t, "", "run", id, "--repo", bad)
		if code != 1 || !strings.Contains(errs, "--repo") || strings.Contains(errs, bad) {
			t.Errorf("--repo %q: code=%d err=%q", bad, code, errs)
		}
	}
}

func TestBriefRunNeverBlocksWithoutAPassphrase(t *testing.T) {
	id, _ := plannedGreeter(t)
	sp := useRun(t, goodReplies(t, ""))
	t.Setenv(vault.PassphraseEnv, "")
	done := make(chan struct{})
	var code int
	var errs string
	go func() {
		defer close(done)
		code, _, errs = runBriefCmd(t, "", "run", id)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("brief run blocked waiting for a passphrase")
	}
	if code != 1 || !strings.Contains(errs, "error: the vault passphrase is not set: set GOPHERMIND_VAULT_PASSPHRASE (no terminal to prompt)") || sp.Calls() != 0 {
		t.Errorf("code=%d err=%q calls=%d", code, errs, sp.Calls())
	}
}

// ---- no secret, prompt or reply text in any output ----

func TestBriefRunNoSecretsPrinted(t *testing.T) {
	id, _ := plannedGreeter(t)
	useRun(t, goodReplies(t, "fn-greet"))
	code, out, errs := runBriefCmd(t, "", "run", id)
	if code != exitEscalated {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	_, rout, rerrs := runBriefCmd(t, "", "report", id)
	_, jout, _ := runBriefCmd(t, "", "report", id, "--json")
	_, sout, _ := runBriefCmd(t, "", "status", id)
	all := out + errs + rout + rerrs + jout + sout
	for _, banned := range []string{cliCanary, "package greet", "func Greet", "Hello, ", "GopherMind executor. Stage"} {
		if strings.Contains(all, banned) {
			t.Errorf("the output contains %q", banned)
		}
	}
	for _, reply := range []string{implReply(t, "bad.fn-greet.1.txt"), implReply(t, "good.fn-farewell.txt")} {
		if strings.Contains(all, strings.TrimSpace(reply)) {
			t.Errorf("the output contains a reply")
		}
	}
	cfg := os.Getenv("GOPHERMIND_CONFIG_DIR")
	_ = filepath.WalkDir(cfg, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, _ := os.ReadFile(p)
		if bytes.Contains(raw, []byte(cliCanary)) {
			t.Errorf("%s holds the secret", filepath.Base(p))
		}
		return nil
	})
}

// ---- report and status ----

func TestBriefReportPrints(t *testing.T) {
	id, repo := plannedGreeter(t)
	if code, _, errs := runBriefCmd(t, "", "report", id); code != 1 || !strings.Contains(errs, "no report for "+id+"; run gophermind brief run "+id) {
		t.Errorf("before a run: code=%d err=%q", code, errs)
	}
	useRun(t, goodReplies(t, ""))
	if code, out, errs := runBriefCmd(t, "", "run", id); code != 0 {
		t.Fatalf("run: code=%d out=%q err=%q", code, out, errs)
	}
	code, out, errs := runBriefCmd(t, "", "report", id)
	if code != 0 || errs != "" {
		t.Fatalf("report: code=%d err=%q", code, errs)
	}
	requireProofLines(t, out)
	code, jout, _ := runBriefCmd(t, "", "report", id, "--json")
	raw, err := os.ReadFile(filepath.Join(runDirOf(repo, id), "report.json"))
	if err != nil || code != 0 || jout != string(raw) || !json.Valid([]byte(jout)) {
		t.Errorf("--json: code=%d equal=%v err=%v", code, jout == string(raw), err)
	}
	if code, jout2, _ := runBriefCmd(t, "", "report", "--json", id); code != 0 || jout2 != jout {
		t.Errorf("flag before id: code=%d", code)
	}
	_, sout, _ := runBriefCmd(t, "", "status", id)
	if !strings.Contains(sout, "executor: verified") || !strings.Contains(sout, "leaves: verified 5 of 5") || strings.Contains(sout, "blocked:") || strings.Contains(sout, "waves done: 3 of 3") == false {
		t.Errorf("status after a verified run:\n%s", sout)
	}
	for _, args := range [][]string{{"report"}, {"report", id, "extra"}, {"report", id, "--yaml"}} {
		if code, _, errs := runBriefCmd(t, "", args...); code != 1 || !strings.Contains(errs, "usage:") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

func TestBriefReportReadsSchemaVersionOne(t *testing.T) {
	id, repo := plannedGreeter(t)
	r := summaryReport(id, "verified", "")
	r.SchemaVersion = 1
	raw, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(runDirOf(repo, id), "report.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runBriefCmd(t, "", "report", id)
	if code != 0 || !strings.Contains(out, "Run "+id+": verified") {
		t.Errorf("code=%d out=%q err=%q", code, out, errs)
	}
	if code, jout, _ := runBriefCmd(t, "", "report", id, "--json"); code != 0 || jout != string(append(raw, '\n')) {
		t.Errorf("--json code=%d", code)
	}
}

func TestBriefStatusExecutorLines(t *testing.T) {
	id, repo := plannedGreeter(t)
	code, before, _ := runBriefCmd(t, "", "status", id)
	if code != 0 {
		t.Fatal(before)
	}
	for _, banned := range []string{"executor:", "waves done", "leaves:", "blocked:"} {
		if strings.Contains(before, banned) {
			t.Errorf("status before a run shows %q:\n%s", banned, before)
		}
	}
	useRun(t, goodReplies(t, "fn-greet"))
	if code, out, errs := runBriefCmd(t, "", "run", id); code != exitEscalated {
		t.Fatalf("run: code=%d out=%q err=%q", code, out, errs)
	}
	code, out, _ := runBriefCmd(t, "", "status", id)
	if code != 0 {
		t.Fatal(out)
	}
	tr, err := loadTreeWaves(runDirOf(repo, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"executor: escalated",
		fmt.Sprintf("waves done: 0 of %d", tr),
		"leaves: verified 1 of 5; pending 3, escalated 1",
		"blocked: fn-hello (waiting on fn-greet)",
		"blocked: fn-serve (waiting on fn-hello)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	// The executor lines sit after the coverage line and before the model call table.
	if i, j, k := strings.Index(out, "Requirements covered"), strings.Index(out, "executor: escalated"), strings.Index(out, "model calls by task type"); !(i < j && j < k) {
		t.Errorf("executor lines are out of place:\n%s", out)
	}
}

// ---- usage ----

func TestBriefUsageListsRunAndReport(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}} {
		code, _, errs := runBriefCmd(t, "", args...)
		if code != 1 {
			t.Errorf("%v: code=%d", args, code)
		}
		for _, line := range []string{
			"gophermind brief run <run-id> [--gate terminal|file] [--repo <path>] [--workers n]",
			"gophermind brief report <run-id> [--json]",
		} {
			if !strings.Contains(errs, line) {
				t.Errorf("%v: usage lacks %q", args, line)
			}
		}
	}
	// A bad arity or flag prints the usage and exits 1.
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{{"run"}, {"run", "a", "b"}, {"run", "gm-2026-01-01-001", "--bogus"}, {"run", "gm-2026-01-01-001", "--workers", "x"}} {
		if code, _, errs := runBriefCmd(t, "", args...); code != 1 || !strings.Contains(errs, "usage:") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

// ---- the run sink ----

func TestRunSinkPrintsOnlyIdsAndCounts(t *testing.T) {
	var b bytes.Buffer
	s := &runSink{printSink: &printSink{w: &b}}
	for _, e := range []events.Event{
		{Kind: "leaf_verified", NodeID: "fn-greet"},
		{Kind: "leaf_failed", NodeID: "fn-bye", Message: "critical_failure"},
		{Kind: "leaf_escalated", NodeID: "fn-hello", Message: "exhausted"},
		{Kind: "blocked", NodeID: "fn-serve", Message: "blocked by fn-hello"},
		{Kind: "wave_check", NodeID: "wave-2", Message: "pass"},
		{Kind: "warning", NodeID: "fn-greet", Message: "weak test: 1 of 3 mutants survived"},
		{Kind: "resume", Message: "released claims: fn-greet; 1 of 5 leaves verified"},
		{Kind: "acceptance_passed", Message: "Acceptance passed: 2 of 2"},
		{Kind: "ledger_error", Message: "disk full"},
		{Kind: "leaf_started", NodeID: "fn-greet"},
		{Kind: "attempt", NodeID: "fn-greet", Message: "entry a/m1 fail build"},
		{Kind: "scan", Message: "clean"},
		{Kind: "warning", Message: "evil\x1b[31m line\nsecond line " + strings.Repeat("x", 400)},
	} {
		s.Emit(e)
	}
	out := b.String()
	for _, want := range []string{"fn-greet: verified\n", "fn-bye: failed (critical_failure)\n", "fn-hello: escalated (exhausted)\n", "fn-serve: blocked by fn-hello\n",
		"wave 2 checked: pass\n", "warning: weak test: 1 of 3 mutants survived\n", "resumed: released claims: fn-greet; 1 of 5 leaves verified\n", "Acceptance passed: 2 of 2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) > 200 || strings.ContainsAny(line, "\x1b\r") {
			t.Errorf("unsafe line %q", line)
		}
	}
	if strings.Contains(out, "entry a/m1") || strings.Contains(out, "leaf_started") {
		t.Errorf("a per-attempt event was printed:\n%s", out)
	}
}

// ---- preflight ----

type preflightEnv struct {
	id, repo string
	srv      *httptest.Server
}

func newPreflightEnv(t *testing.T) *preflightEnv {
	t.Helper()
	id, repo := plannedGreeter(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" || r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"version":"0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	runHook = func() (*settings.Config, map[string]provider.Provider, error) {
		cfg := planner.FixtureSettings()
		cfg.Providers[0].BaseURL = srv.URL + "/v1"
		cfg.Executor.Sandbox = "off"
		cfg.Executor.GoModCache = filepath.Join(t.TempDir(), "gomodcache")
		cfg.Toolchain = map[string]string{"PATH": filepath.Dir(goBin) + ":/usr/bin:/bin"}
		return cfg, nil, nil
	}
	t.Cleanup(func() { runHook = nil })
	return &preflightEnv{id: id, repo: repo, srv: srv}
}

func TestBriefRunCheckEnvPasses(t *testing.T) {
	e := newPreflightEnv(t)
	code, out, errs := runBriefCmd(t, "", "run", e.id, "--check-env")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	for _, name := range []string{"sandbox", "go toolchain", "git repository", "clean tree", "vault passphrase", "secret GREETER_TOKEN", "provider fake", "approval", "disk space"} {
		if !strings.Contains(out, "pass: "+name) {
			t.Errorf("no pass line for %q:\n%s", name, out)
		}
	}
	if strings.Contains(out, "FAIL") || strings.Contains(out+errs, cliCanary) {
		t.Errorf("out=%q err=%q", out, errs)
	}
	if !strings.HasSuffix(out, "preflight: 9 of 9 passed\n") {
		t.Errorf("last line: %q", lastLines(out, 1))
	}
	if _, err := os.Stat(filepath.Join(runDirOf(e.repo, e.id), "report.json")); err == nil {
		t.Error("a preflight wrote a run report")
	}
}

func TestBriefRunCheckEnvFailuresExitSix(t *testing.T) {
	cases := []struct {
		name   string
		fail   string // the check that must fail
		mutate func(t *testing.T, e *preflightEnv)
	}{
		{"no go on the toolchain path", "go toolchain", func(t *testing.T, e *preflightEnv) {
			old := runHook
			runHook = func() (*settings.Config, map[string]provider.Provider, error) {
				cfg, p, err := old()
				cfg.Toolchain = map[string]string{"PATH": t.TempDir()}
				return cfg, p, err
			}
		}},
		{"a stray file in the tree", "clean tree", func(t *testing.T, e *preflightEnv) {
			if err := os.WriteFile(filepath.Join(e.repo, "stray.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"no passphrase", "vault passphrase", func(t *testing.T, e *preflightEnv) { t.Setenv(vault.PassphraseEnv, "") }},
		{"a missing secret", "secret GREETER_TOKEN", func(t *testing.T, e *preflightEnv) {
			t.Setenv("GOPHERMIND_VAULT_PATH", filepath.Join(t.TempDir(), "empty.age"))
		}},
		{"a provider that does not answer", "provider fake", func(t *testing.T, e *preflightEnv) { e.srv.Close() }},
		{"no approval", "approval", func(t *testing.T, e *preflightEnv) {
			ap := filepath.Join(runDirOf(e.repo, e.id), "approval.json")
			if filepath.Base(ap) != "approval.json" || !strings.HasPrefix(ap, e.repo) {
				t.Fatal("unexpected path")
			}
			if err := os.Remove(ap); err != nil {
				t.Fatal(err)
			}
		}},
		{"no disk space", "disk space", func(t *testing.T, e *preflightEnv) {
			old := minFreeBytes
			minFreeBytes = 1 << 62
			t.Cleanup(func() { minFreeBytes = old })
		}},
		{"an unreachable secret host", "secret host GREETER_TOKEN", func(t *testing.T, e *preflightEnv) {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := l.Addr().String()
			l.Close()
			v, err := vault.Open(os.Getenv("GOPHERMIND_VAULT_PATH"), "pw", vaultOptions)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Set(vault.RunScope(e.id), "GREETER_TOKEN", "postgres://u:"+cliCanary+"@"+addr+"/db"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newPreflightEnv(t)
			c.mutate(t, e)
			code, out, errs := runBriefCmd(t, "", "run", e.id, "--check-env")
			if code != exitPreflight || !strings.Contains(out, "FAIL: "+c.fail) {
				t.Fatalf("code=%d out=%q err=%q", code, out, errs)
			}
			if strings.Contains(out+errs, cliCanary) || strings.Contains(out, "postgres://") {
				t.Errorf("a secret value or URL leaked: %q", out)
			}
			if !strings.Contains(out, "preflight: ") || strings.Contains(out, "preflight: 9 of 9") || strings.Contains(out, "preflight: 10 of 10") {
				t.Errorf("summary line: %q", lastLines(out, 1))
			}
		})
	}
}

func TestBriefRunCheckEnvSecretHostReachable(t *testing.T) {
	e := newPreflightEnv(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	v, err := vault.Open(os.Getenv("GOPHERMIND_VAULT_PATH"), "pw", vaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(vault.RunScope(e.id), "GREETER_TOKEN", "postgres://u:"+cliCanary+"@"+l.Addr().String()+"/db"); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runBriefCmd(t, "", "run", e.id, "--check-env")
	if code != 0 || !strings.Contains(out, "pass: secret host GREETER_TOKEN") || strings.Contains(out+errs, cliCanary) {
		t.Errorf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestBriefRunCheckEnvMakesNoModelCall(t *testing.T) {
	e := newPreflightEnv(t)
	sp := &stagedProvider{fn: goodReplies(t, "")}
	old := runHook
	runHook = func() (*settings.Config, map[string]provider.Provider, error) {
		cfg, _, err := old()
		return cfg, map[string]provider.Provider{"fake": sp}, err
	}
	t.Cleanup(func() { runHook = old })
	if code, out, errs := runBriefCmd(t, "", "run", e.id, "--check-env"); code != 0 || sp.Calls() != 0 {
		t.Errorf("code=%d calls=%d out=%q err=%q", code, sp.Calls(), out, errs)
	}
}

func TestBriefRunCheckEnvTakesTheFallbackThatAnswers(t *testing.T) {
	e := newPreflightEnv(t)
	dead := closedAddr(t)
	old := runHook
	runHook = func() (*settings.Config, map[string]provider.Provider, error) {
		cfg, p, err := old()
		fb := cfg.Providers[0].BaseURL
		cfg.Providers[0].BaseURL = "http://" + dead + "/v1"
		cfg.Providers[0].BaseURLFallbacks = []string{"http://" + dead + "/v1", fb}
		return cfg, p, err
	}
	t.Cleanup(func() { runHook = old })
	code, out, errs := runBriefCmd(t, "", "run", e.id, "--check-env")
	host := strings.TrimPrefix(e.srv.URL, "http://")
	host, _, _ = net.SplitHostPort(host)
	if code != 0 || !strings.Contains(out, "pass: provider fake: answered on fallback host "+host) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// ---- base URL resolution (preflight and run start) ----

func answering(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "{}") }))
	t.Cleanup(s.Close)
	return s
}

func TestResolveBaseURLsUsesTheFirstFallbackThatAnswersWithoutTouchingTheConfig(t *testing.T) {
	good := answering(t)
	cfg := planner.FixtureSettings()
	cfg.Providers[0].BaseURL = "http://" + closedAddr(t) + "/v1"
	cfg.Providers[0].BaseURLFallbacks = []string{"http://" + closedAddr(t) + "/v1", good.URL + "/v1", "http://" + closedAddr(t) + "/v1"}
	orig := *cfg
	origProviders := append([]settings.ProviderConfig(nil), cfg.Providers...)

	got, res := resolveBaseURLs(context.Background(), cfg, &http.Client{})
	if got.Providers[0].BaseURL != good.URL+"/v1" {
		t.Errorf("the run's base url = %q, want the answering fallback", got.Providers[0].BaseURL)
	}
	if len(res) != 1 || !res[0].Answered || !res[0].Fallback || res[0].Host != strings.TrimPrefix(good.URL, "http://")[:9] {
		t.Errorf("results = %+v", res)
	}
	if cfg.Providers[0].BaseURL != origProviders[0].BaseURL || orig.Providers[0].BaseURL != origProviders[0].BaseURL {
		t.Error("the caller's config was changed")
	}
	if notes := envNotes(res); len(notes) != 1 || !strings.Contains(notes[0], "base url host 127.0.0.1 answered") || strings.Contains(notes[0], "http") {
		t.Errorf("notes = %q (host only)", notes)
	}
}

func TestResolveBaseURLsPrimaryWinsAndNothingAnsweringKeepsThePrimary(t *testing.T) {
	good := answering(t)
	cfg := planner.FixtureSettings()
	cfg.Providers[0].BaseURL = good.URL + "/v1"
	cfg.Providers[0].BaseURLFallbacks = []string{"http://" + closedAddr(t) + "/v1"}
	got, res := resolveBaseURLs(context.Background(), cfg, &http.Client{})
	if got.Providers[0].BaseURL != good.URL+"/v1" || res[0].Fallback || !res[0].Answered {
		t.Errorf("primary: %q %+v", got.Providers[0].BaseURL, res)
	}
	dead := "http://" + closedAddr(t) + "/v1"
	cfg.Providers[0].BaseURL = dead
	got, res = resolveBaseURLs(context.Background(), cfg, &http.Client{})
	if got.Providers[0].BaseURL != dead || res[0].Answered {
		t.Errorf("none answered: %q %+v", got.Providers[0].BaseURL, res)
	}
}

func TestResolveBaseURLsTimesOutAHungPrimary(t *testing.T) {
	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer hung.Close()
	defer close(release)
	good := answering(t)
	old := probeTimeout
	probeTimeout = 150 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	cfg := planner.FixtureSettings()
	cfg.Providers[0].BaseURL = hung.URL + "/v1"
	cfg.Providers[0].BaseURLFallbacks = []string{good.URL + "/v1"}
	start := time.Now()
	got, res := resolveBaseURLs(context.Background(), cfg, &http.Client{})
	if got.Providers[0].BaseURL != good.URL+"/v1" || !res[0].Fallback {
		t.Errorf("got %q %+v", got.Providers[0].BaseURL, res)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func TestResolveBaseURLsOnlyProbesTheStrongTier(t *testing.T) {
	cfg := planner.FixtureSettings()
	cfg.Providers = append(cfg.Providers, settings.ProviderConfig{Name: "other", BaseURL: "http://" + closedAddr(t) + "/v1", Visibility: settings.Private, MaxConcurrent: 1,
		Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}}})
	cfg.Models["any"] = []string{"fake/fixture", "other/m"}
	good := answering(t)
	cfg.Providers[0].BaseURL = good.URL + "/v1"
	_, res := resolveBaseURLs(context.Background(), cfg, &http.Client{})
	if len(res) != 1 || res[0].Provider != "fake" {
		t.Errorf("results = %+v", res)
	}
}

// loadTreeWaves is how many distinct waves the planned leaves (fn-*) are in.
func loadTreeWaves(runDir string) (int, error) {
	tr, err := tree.NewStore(runDir).Load()
	if err != nil {
		return 0, err
	}
	waves, err := tr.ComputeWaves()
	if err != nil {
		return 0, err
	}
	seen := map[int]bool{}
	for id, w := range waves {
		if strings.HasPrefix(id, "fn-") {
			seen[w] = true
		}
	}
	return len(seen), nil
}
