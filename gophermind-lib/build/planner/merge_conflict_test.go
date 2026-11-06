package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

// conflictBase is okOutline with fn-run of component greeting already merged.
func conflictBase(t *testing.T) map[string]any {
	t.Helper()
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, _, err := mergePass(base, "greeting", `{"functions": [`+runFn("func Run(args []string) int")+`]}`, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func runFn(sig string) string {
	return `{"id": "fn-run", "package": "x", "file": "internal/x/run.go", "signature": "` + sig + `", "doc": "d", "uses": []}`
}

func TestSecondDifferentDeclarationIsAConflict(t *testing.T) {
	doc := conflictBase(t)
	before, _ := json.Marshal(doc)
	_, _, _, err := mergePass(doc, "greeting", `{"functions": [`+runFn("func Run(args []string, stdout io.Writer) int")+`]}`, testRunID)
	if err == nil || !strings.Contains(err.Error(), "conflicting declaration of fn-run") || !strings.Contains(err.Error(), "by component greeting") {
		t.Fatalf("err = %v, want a conflicting declaration of fn-run by component greeting", err)
	}
	if after, _ := json.Marshal(doc); string(after) != string(before) {
		t.Errorf("the stored doc changed:\n%s\n%s", before, after)
	}
}

func TestIdenticalReplayIsSilent(t *testing.T) {
	doc := conflictBase(t)
	next, _, ignored, err := mergePass(doc, "greeting", `{"functions": [`+runFn("func Run(args []string) int")+`]}`, testRunID)
	if err != nil || len(ignored) != 0 {
		t.Fatalf("identical replay: err %v ignored %v", err, ignored)
	}
	if n := len(objects(next["functions"])); n != 1 {
		t.Errorf("functions = %d, want 1", n)
	}
	out, added, ignored, conflicts := mergeByID(doc["functions"], []map[string]any{objects(doc["functions"])[0]}, 0, "function", nil, nil, nil)
	if len(out) != 1 || added != 0 || len(ignored) != 0 || len(conflicts) != 0 {
		t.Errorf("mergeByID replay: out %d added %d ignored %v conflicts %v", len(out), added, ignored, conflicts)
	}
}

const entryReply = `{"functions": [
 {"id": "fn-main", "package": "cmd/hello", "file": "cmd/hello/main.go", "signature": "func main()", "doc": "d", "uses": ["fn-run"]},
 {"id": "fn-run", "package": "cmd/hello", "file": "cmd/hello/main.go", "signature": "func run(args []string, stdout, stderr io.Writer) int", "doc": "d", "uses": []}],
 "more": false}`

func TestEntryExtrasDroppedWhenTheCallIsFixed(t *testing.T) {
	text, dropped := dropEntryExtras(entryReply, "cmd/hello", archOf(t, "hello"))
	if strings.Join(dropped, ",") != "fn-run" {
		t.Fatalf("dropped = %v, want fn-run", dropped)
	}
	var reply struct{ Functions []map[string]any }
	if err := json.Unmarshal([]byte(text), &reply); err != nil || len(reply.Functions) != 1 || reply.Functions[0]["id"] != "fn-main" {
		t.Errorf("functions = %v (%v), want only fn-main", reply.Functions, err)
	}
	if got := entryExtraDroppedText("entry-point", dropped); got != "entry_extra_function_dropped: entry-point: fn-run" {
		t.Errorf("warning = %q", got)
	}
	// A package outside cmd/ keeps every function.
	if _, d := dropEntryExtras(entryReply, "internal/cli", archOf(t, "hello")); len(d) != 0 {
		t.Errorf("dropped %v outside the entry package", d)
	}
}

func TestEntryExtrasKeptWithoutAFixedCall(t *testing.T) {
	// slugify fixes cli.Run: its run lives in internal/cli, not in cmd/, and stays.
	cliReply := strings.ReplaceAll(entryReply, "cmd/hello", "internal/cli")
	if text, dropped := dropEntryExtras(cliReply, "internal/cli", archOf(t, "slugify")); len(dropped) != 0 || text != cliReply {
		t.Errorf("slugify internal/cli: dropped %v", dropped)
	}
	for _, arch := range []string{
		archOf(t, "greeter"),
		"- `cmd/hello` (package `main`): parses the flags and prints the greeting.\n- `internal/greet`: builds messages.",
	} {
		text, dropped := dropEntryExtras(entryReply, "cmd/hello", arch)
		if len(dropped) != 0 || text != entryReply {
			t.Errorf("arch %q: dropped %v", arch, dropped)
		}
	}
}

func TestEntryPointIdIsNeverSuffixed(t *testing.T) {
	doc := alignDoc([]any{comp("cli", "internal/cli"), comp("entry-point", "internal/entry")},
		mainFn("cli", "internal/cli", "internal/cli/main.go"),
		fnFull("fn-run", "cli", "internal/cli", "internal/cli/run.go"))
	doc["module"] = "hello"
	id, err := entryComponentFor(doc, objects(doc["functions"])[0], "cmd/hello")
	if err == nil || !strings.Contains(err.Error(), "conflicting declaration of entry-point") {
		t.Fatalf("entryComponentFor = %q, %v; want a conflict error", id, err)
	}
	for _, c := range objects(doc["components"]) {
		if c["id"] == "entry-point-main" {
			t.Errorf("component entry-point-main was made")
		}
	}
	if _, err := moveMainToEntryPoint(doc); err == nil {
		t.Error("moveMainToEntryPoint hid the conflict")
	}
}

func TestComponentPromptLimitsHelpers(t *testing.T) {
	out, err := render("contract_component", map[string]string{
		"Defect": "", "Surface": "", "Component": "c", "Outline": "o", "Declared": "d", "Written": "w",
		"BriefSection": "bs", "Answers": "a", "ItemSchemas": "i", "EntryCall": "", "Fixed": "", "SurfaceName": "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"no more", "exact bytes", "Do not add helpers to a package whose fixed interfaces already cover its behaviour", `trailing newline written as \n`} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, bad := range []string{"no limit on the number of functions", "Functions are small"} {
		if strings.Contains(out, bad) {
			t.Errorf("prompt still says %q", bad)
		}
	}
}

func helloDoc() map[string]any {
	return map[string]any{
		"module": "hello",
		"components": []any{
			comp("say-hello", "internal/greet"), comp("report-errors", "internal/cli"), comp("entry-point", "cmd/hello"),
		},
		"types": []any{},
		"functions": []any{
			map[string]any{"id": "fn-greeting", "component": "say-hello", "package": "internal/greet", "signature": "func Greeting(name string) string", "uses": []any{}},
			map[string]any{"id": "fn-run", "component": "report-errors", "package": "internal/cli", "signature": "func Run(args []string, stdout, stderr io.Writer) int", "uses": []any{}},
			map[string]any{"id": "fn-main", "component": "entry-point", "package": "cmd/hello", "signature": "func main()", "uses": []any{}},
		},
	}
}

func TestFixedCallBecomesAUse(t *testing.T) {
	doc := helloDoc()
	warn := applyFixedUses(doc, archOf(t, "hello"))
	fns := objects(doc["functions"])
	if u := strList(fns[1]["uses"]); strings.Join(u, ",") != "fn-greeting" {
		t.Errorf("Run uses %v, want fn-greeting", u)
	}
	if u := strList(fns[2]["uses"]); strings.Join(u, ",") != "fn-run" {
		t.Errorf("main uses %v, want fn-run", u)
	}
	want := "fixed_use_added: fn-main uses fn-run|fixed_use_added: fn-run uses fn-greeting"
	if strings.Join(warn, "|") != want {
		t.Errorf("warnings = %v, want %s", warn, want)
	}
	// Applied again it adds nothing.
	if again := applyFixedUses(doc, archOf(t, "hello")); len(again) != 0 {
		t.Errorf("second apply warned %v", again)
	}
	// The function called is not declared yet: nothing is added.
	early := helloDoc()
	early["functions"] = objectsAsAny(early["functions"])[1:]
	if w := applyFixedUses(early, archOf(t, "hello")); strings.Join(w, "|") != "fixed_use_added: fn-main uses fn-run" {
		t.Errorf("warnings with Greeting undeclared = %v", w)
	}
}

// The missing-interface defect round asks the component again; its reply
// replaces the functions of the refused pass, and a function it does not
// write again is dropped. A more: true continuation adds to the component.
func TestReaskedPassSupersedesTheRefusedOne(t *testing.T) {
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	fn := func(id, sig string) string {
		return `{"id": "` + id + `", "package": "x", "file": "internal/x/greet.go", "signature": "` + sig + `", "doc": "d", "uses": []}`
	}
	first, more, _, err := mergePass(base, "greeting", `{"functions": [`+fn("fn-helper", "func Helper() string")+`], "more": true}`, testRunID)
	if err != nil || !more {
		t.Fatalf("first pass: more %v err %v", more, err)
	}
	before := functionIDs(first, "greeting")
	refusedDoc, _, _, err := mergePass(first, "greeting", `{"functions": [`+fn("fn-format-greeting", "func FormatGreeting(name string) string")+`,`+fn("fn-write-greeting", "func WriteGreeting(name string) string")+`]}`, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	refused := newFunctionIDs(refusedDoc, "greeting", before)
	if strings.Join(refused, ",") != "fn-format-greeting,fn-write-greeting" {
		t.Fatalf("refused pass = %v; a continuation pass must add, not replace", refused)
	}
	if ids := functionIDs(refusedDoc, "greeting"); strings.Join(ids, ",") != "fn-helper,fn-format-greeting,fn-write-greeting" {
		t.Fatalf("after the continuation the component has %v", ids)
	}

	reask := withoutFunctionIDs(refusedDoc, refused)
	doc, _, _, err := mergePass(reask, "greeting", `{"functions": [`+fn("fn-write-greeting", "func WriteGreeting(name string) string")+`,`+fn("fn-greeting", "func Greeting(name string) string")+`]}`, testRunID)
	if err != nil {
		t.Fatalf("a re-asked pass that rewrites a refused function is no conflict: %v", err)
	}
	if ids := functionIDs(doc, "greeting"); strings.Join(ids, ",") != "fn-helper,fn-write-greeting,fn-greeting" {
		t.Errorf("after the re-ask the component has %v", ids)
	}
	gone := supersededIDs(doc, refused)
	if strings.Join(gone, ",") != "fn-format-greeting" {
		t.Errorf("superseded = %v, want fn-format-greeting", gone)
	}
	if got := supersededText("greeting", gone); got != "superseded_function_dropped: greeting: fn-format-greeting" {
		t.Errorf("warning = %q", got)
	}
	if len(objects(refusedDoc["functions"])) != 3 {
		t.Error("withoutFunctionIDs changed the doc it was given")
	}
}

// declaredOutline is okOutline with a component helper in package y.
func declaredOutline(t *testing.T) map[string]any {
	t.Helper()
	text := strings.Replace(okOutline, `{"id": "greeting", "package": "x"}`, `{"id": "greeting", "package": "x"}, {"id": "helper", "package": "y"}`, 1)
	doc, _, err := parseOutline(text, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

const declaredArch = "- `x`: `Run(args []string) int` checks the arguments and calls `y.Help`.\n- `y`: helpers."

// mergeKeeping merges one pass and keeps its declarations in declared, the
// way the contract stage does.
func mergeKeeping(t *testing.T, doc map[string]any, declared map[string]string, component, reply string) map[string]any {
	t.Helper()
	next, _, ignored, decl, err := mergePassDeclared(doc, declared, component, reply, testRunID)
	if err != nil || len(ignored) != 0 {
		t.Fatalf("merge %s: err %v ignored %v", component, err, ignored)
	}
	for k, v := range decl {
		declared[k] = v
	}
	return next
}

func storedFunction(doc map[string]any, id string) map[string]any {
	for _, f := range objects(doc["functions"]) {
		if f["id"] == id {
			return f
		}
	}
	return nil
}

const helpReply = `{"functions": [{"id": "fn-help", "package": "y", "file": "y/help.go", "signature": "func Help() string", "doc": "d", "uses": []}]}`

func TestReEmissionAfterFixedUseIsSilent(t *testing.T) {
	declared := map[string]string{}
	doc := mergeKeeping(t, declaredOutline(t), declared, "helper", helpReply)
	runReply := `{"functions": [` + runFn("func Run(args []string) int") + `], "more": true}`
	doc = mergeKeeping(t, doc, declared, "greeting", runReply)
	if w := applyFixedUses(doc, declaredArch); strings.Join(w, "|") != "fixed_use_added: fn-run uses fn-help" {
		t.Fatalf("applyFixedUses = %v", w)
	}
	doc = mergeKeeping(t, doc, declared, "greeting", runReply)
	if u := strList(storedFunction(doc, "fn-run")["uses"]); strings.Join(u, ",") != "fn-help" {
		t.Errorf("stored uses = %v, want the fixed use kept", u)
	}
	// Without the model's own declaration the edited record is the reference.
	if _, _, _, err := mergePass(doc, "greeting", runReply, testRunID); err == nil {
		t.Error("compared with the edited record, the replay must conflict; the test no longer proves the rule")
	}
}

func TestReEmissionAfterFixedSignatureIsSilent(t *testing.T) {
	declared := map[string]string{}
	doc := declaredOutline(t)
	runReply := `{"functions": [` + runFn("func run(args []string) int") + `]}`
	doc = mergeKeeping(t, doc, declared, "greeting", runReply)
	applied, missing := applyFixedInterfaces(doc, "x", fixedInterfaces(declaredArch))
	if len(applied) != 1 || len(missing) != 0 {
		t.Fatalf("applied %v missing %v", applied, missing)
	}
	doc = mergeKeeping(t, doc, declared, "greeting", runReply)
	if sig := storedFunction(doc, "fn-run")["signature"]; sig != "func Run(args []string) int" {
		t.Errorf("stored signature = %v, want the fixed one kept", sig)
	}
	if _, _, _, _, err := mergePassDeclared(doc, declared, "greeting", `{"functions": [`+runFn("func run(args []string, n int) int")+`]}`, testRunID); err == nil || !strings.Contains(err.Error(), "conflicting declaration of fn-run") {
		t.Errorf("a different declaration after the fix: err %v, want a conflict", err)
	}
}

func TestSiblingComponentIdenticalReEmissionIsSilent(t *testing.T) {
	declared := map[string]string{}
	doc := mergeKeeping(t, declaredOutline(t), declared, "greeting", `{"functions": [`+runFn("func Run(args []string) int")+`]}`)
	doc = mergeKeeping(t, doc, declared, "types", `{"functions": [`+runFn("func Run(args []string) int")+`]}`)
	if c := storedFunction(doc, "fn-run")["component"]; c != "greeting" {
		t.Errorf("stored component = %v, want greeting", c)
	}
	if n := len(objects(doc["functions"])); n != 1 {
		t.Errorf("functions = %d, want 1", n)
	}
	// The stored record alone, without the declared forms, is compared without the stamp too.
	if _, _, _, err := mergePass(doc, "types", `{"functions": [`+runFn("func Run(args []string) int")+`]}`, testRunID); err != nil {
		t.Errorf("mergePass: %v", err)
	}
}

func TestSiblingComponentDifferentDeclarationConflicts(t *testing.T) {
	declared := map[string]string{}
	doc := mergeKeeping(t, declaredOutline(t), declared, "greeting", `{"functions": [`+runFn("func Run(args []string) int")+`]}`)
	_, _, _, _, err := mergePassDeclared(doc, declared, "types", `{"functions": [`+runFn("func Run(args []string, n int) int")+`]}`, testRunID)
	if err == nil || !strings.Contains(err.Error(), "conflicting declaration of fn-run by component types") {
		t.Errorf("err = %v, want a conflict naming component types", err)
	}
}
