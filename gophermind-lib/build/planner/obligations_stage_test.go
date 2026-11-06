package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// testsBrief adds a Tests section to the greeter brief: one bullet that calls
// Greet and one that calls nothing.
func testsBrief(s string) string {
	return strings.TrimRight(s, "\n") + "\n\n## Tests\n\n- `Greet(\"Ada\")` returns `Hello, Ada!` and a nil error.\n- Every case runs on its own.\n"
}

// citingGreet is the greeter's fn-greet reply with its happy case citing T1.
func citingGreet(t *testing.T, cite bool) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(greeter, "testwrite.fn-greet.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !cite {
		return string(raw)
	}
	return strings.Replace(string(raw), `"covers": "happy",`, `"covers": "happy", "obligations": ["T1"],`, 1)
}

func TestTestwriterCarriesAndChecksObligations(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": citingGreet(t, true)}))
	g.briefPath = writeBrief(t, greeter, g.repo, testsBrief)
	g.mustPlan(planner.Options{})

	var f struct {
		Obligations []planner.TestObligation `json:"obligations"`
		Unattached  []string                 `json:"unattached"`
	}
	if err := json.Unmarshal(g.read("_state/obligations.json"), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Obligations) != 1 || f.Obligations[0].ID != "T1" || f.Obligations[0].Target != "fn-greet" || !reflect.DeepEqual(f.Unattached, []string{"T2"}) {
		t.Errorf("obligations.json = %+v", f)
	}
	warned := false
	for _, w := range g.sink.OfKind("warning") {
		warned = warned || strings.HasPrefix(w.Message, "test_obligation_unattached: T2: Every case runs on its own.")
	}
	if !warned {
		t.Error("no test_obligation_unattached warning for T2")
	}
	lt, err := planner.ReadLeafTests(g.runDir)
	if err != nil || !reflect.DeepEqual(lt["fn-greet"].Obligations, []string{"T1"}) || lt["fn-farewell"].Obligations != nil {
		t.Errorf("leaf tests = %+v, %v", lt, err)
	}
	for _, r := range g.fake.Requests() {
		prompt := r.Messages[1].Content
		switch planner.StageOf(r) {
		case "testwrite:fn-greet":
			if !strings.Contains(prompt, "<obligations>\nT1: `Greet(\"Ada\")` returns") {
				t.Error("the fn-greet prompt lacks its obligation")
			}
		case "testwrite:fn-farewell":
			if strings.Contains(prompt, "<obligations>") {
				t.Error("the fn-farewell prompt carries obligations it has none of")
			}
		case "coverage":
			if strings.Contains(prompt, "Every case runs on its own") {
				t.Error("the coverage prompt lists a Tests bullet")
			}
		}
	}
	reqs, err := planner.ReadRequirements(g.runDir)
	if err != nil || reqs[len(reqs)-1].ID != "T2" {
		t.Errorf("requirements.json lacks the Tests bullets: %+v, %v", reqs, err)
	}
}

func TestTestwriterRefusesAnUncitedObligation(t *testing.T) {
	g := newRig(t, approving())
	g.briefPath = writeBrief(t, greeter, g.repo, testsBrief)
	if out, err := g.plan(planner.Options{}); err == nil && out == planner.Done {
		t.Fatal("a reply citing no obligation was accepted")
	}
	refusals, _ := os.ReadFile(filepath.Join(g.runDir, "_state", "refusals.jsonl"))
	if !strings.Contains(string(refusals), "no test case covers obligation T1") {
		t.Errorf("refusals = %s", refusals)
	}
}
