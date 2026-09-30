package planner_test

import (
	"fmt"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

const outlineHeadJSON = `"module": "example.com/greeter",
 "conventions": {"layout": ["internal/greet: pure functions."], "naming": ["Verbs."],
  "errors": "Functions return *NameError.", "testing": "Table-driven tests."}`

const nameErrorType = `{"id": "name-error", "package": "greet", "file": "internal/greet/errors.go",
  "decl": "// NameError says why a name was refused.\ntype NameError struct {\n\tReason string\n}"}`

func comp(id string) string {
	return fmt.Sprintf(`{"id": %q, "package": "greet", "exports": []}`, id)
}

func outlineStages(g *rig) []string {
	var out []string
	for _, s := range g.stagesCalled() {
		if s == "contract:outline" {
			out = append(out, s)
		}
	}
	return out
}

func componentIDs(g *rig) string {
	var ids []string
	for _, c := range g.contracts().Components {
		ids = append(ids, c.ID)
	}
	return strings.Join(ids, " ")
}

func TestOutlineContinuesWhileMoreIsTrue(t *testing.T) {
	t.Run("two passes", func(t *testing.T) {
		g := newRig(t, approving(), variant(t, map[string]string{
			"contract.outline.txt":   `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `], "more": true}`,
			"contract.outline.2.txt": `{"components": [` + comp("farewell") + `], "types": [], "more": false}`,
		}))
		g.mustPlan(planner.Options{StopAfter: "contract"})
		if n := len(outlineStages(g)); n != 2 {
			t.Fatalf("outline calls = %d, want 2", n)
		}
		if got := componentIDs(g); got != "types greeting farewell" {
			t.Errorf("components = %q, want the passes merged in order", got)
		}
		if len(g.contracts().Types) != 1 || len(g.contracts().Functions) != 3 {
			t.Error("the merged contract lost a type or function")
		}
		// The continuation names what is already emitted; the first call does not.
		n := 0
		for _, r := range g.fake.Requests() {
			if planner.StageOf(r) != "contract:outline" {
				continue
			}
			n++
			p := r.Messages[1].Content
			has := strings.Contains(p, "<emitted>") && strings.Contains(p, "greeting") && strings.Contains(p, "name-error")
			if n == 1 && strings.Contains(p, "<emitted>\ntypes") || n == 2 && !has {
				t.Errorf("outline call %d: already-emitted list is wrong", n)
			}
		}
	})
	t.Run("three passes", func(t *testing.T) {
		g := newRig(t, approving(), variant(t, map[string]string{
			"contract.outline.txt":   `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "types": [` + nameErrorType + `], "more": true}`,
			"contract.outline.2.txt": `{"components": [` + comp("greeting") + `], "more": true}`,
			"contract.outline.3.txt": `{"components": [` + comp("farewell") + `]}`,
		}))
		g.mustPlan(planner.Options{StopAfter: "contract"})
		if n := len(outlineStages(g)); n != 3 {
			t.Fatalf("outline calls = %d, want 3", n)
		}
		if got := componentIDs(g); got != "types greeting farewell" {
			t.Errorf("components = %q", got)
		}
	})
	t.Run("one pass is unchanged", func(t *testing.T) {
		g := newRig(t, approving())
		g.mustPlan(planner.Options{StopAfter: "contract"})
		if n := len(outlineStages(g)); n != 1 {
			t.Fatalf("outline calls = %d, want 1", n)
		}
		if got := componentIDs(g); got != "types greeting farewell" {
			t.Errorf("components = %q", got)
		}
	})
}

func TestOutlineDropsAnIdenticalDuplicateAndRefusesAConflictingOne(t *testing.T) {
	first := `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `], "more": true}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   first,
		"contract.outline.2.txt": `{"components": [` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell" || len(g.contracts().Types) != 1 {
		t.Errorf("components = %q, types %d: identical duplicates must be dropped", got, len(g.contracts().Types))
	}

	bad := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   first,
		"contract.outline.2.txt": `{"components": [{"id": "greeting", "package": "other", "exports": []}], "more": false}`,
		"contract.outline.3.txt": `{"components": [{"id": "greeting", "package": "other", "exports": []}], "more": false}`,
	}))
	_, err := bad.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), `"greeting"`) {
		t.Fatalf("err = %v, want the conflicting id named", err)
	}
}

func TestOutlineNeedsProgressWhenItSaysMore(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "more": true}`,
		"contract.outline.2.txt": `{"components": [], "more": true}`,
		"contract.outline.3.txt": `{"components": [], "more": true}`,
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil || !strings.Contains(err.Error(), "adds nothing") {
		t.Fatalf("err = %v, want a refusal of a pass that says more but adds nothing", err)
	}
}

func TestOutlinePassCapEndsWithAFixedError(t *testing.T) {
	files := map[string]string{}
	for i := 1; i <= 25; i++ {
		name := "contract.outline.txt"
		if i > 1 {
			name = fmt.Sprintf("contract.outline.%d.txt", i)
		}
		head := ""
		if i == 1 {
			head = outlineHeadJSON + ", "
		}
		files[name] = `{` + head + `"components": [` + comp(fmt.Sprintf("part-%d", i)) + `], "more": true}`
	}
	g := newRig(t, approving(), variant(t, files))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "contract:outline") || !strings.Contains(err.Error(), "20 passes") {
		t.Fatalf("err = %v, want a fixed message naming the stage and the cap", err)
	}
	if n := len(outlineStages(g)); n != 20 {
		t.Errorf("outline calls = %d, want exactly the cap of 20", n)
	}
}

func TestOutlineResumeDoesNotAskForStoredPasses(t *testing.T) {
	first := `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `], "more": true}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   first,
		"contract.outline.2.txt": "the model fell over",
		"contract.outline.3.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the outline to fail on its second pass")
	}
	if g.has("contracts.json") || !g.has("_state/contract.json") {
		t.Fatal("the first pass must be stored and contracts.json not written yet")
	}

	// After the restart the first request for the stage is the second pass.
	g.wire(variant(t, map[string]string{
		"contract.outline.txt": `{"components": [` + comp("farewell") + `], "more": false}`,
	}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if n := len(outlineStages(g)); n != 1 {
		t.Errorf("resume made %d outline calls, want 1 (only the unfinished pass)", n)
	}
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
}

func TestBudgets(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	for _, r := range g.fake.Requests() {
		switch planner.StageOf(r) {
		case "contract:outline":
			if r.MaxTokens < 16000 || r.MaxGrownTokens < 32768 {
				t.Errorf("outline request max_tokens %d grown cap %d, want >= 16000 and >= 32768", r.MaxTokens, r.MaxGrownTokens)
			}
		case "contract:greeting", "contract:farewell", "contract:types":
			if r.MaxTokens != 8000 || r.MaxGrownTokens != 0 {
				t.Errorf("%s request max_tokens %d grown cap %d, want the unchanged 8000 and the router default", planner.StageOf(r), r.MaxTokens, r.MaxGrownTokens)
			}
		}
	}
}
