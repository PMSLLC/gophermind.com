package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/report"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

// The end-to-end tests run the whole executor, scripted and offline, over the
// greeter repository (Task 16, spec 18). Every leaf goes through the real
// checks, the real runner and (on darwin) the real sandbox; only the model is
// scripted.

// e2eMaxWall is the budget of the uninterrupted run.
const e2eMaxWall = 2 * time.Minute

// e2eWaveOrder is the order the leaves run: wave, then id.
var e2eWaveOrder = []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello", "fn-serve"}

// e2eOpts is the rig of an end-to-end run: the sandbox on (a missing
// sandbox-exec on darwin is a failure, not a skip) and a revision budget of 1,
// so the fn-serve leaf reaches the revise rung and then the human gate.
func e2eOpts(t *testing.T) func(*rigOpts) {
	t.Helper()
	sb := "on"
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Fatalf("sandbox-exec is missing on darwin: the release bar needs the real sandbox: %v", err)
		}
	} else {
		sb = "off"
		t.Logf("not darwin: executor.sandbox is off for this test")
	}
	return func(o *rigOpts) {
		o.Settings = func(c *settings.Config) {
			c.Executor.Sandbox = sb
			c.Defaults.MaxRevisions = 1
		}
	}
}

// refusedServe is a reply the import policy refuses (os/exec): a failed
// attempt that runs no test. n makes every one different, so the identical
// reply shortcut never applies.
func refusedServe(n int) string {
	return fmt.Sprintf("package main\n\nimport _ \"os/exec\"\n\n// refused variant %d\nfunc serve(addr string) error {\n\treturn nil\n}\n", n)
}

// e2eScript is the model of the end-to-end run, by stage:
//
//	fn-farewell  prose (malformed, the router's repair is request 2), then the good file
//	fn-greet     a wrong file that prints the canaries, then the good file
//	fn-bye       a racy file that passes plain go test, then (the repair) the good file
//	fn-hello     three wrong files on a/m1, the good file on b/m2
//	fn-serve     three wrong files on a/m1, refused files on b/m2, a revise, refused
//	             files on both entries, a human retry, then the good file
func e2eScript(t *testing.T) Script {
	t.Helper()
	serve := []step{reply(bad("fn-serve", 1)), reply(bad("fn-serve", 2)), reply(bad("fn-serve", 3))}
	for n := 1; n <= 9; n++ {
		serve = append(serve, reply(refusedServe(n)))
	}
	serve = append(serve, reply(good("fn-serve")))
	return Script{
		"implement:fn-farewell": {reply(fixtureReply("prose.txt")), reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(fixtureReply("fn-greet.canary.txt")), reply(good("fn-greet"))},
		"implement:fn-bye":      {reply(fixtureReply("fn-bye.race.txt")), reply(good("fn-bye"))},
		"implement:fn-hello":    {reply(bad("fn-hello", 1)), reply(bad("fn-hello", 2)), reply(bad("fn-hello", 3)), reply(good("fn-hello"))},
		"implement:fn-serve":    serve,
		"revise:fn-serve":       {reply(`{"notes":["check what the listen call returns"]}`)},
	}
}

// ---- the timeline: calls and leaf events in one order ----

type e2eTrace struct {
	mu  sync.Mutex
	seq []string
}

func (e *e2eTrace) add(s string) {
	e.mu.Lock()
	e.seq = append(e.seq, s)
	e.mu.Unlock()
}

func (e *e2eTrace) list() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.seq...)
}

func (e *e2eTrace) index(s string) int {
	for i, x := range e.list() {
		if x == s {
			return i
		}
	}
	return -1
}

type e2eSink struct {
	next  events.Sink
	trace *e2eTrace
}

func (s e2eSink) Emit(e events.Event) {
	if e.Kind == "leaf_verified" {
		s.trace.add("verified:" + e.NodeID)
	}
	s.next.Emit(e)
}

// ---- git, with a clean environment and nothing inherited ----

// realGit is the git binary the tests use; the argv recorder execs it.
func realGit(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("GITLAND_TEST_GIT"); b != "" {
		return b
	}
	// Not the guard wrapper on PATH: it runs the first git on PATH, which would be
	// the argv recorder, which runs the wrapper.
	if _, err := os.Stat("/usr/bin/git"); err == nil {
		return "/usr/bin/git"
	}
	b, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	return b
}

// e2eGit runs git in dir with no inherited GIT_* variable and no user or
// system configuration, and returns its stdout.
func e2eGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(realGit(t), args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "LANG=C", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\n%s", args[0], err, errb.String())
	}
	return out.Bytes()
}

// recordGit puts a git on PATH that appends the argv of every call to the
// returned log and then runs the real one. It must be called before newRig:
// gitland resolves git once, when it is built.
func recordGit(t *testing.T) (logPath string) {
	t.Helper()
	real := realGit(t)
	dir := t.TempDir()
	logPath = filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return logPath
}

// e2eGo runs the go tool in dir over a fresh archive: offline, read-only
// modules, the shared build cache, no cgo.
func e2eGo(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + testToolchainPATH(t), "HOME=" + t.TempDir(), "GOCACHE=" + goCacheOverride, "GOFLAGS=-mod=readonly -buildvcs=false",
		"GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOMODCACHE=" + filepath.Join(t.TempDir(), "mod")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go %s in %s failed (%d bytes of output)", args[0], filepath.Base(dir), len(out))
		return err
	}
	return nil
}

// archiveOf extracts the commit's tree into a new temporary directory.
func archiveOf(t *testing.T, repo, sha string) string {
	t.Helper()
	dir := t.TempDir()
	tarball := e2eGit(t, repo, "archive", sha)
	cmd := exec.Command("tar", "-x", "-C", dir)
	cmd.Stdin = bytes.NewReader(tarball)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	return dir
}

// ---- the stores ----

// e2eStores is every byte the run could have written to a store: the SQLite
// file and its WAL, the run folder and its scratch (state files, events,
// report.json, acceptance.json, the proxy log), the ledger and blackboard rows
// as the code reads them, the events, every object of the target repository
// (decompressed), the commit messages and what the CLI prints (the summary and
// the progress lines).
func e2eStores(t *testing.T, g *rig, rep Report, runErr error) map[string][]byte {
	t.Helper()
	st := map[string][]byte{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if b, err := os.ReadFile(g.dbPath + suffix); err == nil {
			st["sqlite"+suffix] = b
		}
	}
	root := filepath.Join(g.repo, ".gophermind")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "bin" {
				return filepath.SkipDir // built binaries hold the source they were built from
			}
			return nil
		}
		if b, rerr := os.ReadFile(p); rerr == nil {
			if strings.Contains(filepath.ToSlash(p), "/attempts/") {
				st["attempt:"+p] = b // kept scrubbed replies and output on purpose: noLeak checks only the secret here
			} else {
				st["file:"+p] = b
			}
		}
		return nil
	})
	var evs bytes.Buffer
	for _, e := range g.sink.Events() {
		fmt.Fprintf(&evs, "%+v\n", e)
	}
	st["events"] = evs.Bytes()
	ctx := context.Background()
	if rows, err := g.led.List(ctx, g.id, ledger.Filter{}); err == nil {
		st["ledger"] = []byte(fmt.Sprintf("%+v", rows))
	}
	if rows, err := g.board.List(ctx, g.id, blackboard.Filter{}); err == nil {
		st["blackboard"] = []byte(fmt.Sprintf("%+v", rows))
	}
	objs := e2eGit(t, g.repo, "cat-file", "--batch-all-objects", "--batch")
	st["git objects"] = objs
	st["git log"] = e2eGit(t, g.repo, "log", "--all", "-p", "--format=%B")
	st["stdout"] = []byte(rep.Summary())
	if runErr != nil {
		st["error"] = []byte(runErr.Error())
	}
	rj, _ := json.Marshal(rep)
	st["report value"] = rj
	return st
}

// legitText is the plan files and the tracked source at the tip of main in one
// string: text that a prompt may quote and a file may rightfully hold.
func legitText(t *testing.T, g *rig) string {
	t.Helper()
	var legit strings.Builder
	addText := func(b []byte) { legit.Write(b); legit.WriteByte('\n') }
	for name := range g.plan.Hashes {
		if b, err := os.ReadFile(planFilePath(g, name)); err == nil {
			addText(b)
		}
	}
	for _, name := range []string{"brief.md", "requirements.json", "coverage.json", "contracts.json"} {
		if b, err := os.ReadFile(filepath.Join(g.runDir, name)); err == nil {
			addText(b)
		}
	}
	files := strings.Fields(string(e2eGit(t, g.repo, "ls-tree", "-r", "--name-only", "main")))
	addText([]byte(strings.Join(files, "\n"))) // file names are ids, not text
	for _, f := range files {
		addText(e2eGit(t, g.repo, "show", "main:"+f))
	}
	return legit.String()
}

// distinctLines is the lines of text (at least minLen bytes after trimming)
// that appear nowhere in legit.
func distinctLines(text string, minLen int, legit string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if len(ln) >= minLen && !strings.Contains(legit, ln) {
			out = append(out, ln)
		}
	}
	return out
}

// noLeak fails for every needle found in any store. The "attempt:" stores
// (attempts/) hold the scrubbed reply and check output on purpose, so only the
// declared secret value is looked for there.
func noLeak(t *testing.T, stores map[string][]byte, needles []string) {
	t.Helper()
	names := make([]string, 0, len(stores))
	for n := range stores {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range needles {
		for _, name := range names {
			if strings.HasPrefix(name, "attempt:") && n != canarySecret {
				continue
			}
			if bytes.Contains(stores[name], []byte(n)) {
				t.Errorf("%q (%d bytes) was found in %s", n, len(n), tailName(name))
			}
		}
	}
}

func tailName(name string) string {
	if i := strings.Index(name, ".gophermind/"); i >= 0 {
		return name[i:]
	}
	return name
}

// ---- the uninterrupted run, and what two runs must agree on ----

// e2eRef is the end state of the uninterrupted run, recorded by TestExecutorE2E
// for the kill test to compare against (the kill test builds its own when this
// test did not run first).
var e2eRef struct {
	mu       sync.Mutex
	set      bool
	tree     string
	subjects []string
}

func recordE2ERef(tree string, subjects []string) {
	e2eRef.mu.Lock()
	e2eRef.set, e2eRef.tree, e2eRef.subjects = true, tree, subjects
	e2eRef.mu.Unlock()
}

// e2eGate is the auto-answering gate of the unattended policy: every
// escalation is retried once with a note, recorded as unattended-default.
func e2eGate(g *rig) {
	g.gate.by = human.AnsweredByUnattended
	g.gate.queue = []human.Resolution{{Action: human.ActionRetry, Note: "look at what the listen call returns"}}
}

func TestExecutorE2E(t *testing.T) {
	// Not parallel: its wall time is asserted, and it must not share the
	// machine with the rest of the package.
	warmBuildCache(t)
	argvLog := recordGit(t)
	g := newRig(t, e2eOpts(t))
	if err := os.WriteFile(argvLog, nil, 0o644); err != nil { // keep only what the executor spawns from here
		t.Fatal(err)
	}
	trace := &e2eTrace{}
	g.wire(e2eScript(t))
	e2eGate(g)
	g.fake.Before = func(stage string) { trace.add("call:" + stage) }
	planBefore := map[string][]byte{}
	for _, name := range append([]string{"contracts.json"}, planFileNames(t, g)...) {
		b, err := os.ReadFile(planFilePath(g, name))
		if err != nil {
			t.Fatal(err)
		}
		planBefore[name] = b
	}

	o := g.options()
	o.Sink = e2eSink{next: g.sink, trace: trace}
	tb := &traceBoard{Blackboard: g.board}
	o.Board = tb
	start := time.Now()
	rep, err := Run(context.Background(), o)
	wall := time.Since(start)
	t.Logf("TestExecutorE2E: the run took %v", wall.Round(time.Second))
	logSlowSteps(t, g.sink.Events())
	gitLog, _ := os.ReadFile(argvLog) // read before any git of the test's own
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 1. The run.
	if rep.Status != "verified" || rep.ExitCode != 0 || rep.Resumed {
		t.Fatalf("report = %s (%s) exit %d resumed %v, failures %v", rep.Status, rep.StopReason, rep.ExitCode, rep.Resumed, rep.Failures)
	}
	if runtime.GOOS == "darwin" && rep.Sandbox != "on" {
		t.Errorf("Sandbox = %q, want on", rep.Sandbox)
	}
	if wall > e2eMaxWall {
		t.Errorf("the run took %v, over the %v budget", wall, e2eMaxWall)
	}

	// 2. Wave order: the implement stages in request order, repairs and router
	// retries collapsed, are wave then id; no wave-2 call precedes the last
	// wave-1 verification.
	var order []string
	for _, r := range g.fake.Requests() {
		s := stageOf(r)
		if id, ok := strings.CutPrefix(s, "implement:"); ok && (len(order) == 0 || order[len(order)-1] != id) {
			order = append(order, id)
		}
	}
	// fn-bye is called again for its repair, after fn-hello: collapse a leaf
	// that already appeared.
	var firsts []string
	seen := map[string]bool{}
	for _, id := range order {
		if !seen[id] {
			seen[id] = true
			firsts = append(firsts, id)
		}
	}
	if !reflect.DeepEqual(firsts, e2eWaveOrder) {
		t.Errorf("implement order = %v, want %v", firsts, e2eWaveOrder)
	}
	if first, last := trace.index("call:implement:fn-serve"), max(trace.index("verified:fn-bye"), trace.index("verified:fn-hello")); first < 0 || last < 0 || first < last {
		t.Errorf("fn-serve was called (at %d) before the last wave-1 leaf was verified (at %d)", first, last)
	}
	for _, w1 := range []string{"fn-bye", "fn-hello"} {
		for _, w0 := range []string{"fn-farewell", "fn-greet"} {
			if c, v := trace.index("call:implement:"+w1), trace.index("verified:"+w0); c < v {
				t.Errorf("%s was called before %s was verified", w1, w0)
			}
		}
	}

	// 3. A malformed reply.
	if v := verdictsOf(t, g.board, "fn-farewell"); v != "pass" {
		t.Errorf("fn-farewell attempts = %q, want a single pass", v)
	}
	if n := len(g.leafCalls("fn-farewell")); n != 2 {
		t.Errorf("fn-farewell provider requests = %d, want 2 (the reply and the router's repair)", n)
	}
	impl := findRow(rep.ByTaskType, "implement")
	if impl.Malformed != 1 {
		t.Errorf("implement malformed = %d, want 1", impl.Malformed)
	}

	// 4. A failing then a passing leaf.
	if v := verdictsOf(t, g.board, "fn-greet"); v != "fail,pass" {
		t.Errorf("fn-greet attempts = %q, want fail,pass", v)
	}
	reasons := reasonsOf(t, g.board, "fn-greet")
	if !strings.HasPrefix(reasons[0], "test_fail: TestGreet") || !regexp.MustCompile(`^test_fail: [A-Za-z0-9_/., -]+$`).MatchString(reasons[0]) {
		t.Errorf("fn-greet first failure reason is not a class and test names only: %q", reasons[0])
	}

	// 5. An escalation to a second chain entry.
	var hello []string
	for _, a := range attemptsOf(t, g.board, "fn-hello") {
		hello = append(hello, a.Provider+"/"+a.Model+":"+string(a.Verdict))
	}
	if want := []string{"a/m1:fail", "a/m1:fail", "a/m1:fail", "b/m2:pass"}; !reflect.DeepEqual(hello, want) {
		t.Errorf("fn-hello attempts = %v, want %v", hello, want)
	}
	var helloModels []string
	for _, r := range g.leafCalls("fn-hello") {
		helloModels = append(helloModels, r.Model)
	}
	if want := []string{"m1", "m1", "m1", "m2"}; !reflect.DeepEqual(helloModels, want) {
		t.Errorf("fn-hello request models = %v, want %v", helloModels, want)
	}
	if n := len(g.fake.Requests()); n == 0 {
		t.Fatal("no request recorded")
	}
	var modelEsc, revEsc, humanEsc int
	for _, r := range rep.ByTaskType {
		modelEsc += r.ModelEscalations
		revEsc += r.RevisionEscalations
		humanEsc += r.HumanEscalations
	}
	if modelEsc < 1 {
		t.Errorf("model escalations = %d, want at least 1", modelEsc)
	}

	// The revise rung and the human escalation, on fn-serve.
	revise := 0
	for _, s := range stagesOf(g.fake) {
		if s == "revise:fn-serve" {
			revise++
		}
	}
	if revise != 1 || revEsc != 1 {
		t.Errorf("revise calls = %d, revision escalations = %d, want 1 and 1", revise, revEsc)
	}
	if humanEsc != 1 || len(rep.HumanLog) != 1 || rep.HumanLog[0].NodeID != "fn-serve" || rep.HumanLog[0].AnsweredBy != human.AnsweredByUnattended {
		t.Errorf("human escalations = %d, log = %+v, want one for fn-serve answered by %s", humanEsc, rep.HumanLog, human.AnsweredByUnattended)
	}
	if es := g.gate.Escalations(); len(es) != 1 || es[0].NodeID != "fn-serve" {
		t.Errorf("the gate was asked %+v, want one escalation of fn-serve", es)
	}
	if v := verdictsOf(t, g.board, "fn-serve"); !strings.HasSuffix(v, ",pass") || strings.Count(v, "pass") != 1 {
		t.Errorf("fn-serve attempts = %q, want failures then one pass", v)
	}

	// 6. The repair of an attributed integration failure.
	if rep.Repairs != 1 {
		t.Errorf("Repairs = %d, want 1", rep.Repairs)
	}
	var byeTrace []string
	for _, s := range tb.Trace() {
		if strings.HasPrefix(s, "fn-bye ") {
			byeTrace = append(byeTrace, strings.TrimPrefix(s, "fn-bye "))
		}
	}
	if !containsSeq(byeTrace, []string{"verified->needs_revision", "needs_revision->ready", "claimed->in_progress", "in_progress->verified"}) {
		t.Errorf("fn-bye transitions = %v, want verified, needs_revision, ready, claimed, in_progress, verified again", byeTrace)
	}
	for _, id := range []string{"fn-farewell", "fn-greet", "fn-hello", "fn-serve"} {
		for _, s := range tb.Trace() {
			if s == id+" verified->needs_revision" {
				t.Errorf("%s was reopened: the failure was attributed to the wrong leaf", id)
			}
		}
	}
	if !strings.Contains(g.gitCmd("log", "--format=%s", "main"), "gm(fn-bye): repair round 1") {
		t.Error("main holds no gm(fn-bye): repair round 1 commit")
	}
	byeCalls := g.leafCalls("fn-bye")
	if len(byeCalls) != 2 {
		t.Fatalf("fn-bye requests = %d, want 2 (the file and its repair)", len(byeCalls))
	}
	if p := userText(byeCalls[1]); !strings.Contains(p, "TestBye") || strings.Contains(p, "helloHandler") {
		t.Errorf("the repair prompt names TestBye = %v and holds a sibling's text = %v; want true and false", strings.Contains(p, "TestBye"), strings.Contains(p, "helloHandler"))
	}

	// 7. Acceptance.
	if rep.Acceptance.Passed != 2 || rep.Acceptance.Total != 2 || rep.Constraints.Passed != 1 || rep.Constraints.Total != 1 {
		t.Errorf("acceptance %+v constraints %+v, want 2 of 2 and 1 of 1", rep.Acceptance, rep.Constraints)
	}
	if rep.Requirements.Covered != 7 || rep.Requirements.Total != 7 {
		t.Errorf("requirements %+v, want 7 of 7", rep.Requirements)
	}
	var acc AcceptanceFile
	raw, err := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
	if err != nil {
		t.Fatalf("acceptance.json: %v", err)
	}
	if err := json.Unmarshal(raw, &acc); err != nil {
		t.Fatal(err)
	}
	if !acc.Complete || len(acc.Bullets) != 2 {
		t.Fatalf("acceptance.json complete %v with %d bullets, want complete with 2", acc.Complete, len(acc.Bullets))
	}
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, b := range acc.Bullets {
		if b.Verdict != "pass" || len(b.Commands) == 0 {
			t.Errorf("bullet %s = %s with %d commands", b.Requirement, b.Verdict, len(b.Commands))
		}
		for _, c := range b.Commands {
			if c.ExitCode != 0 || !hex64.MatchString(c.OutputSHA) {
				t.Errorf("bullet %s command: exit %d, output hash %q", b.Requirement, c.ExitCode, c.OutputSHA)
			}
		}
	}
	bin := filepath.Join(g.repo, ".gophermind", g.id+"-scratch", "bin", "greeter")
	if !fileExists(bin) {
		t.Errorf("the built binary %s does not exist", bin)
	}
	if len(acc.Binaries) == 0 || acc.Binaries[0].Name != "greeter" {
		t.Errorf("acceptance.json binaries = %+v, want the built greeter", acc.Binaries)
	}
	if ps, _ := exec.Command("ps", "-axo", "command=").Output(); bytes.Contains(ps, []byte(bin)) {
		t.Error("a process running the built greeter outlived the run")
	}

	// 8. Git landing.
	st, err := LoadState(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "gm/"+g.id {
		t.Errorf("work branch = %q, want gm/%s", st.Branch, g.id)
	}
	mainSHA := strings.TrimSpace(g.gitCmd("rev-parse", "main"))
	if wb := strings.TrimSpace(g.gitCmd("rev-parse", st.Branch)); mainSHA != wb {
		t.Errorf("main %s is not the work branch tip %s", mainSHA, wb)
	}
	seed := strings.TrimSpace(g.gitCmd("rev-list", "--max-parents=0", "main"))
	e2eGit(t, g.repo, "merge-base", "--is-ancestor", seed, "main") // fails the test unless the seed is an ancestor
	if merges := strings.TrimSpace(g.gitCmd("log", "--merges", "--format=%H", "main")); merges != "" {
		t.Errorf("main holds merge commits: %s", merges)
	}
	subjects := strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%s", seed+"..main")), "\n")
	if len(subjects) != 8 {
		t.Fatalf("main holds %d commits above the seed, want 8: %q", len(subjects), subjects)
	}
	// From the tip: the final commit, fn-serve (wave 2), the repair of fn-bye
	// (made at the wave 1 check, before wave 2), then the leaves in
	// verification order and Wave 0.
	wantSubjects := []string{"gm(run): Greeter built, Acceptance passed 2 of 2", "", "gm(fn-bye): repair round 1", "", "", "", "", ""}
	if subjects[0] != wantSubjects[0] || subjects[2] != wantSubjects[2] {
		t.Errorf("tip subjects = %q, want the final commit then the repair after fn-serve", subjects[:3])
	}
	if !strings.HasPrefix(subjects[7], "gm(wave0): ") {
		t.Errorf("first commit above the seed = %q, want Wave 0", subjects[7])
	}
	var leafOrder []string
	for _, i := range []int{6, 5, 4, 3, 1} {
		id, _, _ := strings.Cut(strings.TrimPrefix(subjects[i], "gm("), "):")
		leafOrder = append(leafOrder, id)
	}
	if want := []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello", "fn-serve"}; !reflect.DeepEqual(leafOrder, want) {
		t.Errorf("leaf commit order = %v, want %v", leafOrder, want)
	}
	for _, kv := range strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%an <%ae>|%cn|%(trailers:key=GopherMind-Run,valueonly,unfold)", seed+"..main")), "\n") {
		if kv == "" {
			continue
		}
		if !strings.HasPrefix(kv, "GopherMind <gophermind@localhost>|GopherMind|") || !strings.Contains(kv, g.id) {
			t.Errorf("a commit has author, committer or run trailer %q, want GopherMind and the run id", kv)
		}
	}
	shaOf := map[string]string{} // leaf id -> its first commit
	for _, rec := range strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%H%x1f%s", seed+"..main")), "\n") {
		sha, subj, _ := strings.Cut(rec, "\x1f")
		if id, _, ok := strings.Cut(strings.TrimPrefix(subj, "gm("), "): "); ok && strings.HasPrefix(subj, "gm(fn-") && !strings.HasPrefix(id, "run") && !strings.Contains(subj, "repair round") {
			shaOf[id] = sha
		}
	}
	for _, l := range g.plan.Leaves {
		sha := shaOf[l.ID]
		if sha == "" {
			t.Errorf("no commit of %s", l.ID)
			continue
		}
		files := strings.Split(strings.TrimSpace(g.gitCmd("show", "--name-status", "--format=", sha)), "\n")
		sort.Strings(files)
		want := []string{"A\t" + l.File, "D\t" + l.StubFile}
		sort.Strings(want)
		if !reflect.DeepEqual(files, want) {
			t.Errorf("commit of %s changes %v, want %v", l.ID, files, want)
		}
		if tr := strings.TrimSpace(g.gitCmd("show", "-s", "--format=%(trailers:key=GopherMind-Node,valueonly,unfold)", sha)); tr != l.ID {
			t.Errorf("commit of %s carries the node trailer %q", l.ID, tr)
		}
	}
	wave0 := strings.TrimSpace(g.gitCmd("log", "--format=%H", "--grep=^gm(wave0): ", "main"))
	w0Files := strings.Fields(g.gitCmd("show", "--name-only", "--format=", wave0))
	for _, l := range g.plan.Leaves {
		for _, f := range []string{l.TestFile, l.StubFile} {
			if !inList(w0Files, f) {
				t.Errorf("Wave 0 does not hold %s", f)
			}
		}
	}
	if !inList(w0Files, "internal/greet/errors.go") {
		t.Error("Wave 0 does not hold internal/greet/errors.go")
	}
	if !strings.Contains(g.gitCmd("ls-tree", "-r", "--name-only", wave0), "go.mod") {
		t.Error("the tree of Wave 0 has no go.mod")
	}
	for _, f := range w0Files {
		if strings.Contains(f, ".gophermind") {
			t.Errorf("Wave 0 holds %s", f)
		}
	}
	for _, sha := range strings.Fields(g.gitCmd("log", "--format=%H", seed+"..main")) {
		dir := archiveOf(t, g.repo, sha)
		if e2eGo(t, dir, "build", "./...") == nil {
			_ = e2eGo(t, dir, "vet", "./...")
		}
	}
	tip := archiveOf(t, g.repo, "main")
	_ = e2eGo(t, tip, "test", "-race", "-count=1", "./...")
	for _, line := range strings.Split(strings.TrimSpace(string(gitLog)), "\n") {
		f := strings.Fields(line)
		for i, a := range f {
			if i > 0 && f[i-1] == "-c" {
				continue
			}
			switch a {
			case "push", "reset", "rebase", "stash", "checkout", "clean", "--force", "-f", "--hard":
				t.Errorf("the executor spawned git %s", a)
			}
		}
	}
	if len(bytes.TrimSpace(gitLog)) == 0 {
		t.Error("the git argv recorder saw nothing: it is not in the path of the executor's git")
	}

	// 9. Report and ledger by hand.
	calls, err := g.led.List(context.Background(), g.id, ledger.Filter{TaskType: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	if impl.Calls != len(calls) {
		t.Errorf("report implement calls = %d, ledger rows = %d", impl.Calls, len(calls))
	}
	perModel := map[string]int{}
	for _, c := range calls {
		perModel[c.Provider+"/"+c.ModelServed]++
	}
	reportModel := map[string]int{}
	var ok, malformed int
	for _, r := range rep.ByTaskType {
		if r.TaskType == "implement" {
			reportModel[r.Model] += r.Calls
			ok += r.OK
			malformed += r.Malformed
		}
	}
	if !reflect.DeepEqual(perModel, reportModel) {
		t.Errorf("per-model implement calls: ledger %v, report %v", perModel, reportModel)
	}
	if want := 2 + 2 + 2 + 4 + 13; len(calls) != want {
		t.Errorf("implement calls = %d, want %d", len(calls), want)
	}
	if ok+malformed > impl.Calls {
		t.Errorf("ok %d + malformed %d exceeds calls %d", ok, malformed, impl.Calls)
	}
	if rd, err := report.Read(g.runDir); err != nil || rd.Acceptance != rep.Acceptance || len(rd.ByTaskType) != len(rep.ByTaskType) {
		t.Errorf("report.json differs from the returned report: %v", err)
	}
	lines := strings.Split(strings.TrimRight(rep.Summary(), "\n"), "\n")
	last := lines[len(lines)-2:]
	if last[0] != "Requirements covered: 7 of 7" || last[1] != "Acceptance passed: 2 of 2" {
		t.Errorf("summary ends with %q", last)
	}

	// 10. Privacy canaries across every store.
	stores := e2eStores(t, g, rep, err)
	needles := []string{canarySecret, "CANARY-REPLY-TEXT", "CANARY-TEST-OUTPUT", "DATA RACE"}
	legit := legitText(t, g)
	var promptLines, replyLines []string
	promptSeen := false
	for _, r := range g.fake.Requests() {
		p := userText(r)
		promptSeen = promptSeen || strings.Contains(p, "CANARY-TEST-OUTPUT")
		promptLines = append(promptLines, distinctLines(p, 30, legit)...)
	}
	if !promptSeen {
		t.Error("CANARY-TEST-OUTPUT never reached a prompt: the output canary proves nothing")
	}
	// The failed replies: every line not in a file that was committed.
	for _, name := range []string{"prose.txt", "fn-greet.canary.txt", "bad.fn-hello.1.txt", "bad.fn-hello.2.txt", "bad.fn-hello.3.txt", "bad.fn-serve.1.txt", "bad.fn-serve.2.txt", "bad.fn-serve.3.txt"} {
		replyLines = append(replyLines, distinctLines(fixtureReply(name), 20, legit)...)
	}
	for n := 1; n <= 9; n++ {
		replyLines = append(replyLines, distinctLines(refusedServe(n), 20, legit)...)
	}
	if len(promptLines) == 0 || len(replyLines) == 0 {
		t.Fatalf("no prompt-only lines (%d) or failed-reply lines (%d) to look for", len(promptLines), len(replyLines))
	}
	noLeak(t, stores, needles)
	noLeak(t, stores, uniq(promptLines))
	noLeak(t, stores, uniq(replyLines))
	// The secret is proven present where it must be: A2 fails without GREETER_TOKEN.
	if v, ok := g.secrets.Get(vault.RunScope(g.id), greeterSecret); !ok || v != canarySecret {
		t.Error("the run's secret is not in the vault: the canary proves nothing")
	}

	// 11. Immutability.
	fresh, err := LoadPlan(g.runDir, g.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Hashes, st.PlanHashes) {
		t.Errorf("the plan hashes changed since the run started:\n now %v\n was %v", fresh.Hashes, st.PlanHashes)
	}
	for name, before := range planBefore {
		if after, err := os.ReadFile(planFilePath(g, name)); err != nil || !bytes.Equal(after, before) {
			t.Errorf("%s changed during the run", name)
		}
	}

	tree, subs := treeAndSubjects(g)
	recordE2ERef(tree, subs)
}

// planFilePath is the file a Plan.Hashes key names: node files are keyed
// "tree/<path>" and live at <path> in the run folder.
func planFilePath(g *rig, name string) string {
	return filepath.Join(g.runDir, filepath.FromSlash(strings.TrimPrefix(name, "tree/")))
}

// planFileNames is the node files of the plan, relative to the run folder.
func planFileNames(t *testing.T, g *rig) []string {
	t.Helper()
	var out []string
	for name := range g.plan.Hashes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func findRow(rows []report.TaskModel, task string) report.TaskModel {
	var sum report.TaskModel
	sum.TaskType = task
	for _, r := range rows {
		if r.TaskType == task {
			sum.Calls += r.Calls
			sum.OK += r.OK
			sum.Malformed += r.Malformed
		}
	}
	return sum
}

func containsSeq(have, want []string) bool {
	i := 0
	for _, h := range have {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}

func uniq(in []string) []string {
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

// ---- a leaf that can never pass ----

func TestExecutorE2EEscalationEndsRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, e2eOpts(t))
	script := e2eScript(t)
	// fn-hello is always wrong: the three wrong replies in turn, so consecutive
	// replies on one entry always differ and the identical-reply shortcut never
	// applies; more replies than the ladder can use. fn-bye is not racy here.
	var hello []step
	for i := 0; i < 20; i++ {
		hello = append(hello, reply(bad("fn-hello", i%3+1)))
	}
	script["implement:fn-hello"] = hello
	script["revise:fn-hello"] = []step{reply(`{"notes":["check the default name"]}`), reply(`{"notes":["check the default name again"]}`)}
	script["implement:fn-bye"] = []step{reply(good("fn-bye"))}
	delete(script, "implement:fn-serve")
	delete(script, "revise:fn-serve")
	g.wire(script)
	seed := strings.TrimSpace(g.gitCmd("rev-parse", "main"))

	o := g.options()
	o.Gate = nil
	start := time.Now()
	rep, err := Run(context.Background(), o)
	t.Logf("TestExecutorE2EEscalationEndsRun: the run took %v", time.Since(start).Round(time.Second))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Status != "escalated" || rep.ExitCode != 4 || rep.StopReason != "human_stop" {
		t.Fatalf("report = %s (%s) exit %d, failures %v", rep.Status, rep.StopReason, rep.ExitCode, rep.Failures)
	}
	want := map[string]blackboard.Status{"fn-farewell": blackboard.StatusVerified, "fn-greet": blackboard.StatusVerified, "fn-bye": blackboard.StatusVerified,
		"fn-hello": blackboard.StatusEscalated, "fn-serve": blackboard.StatusPending}
	for id, st := range want {
		if got := g.row(t, id).Status; got != st {
			t.Errorf("%s is %s, want %s", id, got, st)
		}
	}
	if len(rep.Failures) != 2 || !strings.HasPrefix(rep.Failures[0], "fn-hello: ") || rep.Failures[1] != "fn-serve: blocked by fn-hello" {
		t.Errorf("failures = %q, want fn-hello and then fn-serve blocked by fn-hello", rep.Failures)
	}
	if n := rep.Nodes; n.Verified != 3 || n.Escalated != 1 || n.Blocked != 1 || n.Verified+n.Escalated+n.Blocked != n.Total || n.Failed != 0 {
		t.Errorf("nodes = %+v, want 3 verified, 1 escalated, 1 blocked of 5", n)
	}
	if rep.Acceptance.Passed != 0 || fileExists(filepath.Join(g.runDir, "acceptance.json")) {
		t.Error("an acceptance command ran for a run with an escalated leaf")
	}
	for _, s := range []string{"verified"} {
		if rd := readReportFile(t, g); rd.Status == s {
			t.Errorf("report.json says %s", s)
		}
	}
	if now := strings.TrimSpace(g.gitCmd("rev-parse", "main")); now != seed {
		t.Errorf("main moved from %s to %s", seed, now)
	}
	branch := "gm/" + g.id
	subjects := strings.Split(strings.TrimSpace(g.gitCmd("log", "--format=%s", seed+".."+branch)), "\n")
	if len(subjects) != 4 || !strings.HasPrefix(subjects[3], "gm(wave0): ") {
		t.Errorf("the work branch holds %q, want Wave 0 and three leaf commits", subjects)
	}
	for _, s := range subjects[:3] {
		if strings.HasPrefix(s, "gm(fn-hello)") || strings.HasPrefix(s, "gm(fn-serve)") {
			t.Errorf("commit %q for a leaf that did not pass", s)
		}
	}
	// The calls for fn-hello stay inside the bound of spec 7.3 with one revision.
	entries, fix, rev := 2, g.cfg.Executor.FixAttempts, 1
	implement, revise := 0, 0
	for _, s := range stagesOf(g.fake) {
		switch s {
		case "implement:fn-hello":
			implement++
		case "revise:fn-hello":
			revise++
		}
	}
	if bound := entries*(1+fix)*(rev+1) + rev; implement > bound || implement == 0 {
		t.Errorf("fn-hello implement calls = %d, bound %d", implement, bound)
	}
	if revise != rev {
		t.Errorf("fn-hello revise calls = %d, want %d", revise, rev)
	}
	for _, s := range stagesOf(g.fake) {
		if strings.HasSuffix(s, ":fn-serve") {
			t.Errorf("a model was called for the blocked leaf: %s", s)
		}
	}
	noLeak(t, e2eStores(t, g, rep, nil), []string{canarySecret, "CANARY-REPLY-TEXT", "CANARY-TEST-OUTPUT"})
}

// ---- a process killed midway, resumed through Run ----

// TestE2EChildProcess is the child of TestExecutorE2EResumeAfterKill. It does
// nothing unless the parent started it: it runs the end-to-end script over the
// parent's disk rig until fn-hello's first reply is asked for, and waits to be
// killed there (waves 0 is verified and committed, a claim is left behind).
func TestE2EChildProcess(t *testing.T) {
	if os.Getenv("GM_E2E_CHILD") != "1" {
		return
	}
	dir := os.Getenv("GM_E2E_DIR")
	g := newRigIn(t, dir, e2eScript(t), e2eOpts(t))
	e2eGate(g)
	var once sync.Once
	g.fake.Before = func(stage string) {
		if stage == "implement:fn-hello" {
			once.Do(func() { blockHere(dir) })
		}
	}
	rep, err := Run(context.Background(), g.options())
	t.Fatalf("the child ran to its end (%s) without reaching the kill point: %v", rep.Status, err)
}

func TestExecutorE2EResumeAfterKill(t *testing.T) {
	t.Parallel()
	cache := goCacheOverride
	if cache == "" {
		t.Skip("no shared go cache")
	}
	opts := e2eOpts(t)

	// The uninterrupted run to compare with: the one TestExecutorE2E recorded,
	// else a run of its own (so this test does not depend on test order).
	var wantTree string
	var wantSubjects []string
	e2eRef.mu.Lock()
	if e2eRef.set {
		wantTree, wantSubjects = e2eRef.tree, append([]string(nil), e2eRef.subjects...)
	}
	e2eRef.mu.Unlock()
	if wantTree == "" {
		refDir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		ref := newRigIn(t, refDir, e2eScript(t), opts)
		e2eGate(ref)
		rep, err := Run(context.Background(), ref.options())
		if err != nil || rep.Status != "verified" {
			t.Fatalf("the reference run is %s (%s): %v %v", rep.Status, rep.StopReason, rep.Failures, err)
		}
		wantTree, wantSubjects = treeAndSubjects(ref)
		ref.close()
	}

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newRigIn(t, dir, Script{}, opts).close() // plans once, in the parent

	cmd := exec.Command(os.Args[0], "-test.run=^TestE2EChildProcess$")
	cmd.Env = append(os.Environ(), "GM_E2E_CHILD=1", "GM_E2E_DIR="+dir, "GM_TEST_GOCACHE="+cache)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	marker := filepath.Join(dir, "in-flight")
	deadline := time.After(8 * time.Minute)
wait:
	for {
		select {
		case <-exited:
			if !fileExists(marker) {
				t.Fatalf("the child ended before the kill point:\n%s", tail(out.String()))
			}
			break wait
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("the kill point was not reached in time:\n%s", tail(out.String()))
		case <-time.After(50 * time.Millisecond):
			if fileExists(marker) {
				break wait
			}
		}
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL: nothing in the child runs again
		t.Logf("kill: %v", err)
	}
	<-exited
	time.Sleep(1500 * time.Millisecond) // the dead process's claim is stale after 1 s

	// The resumed process has a new script object: it holds what the dead process
	// had not used yet. fn-farewell and fn-greet were verified, and fn-bye's racy
	// reply was consumed (its repair takes the good one).
	rest := e2eScript(t)
	delete(rest, "implement:fn-farewell")
	delete(rest, "implement:fn-greet")
	rest["implement:fn-bye"] = []step{reply(good("fn-bye"))}
	g := newRigIn(t, dir, rest, opts)
	e2eGate(g)
	ctx := context.Background()
	rows, err := g.board.List(ctx, g.id, blackboard.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	pre := map[string]blackboard.Status{}
	for _, r := range rows {
		pre[r.NodeID] = r.Status
	}
	if pre["fn-farewell"] != blackboard.StatusVerified || pre["fn-greet"] != blackboard.StatusVerified {
		t.Fatalf("after the kill the wave 0 leaves are %s and %s, want verified", pre["fn-farewell"], pre["fn-greet"])
	}
	rep, err := Run(ctx, g.options())
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if rep.Status != "verified" || !rep.Resumed || rep.ExitCode != 0 {
		t.Fatalf("resume = %s (%s) resumed %v exit %d, failures %v", rep.Status, rep.StopReason, rep.Resumed, rep.ExitCode, rep.Failures)
	}
	for _, id := range []string{"fn-farewell", "fn-greet"} {
		for _, s := range stagesOf(g.fake) {
			if s == "implement:"+id {
				t.Errorf("the resume called %s again after it was verified before the kill", s)
			}
		}
	}
	tree, subs := treeAndSubjects(g)
	if tree != wantTree {
		t.Errorf("final tree %s, uninterrupted %s", tree, wantTree)
	}
	// The same commits. Their order may differ: a resume re-runs the integration
	// checks of the waves already built over every verified leaf, so fn-bye's
	// race is found and repaired at that check, before fn-hello is built.
	sort.Strings(subs)
	sort.Strings(wantSubjects)
	if strings.Join(subs, "\n") != strings.Join(wantSubjects, "\n") {
		t.Errorf("commits differ from the uninterrupted run:\n%s\nvs\n%s", strings.Join(subs, "\n"), strings.Join(wantSubjects, "\n"))
	}
	for _, l := range g.plan.Leaves {
		if n := g.leafCommits(l.ID); n != 1 && !(l.ID == "fn-bye" && n == 2) {
			t.Errorf("%s has %d commits", l.ID, n)
		}
	}
	for _, needle := range []string{canarySecret, "CANARY-REPLY-TEXT", "CANARY-TEST-OUTPUT"} {
		if hasCanary(t, g, needle) {
			t.Errorf("%q reached a store", needle)
		}
	}
}

// logSlowSteps says where the wall time went: the five longest gaps between
// consecutive events, each named by the event that ended it.
func logSlowSteps(t *testing.T, evs []events.Event) {
	t.Helper()
	type gap struct {
		d    time.Duration
		what string
	}
	var gaps []gap
	for i := 1; i < len(evs); i++ {
		gaps = append(gaps, gap{evs[i].At.Sub(evs[i-1].At), evs[i].Kind + " " + evs[i].NodeID})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].d > gaps[j].d })
	for i := 0; i < len(gaps) && i < 5; i++ {
		t.Logf("slow step %d: %v before %s", i+1, gaps[i].d.Round(10*time.Millisecond), gaps[i].what)
	}
}

// warmBuildCache compiles, once and outside the timed run, the standard library
// packages the greeter leaves use (net/http, httptest, testing, with and
// without -race) into the shared GOCACHE. The test process starts with an empty
// cache, and on a busy machine that cold compile alone was two minutes of the
// run (the slowest step in logSlowSteps), which says nothing about the
// executor. The time it took is logged.
func warmBuildCache(t *testing.T) {
	t.Helper()
	start := time.Now()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/warm\n\ngo 1.22\n")
	write(t, filepath.Join(dir, "warm.go"), "package warm\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n\t\"strings\"\n)\n\nfunc W(w http.ResponseWriter) { fmt.Fprint(w, strings.TrimSpace(\" x \")) }\n")
	write(t, filepath.Join(dir, "warm_test.go"), "package warm\n\nimport (\n\t\"net/http/httptest\"\n\t\"sync\"\n\t\"testing\"\n)\n\nfunc TestW(t *testing.T) {\n\tvar wg sync.WaitGroup\n\twg.Add(1)\n\tgo func() { defer wg.Done(); W(httptest.NewRecorder()) }()\n\twg.Wait()\n}\n")
	_ = e2eGo(t, dir, "vet", "./...")
	_ = e2eGo(t, dir, "test", "-race", "-count=1", "./...")
	_ = e2eGo(t, dir, "test", "-count=1", "./...")
	t.Logf("build cache warmed in %v (not part of the timed run)", time.Since(start).Round(time.Second))
}
