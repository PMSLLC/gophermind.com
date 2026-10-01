package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// orderLog is the order in which a run called the checker, the git layer and
// the event sink.
type orderLog struct {
	mu    sync.Mutex
	items []string
}

func (o *orderLog) add(s string) {
	o.mu.Lock()
	o.items = append(o.items, s)
	o.mu.Unlock()
}

func (o *orderLog) list() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.items...)
}

// count is how many entries equal s; last is the index of the last one, or -1.
func (o *orderLog) count(s string) int {
	n := 0
	for _, it := range o.list() {
		if it == s {
			n++
		}
	}
	return n
}

func (o *orderLog) index(s string, last bool) int {
	at := -1
	for i, it := range o.list() {
		if it == s {
			at = i
			if !last {
				return i
			}
		}
	}
	return at
}

// hybridChecker scripts the leaf checks and the wave checks (the fake) and runs
// every command for real (go list, go build, go mod verify, the acceptance
// commands). It logs the kind of each command.
type hybridChecker struct {
	*fakeChecker
	real    Checker
	log     *orderLog
	failMod bool // go mod verify exits 1
}

func (h *hybridChecker) Run(ctx context.Context, s runner.Spec) runner.Result {
	kind := "accept"
	if len(s.Argv) > 1 && filepath.Base(s.Argv[0]) == "go" {
		switch s.Argv[1] {
		case "mod":
			kind = "modverify"
		case "list":
			kind = "golist"
		case "build":
			kind = "gobuild"
		default:
			kind = "go:" + s.Argv[1]
		}
	}
	h.log.add(kind)
	if kind == "modverify" && h.failMod {
		return runner.Result{ExitCode: 1}
	}
	return h.real.Run(ctx, s)
}

func (h *hybridChecker) BuildVet(ctx context.Context, repo string, env []string) runner.Verdict {
	h.log.add("buildvet")
	return h.fakeChecker.BuildVet(ctx, repo, env)
}

func (h *hybridChecker) Test(ctx context.Context, t runner.TestSet) runner.Verdict {
	h.log.add("test")
	return h.fakeChecker.Test(ctx, t)
}

// tee logs the kind of every event, then passes it on.
type teeSink struct {
	next events.Sink
	log  *orderLog
}

func (s teeSink) Emit(e events.Event) {
	s.log.add("event:" + e.Kind)
	s.next.Emit(e)
}

// accRun is one executor run with real acceptance commands over a scripted
// leaf loop. edit runs on the started run, before any wave.
func (g *rig) accRun(t *testing.T, script Script, fc *fakeChecker, edit func(rc *runCtx, h *hybridChecker), mod func(*Options, *orderLog)) (Report, error, *orderLog) {
	t.Helper()
	g.wire(script)
	o := g.options()
	log := &orderLog{}
	o.Sink = teeSink{next: g.sink, log: log}
	if mod != nil {
		mod(&o, log)
	}
	hook := func(rc *runCtx) {
		h := &hybridChecker{fakeChecker: fc, real: rc.chk, log: log}
		rc.chk = h
		if edit != nil {
			edit(rc, h)
		}
	}
	rep, err := run(context.Background(), o, runFlags{afterStart: hook})
	return rep, err, log
}

// bullet is the plan's bullet (acceptance or constraint) with the id.
func bullet(t *testing.T, rc *runCtx, id string) *planBullet {
	t.Helper()
	for _, list := range [][]planBullet{rc.accept.acceptance, rc.accept.constraints} {
		for i := range list {
			if list[i].req.ID == id {
				return &list[i]
			}
		}
	}
	t.Fatalf("no bullet %s in the plan", id)
	return nil
}

// setCommand replaces the command of the bullet's only root test.
func setCommand(t *testing.T, rc *runCtx, id, cmd string) {
	t.Helper()
	b := bullet(t, rc, id)
	b.tests = append([]planner.RootTest(nil), b.tests...)
	b.tests[0].Command = cmd
}

// oneBullet is an acceptPlan of acceptance bullets with the given commands,
// named A1, A2, ...
// vr makes a command a real probe of the server as far as the vacuity check
// can tell (it names the base URL and does something), then runs cmd.
func vr(cmd string) string {
	return `curl -s -m 1 "$GM_ACCEPTANCE_URL" >/dev/null 2>&1; ` + cmd
}

func oneBullets(cmds ...string) acceptPlan {
	var p acceptPlan
	for i, c := range cmds {
		id := "A" + strconv.Itoa(i+1)
		p.acceptance = append(p.acceptance, planBullet{
			req:   planner.Requirement{ID: id, Kind: planner.ReqAcceptance, Text: "bullet " + id},
			tests: []planner.RootTest{{Requirement: id, Name: "t" + id, Command: c}},
		})
	}
	return p
}

// markerScript is goodScript with extra replies for a leaf: each repair round
// gets a distinct variant, and the last one carries MARKER when withMarker.
func withRepairs(s Script, id string, n int, marker string) {
	steps := append([]step(nil), s["implement:"+id]...)
	for i := 1; i <= n; i++ {
		src := variant(good(id), i)
		if marker != "" {
			src += "// " + marker + "\n"
		}
		steps = append(steps, reply(src))
	}
	s["implement:"+id] = steps
}

// reapprove makes the approval match the plan as the test just edited it.
func reapprove(t *testing.T, runDir string) {
	t.Helper()
	_, hash, err := planner.RenderPlan(runDir)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(runDir, "approval.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["plan_hash"] = hash
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// editCoverage rewrites coverage.json with f, and approves the result again.
func editCoverage(t *testing.T, runDir string, f func(*planner.CoverageFile)) {
	t.Helper()
	cov, err := planner.ReadCoverage(runDir)
	if err != nil {
		t.Fatal(err)
	}
	f(&cov)
	raw, _ := json.MarshalIndent(cov, "", "  ")
	if err := os.WriteFile(filepath.Join(runDir, "coverage.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reapprove(t, runDir)
}

func without(tests []planner.RootTest, req string) []planner.RootTest {
	var out []planner.RootTest
	for _, rt := range tests {
		if rt.Requirement != req {
			out = append(out, rt)
		}
	}
	return out
}

func TestAcceptanceCountFromRequirements(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	reqs, cov := g.plan.Requirements, g.plan.Coverage

	cases := []struct {
		name      string
		cov       func() planner.CoverageFile
		wantErr   string // requirement id the error must name
		total     int
		cons      int
		wantNodes map[string]int
	}{
		{name: "all mapped", cov: func() planner.CoverageFile { return cov }, total: 2, cons: 1},
		{name: "a root test removed", cov: func() planner.CoverageFile {
			c := cov
			c.RootTests = without(cov.RootTests, "A2")
			return c
		}, wantErr: "A2"},
		{name: "a blank command", cov: func() planner.CoverageFile {
			c := cov
			c.RootTests = append([]planner.RootTest(nil), cov.RootTests...)
			for i := range c.RootTests {
				if c.RootTests[i].Requirement == "A1" {
					c.RootTests[i].Command = "  \n"
				}
			}
			return c
		}, wantErr: "A1"},
		{name: "coverage claims one covered, requirements hold two bullets", cov: func() planner.CoverageFile {
			c := cov
			c.Covered = cov.Covered[:1]
			return c
		}, total: 2, cons: 1},
		{name: "a constraint with no root test is not counted", cov: func() planner.CoverageFile {
			c := cov
			c.RootTests = without(cov.RootTests, "C1")
			return c
		}, total: 2, cons: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := mapAcceptance(reqs, tc.cov())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("mapAcceptance error = %v, want one naming %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(p.acceptance) != tc.total || len(p.constraints) != tc.cons {
				t.Errorf("acceptance %d constraints %d, want %d and %d", len(p.acceptance), len(p.constraints), tc.total, tc.cons)
			}
		})
	}

	for _, blank := range []bool{false, true} {
		name := "a dropped root test stops the run before anything starts"
		if blank {
			name = "a blank command stops the run before anything starts"
		}
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			editCoverage(t, g.runDir, func(c *planner.CoverageFile) {
				if blank {
					for i := range c.RootTests {
						if c.RootTests[i].Requirement == "A2" {
							c.RootTests[i].Command = ""
						}
					}
					return
				}
				c.RootTests = without(c.RootTests, "A2")
			})
			fc := g.fastChecker()
			g.wire(goodScript(g))
			rep, err := run(context.Background(), g.options(), runFlags{afterStart: useChecker(fc)})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != "failed" || rep.StopReason != "acceptance_unmapped" {
				t.Fatalf("report = %s (%s), want failed/acceptance_unmapped", rep.Status, rep.StopReason)
			}
			if n := len(g.fake.Requests()); n != 0 {
				t.Errorf("%d model calls before the tripwire", n)
			}
			fc.mu.Lock()
			spawned := fc.buildVets + len(fc.testCalls)
			fc.mu.Unlock()
			if spawned != 0 {
				t.Errorf("%d checks ran before the tripwire", spawned)
			}
			if fileExists(filepath.Join(g.runDir, "_state", "executor.json")) {
				t.Error("the run started (executor.json exists) despite the tripwire")
			}
		})
	}
}

// fakeInstalled is a directory holding a `greeter` that would fail the bullet.
func fakeInstalled(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "greeter"), "#!/bin/sh\necho installed-binary\nexit 1\n")
	if err := os.Chmod(filepath.Join(dir, "greeter"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAcceptanceUsesBuiltBinary(t *testing.T) {
	t.Parallel()
	fake := fakeInstalled(t)
	g := newRig(t, func(o *rigOpts) {
		o.Settings = func(c *settings.Config) { c.Toolchain["PATH"] = fake + ":" + c.Toolchain["PATH"] }
	})
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "verified" || rep.Acceptance.Passed != 2 || rep.Acceptance.Total != 2 {
		t.Fatalf("report = %s (%s), acceptance %+v, failures %v", rep.Status, rep.StopReason, rep.Acceptance, rep.Failures)
	}
	bin := filepath.Join(g.repo, ".gophermind", g.id+"-scratch", "bin", "greeter")
	if !fileExists(bin) {
		t.Fatalf("the built binary %s does not exist", bin)
	}
	if out := strings.TrimSpace(g.gitCmd("status", "--porcelain", "--untracked-files=all")); out != "" {
		t.Errorf("the run left the working tree dirty: %s", out)
	}
	if fileExists(filepath.Join(g.repo, "greeter")) || fileExists(filepath.Join(g.repo, "cmd", "greeter", "greeter")) {
		t.Error("a binary was left in the repository tree")
	}
}

func gone(pid int) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the command left no pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestAcceptanceKillsProcessGroup(t *testing.T) {
	t.Parallel()
	t.Run("a backgrounded child is killed when the command ends", func(t *testing.T) {
		g := newRig(t)
		rc, _ := g.leafRC(t, goodScript(g), false)
		_, failed, err := rc.acceptanceRun(context.Background(), oneBullets(`sleep 300 & echo $! > "$TMPDIR/pid"; exit 0`), 0)
		if err != nil || len(failed) != 0 {
			t.Fatalf("acceptanceRun = %v, %v", failed, err)
		}
		if !gone(readPID(t, filepath.Join(rc.scratch, "pid"))) {
			t.Fatal("the child outlived the command")
		}
	})
	t.Run("a command that outlives its timeout is killed with its child", func(t *testing.T) {
		g := newRig(t, func(o *rigOpts) {
			o.Settings = func(c *settings.Config) { c.Executor.AcceptanceTimeoutSeconds = 1 }
		})
		rc, _ := g.leafRC(t, goodScript(g), false)
		start := time.Now()
		file, failed, err := rc.acceptanceRun(context.Background(), oneBullets(`sleep 300 & echo $! > "$TMPDIR/pid"; wait`), 0)
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("took %v", d)
		}
		if _, bad := failed["A1"]; !bad || file.Bullets[0].Commands[0].TimedOut != true || file.Acceptance.Passed != 0 {
			t.Fatalf("a timed out command did not fail its bullet: %+v", file.Bullets)
		}
		if !gone(readPID(t, filepath.Join(rc.scratch, "pid"))) {
			t.Fatal("the child outlived the timeout")
		}
	})
	t.Run("the fixture's server is gone when A2 returns", func(t *testing.T) {
		g := newRig(t)
		edit := func(rc *runCtx, h *hybridChecker) {
			b := bullet(t, rc, "A2")
			cmd := b.tests[0].Command
			cmd = strings.Replace(cmd, "greeter --addr", `echo "$GM_ACCEPTANCE_ADDR" > "$TMPDIR/port"; greeter --addr`, 1)
			setCommand(t, rc, "A2", cmd)
		}
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), edit, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		raw, err := os.ReadFile(filepath.Join(g.repo, ".gophermind", g.id+"-scratch", "port"))
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.DialTimeout("tcp", strings.TrimSpace(string(raw)), time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("the server A2 started still accepts connections")
		}
	})
}

func TestAcceptanceProofFile(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, goodScript(g), false)
	plan := oneBullets(`printf proof-A1`, `printf proof-A2; exit 3`, `printf proof-A3`)
	file, failed, err := rc.acceptanceRun(context.Background(), plan, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed["A2"].ExitCode != 3 {
		t.Fatalf("failed = %+v", failed)
	}
	p := filepath.Join(g.runDir, "acceptance.json")
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("acceptance.json: %v, %v", fi, err)
	}
	raw, _ := os.ReadFile(p)
	var got AcceptanceFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Acceptance != (Counts{Passed: 2, Total: 3}) || file.Acceptance != got.Acceptance {
		t.Errorf("acceptance = %+v, want 2 of 3", got.Acceptance)
	}
	sum := sha256.Sum256([]byte("proof-A1"))
	c := got.Bullets[0].Commands[0]
	if c.ExitCode != 0 || c.OutputBytes != 8 || c.OutputSHA != hex.EncodeToString(sum[:]) || !c.Passed || len(c.OutputSHA) != 64 {
		t.Errorf("A1 proof = %+v", c)
	}
	if got.Bullets[1].Commands[0].ExitCode != 3 || got.Bullets[1].Verdict != "fail" {
		t.Errorf("A2 proof = %+v", got.Bullets[1])
	}
	want := Counts{Passed: len(g.plan.Coverage.Covered), Total: len(g.plan.Requirements)}
	if got.RequirementsCovered != want {
		t.Errorf("requirements_covered = %+v, want %+v", got.RequirementsCovered, want)
	}
	for _, s := range []string{"proof-A1", "proof-A2", "proof-A3"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("acceptance.json holds the printed text %q", s)
		}
	}
}

// bye and hello are the leaves the two failing bullets of the repair tests are
// mapped to (the fixture's coverage maps an acceptance bullet to no node).
func repairBullets(rc *runCtx, t *testing.T, fixA1 bool) {
	byeFile := rc.plan.Leaf("fn-bye").File
	helloFile := rc.plan.Leaf("fn-hello").File
	setCommand(t, rc, "A2", vr("grep -q MARKER-BYE "+byeFile+" || { echo CANARY-ACC-OUT; exit 1; }"))
	bullet(t, rc, "A2").nodes = []string{"fn-bye"}
	if !fixA1 {
		setCommand(t, rc, "A1", vr("grep -q MARKER-HELLO "+helloFile+" || { echo CANARY-A1-OTHER; exit 1; }"))
		bullet(t, rc, "A1").nodes = []string{"fn-hello"}
	} else {
		setCommand(t, rc, "A1", vr("grep -q main cmd/greeter/main.go"))
	}
}

func TestAcceptanceRepairThenFail(t *testing.T) {
	t.Parallel()
	t.Run("pass after one repair round", func(t *testing.T) {
		g := newRig(t)
		script := goodScript(g)
		withRepairs(script, "fn-bye", 1, "MARKER-BYE")
		fc := g.fastChecker()
		fc.LeafScript["fn-bye"] = []runner.Verdict{passVerdict(), passVerdict()}
		var tb *traceBoard
		edit := func(rc *runCtx, h *hybridChecker) {
			repairBullets(rc, t, true)
			tb = traced(rc)
		}
		rep, err, _ := g.accRun(t, script, fc, edit, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		if rep.Repairs != 1 || rep.Acceptance.Passed != 2 {
			t.Errorf("repairs %d acceptance %+v", rep.Repairs, rep.Acceptance)
		}
		trace := strings.Join(tb.Trace(), "|")
		if !strings.Contains(trace, "fn-bye verified->needs_revision|fn-bye needs_revision->ready") {
			t.Errorf("trace lacks the reopen of fn-bye: %s", trace)
		}
		if !strings.Contains(g.gitCmd("log", "--format=%s", "main"), "gm(fn-bye): repair round 1") {
			t.Error("no repair commit for fn-bye on main")
		}
		var file AcceptanceFile
		raw, _ := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
		if err := json.Unmarshal(raw, &file); err != nil || file.Round != 1 {
			t.Errorf("acceptance.json round = %d (%v), want 1", file.Round, err)
		}
	})

	t.Run("fail after the bound", func(t *testing.T) {
		g := newRig(t)
		script := goodScript(g)
		withRepairs(script, "fn-bye", 2, "")
		withRepairs(script, "fn-hello", 2, "")
		fc := g.fastChecker()
		for _, id := range []string{"fn-bye", "fn-hello"} {
			fc.LeafScript[id] = []runner.Verdict{passVerdict(), passVerdict(), passVerdict()}
		}
		rep, err, log := g.accRun(t, script, fc, func(rc *runCtx, h *hybridChecker) { repairBullets(rc, t, false) }, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != "failed" || rep.StopReason != "acceptance_failed" {
			t.Fatalf("report = %s (%s), %v", rep.Status, rep.StopReason, rep.Failures)
		}
		msg := strings.Join(rep.Failures, "\n")
		for _, want := range []string{"A1", "GET /hello?name=Ada returns Hello, Ada!", "exit 1", "A2"} {
			if !strings.Contains(msg, want) {
				t.Errorf("failures %q lack %q", msg, want)
			}
		}
		for _, secret := range []string{"CANARY-ACC-OUT", "CANARY-A1-OTHER"} {
			if strings.Contains(msg, secret) {
				t.Errorf("a failure line holds command output %q", secret)
			}
		}
		if n := len(g.leafCalls("fn-bye")); n != 3 {
			t.Errorf("fn-bye had %d implement calls, want 3 (one build and two acceptance repair rounds)", n)
		}
		if log.count("event:acceptance_passed") != 0 {
			t.Error("Acceptance passed was announced for a failing run")
		}
		for _, ev := range g.sink.Events() {
			if strings.Contains(ev.Message, "Acceptance passed") {
				t.Errorf("event %q announces a pass", ev.Message)
			}
		}
		if hasCanary(t, g, "CANARY-ACC-OUT") || hasCanary(t, g, "CANARY-A1-OTHER") {
			t.Error("command output reached a store")
		}
		// need to know: a leaf sees its own bullets' output only
		byeReq := userText(g.leafCalls("fn-bye")[1])
		helloReq := userText(g.leafCalls("fn-hello")[1])
		if !strings.Contains(byeReq, "CANARY-ACC-OUT") || strings.Contains(byeReq, "CANARY-A1-OTHER") {
			t.Error("the repair prompt of fn-bye does not carry exactly its own bullet's output")
		}
		if !strings.Contains(helloReq, "CANARY-A1-OTHER") || strings.Contains(helloReq, "CANARY-ACC-OUT") {
			t.Error("the repair prompt of fn-hello does not carry exactly its own bullet's output")
		}
	})

	t.Run("a bullet no leaf owns stops at once", func(t *testing.T) {
		g := newRig(t)
		edit := func(rc *runCtx, h *hybridChecker) {
			setCommand(t, rc, "A2", vr("echo CANARY-ACC-OUT; exit 1"))
			bullet(t, rc, "A2").nodes = []string{"root"} // a node that is not a leaf
		}
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), edit, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != "failed" || rep.StopReason != "acceptance_unmapped" || !strings.Contains(strings.Join(rep.Failures, " "), "A2") {
			t.Fatalf("report = %s (%s), %v", rep.Status, rep.StopReason, rep.Failures)
		}
		if n := len(g.leafCalls("fn-bye")) + len(g.leafCalls("fn-serve")); n != 2 {
			t.Errorf("%d implement calls, want 2 (no repair call)", n)
		}
	})
}

func TestConstraintsChecked(t *testing.T) {
	t.Parallel()
	addC2 := func(t *testing.T, rc *runCtx, cmd string) {
		rc.accept.constraints = append(rc.accept.constraints, planBullet{
			req:   planner.Requirement{ID: "C2", Kind: planner.ReqConstraint, Text: "Standard library only"},
			tests: []planner.RootTest{{Requirement: "C2", Name: "stdlib", Command: cmd}},
		})
	}
	t.Run("both pass", func(t *testing.T) {
		g := newRig(t)
		rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), func(rc *runCtx, h *hybridChecker) { addC2(t, rc, "true") }, nil)
		if err != nil || rep.Status != "verified" {
			t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		if rep.Constraints.Passed != 2 || rep.Constraints.Total != 2 {
			t.Errorf("constraints = %+v, want 2 of 2", rep.Constraints)
		}
		if !strings.Contains(rep.Summary(), "Constraints checked: 2 of 2") {
			t.Error("the summary lacks the constraints line")
		}
	})
	t.Run("a failing constraint ends the run failed", func(t *testing.T) {
		g := newRig(t)
		script := goodScript(g)
		withRepairs(script, "fn-greet", 2, "")
		fc := g.fastChecker()
		fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), passVerdict(), passVerdict()}
		edit := func(rc *runCtx, h *hybridChecker) {
			addC2(t, rc, "exit 1")
			bullet(t, rc, "C2").nodes = []string{"fn-greet"}
		}
		rep, err, _ := g.accRun(t, script, fc, edit, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != "failed" || rep.StopReason != "constraint_failed" {
			t.Fatalf("report = %s (%s), %v", rep.Status, rep.StopReason, rep.Failures)
		}
		if rep.Constraints.Passed != 1 || rep.Constraints.Total != 2 {
			t.Errorf("constraints = %+v, want 1 of 2", rep.Constraints)
		}
	})
}
