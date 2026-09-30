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
		if s == "contract:outline:1" {
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

// The greeter brief has two features: a shared pass and one batch.
func TestOutlineIsASharedPassAndBatches(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:1 contract:outline:2" {
		t.Fatalf("outline stages = %q", got)
	}
	if got := componentIDs(g); got != "types greeting farewell" {
		t.Errorf("components = %q", got)
	}
	// The batch names what is already declared; the shared pass does not.
	n := 0
	for _, r := range g.fake.Requests() {
		if !strings.HasPrefix(planner.StageOf(r), "contract:outline") {
			continue
		}
		n++
		p := r.Messages[1].Content
		if n == 1 && !strings.Contains(p, "<emitted>\n(nothing yet)\n</emitted>") {
			t.Errorf("shared pass must list nothing as emitted")
		}
		if n == 2 && !strings.Contains(p, "THESE features only: Greeting, Farewell\n") {
			t.Errorf("the batch prompt does not name the features:\n%s", p)
		}
		if n == 2 && !strings.Contains(p, "<emitted>\ncomponents: types\ntypes: name-error\n</emitted>") {
			t.Errorf("batch must list the ids of the shared pass:\n%s", p)
		}
	}
}

func TestOutlineDropsAnIdenticalDuplicateSilently(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "types": [` + nameErrorType + `]}`,
		"contract.outline.2.txt": `{"components": [` + comp("types") + `, ` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + nameErrorType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := componentIDs(g); got != "types greeting farewell" || len(g.contracts().Types) != 1 {
		t.Errorf("components = %q, types %d: identical duplicates must be dropped", got, len(g.contracts().Types))
	}
	if w := g.sink.OfKind("warning"); len(w) != 0 {
		t.Errorf("identical duplicates raised warnings: %v", w)
	}
}

func TestOutlineResumeDoesNotAskForStoredPasses(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "types": [` + nameErrorType + `]}`,
		"contract.outline.2.txt": "the model fell over", "contract.outline.2.2.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the outline to fail on its batch")
	}
	if g.has("contracts.json") || !g.has("_state/contract.json") {
		t.Fatal("the shared pass must be stored and contracts.json not written yet")
	}
	g.wire(variant(t, map[string]string{
		"contract.outline.2.txt": `{"components": [` + comp("greeting") + `, ` + comp("farewell") + `]}`,
	}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:2" {
		t.Errorf("resume made outline calls %q, want only the unfinished batch", got)
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
		case "contract:outline:1", "contract:outline:2":
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

// A type written in the shared pass may use a type that only a batch writes.
func TestOutlineAllowsAUsesOfALaterPass(t *testing.T) {
	later := `{"id": "late-type", "package": "greet", "file": "internal/greet/late.go", "decl": "// LateType is written in the batch.\ntype LateType int"}`
	early := `{"id": "early-type", "package": "greet", "file": "internal/greet/early.go", "uses": ["late-type"], "decl": "// EarlyType uses LateType.\ntype EarlyType struct{ L LateType }"}`
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.1.txt": `{` + outlineHeadJSON + `, "components": [` + comp("types") + `], "types": [` + nameErrorType + `, ` + early + `]}`,
		"contract.outline.2.txt": `{"components": [` + comp("greeting") + `, ` + comp("farewell") + `], "types": [` + later + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if got := passStages(g); got != "contract:outline:1 contract:outline:2" {
		t.Errorf("outline stages = %q, want no retry and no repair", got)
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
		"contract.outline.txt":        danglingFirst("missing-type"),
		"contract.outline.repair.txt": `{"types": [` + missingType + `]}`,
	}))
	g.mustPlan(planner.Options{StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:outline:repair"); n != 1 {
		t.Fatalf("repair calls = %d, want 1", n)
	}
	n := 0
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "contract:outline:repair" {
			n++
			p := r.Messages[1].Content
			if !strings.Contains(p, "<unresolved>\nmissing-type\n</unresolved>") {
				t.Errorf("repair call %d lacks the unresolved section", n)
			}
		}
	}
	if len(g.contracts().Types) != 3 {
		t.Errorf("types = %d, want 3", len(g.contracts().Types))
	}
	rows, _ := g.led.List(context.Background(), greeterID, ledger.Filter{TaskType: "contract"})
	outline := 0
	for _, r := range rows {
		if strings.HasPrefix(r.Stage, "contract:outline") {
			outline++
			if r.Outcome != ledger.OutcomeOK || r.TaskType != "contract" {
				t.Errorf("outline row = %s/%s, want ok and contract", r.Outcome, r.TaskType)
			}
		}
	}
	if outline != 3 {
		t.Errorf("outline ledger rows = %d, want one per attempt (shared, batch, repair)", outline)
	}
}

// Still dangling after the repair bound: the error names the ids that pass
// the id syntax, counts the rest, and quotes nothing else.
func TestOutlineStillDanglingAfterRepairsFailsNamingTheIDs(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":          danglingFirst("missing-type", "ghost CANARY words"),
		"contract.outline.repair.txt":   `{"types": []}`,
		"contract.outline.repair.2.txt": `{"types": []}`,
	}))
	_, err := g.plan(planner.Options{StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "contract:outline") || !strings.Contains(err.Error(), `"missing-type"`) || !strings.Contains(err.Error(), "2 repair passes") {
		t.Fatalf("err = %v, want the stage, the id and the bound named", err)
	}
	if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), "ghost") {
		t.Errorf("err quotes an id that fails the id syntax: %v", err)
	}
	if n := count(g.stagesCalled(), "contract:outline:repair"); n != 2 {
		t.Errorf("repair calls = %d, want two", n)
	}
}

func TestOutlineRepairPassesAreStoredAndNotReAsked(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"contract.outline.txt":          danglingFirst("missing-type"),
		"contract.outline.repair.txt":   "the model fell over",
		"contract.outline.repair.2.txt": "and again",
	}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the first repair to fail")
	}
	// After the restart the first outline request is the second repair attempt.
	g.wire(variant(t, map[string]string{"contract.outline.repair.txt": `{"types": [` + missingType + `]}`}))
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if n := count(g.stagesCalled(), "contract:outline:repair"); n != 1 || len(outlineStages(g)) != 0 {
		t.Errorf("resume made %d repair calls and %d pass calls, want 1 and 0", n, len(outlineStages(g)))
	}
	if len(g.contracts().Types) != 3 {
		t.Errorf("types = %d, want 3", len(g.contracts().Types))
	}
}

// A failed repair attempt is an attempt: the count survives a restart.
func TestFailedOutlineRepairsCountAcrossResumes(t *testing.T) {
	bad := variant(t, map[string]string{"contract.outline.txt": danglingFirst("missing-type"),
		"contract.outline.repair.txt": "the model fell over", "contract.outline.repair.2.txt": "and again"})
	g := newRig(t, approving(), bad)
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil {
		t.Fatal("want the first repair to fail")
	}
	g.wire(variant(t, map[string]string{"contract.outline.repair.txt": "fell over", "contract.outline.repair.2.txt": "again"}))
	if _, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "contract"}); err == nil {
		t.Fatal("want the second repair to fail")
	}
	g.wire(variant(t, map[string]string{"contract.outline.repair.txt": `{"types": [` + missingType + `]}`}))
	_, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "contract"})
	if err == nil || !strings.Contains(err.Error(), "2 repair passes") || len(g.stagesCalled()) != 0 {
		t.Fatalf("err = %v calls %v, want the bound reached from the stored count with no call", err, g.stagesCalled())
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
// outline passes existed. Provenance: the repo at 682d332 was extracted with
// `git archive 682d332 | tar -x` (no checkout), its one-pass path was run on
// these fixtures, and the sha256 of its output equals the committed files:
//
//	plain.contracts.json     b045d9797a621a01f70c9a00dd8c8871d935c57bf7c661bb512d6985c43c41d4
//	plain.dependencies.json  37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570
//	deps.contracts.json      fb65c06dd2359087390fb3cf5dfef7c222e1aa61ee75e58b7d6a3a172d82a05c
//	deps.dependencies.json   f7904755d859d97dac0eaeefa9a5988945d4e374acfd2095ea5aac1b4127b9b8
//
// A one-pass outline must write the same bytes.
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
		if planner.StageOf(r) != "contract:outline:1" {
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
