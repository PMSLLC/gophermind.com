package planner

import (
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
)

// The examples the prompts teach are judged by the checker that gates the
// plan: every good one passes, every bad one is refused.
func TestHarnessPromptExamplesMatchTheChecker(t *testing.T) {
	text := harnessContract()
	gi, bi := strings.Index(text, "\nGood ("), strings.Index(text, "\nBad (")
	if gi < 0 || bi < gi {
		t.Fatal("harness.md lost its Good and Bad sections")
	}
	span := regexp.MustCompile("`([^`]+)`")
	spans := func(section string) []string {
		var out []string
		for _, line := range strings.Split(section, "\n")[1:] {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			for _, m := range span.FindAllStringSubmatch(line, -1) {
				out = append(out, m[1])
			}
		}
		return out
	}
	o := acceptcheck.Options{Bins: []string{"venture-server"}}
	good, bad := spans(text[gi:bi]), spans(text[bi:])
	if len(good) != 4 || len(bad) < 4 {
		t.Fatalf("%d good and %d bad examples", len(good), len(bad))
	}
	for i, c := range good {
		if f := acceptcheck.Quality(c, o); len(f) != 0 {
			t.Errorf("good example %q is refused: %v", c, f)
		}
		if i > 0 && acceptcheck.Vacuous(c, o.Bins) { // the first is a constraint check, which is not held to the server probe rule
			t.Errorf("good example %q is vacuous", c)
		}
	}
	for _, c := range bad {
		if len(acceptcheck.Quality(c, o)) == 0 {
			t.Errorf("bad example %q is accepted", c)
		}
	}
}

func TestCoveragePromptsCarryTheHarnessContract(t *testing.T) {
	for _, name := range []string{"coverage", "coverage_fill"} {
		data := map[string]string{"Requirements": "r", "Nodes": "n", "Gaps": "g", "Weak": "w", "Components": "c", "ItemSchemas": "s", "Harness": harnessContract()}
		out, err := render(name, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"GM_ACCEPTANCE_ADDR", "GM_ACCEPTANCE_URL", "GM_ACCEPTANCE_BIN", "GM_ACCEPTANCE_PIDFILE", "masked_failure", "placeholder", "-run '^TestA8$'", "serve"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s prompt lacks %q", name, want)
			}
		}
	}
}
