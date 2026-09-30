package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
)

// newRC is the runCtx after preflight (nothing started, no git branch), closed when the test ends.
func (g *rig) newRC(t *testing.T) *runCtx {
	t.Helper()
	if g.router == nil {
		g.wire(Script{})
	}
	rc, err := newRunCtx(context.Background(), g.options())
	if err != nil {
		t.Fatalf("newRunCtx: %v", err)
	}
	t.Cleanup(rc.close)
	return rc
}

// start is startRun over the rig, closed when the test ends.
func (g *rig) start(t *testing.T) (*runCtx, error) {
	t.Helper()
	if g.router == nil {
		g.wire(Script{})
	}
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		t.Cleanup(rc.close)
	}
	return rc, err
}

func (g *rig) branches(pattern string) string {
	g.t.Helper()
	return strings.TrimSpace(g.gitCmd("branch", "--list", pattern))
}

func (g *rig) commitFiles(rev string) []string {
	g.t.Helper()
	out := strings.Fields(g.gitCmd("show", "--name-only", "--format=", rev))
	sort.Strings(out)
	return out
}

func (g *rig) testFiles() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range g.plan.Leaves {
		if !seen[l.TestFile] {
			seen[l.TestFile] = true
			out = append(out, l.TestFile)
		}
	}
	return out
}

func wantStop(t *testing.T, err error, status, reason string) *stopError {
	t.Helper()
	var se *stopError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *stopError %s/%s", err, status, reason)
	}
	if se.Status != status || se.Reason != reason {
		t.Fatalf("stop = %s/%s (%s), want %s/%s", se.Status, se.Reason, se.Message, status, reason)
	}
	return se
}

func TestPreflightPrivacyOnly(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.Settings = func(c *settings.Config) {
			c.Privacy.Mode = "private_only"
			for i := range c.Providers {
				c.Providers[i].Visibility = settings.Public
			}
		}
	})
	_, err := g.start(t)
	if err == nil || !strings.Contains(err.Error(), "privacy.mode") {
		t.Fatalf("err = %v, want text naming privacy.mode", err)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("%d model calls before the refusal", n)
	}
	if g.branches("gm/*") != "" {
		t.Fatal("a work branch exists after a preflight refusal")
	}
}

func TestPullRequestUnsupported(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: pull_request", 1) }
	})
	_, err := g.start(t)
	if err == nil || !strings.Contains(err.Error(), "landing") {
		t.Fatalf("err = %v, want text naming landing", err)
	}
	if g.branches("gm/*") != "" {
		t.Fatal("the repository was touched before the landing check")
	}
}

func TestFreshRunAllowsTestWriterFiles(t *testing.T) {
	g := newRig(t)
	rc, err := g.start(t)
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
	if !strings.Contains(g.branches("gm/*"), "gm/"+g.id) {
		t.Fatal("the work branch does not exist")
	}
	want := append([]string{}, g.testFiles()...)
	for _, ty := range g.plan.Contracts.Types {
		want = append(want, ty.File)
	}
	for _, l := range g.plan.Leaves {
		want = append(want, l.StubFile)
	}
	want = dedupe(want)
	sort.Strings(want)
	got := g.commitFiles("HEAD")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Wave 0 commit holds\n%v\nwant\n%v", got, want)
	}
	if d, _ := g.git.Dirty(); len(d) != 0 {
		t.Fatalf("tree dirty after Wave 0: %v", d)
	}
	if !rc.state.Wave0Done {
		t.Fatal("Wave0Done is not set")
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func TestFreshRunDirtyTreeRefused(t *testing.T) {
	g := newRig(t)
	write(t, filepath.Join(g.repo, "notes.txt"), "CANARY-notes\n")
	_, err := g.start(t)
	if err == nil || !strings.Contains(err.Error(), "repository is not clean") || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("err = %v, want the friendly message naming notes.txt", err)
	}
	if strings.Contains(err.Error(), "CANARY-notes") {
		t.Fatal("file content reached the error")
	}
	if g.branches("gm/*") != "" {
		t.Fatal("a work branch was created for a dirty tree")
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("%d model calls", n)
	}
	if fileExists(filepath.Join(g.runDir, "_state", "executor.json")) {
		t.Fatal("state was written for a refused start")
	}
}

func TestNoRemoteRequired(t *testing.T) {
	g := newRig(t)
	if _, err := g.start(t); err != nil {
		t.Fatalf("startRun on a repository with no remote: %v", err)
	}
	if out := strings.TrimSpace(g.gitCmd("remote")); out != "" {
		t.Fatalf("git remote = %q, want none", out)
	}
}

func TestProxyAllowlistFromBriefAndProviders(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string {
			return strings.Replace(s, "\n---\n\n## Overview", "\nnetwork:\n  - host: api.example.com\n    purpose: \"x\"\n    critical: true\n  - host: docs.example.com\n    purpose: \"y\"\n    critical: false\n---\n\n## Overview", 1)
		}
		o.Settings = func(c *settings.Config) {
			c.Providers[1].BaseURL = "http://192.168.1.35:11434/v1"
		}
	})
	allow, critical := allowlist(g.plan, g.cfg)
	wantAllow := []string{"192.168.1.35", "a.invalid", "api.example.com", "docs.example.com", "proxy.golang.org", "sum.golang.org"}
	if !reflect.DeepEqual(allow, wantAllow) {
		t.Fatalf("allow = %v, want %v", allow, wantAllow)
	}
	if !reflect.DeepEqual(critical, []string{"api.example.com"}) {
		t.Fatalf("critical = %v", critical)
	}
	rc := g.newRC(t) // the rules were built from the same inputs and accepted
	if rc.prox == nil {
		t.Fatal("no proxy")
	}
}

func TestWave0BuildFailureStopsRun(t *testing.T) {
	g := newRig(t)
	tf := g.plan.Leaf("fn-greet").TestFile
	write(t, g.abs(tf), g.read(tf)+"\nvar _ = undefinedByWave0Test\n")
	_, err := g.start(t)
	se := wantStop(t, err, "failed", "wave0_build")
	if !strings.Contains(se.Message, filepath.Base(tf)+":") {
		t.Fatalf("message = %q, want file:line of the break", se.Message)
	}
	if strings.Contains(se.Message, "undefinedByWave0Test") {
		t.Fatalf("message carries compiler text: %q", se.Message)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("%d model calls", n)
	}
	if fileExists(filepath.Join(g.runDir, "_state", "executor.json")) {
		st, _ := LoadState(g.runDir)
		if st.Wave0Done {
			t.Fatal("Wave0Done after a build failure")
		}
	}
}

func TestRedCheckWeakTestWarned(t *testing.T) {
	g := newRig(t)
	tf := g.plan.Leaf("fn-greet").TestFile
	write(t, g.abs(tf), "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {}\n")
	rc, err := g.start(t)
	if err != nil {
		t.Fatalf("startRun: %v", err)
	}
	weak := g.sink.OfKind("weak_test")
	if len(weak) != 1 || weak[0].NodeID != "fn-greet" {
		t.Fatalf("weak_test events = %+v, want one for fn-greet", weak)
	}
	if weak[0].Stage == "" && weak[0].Message == "" {
		t.Fatal("the warning says nothing")
	}
	if rc.rep.weak != 1 {
		t.Fatalf("rc.rep.weak = %d, want 1", rc.rep.weak)
	}
	if !rc.state.Wave0Done {
		t.Fatal("the run did not continue past a weak test")
	}
	for _, l := range g.plan.Leaves {
		if !rc.state.RedChecked[l.ID] {
			t.Fatalf("leaf %s not recorded as red checked", l.ID)
		}
	}
}

func TestStubsBuildAtEveryCommit(t *testing.T) {
	g := newRig(t)
	if _, err := g.start(t); err != nil {
		t.Fatal(err)
	}
	commits := []string{strings.TrimSpace(g.gitCmd("rev-parse", "HEAD"))}
	for _, l := range g.plan.Leaves {
		stub, err := stubFor(g.plan.Contracts, g.plan.Policy(), l)
		if err != nil {
			t.Fatal(err)
		}
		sw := NewSwap(g.repo, l, g.git, stub)
		if err := sw.Enter([]byte(good(l.ID))); err != nil {
			t.Fatal(err)
		}
		h, err := sw.Pass("leaf " + l.ID)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, strings.TrimSpace(g.gitCmd("rev-parse", h)))
	}
	for i, c := range commits {
		dir := t.TempDir()
		tarball := filepath.Join(t.TempDir(), "c.tar")
		g.gitCmd("archive", "--format=tar", "-o", tarball, c)
		if out, err := exec.Command("tar", "-xf", tarball, "-C", dir).CombinedOutput(); err != nil {
			t.Fatalf("tar: %v\n%s", err, out)
		}
		for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
			cmd := exec.Command("go", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly -buildvcs=false", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("commit %d (%s): go %s failed:\n%s", i, c[:8], args[0], out)
			}
		}
	}
}

func TestStartRecordsState(t *testing.T) {
	g := newRig(t)
	rc, err := g.start(t)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.PlanHashes, g.plan.Hashes) {
		t.Fatal("the plan hashes in the state are not Plan.Hashes")
	}
	if st.Branch != "gm/"+g.id || !st.Wave0Done || st.StartedAt == "" {
		t.Fatalf("state = %+v", st)
	}
	fi, err := os.Stat(filepath.Join(g.runDir, "_state", "executor.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	if rc.state.StartedAt != st.StartedAt {
		t.Fatal("rc.state is not the saved state")
	}
	for _, l := range g.plan.Leaves {
		row, err := g.board.Get(context.Background(), g.id, l.ID)
		if err != nil || row.Status != blackboard.StatusPending && row.Status != blackboard.StatusReady {
			t.Fatalf("board row of %s = %+v, %v", l.ID, row, err)
		}
	}
}

func TestStartResumeSeam(t *testing.T) {
	g := newRig(t)
	if _, err := g.start(t); err != nil {
		t.Fatal(err)
	}
	_, err := g.start(t)
	if err == nil || !strings.Contains(err.Error(), "resume is added in Task 14") {
		t.Fatalf("second start err = %v, want the resume stub", err)
	}
}

func TestStartDiffOnlyMakesNoBranchOrCommit(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.BriefEdit = func(s string) string { return strings.Replace(s, "landing: commit", "landing: diff_only", 1) }
	})
	head := strings.TrimSpace(g.gitCmd("rev-parse", "HEAD"))
	rc, err := g.start(t)
	if err != nil {
		t.Fatal(err)
	}
	if !rc.diffOnly {
		t.Fatal("diffOnly is not set")
	}
	if g.branches("gm/*") != "" || strings.TrimSpace(g.gitCmd("rev-parse", "HEAD")) != head {
		t.Fatal("diff_only made a branch or a commit")
	}
}

func TestSandboxSettingAndAvailabilityRecorded(t *testing.T) {
	for _, mode := range []string{"off", "on"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "on" && !sandboxUsable() {
				t.Skip("sandbox-exec is not usable here")
			}
			g := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.Sandbox = mode } })
			rc := g.newRC(t)
			if rc.sandboxOn != (mode == "on") {
				t.Fatalf("sandboxOn = %v for setting %s", rc.sandboxOn, mode)
			}
			ev := g.sink.OfKind("sandbox")
			if len(ev) != 1 || !strings.HasPrefix(ev[0].Message, mode) {
				t.Fatalf("sandbox events = %+v, want one stating %s", ev, mode)
			}
			if !strings.Contains(ev[0].Message, "sandbox-exec") {
				t.Fatalf("the event does not state sandbox-exec availability: %q", ev[0].Message)
			}
			if rc.rep.sandboxExec == "" {
				t.Fatal("availability is not recorded for the report")
			}
			if got := rc.sandboxLabel(); got != mode {
				t.Fatalf("sandboxLabel = %q, want %q", got, mode)
			}
		})
	}
}

func TestSandboxOffRefusedOffDarwinUnlessExplicit(t *testing.T) {
	old := hostOS
	hostOS = "linux"
	t.Cleanup(func() { hostOS = old })
	g := newRig(t) // sandbox "off" is explicit in the rig
	if rc := g.newRC(t); rc.sandboxOn {
		t.Fatal("explicit off was not honoured")
	}
	g2 := newRig(t, func(o *rigOpts) { o.Settings = func(c *settings.Config) { c.Executor.Sandbox = "on" } })
	g2.wire(Script{})
	_, err := newRunCtx(context.Background(), g2.options())
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("off-darwin with sandbox on: err = %v, want a refusal naming the sandbox", err)
	}
	if n := len(g2.fake.Requests()); n != 0 {
		t.Fatal("model calls before the refusal")
	}
}

func TestMissingGoIsPreflightFailure(t *testing.T) {
	g := newRig(t, func(o *rigOpts) {
		o.Settings = func(c *settings.Config) { c.Toolchain = map[string]string{"PATH": t.TempDir()} }
	})
	g.wire(Script{})
	_, err := newRunCtx(context.Background(), g.options())
	if err == nil || !strings.Contains(err.Error(), "preflight: go not found on PATH") {
		t.Fatalf("err = %v, want the missing-go preflight error", err)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatal("model calls before the refusal")
	}
}

func TestScanHitInGoModFailsRunBeforeCommit(t *testing.T) {
	g := newRig(t)
	write(t, g.abs("go.mod"), g.read("go.mod")+"\nreplace example.org/x => ../x\n")
	g.gitCmd("add", "go.mod")
	g.gitCmd("commit", "-q", "-m", "a go.mod with a replace directive")
	head := strings.TrimSpace(g.gitCmd("rev-parse", "HEAD"))
	_, err := g.start(t)
	se := wantStop(t, err, "failed", "forbidden_go_mod")
	if !strings.Contains(se.Message, "go.mod:") {
		t.Fatalf("message = %q, want go.mod and a line", se.Message)
	}
	if strings.Contains(se.Message, "example.org/x") {
		t.Fatalf("message carries source text: %q", se.Message)
	}
	if strings.TrimSpace(g.gitCmd("rev-parse", "HEAD")) != head && g.branches("gm/*") == "" {
		t.Fatal("HEAD moved without a branch")
	}
	if n := len(g.sink.OfKind("wave0_commit")); n != 0 {
		t.Fatal("Wave 0 committed after a forbidden go.mod")
	}
}

func TestScanHitInGoFlagsFailsRun(t *testing.T) {
	t.Setenv("GOFLAGS", "-toolexec=/bin/true")
	g := newRig(t)
	_, err := g.start(t)
	wantStop(t, err, "failed", "forbidden_go_mod")
}

func TestScanHitInSourceIsPlanDefect(t *testing.T) {
	g := newRig(t)
	tf := g.plan.Leaf("fn-greet").TestFile
	write(t, g.abs(tf), g.read(tf)+"\n//go:generate echo CANARY-generate\n")
	_, err := g.start(t)
	se := wantStop(t, err, "failed", "forbidden_source")
	if !strings.Contains(se.Message, "generate") || strings.Contains(se.Message, "CANARY") {
		t.Fatalf("message = %q", se.Message)
	}
}

func TestDepsStepOrder(t *testing.T) {
	g := newRig(t)
	rc := g.newRC(t)
	twoDepProxy(t)
	rc.plan.Deps = twoDeps
	ctx := context.Background()
	if err := rc.begin(ctx); err != nil {
		t.Fatalf("begin: %v", err)
	}
	var order []string
	for _, e := range g.sink.Events() {
		switch e.Kind {
		case "deps_start", "deps_module", "deps_done", "scan", "wave0_commit":
			order = append(order, e.Kind)
		}
	}
	want := []string{"deps_start", "deps_module", "deps_module", "deps_done", "scan", "wave0_commit"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("event order = %v, want %v", order, want)
	}
	got := g.commitFiles("HEAD")
	for _, need := range append(g.testFiles(), "go.mod", "go.sum", g.plan.Contracts.Types[0].File, g.plan.Leaves[0].StubFile, g.plan.Leaves[len(g.plan.Leaves)-1].StubFile) {
		if !contains(got, need) {
			t.Errorf("Wave 0 commit lacks %s (has %v)", need, got)
		}
	}
	for _, l := range g.plan.Leaves {
		if !contains(got, l.StubFile) {
			t.Errorf("Wave 0 commit lacks the stub of %s", l.ID)
		}
	}
}

func TestEmptyDependenciesNeverTouchTheNetwork(t *testing.T) {
	old := testHooks
	testHooks = depsHooks{GoProxy: "http://127.0.0.1:1", GoSumDB: "off"} // would fail loudly if used
	t.Cleanup(func() { testHooks = old })
	g := newRig(t)
	if _, err := g.start(t); err != nil {
		t.Fatalf("startRun with no dependencies: %v", err)
	}
	for _, k := range []string{"deps_start", "deps_module", "deps_done"} {
		if n := len(g.sink.OfKind(k)); n != 0 {
			t.Fatalf("%d %s events for an empty dependency list", n, k)
		}
	}
	if fileExists(g.abs("go.sum")) {
		t.Fatal("go.sum was made for a run with no dependencies")
	}
	if fi, err := os.Stat(filepath.Join(g.runDir, "proxy.log")); err == nil && fi.Size() != 0 {
		t.Fatal("the proxy saw a request")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestNoPromptOrOutputInStartEvents(t *testing.T) {
	g := newRig(t)
	tf := g.plan.Leaf("fn-greet").TestFile
	write(t, g.abs(tf), g.read(tf)+"\nvar _ = undefinedCANARYoutput\n")
	_, _ = g.start(t)
	for _, e := range g.sink.Events() {
		if strings.Contains(e.Message, "CANARY") || containsCanary(e.Message) {
			t.Fatalf("event %s carries output text", e.Kind)
		}
	}
}

func TestMakeModuleProxyIsOffline(t *testing.T) {
	url := makeModuleProxy(t, "example.org/m", "v1.0.0", map[string]string{"m.go": "package m\n"})
	if !strings.HasPrefix(url, "file://") {
		t.Fatalf("url = %q", url)
	}
	for _, f := range []string{"list", "v1.0.0.info", "v1.0.0.mod", "v1.0.0.zip"} {
		if !fileExists(filepath.Join(strings.TrimPrefix(url, "file://"), "example.org", "m", "@v", f)) {
			t.Fatalf("proxy lacks %s", f)
		}
	}
}

func sandboxUsable() bool { return sandbox.Preflight(context.Background()) == nil }
