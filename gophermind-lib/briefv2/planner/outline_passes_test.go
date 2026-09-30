package planner_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/ledger"
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
			if n == 1 && !strings.Contains(p, "<emitted>\n(nothing yet)\n</emitted>") {
				t.Errorf("first outline call must list nothing as emitted")
			}
			if n == 2 && !strings.Contains(p, "<emitted>\ncomponents: types, greeting\ntypes: name-error\n</emitted>") {
				t.Errorf("second outline call must list the ids of pass 1")
			}
		}
		if n != 2 {
			t.Errorf("saw %d outline requests, want 2", n)
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

// A type written in pass 1 may use a type that only pass 2 writes.
func TestOutlineAllowsAUsesOfALaterPass(t *testing.T) {
	later := `{"id": "late-type", "package": "greet", "file": "internal/greet/late.go", "decl": "// LateType is written in pass 2.\ntype LateType int"}`
	early := `{"id": "early-type", "package": "greet", "file": "internal/greet/early.go", "uses": ["late-type"], "decl": "// EarlyType uses LateType.\ntype EarlyType struct{ L LateType }"}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `], "types": [` + nameErrorType + `, ` + early + `], "more": true}`,
		"contract.outline.2.txt": `{"components": [` + comp("farewell") + `], "types": [` + later + `], "more": false}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := len(outlineStages(g)); n != 2 {
		t.Errorf("outline calls = %d, want 2 (no retry of pass 1)", n)
	}
	if len(g.contracts().Types) != 3 {
		t.Errorf("types = %d, want 3", len(g.contracts().Types))
	}
}

func danglingType(uses ...string) string {
	q := make([]string, len(uses))
	for i, u := range uses {
		q[i] = fmt.Sprintf("%q", u)
	}
	return `{"id": "early-type", "package": "greet", "file": "internal/greet/early.go", "uses": [` + strings.Join(q, ", ") + `], "decl": "// EarlyType.\ntype EarlyType int"}`
}

func danglingFirst(uses ...string) string {
	return `{` + outlineHeadJSON + `, "components": [` + comp("types") + `, ` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `, ` + danglingType(uses...) + `]}`
}

const missingType = `{"id": "missing-type", "package": "greet", "file": "internal/greet/missing.go", "decl": "// MissingType was asked for by a repair pass.\ntype MissingType int"}`

// A reference to an id no pass wrote is repaired by asking again, not by
// excluding the model.
func TestOutlineRepairsADanglingUses(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   danglingFirst("missing-type"),
		"contract.outline.2.txt": `{"types": [` + missingType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := len(outlineStages(g)); n != 2 {
		t.Fatalf("outline calls = %d, want the pass and one repair", n)
	}
	n := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:outline" {
			n++
			p := r.Messages[1].Content
			if has := strings.Contains(p, "<unresolved>\nmissing-type\n</unresolved>"); has != (n == 2) {
				t.Errorf("outline call %d: unresolved section present = %v", n, has)
			}
		}
	}
	if len(g.contracts().Types) != 3 {
		t.Errorf("types = %d, want 3", len(g.contracts().Types))
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	outline := 0
	for _, r := range rows {
		if r.Stage == "contract:outline" {
			outline++
			if r.Outcome != ledger.OutcomeOK || r.TaskType != "contract" {
				t.Errorf("outline row = %s/%s, want ok and contract", r.Outcome, r.TaskType)
			}
		}
	}
	if outline != 2 {
		t.Errorf("outline ledger rows = %d, want one per attempt (2)", outline)
	}
}

// Still dangling after the repair bound: the error names the ids that pass
// the id syntax, counts the rest, and quotes nothing else.
func TestOutlineStillDanglingAfterRepairsFailsNamingTheIDs(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   danglingFirst("missing-type", "ghost CANARY words"),
		"contract.outline.2.txt": `{"types": []}`,
		"contract.outline.3.txt": `{"types": []}`,
	}))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "contract:outline") || !strings.Contains(err.Error(), `"missing-type"`) || !strings.Contains(err.Error(), "2 repair passes") {
		t.Fatalf("err = %v, want the stage, the id and the bound named", err)
	}
	if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), "ghost") {
		t.Errorf("err quotes an id that fails the id syntax: %v", err)
	}
	if n := len(outlineStages(g)); n != 3 {
		t.Errorf("outline calls = %d, want the pass and two repairs", n)
	}
}

func TestOutlineRepairPassesAreStoredAndNotReAsked(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":   danglingFirst("missing-type"),
		"contract.outline.2.txt": `{"types": []}`,
		"contract.outline.3.txt": "the model fell over",
		"contract.outline.4.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the second repair to fail")
	}
	// After the restart the first outline request is the second repair.
	g.wire(variant(t, map[string]string{"contract.outline.txt": `{"types": [` + missingType + `]}`}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if n := len(outlineStages(g)); n != 1 {
		t.Errorf("resume made %d outline calls, want 1 (passes and the first repair are stored)", n)
	}
	if len(g.contracts().Types) != 3 {
		t.Errorf("types = %d, want 3", len(g.contracts().Types))
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const goldOutlineDeps = "Here.\n```json\n" + `{"module": "example.com/greeter",
 "conventions": {"layout": ["internal/greet: pure functions."], "naming": ["Verbs."], "errors": "Functions return *NameError.", "testing": "Table-driven tests."},
 "components": [{"id": "types", "package": "greet"}, {"id": "greeting", "package": "greet", "exports": []}, {"id": "farewell", "package": "greet", "exports": []}],
 "types": [{"id": "name-error", "package": "greet", "file": "internal/greet/errors.go", "decl": "// NameError says why a name was refused.\ntype NameError struct {\n\tReason string\n}"}],
 "dependencies": [{"module": "github.com/zeta/z", "version": "v1.2.3", "purpose": "z"}, {"module": "github.com/alpha/a", "version": "v0.1.0", "purpose": "a"}]}
` + "```\n"

// The golden files were recorded from the planner as it was before the
// outline passes existed: a one-pass outline must write the same bytes.
func TestOnePassOutlineWritesThePreWaveBytes(t *testing.T) {
	for name, dirs := range map[string][]string{"plain": nil, "deps": {variant(t, map[string]string{"contract.outline.txt": goldOutlineDeps})}} {
		g := newRig(t, approving(), dirs...)
		g.mustPlan(planner.Options{StopAfter: "contract"})
		if got, want := string(g.read("contracts.json")), readGolden(t, name+".contracts.json"); got != want {
			t.Errorf("%s: contracts.json differs from the pre-wave bytes", name)
		}
		if got, want := string(g.read("dependencies.json")), readGolden(t, name+".dependencies.json"); got != want {
			t.Errorf("%s: dependencies.json = %q, want %q", name, got, want)
		}
	}
}

func TestOutlinePromptHasNoStrayPlaceholder(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) != "contract:outline" {
			continue
		}
		p := r.Messages[1].Content
		for _, bad := range []string{"{{", "}}", "<no value>", "%!"} {
			if strings.Contains(p, bad) {
				t.Errorf("outline prompt contains %q", bad)
			}
		}
		if !strings.Contains(p, "<fixed>\n(not written yet") || !strings.Contains(p, "<emitted>\n(nothing yet)\n</emitted>") {
			t.Error("first-pass prompt lacks the fixed/emitted sections")
		}
	}
}
