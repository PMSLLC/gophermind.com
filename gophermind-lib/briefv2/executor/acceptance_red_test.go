package executor

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/planner"
)

func withRed(o *Options, _ *orderLog) { o.noRedCheck = false }

func redEvents(g *rig) (red, notRed int) {
	for _, e := range g.sink.OfKind("acceptance_red") {
		if strings.Contains(e.Message, "fails against") {
			red++
		} else {
			notRed++
		}
	}
	return
}

// A good bullet is red against the stubs and green once the leaves are built.
func TestAcceptanceRedThenGreen(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, withRed)
	if err != nil || rep.Status != "verified" || rep.Acceptance.Passed != 2 {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if red, notRed := redEvents(g); red != 2 || notRed != 0 {
		t.Errorf("red checks: %d red, %d not red", red, notRed)
	}
}

// A root test that passes against the stubs proves nothing: the run stops at
// Wave 0, before any model call, naming that bullet only.
func TestAcceptanceRedRefusesABulletThatPassesOnStubs(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	editCoverage(t, g.runDir, func(c *planner.CoverageFile) {
		for i := range c.RootTests {
			if c.RootTests[i].Requirement == "A1" {
				c.RootTests[i].Command = `curl -s -m 1 "$GM_ACCEPTANCE_URL" >/dev/null 2>&1; true`
			}
		}
	})
	g.wire(goodScript(g))
	o := g.options()
	o.noRedCheck = false
	rep, err := run(testCtx(), o, runFlags{skipAcceptance: true})
	if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_not_red" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	msg := strings.Join(rep.Failures, " ")
	if !strings.Contains(msg, "A1") || strings.Contains(msg, "A2") || strings.Contains(msg, "curl") {
		t.Errorf("failures %v must name A1 only and no command", rep.Failures)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Errorf("%d model calls before the stop", n)
	}
	if red, notRed := redEvents(g); red != 1 || notRed != 1 {
		t.Errorf("red checks: %d red, %d not red", red, notRed)
	}
}

// Commands that only check the code are exempt: they pass on the stubs.
func TestAcceptanceRedExemptsStaticChecks(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	edit := func(rc *runCtx, h *hybridChecker) {}
	editCoverage(t, g.runDir, func(c *planner.CoverageFile) {
		for i := range c.RootTests {
			switch c.RootTests[i].Requirement {
			case "A1":
				c.RootTests[i].Command = "go build ./..."
			case "A2":
				c.RootTests[i].Command = "go vet ./... && test -z \"$(gofmt -l .)\""
			}
		}
	})
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), edit, withRed)
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if red, notRed := redEvents(g); red != 0 || notRed != 0 {
		t.Errorf("a static check was run as a red check: %d, %d", red, notRed)
	}
}

// With a serve declaration the Go acceptance tests are red against the stubs
// (no server answers) and green against the built one.
func TestGoAcceptanceRedThenGreen(t *testing.T) {
	t.Parallel()
	g := goAcceptRig(t)
	rep, err, _ := g.accRun(t, goodScript(g), g.fastChecker(), nil, withRed)
	if err != nil || rep.Status != "verified" || rep.Acceptance.Passed != 2 {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	if red, notRed := redEvents(g); red != 2 || notRed != 0 {
		t.Errorf("red checks: %d red, %d not red", red, notRed)
	}
}

// A Go acceptance test that never talks to the server passes on the stubs and
// is refused.
func TestGoAcceptanceRedRefusesATestThatNeverCallsTheServer(t *testing.T) {
	t.Parallel()
	lazy := `{"test_file": "package acceptance\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestA1(t *testing.T) {\n\tif os.Getenv(\"GM_ACCEPTANCE_URL\") == \"\" {\n\t\tt.Fatal(\"GM_ACCEPTANCE_URL is not set\")\n\t}\n}\n"}`
	g := goAcceptRig(t, func(o *rigOpts) {
		o.PlannerDirs = append([]string{plannerVariant(t, map[string]string{"testwrite.accept-A1.txt": lazy})}, o.PlannerDirs...)
	})
	g.wire(goodScript(g))
	o := g.options()
	o.noRedCheck = false
	rep, err := run(testCtx(), o, runFlags{skipAcceptance: true})
	if err != nil || rep.Status != "failed" || rep.StopReason != "acceptance_not_red" {
		t.Fatalf("run = %s (%s), %v, %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	msg := strings.Join(rep.Failures, " ")
	if !strings.Contains(msg, "A1") || strings.Contains(msg, "A2") {
		t.Errorf("failures %v must name A1 only", rep.Failures)
	}
}

var _ = events.KindWarning
