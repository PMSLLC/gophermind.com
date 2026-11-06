package planner

import (
	"strings"
	"testing"
)

func TestTestwriterPromptCarriesBriefSection(t *testing.T) {
	brief := "### Make a slug\n\n- a `max` of 0 or less means no limit."
	got, err := renderTestwriterBrief(`{"contract":{"signature":"func Make(s string, max int) string"}}`,
		"example.com/slugify", "internal/slug/make.go", "TestFnMake", "internal/slug/fn_make_test.go", nil, brief, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<brief_section>") || !strings.Contains(got, "0 or less means no limit") {
		t.Fatal("the prompt lacks the brief section")
	}
	if !strings.Contains(got, "the brief wins") {
		t.Error("the prompt does not say the brief wins over the node")
	}
	plain, err := renderTestwriterWith(`{"contract":{"signature":"func Make(s string, max int) string"}}`,
		"example.com/slugify", "internal/slug/make.go", "TestFnMake", "internal/slug/fn_make_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "<brief_section>") {
		t.Error("an empty brief section is still rendered")
	}
}

func TestTestwriterPromptCarriesObligations(t *testing.T) {
	node := `{"contract":{"signature":"func Run(args []string, stdout, stderr io.Writer) int"}}`
	got, err := renderTestwriterBrief(node, "hello", "internal/cli/cli.go", "TestRun", "internal/cli/fn_run_test.go", nil, "", runObligations, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<obligations>\nT1: `Run(nil, stdout, stderr)` returns 0",
		"\nT2: `Run([]string{}, stdout, stderr)` returns 0",
		"</obligations>",
		`Every obligation in <obligations> is one case of the table. Set that case's "obligations" to the ids it covers (["T3"]). Use the obligation's exact arguments: nil and []string{} are different cases when both are listed. Obligations are the brief's own words and win over the contract.`,
		`"obligations": ["T1"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	if strings.Index(got, "<obligations>") > strings.Index(got, "Node:\n<node>") {
		t.Error("the obligations block is not before the node")
	}
	none, err := renderTestwriterBrief(node, "hello", "internal/cli/cli.go", "TestRun", "internal/cli/fn_run_test.go", nil, "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(none, "obligations") {
		t.Error("a node with no obligations has obligations text in its prompt")
	}
}
