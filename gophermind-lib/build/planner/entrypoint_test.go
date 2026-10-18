package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// cliBrief adds an acceptance bullet that runs the tool by its name, so the
// greeter brief describes something to be run.
func cliBrief(s string) string {
	return s + "- `greeter hello.txt` prints a greeting for every name in the file.\n"
}

const entryComp = `{"id": "entry-point", "package": "cmd/greeter", "exports": []}`

const entryFns = `{"types": [], "functions": [
  {"id": "fn-main", "package": "cmd/greeter", "file": "cmd/greeter/main.go",
   "signature": "func main()", "doc": "main calls run and exits with its code.", "uses": ["fn-run"]},
  {"id": "fn-run", "package": "cmd/greeter", "file": "cmd/greeter/run.go",
   "signature": "func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", "doc": "run wires the feature functions and returns the exit code.", "uses": ["fn-greet"]}
], "more": false}`

func cliRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	g := newRig(t, approving(), variant(t, files))
	g.briefPath = writeBrief(t, g.repo, cliBrief)
	return g
}

func hasCmdPackage(g *rig) bool {
	for _, c := range g.contracts().Components {
		if strings.HasPrefix(c.Package, "cmd/") {
			return true
		}
	}
	return false
}

func entryCalls(g *rig) int { return count(g.stagesCalled(), "contract:outline:entry") }

func TestMissingEntryPointIsAskedForOnce(t *testing.T) {
	g := cliRig(t, map[string]string{
		"contract.outline.entry.txt": `{"components": [` + entryComp + `]}`,
		"contract.entry-point.txt":   entryFns,
	})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := entryCalls(g); n != 1 {
		t.Fatalf("entry pass calls = %d, want 1", n)
	}
	if !hasCmdPackage(g) {
		t.Fatal("the contract has no package under cmd/ after the entry pass")
	}
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:outline:entry" {
			p := r.Messages[1].Content
			if !strings.Contains(p, "cmd/greeter") {
				t.Errorf("the entry pass does not name the package:\n%s", p)
			}
		}
	}
}

func TestEntryPointPresentIsNotAskedFor(t *testing.T) {
	g := cliRig(t, map[string]string{
		"contract.outline.2.txt":   `{"components": [` + comp("greeting") + `, ` + comp("farewell") + `, ` + entryComp + `]}`,
		"contract.entry-point.txt": entryFns,
	})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := entryCalls(g); n != 0 {
		t.Fatalf("entry pass calls = %d, want none", n)
	}
	for _, r := range g.fake.Requests() {
		p := r.Messages[1].Content
		switch planner.StageOf(r) {
		case "contract:outline:1":
			if strings.Contains(p, "cmd/greeter") {
				t.Errorf("the shared pass must not write the entry point")
			}
		case "contract:outline:2":
			if !strings.Contains(p, "cmd/greeter") {
				t.Errorf("the last batch pass must ask for the entry point:\n%s", p)
			}
		}
	}
}

func TestLibraryBriefHasNoEntryPass(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := entryCalls(g); n != 0 || hasCmdPackage(g) {
		t.Fatalf("a library brief got an entry pass (%d) or a cmd package", n)
	}
	for _, r := range g.fake.Requests() {
		if strings.Contains(r.Messages[1].Content, "cmd/greeter") {
			t.Fatalf("stage %s mentions an entry point for a library brief", planner.StageOf(r))
		}
	}
}

func TestEntryPointStillMissingWarnsAndContinues(t *testing.T) {
	g := cliRig(t, map[string]string{"contract.outline.entry.txt": `{"components": []}`})
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := entryCalls(g); n != 2 {
		t.Fatalf("entry pass calls = %d, want 2 (the cap)", n)
	}
	warned := false
	for _, w := range g.sink.OfKind("warning") {
		if strings.Contains(w.Message, "outline_no_entry_point") {
			warned = true
		}
	}
	if !warned {
		t.Error("no outline_no_entry_point warning")
	}
}
