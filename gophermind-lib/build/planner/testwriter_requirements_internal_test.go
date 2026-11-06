package planner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helloReqs is the requirements of the hello fixture brief.
func helloReqs(t *testing.T) []Requirement {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "hello", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	return ParseRequirements(src)
}

// featureID is the id of the hello feature named name.
func featureID(t *testing.T, reqs []Requirement, name string) string {
	t.Helper()
	for _, q := range reqs {
		if q.Kind == ReqFeature && q.Name == name {
			return q.ID
		}
	}
	t.Fatalf("no feature %q", name)
	return ""
}

const runNode = `{"contract":{"signature":"func Run(args []string, stdout, stderr io.Writer) int"}}`

func TestTestwriterPromptCarriesBothFeaturesForRun(t *testing.T) {
	reqs := helloReqs(t)
	f1, f2 := featureID(t, reqs, "Say hello"), featureID(t, reqs, "Report errors")
	covered := []Covered{
		{Requirement: f1, Nodes: []string{"fn-greeting", "fn-run"}},
		{Requirement: f2, Nodes: []string{"cli"}}, // the component of Run
		{Requirement: "C1", Nodes: []string{"fn-greeting"}},
	}
	mapped := nodeRequirements(reqs, covered, "fn-run", "cli")
	if len(mapped) != 2 || mapped[0].ID != f1 || mapped[1].ID != f2 {
		t.Fatalf("mapped = %+v, want %s and %s", mapped, f1, f2)
	}
	got, err := renderTestwriterBrief(runNode, "hello", "internal/cli/cli.go", "TestRun", "internal/cli/fn_run_test.go", nil,
		"### Say hello", nil, requirementsBlock(mapped))
	if err != nil {
		t.Fatal(err)
	}
	open, end := strings.Index(got, "<requirements>"), strings.Index(got, "</requirements>")
	if open < 0 || end < open {
		t.Fatal("the prompt has no <requirements> block")
	}
	block := got[open:end]
	for _, want := range []string{f1 + ": ", f2 + ": ", "Hello, World!", "usage: hello [NAME]"} {
		if !strings.Contains(block, want) {
			t.Errorf("the requirements block lacks %q", want)
		}
	}
	if !strings.Contains(got, "These are the brief's requirements this function satisfies. Expected values come from them, not from the contract.") {
		t.Error("the prompt lacks the requirements sentence")
	}
	if open > strings.Index(got, "<brief_section>") {
		t.Error("the requirements block is not before the brief block")
	}
	none, err := renderTestwriterBrief(runNode, "hello", "internal/cli/cli.go", "TestRun", "internal/cli/fn_run_test.go", nil, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(none, "<requirements>") {
		t.Error("a node with no mapped requirement has a requirements block")
	}
}

// runUsageOnly is a test file for Run that asserts only the usage case.
const runUsageOnly = `package cli

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"a", "b"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if got := stderr.String(); got != "usage: hello [NAME]\n" {
		t.Fatalf("stderr = %q", got)
	}
}
`

func TestTestFileWithoutTheGreetingIsRefused(t *testing.T) {
	reqs := helloReqs(t)
	f1, f2 := featureID(t, reqs, "Say hello"), featureID(t, reqs, "Report errors")
	mapped := nodeRequirements(reqs, []Covered{{Requirement: f1, Nodes: []string{"fn-run"}}, {Requirement: f2, Nodes: []string{"fn-run"}}}, "fn-run", "cli")
	runText := "Prints Hello, World! or Hello, <NAME>! (Hello, Ada! for Ada) on stdout; on more than one argument prints usage: hello [NAME] on stderr and returns 2."
	lits := nodeLiterals(mapped, runText, []string{"hello"})

	err := checkRequirementLiterals(lits, runUsageOnly)
	if err == nil || !strings.HasPrefix(err.Error(), "expected_value_missing: Hello, World!") {
		t.Fatalf("err = %v, want the refusal expected_value_missing: Hello, World!", err)
	}
	full := strings.Replace(runUsageOnly, "func TestRun(t *testing.T) {", `func TestRun(t *testing.T) {
	for _, c := range []struct{ args []string; want string }{{nil, "Hello, World!\n"}, {[]string{"Ada"}, "Hello, Ada!\n"}} {
		var out, errb bytes.Buffer
		Run(c.args, &out, &errb)
		if out.String() != c.want {
			t.Errorf("got %q", out.String())
		}
	}`, 1)
	if err := checkRequirementLiterals(lits, full); err != nil {
		t.Errorf("a file asserting every literal was refused: %v", err)
	}

	// A literal the node does not mention is another function's to assert.
	greetText := "Returns Hello, <name>! or Hello, World! when name is empty."
	for _, l := range nodeLiterals(mapped, greetText, []string{"hello"}) {
		if l.lit == "usage: hello [NAME]" || l.lit == "2" {
			t.Errorf("Greeting must assert %q, which its node never mentions", l.lit)
		}
	}
}

// A test file writes a trailing newline as the escape \n, exactly as the brief
// does, so a test that asserts the text without it is refused, while the node
// doc that writes the newline for real still owns the literal.
func TestTestFileMustAssertTheEscapedNewline(t *testing.T) {
	q := Requirement{ID: "F1", Kind: ReqFeature, Text: "With no argument it writes exactly `Hello, World!\\n` to standard output."}
	lits := nodeLiterals([]Requirement{q}, "Writes 'Hello, World!\n' to stdout.", nil)
	if len(lits) != 1 || lits[0].lit != `Hello, World!\n` {
		t.Fatalf("node literals = %+v, want Hello, World!\\n", lits)
	}
	err := checkRequirementLiterals(lits, `if got != "Hello, World!" {`)
	if err == nil || !strings.HasPrefix(err.Error(), `expected_value_missing: Hello, World!\n`) {
		t.Errorf("err = %v, want expected_value_missing: Hello, World!\\n", err)
	}
	if err := checkRequirementLiterals(lits, `if got != "Hello, World!\n" {`); err != nil {
		t.Errorf("a file asserting the escaped newline was refused: %v", err)
	}
}
