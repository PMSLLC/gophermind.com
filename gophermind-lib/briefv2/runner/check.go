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
const (
	ClassMalformed   = "malformed"
	ClassBuild       = "build"
	ClassVet         = "vet"
	ClassTestFail    = "test_fail"
	ClassTestPanic   = "test_panic"
	ClassTestTimeout = "test_timeout"
	ClassNoTestsRan  = "no_tests_ran"
)

const maxVerdictNames = 8

// Verdict is the outcome of a check. Its persisted form is Reason: a class and
// test names only. Out is the failing stage's output, in memory only.
type Verdict struct {
	Class     string   // "" means pass
	Names     []string // failing test names, at most 8, sanitized
	Locations []Location
	Events    int
	Out       Output
}

// Pass reports that no stage failed.
func (v Verdict) Pass() bool { return v.Class == "" }

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

func (r *Runner) tool(name string, env []string, args ...string) ([]string, error) {
	p, err := lookInEnv(name, env)
	if err != nil {
		return nil, err
	}
	return append([]string{p}, args...), nil
}

// stageFail builds the failing verdict for a non-test stage.
func stageFail(class, repo string, res Result) Verdict {
	return Verdict{Class: class, Out: res.Out, Locations: ParseLocations(repo, res.Out.Text())}
}

// goStage runs one go build or vet stage. A vet run that could not type-check
// prints "vet: " diagnostics; that is a build problem, not a vet finding.
func (r *Runner) goStage(ctx context.Context, repo string, env []string, class string, args ...string) (Verdict, bool) {
	argv, err := r.tool("go", env, args...)
	if err != nil {
		return Verdict{Class: class}, false
	}
	res := r.Run(ctx, Spec{Dir: repo, Argv: argv, Env: env})
	if res.Err != nil || res.Canceled || res.TimedOut || res.ExitCode != 0 {
		if class == ClassVet && vetTypeCheckFailed(res.Out.Text()) {
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
	if v, ok := r.goStage(ctx, repo, env, ClassBuild, "build", "./..."); !ok {
		return v
	}
	if v, ok := r.goStage(ctx, repo, env, ClassVet, "vet", "./..."); !ok {
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

// CheckLeaf runs gofmt, build, vet and test in that order; the first failure wins.
func (r *Runner) CheckLeaf(ctx context.Context, c LeafCheck) Verdict {
	regex, err := RunRegex([]string{c.TestFunc})
	if err != nil {
		return Verdict{Class: ClassMalformed}
	}
	if len(c.Files) > 0 {
		argv, err := r.tool("gofmt", c.Env, append([]string{"-l"}, c.Files...)...)
		if err != nil {
			return Verdict{Class: ClassMalformed}
		}
		res := r.Run(ctx, Spec{Dir: c.Repo, Argv: argv, Env: c.Env})
		if res.Err != nil || res.Canceled || res.TimedOut || res.ExitCode != 0 || strings.TrimSpace(res.Out.Text()) != "" {
			return stageFail(ClassMalformed, c.Repo, res)
		}
	}
	pkg := pkgArg(c.Dir)
	if v, ok := r.goStage(ctx, c.Repo, c.Env, ClassBuild, "build", pkg); !ok {
		return v
	}
	if v, ok := r.goStage(ctx, c.Repo, c.Env, ClassVet, "vet", pkg); !ok {
		return v
	}
	return r.runTests(ctx, c.Repo, c.Env, c.TestTimeout, c.TestFunc, "-run", regex, pkg)
}

// TestSet is a go test run over a package or the whole module.
type TestSet struct {
	Repo    string
	Pkg     string   // "./internal/greet", or "./..." with Funcs empty
	Funcs   []string // when non-empty, -run ^(A|B)$
	Race    bool
	Env     []string
	Timeout time.Duration
}

// Test runs go test -json and classifies it. Zero events is never a pass.
func (r *Runner) Test(ctx context.Context, t TestSet) Verdict {
	var extra []string
	if t.Race {
		extra = append(extra, "-race")
	}
	if len(t.Funcs) > 0 {
		regex, err := RunRegex(t.Funcs)
		if err != nil {
			return Verdict{Class: ClassMalformed}
		}
		extra = append(extra, "-run", regex)
	}
	return r.runTests(ctx, t.Repo, t.Env, t.Timeout, "", append(extra, t.Pkg)...)
}

// runTests runs go test with the given extra args (flags then package) and
// classifies the result. wantFunc, when set, must have passed at top level.
func (r *Runner) runTests(ctx context.Context, repo string, env []string, tmo time.Duration, wantFunc string, extra ...string) Verdict {
	args := []string{"test", "-count=1", "-json"}
	var kill time.Duration
	if tmo > 0 {
		args = append(args, "-timeout", tmo.String())
		kill = tmo + 30*time.Second // go test reports its own timeout first
	}
	argv, err := r.tool("go", env, append(args, extra...)...)
	if err != nil {
		return Verdict{Class: ClassBuild}
	}
	res := r.Run(ctx, Spec{Dir: repo, Argv: argv, Env: env, Timeout: kill})
	rep := ParseTestJSON(strings.NewReader(res.Out.Text()))
	v := Verdict{Events: rep.Events, Out: newOutput(rep.text, r.cfg.OutputCap), Locations: ParseLocations(repo, rep.text)}
	if res.Err != nil || res.Canceled {
		v.Class = ClassTestFail
		return v
	}
	v.Names = safeNames(rep.names)
	switch {
	case res.TimedOut || rep.TimedOut:
		v.Class = ClassTestTimeout
	case rep.BuildFailed:
		v.Class, v.Names = ClassBuild, nil
	case rep.Panicked:
		v.Class = ClassTestPanic
	case len(rep.Failed) > 0 || rep.PkgFailed:
		v.Class = ClassTestFail
	case res.ExitCode != 0:
		v.Class = ClassTestFail
	case rep.Events == 0:
		v.Class = ClassNoTestsRan
	case wantFunc != "" && !passedTopLevel(rep, wantFunc):
		v.Class, v.Names = ClassTestFail, []string{wantFunc}
	}
	return v
}

func passedTopLevel(rep TestReport, fn string) bool {
	for k := range rep.Passed {
		if strings.HasSuffix(k, "."+fn) {
			return true
		}
	}
	return false
}
