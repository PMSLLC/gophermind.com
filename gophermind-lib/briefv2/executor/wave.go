package executor

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/runner"
)

// checkFailure is one failing step of the integration checks. It carries
// classes, names and locations only: the runner's output stays in memory inside
// the runner's Verdict and never reaches this value.
type checkFailure struct {
	Kind      string            // runner.ClassBuild, ClassVet, ClassTestFail, ClassTestPanic, ClassTestTimeout, ClassNoTestsRan
	Dir       string            // package dir ("internal/greet", "." for the root) for a test failure; "" for build and vet of ./...
	Names     []string          // failing test names, sanitized (runner.SafeName), top-level and subtests
	Locations []runner.Location // repo-relative file:line records
}

// checkResult is the outcome of the integration checks of one wave.
type checkResult struct{ Failures []checkFailure } // empty means pass

// Pass reports that no step failed.
func (c checkResult) Pass() bool { return len(c.Failures) == 0 }

// redacted stands in for a package, a file or a name that held a secret.
const redacted = "(redacted)"

// waves is the sorted distinct wave numbers that hold a leaf.
func (rc *runCtx) waves() []int {
	seen := map[int]bool{}
	var out []int
	for _, l := range rc.plan.Leaves {
		if !seen[l.Wave] {
			seen[l.Wave] = true
			out = append(out, l.Wave)
		}
	}
	sort.Ints(out)
	return out
}

// verifiedTests is leaf Dir -> the sorted test functions of every leaf that is
// verified now. A leaf whose row cannot be read counts as not verified.
func (rc *runCtx) verifiedTests() map[string][]string {
	m, _ := rc.verifiedTestsCtx(context.Background())
	return m
}

func (rc *runCtx) verifiedTestsCtx(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	for _, l := range rc.plan.Leaves {
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, interruptStop("")
			}
			return nil, fmt.Errorf("executor: reading the row of %s failed", l.ID)
		}
		if row.Status == blackboard.StatusVerified {
			out[l.Dir] = append(out[l.Dir], l.TestFunc)
		}
	}
	for d := range out {
		sort.Strings(out[d])
	}
	return out, nil
}

// unverified is the leaves of the wave that are not verified, in id order.
func (rc *runCtx) unverified(ctx context.Context, leaves []*Leaf) ([]*Leaf, error) {
	var out []*Leaf
	for _, l := range leaves {
		row, err := rc.o.Board.Get(ctx, rc.plan.RunID, l.ID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, interruptStop("")
			}
			return nil, fmt.Errorf("executor: reading the row of %s failed", l.ID)
		}
		if row.Status != blackboard.StatusVerified {
			out = append(out, l)
		}
	}
	return out, nil
}

// stopOf returns the *stopError inside err, if there is one.
func stopOf(err error) (*stopError, bool) {
	var se *stopError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// runWave builds wave w (spec 8.2) and then checks it (8.3). The leaves of the
// wave that are not verified go to the scheduler in id order; a stop from it is
// returned as it is. A leaf skipped or escalated and continued does not stop
// the wave; its dependents are blocked. A wave with a blocked leaf is not
// complete, so the integration checks do not run: the run stops failed with
// every blocked leaf named with the dependency that blocks it. Otherwise the
// checks run; a failure that names no leaf stops the run (R10), any other goes
// to repairWave. A wave that passes its checks while a leaf of the last wave
// is not verified still ends the run failed: only a run with every leaf
// verified may pass.
func (rc *runCtx) runWave(ctx context.Context, w int) (*stopError, error) {
	var leaves []*Leaf
	for _, l := range rc.plan.Leaves {
		if l.Wave == w {
			leaves = append(leaves, l)
		}
	}
	todo, err := rc.unverified(ctx, leaves)
	if err != nil {
		return rc.faultOrStop(err)
	}
	wr, err := rc.scheduleLeaves(ctx, todo)
	if err != nil {
		return rc.faultOrStop(err)
	}
	if wr.Stop != nil {
		return wr.Stop, nil
	}
	if len(wr.Blocked) > 0 {
		return blockedStop(w, wr.Blocked), nil
	}

	all, err := rc.allVerified(ctx)
	if err != nil {
		return rc.faultOrStop(err)
	}
	last := w == rc.waves()[len(rc.waves())-1]
	final := last && all
	res, err := rc.waveChecks(ctx, w, final)
	if err != nil {
		return rc.faultOrStop(err)
	}
	if !res.Pass() {
		att := rc.plan.Attribute(res)
		if len(att.Unattributed) > 0 {
			return unattributableStop(w, att.Unattributed), nil
		}
		return rc.repairWave(ctx, w, res, final)
	}
	if last && !all {
		return rc.notVerifiedStop(ctx), nil
	}
	return nil, nil
}

// faultOrStop turns an error from a step into a stop when it is one, into the
// interrupted stop when the context ended, and otherwise into a harness fault.
func (rc *runCtx) faultOrStop(err error) (*stopError, error) {
	if se, ok := stopOf(err); ok {
		return se, nil
	}
	return nil, err
}

func (rc *runCtx) allVerified(ctx context.Context) (bool, error) {
	todo, err := rc.unverified(ctx, rc.plan.Leaves)
	return len(todo) == 0, err
}

// notVerifiedStop names every leaf that is not verified when the last wave
// ends. The ids come from the plan and the statuses from the blackboard.
func (rc *runCtx) notVerifiedStop(ctx context.Context) *stopError {
	todo, _ := rc.unverified(ctx, rc.plan.Leaves)
	var ids []string
	for _, l := range todo {
		ids = append(ids, l.ID)
	}
	return &stopError{Status: "failed", Reason: "leaves_not_verified",
		Message: "executor: the last wave ended with leaves that are not verified: " + strings.Join(ids, ", ")}
}

// blockedStop is the stop of a wave with blocked leaves: each named with the
// dependency that blocks it.
func blockedStop(w int, blocked map[string]string) *stopError {
	ids := make([]string, 0, len(blocked))
	for id := range blocked {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id + " (blocked by " + blocked[id] + ")"
	}
	return &stopError{Status: "failed", Reason: "blocked",
		Message: fmt.Sprintf("executor: wave %d has blocked leaves, so its integration checks did not run: %s", w, strings.Join(parts, ", "))}
}

// unattributableStop is R10: the failing locations are listed, never guessed
// onto a leaf.
func unattributableStop(w int, locs []string) *stopError {
	return &stopError{Status: "failed", Reason: "unattributable",
		Message: fmt.Sprintf("executor: wave %d failed its integration checks in a place no leaf owns: %s", w, strings.Join(locs, ", "))}
}

// repairWave is the repair loop of spec 8.3. Task 12a ships the stub: the run
// ends failed with the failure classes only. Task 12b replaces the body; the
// signature is what runWave calls and does not change. final says whether the
// failing check was the whole-module one.
func (rc *runCtx) repairWave(ctx context.Context, w int, res checkResult, final bool) (*stopError, error) {
	var kinds []string
	for _, f := range res.Failures {
		kinds = append(kinds, f.Kind)
	}
	return &stopError{Status: "failed", Reason: "wave_check_failed",
		Message: fmt.Sprintf("executor: wave %d failed its integration checks (%s)", w, strings.Join(kinds, ", "))}, nil
}

// waveChecks is spec 8.3 with no model: go build and go vet over ./..., then one
// race test run per package that holds a verified leaf (Funcs restricted to
// those leaves, so a later leaf's stub is never run), or, when final, one run of
// ./... with no restriction. Every step goes through the checker (the runner
// and its sandbox). Afterwards nothing may be dirty: anything that is was
// written by a check, is removed and fails the wave (stray_write).
//
// A harness fault or a cancelled check is an error and never a failure of a
// leaf. Results carry classes, names and locations only; a secret value in a
// name, a package or a file is replaced before the result leaves this function.
func (rc *runCtx) waveChecks(ctx context.Context, w int, final bool) (checkResult, error) {
	snap, err := TakeSnapshot(rc.o.Repo, rc.git)
	if err != nil {
		return checkResult{}, err
	}
	node := fmt.Sprintf("wave-%d", w)
	res, cerr := rc.runChecks(ctx, node, final)

	stray, err := snap.Stray(rc.o.Repo, rc.git)
	if err != nil {
		return checkResult{}, err
	}
	if len(stray) > 0 {
		if err := Revert(rc.git, stray); err != nil {
			return checkResult{}, fmt.Errorf("executor: removing the files the checks of wave %d wrote failed", w)
		}
		shown := make([]string, len(stray))
		for i, p := range stray {
			shown[i] = rc.scrubText(p)
		}
		rc.emit("stray_write", node, fmt.Sprintf("%d files written by the wave checks were removed", len(stray)))
		return checkResult{}, &stopError{Status: "failed", Reason: "stray_write",
			Message: fmt.Sprintf("executor: the checks of wave %d wrote files outside the plan (removed): %s", w, strings.Join(shown, ", "))}
	}
	if cerr != nil {
		return checkResult{}, cerr
	}
	if res.Pass() {
		rc.emit("wave_check", node, "pass")
	} else {
		kinds := make([]string, len(res.Failures))
		for i, f := range res.Failures {
			kinds[i] = f.Kind
		}
		rc.emit("wave_check", node, "fail: "+strings.Join(kinds, ", "))
	}
	return res, nil
}

func (rc *runCtx) runChecks(ctx context.Context, node string, final bool) (checkResult, error) {
	env, err := rc.env(node, envLeaf)
	if err != nil {
		return checkResult{}, errors.New("executor: the environment of the wave checks could not be built")
	}
	bv := rc.chk.BuildVet(ctx, rc.o.Repo, env)
	if !bv.Pass() {
		f, err := rc.failureOf(ctx, bv, "")
		if err != nil {
			return checkResult{}, err
		}
		if f.Kind != runner.ClassBuild && f.Kind != runner.ClassVet {
			return checkResult{}, errors.New("executor: go build and go vet returned an unexpected class")
		}
		return checkResult{Failures: []checkFailure{rc.scrubFailure(f)}}, nil
	}
	if ctx.Err() != nil {
		return checkResult{}, interruptStop("")
	}

	if final {
		v := rc.chk.Test(ctx, runner.TestSet{Repo: rc.o.Repo, Pkg: "./...", Race: true, Env: env, Timeout: rc.testTimeout()})
		if v.Pass() {
			return checkResult{}, nil
		}
		fs, err := rc.moduleFailures(ctx, v)
		if err != nil {
			return checkResult{}, err
		}
		return checkResult{Failures: fs}, nil
	}

	verified, err := rc.verifiedTestsCtx(ctx)
	if err != nil {
		return checkResult{}, err
	}
	dirs := make([]string, 0, len(verified))
	for d := range verified {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var out checkResult
	for _, d := range dirs {
		v := rc.chk.Test(ctx, runner.TestSet{
			Repo: rc.o.Repo, Pkg: pkgPattern(d), Funcs: verified[d], Race: true, Env: env, Timeout: rc.testTimeout(),
		})
		if v.Pass() {
			continue
		}
		f, err := rc.failureOf(ctx, v, d)
		if err != nil {
			return checkResult{}, err
		}
		out.Failures = append(out.Failures, rc.scrubFailure(f))
	}
	return out, nil
}

// pkgPattern is the go package argument of a repo-relative directory.
func pkgPattern(dir string) string {
	if dir == "." || dir == "" {
		return "."
	}
	return "./" + dir
}

// failureOf turns a failing verdict into a checkFailure, or says why it is
// not one: a cancelled check is an interruption and any harness class is a
// fault, neither attributable to a leaf.
func (rc *runCtx) failureOf(ctx context.Context, v runner.Verdict, dir string) (checkFailure, error) {
	switch v.Class {
	case runner.ClassBuild, runner.ClassVet, runner.ClassTestFail, runner.ClassTestPanic, runner.ClassTestTimeout, runner.ClassNoTestsRan:
		return checkFailure{Kind: v.Class, Dir: dir, Names: append([]string(nil), v.Names...), Locations: append([]runner.Location(nil), v.Locations...)}, nil
	case runner.ClassCancelled:
		return checkFailure{}, interruptStop("")
	}
	if ctx.Err() != nil {
		return checkFailure{}, interruptStop("")
	}
	return checkFailure{}, errors.New("executor: a wave check could not run (a fault of the harness, not of a leaf)")
}

var qnameRE = regexp.MustCompile(`^(.*?)\.((?:Test|Example|Benchmark|Fuzz)[A-Za-z0-9_]*(?:/.*)?)$`)

// moduleFailures splits the failure of a ./... run by package, using the
// package-qualified names the runner reports. Locations go to the package that
// holds their file; the rest stay in one failure with no package. With no
// qualified name the whole verdict is one failure.
func (rc *runCtx) moduleFailures(ctx context.Context, v runner.Verdict) ([]checkFailure, error) {
	base, err := rc.failureOf(ctx, v, "")
	if err != nil {
		return nil, err
	}
	if len(v.QNames) == 0 {
		return []checkFailure{rc.scrubFailure(base)}, nil
	}
	names := map[string][]string{}
	for _, q := range v.QNames {
		dir, name := "", q
		if m := qnameRE.FindStringSubmatch(q); m != nil {
			dir, name = rc.dirOfPackage(m[1]), m[2]
		}
		names[dir] = append(names[dir], name)
	}
	dirs := make([]string, 0, len(names))
	for d := range names {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	var out []checkFailure
	used := map[runner.Location]bool{}
	for _, d := range dirs {
		f := checkFailure{Kind: v.Class, Dir: d, Names: names[d]}
		for _, l := range v.Locations {
			if d != "" && path.Dir(l.File) == d {
				f.Locations = append(f.Locations, l)
				used[l] = true
			}
		}
		out = append(out, rc.scrubFailure(f))
	}
	var rest []runner.Location
	for _, l := range v.Locations {
		if !used[l] {
			rest = append(rest, l)
		}
	}
	if len(rest) > 0 {
		out = append(out, rc.scrubFailure(checkFailure{Kind: v.Class, Locations: rest}))
	}
	return out, nil
}

// dirOfPackage maps an import path to a repo-relative directory. The runner
// trims a long package from the left, so a path that is the tail of a known
// package matches it too.
func (rc *runCtx) dirOfPackage(pkg string) string {
	module := rc.plan.Contracts.Module
	if pkg == module {
		return "."
	}
	if rest, ok := strings.CutPrefix(pkg, module+"/"); ok {
		return rest
	}
	for _, l := range rc.plan.Leaves {
		full := module + "/" + l.Dir
		if l.Dir == "." {
			full = module
		}
		if pkg != "" && strings.HasSuffix(full, pkg) {
			return l.Dir
		}
	}
	return pkg
}

// scrubText is s, or the redaction mark when it holds a secret value.
func (rc *runCtx) scrubText(s string) string {
	if packer.HasSecret(s, rc.secretValues()) {
		return redacted
	}
	return s
}

// scrubFailure replaces every secret-bearing package, test name and file of f
// (in the packer's forms) so none reaches an event, a store or a prompt.
func (rc *runCtx) scrubFailure(f checkFailure) checkFailure {
	secrets := rc.secretValues()
	if len(secrets) == 0 {
		return f
	}
	f.Dir = rc.scrubText(f.Dir)
	names := make([]string, len(f.Names))
	for i, n := range f.Names {
		names[i] = n
		if packer.HasSecret(n, secrets) {
			names[i] = "(unnamed)"
		}
	}
	f.Names = names
	locs := make([]runner.Location, len(f.Locations))
	for i, l := range f.Locations {
		if packer.HasSecret(l.File, secrets) {
			l.File = redacted
		}
		locs[i] = l
	}
	f.Locations = locs
	return f
}
