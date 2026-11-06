# Hello Pipeline Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A fresh GopherMind run of `examples/briefs/14-hello.md` builds, vets, tests and passes the three CLI cases with the local model, with every GopherMind-made contradiction removed deterministically before implementation.

**Architecture:** The planner keeps its one `package` field (a directory) and its first-wins merge, but a component reply is filtered and refused before merge, fixed interfaces and the entry call are enforced (Milestone 1 of the reliability plan, landing now), nodes carry inputs and tiers derived from the Go signature, coverage and the test writer check the brief's literals, acceptance commands are validated for status masking, and the executor's reply gate allows `os.Exit` only in `func main()` of package `main`, with a preflight that fails a node whose contract needs something the gate forbids.

**Tech Stack:** Go 1.22+, `gophermind-lib/build/{planner,packer,executor,acceptcheck,contract}`, fixture provider tests under `build/planner/testdata/{hello,slugify}`.

**Spec:** `docs/superpowers/specs/2026-11-05-hello-pipeline-fixes-brief.md` (John's 30 sections). Code facts in this plan come from the survey of 2026-11-05 (`.superpowers/hello-survey.md`, scratch); line numbers are approximate after Milestone 1 lands. Prerequisite: `docs/superpowers/plans/2026-11-05-planner-reliability-m1.md` complete (Task 2 whole brief in packets, Task 3 package mapping, Task 4 fixed interfaces and the foreign-function drop ruled during its fix round, Task 5 entry once, Task 6 slugify regression).

## What Milestone 1 already covers

| Brief section | Covered by |
|---|---|
| 4 fixed architecture extracted, 23 no stdin | M1 Task 4 `fixedInterfaces`, `applyFixedInterfaces`, the missing-interface defect round |
| 6 all requirements reach a function's packet | M1 Task 2 `componentBrief` level 0 and 1 |
| 7 foreign declaration rejected (`fn-main` from the cli pass) | M1 Task 4 fix round: `component_foreign_function_dropped` |
| 9 no `entry-point-main` from the batch pass | M1 Task 5 removes the entry injection |
| 5 package directory vs name | Survey: keep one `package` field; `PackageClause` is the only source of the Go name. Milestone B hardens `localChecks` |

## Global Constraints

- No em dashes, no emojis in code, prompts, docs or commit messages.
- Never special-case `hello`, a component name or a function name. Every rule is general.
- The hello brief is not weakened. Recorded fixture replies are never edited; a corrected reply goes in the next `.N.txt` slot and is listed in `testdata/hello/README.md` with its reason.
- Test first: each task's red test fails for the brief's reason before the fix.
- Before each commit: `cd gophermind-lib && go build ./... && go vet ./... && gofmt -l .` clean, and the package suites the task names.
- Never push, install, deploy or start a run unless John says so. The git wrapper blocks `push`, `checkout`, `switch`, `stash`, `reset`, `clean`, `restore`, `branch`, `worktree`.
- Commit trailers: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.

## Review Focus

1. A brief whose Architecture fixes an entry call with a reader (`Run(args, stdin, stdout, stderr)`, the slugify brief): `main` must still pass `os.Stdin`, and `cmd/slugify`'s extra `run` helper must not be dropped as an entry extra. Pinned by Task 2's slugify assertions.
2. A model that re-emits an identical record in a continuation pass (`more: true`): still silent, never a conflict. Task 2 `TestIdenticalReplayIsSilent`.
3. A non-main package in a `cmd/` directory tree (`cmd/hello/internal/x`): `os.Exit` stays refused; the rule is package clause `main` plus `func main()`, not the directory. Task 3 `TestExitRefusedOutsideMain`.
4. An acceptance command that reads `$?` legitimately after a `;` or newline (`hello a b; status=$?`): not `status_masked`. Task 4 `TestStatusCapturedAfterSemicolonIsClean`.
5. A feature body whose backticked literal is an input, not an output (`hello Ada` itself): the coverage literal audit must look for outputs and exit codes only, or it will demand `hello Ada` appear in a node's text. Task 7 extracts literals from "prints", "exits with code" and "returns" sentences only; `TestCoverageLiteralsSkipInvocations`.

---

## Milestone A: the hello run turns green

### Task 1: Hello planner regression on the recorded fixtures (brief 2, 28)

**Files:**
- Create: `gophermind-lib/build/planner/hello_fixture_test.go`
- Create: `gophermind-lib/build/planner/testdata/hello/contract.say-hello.2.txt` (corrected reply: the `say-hello` component with `Greeting(name string) string` in `greet.go`, doc stating `Hello, World!` for empty and `Hello, <name>!` otherwise, no other function)
- Modify: `gophermind-lib/build/planner/testdata/hello/README.md` (list the corrected reply and why: Milestone 1 Task 4's missing-interface defect round asks the pass again)
- Modify: `gophermind-lib/build/planner/harness_test.go:22-25` (consts `hello = "testdata/hello"`, `helloID = "hello-v1-20261105-r001"`)

**Interfaces:**
- Consumes: `newRigFor(t, approving(), hello, helloID)`, `g.plan(planner.Options{StopAfter: "contract"})`, `g.contracts()`, `g.sink.OfKind("warning")`, `g.stagesCalled()`.
- Produces: the test the rest of Milestone A keeps green.

- [ ] **Step 1: Write the failing test**

```go
func TestHelloContractKeepsTheFixedArchitecture(t *testing.T)
// after g.plan(StopAfter "contract") succeeds:
// functions by Go name: Greeting in package internal/greet with signature exactly
//   "func Greeting(name string) string"; Run in internal/cli with exactly
//   "func Run(args []string, stdout, stderr io.Writer) int"; main in cmd/hello, "func main()"
// no function signature contains "stdin"
// no function named FormatGreeting; no component id "entry-point-main"
// every function whose component is the cmd/hello component is named main
// the Run function's Uses contains the Greeting function's id
// stagesCalled has no "contract:outline:entry"
```

- [ ] **Step 2: Run it to see it fail for the brief's reasons**

Run: `cd gophermind-lib && go test ./build/planner/ -run TestHelloContractKeepsTheFixedArchitecture -count=1 -v`
Expected: FAIL. Record which assertions fail after Milestone 1 (expected: cmd/hello still has `run`; possibly `Run` lacks `Uses` of Greeting). Put the list in the commit message.

- [ ] **Step 3: Commit the red test**

```bash
git add gophermind-lib/build/planner/hello_fixture_test.go gophermind-lib/build/planner/harness_test.go gophermind-lib/build/planner/testdata/hello/contract.say-hello.2.txt gophermind-lib/build/planner/testdata/hello/README.md
git commit -m "test(planner): hello contract regression on the recorded r001 replies (red)"
```

### Task 2: Conflicts, replay and entry extras at merge (brief 7, 8, 9, 21)

**Files:**
- Modify: `gophermind-lib/build/planner/contract_stage.go` (`mergeByID` about line 944, `mergePass` about 1211, the component loop)
- Modify: `gophermind-lib/build/planner/align.go:108-116` (`entryComponentFor`: delete the `-main` suffix loop; a taken `entry-point` id is a conflict error)
- Modify: `gophermind-lib/build/planner/prompts/contract_component.md` (line 5 "Functions are small ... split it" and line 17 "There is no limit on the number of functions")
- Test: `gophermind-lib/build/planner/merge_conflict_test.go`

**Interfaces:**
- Produces: `mergeByID` returns a fourth value `conflicts []string` (ids whose canonical JSON differs from the stored record); identical re-emissions stay silent. `mergePass` returns an error `contract: conflicting declaration of <id> by component <c>` when `conflicts` is not empty, so the router re-asks and the stored doc is unchanged. When the pass component is the entry package and `entryCall` (M1 Task 4) is set, every function of the reply other than `main` is dropped with warning `entry_extra_function_dropped: <component>: <ids>`; without a fixed entry call nothing is dropped (slugify keeps `run`).
- Fixed calls (brief 24): for every Architecture bullet that fixes an interface and names a `pkg.Func` call (`archQualRE`), after the component pass the fixed function's `uses` must contain the id of the function named; when it does not, add it and warn `fixed_use_added: <id> uses <dep id>`. The hello `Run` therefore always uses `Greeting`. Name it `applyFixedUses(doc, arch) []string` in `fixediface.go`.
- Prompt text: replace line 5 with "A function exists because the brief names it or because a function would otherwise be hard to test. Do not add helpers to a package whose fixed interfaces already cover its behaviour." Replace line 17 with "Write every function the component needs and no more. If the reply is getting long, stop at a function boundary and set \"more\" to true; you will be asked to continue. Set \"more\" to false only when the component is complete." Add after it: "When the brief fixes an output line, state its exact bytes in the doc, including the trailing newline written as \\n."

- [ ] **Step 1: Write the failing tests**

```go
func TestSecondDifferentDeclarationIsAConflict(t *testing.T)
// mergePass on a doc holding fn-run with one signature, reply with fn-run and another signature:
// error contains "conflicting declaration of fn-run"; doc unchanged
func TestIdenticalReplayIsSilent(t *testing.T)
// same record again: no error, no warning, functions count unchanged
func TestEntryExtrasDroppedWhenTheCallIsFixed(t *testing.T)
// hello Architecture; entry-point reply with fn-main and fn-run: fn-run dropped,
// warning "entry_extra_function_dropped: entry-point: fn-run"
func TestEntryExtrasKeptWithoutAFixedCall(t *testing.T)
// greeter Architecture (no pkg.Func in the cmd bullet): fn-run kept
func TestEntryPointIdIsNeverSuffixed(t *testing.T)
// entryComponentFor with entry-point taken returns an error, never "entry-point-main"
func TestComponentPromptLimitsHelpers(t *testing.T)
// render contains "no more" and "exact bytes", not "no limit on the number of functions"
func TestFixedCallBecomesAUse(t *testing.T)
// hello Architecture; Run with empty uses: after applyFixedUses, Run uses the Greeting id; warning fixed_use_added
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/planner/ -run 'TestSecondDifferent|TestIdenticalReplay|TestEntryExtras|TestEntryPointId|TestComponentPromptLimits' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** as the Interfaces block says. `mergeByID` already distinguishes identical from different JSON; return the different ones instead of only noting them.

- [ ] **Step 4: Run the planner suite, including Task 1's test and the slugify and greeter rigs**

Run: `cd gophermind-lib && go test ./build/planner/ -count=1`
Expected: PASS, with `TestHelloContractKeepsTheFixedArchitecture` now green except any assertion Task 1 recorded as owned by a later task.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/align.go gophermind-lib/build/planner/fixediface.go gophermind-lib/build/planner/prompts/contract_component.md gophermind-lib/build/planner/merge_conflict_test.go
git commit -m "fix(planner): conflicting declarations refuse the reply; fixed entry keeps only main; fewer helpers"
```

### Task 3: `os.Exit` for the validated entry only, policy preflight, repeated refusal ends the ladder (brief 18, 19, 20)

**Files:**
- Modify: `gophermind-lib/build/packer/reply.go` (`Expect` gains `AllowExit bool`; `gate` skips the `os`/`syscall` `Exit` refusal when set and the enclosing function is the target `func main()`)
- Modify: `gophermind-lib/build/packer/prompts/implement.md:10` (the sentence becomes `{{if .AllowExit}}Call os.Exit only once, at the end of main, with the result of the entry call.{{else}}Never call os.Exit{{end}}, log.Fatal, ...` keeping the rest)
- Modify: `gophermind-lib/build/executor/leaf.go` (`newLeafRun` about line 267 sets `AllowExit` when `l.Package == "main"` and `l.Signature == "func main()"`; `noteForbiddenCall` becomes a function of `allowExit`; preflight after `newLeafRun` and before the ladder loop; `providerError` ends the revision when the parse error class is `ClassForbidden` and the reply SHA equals `lastSHA`)
- Modify: `gophermind-lib/build/packer/packer.go` (prompt data gains `AllowExit`)
- Test: `gophermind-lib/build/packer/exit_test.go`, `gophermind-lib/build/executor/preflight_test.go`

**Interfaces:**
- Produces: `packer.Expect.AllowExit bool`; `packer.Guidance`/prompt data field `AllowExit bool`; executor failure reason `execution_policy_conflict` (through `lr.fail`) whose cause names the node id, the required action and the rule, for example `fn-run requires os.Exit; model code may not end the process`; ladder outcome `revPolicy` (or the existing `revContractProblem`) on a repeated forbidden reply.
- Preflight rule: the node's `contract.side_effects` or `description` mentions `os.Exit` (word match) and `AllowExit` is false.

- [ ] **Step 1: Write the failing tests**

```go
// packer/exit_test.go
func TestExitAcceptedInValidatedMain(t *testing.T)
// NewExpect("main", "func main()"), AllowExit true, source calling os.Exit(cli.Run(...)): ParseReply ok
func TestExitRefusedInRun(t *testing.T)
// NewExpect("cli", "func Run(args []string, stdout, stderr io.Writer) int"): refused, kind forbidden_call
func TestExitRefusedOutsideMain(t *testing.T)
// package clause main but AllowExit false (not the validated entry): refused
func TestImplementPromptNamesExitOnlyForMain(t *testing.T)
// rendered prompt with AllowExit true contains "Call os.Exit only once"; false contains "Never call os.Exit"

// executor/preflight_test.go
func TestPreflightFailsAPolicyConflictBeforeTheLadder(t *testing.T)
// a leaf in package cli whose side_effects say "calls os.Exit": runLeaf fails with reason
// execution_policy_conflict and the scripted provider received zero implement calls
func TestRepeatedForbiddenReplyEndsTheRevision(t *testing.T)
// provider returns the same os.Exit source twice for a cli leaf: the entry ends after the second,
// no further ladder entries are tried
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/packer/ ./build/executor/ -run 'TestExit|TestImplementPromptNames|TestPreflight|TestRepeatedForbidden' -count=1`
Expected: FAIL (`AllowExit` undefined; preflight absent).

- [ ] **Step 3: Implement** as the Interfaces block says. `ScanGoSource` (`executor/scan.go:210`) already has the same exemption; do not change it.

- [ ] **Step 4: Run the suites**

Run: `cd gophermind-lib && go test ./build/packer/ ./build/executor/ -count=1` (add `-race` for the executor; it is slow, run once)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/packer/reply.go gophermind-lib/build/packer/packer.go gophermind-lib/build/packer/prompts/implement.md gophermind-lib/build/packer/exit_test.go gophermind-lib/build/executor/leaf.go gophermind-lib/build/executor/preflight_test.go
git commit -m "fix(executor): os.Exit allowed only in the validated main; policy preflight; repeated refusal ends the ladder"
```

### Task 3a: a given main.go satisfies the fixed func main() (prerequisite, found 2026-11-05)

Milestone 1 (975a54b5) fixes `func main()` for every `cmd/` directory and stops the run when the owning component does not declare it. A repository that already holds the entry point (the executor's greeter fixture: "`main.go` is given") is refused, and the executor suite fails 379 tests. The contract stage now drops the fixed main of a directory whose repository already has `func main()` in a non-test file of package main, decided from the repository, not the prose. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3a-brief.md`.

**Files:** Modify `gophermind-lib/build/planner/fixediface.go` (`givenMain`, `dropGivenMains`), `contract_stage.go:397`; tests in the planner package. Proof: `go test ./build/executor/` green.

### Task 3b: a dependency from another package is shown qualified; undefined dependency hint (found in r003, 2026-11-05)

r003 implemented `Run` with a bare `Greeting` call: the packet showed `func Greeting(name string) string` twice as a plain declaration under `package: cli` with a trailing note, the doc comment was doubled (`// //`), and the build failure gave the model only the compiler line. The packet marks each declaration from another package in place (`// From package greet, import "hello/internal/greet". Call it as greet.Greeting, never as Greeting.`), `docComment` strips a slash prefix the model wrote, and `BuildHints` names the import and qualifier for `undefined: <name>` when the name is a dependency from another package. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3b-brief.md`.

**Files:** Modify `gophermind-lib/build/contract/contract.go` (`docComment`), `build/packer/packer.go` (`state.render`), `build/packer/prompts/implement.md:8`, `build/planner/compilediag.go` (`BuildHints`); tests in each package.

### Task 3c: test functions never enter the contract; test-practice constraints are the test writer's (found in run hello-v1.1-20261106-r001, 2026-11-05)

After Tasks 3, 3a, 3b and 4 were installed, the run got past planning's contract stage correctly, then coverage opened gaps for C6, C7 and C8 (the brief's constraints about how tests are written), the fill round added `TestRunCLI` and `TestRunCLISeparateStreams` to the contract as functions, decompose made nodes of them, and the test writer looped eighteen rounds on a node whose function under test was a test. A function with a `*testing.T/B/M/F` parameter or a `_test.go` file is dropped with `test_function_dropped` wherever functions enter the contract (contract pass merge, coverage fill), and a Constraint that begins with "Tests" is recorded as covered by the test writer instead of a gap. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3c-brief.md`.

**Files:** Modify `gophermind-lib/build/planner/fixediface.go` (`isTestFunction`, `dropTestFunctions`), `contract_stage.go` (beside `dropForeignFunctions`), `coverage.go` (fill acceptance), `coverage_check.go` (`CheckCoverage`), `requirements.go` (`Requirement.IsTestRule`), `prompts/coverage.md`, `prompts/testwriter.md`; tests in the planner package.

### Task 3d: test obligations attach to the production function and are checked (John's design note, 2026-11-05 18:02 MST)

A `## Tests` bullet becomes a requirement of kind test (`T` ids), attached to the contract function its first backticked call names (`Run(nil, stdout, stderr)` names `Run`), carried into the test writer packet as an `<obligations>` block, and cited by the reply's cases (`"obligations": ["T3"]`). The validator refuses a test file that leaves an obligation uncited or omits its literal arguments and expected text, so `nil` and `[]string{}` are two cases when the brief lists both. Coverage ignores kind test. `checkTestExercises` says "the production function under test" and asks for a return comparison only when the signature has results. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3d-brief.md`.

**Files:** Modify `requirements.go`, `testwriter.go`, `finish.go`, `exercise.go`, `exported.go`, the coverage requirement filter, `prompts/testwriter.md`; create `obligations.go`; tests.

### Task 3e: test writer preflight, bounded no-progress, bench only with a reason (same note)

Before any model call, `checkTestTarget` fails the stage with `invalid_test_target` when the target is a Test/Benchmark/Fuzz/Example function, the contract file is a `_test.go`, or the artifact equals the target. Two consecutive identical refusals of the same Go block end the node with `testwriter_no_progress` instead of eighteen rounds. `bench` is dropped from `profile_hooks` (warning `profile_hook_dropped`) unless a requirement mentions performance or a benchmark. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3e-brief.md`.

**Files:** Create `testtarget.go`; modify `testwriter.go`, `profilehooks.go`, `enrich.go`, `prompts/enrich.md`, `router/router.go` only if a no-retry sentinel is needed; tests.

### Task 3f: the executor's plan-coverage tripwire ignores test obligations (run hello-v1.1-20261106-r003, 2026-11-05 19:39 MST)

The sixth run planned cleanly and stopped after 86 ms: `checkCoverageTotal` in the executor still demands a `Covered` entry for every requirement, and Task 3d's kind-test requirements (T1 to T9) never get one. The tripwire skips kind test and reports a `Covered` entry naming a T id as a defect. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3f-brief.md`.

**Files:** Modify `build/executor/acceptance.go`; test.

### Task 3g: an acceptance failure packet names the bullet, command and output; two identical replies end a ladder (run hello-v1.1-20261106-r004, 2026-11-05 20:07 MST)

The seventh run built the right program and failed three root tests that exited silently; the revise packet carried no failure at all and the model returned the same correct file five times before the node escalated. The failure text now always names the requirement, the command, the exit code and the output or its absence; a second identical reply in a row escalates the node. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-3g-brief.md`.

**Files:** Modify `build/executor/acceptance.go`, `build/executor/leaf.go`; tests.

### Task 4: Root acceptance test validator catches masked status and inverted checks (brief 17)

**Files:**
- Modify: `gophermind-lib/build/acceptcheck/quality.go` (`qual.script`, top level)
- Test: `gophermind-lib/build/acceptcheck/quality_test.go` (append)

**Interfaces:**
- Produces: findings `inverted_check` (a pipeline ending in `grep -q` whose first command is `gofmt -l`, `go vet`, `golint` or `staticcheck`, followed by `||`), `status_masked` (a statement running a built binary from `o.Bins`, joined by `&&` to a later statement, where any later statement in the script reads `$?`), `print_substitution_test` (a `test` or `[` whose argument is `$(echo ...)` or `$(printf ...)`).

- [ ] **Step 1: Write the failing tests**

```go
func TestInvertedGofmtCheckIsRefused(t *testing.T)
// `gofmt -l . | grep -q . || (go vet ./... && exit 0 || exit 1)` -> contains inverted_check
func TestGofmtTestZIsClean(t *testing.T)
// `test -z "$(gofmt -l .)"` -> no findings
func TestStatusMaskedByAndIsRefused(t *testing.T)
// `hello a b > /dev/null && test $(echo $? -eq 2)` with Bins hello -> status_masked and print_substitution_test
func TestStatusCapturedAfterSemicolonIsClean(t *testing.T)
// out=$(mktemp); err=$(mktemp); hello a b >"$out" 2>"$err"; status=$?; test "$status" -eq 2 && test ! -s "$out" -> none
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd gophermind-lib && go test ./build/acceptcheck/ -run 'TestInverted|TestGofmtTestZ|TestStatus' -count=1`
Expected: the two refusal tests FAIL.

- [ ] **Step 3: Implement** on the parsed statements already in `qual.script`; no new parser.

- [ ] **Step 4: Run the suite and the planner's coverage quality tests**

Run: `cd gophermind-lib && go test ./build/acceptcheck/ ./build/planner/ -run 'Quality|RootTest|Coverage' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/acceptcheck/quality.go gophermind-lib/build/acceptcheck/quality_test.go
git commit -m "fix(acceptcheck): refuse status masked by &&, inverted lister checks, tests of print substitutions"
```

### Task 4b: acceptance commands create temp files under $TMPDIR (run hello-v1.1-20261106-r002, 2026-11-05 18:21 MST)

The fifth run built and verified all three functions, then acceptance failed A4, A5 and A6 and the executor revised correct code: macOS `mktemp` ignores `TMPDIR` and writes to the per-user `/var/folders` temp dir, which the sandbox denies, and the coverage prompt's own example (Task 4) uses a bare `mktemp`. The prompt example becomes `mktemp "$TMPDIR/out.XXXXXX"`, `acceptcheck.Quality` gains `mktemp_outside_tmpdir` for a `mktemp` without a `$TMPDIR` template, and the executor stops `acceptance_environment`, naming the root test, when a command's output shows `mktemp` could not create its file. The sandbox is not widened. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-4b-brief.md`.

**Files:** Modify `build/acceptcheck/quality.go`, `build/planner/prompts/coverage.md`, `build/executor/acceptance.go`; tests.

### Task 4c: root tests compare exact text literally, count across files correctly, carry no placeholder words, and invoke the tools a constraint names (run hello-v1.1-20261106-r004)

The seventh run's three failing root tests: `grep -qx 'usage: hello [NAME]'` (a bracket expression; exact text needs `-F`), `grep -cE ... f1 f2 f3 | grep -q '3'` (per-file counts), and `hello | grep -q 'expected_output_here'` for the gofmt and vet constraint. acceptcheck gains `grep_regex_exact`, `grep_count_per_file` and identifier placeholders; the planner's quality round gains `tool_not_invoked`; the coverage prompt shows `grep -qxF`. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-4c-brief.md`.

**Files:** Modify `build/acceptcheck/quality.go`, `build/planner/coverage_quality.go`, `build/planner/prompts/coverage.md`; tests.

### Task 4d: a root test that does not parse is refused at planning time (run hello-v1.1-20261106-r005, 2026-11-05 20:49 MST)

The eighth run verified all three functions first try with acceptance 6 of 6; the one failure was the standard-library constraint's root test, which had an unterminated quote. `shellParse` already reports the condition and `Quality` discarded it; it becomes the finding `syntax_error`, and the coverage prompt shows `test "$(go list -m all | wc -l)" -eq 1` for a dependency constraint. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-4d-brief.md`.

**Files:** Modify `build/acceptcheck/quality.go`, `build/planner/prompts/coverage.md`; tests.

### Task 7b: the literal check reads examples, escapes, inputs and used nodes as the brief means them; a fill reply may not duplicate a function (run hello-v1.1-20261106-r007, 2026-11-05 21:43 MST)

The ninth run, the first with Task 7's literal check, planned the right contract and stopped at coverage: the check wanted every example of `Hello, <name>!` spelled out in node text, read an escaped `\n` the doc wrote as a real newline, took the input `Ada` of a sentence for a result, and did not see `Hello, World!` in the function fn-run uses. The fill round then declared `Greeting` and `Run` a second time. Examples of a carried pattern, newline escapes, sentence inputs and used nodes are read the way the brief means them; a fill reply that duplicates a function's name and package is refused. Brief: `.superpowers/sdd/2026-11-05-hello-pipeline-fixes/task-7b-brief.md`.

**Files:** Modify `build/planner/coverage_literals.go`, `coverage_check.go` (`PlanNode.Uses`), `decompose.go`, `coverage.go`; tests.

### Task 5: Node inputs and tier come from the signature and class (brief 11, 12, 14)

**Files:**
- Modify: `gophermind-lib/build/planner/decompose.go` (`normalizeNode` about line 374-440)
- Modify: `gophermind-lib/build/planner/prompts/decompose.md` (the `side_effects` sentence: "every call to another contract function" becomes "externally observable effects only: writes to a writer or file, network, process exit, persistent state; calls to other contract functions belong in depends_on, not here")
- Test: `gophermind-lib/build/planner/decompose_inputs_test.go`

**Interfaces:**
- Produces: in `normalizeNode`, `contract.inputs` is rebuilt from `fd.Type.Params` in order: `name` and `type` from the parsed signature, `description` from the reply entry with the same name (empty when absent); reply inputs with no matching parameter are dropped with note `<id> input <name> is not a parameter`. `model_tier` is set from `node_class` after the class is settled: `wiring` and classes that touch I/O map to `strong`, pure classes to `any`; the reply's `model_tier` is ignored with a note when it differs. Name the class table `tierOfClass` in `decompose.go`.

- [ ] **Step 1: Write the failing tests**

```go
func TestNodeInputsFollowTheSignature(t *testing.T)
// Run(args []string, stdout, stderr io.Writer) int with a reply listing args, stdin, stdout, stderr:
// inputs are exactly args, stdout, stderr with the parsed types; note mentions "stdin is not a parameter"
func TestModelTierFollowsTheClass(t *testing.T)
// a wiring node whose reply says "any": model_tier strong; a pure node saying "strong": any
func TestDecomposePromptSeparatesSideEffectsFromDependencies(t *testing.T)
// render contains "belong in depends_on"
```

- [ ] **Step 2: Run to see them fail**, **Step 3: Implement**, **Step 4:** `go test ./build/planner/ -count=1` PASS (the enrich tests derive `go_type` from the signature already; confirm `deriveGoTypes` still agrees).

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/decompose.go gophermind-lib/build/planner/prompts/decompose.md gophermind-lib/build/planner/decompose_inputs_test.go
git commit -m "fix(planner): node inputs and tier are derived from the signature and class"
```

### Task 6: Enrichment explains, it does not add behaviour (brief 13)

**Files:**
- Modify: `gophermind-lib/build/planner/prompts/enrich.md:8` (portability example becomes `{"os": ["any"], "arch": ["any"], ...}` and a sentence: "Name an OS or architecture only when the brief restricts one.") and a rule line: "Enrichment explains the accepted contract. Add no behaviour the brief does not require: no log events, metrics, sanitization, flags, parameters or dependencies the brief and the contract do not already state."
- Modify: `gophermind-lib/build/planner/groups.go` (`groupDefects`: a `log_events` entry is a defect `observability_unrequested` when the outline `conventions` or the brief's Constraints contain "no logging", or when the node class is pure)
- Test: `gophermind-lib/build/planner/enrich_behaviour_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestEnrichPromptDoesNotSuggestLinuxAmd64(t *testing.T)
// render("enrich", data) contains `"os": ["any"]` and not "linux"
func TestUnrequestedLogEventIsADefect(t *testing.T)
// a pure node reply with observability.log_events: defect kind observability_unrequested
```

- [ ] **Step 2 to 4:** red, implement, `go test ./build/planner/ -run 'Enrich|Group' -count=1` PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/prompts/enrich.md gophermind-lib/build/planner/groups.go gophermind-lib/build/planner/enrich_behaviour_test.go
git commit -m "fix(planner): enrichment adds no platform limits or log events the brief does not ask for"
```

### Task 7: Coverage checks the behaviour, not only the mapping (brief 16)

**Files:**
- Create: `gophermind-lib/build/planner/coverage_literals.go`
- Modify: `gophermind-lib/build/planner/coverage.go` (after `CheckCoverage`, before the fill loop)
- Test: `gophermind-lib/build/planner/coverage_literals_test.go`

**Interfaces:**
- Produces: `func requirementLiterals(req Requirement) []string`: backticked spans in sentences of a feature (F) requirement that contain "prints", "writes", "exits with code", "returns" or "line is", plus the integer after "code" (`2`). Invocations (a span starting with the binary name followed by a space or the end) are skipped. `func literalGaps(reqs []Requirement, covered []Covered, nodes []PlanNode) []Gap`: for each F requirement, every literal must appear in the title, description, doc, outputs or errors text of at least one mapped node; a miss is a `Gap` whose reason is `behaviour missing: <literal>` and feeds the existing fill loop.

- [ ] **Step 1: Write the failing tests**

```go
func TestCoverageLiteralsOfSayHello(t *testing.T)
// literals: "Hello, World!", "Hello, <NAME>!", "Hello, Ada!"; and for Report errors: "usage: hello [NAME]", "2"
func TestCoverageLiteralsSkipInvocations(t *testing.T)
// "hello Ada" and "hello [NAME]" are not literals
func TestRunWithoutGreetingFailsF1(t *testing.T)
// F1 mapped to a Run node whose text never mentions Hello: one gap "behaviour missing: Hello, World!"
```

- [ ] **Step 2 to 4:** red, implement, `go test ./build/planner/ -run 'Coverage' -count=1` and the full planner suite PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/coverage_literals.go gophermind-lib/build/planner/coverage.go gophermind-lib/build/planner/coverage_literals_test.go
git commit -m "fix(planner): coverage requires a mapped node to carry each feature's output literal"
```

### Task 8: Tests are written from the requirements, and checked against them (brief 15)

**Files:**
- Modify: `gophermind-lib/build/planner/testwriter.go` (`renderTestwriterBrief` gains `requirements string`; `writeTests` builds it from `fileCoverage` for the node's mapped requirement ids: `<id>: <full text>` one per paragraph; after a reply parses, every literal of a mapped F requirement (Task 7's `requirementLiterals`) that the node's outputs mention must appear in the test source, else refusal kind `expected_value_missing: <literal>` and the ordinary refusal retry)
- Modify: `gophermind-lib/build/planner/prompts/testwriter.md` (a `<requirements>` data block before the brief block, with the sentence "These are the brief's requirements this function satisfies. Expected values come from them, not from the contract.")
- Test: `gophermind-lib/build/planner/testwriter_requirements_internal_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestTestwriterPromptCarriesBothFeaturesForRun(t *testing.T)
// node Run mapped to F1 and F2: prompt contains "Hello, World!" and "usage: hello [NAME]" inside <requirements>
func TestTestFileWithoutTheGreetingIsRefused(t *testing.T)
// a reply for Run asserting only the usage case: refusal kind expected_value_missing: Hello, World!
```

- [ ] **Step 2 to 4:** red, implement, `go test ./build/planner/ -run 'Testwriter|TestWrite' -count=1` and the full suite PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/planner/testwriter.go gophermind-lib/build/planner/prompts/testwriter.md gophermind-lib/build/planner/testwriter_requirements_internal_test.go
git commit -m "fix(planner): the test writer gets the mapped requirements and must assert their literals"
```

### Task 9: Statuses that say why (brief 25)

**Files:**
- Modify: `gophermind-lib/build/router/parsed.go` (`markMalformed` writes `ErrorKind` from the parse error: `schema`, `contract`, `forbidden`, `conflict`, or the error's class when it has one)
- Modify: `gophermind-lib/build/planner/contract_stage.go` (`contractState` gains `CandidateStatus map[string]string` keyed by pass name: `accepted`, `accepted_with_repairs:<warning kinds>`, `rejected:<kind>`)
- Test: `gophermind-lib/build/planner/candidate_status_test.go`, `gophermind-lib/build/router/parsed_test.go` (append)

- [ ] **Step 1: Write the failing tests**

```go
func TestMalformedRowNamesItsKind(t *testing.T)       // router: ErrorKind "contract" for a contract error
func TestCandidateStatusRecordsRepairsAndRejections(t *testing.T)
// after the hello rig stops at contract: _state/contract.json candidate_status has an entry for
// every pass; the say-hello entry is not plain "accepted" (it names fixed_interface_missing, as a
// rejection or a repair depending on how Milestone 1 Task 4 re-asks); entry-point names entry_extra_function_dropped
```

- [ ] **Step 2 to 4:** red, implement, `go test ./build/router/ ./build/planner/ -count=1` PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/build/router/parsed.go gophermind-lib/build/router/parsed_test.go gophermind-lib/build/planner/contract_stage.go gophermind-lib/build/planner/candidate_status_test.go
git commit -m "feat(planner): candidate status per contract pass; malformed ledger rows name their kind"
```

### Task 10: Fresh hello run (brief 29, 30)

Needs John's word to install (`bash scripts/local-service.sh install`) and to submit the run. Not a subagent task.

- [ ] **Step 1:** install the build; confirm `INSTALLED_COMMIT` is the Milestone A head.
- [ ] **Step 2:** submit `examples/briefs/14-hello.md` with `ask=1&verbose=1`; watch planning: expect `fixed_interface_applied` or none, no `outline_duplicate_ignored`, no `entry_extra_function_dropped` beyond the entry pass, coverage 6/6, no `contract_flagged`.
- [ ] **Step 3:** executor: fn-main implemented on the first attempt; acceptance 6/6.
- [ ] **Step 4:** verify by hand on the run's repo: `go build ./... && go vet ./... && go test ./...`, then the three CLI cases with captured stdout, stderr and status.
- [ ] **Step 5:** report per brief section 30 (files changed, tests added, stage summary, contract, source tree, build and acceptance results, limitations). Capture the run's replies into `testdata/hello/` as a second fixture set only if John wants it.

---

## Milestone B: hardening (after the run is green; John's call)

Each becomes a task in a follow-up plan when scheduled.

- Brief 10: stamp `contract_revision` on drafts (`normalizeNode`), enrichment entries, leaf-test records and skeleton nodes; the executor refuses a node whose revision differs from `contracts.json`; `refreshPlan` invalidates drafts of changed functions. Tests: stale work rejected.
- Brief 9: a `replace` list in `contract_repair.md` and `mergeRepair` applied to a validated candidate copy; revise may request it through `CONTRACT_CHANGE_REQUIRED` instead of `human_stop`. Tests: repair effectiveness, no_progress counted.
- Brief 20: `no_progress` budget on contract repairs (`maxOutlineRepairs` already bounds re-asks; add the canonical-content comparison).
- Brief 3: a `Requirement` record with stable ids written to `_state/requirements.json` and `requirement_ids` on nodes at decomposition rather than at `finishTree`.
- Brief 5: `localChecks` refuses a `package` with no `/` other than `.`; refuses a function whose `FileDir` is not its `package`.
- Brief 17 rest: structural check for "table driven" (a `[]struct{` literal in the test file) classified separately from behavioural proof.

## Parked from the Milestone 1 reviews (2026-11-05)

Findings the task and whole-branch reviews recorded but Milestone 1 did not take. Fold each into the task that touches its file, or into Milestone B.

- `planner.FixtureProvider`: a stage with no fixture file is retried by the router until the test's 10 minute timeout; it should return an error the router does not retry. Take in Task 1 (the hello rig is the most exposed).
- `cli_surface_missing` reads the brief's example text `--Go & Rust` as a flag.
- `tooLongErr` (`promptbudget.go`) says "the brief sources were kept whole and never cut" for every sized stage, including the outline, repair and schema-fix excerpts that are cut; say it only for `componentBrief` stages.
- `sources.json` and `component_sources` list the level 1 section set even for level 0 packets, and the file is written only when the outline completes in the same run.
- The `component_sources` event message repeats its kind prefix.
- Stale labels after Task 2: `contract_component.md` "Brief section for this component", `decompose.md`, `enrich.md`, `enrich_structure.md`, `testwriter.md` "the part of the program this function belongs to"; `contract_component.md` "report the conflict" has no reply field to report into.
- `componentBrief` level 1 emits `### <feature>` headings without the `## Features` parent.
- `featuresOfComponents` ignores its `comps` argument.
- A missing `main` is now a hard error after two re-asks (`MainRepairs`, `FixedRepairs`) instead of the `main_function_missing` warning: CHANGELOG line.
- Fixed interfaces with a receiver or a type parameter are ignored silently; two `cmd/` directories both get the first bullet's `EntryCall`.
- `packetParts` scans `##` lines inside code fences; `scripts/extract-llm-fixtures.py` raises `KeyError` on a call row without a stage; a case-only package mismatch passes `localChecks`; `Entry` and `EntryName` keys are now equal; the slugify `fn-run` assertion depends on component order (README documents it).
- Decompose and enrich send the whole brief through `callAsking` with no sizing, so an over-window brief fails there with the router's too-long error, not `tooLongErr`.
- After the final fix (0f8c8e0c): two sibling components in one directory may each write the fixed function and the first exact match wins silently; `ownPackage` with a bare component name accepts any function path ending in `/name`; a module-prefixed `cmd/` bullet gets no fixed `main` and no entry call.
- Architecture parsing is brittle against a natural bullet style (John's rewritten hello brief, 2026-11-05 12:34): a bullet "`internal/greet` contains exactly `func Greeting(name string) string`" registers no fixed interface because `fixedSpanRE` rejects the `func ` prefix; a `cmd/` bullet with the call on a different bullet gives no entry call; a bullet starting with any backtick span (`` `Greeting` owns ... ``, `` `os.Exit` must not ... ``) is read as a directory by `archBulletDir`. Fix in Task 2 or Milestone B: accept an optional `func ` prefix in `fixedSpanRE`; treat a span as a directory only when it contains `/` or starts with `cmd/`; look for the entry call on every bullet that names the `cmd/` package's function. Test with the v1.1 brief's predecessor text.
- Policy change to put to John: public providers at node scope now see the brief's feature text and every section but Overview (Task 2 ruling; docs updated in the final fix).

## Out of this plan

- Editable prompts (`docs/superpowers/plans/2026-11-05-editable-prompts.md`), queued separately.
- Executor truncation classified as `provider_unavailable` (CONTEXT.md 2026-11-04 22:45): its own fix; it will bite a hello run only if the model runs away.
- A planner rig that builds and runs the generated code: the executor e2e stays greeter-wired; Task 10 is the live proof.
