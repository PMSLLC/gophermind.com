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
