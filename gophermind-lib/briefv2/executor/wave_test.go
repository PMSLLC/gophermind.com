package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/runner"
)

// waveRC is a started run whose five leaves all pass at once, over a fake
// checker whose repository-wide checks pass unless a test scripts them.
func (g *rig) waveRC(t *testing.T) (*runCtx, *fakeChecker) {
	t.Helper()
	script := Script{}
	for _, l := range g.plan.Leaves {
		script["implement:"+l.ID] = []step{reply(good(l.ID))}
	}
	rc, fc := g.schedRC(t, script)
	for _, l := range g.plan.Leaves {
		fc.LeafScript[l.ID] = []runner.Verdict{passVerdict()}
	}
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	return rc, fc
}

func (fc *fakeChecker) tests() []repoCall {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([]repoCall(nil), fc.testCalls...)
}

func (fc *fakeChecker) vets() int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.buildVets
}

func runWaves(t *testing.T, rc *runCtx, upTo int) {
	t.Helper()
	for _, w := range rc.waves() {
		if w > upTo {
			return
		}
		stop, err := rc.runWave(context.Background(), w)
		if err != nil || stop != nil {
			t.Fatalf("runWave(%d) = %+v, %v", w, stop, err)
		}
	}
}

func TestWaveOrder(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	var atCheck []int
	fc.RepoHook = func(string) { atCheck = append(atCheck, len(implementOrder(g))) }
	if got := rc.waves(); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("waves = %v", got)
	}
	runWaves(t, rc, 2)
	if got := implementOrder(g); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello", "fn-serve"}) {
		t.Fatalf("requests arrived in order %v", got)
	}
	// Each wave's check ran after that wave's leaves and before the next wave's first request.
	if !reflect.DeepEqual(atCheck, []int{2, 4, 5}) {
		t.Fatalf("implement requests seen at each check = %v, want [2 4 5]", atCheck)
	}
}

func TestWaveRacePerPackage(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	runWaves(t, rc, 2)
	tf := func(id string) string { return g.plan.Leaf(id).TestFunc }
	greet := sorted([]string{tf("fn-farewell"), tf("fn-greet")})
	cmd := sorted([]string{tf("fn-bye"), tf("fn-hello")})
	// Wave 1 holds two packages, in sorted directory order: cmd/greeter first.
	want := []repoCall{
		{Pkg: "./internal/greet", Funcs: greet, Race: true},
		{Pkg: "./cmd/greeter", Funcs: cmd, Race: true},
		{Pkg: "./internal/greet", Funcs: greet, Race: true},
		{Pkg: "./...", Funcs: nil, Race: true},
	}
	got := fc.tests()
	if len(got) != len(want) {
		t.Fatalf("Test calls = %+v", got)
	}
	for i := range want {
		if len(got[i].Funcs) == 0 {
			got[i].Funcs = nil
		}
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("call %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, c := range got {
		for _, f := range c.Funcs {
			if f == tf("fn-serve") {
				t.Errorf("a still-stubbed leaf's test %s was run in %s", f, c.Pkg)
			}
		}
	}
}

func TestWaveChecksAttribution(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	w0 := g.leaves("fn-farewell", "fn-greet")
	if wr, err := rc.scheduleLeaves(context.Background(), w0); err != nil || wr.Stop != nil || len(wr.Verified) != 2 {
		t.Fatalf("wave 0 = %+v, %v", wr, err)
	}
	a, b := g.plan.Leaf("fn-greet"), g.plan.Leaf("fn-farewell")

	cases := []struct {
		name  string
		setup func()
		kind  string
		want  string
		tests int // Test calls made
	}{
		{"build failure in a leaf file", func() {
			fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassBuild, Locations: []runner.Location{loc(a.File, 4)}, Out: outputOf("CANARY-compiler-text")}}
		}, runner.ClassBuild, a.ID, 0},
		{"vet failure in a leaf file", func() {
			fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassVet, Locations: []runner.Location{loc(b.File, 2)}}}
		}, runner.ClassVet, b.ID, 0},
		{"test failure of one leaf", func() {
			fc.RepoScript.Test["./"+a.Dir] = []runner.Verdict{{Class: runner.ClassTestFail, Names: []string{a.TestFunc + "/empty"}}}
		}, runner.ClassTestFail, a.ID, 1},
	}
	for _, tc := range cases {
		before := len(fc.tests())
		tc.setup()
		res, err := rc.waveChecks(context.Background(), 0, false)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.Pass() || len(res.Failures) != 1 || res.Failures[0].Kind != tc.kind {
			t.Fatalf("%s: result = %+v", tc.name, res)
		}
		att := g.plan.Attribute(res)
		if !reflect.DeepEqual(att.Nodes, []string{tc.want}) || len(att.Unattributed) != 0 {
			t.Errorf("%s: attribution = %+v, want only %s", tc.name, att, tc.want)
		}
		if n := len(fc.tests()) - before; n < tc.tests {
			t.Errorf("%s: %d Test calls, want at least %d", tc.name, n, tc.tests)
		}
		if tc.tests == 0 && len(fc.tests()) != before {
			t.Errorf("%s: tests ran after a build or vet failure", tc.name)
		}
	}
	for _, id := range []string{"fn-greet", "fn-farewell"} {
		if r := g.row(t, id); r.Status != blackboard.StatusVerified {
			t.Errorf("%s = %s: a check must not change a verified leaf", id, r.Status)
		}
	}
	if hasCanary(t, g, "CANARY-compiler-text") {
		t.Error("compiler output reached a store")
	}
}

func TestUnattributableStopsRun(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassBuild, Locations: []runner.Location{loc("go.mod", 3)}, Out: outputOf("CANARY-compiler-text")}}
	stop, err := rc.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "failed" || stop.Reason != "unattributable" {
		t.Fatalf("runWave = %+v, %v", stop, err)
	}
	if !strings.Contains(stop.Message, "go.mod:3") || strings.Contains(stop.Message, "CANARY") {
		t.Errorf("message = %q", stop.Message)
	}
	before := len(implementOrder(g))
	if before != 2 || len(fc.tests()) != 0 {
		t.Fatalf("requests %d, tests %d", before, len(fc.tests()))
	}
	// No repair round ran: nothing was reopened.
	for _, id := range []string{"fn-farewell", "fn-greet"} {
		if r := g.row(t, id); r.Status != blackboard.StatusVerified {
			t.Errorf("%s = %s", id, r.Status)
		}
	}
	if hasCanary(t, g, "CANARY-compiler-text") {
		t.Error("compiler output reached a store")
	}
}

func TestStrayFileFailsWave(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	fc.RepoHook = func(repo string) {
		_ = os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("STRAY-CONTENT"), 0o644)
	}
	stop, err := rc.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "failed" || stop.Reason != "stray_write" {
		t.Fatalf("runWave = %+v, %v", stop, err)
	}
	if !strings.Contains(stop.Message, "stray.txt") || strings.Contains(stop.Message, "STRAY-CONTENT") {
		t.Errorf("message = %q", stop.Message)
	}
	if _, err := os.Lstat(filepath.Join(g.repo, "stray.txt")); !os.IsNotExist(err) {
		t.Error("the stray file was not removed")
	}
	if hasCanary(t, g, "STRAY-CONTENT") {
		t.Error("the stray file's content reached a store")
	}
}

// A wave with a blocked leaf does not run the integration checks: it names
// the leaf and its blocker and the run stops failed, never verified.
func TestBlockedWaveStopsWithoutChecks(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    distinct("fn-greet", 2),
		"implement:fn-bye":      {reply(good("fn-bye"))},
	})
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	failing(fc, "fn-greet", 2)
	fc.LeafScript["fn-bye"] = []runner.Verdict{passVerdict()}
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}

	if stop, err := rc.runWave(context.Background(), 0); err != nil || stop != nil {
		t.Fatalf("wave 0 = %+v, %v", stop, err)
	}
	checksAfter0 := fc.vets()
	stop, err := rc.runWave(context.Background(), 1)
	if err != nil || stop == nil || stop.Status != "failed" || stop.Reason != "blocked" {
		t.Fatalf("wave 1 = %+v, %v", stop, err)
	}
	if !strings.Contains(stop.Message, "fn-hello") || !strings.Contains(stop.Message, "fn-greet") {
		t.Errorf("message = %q", stop.Message)
	}
	if fc.vets() != checksAfter0 {
		t.Error("the integration checks ran for a wave with a blocked leaf")
	}
	if rc.rep.blocked["fn-hello"] != "fn-greet" {
		t.Errorf("report blocked = %v", rc.rep.blocked)
	}
}

// Nothing blocked but the last wave holds a skipped leaf: the checks run over
// what is verified, and the run still cannot end verified.
func TestLastWaveWithUnverifiedLeafNeverPasses(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0))
	script := Script{"implement:fn-serve": distinct("fn-serve", 2)}
	for _, id := range []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello"} {
		script["implement:"+id] = []step{reply(good(id))}
	}
	rc, fc := g.schedRC(t, script)
	fc.RepoScript = &repoScript{Test: map[string][]runner.Verdict{}}
	for _, id := range []string{"fn-farewell", "fn-greet", "fn-bye", "fn-hello"} {
		fc.LeafScript[id] = []runner.Verdict{passVerdict()}
	}
	failing(fc, "fn-serve", 2)
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	runWaves(t, rc, 1)
	stop, err := rc.runWave(context.Background(), 2)
	if err != nil || stop == nil || stop.Status != "failed" || stop.Reason != "leaves_not_verified" {
		t.Fatalf("wave 2 = %+v, %v", stop, err)
	}
	if !strings.Contains(stop.Message, "fn-serve") {
		t.Errorf("message = %q", stop.Message)
	}
	calls := fc.tests()
	if last := calls[len(calls)-1]; last.Pkg == "./..." {
		t.Error("the whole-module test ran with a leaf still a stub")
	}
}

// A harness fault or a cancelled check is never attributed to a leaf.
func TestHarnessFaultNotAttributed(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassHarness, Err: errors.New("sandbox refused")}}
	stop, err := rc.runWave(context.Background(), 0)
	if stop != nil || err == nil {
		t.Fatalf("a harness fault = %+v, %v, want a plain error", stop, err)
	}
	var se *stopError
	if errors.As(err, &se) {
		t.Errorf("a harness fault became a stop: %+v", se)
	}

	g2 := newRig(t)
	rc2, fc2 := g2.waveRC(t)
	fc2.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassCancelled, Err: context.Canceled}}
	stop, err = rc2.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "interrupted" {
		t.Fatalf("a cancelled check = %+v, %v", stop, err)
	}
	for _, id := range []string{"fn-farewell", "fn-greet"} {
		if r := g2.row(t, id); r.Status != blackboard.StatusVerified {
			t.Errorf("%s = %s", id, r.Status)
		}
	}
}

// Secrets in a package name, a test name, a file name or output never reach
// the result, the attribution or a store.
func TestWaveCheckScrubsSecrets(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	if wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet")); err != nil || wr.Stop != nil {
		t.Fatalf("wave 0 = %+v, %v", wr, err)
	}
	a := g.plan.Leaf("fn-greet")
	fc.RepoScript.Test["./..."] = []runner.Verdict{{
		Class: runner.ClassTestFail,
		QNames: []string{
			"example.com/greeter/internal/pkg_" + canarySecret + "." + a.TestFunc,
			"example.com/greeter/internal/greet.TestName_" + canarySecret,
		},
		Locations: []runner.Location{loc("internal/"+canarySecret+"/x.go", 9)},
		Out:       outputOf("compiler says " + canarySecret),
	}}
	res, err := rc.waveChecks(context.Background(), 0, true)
	if err != nil || res.Pass() {
		t.Fatalf("waveChecks = %+v, %v", res, err)
	}
	att := g.plan.Attribute(res)
	blob := fmt.Sprintf("%+v %+v", res, att)
	if strings.Contains(blob, canarySecret) {
		t.Fatalf("the secret is in the result: %s", blob)
	}
	if len(att.Unattributed) == 0 {
		t.Errorf("unattributed = %v, want the scrubbed records unattributable", att.Unattributed)
	}
	if hasCanary(t, g, canarySecret) {
		t.Error("the secret reached a store")
	}
}

func TestVerifiedTestsOnlyVerified(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.waveRC(t)
	if got := rc.verifiedTests(); len(got) != 0 {
		t.Fatalf("verifiedTests before any leaf = %v", got)
	}
	if wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-greet")); err != nil || len(wr.Verified) != 1 {
		t.Fatalf("%+v %v", wr, err)
	}
	want := map[string][]string{"internal/greet": {g.plan.Leaf("fn-greet").TestFunc}}
	if got := rc.verifiedTests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("verifiedTests = %v, want %v", got, want)
	}
}

// An interrupted check is reported as interrupted even when it left a stray
// file behind: the stray file is removed, the stop stays the interruption.
func TestInterruptedNotMaskedByStray(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	fc.RepoScript.BuildVet = []runner.Verdict{{Class: runner.ClassCancelled, Err: context.Canceled}}
	fc.RepoHook = func(repo string) {
		_ = os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644)
	}
	stop, err := rc.runWave(context.Background(), 0)
	if err != nil || stop == nil || stop.Status != "interrupted" {
		t.Fatalf("runWave = %+v, %v, want interrupted", stop, err)
	}
	if _, err := os.Lstat(filepath.Join(g.repo, "stray.txt")); !os.IsNotExist(err) {
		t.Error("the stray file was not removed")
	}
}

// The package of a qualified test name ends at the last ".Test" boundary that
// names a known package: a module path or a subtest name holding ".Test" does
// not move it.
func TestModuleFailuresSplitAtLastTestBoundary(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.waveRC(t)
	rc.plan.Contracts.Module = "example.com/My.Testing"
	a := g.plan.Leaf("fn-greet")
	v := runner.Verdict{Class: runner.ClassTestFail, QNames: []string{
		"example.com/My.Testing/" + a.Dir + "." + a.TestFunc + "/sub.TestB",
		"example.com/My.Testing/" + a.Dir + ".TestOther",
	}}
	fs, err := rc.moduleFailures(context.Background(), v)
	if err != nil || len(fs) != 1 {
		t.Fatalf("moduleFailures = %+v, %v", fs, err)
	}
	if fs[0].Dir != a.Dir || !reflect.DeepEqual(fs[0].Names, []string{a.TestFunc + "/sub.TestB", "TestOther"}) {
		t.Errorf("failure = %+v, want dir %s and both names", fs[0], a.Dir)
	}
}

// A secret split with whitespace in a package name, a test name, a file name or
// the output is caught as well.
func TestWaveCheckScrubsSplitSecrets(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.waveRC(t)
	if wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet")); err != nil || wr.Stop != nil {
		t.Fatalf("wave 0 = %+v, %v", wr, err)
	}
	split := "CANARY-SEC RET-VALUE"
	fc.RepoScript.Test["./..."] = []runner.Verdict{{
		Class: runner.ClassTestFail,
		QNames: []string{
			"example.com/greeter/internal/pkg_" + split + ".TestX",
			"example.com/greeter/internal/greet.TestName_" + split,
		},
		Locations: []runner.Location{loc("internal/"+split+"/x.go", 9)},
		Out:       outputOf("compiler says CANARY-SEC\nRET-VALUE"),
	}}
	res, err := rc.waveChecks(context.Background(), 0, true)
	if err != nil || res.Pass() {
		t.Fatalf("waveChecks = %+v, %v", res, err)
	}
	blob := fmt.Sprintf("%+v %+v", res, g.plan.Attribute(res))
	if strings.Contains(blob, "CANARY-SEC RET") || strings.Contains(blob, "SEC\nRET") {
		t.Fatalf("the split secret is in the result: %s", blob)
	}
}
