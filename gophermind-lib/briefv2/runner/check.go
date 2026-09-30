package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Failure classes: the text before the first colon in blackboard failure_reason (spec 7.2).
// The last three are not the model's fault or a pass: ClassHarness (the command could not
// run: missing binary, sandbox refusal, invalid caller input), ClassCancelled (the context
// ended) and ClassTimeout (a gofmt, build or vet stage outlived Config.StageTimeout). The
// executor must handle them apart from build, vet and test failures; none of them is a pass.
const (
	ClassMalformed   = "malformed"
	ClassBuild       = "build"
	ClassVet         = "vet"
	ClassTestFail    = "test_fail"
	ClassTestPanic   = "test_panic"
	ClassTestTimeout = "test_timeout"
	ClassNoTestsRan  = "no_tests_ran"
	ClassHarness     = "harness"
	ClassCancelled   = "cancelled"
	ClassTimeout     = "timeout"
)

const maxVerdictNames = 8

var errCancelled = errors.New("runner: the run was cancelled")

// Verdict is the outcome of a check. Its persisted form is Reason: a class and test names
// only. Out is the failing stage's output, in memory only.
type Verdict struct {
	Class     string   // "" means pass
	Names     []string // failing test names without the package, at most 8, sanitized
	QNames    []string // the same failures as "package.Test", at most 8, sanitized (./... attribution)
	Locations []Location
	Events    int
	Out       Output
	Err       error // set for ClassHarness and ClassCancelled; a fixed message, never output
}

// Pass reports that no stage failed.
func (v Verdict) Pass() bool { return v.Class == "" }

// Faulted reports a harness fault or a cancellation: neither a model failure nor a pass.
func (v Verdict) Faulted() bool { return v.Class == ClassHarness || v.Class == ClassCancelled }

// Reason is "test_fail: TestX/case_a, TestX/case_b" or just "build". Class and names only.
func (v Verdict) Reason() string {
	if v.Class == "" {
		return ""
	}
	if len(v.Names) == 0 {
		return v.Class
	}
	return v.Class + ": " + strings.Join(v.Names, ", ")
}

func safeNames(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, n := range raw {
		out = append(out, SafeName(n))
	}
	sort.Strings(out)
	if len(out) > maxVerdictNames {
		out = out[:maxVerdictNames]
	}
	return out
}

func capNames(in []string) []string {
	out := append([]string(nil), in...)
	if len(out) > maxVerdictNames {
		out = out[:maxVerdictNames]
	}
	return out
}

var testFuncRE = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)

// RunRegex returns "^(TestA|TestB)$"; each name must match ^Test[A-Za-z0-9_]*$.
func RunRegex(funcs []string) (string, error) {
	if len(funcs) == 0 {
		return "", errors.New("runner: no test functions named")
	}
	for i, f := range funcs {
		if !testFuncRE.MatchString(f) {
			return "", fmt.Errorf("runner: test function %d is not a valid name", i)
		}
	}
	return "^(" + strings.Join(funcs, "|") + ")$", nil
}

var pkgRE = regexp.MustCompile(`^(\./)?[A-Za-z0-9_./-]*(\.\.\.)?$`)

// validPkg accepts a package pattern the go tool cannot read as a flag or a path outside the repo.
func validPkg(p string) bool {
	if p == "" || strings.HasPrefix(p, "-") || strings.HasPrefix(p, "/") || !pkgRE.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// validFile accepts a repo-relative file path that no tool can read as a flag.
func validFile(f string) bool {
	if f == "" || strings.HasPrefix(f, "-") || !filepath.IsLocal(f) {
		return false
	}
	for _, r := range f {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func harness(err error) Verdict { return Verdict{Class: ClassHarness, Err: err} }

var errInvalidInput = errors.New("runner: invalid argument for a check")

// lookInEnv finds name on the PATH given in env. The runner never reads os.Environ.
func lookInEnv(name string, env []string) (string, error) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, d := range filepath.SplitList(v) {
				if !filepath.IsAbs(d) {
					continue
				}
				p := filepath.Join(d, name)
				if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
					return p, nil
				}
			}
		}
	}
	return "", errors.New("runner: " + name + " not found on the toolchain PATH")
}

func tool(name string, env []string, args ...string) ([]string, error) {
	p, err := lookInEnv(name, env)
	if err != nil {
		return nil, err
	}
	return append([]string{p}, args...), nil
}

// fault maps a Result that says the command did not really run to its verdict.
func fault(res Result) (Verdict, bool) {
	switch {
	case res.Err != nil:
		return harness(res.Err), true
	case res.Canceled:
		return Verdict{Class: ClassCancelled, Err: errCancelled}, true
	}
	return Verdict{}, false
}

// stageFail builds the failing verdict for a non-test stage.
func stageFail(class, repo string, res Result) Verdict {
	if v, ok := fault(res); ok {
		return v
	}
	if res.TimedOut {
		class = ClassTimeout
	}
	return Verdict{Class: class, Out: res.Out, Locations: ParseLocations(repo, res.Out.Text())}
}

// goStage runs one go build or vet stage. A vet run that could not type-check prints
// "vet: " diagnostics; that is a build problem, not a vet finding.
func (r *Runner) goStage(ctx context.Context, repo string, env []string, class string, args ...string) (Verdict, bool) {
	argv, err := tool("go", env, args...)
	if err != nil {
		return harness(err), false
	}
	res := r.Run(ctx, Spec{Dir: repo, Argv: argv, Env: env, Timeout: r.cfg.StageTimeout})
	if res.Err != nil || res.Canceled || res.TimedOut || res.ExitCode != 0 {
		if class == ClassVet && res.Err == nil && vetTypeCheckFailed(res.Out.Text()) {
			class = ClassBuild
		}
		return stageFail(class, repo, res), false
	}
	return Verdict{}, true
}

func vetTypeCheckFailed(text string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "vet: ") {
			return true
		}
	}
	return false
}

// BuildVet runs go build ./... then go vet ./..., first failure wins.
func (r *Runner) BuildVet(ctx context.Context, repo string, env []string) Verdict {
	if v, ok := r.goStage(ctx, repo, env, ClassBuild, "build", "--", "./..."); !ok {
		return v
	}
	if v, ok := r.goStage(ctx, repo, env, ClassVet, "vet", "--", "./..."); !ok {
		return v
	}
	return Verdict{}
}

// LeafCheck is one leaf's verification: gofmt, build, vet, then its one test.
type LeafCheck struct {
	Repo, Dir   string   // Dir is the repo-relative package directory, "internal/greet"
	TestFunc    string   // "TestGreet"; must match ^Test[A-Za-z0-9_]*$
	Files       []string // repo-relative files to gofmt-check (the real file)
	Env         []string
	TestTimeout time.Duration // also passed as -timeout so a hang is reported by go test, not only killed
}

func pkgArg(dir string) string {
	d := filepath.ToSlash(filepath.Clean(dir))
	if d == "." || d == "" {
		return "."
	}
	return "./" + d
}

// CheckLeaf runs gofmt, build, vet and test in that order; the first failure wins. A leaf
// passes only when go test exits 0, the package result is pass and TestFunc itself has a pass action.
func (r *Runner) CheckLeaf(ctx context.Context, c LeafCheck) Verdict {
	regex, err := RunRegex([]string{c.TestFunc})
	if err != nil || !validPkg(c.Dir) {
		return harness(errInvalidInput)
	}
	for _, f := range c.Files {
		if !validFile(f) {
			return harness(errInvalidInput)
		}
	}
	if len(c.Files) > 0 {
		argv, err := tool("gofmt", c.Env, append([]string{"-l", "--"}, c.Files...)...)
		if err != nil {
			return harness(err)
		}
		res := r.Run(ctx, Spec{Dir: c.Repo, Argv: argv, Env: c.Env, Timeout: r.cfg.StageTimeout})
		if res.Err != nil || res.Canceled || res.TimedOut || res.ExitCode != 0 || strings.TrimSpace(res.Out.Text()) != "" {
			return stageFail(ClassMalformed, c.Repo, res)
		}
	}
	pkg := pkgArg(c.Dir)
	if v, ok := r.goStage(ctx, c.Repo, c.Env, ClassBuild, "build", "--", pkg); !ok {
		return v
	}
	if v, ok := r.goStage(ctx, c.Repo, c.Env, ClassVet, "vet", "--", pkg); !ok {
		return v
	}
	return r.runTests(ctx, c.Repo, c.Env, c.TestTimeout, []string{c.TestFunc}, "-run", regex, pkg)
}

// TestSet is a go test run over a package or the whole module.
type TestSet struct {
	Repo    string
	Pkg     string   // "./internal/greet", or "./..." with Funcs empty
	Funcs   []string // when non-empty, -run ^(A|B)$
	Expect  []string // top-level tests that must each have a pass action; defaults to Funcs
	Race    bool
	Env     []string
	Timeout time.Duration
}

// Test runs go test -json and classifies it. Zero events is never a pass.
func (r *Runner) Test(ctx context.Context, t TestSet) Verdict {
	if !validPkg(t.Pkg) {
		return harness(errInvalidInput)
	}
	var extra []string
	if t.Race {
		extra = append(extra, "-race")
	}
	expect := t.Expect
	if len(t.Funcs) > 0 {
		regex, err := RunRegex(t.Funcs)
		if err != nil {
			return harness(errInvalidInput)
		}
		extra = append(extra, "-run", regex)
		if len(expect) == 0 {
			expect = t.Funcs
		}
	}
	for _, e := range expect {
		if !testFuncRE.MatchString(e) {
			return harness(errInvalidInput)
		}
	}
	return r.runTests(ctx, t.Repo, t.Env, t.Timeout, expect, append(extra, t.Pkg)...)
}

// runTests runs go test with the given extra args (flags then package) and classifies the
// result from the structured stream, which a separate line parser reads as the child writes,
// independent of the capped Output. See classify.
func (r *Runner) runTests(ctx context.Context, repo string, env []string, tmo time.Duration, expect []string, extra ...string) Verdict {
	args := []string{"test", "-count=1", "-json"}
	kill := r.cfg.StageTimeout
	if tmo > 0 {
		args = append(args, "-timeout", tmo.String())
		kill = tmo + 30*time.Second // go test reports its own timeout first
	}
	argv, err := tool("go", env, append(args, extra...)...)
	if err != nil {
		return harness(err)
	}
	p := newStreamParser(r.cfg.OutputCap)
	res := r.Run(ctx, Spec{Dir: repo, Argv: argv, Env: env, Timeout: kill, stream: p})
	rep := p.finish()
	if v, ok := fault(res); ok {
		return v
	}
	v := Verdict{Events: rep.Events, Out: rep.out, Locations: ParseLocations(repo, rep.out.Text())}
	var names, qnames []string
	v.Class, names, qnames = classify(res, rep, expect)
	if v.Class != "" {
		v.Names, v.QNames = safeNames(names), capNames(qnames)
	}
	return v
}

// classify decides from the structure: exit code, per-test and per-package actions and the
// FailedBuild field. Text markers only name the kind of a failure already established; they
// never create one, so a passing test printing "panic: " cannot force a failure.
//
// A pass needs exit 0, no failed or unfinished test, a package-level pass, at least one test
// event and a pass action for every expected top-level test.
func classify(res Result, rep TestReport, expect []string) (class string, names, qnames []string) {
	if res.TimedOut {
		return ClassTestTimeout, rep.names, rep.qnames
	}
	crashed := res.ExitCode != 0
	failed := len(rep.Failed) > 0 || rep.PkgFailed || rep.Overflow || crashed
	for _, st := range rep.Pkgs {
		if st == "fail" {
			failed = true
		}
	}
	if failed {
		switch {
		case rep.BuildFailed, rep.RawBuildFailed && !rep.Structured:
			return ClassBuild, nil, nil
		case rep.TimedOut:
			return ClassTestTimeout, rep.names, rep.qnames
		case rep.Panicked:
			return ClassTestPanic, rep.names, rep.qnames
		}
		return ClassTestFail, rep.names, rep.qnames
	}
	if rep.Events == 0 {
		return ClassNoTestsRan, nil, nil
	}
	anyPass := false
	for _, st := range rep.Pkgs {
		if st == "pass" {
			anyPass = true
		}
	}
	if !anyPass {
		return ClassTestFail, nil, nil
	}
	var missing []string
	for _, e := range expect {
		if !rep.tops[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		return ClassTestFail, missing, nil
	}
	return "", nil, nil
}
