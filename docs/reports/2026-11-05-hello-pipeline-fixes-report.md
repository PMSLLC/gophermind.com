# Hello pipeline fixes: final report

Plan: `docs/superpowers/plans/2026-11-05-hello-pipeline-fixes.md`. Brief: `docs/superpowers/specs/2026-11-05-hello-pipeline-fixes-brief.md`. Range reported: `dee6a15e..695cef47` (46 commits). Proof run: `hello-v1.1-20261106-r011` on installed commit `695cef47`, local model gemma-4-26b-a4b at 127.0.0.1:8081 only.

## Files changed

106 files, 7237 insertions, 294 deletions (`git diff --stat dee6a15e..695cef47`).

| Package | Files | Insertions | Deletions |
|---|---|---|---|
| gophermind-lib/build/planner | 70 | 5133 | 229 |
| gophermind-lib/build/executor | 12 | 638 | 33 |
| gophermind-lib/build/acceptcheck | 3 | 629 | 5 |
| gophermind-lib/build/packer | 6 | 322 | 16 |
| gophermind-lib/build/router | 4 | 127 | 4 |
| gophermind-lib/build/contract | 2 | 36 | 0 |
| gophermind-lib/build/projectrun | 3 | 18 | 3 |
| gophermind-lib/build/brief | 1 | 2 | 2 |
| gophermind-lib/build/report | 1 | 1 | 1 |
| gophermind-lib/runview | 1 | 1 | 1 |
| docs/superpowers (plan, resume plan, resume design) | 3 | 330 | 0 |

No file under `serve` changed.

## Architectural changes made

### build/planner

- Contract merge: a second, different declaration of an id is a conflict and refuses the reply. A conflict is measured against the model's own declaration (kept as sent, stored in the stage state as `declared`), not the harness-edited record. An identical re-emission is silent, also after a fixed signature or fixed use was applied, and also from a sibling component.
- A re-asked pass supersedes the functions its component declared in the refused pass (warning `superseded_function_dropped`).
- Fixed interface: a fixed call becomes a use; entry extras are dropped when the entry call is fixed; the entry id is never suffixed; a fixed entry keeps only `main`; fewer helpers per component. A given `main.go` in the repo satisfies the fixed `func main()`.
- Test functions never enter the contract (warning `test_function_dropped`). Constraints that begin "Tests" are the test writer's rules, never coverage gaps.
- Test obligations: each Tests-section bullet gets a T id and attaches to its production function by callee. Every call span of the callee contributes its literal arguments, and expected text matches as written or in Go-quoted form. The test writer gets the mapped requirements and must assert their literals. Obligations bind revisions too.
- Test writer: preflight refuses an invalid test target before any model call, a bounded no-progress rule stops repeated replies, and benchmarks are kept only with a performance requirement.
- Coverage: a mapped node must carry each feature's output literal. The literal check reads examples, newline escapes, inputs, used nodes, and destinations after "to" or "on" as the brief means them. A fill reply may restate a mapped function's doc and may not duplicate a function. Gaps are listed by id.
- Root-test quality gate (shared with acceptcheck): a root test that `/bin/sh` cannot parse is refused, a tool a constraint names must be invoked, and a forbidden token is refused only when the brief requires it where the grep searches (negation and path aware).
- Node inputs and tier are derived from the signature and class. Enrichment adds no platform limits or log events the brief does not ask for.
- Statuses: candidate status per contract pass (`rejected:*`, repairs); a fixed-interface stop stores its status on the last saved state; the plan total counts only coverable requirements.
- Prompts changed: contract_component, coverage, coverage_fill, decompose, enrich, testwriter.

### build/acceptcheck

New findings: `status_masked` (also inside an assignment's substitution), inverted lister checks, tests of print substitutions, `mktemp_outside_tmpdir`, `grep_regex_exact`, `grep_count_per_file`, placeholder identifiers, `syntax_error` (unclosed quote or heredoc), `bash_only` (process substitution), `empty_line_grep`, `version_enumerated`. `shellParseFull` reports an unclosed flag; `shellParse` stays as a wrapper.

### build/executor

- `os.Exit` is allowed only in the validated entry `main`; a policy preflight reads the node's side effects only (not prose) and stops the run on a conflict; a repeated forbidden reply ends the revision or the entry.
- The plan-coverage tripwire ignores test obligations; the acceptance total counts only coverable requirements.
- Acceptance failure packets name the requirement, command, exit code and output (or its absence); two identical replies in a row end the ladder.
- Stops that name the fault: `acceptance_environment` for a temp-dir fault, `acceptance_test_invalid` for a root test with a shell syntax error.

### build/packer

- `os.Exit` accepted only in the validated `main` (implement prompt says so).
- A foreign dependency is shown qualified where it stands; an undefined-dependency build hint names the import.
- Reply handling updated for the exit rule.

### build/router

- A parse error flagged no-retry stops the call (`ErrNoRetry`, checked in `parsed.go`).
- A malformed ledger row names its kind (schema, contract, forbidden, conflict, else malformed).

### Other

- build/contract: doc comments no longer double `//`.
- build/projectrun, build/report, runview: unattended gate and counts ignore test requirements.
- build/brief: examples count test updated (14 example briefs).

## Regression tests added

Test functions added in `_test.go` files over the range, by file (`git diff -U0`, lines `+func Test`).

- acceptcheck/quality_test.go: TestInvertedGofmtCheckIsRefused, TestGofmtTestZIsClean, TestStatusMaskedByAndIsRefused, TestStatusCapturedAfterSemicolonIsClean, TestQualityMktemp, TestStatusMaskedInsideAssignmentSubstitution, TestQualityGrepFormsAndPlaceholderWords, TestQualitySyntaxError, TestQualityShellFormsVersionsAndEmptyOutput, TestNegatedGreps
- contract/contract_test.go: TestDocCommentDoesNotDoubleSlashes
- executor/acceptance_packet_test.go: TestAcceptanceBulletFailureText, TestBulletWithoutCommandFailureText
- executor/acceptance_shellsyntax_test.go: TestShellSyntaxFaultStopsTheRun, TestShellSyntaxFaultIgnoresOtherExit2, TestAcceptanceTotalExcludesTestObligations
- executor/acceptance_tempdir_test.go: TestTempDirFault, TestAcceptanceTempDirFaultStops
- executor/acceptance_test.go: TestCheckCoverageTotalIgnoresTestObligations
- executor/ladder_test.go: TestIdenticalReplyTwiceEndsLadder
- executor/preflight_test.go: TestPreflightFailsAPolicyConflictBeforeTheLadder, TestPreflightMatchesTheWordOnly, TestRepeatedForbiddenReplyEndsTheRevision, TestPreflightRule, TestRepeatedForbiddenReplyEndsTheEntryUnit
- packer/exit_test.go: TestExitAcceptedInValidatedMain, TestExitRefusedInRun, TestExitRefusedOutsideMain, TestImplementPromptNamesExitOnlyForMain
- packer/packer_test.go: TestForeignDeclarationIsMarkedWhereItStands, TestForeignNameWithoutADeclarationKeepsTheNote
- planner/builddiag_test.go: TestBuildHintsNameTheImportForAnUndefinedDependency
- planner/candidate_status_test.go: TestCandidateStatusRecordsRepairsAndRejections, TestPlanStatusTotalExcludesTestObligations, TestFixedInterfaceStopStoresTheStateBeforeTheRefusedPass
- planner/coverage_check_test.go: TestTestRuleConstraintsAreNotGaps
- planner/coverage_fill_test.go: TestCoverageFillRefusesADuplicateFunction, TestCoverageFillRestatesADoc, TestGapsAreListedById
- planner/coverage_literals_test.go: TestCoverageLiteralsOfSayHello, TestCoverageLiteralsSkipInvocations, TestRunWithoutGreetingFailsF1, TestExitCodeLiteralIsAWord, TestCoverageLiteralsComeAfterTheVerb, TestCoverageLiteralsSkipGoTypes, TestLiteralExamplesOfACarriedPatternAreCarried, TestLiteralNewlineEscapesMatchDocNewlines, TestLiteralInputsAreNotResults, TestLiteralsOfUsedNodesCount, TestLiteralDestinationsAreNotResults
- planner/coverage_quality_internal_test.go: TestToolsNotInvoked, TestShellSyntaxRejectsProcessSubstitution, TestForbidsBriefTokens, TestForbidsBriefTokensReadsNegationAndPaths, TestCoveragePromptNegatedGrepsPassTheBriefTokenCheck
- planner/coverage_quality_test.go: TestCoverageQualityGateRefusesABareMktemp, TestCoveragePromptTeachesTheCapturePattern, TestCoverageQualityGateRefusesAConstraintThatSkipsItsTools, TestCoveragePromptExamplesPassTheQualityGate, TestCoverageQualityGateRefusesAForbiddenBriefToken, TestCoverageQualityGateRefusesWhatShCannotParse
- planner/coverage_test.go: TestCoverageFillRestatesADocToCloseABehaviourGap
- planner/decompose_inputs_test.go: TestNodeInputsFollowTheSignature, TestModelTierFollowsTheClass, TestDecomposePromptSeparatesSideEffectsFromDependencies
- planner/enrich_behaviour_test.go: TestEnrichPromptDoesNotSuggestLinuxAmd64, TestUnrequestedLogEventIsADefect, TestNoLoggingAskedReadsConstraintsAndConventions
- planner/exercise_internal_test.go: TestCheckTestExercisesAsksForAComparisonOnlyWithResults
- planner/givenmain_test.go: TestGivenMain, TestDropGivenMainsKeepsEverythingElse
- planner/hello_fixture_test.go: TestHelloContractKeepsTheFixedArchitecture (the hello contract regression on the recorded replies)
- planner/merge_conflict_test.go: TestSecondDifferentDeclarationIsAConflict, TestIdenticalReplayIsSilent, TestEntryExtrasDroppedWhenTheCallIsFixed, TestEntryExtrasKeptWithoutAFixedCall, TestEntryPointIdIsNeverSuffixed, TestComponentPromptLimitsHelpers, TestFixedCallBecomesAUse, TestReaskedPassSupersedesTheRefusedOne, TestReEmissionAfterFixedUseIsSilent, TestReEmissionAfterFixedSignatureIsSilent, TestSiblingComponentIdenticalReEmissionIsSilent, TestSiblingComponentDifferentDeclarationConflicts
- planner/obligation_check_test.go: TestObligationCheckRefusesAnUncitedObligation, TestObligationCheckRefusesAMissingArgument, TestObligationCheckPassesJohnsTable, TestObligationCheckRefusesMissingExpectedText, TestObligationLiteralArguments, TestObligationCheckRealTwoCallBullet, TestObligationCallOfAnotherFunctionAddsNothing, TestObligationExpectedTextRows (plus fixture helper functions named TestRun and TestGreeting inside fixture text)
- planner/obligations_stage_test.go: TestTestwriterCarriesAndChecksObligations, TestTestwriterRefusesAnUncitedObligation, TestReviseChecksTheObligations, TestReviseAcceptsARewriteCitingTheObligations, TestSetLeafTestSHARefreshesObligations
- planner/obligations_test.go: TestObligationsAttachByCallee, TestObligationsAmbiguousCalleeIsUnattached, TestObligationCalleeOfAMethodCall, TestRequirementsTextLeavesOutTestRequirements
- planner/profilehooks_test.go: TestBenchIsDroppedWithoutAPerformanceRequirement, TestBenchIsKeptWithAPerformanceRequirement, TestHelloProfileHooksHaveNoBench
- planner/requirements_test.go: TestIsTestRule, TestTestsSectionBulletsGetTIds, TestCoverageIgnoresTestRequirements
- planner/testfunc_test.go: TestIsTestFunction, TestDropTestFunctionsKeepsOrder, TestDropTestFunctionsText
- planner/testtarget_test.go: TestCheckTestTarget, TestWriteTestsRefusesAnInvalidTargetBeforeAnyCall
- planner/testwriter_brief_internal_test.go: TestTestwriterPromptCarriesObligations
- planner/testwriter_noprogress_test.go: TestTestwriterStopsOnNoProgress
- planner/testwriter_requirements_internal_test.go: TestTestwriterPromptCarriesBothFeaturesForRun, TestTestFileWithoutTheGreetingIsRefused, TestTestFileMustAssertTheEscapedNewline
- projectrun/gate_test.go: TestUnattendedGateIgnoresTestRequirements
- router/parsed_test.go: TestMalformedRowNamesItsKind
- router/router_test.go: TestCallParsedStopsOnANoRetryParseError

Fixtures: `gophermind-lib/build/planner/testdata/hello/` gained `contract.say-hello.2.txt` and `tests-section.md` (the real hello v1.1 Tests section, nine bullets) and a README update; `testdata/slugify/` gained `coverage_fill.txt` and updated `coverage.txt`, `contract.cli-logic.2.txt`, `enrich._fix.3.txt`.

Test functions named in a few files such as TestRun, TestGreeting, TestGreet and TestRun in `testwriter_*` are sample test bodies embedded in test fixtures, listed here only because they matched the grep.

## Tests run and actual results

Run history (from the ledger). The plan found each failure by a live run on the local model and turned it into a task.

| Run | Installed | Result and what it became |
|---|---|---|
| hello-v1.1-20261105-r002 | f5406f49 | Contract right; failed escalated at fn-main on the os.Exit gate. Task 3. |
| hello-v1.1-20261105-r003 | f5406f49 | fn-run escalated `contract_flagged` (bare `Greeting("")`, undefined); never reached fn-main. Task 3b. |
| hello-v1.1-20261106-r001 | 2a169056 | Failed in planning: fill added test functions as contract functions, test writer refused 18 rounds. Task 3c. |
| hello-v1.1-20261106-r002 | bd1d624b | Planning passed; acceptance 3 of 6, root tests used a bare `mktemp` the sandbox denied. Task 4b. |
| hello-v1.1-20261106-r003 | 63cefbd9 | Stopped after 86 ms: executor tripwire demanded coverage for test obligations. Task 3f. |
| hello-v1.1-20261106-r004 | cb3271b3 | Acceptance 5 of 6, constraints 3 of 5: bracket grep without -F, per-file counts, placeholder test; five identical replies. Tasks 4c, 3g. |
| hello-v1.1-20261106-r005 | 45a1aadd | Acceptance 6 of 6, constraints 3 of 4; C2 root test had an unterminated quote. Task 4d. |
| hello-v1.1-20261106-r006 | 45a1aadd | Acceptance 6 of 6, constraints 3 of 4; C2 root test unparseable again. |
| hello-v1.1-20261106-r007 | cedeac05 | Stopped at coverage after two rounds on the literal check reading the brief wrongly. Task 7b. |
| hello-v1.1-20261106-r008 | 947ed674 | One real literal gap left (usage line missing from fn-run doc); fill had no action to change a doc. Task 7c. |
| hello-v1.1-20261106-r009 | 45a8b0ed | Acceptance 4 of 6, constraints 2 of 5: bash process substitution under /bin/sh, enumerated Go versions, forbidden `os.Args`, empty-output grep. Tasks 4e, 3h. |
| hello-v1.1-20261106-r010 | a5a38158 | Green: 3 of 3 nodes verified (fn-greeting one revision, fn-run tests rewritten once), acceptance 6 of 6, constraints 5 of 5, requirements 16 of 16. |
| hello-v1.1-20261106-r011 | 695cef47 | Green and clean, the proof run (below). |

Full `gophermind-lib` check at 695cef47 (`go build ./... && go vet ./... && gofmt -l . && go test ./...`), log `/tmp/gm-fulltest-695cef47.log`. The log had no `exit N` line when this report was written: the run was still in progress, and the controller appends the result. Lines so far, verbatim:

```
gofmt: 0 files
gofmt: 0 files
ok  	gophermind/gophermind-lib/abtest	0.578s
ok  	gophermind/gophermind-lib/agent	3.522s
ok  	gophermind/gophermind-lib/banner	0.384s
ok  	gophermind/gophermind-lib/build	0.421s
ok  	gophermind/gophermind-lib/build/acceptcheck	0.445s
ok  	gophermind/gophermind-lib/build/artifacts	0.678s
ok  	gophermind/gophermind-lib/build/blackboard	8.917s
ok  	gophermind/gophermind-lib/build/brief	0.449s
ok  	gophermind/gophermind-lib/build/contract	0.426s
ok  	gophermind/gophermind-lib/build/diagnose	0.553s
ok  	gophermind/gophermind-lib/build/envcheck	0.600s
ok  	gophermind/gophermind-lib/build/events	0.330s
ok  	gophermind/gophermind-lib/build/execenv	0.368s
```

Earlier, the full-tree run at b5c7f76b was clean for build, vet, gofmt and the executor suite, with two failures: the examples count (fixed at 2a169056) and `serve` TestBriefEventsEndsWhenRunIsFinal (passes alone with -count=3; parked as a load-timing flake).

Result of the full run at 695cef47 (23:05 to 00:03 MST, while hello r011 and r012 ran on the same machine): build, vet and gofmt clean; 81 packages ok, two FAIL.

- `build/executor` (3497 s under load): `TestLoadPlanGreeter` (fn-serve Tier "strong", want "any") and `TestEveryStageEmitsEvents` (the planner's `leaf_normalized` warning event exceeds the test's 200-character cap). Both fail by name at HEAD in 22 s and pass at 0932dc1 (the commit before this plan), so they are regressions of 8a24ca35 (a node's tier follows its class; wiring is strong) that no task caught because implementers never run the executor suite. Both are stale test expectations against the ruled behaviour; fixed at 7ddb8773 (fn-serve expects strong; the events test skips the leaf_normalized warning like main_function_missing). Both pass by name after the fix (26 s).
- `serve`: `TestRunPreviewTokenMatchStartsRun` failed on TempDir cleanup ("directory not empty"); passes 3 of 3 alone. Load-timing flake, parked with the one above.

## Final Hello run stage summary

Run `hello-v1.1-20261106-r011`, 23:04 to 23:10 MST on 695cef47. Run log: `~/.gophermind/local/briefs/hello-v1.1-20261106-r011/run.log`.

| Stage | Provider/model | Calls | ok | Malformed | Retries | Escalations | Tokens in/out |
|---|---|---|---|---|---|---|---|
| clarify | gemma/gemma-4-26b-a4b | 1 | 1 | 0 | 0 | m0/r0/h0 | 2036/2 |
| contract | gemma/gemma-4-26b-a4b | 6 | 6 | 0 | 0 | m0/r0/h0 | 19710/1290 |
| decompose | gemma/gemma-4-26b-a4b | 3 | 3 | 0 | 0 | m0/r0/h0 | 7413/807 |
| coverage | gemma/gemma-4-26b-a4b | 2 | 2 | 0 | 0 | m0/r0/h0 | 5679/1323 |
| testwrite | gemma/gemma-4-26b-a4b | 2 | 2 | 0 | 0 | m0/r0/h0 | 7607/2204 |
| implement | gemma/gemma-4-26b-a4b | 3 | 3 | 0 | 0 | m0/r0/h0 | 4871/369 |
| enrich | gemma/gemma-4-26b-a4b | 7 | 7 | 0 | 0 | m0/r0/h0 | 15366/2104 |

Totals from the log: Nodes 3 total, 3 verified, 0 failed, 0 escalated, 0 blocked. Waves 3. Weak tests 1, Repairs 0. Constraints checked 4 of 4. Requirements covered 16 of 16. Acceptance passed 6 of 6. Run verdict: verified. Warning: `test_obligation_unattached: T5` (the bullet "No `Greeting` result ends with a newline." has no call, by design). Sandbox on, go1.27.1 darwin/arm64.

Acceptance verdicts (`acceptance.json`):

| Id | Requirement | Verdict |
|---|---|---|
| A1 | `go build ./...` succeeds | pass |
| A2 | `go vet ./...` reports nothing | pass |
| A3 | `go test ./...` passes | pass |
| A4 | `hello` prints `Hello, World!`, nothing on stderr, exit 0 | pass |
| A5 | `hello Ada` prints `Hello, Ada!`, nothing on stderr, exit 0 | pass |
| A6 | `hello a b` prints usage on stderr, nothing on stdout, exit 2 | pass |
| C1 | Go 1.22 or later | pass |
| C2 | Standard library only | pass |
| C4 | No logging, flags, configuration, file, network, environment, `init` | pass |
| C5 | `gofmt` clean and `go vet` clean | pass (two root tests) |

Coverage (`coverage.json`): 1 round, 16 requirements, root tests for A1 to A6, C1, C2, C4, C5 (C5 has two), test-rule constraints C6, C7, C8 handled by the test writer, F1 and F2 mapped to fn-greeting and fn-run, C3 mapped to all three nodes.

## Final generated contract

From `contracts.json` (module `hello`, spec_version 2.0, revision 0).

Functions:

| Id | Package | Signature | Uses |
|---|---|---|---|
| fn-greeting | internal/greet | `func Greeting(name string) string` | none |
| fn-run | internal/cli | `func Run(args []string, stdout, stderr io.Writer) int` | fn-greeting |
| fn-main | cmd/hello | `func main()` | fn-run |

Docs:

- fn-greeting: Returns a greeting string for the given name. If the name is empty, returns exactly 'Hello, World!'. Otherwise, returns 'Hello, <name>!' without a newline.
- fn-run: Handles command-line arguments and manages output to stdout and stderr. Returns 0 for successful execution or 2 if argument count is invalid. Writes exactly 'usage: hello [NAME]\n' to stderr and exits with 2 if more than one argument is provided.
- fn-main: The application entry point. It calls cli.Run with command line arguments and standard I/O streams, then exits with the returned code.

Components:

| Id | Package | Exports | Integration tests |
|---|---|---|---|
| greeting-generation | internal/greet | fn-greeting | 0 |
| command-execution | internal/cli | fn-run | 3 (no args, one arg, too many args) |
| entry-point | cmd/hello | fn-main | 0 |

Types: none. Conventions: errors as integer exit codes or stderr text; no logging permitted.

## Generated source tree

Run repository `~/.gophermind/local/repos/hello-v1.1-20261106-r011/`, `.go` files outside `.gophermind`:

```
cmd/hello/fn_main_test.go
cmd/hello/main.go
internal/cli/cli.go
internal/cli/fn_run_test.go
internal/greet/fn_greeting_test.go
internal/greet/greet.go
```

Plus `go.mod`. `cmd/hello/main.go`:

```go
package main

import (
	"hello/internal/cli"
	"os"
)

// main is the application entry point. It calls cli.Run with command line arguments and standard I/O streams, then exits with the returned code.
func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
```

`internal/cli/cli.go`:

```go
package cli

import (
	"fmt"
	"io"

	"hello/internal/greet"
)

// Run handles command-line arguments and manages output to stdout and stderr. Returns 0 for successful execution or 2 if argument count is invalid. Writes exactly 'usage: hello [NAME]\n' to stderr and exits with 2 if more than one argument is provided.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprint(stderr, "usage: hello [NAME]\n")
		return 2
	}

	name := ""
	if len(args) == 1 {
		name = args[0]
	}

	greeting := greet.Greeting(name)
	fmt.Fprintln(stdout, greeting)

	return 0
}
```

`internal/greet/greet.go`:

```go
package greet

import "fmt"

// Greeting returns a greeting string for the given name. If the name is empty, returns exactly 'Hello, World!'. Otherwise, returns 'Hello, <name>!' without a newline.
func Greeting(name string) string {
	if name == "" {
		return "Hello, World!"
	}
	return fmt.Sprintf("Hello, %s!", name)
}
```

## Final build, vet and test results

Run in a fresh temporary copy of the generated repo (without `.gophermind`), Go go1.27.1:

```
go build ./... && go vet ./... && gofmt -l . && go test ./...
ok  	hello/cmd/hello	0.318s
ok  	hello/internal/cli	0.320s
ok  	hello/internal/greet	0.319s
exit 0
```

Build and vet printed nothing, `gofmt -l .` listed no files, and the other three test results are the `ok` lines above. The temporary directory was removed afterwards.

## CLI acceptance results

Built with `go build -o ./hello ./cmd/hello` in the same copy. Stdout, stderr and status captured separately.

| Case | Stdout | Stderr | Exit status |
|---|---|---|---|
| `./hello` | `Hello, World!\n` | empty | 0 |
| `./hello Ada` | `Hello, Ada!\n` | empty | 0 |
| `./hello a b` | empty | `usage: hello [NAME]\n` | 2 |

## Remaining known limitations

The run used John's library brief hello v1.1, not the regression brief in the spec (same program, six provable acceptance bullets). The local model needed one coverage fill round on the proof run: acceptance bullet A1 had no root test, and the model had forbidden `os.Args` in C4, which the plan refused as `forbids_brief_token` and the model corrected. The run before it (r010, on a5a38158) needed one revision each for fn-greeting and fn-run. Parked findings, one line each:

### build/planner

- `FixtureProvider`: a stage with no fixture file is retried until the 10 minute test timeout; it should return a no-retry error.
- `cli_surface_missing` reads the example text `--Go & Rust` as a flag.
- `tooLongErr` says the brief sources are never cut for every sized stage, though outline, repair and schema-fix excerpts are cut.
- `sources.json` and `component_sources` list the level 1 set for level 0 packets; the file is written only when the outline completes in the same run.
- The `component_sources` event message repeats its kind prefix.
- Stale labels after the contract changes (`contract_component.md`, `decompose.md`, `enrich.md`, `enrich_structure.md`, `testwriter.md`); "report the conflict" has no reply field.
- `componentBrief` level 1 emits `### <feature>` without the `## Features` parent; `featuresOfComponents` ignores its `comps` argument.
- A missing `main` is now a hard error after two re-asks instead of the `main_function_missing` warning.
- Fixed interfaces with a receiver or type parameter are ignored silently; two `cmd/` directories both get the first bullet's `EntryCall`.
- `packetParts` scans `##` lines inside code fences; a case-only package mismatch passes `localChecks`; the slugify `fn-run` assertion depends on component order.
- Decompose and enrich send the whole brief with no sizing, so an over-window brief fails with the router's too-long error.
- Two sibling components in one directory may each write the fixed function (first exact match wins silently); `ownPackage` with a bare name accepts any path ending in `/name`; a module-prefixed `cmd/` bullet gets no fixed `main`.
- Architecture parsing is brittle: `fixedSpanRE` rejects a `func ` prefix, an entry call on a different bullet gives none, and a bullet starting with a backtick span is read as a directory.
- Public providers at node scope now see the brief's feature text and every section but Overview (a policy change to put to John).
- Literal check: placeholder and number literal matching is loose; trigger verbs match by plain substring; `ReadCoverage` and `loadContracts` errors are swallowed; the example rule can be fooled by a one-letter fill; "to" and "on" skip a result written right after them; a `\n` inside a literal no longer matches a doc in backslash form; the revise path has no literal check.
- Test obligations: short literals match by substring; `expectedTexts` skips any backticked span shaped like a call; fill root tests naming dropped test functions or `_test.go` paths are kept; a "Tests run in under 2 seconds" style line can be a false positive.
- Quality gate: `forbidsBriefTokens` negation is word based and can over-exclude "not only" phrasing; an unterminated heredoc is now a `syntax_error` though bash only warns; `group()` does not skip comments or heredoc bodies inside `$( )`; no tests for well-formed quote cases; the prompt cannot vet `prompts/harness.md`, which shows refused forms on purpose.
- Task 9: pending kinds held in memory are lost on a restart mid-loop (diagnostic only); `rejected:stage` and `rejected:error` for errors without a kind.
- The coverage fill's unknown-function and duplicate errors print the reply's id unbounded.
- Revise with the local model sends whole nodes instead of a JSON patch and trips the 1 to 600 byte paragraph cap; revise could accept a whole node and compute the patch (Milestone B).
- Task 3c: no wiring-point test; the coverage prompt line undersells the prefix list.
- A given `main.go` check: a build-tagged `main.go` counts as given; the `ReadDir` error is swallowed; no two-file main test.

### build/acceptcheck

- `inverted_check` only at top level with `|| ` directly after grep; `[[ ]]` not covered by the print-substitution test; a `$?` read after a different chain is flagged.
- `x=$(cd d; hello)` is not caught; no nested or quoted `$(` tests.
- `status_masked` scope is narrow; hello coverage fixture line 75 and slugify 110 and 115 are refusable but untested.
- Go verbs for the tool check are limited to vet, test, build.
- GNU `mktemp --tmpdir=` is flagged.

### build/executor

- A cosmetic `acceptance <id>:` header is written for constraint bullets too (ruling wanted `constraint <id>:`).
- The "same file twice" message reaches only the `ladder_ended` event, not the gate.
- A human retry followed by one identical reply ends the ladder again.
- dash's capitalised `Syntax error:` is not matched (Linux hosts); the v2 executor spec prose does not list `acceptance_test_invalid`; the EOF form shares a test with the exit-2 negative.
- The macOS `mktemp` ignoring `TMPDIR` is handled by the planner rule and an executor stop; the sandbox is deliberately not widened to `/var/folders`.
- Executor truncation classified as `provider_unavailable` is out of this plan.

### build/packer

- The trailing note is also emitted when only the contract declares the name; the hint is still emitted when a name is declared both in the failing dir and elsewhere.
- Generic types and `type (` blocks fall back to the note; no tests for the type sentence or the Greet/Greeting prefix.
- The packer's "failure lines to 10" cut keeps the first lines, so appended hints can be dropped on long output (pre-existing).

### build/router

- None parked beyond the planner items above that use its error kinds.

### serve

- Load-timing flake: `TestBriefEventsEndsWhenRunIsFinal` (a run that finishes before the second SSE event is read sends a final `state` before `end`); passes alone with -count=3, no change in this range touches it.

## Definition of done

The Definition of done in the brief is "a fresh GopherMind run of that brief". Evidence is the proof run r011 on 695cef47, using the library brief hello v1.1 (same program as the regression brief).

- [x] 1. Preserves the explicit architecture: met. Three components and packages `internal/greet`, `internal/cli`, `cmd/hello` as in `contracts.json`; `TestHelloContractKeepsTheFixedArchitecture` guards it.
- [x] 2. Produces `Greeting`, `Run` and `main` with exact signatures: met. `func Greeting(name string) string`, `func Run(args []string, stdout, stderr io.Writer) int`, `func main()`.
- [x] 3. Does not introduce stdin: met. The generated tree reads only `os.Args`; no stdin use in `cli.go` or `main.go`.
- [x] 4. Does not replace `Greeting`: met. One `Greeting` in `internal/greet/greet.go`, used by fn-run; no other greeting function in the tree.
- [x] 5. Creates no extra command-local runner: met. `cmd/hello/main.go` holds only `main`, which calls `cli.Run`.
- [x] 6. Preserves success and error behaviour in `Run`: met. CLI cases: exit 0 with greeting, exit 2 with usage on stderr and empty stdout.
- [x] 7. Generated tests independently verify the original brief: met. Test obligations T1 to T9 attach to fn-greeting and fn-run (T5 unattached by design); tests assert the brief's literals (`TestTestFileMustAssertTheEscapedNewline`, `TestRunWithoutGreetingFailsF1`).
- [x] 8. `os.Exit` only in the validated process entry point: met. `os.Exit` appears only in `cmd/hello/main.go`; packer and executor tests (`TestExitRefusedInRun`, `TestExitRefusedOutsideMain`) enforce it.
- [x] 9. Invalid model contract proposals are rejected before canonical state: met. Conflicts, fixed-interface violations and test functions are refused at merge (`TestSecondDifferentDeclarationIsAConflict`, `TestFixedInterfaceStopStoresTheStateBeforeTheRefusedPass`).
- [x] 10. Generates valid acceptance checks: met. Root tests for A1 to A6, C1, C2, C4, C5 passed the acceptcheck gate; the forbidden `os.Args` check was refused and corrected in the fill round.
- [x] 11. Builds successfully: met. `go build ./...` exit 0 in the copy; acceptance A1 pass.
- [x] 12. Passes `go vet`: met. `go vet ./...` printed nothing; A2 and the C5 root test passed.
- [x] 13. Passes all tests: met. `ok` for `hello/cmd/hello`, `hello/internal/cli`, `hello/internal/greet`; A3 pass. (The gophermind-lib suite result is pending, see above.)
- [x] 14. Passes all three binary-level acceptance cases: met. Table above: `./hello` 0, `./hello Ada` 0, `./hello a b` 2 with the exact usage line on stderr.
- [x] 15. Reaches completion without an implementation or revision loop caused by a contradiction GopherMind created: met. r011: 3 of 3 nodes verified on the first attempt, no revisions, no escalations. The one coverage fill round is a planning-time correction, not an implementation loop. (r010 needed one revision each for fn-greeting and fn-run; its contradictions were the cause of Tasks 4e and 3h, now fixed.)
