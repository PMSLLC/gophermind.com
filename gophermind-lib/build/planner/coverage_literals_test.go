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

func TestCoverageLiteralsSkipGoTypes(t *testing.T) {
	q := Requirement{ID: "F2", Kind: ReqFeature, Text: "`Farewell(name)` returns `Goodbye, <name>!`.\n\n- An empty name returns a `*NameError`, and a closed reader returns `io.EOF` or `ErrClosed`."}
	if got, want := requirementLiterals(q), []string{"Goodbye, <name>!"}; !reflect.DeepEqual(got, want) {
		t.Errorf("literals = %q, want %q", got, want)
	}
}

// Run hello-v1.1-20261106-r007: the brief wrote the pattern Hello, <name>! and
// three examples of it; the doc wrote the pattern. An example of a carried
// pattern is carried, and a missing pattern is one gap line, not one per
// example.
func TestLiteralExamplesOfACarriedPatternAreCarried(t *testing.T) {
	f1 := Requirement{ID: "F1", Kind: ReqFeature, Text: "For any non-empty name, Greeting returns exactly `Hello, <name>!` with the name inserted unchanged: " +
		"`Greeting(\"Ada\")` returns `Hello, Ada!`, `Greeting(\"A B\")` returns `Hello, A B!`, and `Greeting(\" Alice \")` returns `Hello,  Alice !`."}
	nodes := []PlanNode{{ID: "fn-greeting", Kind: tree.KindFunction, Parent: "greet", Title: "Greeting",
		Text: "returns 'Hello, <name>!' and 'Hello, World!'"}}
	covered := []Covered{{Requirement: "F1", Nodes: []string{"fn-greeting"}}}
	if gaps := literalGaps([]Requirement{f1}, covered, nodes); len(gaps) != 0 {
		t.Errorf("pattern carried: gaps = %+v, want none", gaps)
	}
	// Hello, World! is itself an instance of Hello, <name>! under literalIn's
	// placeholder rule, so the missing case uses a text that carries neither.
	nodes[0].Text = "returns a greeting"
	gaps := literalGaps([]Requirement{f1}, covered, nodes)
	if len(gaps) != 1 || gaps[0].Reason != "behaviour missing: Hello, <name>!" {
		t.Errorf("pattern missing: gaps = %+v, want one gap naming only Hello, <name>!", gaps)
	}
	// Hello, World! fills the placeholder with text the brief names nowhere
	// else: a behaviour of its own, not carried by the pattern.
	f1.Text += " An empty name returns `Hello, World!`."
	nodes[0].Text = "returns 'Hello, <name>!'"
	gaps = literalGaps([]Requirement{f1}, covered, nodes)
	if len(gaps) != 1 || gaps[0].Reason != "behaviour missing: Hello, World!" {
		t.Errorf("empty name: gaps = %+v, want one gap naming only Hello, World!", gaps)
	}
}

// The brief writes a trailing newline as the escape \n; a doc writes it as a
// real newline or not at all.
func TestLiteralNewlineEscapesMatchDocNewlines(t *testing.T) {
	if !literalIn("Writes 'usage: hello [NAME]\n' to stderr", `usage: hello [NAME]\n`) {
		t.Error("escaped newline not matched by a real one")
	}
	if !literalIn("writes usage: hello [NAME] to stderr", `usage: hello [NAME]\n`) {
		t.Error("escaped trailing newline not matched by none")
	}
	if literalIn("writes nothing", `usage: hello [NAME]\n`) {
		t.Error("matched a text without the literal")
	}
}

// helloF2 is the F2 text of run hello-v1.1-20261106-r007.
const helloF2 = "With zero arguments `Run` writes exactly `Hello, World!\\n` to `stdout` and returns `0`; with `Ada` it writes `Hello, Ada!\\n` to `stdout`. " +
	"With two or more arguments it writes exactly `usage: hello [NAME]\\n` to `stderr`, and returns `2`."

func TestLiteralInputsAreNotResults(t *testing.T) {
	got := requirementLiterals(Requirement{ID: "F2", Kind: ReqFeature, Text: helloF2})
	for _, want := range []string{`Hello, Ada!\n`, `usage: hello [NAME]\n`, "2"} {
		if !contains(got, want) {
			t.Errorf("literals = %q, missing %q", got, want)
		}
	}
	if contains(got, "Ada") {
		t.Errorf("literals = %q, the input Ada taken as a result", got)
	}
}

// The behaviour of a node includes what the nodes it uses say they produce.
func TestLiteralsOfUsedNodesCount(t *testing.T) {
	f1 := Requirement{ID: "F1", Kind: ReqFeature, Text: "Greeting returns `Hello, <name>!`."}
	f2 := Requirement{ID: "F2", Kind: ReqFeature, Text: helloF2}
	nodes := []PlanNode{
		{ID: "fn-run", Kind: tree.KindFunction, Parent: "cli", Title: "Run", Uses: []string{"fn-greeting"},
			Text: "Run writes the greeting to stdout and returns 0; on misuse it writes 'usage: hello [NAME]' to stderr and returns 2."},
		{ID: "fn-greeting", Kind: tree.KindFunction, Parent: "greet", Title: "Greeting", Uses: []string{"fn-run"},
			Text: "returns 'Hello, <name>!', or 'Hello, World!' for an empty name"},
	}
	covered := []Covered{{Requirement: "F2", Nodes: []string{"fn-run"}}}
	reqs := []Requirement{f1, f2}
	if gaps := literalGaps(reqs, covered, nodes); len(gaps) != 0 {
		t.Errorf("with uses: gaps = %+v, want none", gaps)
	}
	nodes[0].Uses = nil
	gaps := literalGaps(reqs, covered, nodes)
	if len(gaps) != 1 || !strings.Contains(gaps[0].Reason, `behaviour missing: Hello, World!\n`) {
		t.Errorf("without uses: gaps = %+v, want one F2 gap naming Hello, World!\\n", gaps)
	}
}
