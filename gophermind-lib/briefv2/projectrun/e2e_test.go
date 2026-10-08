package projectrun

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/gitenv"
)

// The end-to-end tests run /project over the greeter with the real planner,
// the real executor and real git; only the model is scripted.

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func lastLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// verifiedRun is one full unattended run that must end verified. Tests that
// need a finished run share it through this helper.
func verifiedRun(t *testing.T, r *projectRig) Result {
	t.Helper()
	res := r.run()
	if res.ExitCode != 0 || res.Status != "verified" {
		t.Fatalf("Result = %+v, want verified\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), r.err.String())
	}
	return res
}

func TestProjectE2EGreeter(t *testing.T) {
	r := newProjectRig(t)
	res := r.run()
	stdout, stderr := r.out.String(), r.err.String()
	if res.ExitCode != 0 || res.Status != "verified" {
		t.Fatalf("(1) Result = %+v, want exit 0 verified\nstdout:\n%s\nstderr:\n%s", res, stdout, stderr)
	}
	if n := atomic.LoadInt32(&r.execCalls); n != 1 {
		t.Fatalf("(1) executor calls = %d, want 1", n)
	}
	if last := r.model.lastPlannerSeq(); r.execAtSeq < last {
		t.Errorf("(1) the executor started after %d requests, but a planner request came later (the last was %d)", r.execAtSeq, last)
	}

	// (2) approval and understanding.
	runDir := r.runDir()
	var ap struct {
		ApprovedBy        string `json:"approved_by"`
		PlanHash          string `json:"plan_hash"`
		UnderstandingHash string `json:"understanding_hash"`
	}
	readJSONFile(t, filepath.Join(runDir, "approval.json"), &ap)
	if ap.ApprovedBy != "unattended" {
		t.Errorf("(2) approved_by = %q", ap.ApprovedBy)
	}
	t.Run("TestUnattendedApproveBindsHash", func(t *testing.T) {
		_, hash, err := planner.RenderPlan(runDir)
		if err != nil {
			t.Fatal(err)
		}
		if ap.PlanHash == "" || ap.PlanHash != hash {
			t.Errorf("plan_hash = %q, RenderPlan hash = %q", ap.PlanHash, hash)
		}
	})
	u, ok, err := planner.ReadUnderstanding(runDir)
	if err != nil || !ok {
		t.Fatalf("(2) understanding: %+v %v %v", u, ok, err)
	}
	if ap.UnderstandingHash == "" || ap.UnderstandingHash != u.Hash || u.ConfirmedBy != "unattended" {
		t.Errorf("(2) understanding_hash = %q, understanding = %+v", ap.UnderstandingHash, u)
	}
	md, err := os.ReadFile(filepath.Join(runDir, "UNDERSTANDING.md"))
	if err != nil {
		t.Fatalf("(2) UNDERSTANDING.md: %v", err)
	}
	if strings.Contains(string(md), "```decision") {
		t.Errorf("(2) UNDERSTANDING.md holds a decision block")
	}

	// (3) the two clarify questions were settled by their recommendations.
	answers, err := planner.ReadAnswers(runDir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"q1": "Yes, trim it.", "q2": "Yes, use world."}
	assumed := 0
	for _, a := range answers {
		if a.Assumed && a.Stage == "clarify" {
			assumed++
			if want[a.ID] != a.Answer {
				t.Errorf("(3) answer %s = %q, want %q", a.ID, a.Answer, want[a.ID])
			}
		}
	}
	if assumed != 2 {
		t.Errorf("(3) assumed clarify answers = %d, want 2 (%+v)", assumed, answers)
	}
	p := r.project()
	if len(p.Ambiguity.ClarifyDefaulted) != 2 || p.Ambiguity.Rounds != 2 {
		t.Errorf("(3) ambiguity = %+v, want 2 defaulted over 2 rounds", p.Ambiguity)
	}
	if !strings.Contains(stdout, "q1, q2") {
		t.Errorf("(3) printed text lacks the ids q1, q2")
	}
	for _, q := range []string{"trimmed of surrounding spaces", "fall back to world"} {
		if strings.Contains(stdout, q) {
			t.Errorf("(3) printed text contains question text %q", q)
		}
	}

	// (4) _state/project.json.
	if p.Mode != "unattended" || !p.Graded || p.Resumed {
		t.Errorf("(4) mode %q graded %v resumed %v", p.Mode, p.Graded, p.Resumed)
	}
	src := map[string]string{}
	for _, s := range p.Secrets {
		src[s.Name] = string(s.Source)
	}
	if src["GREETER_TOKEN"] != "vault" || src["GREETER_SALT"] != "generated:hex32" {
		t.Errorf("(4) secrets = %v", src)
	}
	if p.Binary.Commit != "abc1234" || p.Binary.Path == "" {
		t.Errorf("(4) binary = %+v", p.Binary)
	}
	if len(p.Providers) != 1 || p.Providers[0].Host != "a.invalid" {
		t.Errorf("(4) providers = %+v", p.Providers)
	}
	m := r.projectMap()
	if _, ok := m["planner_warnings"].(map[string]any); !ok {
		t.Errorf("(4) planner_warnings is %T, want an object", m["planner_warnings"])
	}
	if !p.Ambiguity.MilestoneApprovals || !strings.Contains(stdout, "milestone_approvals: declared") {
		t.Errorf("(4) milestone_approvals not recorded: %+v", p.Ambiguity)
	}
	if p.Executor == nil || p.Executor.Status != "verified" || p.Understanding.ConfirmedBy != "unattended" {
		t.Errorf("(4) executor %+v understanding %+v", p.Executor, p.Understanding)
	}
	if len(p.ByNodeClass) == 0 {
		t.Errorf("(4) by_node_class is empty")
	}
	leaves, verified := 0, 0
	for _, c := range p.ByNodeClass {
		leaves += c.Leaves
		verified += c.Verified
	}
	if leaves != 5 || verified != 5 {
		t.Errorf("(4) by_node_class leaves %d verified %d, want 5 and 5: %+v", leaves, verified, p.ByNodeClass)
	}

	// (5) the proof lines and the resumed line.
	tail := lastLines(stdout, 2)
	if len(tail) != 2 || tail[0] != "Requirements covered: 7 of 7" || tail[1] != "Acceptance passed: 2 of 2" {
		t.Errorf("(5) last lines = %q", tail)
	}
	if !strings.Contains(stdout, "resumed: no") {
		t.Errorf("(5) stdout lacks \"resumed: no\"")
	}

	// (6) landing.
	if got := strings.TrimSpace(r.git("rev-parse", "main")); got == strings.TrimSpace(r.git("rev-parse", "baseline")) {
		t.Errorf("(6) main did not move from baseline")
	}
	if out := r.git("merge-base", "--is-ancestor", "baseline", "main"); out != "" {
		t.Errorf("(6) %q", out)
	}
	if out := strings.TrimSpace(r.git("branch", "--list", r.branch)); out == "" {
		t.Errorf("(6) branch %s does not exist", r.branch)
	}

	// (7) no question or approval files, no database anywhere.
	for _, name := range []string{"QUESTIONS.md", "APPROVAL.md"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err == nil {
			t.Errorf("(7) %s exists in the run folder", name)
		}
	}
	walkFiles(t, []string{runDir, r.repo, r.cfgDir}, func(path string, _ []byte) {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".db") || strings.HasSuffix(base, ".db-wal") || strings.Contains(base, ".sqlite") {
			t.Errorf("(7) database file %s", path)
		}
	})
	for _, f := range []string{"calls.jsonl", "events.jsonl"} {
		if _, err := os.Stat(filepath.Join(runDir, "_state", f)); err != nil {
			t.Errorf("(7) _state/%s: %v", f, err)
		}
	}
	lv := 0
	_ = filepath.WalkDir(runDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".runtime.json") && !strings.Contains(path, "_state") {
			if _, serr := os.Stat(strings.TrimSuffix(path, ".json") + ".runtime.json"); serr == nil {
				lv++
			}
		}
		return nil
	})
	if lv < 5 {
		t.Errorf("(7) only %d nodes have a .runtime.json beside them, want at least 5 leaves", lv)
	}
}

func TestHealthyPathNeverResumes(t *testing.T) {
	r := newProjectRig(t)
	var starts []planner.Options
	withPlanner(t, func(ctx context.Context, d planner.Deps, o planner.Options) (planner.Outcome, error) {
		starts = append(starts, o)
		return planner.New(d).Run(ctx, o)
	})
	verifiedRun(t, r)
	if len(starts) != 1 || starts[0].RunID != "" {
		t.Errorf("planner starts = %+v, want one with an empty RunID", starts)
	}
	if p := r.project(); p.Executor == nil || p.Executor.Resumed || p.Resumed {
		t.Errorf("resumed: report %v executor %+v", p.Resumed, p.Executor)
	}
	if strings.Contains(r.out.String(), "--resume") || strings.Contains(r.err.String(), "--resume") {
		t.Errorf("a healthy run mentioned --resume\nstdout:\n%s\nstderr:\n%s", r.out.String(), r.err.String())
	}
}

func TestNoSecretValueAnywhere(t *testing.T) {
	r := newProjectRig(t)
	verifiedRun(t, r)
	v, err := vault.Open(r.vault, e2ePass, vault.Options{WorkFactor: 10})
	if err != nil {
		t.Fatal(err)
	}
	salt, ok := v.Get(vault.HarnessScope, "GREETER_SALT")
	if !ok {
		salt, ok = v.Get(vault.RunScope(r.id), "GREETER_SALT")
	}
	if !ok || len(salt) < 32 {
		t.Fatalf("the generated salt is not in the vault (found %v, length %d)", ok, len(salt))
	}
	secrets := map[string]string{"canary": e2eCanary, "salt": salt}
	check := func(where, text string) {
		for name, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("the %s value is in %s", name, where)
			}
		}
	}
	check("stdout", r.out.String())
	check("stderr", r.err.String())
	roots := []string{r.runDir()}
	if _, err := os.Stat(r.scratch()); err == nil {
		roots = append(roots, r.scratch())
	}
	files := 0
	walkFiles(t, roots, func(path string, raw []byte) {
		files++
		check(path, string(raw))
	})
	for _, rel := range []string{"_state/project.json", "report.json", "_state/calls.jsonl", "_state/events.jsonl"} {
		if _, err := os.Stat(filepath.Join(r.runDir(), rel)); err != nil {
			t.Errorf("%s was not scanned: %v", rel, err)
		}
	}
	if files < 20 {
		t.Errorf("only %d files were scanned", files)
	}
	check("git log -p --all", r.git("log", "-p", "--all"))
}

// stuckCoverage writes the folder of replies whose coverage stage leaves
// acceptance bullet A2 without a root test and cannot fill it
// (planner/testdata/greeter-stuck, built from the executor greeter).
func stuckCoverage(t *testing.T, fixture string) string {
	t.Helper()
	var cov struct {
		Map       json.RawMessage              `json:"map"`
		RootTests []map[string]json.RawMessage `json:"root_tests"`
	}
	readJSONFile(t, filepath.Join(fixture, "planner", "coverage.txt"), &cov)
	kept := cov.RootTests[:0:0]
	for _, rt := range cov.RootTests {
		if string(rt["requirement"]) != `"A2"` {
			kept = append(kept, rt)
		}
	}
	if len(kept) != len(cov.RootTests)-1 {
		t.Fatalf("the fixture coverage has no A2 root test to remove")
	}
	cov.RootTests = kept
	raw, err := json.Marshal(cov)
	if err != nil {
		t.Fatal(err)
	}
	return variant(t, map[string]string{"coverage.txt": string(raw), "coverage_fill.txt": `{"map": [], "root_tests": []}`})
}

func TestProjectPlanFailureExit1(t *testing.T) {
	r := newProjectRig(t)
	r.model = comboProvider(t, r.fixture, []string{filepath.Join(r.dir, "variant"), stuckCoverage(t, r.fixture)}, nil)
	res := r.run()
	wantResult(t, res, ExitFailed, "failed", "plan:coverage")
	if n := atomic.LoadInt32(&r.execCalls); n != 0 {
		t.Errorf("the executor ran %d time(s)", n)
	}
	tail := lastLines(r.out.String(), 2)
	if len(tail) != 2 || !strings.HasPrefix(tail[0], "Requirements covered: ") || tail[1] != "Acceptance passed: 0 of 2" {
		t.Errorf("the printed text ends with %q\n%s", tail, r.out.String())
	}
	if p := r.project(); p.StopReason != "plan:coverage" || p.ExitCode != ExitFailed {
		t.Errorf("project.json: %q exit %d", p.StopReason, p.ExitCode)
	}
}

func TestProjectEscalationExit4(t *testing.T) {
	r := newProjectRig(t, withScript(map[string][]string{
		"fn-hello": {"bad.fn-hello.1.txt", "bad.fn-hello.2.txt", "bad.fn-hello.3.txt"},
	}))
	res := r.run()
	if res.ExitCode != ExitEscalated || res.Status != "escalated" {
		t.Fatalf("Result = %+v, want exit 4 escalated\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), r.err.String())
	}
	if !strings.Contains(r.out.String(), "fn-hello") {
		t.Errorf("the printed text does not name the leaf:\n%s", r.out.String())
	}
}

func TestProjectInterruptedExit5(t *testing.T) {
	r := newProjectRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.model.onExec = func(stage string) {
		if stage == "implement:fn-hello" {
			cancel()
		}
	}
	res := r.runCtx(ctx)
	if res.ExitCode != ExitInterrupted || res.Status != "interrupted" {
		t.Fatalf("Result = %+v, want exit 5 interrupted\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), r.err.String())
	}
	if !strings.Contains(r.out.String(), "resume with --resume") {
		t.Errorf("the report does not say how to resume:\n%s", r.out.String())
	}
}

func TestProjectRefusesStaleStateWithoutResume(t *testing.T) {
	r := newProjectRig(t)
	verifiedRun(t, r)
	calls := r.model.fake.Calls()
	r.o.ExpectHead = ""
	r.o.Graded = false

	again := func() (Result, string) {
		r.out.Reset()
		r.err.Reset()
		res := r.run()
		return res, r.err.String()
	}
	res, errText := again()
	wantResult(t, res, ExitPreflight, "preflight_failed", "preflight")
	if !strings.Contains(errText, "stale state") || !strings.Contains(errText, "run folder") {
		t.Errorf("the stale state check is not named:\n%s", errText)
	}
	if n := r.model.fake.Calls(); n != calls {
		t.Errorf("provider calls went from %d to %d", calls, n)
	}

	r.git("branch", "-D", r.branch)
	res, errText = again()
	wantResult(t, res, ExitPreflight, "preflight_failed", "preflight")
	if !strings.Contains(errText, "stale state") || !strings.Contains(errText, "run folder") {
		t.Errorf("with only the branch deleted the run folder must still block:\n%s", errText)
	}

	r.o.Resume, r.o.PreflightOnly = true, true
	res, errText = again()
	if res.ExitCode != 0 || strings.Contains(r.out.String(), "FAILED") {
		t.Errorf("--resume over a finished run: %+v\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), errText)
	}
	if n := r.model.fake.Calls(); n != calls {
		t.Errorf("provider calls went from %d to %d", calls, n)
	}
}

func TestResumeFlagContinuesAndIsRecorded(t *testing.T) {
	r := newProjectRig(t)
	r.o.Graded, r.o.ExpectHead = false, ""
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.model.onExec = func(stage string) {
		if stage == "implement:fn-hello" {
			cancel()
		}
	}
	res := r.runCtx(ctx)
	if res.ExitCode != ExitInterrupted {
		t.Fatalf("first run = %+v\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), r.err.String())
	}
	planned := r.model.plannerRequests()

	r.model.onExec = nil
	r.o.Resume = true
	r.out.Reset()
	r.err.Reset()
	res = r.run()
	if res.ExitCode != 0 || res.Status != "verified" {
		t.Fatalf("resumed run = %+v\nstdout:\n%s\nstderr:\n%s", res, r.out.String(), r.err.String())
	}
	if n := r.model.plannerRequests(); n != planned {
		t.Errorf("the resumed run made %d planner request(s) for finished stages", n-planned)
	}
	if p := r.project(); !p.Resumed {
		t.Errorf("project.json resumed = false")
	}
	var rep struct {
		Resumed bool `json:"resumed"`
	}
	readJSONFile(t, filepath.Join(r.runDir(), "report.json"), &rep)
	if !rep.Resumed {
		t.Errorf("report.json resumed = false")
	}
}

func TestPreflightMissingItemsEndToEnd(t *testing.T) {
	r := newProjectRig(t, withoutToken(), withoutPassphrase())
	res := r.run()
	wantResult(t, res, ExitPreflight, "preflight_failed", "preflight")
	text := r.err.String()
	if !strings.Contains(text, "  1. ") || !strings.Contains(text, "  2. ") || !strings.Contains(text, "vault") || !strings.Contains(text, "GREETER_TOKEN") {
		t.Errorf("both items must come in one numbered listing:\n%s", text)
	}
	if n := r.model.fake.Calls(); n != 0 {
		t.Errorf("provider calls = %d, want 0", n)
	}
}

// probeClearCommands runs the clear commands in a throwaway repo. It reports
// the first one the user's git wrapper refuses, "" when none is.
func probeClearCommands(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "probe@example.com"},
		{"config", "user.name", "Probe"},
		{"config", "commit.gpgsign", "false"},
	} {
		e2eGit(t, dir, a...)
	}
	writeFile(t, filepath.Join(dir, "f.txt"), "x\n")
	e2eGit(t, dir, "add", "f.txt")
	e2eGit(t, dir, "commit", "-q", "-m", "seed")
	e2eGit(t, dir, "tag", "baseline")
	e2eGit(t, dir, "branch", "gm/x")
	writeFile(t, filepath.Join(dir, "junk.txt"), "j\n")
	for _, a := range [][]string{
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"branch", "-D", "gm/x"},
		{"reset", "--hard", "baseline"},
		{"clean", "-fdx", "-e", ".remember"},
	} {
		cmd := gitenv.Command(dir, a...)
		cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("git %s: %v: %s", strings.Join(a, " "), err, strings.TrimSpace(string(out)))
			return a[0]
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); err == nil {
		return "clean (did not remove an untracked file)"
	}
	return ""
}

// copyTreeTo copies a directory tree with a plain Go walk (the archive step).
func copyTreeTo(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
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

func countLines(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimRight(string(raw), "\n"), "\n"))
}

func TestClearedStateRerunsClean(t *testing.T) {
	if refused := probeClearCommands(t); refused != "" {
		t.Skip("the git wrapper blocks the clear commands; the orchestrator must test them in the target repo (spec 9)")
	}
	r := newProjectRig(t)
	verifiedRun(t, r)
	tree1 := strings.TrimSpace(r.git("rev-parse", "main^{tree}"))
	calls1 := countLines(t, filepath.Join(r.runDir(), "_state", "calls.jsonl"))
	if calls1 < 20 {
		t.Fatalf("the first run logged only %d calls", calls1)
	}

	// Step 0: archive the run folder, then check the archive.
	archive := filepath.Join(r.dir, "archive")
	copyTreeTo(t, r.runDir(), archive)
	for _, f := range []string{"_state/calls.jsonl", "report.json"} {
		if _, err := os.Stat(filepath.Join(archive, f)); err != nil {
			t.Fatalf("the archive lacks %s: %v", f, err)
		}
	}

	// The clear of spec 9, minus the branch delete first.
	r.git("symbolic-ref", "HEAD", "refs/heads/main")
	r.git("reset", "--hard", "baseline")
	r.git("clean", "-fdx", "-e", ".remember")
	record := filepath.Join(r.cfgDir, "runs", r.id+".json")
	if !filepath.IsAbs(record) || !strings.HasPrefix(record, r.cfgDir+string(filepath.Separator)) {
		t.Fatalf("the run record path %q is not inside the config dir", record)
	}
	t.Logf("removing %s", record)
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{r.runDir(), r.scratch()} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still exists after the clear", gone)
		}
	}

	r.o.PreflightOnly = true
	t.Run("branch not deleted is refused", func(t *testing.T) {
		r.out.Reset()
		r.err.Reset()
		res := r.run()
		wantResult(t, res, ExitPreflight, "preflight_failed", "preflight")
		if !strings.Contains(r.err.String(), "stale state") || !strings.Contains(r.err.String(), r.branch) {
			t.Errorf("exit 6 must name %s:\n%s", r.branch, r.err.String())
		}
	})

	r.git("branch", "-D", r.branch)
	r.out.Reset()
	r.err.Reset()
	if res := r.run(); res.ExitCode != 0 {
		t.Fatalf("preflight after the clear = %+v\n%s", res, r.err.String())
	}
	r.o.PreflightOnly = false
	r.out.Reset()
	r.err.Reset()
	before := r.model.fake.Calls()
	verifiedRun(t, r)
	if tree2 := strings.TrimSpace(r.git("rev-parse", "main^{tree}")); tree2 != tree1 {
		t.Errorf("the second run built tree %s, the first built %s", tree2, tree1)
	}
	calls := filepath.Join(r.runDir(), "_state", "calls.jsonl")
	if n, want := countLines(t, calls), r.model.fake.Calls()-before; n != want {
		t.Errorf("the second ledger holds %d calls, the second run made %d", n, want)
	}
	raw, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	var c struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(first), &c); err != nil || c.ID != 1 {
		t.Errorf("the second ledger starts at id %d (%v): %s", c.ID, err, first)
	}
}
