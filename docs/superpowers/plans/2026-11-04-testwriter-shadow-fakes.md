# Test writer shadow, fakes and prompt noise Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The planner refuses a contract package named like a standard library package, the test writer refuses a test file that never calls the function or cannot fail, and the test-writer prompt loses two pieces of noise.

**Architecture:** Three independent changes in `gophermind-lib/build/planner`: a new check called from `localChecks` (contract stage), a new check called from the `writeTests` parse closure (test writer), and a prompt wording fix plus a view filter applied before the node is rendered into the prompt.

**Tech Stack:** Go, `go/ast`, `go/parser`, existing planner test rig (`newRig`, `variant`, `badTestwrites`, `fnGreetRequests` in `e2e_test.go`).

**Spec:** `docs/superpowers/specs/2026-11-04-testwriter-shadow-fakes-design.md`

## Global Constraints

- No em dashes, no emojis in any file or message.
- Work on `main` in this checkout. Never push, stash, checkout, reset, clean or restore. Commit with explicit `git add` paths.
- Every commit message ends with the two trailers:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`
- Refusal text never quotes reply text. It names identifiers from the contract (through `boundedID` for ids and package names) or fixed words.
- Nothing under `~/.gophermind` is read or written by tests.
- Build gate from `gophermind-lib`: `go build ./... && go vet ./build/planner && gofmt -l build/planner` prints nothing.
- Touch only what the task names. Match the file's existing style.

## Review Focus

- A contract package `internal/http` (last element `http`) is refused like `http` is; `internal/types` and `cmd/linkcheck` are not. Task 1 tests both.
- A method under test referenced as `s.Get(...)` counts as calling the function. Task 2 tests it.
- A test helper that takes `testing.TB` and calls `tb.Fatalf` still counts as an assertion. Task 2 tests it.
- A node whose function is unexported keeps the unexported helpers in `dependency_signatures`. Task 3 tests it.
- The existing e2e fixtures that return a good reply still plan green after Task 2. Task 2 runs the whole planner suite.

---

### Task 1: Refuse a contract package named like the standard library

**Files:**
- Create: `gophermind-lib/build/planner/stdshadow.go`
- Create: `gophermind-lib/build/planner/stdshadow_internal_test.go`
- Modify: `gophermind-lib/build/planner/contract_stage.go:1221-1266` (`localChecks`)

**Interfaces:**
- Consumes: `StdQualifiers` (`qualifier.go`), `boundedID` (`contract_stage.go`), `contract.Contracts` (`build/contract/contract.go`: `Components[].Package`, `Types[].Package`, `Functions[].Package`).
- Produces: `func stdShadow(c *contract.Contracts) error`.

- [ ] **Step 1: Write the failing tests** in `stdshadow_internal_test.go` (package `planner`):

```go
func TestLocalChecksRefusesAPackageNamedLikeTheStandardLibrary(t *testing.T) {
	c := &contract.Contracts{
		BriefID:    "b",
		Components: []contract.Component{{ID: "io", Package: "io"}},
		Types: []contract.Type{{ID: "io-writer", Package: "io", File: "io/io.go",
			Decl: "type Writer interface {\n\tWrite(p []byte) (n int, err error)\n}"}},
	}
	err := localChecks(c, "b")
	if err == nil || !strings.Contains(err.Error(), "package io is named like the standard library package io") || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("localChecks = %v, want the shadow rule naming io", err)
	}
}

func TestStdShadowLooksAtTheLastPathElement(t *testing.T) {
	refused := &contract.Contracts{Components: []contract.Component{{ID: "web", Package: "internal/http"}}}
	if err := stdShadow(refused); err == nil || !strings.Contains(err.Error(), "net/http") {
		t.Errorf("internal/http: err = %v, want a refusal naming net/http", err)
	}
	ok := &contract.Contracts{
		Components: []contract.Component{{ID: "types", Package: "internal/types"}, {ID: "main", Package: "cmd/linkcheck"}, {ID: "root", Package: "."}},
		Functions:  []contract.Function{{ID: "fn-all", Package: "internal/check"}},
	}
	if err := stdShadow(ok); err != nil {
		t.Errorf("allowed packages refused: %v", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run from `gophermind-lib`: `go test ./build/planner -run 'TestLocalChecksRefusesAPackageNamedLikeTheStandardLibrary|TestStdShadowLooksAtTheLastPathElement'`
Expected: compile error, `stdShadow` undefined.

- [ ] **Step 3: Implement `stdShadow` in `stdshadow.go`**

`func stdShadow(c *contract.Contracts) error`. Walk components, then types, then functions. For each, `pkg := strings.TrimSpace(Package)`; skip `""` and `"."`; `base := path.Base(pkg)`; if `std, ok := StdQualifiers[base]; ok` return
`fmt.Errorf("contract: package %s is named like the standard library package %s, which tests import by that qualifier (declared by %s); rename the directory and its package to a name no standard library package uses", boundedID(base), std, boundedID(id))`.
Doc comment states why: `ResolveQualifier` prefers a contract package, so a test that follows the prompt imports the module's package and finds none of the standard library's names in it.

- [ ] **Step 4: Call it from `localChecks`** right after the components loop (before the types loop): `if err := stdShadow(c); err != nil { return err }`.

- [ ] **Step 5: Run the tests and the contract suite**

Run: `go test ./build/planner -run 'TestLocalChecks|TestStdShadow|TestContract|Outline|Repair'`
Expected: PASS. If an existing fixture declares a package named like a standard library package, rename it in the fixture and say so in the report.

- [ ] **Step 6: Build gate, then commit**

```bash
cd gophermind-lib && go build ./... && go vet ./build/planner && gofmt -l build/planner
git add gophermind-lib/build/planner/stdshadow.go gophermind-lib/build/planner/stdshadow_internal_test.go gophermind-lib/build/planner/contract_stage.go
git commit -m "fix(planner): the contract refuses a package named like the standard library"
```

---

### Task 2: Refuse a test file that never calls the function or cannot fail

**Files:**
- Create: `gophermind-lib/build/planner/exercise.go`
- Create: `gophermind-lib/build/planner/exercise_internal_test.go`
- Modify: `gophermind-lib/build/planner/testwriter.go:565-570` (the `parse` closure in `writeTests`, right after `parseTestwriteCounted`)
- Modify: `gophermind-lib/build/planner/e2e_test.go` (one new test near line 946)
- Modify as needed: fixtures under `gophermind-lib/build/planner/testdata/greeter*/testwrite.*.txt`, and inline fixtures at `e2e_test.go:381`, `refusal_test.go:16`, `revise_test.go:228`.

**Interfaces:**
- Consumes: `internalTarget(sig string) (name string, method bool, why string)` (`testwriter.go:347`), `sig` is `ct["signature"]` (already read at `testwriter.go:562`).
- Produces: `func checkTestExercises(src, name string) error`.

- [ ] **Step 1: Write the failing unit tests** in `exercise_internal_test.go`:

```go
const exercisedSrc = `package cli_test

import (
	"testing"

	"example.com/linkcheck/cli"
)

func TestRun(t *testing.T) {
	if got := cli.Run([]string{"x"}, nil, nil, nil); got != 2 {
		t.Fatalf("Run = %d, want 2", got)
	}
}
`

func TestCheckTestExercisesAcceptsACallAndAnAssertion(t *testing.T) {
	if err := checkTestExercises(exercisedSrc, "Run"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesRefusesAFileThatNeverCallsTheFunction(t *testing.T) {
	src := strings.Replace(exercisedSrc, `got := cli.Run([]string{"x"}, nil, nil, nil)`, `got := 2`, 1)
	src = strings.Replace(src, "\n\t\"example.com/linkcheck/cli\"\n", "\n", 1)
	err := checkTestExercises(src, "Run")
	if err == nil || !strings.Contains(err.Error(), "never calls Run") {
		t.Fatalf("err = %v, want never calls Run", err)
	}
}

func TestCheckTestExercisesRefusesAFileThatCannotFail(t *testing.T) {
	src := strings.Replace(exercisedSrc, `t.Fatalf("Run = %d, want 2", got)`, `t.Log(got)`, 1)
	err := checkTestExercises(src, "Run")
	if err == nil || !strings.Contains(err.Error(), "cannot fail") {
		t.Fatalf("err = %v, want cannot fail", err)
	}
}

func TestCheckTestExercisesAcceptsAMethodAndAHelperAssertion(t *testing.T) {
	src := `package store

import "testing"

func check(tb testing.TB, got, want int) {
	tb.Helper()
	if got != want {
		tb.Fatalf("got %d, want %d", got, want)
	}
}

func TestGet(t *testing.T) {
	var s Store
	check(t, s.Get("k"), 0)
}
`
	if err := checkTestExercises(src, "Get"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTestExercisesSkipsMainAndAnEmptyName(t *testing.T) {
	src := "package main\n\nimport \"testing\"\n\nfunc TestMain2(t *testing.T) { _ = main }\n"
	if err := checkTestExercises(src, "main"); err != nil {
		t.Error(err)
	}
	if err := checkTestExercises(src, ""); err != nil {
		t.Error(err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./build/planner -run 'TestCheckTestExercises'`
Expected: compile error, `checkTestExercises` undefined.

- [ ] **Step 3: Implement `checkTestExercises` in `exercise.go`**

`func checkTestExercises(src, name string) error`. Return nil when `name` is `""` or `"main"`. Parse with `parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)`; on a parse error return nil (the earlier checks already reported it). Walk with `ast.Inspect`:
- `named` becomes true on any `*ast.Ident` whose `Name == name`, except the `Name` ident of an `*ast.FuncDecl` (skip that one ident, keep walking the body).
- `fails` becomes true on any `*ast.CallExpr` whose `Fun` is an `*ast.SelectorExpr` with `Sel.Name` in `Error`, `Errorf`, `Fatal`, `Fatalf`, `Fail`, `FailNow`.
Errors, in this order:
- `fmt.Errorf("test file never calls %s, the function under test: every case must call it and compare what it returns", name)`
- `errors.New("test file cannot fail: it has no t.Error, t.Errorf, t.Fatal or t.Fatalf; compare each case's result with the expected value and report a mismatch")`

- [ ] **Step 4: Hook it into `writeTests`.** In the `parse` closure, right after `parseTestwriteCounted` returns without error and before `otherTestSources`: `name, _, _ := internalTarget(sig)` then `if err := checkTestExercises(src, name); err != nil { return err }`. `sig` is read at line 562, before the closure; keep that.

- [ ] **Step 5: Write the failing e2e test** in `e2e_test.go` after `TestATestWriterNodeGetsAFreshRoundAfterTheChainIsExhausted`:

```go
// A reply whose test never calls the function is refused, and the retry is
// told why in the harness's words.
func TestATestWriterFakeIsRefusedAndTheRetryIsToldWhy(t *testing.T) {
	fake := `{"tests": [{"name": "greets a name", "polarity": "success", "covers": "happy", "given": "Ada", "expect": "Hello, Ada!"},
	 {"name": "refuses an empty name", "polarity": "negative", "covers": "error:1", "given": "   ", "expect": "a *NameError"}],
	 "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tgot := \"Hello, Ada!\"\n\tif got != \"Hello, Ada!\" {\n\t\tt.Fatal(got)\n\t}\n}\n"}`
	files := map[string]string{"testwrite.fn-greet.txt": fake,
		"testwrite.fn-greet.2.txt": string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))}
	g := newRig(t, approving(), variant(t, files))
	g.mustPlan(planner.Options{})
	fnGreet := fnGreetRequests(g)
	if len(fnGreet) != 2 {
		t.Fatalf("fn-greet calls = %d, want 2", len(fnGreet))
	}
	last := fnGreet[1].Messages[len(fnGreet[1].Messages)-1].Content
	if !strings.Contains(last, "never calls Greet") {
		t.Errorf("retry prompt = %q, want the refusal", last)
	}
}
```

Check how `TestATestWriterNodeGetsAFreshRoundAfterTheChainIsExhausted` counts calls: the first retry is conversational and carries the error in the last message. If the greeter contract lists a different number of errors than the two tests above, match it (read `testdata/greeter/testwrite.fn-greet.txt` for the shape).

- [ ] **Step 6: Run the new tests, then the whole planner suite**

Run: `go test ./build/planner -run 'TestCheckTestExercises|TestATestWriterFakeIsRefused'` then `go test -count=1 -timeout 30m ./build/planner`
Expected: PASS. A fixture that now fails for "never calls" or "cannot fail" is a fixture this check changed the meaning of. Fix it under the spec's rule: a fixture standing for a good reply gets a real call and a `t.Fatalf`; a fixture standing for a bad reply keeps its original defect and also gets a call and an assertion, so its test still fails for the original reason. Known candidates: `testdata/greeter/testwrite.fn-name-error-error.txt`, `testdata/greeter-badtest/testwrite.fn-greet.2.txt`, `e2e_test.go:381`, `refusal_test.go:16`, `revise_test.go:228`. List every fixture you changed in the report.

- [ ] **Step 7: Build gate, then commit**

```bash
cd gophermind-lib && go build ./... && go vet ./build/planner && gofmt -l build/planner
git add gophermind-lib/build/planner/exercise.go gophermind-lib/build/planner/exercise_internal_test.go gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/e2e_test.go
# plus every fixture file you changed, by path
git commit -m "fix(planner): the test writer refuses a file that never calls the function or cannot fail"
```

---

### Task 3: Prompt wording and unexported helpers out of the view

**Files:**
- Modify: `gophermind-lib/build/planner/prompts/testwriter.md:7`
- Modify: `gophermind-lib/build/planner/testwriter_prompt_internal_test.go` (one new test)
- Create: `gophermind-lib/build/planner/depsview.go`
- Create: `gophermind-lib/build/planner/depsview_internal_test.go`
- Modify: `gophermind-lib/build/planner/testwriter.go:552` (the `renderTestwriterBrief` call in `writeTests`)

**Interfaces:**
- Consumes: `withReceiverDecl(c, view)` (`testwriter.go:488`), `testwriterView(d)` (`finish.go:59`), `internalTarget` and `whyNone`, `PackageClause(dir)`.
- Produces: `func withoutUnexportedDeps(view map[string]any, sig, pkgDir string) map[string]any`.

- [ ] **Step 1: Write the failing prompt test** in `testwriter_prompt_internal_test.go`:

```go
func TestTestwriterPromptStatesTwoRules(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Fatal(err)
	}
	p := string(b)
	if !strings.Contains(p, "Two rules you must meet:") || strings.Contains(p, "Exactly one rule") {
		t.Error("testwriter.md does not state two rules")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**, then change line 7 of `prompts/testwriter.md` from `Exactly one rule you must meet:` to `Two rules you must meet:`. Run again: PASS.

- [ ] **Step 3: Write the failing view tests** in `depsview_internal_test.go`:

```go
func depsView(sigs ...any) map[string]any {
	return map[string]any{"id": "fn-run", "context": map[string]any{"dependency_signatures": sigs, "constraints": []any{"c"}}}
}

const linkDecl = "// Link is one URL.\ntype Link struct {\n\tURL string\n}"
const parseFlagsSig = "// Parses flags.\nfunc parseFlags(args []string) (bool, string, error)"
const allSig = "// All checks links.\nfunc All(links []Link) []Result"

func TestWithoutUnexportedDepsDropsHelpersForAnExportedTarget(t *testing.T) {
	in := depsView(linkDecl, parseFlagsSig, allSig)
	out := withoutUnexportedDeps(in, "func Run(args []string) int", "cli")
	got := out["context"].(map[string]any)["dependency_signatures"].([]any)
	if len(got) != 2 || got[0] != linkDecl || got[1] != allSig {
		t.Errorf("dependency_signatures = %v", got)
	}
	if n := len(in["context"].(map[string]any)["dependency_signatures"].([]any)); n != 3 {
		t.Errorf("input was mutated: %d entries", n)
	}
	if out["context"].(map[string]any)["constraints"] == nil {
		t.Error("constraints were dropped")
	}
}

func TestWithoutUnexportedDepsKeepsThemForAnUnexportedTargetAndMain(t *testing.T) {
	for _, c := range []struct{ sig, dir string }{{"func run(args []string) int", "cli"}, {"func Run(args []string) int", "cmd/linkcheck"}} {
		out := withoutUnexportedDeps(depsView(linkDecl, parseFlagsSig), c.sig, c.dir)
		if got := out["context"].(map[string]any)["dependency_signatures"].([]any); len(got) != 2 {
			t.Errorf("%s in %s: dependency_signatures = %v, want both kept", c.sig, c.dir, got)
		}
	}
}

func TestWithoutUnexportedDepsKeepsWhatItCannotParse(t *testing.T) {
	out := withoutUnexportedDeps(depsView("not go at all"), "func Run() int", "cli")
	if got := out["context"].(map[string]any)["dependency_signatures"].([]any); len(got) != 1 {
		t.Errorf("dependency_signatures = %v", got)
	}
}
```

- [ ] **Step 4: Run them to verify they fail** (undefined `withoutUnexportedDeps`).

- [ ] **Step 5: Implement `withoutUnexportedDeps` in `depsview.go`**

`func withoutUnexportedDeps(view map[string]any, sig, pkgDir string) map[string]any`. Return `view` unchanged when `internalTarget(sig)` gives `why != whyNone`, or `PackageClause(pkgDir) == "main"`, or there are no signatures. Read `dependency_signatures` as `[]any` or `[]string` the way `withReceiverDecl` does. An entry is dropped when `parser.ParseFile` of `"package p\n" + entry` yields a `*ast.FuncDecl` whose `Name.Name` is not `ast.IsExported`; everything else (types, exported functions, text that does not parse) is kept. Copy `view` and its `context` map before changing them, as `withReceiverDecl` does. Doc comment: an external test package cannot call an unexported helper and the prompt forbids testing it, so its signature is noise; a test in the same package (unexported target, main package) may still need it.

- [ ] **Step 6: Hook it into `writeTests`.** Line 552 becomes `renderTestwriterBrief(mustJSON(withoutUnexportedDeps(withReceiverDecl(c, testwriterView(d)), sig, path.Dir(file))), ...)`. `sig` is currently read at line 562; move that read above line 552 (it only reads `ct`).

- [ ] **Step 7: Run the tests, the build gate, and commit**

Run: `go test ./build/planner -run 'TestTestwriterPrompt|TestWithoutUnexportedDeps|TestTestwriter'`
Expected: PASS.

```bash
cd gophermind-lib && go build ./... && go vet ./build/planner && gofmt -l build/planner
git add gophermind-lib/build/planner/prompts/testwriter.md gophermind-lib/build/planner/testwriter_prompt_internal_test.go gophermind-lib/build/planner/depsview.go gophermind-lib/build/planner/depsview_internal_test.go gophermind-lib/build/planner/testwriter.go
git commit -m "fix(planner): the test-writer prompt states two rules and omits unexported helpers"
```

---

### Task 4: Syntax refusals list every error with its line; compile refusals report up to ten

**Files:**
- Create: `gophermind-lib/build/planner/syntaxlist.go`
- Create: `gophermind-lib/build/planner/syntaxlist_internal_test.go`
- Modify: `gophermind-lib/build/planner/testwriter.go:985-990` (the parse call and its error in `checkTestSourceIn`)
- Modify: `gophermind-lib/build/planner/compilecheck.go:243` (`if len(msgs) == 3`)
- Modify: `gophermind-lib/build/planner/testwriter_internal_test.go:149` (the `"not Go, valid start"` case of `TestTestwriterErrorsNeverQuoteTheReply`)
- Modify if a test pins the cap of 3: `gophermind-lib/build/planner/compilecheck_test.go`

**Interfaces:**
- Consumes: `syntaxDetail(msg string) string` and `scanner.ErrorList` handling in `safeerr.go:252-277`, `capLine` and `maxQuotedLine` in `compilecheck.go:315-337`.
- Produces: `func syntaxErrs(err error, src string) string`, `const maxSyntaxErrors = 10`, `const maxCompileMessages = 10`.

- [ ] **Step 1: Write the failing tests** in `syntaxlist_internal_test.go`:

```go
func syntaxList(t *testing.T, src string) string {
	t.Helper()
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution|parser.AllErrors)
	if err == nil {
		t.Fatal("source parsed")
	}
	return syntaxErrs(err, src)
}

func TestSyntaxErrsQuotesTheOffendingLine(t *testing.T) {
	got := syntaxList(t, "package p\n\nfunc f() {\n\tx := (1\n}\n")
	want := "syntax error at line 4, column 9: expected ')', found newline: x := (1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyntaxErrsListsEveryErrorOnItsOwnLine(t *testing.T) {
	got := syntaxList(t, "package p\n\nvar a = ]\n\nvar b = 1\n\nvar c = ]\n")
	for _, want := range []string{"2 syntax errors", "line 3, column", "var a = ]", "line 7, column", "var c = ]"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "var b") {
		t.Errorf("got %q, quotes a line that has no error", got)
	}
}

func TestSyntaxErrsCapsTheList(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n")
	for i := 0; i < 14; i++ {
		fmt.Fprintf(&b, "\nvar v%d = ]\n", i)
	}
	got := syntaxList(t, b.String())
	if strings.Count(got, "line ") > maxSyntaxErrors || !strings.Contains(got, "more") {
		t.Errorf("got %q, want at most %d entries and a count of the rest", got, maxSyntaxErrors)
	}
}

func TestSyntaxErrsCapsTheQuotedLine(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := syntaxList(t, "package p\n\nvar x = ] // "+long+"\n")
	if strings.Contains(got, long) {
		t.Errorf("got %q, want the line cut to %d characters", got, maxQuotedLine)
	}
}
```

Note: go/parser may report a different count for the 14-line file (it stops after 10 errors on its own, then adds a "too many errors" entry). Adjust `TestSyntaxErrsCapsTheList` to what the parser actually produces, keeping the two assertions: never more than `maxSyntaxErrors` entries, and a note that more exist when they do. Say what you observed in the report.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./build/planner -run 'TestSyntaxErrs'`
Expected: compile error, `syntaxErrs` and `maxSyntaxErrors` undefined.

- [ ] **Step 3: Implement `syntaxErrs` in `syntaxlist.go`**

`const maxSyntaxErrors = 10`. `func syntaxErrs(err error, src string) string`: if `err` is not a `scanner.ErrorList`, return `syntaxErr(err)`. Otherwise walk the list in order, skip an entry whose line was already reported (cascades), and for each kept entry build `line N, column M` plus `: ` and `syntaxDetail(msg)` when it is not empty, plus `: ` and `capLine(lines[N-1], maxQuotedLine)` when the line exists and is not blank. Skip an entry whose message is "too many errors". One entry renders as `syntax error at line N, column M[: detail][: line]` (the same shape `syntaxErr` gives, with the line appended). Several render as `K syntax errors: ` plus the entries joined by `; `, with ` and R more` appended when more than `maxSyntaxErrors` were kept. Doc comment: the quoted text is the model's own test file; the brief is never quoted.

- [ ] **Step 4: Use it in `checkTestSourceIn`.** Add `parser.AllErrors` to the parse flags at `testwriter.go:987` and change the error at line 989 to `fmt.Errorf("test file (%d bytes) is not valid Go: %s", len(src), syntaxErrs(err, src))`.

- [ ] **Step 5: Raise the compile cap.** In `compilecheck.go`, add `const maxCompileMessages = 10` beside `maxQuotedLine` and change `if len(msgs) == 3` to `if len(msgs) == maxCompileMessages`. Run `grep -n '3' compilecheck_test.go | grep -i 'diag\|msgs\|three'`; if a test pins three diagnostics, move it to ten.

- [ ] **Step 6: Keep the canary invariant.** In `TestTestwriterErrorsNeverQuoteTheReply`, replace the `"not Go, valid start"` case so the canary sits on a line of its own and the syntax error on another:

```go
"not Go, valid start":   sub("func TestGreet(t *testing.T) {", "var _ = \""+canary+"\"\n\nfunc TestGreet(t *testing.T) { 1 +"),
```

The error quotes only the broken line, so the canary must stay out of the message.

- [ ] **Step 7: Run the tests, the build gate, and commit**

Run: `go test ./build/planner -run 'TestSyntaxErr|TestTestwriterErrorsNeverQuoteTheReply|TestCheckTestSource|TestCompile|Compile'`
Expected: PASS.

```bash
cd gophermind-lib && go build ./... && go vet ./build/planner && gofmt -l build/planner
git add gophermind-lib/build/planner/syntaxlist.go gophermind-lib/build/planner/syntaxlist_internal_test.go gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/compilecheck.go gophermind-lib/build/planner/testwriter_internal_test.go
# plus compilecheck_test.go if you changed it
git commit -m "fix(planner): a syntax refusal lists every error with its line; compile refusals report ten"
```

---

### Task 5: Refuse a test file that names a real host

**Files:**
- Create: `gophermind-lib/build/planner/nethost.go`
- Create: `gophermind-lib/build/planner/nethost_internal_test.go`
- Modify: `gophermind-lib/build/planner/testwriter.go` (the `parse` closure in `writeTests`, right after the `checkTestExercises` call Task 2 added)
- Modify: `gophermind-lib/build/planner/prompts/testwriter.md` (the last bullet of the `Do not:` list, "depend on ordering, timing, randomness, or the network")
- Modify: `gophermind-lib/build/planner/testwriter_prompt_internal_test.go` (one new test)

**Interfaces:**
- Consumes: `capLine` and `maxQuotedLine` (`compilecheck.go`).
- Produces: `func checkNoRealHost(src string) error`.

- [ ] **Step 1: Write the failing tests** in `nethost_internal_test.go`:

```go
func hostSrc(body string) string {
	return "package cli_test\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {\n\t" + body + "\n}\n"
}

func TestCheckNoRealHostRefusesALiteralHost(t *testing.T) {
	for _, c := range []struct{ name, body, host string }{
		{"https host", `_ = "https://example.com/redir"`, "example.com"},
		{"http host with a port", `_ = "http://this-does-not-exist-12345.com:8080/x"`, "this-does-not-exist-12345.com:8080"},
		{"raw string fixture", "_ = `- [a](http://example.org/a)`", "example.org"},
	} {
		err := checkNoRealHost(hostSrc(c.body))
		if err == nil || !strings.Contains(err.Error(), "line 6") || !strings.Contains(err.Error(), c.host) || !strings.Contains(err.Error(), "srv.URL") {
			t.Errorf("%s: err = %v, want a refusal naming line 6, %s and srv.URL", c.name, err, c.host)
		}
	}
}

func TestCheckNoRealHostAcceptsLoopbackFormatVerbsAndComments(t *testing.T) {
	for _, body := range []string{
		`_ = "http://127.0.0.1:8080/x"`,
		`_ = "http://localhost/x"`,
		`_ = "http://[::1]/x"`,
		`_ = "http://%s/a"`,
		`_ = "http://" + "x"`,
		`srv := "x"; _ = srv + "/a"`,
		`// see http://example.com/docs`,
		`_ = "no scheme here example.com"`,
	} {
		if err := checkNoRealHost(hostSrc(body)); err != nil {
			t.Errorf("%s: refused: %v", body, err)
		}
	}
}

func TestTestwriterPromptForbidsARealHost(t *testing.T) {
	b, err := promptFS.ReadFile("prompts/testwriter.md")
	if err != nil {
		t.Fatal(err)
	}
	if p := string(b); !strings.Contains(p, "never names a real host") || !strings.Contains(p, "srv.URL") {
		t.Error("testwriter.md does not forbid a real host")
	}
}
```

Put `TestTestwriterPromptForbidsARealHost` in `testwriter_prompt_internal_test.go`, the other two in `nethost_internal_test.go`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./build/planner -run 'TestCheckNoRealHost|TestTestwriterPromptForbidsARealHost'`
Expected: compile error, `checkNoRealHost` undefined; the prompt test fails.

- [ ] **Step 3: Implement `checkNoRealHost` in `nethost.go`**

`func checkNoRealHost(src string) error`. Parse with `parser.SkipObjectResolution`; a parse error returns nil (reported earlier). `ast.Inspect` for `*ast.BasicLit` with `Kind == token.STRING`; unquote with `strconv.Unquote` (a raw string unquotes too); on an unquote error use the literal text. Find every match of `https?://([^/\s"'` + "`" + `?#]+)`; the host (group 1) is allowed when, lower-cased and with any `:port` removed, it is `localhost`, `127.0.0.1`, `::1`, `[::1]`, `0.0.0.0`, or when it contains `%` (a format verb). The first disallowed host refuses the file with
`fmt.Errorf("test file reaches the network at line %d: a test never names a real host (%s); serve fixtures with httptest.NewServer and build every URL from srv.URL, or use http://127.0.0.1", n, capLine(host, 60))`
where `n` is `fset.Position(lit.Pos()).Line`. Doc comment: the host is the model's own text; the brief is never quoted.

- [ ] **Step 4: Hook it into `writeTests`.** In the `parse` closure, right after the `checkTestExercises` call: `if err := checkNoRealHost(src); err != nil { return err }`.

- [ ] **Step 5: Change the prompt.** In `prompts/testwriter.md`, replace the bullet

`- depend on ordering, timing, randomness, or the network. Fake `*http.Client` transports and `Store` implementations inline in the test file when needed.`

with

`- depend on ordering, timing, randomness, or the network: a test never names a real host. Serve fixtures with `httptest.NewServer` and build every URL from `srv.URL`; a literal `http://` host other than `127.0.0.1` or `localhost` is refused. Fake `*http.Client` transports and `Store` implementations inline in the test file when needed.`

- [ ] **Step 6: Run the tests, the whole planner suite, the build gate, and commit**

Run: `go test ./build/planner -run 'TestCheckNoRealHost|TestTestwriterPrompt'` then `go test -count=1 -timeout 30m ./build/planner`
Expected: PASS. A fixture that now fails for "reaches the network" is changed to a loopback host; list it in the report.

```bash
cd gophermind-lib && go build ./... && go vet ./build/planner && gofmt -l build/planner
git add gophermind-lib/build/planner/nethost.go gophermind-lib/build/planner/nethost_internal_test.go gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/prompts/testwriter.md gophermind-lib/build/planner/testwriter_prompt_internal_test.go
git commit -m "fix(planner): the test writer refuses a file that names a real host"
```
