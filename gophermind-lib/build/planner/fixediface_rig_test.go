package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// The repair reply to the fixed-interface pass still lacks Run: the stage
// stops with the error.
func TestSlugifyFixedInterfaceStillMissingStopsTheStage(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join(slugify, "contract.cli-logic.txt"))
	if err != nil {
		t.Fatal(err)
	}
	g := newRigFor(t, approving(), slugify, slugifyID,
		variant(t, map[string]string{"contract.cli-logic.2.txt": string(recorded)}))
	_, err = g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "does not declare the fixed interface") {
		t.Fatalf("plan error = %v, want the fixed interface error", err)
	}
}

// A repair reply that writes Run with other parameter names gets the brief's
// signature and a fixed_interface_applied warning.
func TestSlugifyFixedInterfaceAppliedIsWarned(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID,
		variant(t, map[string]string{"contract.cli-logic.2.txt": `{"types": [], "functions": [{"id": "fn-cli-run", "package": "internal/cli", "file": "internal/cli/run.go",` +
			` "signature": "func Run(args []string, in io.Reader, out, errw io.Writer) int", "doc": "Runs the command and returns the exit code.",` +
			` "uses": ["fn-parse-args"]}], "more": false}`}))
	_, _ = g.plan(planner.Options{StopAfter: "contract"})
	for _, w := range g.sink.OfKind("warning") {
		if strings.HasPrefix(w.Message, "fixed_interface_applied: ") &&
			strings.Contains(w.Message, "fn-cli-run: func Run(args []string, in io.Reader, out, errw io.Writer) int -> func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int") {
			return
		}
	}
	t.Errorf("no fixed_interface_applied warning for fn-cli-run; warnings = %v", g.sink.OfKind("warning"))
}

// promptsOf is the user prompt of every request of stage, in order.
func promptsOf(g *rig, stage string) []string {
	var out []string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == stage {
			out = append(out, r.Messages[1].Content)
		}
	}
	return out
}

// Every component, not only the one under cmd/, is told that main calls the
// fixed entry call, so none of them is asked for a run function.
func TestComponentPromptNamesTheFixedEntryCallForEveryComponent(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID)
	_, _ = g.plan(planner.Options{StopAfter: "contract"})
	prompts := promptsOf(g, "contract:slug-logic")
	if len(prompts) == 0 {
		t.Fatal("no contract:slug-logic request")
	}
	if p := prompts[0]; !strings.Contains(p, "with the result of `cli.Run`") || strings.Contains(p, "`run` has the signature") {
		t.Errorf("slug-logic prompt does not name the fixed entry call:\n%s", p)
	}
}

// A lowercase run in internal/cli is renamed to Run before the surface check,
// so the surface repair sees it and asks for its doc again.
func TestSlugifyFixedRenameComesBeforeTheSurfaceCheck(t *testing.T) {
	g := newRigFor(t, approving(), slugify, slugifyID,
		variant(t, map[string]string{"contract.cli-logic.2.txt": `{"types": [], "functions": [{"id": "fn-run", "package": "internal/cli", "file": "internal/cli/run.go",` +
			` "signature": "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", "doc": "Runs the command and returns the exit code.",` +
			` "uses": ["fn-parse-args"]}], "more": false}`}))
	_, _ = g.plan(planner.Options{StopAfter: "contract"})
	prompts := promptsOf(g, "contract:cli-logic")
	if len(prompts) < 3 || !strings.Contains(prompts[2], "the doc of fn-run does not state these items of the command-line surface") {
		t.Errorf("no surface repair pass for the renamed fn-run; %d cli-logic passes", len(prompts))
	}
}
