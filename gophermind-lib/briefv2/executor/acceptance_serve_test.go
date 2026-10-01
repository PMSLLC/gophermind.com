package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// plannerVariant is a fixture directory whose files go in front of the
// planner's greeter replies.
func plannerVariant(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}
	return dir
}

// goAcceptRig is the greeter rig planned with a serve declaration: both
// acceptance bullets are narrative, so the planner gave each a Go test
// (acceptance/a1_test.go, acceptance/a2_test.go) and the root test command that
// runs it.
func goAcceptRig(t *testing.T, mods ...func(*rigOpts)) *rig {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(greeterDir, "planner-accept"))
	if err != nil {
		t.Fatal(err)
	}
	return newRig(t, append([]func(*rigOpts){func(o *rigOpts) { o.PlannerDirs = []string{dir} }}, mods...)...)
}

func TestGoAcceptanceFixturePlan(t *testing.T) {
	t.Parallel()
	g := goAcceptRig(t)
	if g.plan.Serve == nil || g.plan.Serve.Ready != "/hello" {
		t.Fatalf("serve = %+v", g.plan.Serve)
	}
	if len(g.plan.AcceptTests) != 2 || g.plan.AcceptTests["A1"].TestFunc != "TestA1" || g.plan.AcceptTests["A2"].TestFile != "acceptance/a2_test.go" {
		t.Fatalf("manifest = %+v", g.plan.AcceptTests)
	}
	for _, id := range []string{"A1", "A2"} {
		if _, err := os.Stat(filepath.Join(g.repo, "acceptance", strings.ToLower(id)+"_test.go")); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for _, rt := range g.plan.Coverage.RootTests {
		if rt.Requirement == "A1" && rt.Command != "go test -tags acceptance ./acceptance -run '^TestA1$' -count=1 -v" {
			t.Errorf("A1 command = %q", rt.Command)
		}
	}
}

// The executor starts the declared server once, runs the Go tests against it
// and kills it: Acceptance passed 2 of 2, with the proof of each command.
func TestGoAcceptanceRunsAgainstTheDeclaredServer(t *testing.T) {
	t.Parallel()
	g := goAcceptRig(t)
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, nil)
	if err != nil || rep.Status != "verified" || rep.Acceptance.Passed != 2 || rep.Acceptance.Total != 2 {
		t.Fatalf("run = %s (%s) %d of %d, %v, %v", rep.Status, rep.StopReason, rep.Acceptance.Passed, rep.Acceptance.Total, rep.Failures, err)
	}
	f := readAcceptanceFile(t, g)
	if !f.Complete || len(f.Bullets) != 2 || len(f.Bullets[0].Commands) != 1 || !f.Bullets[0].Commands[0].Passed {
		t.Errorf("acceptance.json = %+v", f)
	}
}

func readAcceptanceFile(t *testing.T, g *rig) AcceptanceFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.runDir, "acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f AcceptanceFile
	if err := jsonUnmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGoAcceptanceServerProblemsStopTheRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		serve planner.Serve
		want  string
	}{
		{"never ready", planner.Serve{Command: `greeter --addr "$GM_ACCEPTANCE_ADDR"`, Ready: "/never"}, "not ready in time"},
		{"exits at once", planner.Serve{Command: `greeter --addr 256.256.256.256:1`, Ready: "/hello"}, "exited before it was ready"},
		{"runs no built binary", planner.Serve{Command: `true`, Ready: "/hello"}, "does not run a built binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := goAcceptRig(t, func(o *rigOpts) {
				o.Settings = func(cfg *settings.Config) { cfg.Executor.AcceptanceTimeoutSeconds = 2 }
			})
			edit := func(rc *runCtx, h *hybridChecker) { s := c.serve; rc.plan.Serve = &s }
			rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), edit, nil)
			if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_serve" {
				t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
			}
			if !strings.Contains(strings.Join(rep.Failures, " "), c.want) {
				t.Errorf("failures %v lack %q", rep.Failures, c.want)
			}
		})
	}
}

// A test file edited after planning stops the run before anything is built.
func TestGoAcceptanceTestFileIsHeldToItsHash(t *testing.T) {
	t.Parallel()
	g := goAcceptRig(t)
	p := filepath.Join(g.repo, "acceptance", "a2_test.go")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(raw, []byte("// edited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	g.wire(goodScript(g))
	rep, err := run(testCtx(), g.options(), runFlags{skipAcceptance: true})
	if err != nil || rep.Status != "failed" || rep.StopReason != "test_file_changed" {
		t.Fatalf("run = %s (%s), %v", rep.Status, rep.StopReason, err)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("%d model calls before the stop", n)
	}
}

func TestPlainTestOutputKeepsWhatTheTestPrinted(t *testing.T) {
	t.Parallel()
	in := `{"Action":"run","Test":"TestA1"}` + "\n" + `{"Action":"output","Test":"TestA1","Output":"    a1_test.go:9: status 500\n"}` + "\n" + "ok raw line\n"
	got := plainTestOutput(in)
	if !strings.Contains(got, "a1_test.go:9: status 500\n") || !strings.Contains(got, "ok raw line") || strings.Contains(got, `"Action"`) {
		t.Errorf("got %q", got)
	}
}

var _ = events.KindWarning

func testCtx() context.Context { return context.Background() }
