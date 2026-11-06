package planner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/tree"
)

// helloFeatures is the hello fixture brief's feature requirements by name.
func helloFeatures(t *testing.T) map[string]Requirement {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "hello", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Requirement{}
	for _, q := range ParseRequirements(src) {
		if q.Kind == ReqFeature {
			out[q.Name] = q
		}
	}
	if len(out) != 2 {
		t.Fatalf("hello features = %v, want Say hello and Report errors", out)
	}
	return out
}

func TestCoverageLiteralsOfSayHello(t *testing.T) {
	f := helloFeatures(t)
	if got, want := requirementLiterals(f["Say hello"]), []string{"Hello, World!", "Hello, <NAME>!", "Hello, Ada!"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Say hello literals = %q, want %q", got, want)
	}
	if got, want := requirementLiterals(f["Report errors"]), []string{"usage: hello [NAME]", "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Report errors literals = %q, want %q", got, want)
	}
	// Only feature requirements state literals.
	if got := requirementLiterals(Requirement{ID: "A1", Kind: ReqAcceptance, Text: "`hello` prints `Hello, World!`."}); got != nil {
		t.Errorf("acceptance literals = %q, want none", got)
	}
}

func TestCoverageLiteralsSkipInvocations(t *testing.T) {
	f := helloFeatures(t)
	for _, name := range []string{"Say hello", "Report errors"} {
		for _, bins := range [][]string{nil, {"hello"}} {
			for _, lit := range requirementLiterals(f[name], bins...) {
				if lit == "hello Ada" || lit == "hello [NAME]" {
					t.Errorf("%s (binaries %v): invocation %q taken as a literal", name, bins, lit)
				}
			}
		}
	}
	// A span after the verb that starts with the binary name is an invocation.
	q := Requirement{ID: "F1", Kind: ReqFeature, Text: "The program prints `Hi, Bo!` when run as `hi Bo`."}
	if got, want := requirementLiterals(q, "hi"), []string{"Hi, Bo!"}; !reflect.DeepEqual(got, want) {
		t.Errorf("literals = %q, want %q", got, want)
	}
}

func TestRunWithoutGreetingFailsF1(t *testing.T) {
	f := helloFeatures(t)
	f1 := f["Say hello"]
	f1.ID = "F1"
	run := PlanNode{ID: "fn-run", Kind: tree.KindFunction, Parent: "cli", Title: "Run",
		Text: "Checks the argument count and prints the greeting to stdout.\n[map[name:code type:int]]"}
	nodes := []PlanNode{{ID: "hello", Kind: tree.KindRoot}, {ID: "cli", Kind: tree.KindComponent, Parent: "hello"}, run}
	covered := []Covered{{Requirement: "F1", Nodes: []string{"fn-run"}}}

	gaps := literalGaps([]Requirement{f1}, covered, nodes, "hello")
	if len(gaps) != 1 || gaps[0].Requirement != "F1" || !strings.HasPrefix(gaps[0].Reason, "behaviour missing: Hello, World!") {
		t.Fatalf("gaps = %+v, want one F1 gap starting behaviour missing: Hello, World!", gaps)
	}

	// The node that carries the greeting closes the gap; a placeholder
	// literal is carried by a format string.
	run.Text = `Prints Hello, World! with no argument, or Hello, %s! with the name; for example Hello, Ada!`
	nodes[2] = run
	if gaps := literalGaps([]Requirement{f1}, covered, nodes, "hello"); len(gaps) != 0 {
		t.Errorf("gaps = %+v, want none", gaps)
	}
	// A mapped component carries the text of its functions.
	covered[0].Nodes = []string{"cli"}
	if gaps := literalGaps([]Requirement{f1}, covered, nodes, "hello"); len(gaps) != 0 {
		t.Errorf("component mapping: gaps = %+v, want none", gaps)
	}
}

func TestExitCodeLiteralIsAWord(t *testing.T) {
	if literalIn("returns 12", "2") {
		t.Error("2 found inside 12")
	}
	if !literalIn("returns 2 on misuse", "2") {
		t.Error("2 not found as a word")
	}
}

func TestCoverageLiteralsComeAfterTheVerb(t *testing.T) {
	q := Requirement{ID: "F2", Kind: ReqFeature, Text: "- A `--max` value that `strconv.Atoi` rejects prints `slugify: invalid --max \"<value>\"` on standard error and exits with code 2.\n" +
		"`slugify [FILE]` reads FILE, or standard input when FILE is `-` or missing, and prints the slug."}
	if got, want := requirementLiterals(q, "slugify"), []string{`slugify: invalid --max "<value>"`, "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("literals = %q, want %q", got, want)
	}
}
