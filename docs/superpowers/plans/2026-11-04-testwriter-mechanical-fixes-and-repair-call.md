# Test-writer mechanical fixes and repair call Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A test file refused today for an unused variable or a missing standard import passes with no model call, and a file with other syntax or compile errors gets one focused repair call before it costs a round.

**Architecture:** The compile check gains a structured diagnostic list. A mechanical pass (new file `mechfix.go`) applies the two one-fix rewrites and recompiles. A shared settle function (new file `settle.go`) runs every post-parse check, the mechanical pass, one repair call (new prompt `testwriter_fix.md`, stage `testwrite:<id>:fix`) and the final compile; the test-writer closure and the revise test-rewrite closure both call it.

**Tech Stack:** Go 1.25, go/ast, go/parser, go/format, the planner's existing seams (`lookGo`, `runGo`, the fixture provider).

**Spec:** `docs/superpowers/specs/2026-11-04-testwriter-mechanical-fixes-and-repair-call-design.md`

All paths below are relative to `gophermind-lib/` unless they start with `docs/`. Run tests from `gophermind-lib/`.

## Global Constraints

- No em dashes, no emojis in code, comments, prompts, commit messages or docs.
- A refusal never quotes the brief; it may quote the model's own file and the compiler's text (existing rule).
- Refusals report the diagnostics of the first compile, on the model's own file, minus those the mechanical pass fixed; errors of a fixed or repaired file go to events only.
- One repair call per model reply; the router never retries a repair call (its parse always returns nil).
- Event texts are fixed: `test_file_mechanical_fix: <kind> <name> in the test file of node <id>`, `test_file_repaired: the test file of node <id> compiled after a repair call`, `test_file_repair_unusable: the repair reply for node <id> held no Go file` (all `events.KindWarning`).
- The repair prompt closes with exactly: `Reply with the complete corrected Go file in one fenced go block and nothing else. Change only what the errors require; keep every test case, helper and assertion.`
- Commit per task with explicit `git add` paths, trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.
- Never touch `~/.gophermind`; never install or push.

## Review Focus

1. A variable declared in a `for ... range` header and never used: `_ = x` must land inside the loop body, not after the loop (Task 2 test `header-declared variable`).
2. A diagnostic for a line the mechanical pass could not fix must still reach the refusal with its original line number after another fix inserted lines above it (Task 2 test `refusal keeps original line numbers`).
3. A repair reply that changes the package clause or holds no Go file must not replace the file; the original errors are refused (Task 4 test `unusable repair reply`).
4. A repair whose file still fails must record one refusal for the original errors, not two, and the retry note must say the repair did not help (Task 4 test `failed repair refuses original errors`).
5. The revise rewrite now runs the host rule: a rewritten test naming `example.com` in a fetched fixture is refused where it was accepted before (Task 5 test).

---

### Task 1: Structured compile diagnostics

**Files:**
- Modify: `build/planner/compilecheck.go` (refuseCompile, lines 210 to 245)
- Test: `build/planner/compilediag_internal_test.go` (create)

**Interfaces:**
- Produces:
  ```go
  type compileDiag struct {
      Line, Col int
      Msg       string // the compiler's text after "file:line:col: ", with diagContinuation appended
  }
  // compileDiags lists the diagnostics of `go test` output out that are in the
  // file base, in output order, deduplicated by line, capped at maxCompileMessages.
  func compileDiags(base, out string) []compileDiag
  // refuseCompileDiags is refuseCompile for an already extracted list: nil for an empty list.
  func refuseCompileDiags(c *contract.Contracts, dir, src string, diags []compileDiag) error
  ```
  `refuseCompile(c, dir, base, src, out)` keeps its signature and body-equivalence: the module-lookup early return, then `refuseCompileDiags(c, dir, src, compileDiags(base, out))`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCompileDiagsKeepsOnlyTheTestFileInOrder(t *testing.T) {
	out := "# m/p\np/zz_other0_test.go:3:1: other\n./p/fn_x_test.go:9:2: declared and not used: x\np/fn_x_test.go:9:9: second on same line\np/fn_x_test.go:4:2: \"fmt\" imported and not used\n"
	got := compileDiags("fn_x_test.go", out)
	want := []compileDiag{{9, 2, "declared and not used: x"}, {4, 2, `"fmt" imported and not used`}}
	if !reflect.DeepEqual(got, want) { t.Fatalf("got %+v", got) }
}

func TestRefuseCompileIsUnchangedByTheSplit(t *testing.T) {
	// Same inputs as TestCompileMessageQuotesTheDiagnosticAndTheLine in compilecheck_test.go; assert the error text is identical before and after.
}
```

The second test pins `refuseCompile(c, dir, base, src, out).Error()` to the exact string the existing test expects (copy its inputs and expected text).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./build/planner -run 'TestCompileDiags|TestRefuseCompileIsUnchanged' -count=1`
Expected: FAIL, `undefined: compileDiags`.

- [ ] **Step 3: Implement `compileDiags` and `refuseCompileDiags`, rewrite `refuseCompile` as their composition**

`compileDiags` uses the existing `diagRE` and `diagContinuation`; `refuseCompileDiags` keeps the `compileMessage` call per diagnostic.

- [ ] **Step 4: Run the compile-check tests**

Run: `go test ./build/planner -run 'Compile|Refuse' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add build/planner/compilecheck.go build/planner/compilediag_internal_test.go
git commit -m "refactor(planner): compile diagnostics are a list before they are a refusal"
```

### Task 2: Mechanical fixes

**Files:**
- Create: `build/planner/mechfix.go`
- Test: `build/planner/mechfix_internal_test.go`

**Interfaces:**
- Consumes: `compileDiag`, `stdTestPackages`, `missingStdImport(diag string) string` (compilecheck.go), `format.Source`.
- Produces:
  ```go
  // mechanicalFix is one applied fix: Kind is "unused variable" or "missing import", Name the variable or package.
  type mechanicalFix struct{ Kind, Name string }
  // applyMechanicalFixes rewrites src for every diagnostic that has one correct fix and returns the
  // formatted result, the fixes applied in diagnostic order, and the diagnostics it did not fix.
  // A src that does not parse, or a fixed src that does not format, is returned unchanged with no fixes.
  func applyMechanicalFixes(src string, diags []compileDiag) (string, []mechanicalFix, []compileDiag)
  ```

Rules (from the spec):
- `declared and not used: x` (regexp `^declared and not used: (\w+)$`): insert `_ = x` where x is in scope. Find the declaring node by the diagnostic's line and column (the identifier at that position). If its enclosing statement is a plain statement in a block body, insert after that statement. If it is in the init of an `if`, `for`, `switch` or `select` (a `for` range or three-clause header, a type-switch assign), insert as the first statement of the body; for a type switch, as the first statement of every clause. One insertion per name.
- `undefined: pkg` with `missingStdImport(msg) != ""` and no existing import whose name is `pkg`: add `"pkg"` to the import block (every member of `stdTestPackages` is a top-level path, so the path is the name).
- Everything else is returned in the unfixed list.

- [ ] **Step 1: Write the failing tests**

Table test `TestApplyMechanicalFixes` with cases (src, diags, want substrings, want fixes, want unfixed count):

```go
{name: "unused variable after a plain statement",
 src: "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\twantErr := true\n\tt.Log(1)\n}\n",
 diags: []compileDiag{{6, 2, "declared and not used: wantErr"}},
 want: "wantErr := true\n\t_ = wantErr\n\tt.Log(1)", fixes: []mechanicalFix{{"unused variable", "wantErr"}}},
{name: "header-declared variable",
 src: "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tfor _, tc := range []int{1} {\n\t\tt.Log(1)\n\t}\n}\n",
 diags: []compileDiag{{6, 9, "declared and not used: tc"}},
 want: "range []int{1} {\n\t\t_ = tc\n\t\tt.Log(1)", fixes: []mechanicalFix{{"unused variable", "tc"}}},
{name: "type switch variable goes in every clause",
 src: "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tvar v any\n\tswitch x := v.(type) {\n\tcase int:\n\t\tt.Log(1)\n\tcase string:\n\t\tt.Log(2)\n\t}\n}\n",
 diags: []compileDiag{{7, 9, "declared and not used: x"}},
 want: "case int:\n\t\t_ = x\n\t\tt.Log(1)\n\tcase string:\n\t\t_ = x\n\t\tt.Log(2)", fixes: []mechanicalFix{{"unused variable", "x"}}},
{name: "missing standard import",
 src: "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Log(strings.ToUpper(\"a\")) }\n",
 diags: []compileDiag{{5, 34, "undefined: strings"}},
 want: "\"strings\"", fixes: []mechanicalFix{{"missing import", "strings"}}},
{name: "undefined non-standard name is left",
 src: "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = srv }\n",
 diags: []compileDiag{{5, 32, "undefined: srv"}}, same: true, unfixed: 1},
{name: "type mismatch is left",
 diags: []compileDiag{{5, 1, "cannot use x (variable of type int) as string value"}}, same: true, unfixed: 1},
{name: "file that does not parse is returned unchanged",
 src: "package p\n\nfunc TestX( {\n", diags: []compileDiag{{3, 1, "declared and not used: x"}}, same: true, unfixed: 1},
```

And `TestApplyMechanicalFixesFormatsTheResult`: after a fix, `format.Source(result)` equals result.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./build/planner -run TestApplyMechanicalFixes -count=1`
Expected: FAIL, `undefined: applyMechanicalFixes`.

- [ ] **Step 3: Implement `mechfix.go`**

Parse with `parser.ParseFile(fset, "", src, parser.ParseComments)`. For the unused variable: `ast.Inspect` to find the `*ast.Ident` whose position equals the diagnostic's line and column; walk the enclosing path (keep a stack in the Inspect) to the nearest `*ast.BlockStmt` or clause, choosing the insertion offset by the rules above; insert `_ = name` text by byte offsets, highest offset first, then `format.Source`. For the import: add the spec to the first import GenDecl (create one after the package clause when there is none).

- [ ] **Step 4: Run the tests**

Run: `go test ./build/planner -run 'TestApplyMechanicalFixes' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add build/planner/mechfix.go build/planner/mechfix_internal_test.go
git commit -m "feat(planner): an unused variable or a missing standard import is fixed before the file is refused"
```

### Task 3: The repair prompt and call

**Files:**
- Create: `build/planner/prompts/testwriter_fix.md`
- Create: `build/planner/repaircall.go`
- Test: `build/planner/repaircall_internal_test.go`

**Interfaces:**
- Consumes: `render(name, data)` (prompts.go), `splitSourceReply` (sourcereply.go), `callSpec`, `p.call`, `hasPackageClause` or `parser.ParseFile(... PackageClauseOnly)`.
- Produces:
  ```go
  // repairSpec is cs with the stage suffix ":fix"; every other field is copied.
  func repairSpec(cs callSpec) callSpec
  // renderRepairPrompt fills prompts/testwriter_fix.md with the file and the error text.
  func renderRepairPrompt(src, errText string) (string, error)
  // repairTestFile asks the model once for a corrected file. ok is false when the call failed,
  // the reply held no Go file, or the file's package clause differs from src's; then fixed is "".
  func (p *Planner) repairTestFile(ctx context.Context, r *run, cs callSpec, src, errText string) (fixed string, ok bool, err error)
  ```
  `err` is non-nil only for a planner error (context cancelled, state write failure), never for a bad reply.

Prompt `testwriter_fix.md`:

```
This Go test file does not compile. Fix it.

```go
{{.File}}
```

Errors:
{{.Errors}}

Reply with the complete corrected Go file in one fenced go block and nothing else. Change only what the errors require; keep every test case, helper and assertion.
```

(`File` and `Errors` are not trusted keys, so `render` neutralizes them; that is wanted.)

- [ ] **Step 1: Write the failing tests**

```go
func TestRepairSpecAddsTheFixStage(t *testing.T) {
	cs := callSpec{stage: "testwrite:fn-x", taskType: "testwrite", nodeID: "fn-x", nodeClass: "pure", tier: router.TierStandard, scope: router.ScopeNode, maxTokens: 7}
	got := repairSpec(cs)
	cs.stage = "testwrite:fn-x:fix"
	if !reflect.DeepEqual(got, cs) { t.Fatalf("got %+v", got) }
}

func TestRepairPromptHoldsTheFileAndTheErrorsAndNothingElse(t *testing.T) {
	got, err := renderRepairPrompt("package p\n\nfunc TestX(t *testing.T) {}\n", "unused import at line 4: \"fmt\" imported and not used")
	// assert: contains "```go\npackage p", contains the error line, ends with the exact closing sentence from Global Constraints, does not contain "brief" or "contract".
}
```

For `repairTestFile`, use the planner's fixture rig (see `newRig` in e2e_test.go and `planned` in revise_test.go for how a `*Planner` with a run is built in tests; the fixture provider serves stage `testwrite:fn-greet:fix` from `testwrite.fn-greet.fix.txt` via `variant`). Three cases:
- fixture holds a fenced go file with the same package clause: `ok == true`, `fixed` is the block body, and `g.stagesCalled()` contains `testwrite:fn-greet:fix` exactly once.
- fixture is `nope` (no Go file): `ok == false`, `fixed == ""`, the stage was called once (no router retry).
- fixture holds `package other` file: `ok == false`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./build/planner -run 'TestRepair' -count=1`
Expected: FAIL, undefined names.

- [ ] **Step 3: Implement**

`repairTestFile` calls `p.call(ctx, r, repairSpec(cs), prompt, parse)` where `parse` stores the text and returns nil always. Then `splitSourceReply`; accept the source when non-empty and its package clause (parsed with `parser.PackageClauseOnly`) equals src's.

- [ ] **Step 4: Run the tests**

Run: `go test ./build/planner -run 'TestRepair' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add build/planner/prompts/testwriter_fix.md build/planner/repaircall.go build/planner/repaircall_internal_test.go
git commit -m "feat(planner): a repair call sees a test file and its errors only"
```

### Task 4: The shared settle function, wired into the test writer

**Files:**
- Create: `build/planner/settle.go`
- Modify: `build/planner/testwriter.go` (the `parse` closure at lines 566 to 604; `writtenTests` at line 76; the `nodeRounds`-independent record write where `tests, source` are stored)
- Test: `build/planner/settle_internal_test.go`, `build/planner/e2e_test.go` (two new tests)

**Interfaces:**
- Consumes: Task 1 to 3 names; `checkTestExercises`, `reachesNetwork`, `importsNet`, `checkNoRealHost`, `otherTestSources`, `resolveSharedNames`, `richDeclaredName`, `dropUnusedImports`, `moduleImportNames`, `compileCheckTestFile` replaced by a diagnostics-returning variant (see below), `p.emit`, `events.KindWarning`.
- Produces:
  ```go
  // settleInput is what both callers have after the reply's JSON part is parsed.
  type settleInput struct {
      c        *contract.Contracts
      id       string   // node id
      file     string   // contract file of the function, e.g. check/all.go
      rel      string   // repo-relative test file path
      funcName string   // TestAll
      sig      string   // the function's signature, for internalTarget
      others   []string // other accepted test sources of the package
      stubs    []string // stubSources(c, path.Dir(file))
  }
  type settled struct {
      src      string
      renamed  bool
      dropped  []string
      fixes    []mechanicalFix
      repaired bool
  }
  // settleTestFile runs every check after the JSON parse: exercises, host rule, shared names,
  // unused imports, format, compile, mechanical fixes, one repair call, compile. A nil error
  // means src compiled (or there is no go tool: errNoGo is swallowed after one warning by the caller
  // as today). The returned error is the refusal for the model's own file.
  func (p *Planner) settleTestFile(ctx context.Context, r *run, cs callSpec, in settleInput, src string) (settled, error)
  ```
  Also in compilecheck.go: `compileCheckDiags(c, rel, src, others) ([]compileDiag, error)` returning the diagnostics instead of the refusal (errNoGo as today; nil, nil when the check is skipped or passes); `compileCheckTestFile` becomes `refuseCompileDiags` over it, unchanged for callers.

Order inside `settleTestFile` (the spec's list):
1. `checkTestExercises(src, name)` with `name` from `internalTarget(in.sig)`.
2. `if reachesNetwork(in.c, in.id) || importsNet(src) { checkNoRealHost(src) }`.
3. `resolveSharedNames` (+ `richDeclaredName` on `errDeclaredName`).
4. `dropUnusedImports(src, moduleImportNames(in.c))`, `format.Source`.
5. `compileCheckDiags`. Empty list: done.
6. `applyMechanicalFixes`; emit one event per fix; if any fix applied, `compileCheckDiags` again. Empty list: done with `fixes`.
7. The refusal to report is `refuseCompileDiags(in.c, dir, src0, unfixedFromFirstCompile)` where `src0` is the file before mechanical fixes and `unfixedFromFirstCompile` is the first compile's list minus the diagnostics the pass fixed. Call this `refusal`.
8. `repairTestFile(ctx, r, cs, srcAfterFixes, refusal.Error())`. Not ok: emit `test_file_repair_unusable`, return `refusal`. Ok: run steps 1 to 6 on the repaired file (no second repair). Passes: emit `test_file_repaired`, return settled with `repaired = true`. Fails: return `fmt.Errorf("after a repair call: %w", refusal)` (the original errors, prefixed).

A syntax refusal happens before settle (in `parseTestwriteCounted`). To give it the repair call too, the caller passes the raw file: add to settle an entry point `settleSyntax(ctx, r, cs, in, rawFile string, synErr error)` that calls `repairTestFile(…, rawFile, synErr.Error())`, then runs the repaired file through `checkTestSourceIn`-level parsing and `settleTestFile`; on any failure returns `fmt.Errorf("after a repair call: %w", synErr)`. The test-writer closure calls it when `parseTestwriteCounted` returns an error whose text starts with `test file (` and contains `is not valid Go` and `splitSourceReply(text)` gave a non-empty source. (The builder may instead make `parseTestwriteCounted` return a typed `syntaxRefusal{src, err}`; either way the closure must be able to tell syntax from the other parse refusals.)

`writtenTests` gains `Fixes []string \`json:"fixes,omitempty"\`` (the `Kind + " " + Name` of each fix) and `Repaired bool \`json:"repaired,omitempty"\``; the test-writer stores them from `settled`.

- [ ] **Step 1: Write the failing tests**

`settle_internal_test.go`, built on the fixture rig with the greeter testdata (`testdata/greeter`), stage `testwrite:fn-greet`:

```go
// The model's file has an unused variable: it compiles after the mechanical fix, no repair call.
func TestSettleFixesAnUnusedVariableWithoutACall(t *testing.T)
//   reply = good fixture with "\tunused := 1\n" inserted as the first statement of TestGreet
//   assert: run completes, node record has Fixes == ["unused variable unused"], Repaired false,
//   stagesCalled has no "testwrite:fn-greet:fix", the written file contains "_ = unused",
//   events hold "test_file_mechanical_fix: unused variable unused in the test file of node fn-greet".

// A type error the pass cannot fix: one repair call, whose fixture is the good file; accepted, Repaired true.
func TestSettleRepairsACompileErrorWithOneCall(t *testing.T)
//   reply = good fixture with one assertion changed to compare a string with an int
//   fixture "testwrite.fn-greet.fix.txt" = the good file alone in a fenced go block
//   assert: count(stagesCalled, "testwrite:fn-greet:fix") == 1, Repaired true, refusals.jsonl has no testwrite:fn-greet entry,
//   events hold "test_file_repaired: the test file of node fn-greet compiled after a repair call".

// The repair reply still fails: one refusal, for the original errors, prefixed.
func TestSettleFailedRepairRefusesTheOriginalErrors(t *testing.T)
//   fix fixture = the bad file again; second testwrite fixture (.2.txt) = good
//   assert: refusals.jsonl has exactly one testwrite:fn-greet line, its text starts with "after a repair call: test file does not compile",
//   the quoted line number is the one in the model's own reply, the run completes with the .2 reply.

// The repair reply has no Go file: refusal is the original error, event says unusable.
func TestSettleUnusableRepairReply(t *testing.T)
//   fix fixture = "nope"; assert the event "test_file_repair_unusable: the repair reply for node fn-greet held no Go file"
//   and the refusal text has no "after a repair call" prefix.

// A syntax error gets the repair call too.
func TestSettleRepairsASyntaxError(t *testing.T)
//   reply = good fixture with one closing brace removed; fix fixture = good file; assert Repaired true, one :fix call.

// Refusal line numbers are the model's: an unfixed diagnostic below an inserted "_ = x" keeps its original line.
func TestSettleRefusalKeepsOriginalLineNumbers(t *testing.T)
//   reply has an unused variable on line N and a type error on line M > N; fix fixture = "nope";
//   assert the refusal names "line M", not M+1.
```

Use `refusal_test.go` and `e2e_test.go` for how refusals.jsonl and events are read in tests.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./build/planner -run 'TestSettle' -count=1`
Expected: FAIL, undefined `settleTestFile` (or the rig compiles but Fixes is missing).

- [ ] **Step 3: Implement `settle.go`, `compileCheckDiags`, and rewire the test-writer closure**

The closure in `writeTests` becomes: parse the JSON and the file (`parseTestwriteCounted`); on a syntax refusal with a recoverable source call `settleSyntax`; otherwise call `settleTestFile`; store `tests, source, renamedHelpers, droppedImports, droppedComments` plus the new `fixes` and `repaired`. The `errNoGo` handling (one warning, accept the file) stays in the closure.

- [ ] **Step 4: Run the tests**

Run: `go test ./build/planner -run 'TestSettle|TestCompile|TestTestwriter|TestE2E' -count=1` then `go test ./build/planner -count=1` (about 8 minutes).
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add build/planner/settle.go build/planner/settle_internal_test.go build/planner/testwriter.go build/planner/compilecheck.go
git commit -m "feat(planner): the test writer fixes, then repairs, a test file before refusing it"
```

### Task 5: The revise rewrite uses the same settle function

**Files:**
- Modify: `build/planner/revise.go` (the closure at lines 470 to 510)
- Test: `build/planner/revise_test.go` (two new tests)

**Interfaces:**
- Consumes: `settleTestFile`, `settleInput`, `settled` from Task 4.

- [ ] **Step 1: Write the failing tests**

Model them on `TestReviseDropsAnUnusedImportFromTheRewrittenTest` (revise_test.go:632):

```go
// The rewritten test declares a variable it never uses: accepted after the mechanical fix, no :fix call.
func TestReviseFixesAnUnusedVariableInTheRewrittenTest(t *testing.T)
//   assert prop.Outcome == OutcomeRewritten and prop.TestSource contains "_ = unused".

// The rewritten test names a real host in a fetched fixture: refused (the host rule now runs in revise).
func TestReviseRefusesARealHostInTheRewrittenTest(t *testing.T)
//   the greeter contract has no network function, so make the rewritten test import "net/http" and hold
//   `http.Get("http://example.com")`; assert the proposal is not OutcomeRewritten and the refusal text contains "a test never names a real host".
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./build/planner -run 'TestRevise(Fixes|Refuses)' -count=1`
Expected: FAIL (the first: no `_ = unused`; the second: the rewrite is accepted).

- [ ] **Step 3: Replace the closure's body after `linkErrorTests` with a `settleTestFile` call**

Keep the secret check and `parseTestwriteIn` and `linkErrorTests` as they are; build `settleInput` from the closure's variables (`c`, `id`, `file`, `testFile`, `funcName`, the merged node's signature, `others`, `stubSources(c, path.Dir(file))`); the `errNoGo` rule stays as today.

- [ ] **Step 4: Run the revise and planner suites**

Run: `go test ./build/planner -run 'TestRevise' -count=1` then `go test ./build/planner -count=1`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add build/planner/revise.go build/planner/revise_test.go
git commit -m "feat(planner): the revise rewrite shares the test writer's fix and repair path"
```

### Task 6: Prompt and runs documentation

**Files:**
- Modify: `build/planner/prompts/testwriter.md` only if Task 3's prompt test needs a shared sentence (expected: no change).
- Modify: `docs/unification-overview.md` (the test-writer section: one paragraph on the mechanical pass and the repair call, the stage name `testwrite:<id>:fix`, and the three event texts).

- [ ] **Step 1: Add the paragraph**

- [ ] **Step 2: Verify the whole module**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1` from `gophermind-lib/`.
Expected: no output from gofmt, all packages `ok`.

- [ ] **Step 3: Commit**

```bash
git add docs/unification-overview.md
git commit -m "docs: the test writer's mechanical fixes and repair call"
```
