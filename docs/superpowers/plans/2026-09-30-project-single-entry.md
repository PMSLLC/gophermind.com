# `/project` Single Entry Point Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/project` the single entry point of the v2 engine: one library call, `projectrun.Run`, behind the slash command `/project <brief>` and the CLI `gophermind project <brief-path>`, that runs load, clarify, contract, decompose, coverage, approve and test-writer with the v2 planner, then `executor.Run`, then prints one final report, in one process, with no second command and no human step. A preflight fails fast, before any model call, listing exactly what a human must provide; an unattended policy takes every clarify default, provisions secrets from the vault or generates them, and approves the plan by hash; the v1 planner moves to `/plan-v1`; the state of an attempt is printable and clearable; the binary that runs a graded attempt is built from a clean pushed commit and stamps its version into the report.

**Architecture:** One new package `gophermind-lib/briefv2/projectrun` (args, preflight, secrets, unattended gate, state paths, report, `Run`), two thin callers (`cmd/gophermind/project.go`, `gophermind-lib/tui/project_v2.go`), a small planner extension (`Options.Repo`, `Options.Unattended`, three exported readers), a mechanical rename of the v1 TUI commands, a dev-binary script, and docs. `projectrun` depends on `planner`, `executor`, `report`, `settings`, `vault`, `router`, `blackboard`, `ledger`, `db`, `human`, `events`, `brief`, `sandbox`, `gitland`, `version`; nothing depends on it except the two callers.

**Tech Stack:** Go (the module's version), the standard library (`net`, `net/url`, `crypto/rand`, `os/exec`, `flag`), `modernc.org/sqlite` and `gopkg.in/yaml.v3` (already dependencies), the `git` CLI, bash for one script. No new dependency.

**Spec:** `docs/superpowers/specs/2026-09-30-project-single-entry-design.md` (binding; read all of it first). Goal and bar: `docs/GOAL.md` in the main checkout (`/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com`), tasks 4 and 5. Executor spec and plan: `docs/superpowers/specs/2026-09-30-v2-executor-design.md`, `docs/superpowers/plans/2026-09-30-v2-executor.md`.

**Prerequisite order:** Tasks 1 to 8 need only the planner and packages already built. Task 9 compiles against `executor.Run`, `executor.Options` and `report.Report` and needs executor plan Task 12b or later committed; Task 14 needs executor Task 16 (the greeter rig and scripted fixtures) committed. If the executor is not done when Tasks 9 to 15 start, stop and report; do not stub the executor in production code.

## Global Constraints

- Pure Go, no cgo, no new dependency. New code lives under `gophermind-lib/briefv2/projectrun/`, `cmd/gophermind/project.go`, `gophermind-lib/tui/project_v2.go`, `scripts/build-dev-binary.sh`. Do not modify an existing package except the named exceptions: Task 1 (`planner`: `planner.go`, `load.go`, `clarify.go`, `exported.go`, new test files and one fixture folder), Task 11 and 12 (`gophermind-lib/tui`: the listed files), Task 10 (`cmd/gophermind/main.go`: one routing block and the usage text), Task 13 (`Makefile`: one target), Task 15 (`docs/briefv2/README.md`, `CHANGELOG.md`).
- Tests never use the real network (loopback listeners and `httptest` are allowed) and never read or write the real `~/.gophermind`: use `t.TempDir()`, `t.Setenv("GOPHERMIND_CONFIG_DIR", ...)`, `t.Setenv("GOPHERMIND_VAULT_PATH", ...)`, and the vault's low work factor option (`vault.Options`, see `cmd/gophermind/brief_test.go` `vaultOptions`).
- No reply text, prompt text, command output or secret value in an error, event, log line, ledger row, blackboard row or printed report. Printed reports carry ids, counts, names, statuses and hashes only; model-written text (clarify questions and answers) lives in `answers.json` and `project.json` (mode 0600). Secret values appear only inside the vault and in a command's environment.
- Any delete, in code or in a shell block, assigns its target to a variable, tests it (non-empty, expected prefix and parent, never `/` or the home directory) and only then deletes; the shell form is `rm -rf -- "${target:?}"` (or `rm -f -- "${target:?}"` for one file). `projectrun` itself deletes nothing.
- Preflight writes nothing, opens no database, makes no model call, and never creates a settings file.
- Tests are TDD: write the failing test, see it fail for the right reason, then implement. Tests pass with `-race`; code is gofmt-clean and `go vet` clean. No em dashes and no emojis in code, comments, docs or commit messages.
- In a git worktree the ignored `desktop/frontend/dist` folder is missing: build with `go build ./cmd/... ./gophermind-lib/...` in `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`.
- The worktree holds unrelated uncommitted work (executor tasks in progress). Stage explicit paths only; never `git add -A` or `.`; never `git stash`; never `git checkout`; never a force flag.
- Commit messages end with two trailers, each on its own line after a blank line: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ`. After each task's commit run `git push origin feat/briefv2-planner-core`. Never push to another branch.
- Working directory for every command: `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`, branch `feat/briefv2-planner-core`.

## Decisions specific to this plan

| # | Choice | Why |
|---|---|---|
| P1 | The package is `briefv2/projectrun`, not `project` | `gophermind-lib/project` already exists (repo context); two packages named `project` invite wrong imports |
| P2 | One `Env` struct holds every seam (settings, providers, vault, db, git, dial, clock, version, executor call); `DefaultEnv()` fills production values; tests replace fields | No global hooks; `cmd` and `tui` tests pass an `Env` |
| P3 | `Unattended` is a planner `Options` field in addition to the gate | Clarify and mid-stage questions never reach the gate in the assume branch; the gate alone cannot change the planner's prompt or answer records |
| P4 | Clarify under `Unattended` needs non-empty defaults, enforced in the parse closure by a new helper `requireDefaults`, not by changing `parseClarify`'s signature | The router's malformed retry repairs a missing default; existing tests keep compiling |
| P5 | Secrets are provisioned at run start by `--generate NAME=KIND` flags, not by a settings section | The command line is logged, explicit, and needs no settings schema change |
| P6 | Database URL checks parse the vault value and dial TCP; no driver, no emptiness check | No new dependency; the clear procedure owns emptiness and the preflight prints that note |
| P7 | Read-only git in preflight goes through one `Env.Git` function that accepts a fixed set of argument vectors | Same discipline as `gitland`: no shell, no write verb |
| P8 | Preflight and progress print to stderr, the final report to stdout | The orchestrator redirects both into one log; stdout ends with the two proof lines |
| P9 | The dev binary path is `~/.gophermind/bin/gophermind-dev` | The brew cask at `/opt/homebrew/bin/gophermind` stays 0.9.0 and untouched |
| P10 | `--print-state-paths` output is tab separated lines, `action scope path` | The orchestrator reads it with `awk`; JSON adds nothing |

### Spec gaps and the ruling taken

| # | Gap found while planning | Ruling |
|---|---|---|
| G1 | `planner.resolveRepo` is unexported and takes the brief's `repo:`; the preflight needs the same resolution | Task 1 exports `planner.ResolveRepo` (a wrapper, behavior unchanged) |
| G2 | `answers.json` and `approval.json` have unexported types; the report needs both | Task 1 exports `ReadAnswers` and `ReadApproval` |
| G3 | Spec 5 says the executor's gate is the unattended gate; executor spec 7.4 says a nil gate also stops | Pass the unattended gate anyway so the stop is explicit and testable |
| G4 | Spec 7 says provider keys must be checked; `settings.BuildProviders` fails on the first missing key | The preflight calls `Env.BuildProviders` with a recording lookup and reports its error text (it names the secret, never a value) |
| G5 | Spec 9 lists the vault run scope as cleared by overwrite; `vault.Vault` has no delete | Overwrite on a fresh start is sufficient: every declared name is rewritten; undeclared names cannot exist because the scope is per brief id |
| G6 | `gitland` forbids `branch -D`, so GopherMind cannot delete a stale `gm/<id>` itself | The preflight refuses it and the runbook gives the orchestrator's command (spec 9) |

## Review Focus

1. A fresh run never resumes: leftover run folder or branch is refused unless `--resume`. (Task 4 `TestPreflightStaleStateAndBranch`; Task 14 `TestProjectRefusesStaleStateWithoutResume`, `TestClearedStateRerunsClean`)
2. No human step: in unattended mode nothing reads stdin and the gate's `Ask` fails loudly; the planner never calls it. (Task 2 `TestUnattendedGateAskFailsLoudly`; Task 14 `TestProjectE2EGreeter` runs with a nil `In`)
3. Every defaulted or assumed decision is recorded and visible: answers with `Assumed`, counts in the report, the override of `halt` named. (Task 1 `TestUnattendedClarifyTakesDefaults`; Task 7 `TestPrintedReportHasNoModelText`; Task 14 `TestProjectE2EGreeter`)
4. Approval binds to the plan hash and refuses below N of N. (Task 2 `TestUnattendedGateRefusesGap`; Task 14 `TestUnattendedApproveBindsHash`)
5. Preflight lists every missing item in one pass, calls no model, writes nothing, and exits 6. (Task 5 `TestPreflightMissingListedNoModelCallNothingWritten`; Task 9 `TestRunPreflightFailureStopsEverything`)
6. No secret value anywhere. (Task 3 `TestProvisionSources`; Task 14 `TestNoSecretValueAnywhere`)
7. Every stop condition has a status, a reason and an exit code. (Task 9 `TestEveryStopConditionHasReasonAndCode`, `TestRunMapsExecutorStatusesToExitCodes`)
8. The printed report always ends with the two proof lines, also when the planner stopped. (Task 7 `TestPrintedReportEndsWithProofLines`, `TestProofLinesWhenExecutorDidNotRun`)
9. v1 still works under its new names. (Task 11 `TestPlanV1CommandsRenamed`; the existing `tui` suite passes unchanged except the renamed literals)

## File Structure

```text
gophermind-lib/briefv2/
  planner/     planner.go, load.go, clarify.go, exported.go            modified (Task 1)
               unattended_test.go, repo_override_test.go, testdata/greeter-ask/   new (Task 1)
  projectrun/  options.go, args.go, gate.go, args_test.go, gate_test.go          (Task 2)
               secrets.go, secrets_test.go                                        (Task 3)
               preflight.go, preflight_repo.go, preflight_repo_test.go           (Task 4)
               preflight_env.go, preflight_env_test.go, preflight_test.go        (Task 5)
               statepaths.go, statepaths_test.go                                  (Task 6)
               report.go, sink.go, report_test.go                                 (Task 7)
               env.go, wiring.go, env_test.go                                     (Task 8)
               run.go, run_test.go                                                (Task 9)
               testhelp_test.go, e2e_test.go                                      (Task 14)
cmd/gophermind/project.go, project_test.go, main.go                              (Task 10)
gophermind-lib/tui/  commands_registry.go, update.go, project.go, approve.go, execute.go, questions.go,
                     question_round.go, commands.go, model.go, run.go + their tests   renamed literals (Task 11)
                     project_v2.go, project_v2_test.go                                 (Task 12)
scripts/build-dev-binary.sh, Makefile, cmd/gophermind/devbinary_test.go          (Task 13)
docs/briefv2/README.md, docs/briefv2/project-runbook.md, CHANGELOG.md            (Task 15)
```

## Spec traceability

| Spec item | Task | Tests |
|---|---|---|
| 4 architecture, `ParseArgs`, `Run` | 2, 9, 10, 12 | `TestParseArgsTable`, `TestProjectCLIExitCodes`, `TestSlashProjectRunsProjectrun` |
| 5 flow, one process, planner then executor | 9, 14 | `TestRunCallsExecutorWithPlanArtifacts`, `TestProjectE2EGreeter` |
| 5 `--resume` | 9, 14 | `TestRunResumeSkipsFinishedPlannerStages`, `TestResumeFlagContinuesAndIsRecorded` |
| 6 flags and exit codes | 2, 9, 10 | `TestParseArgsTable`, `TestExitCodes`, `TestProjectCLIExitCodes` |
| 7 preflight, every check | 4, 5 | `TestPreflightRepoChecks`, `TestPreflightStaleStateAndBranch`, `TestPreflightToolsAndSettingsNotCreated`, `TestPreflightRequirePrivate`, `TestPreflightExpectBinaryCommit`, `TestPreflightVaultPassphrase`, `TestPreflightSecretMatrix`, `TestPreflightDatabaseLoopbackAndDistinct` |
| 7 preflight writes nothing, is not a run failure (R7) | 5, 9 | `TestPreflightMissingListedNoModelCallNothingWritten`, `TestPreflightFailureExit6IsNotARun`, `TestRunPreflightFailureStopsEverything` |
| 8.1 secrets, sources, generated shapes, overwrite, keep on resume (R3a) | 3 | `TestProvisionSources`, `TestGenerateShapes`, `TestProvisionOverwritesRunScope`, `TestProvisionKeepsOnResume`, `TestPlanReadOnly` |
| 8.2 clarify defaults, required defaults, mid-stage rule (R3b) | 1 | `TestUnattendedClarifyTakesDefaults`, `TestUnattendedClarifyRequiresDefaults`, `TestUnattendedClarifyFailsWithoutDefaults`, `TestUnattendedClarifyEmptyDefaultFromSavedQuestions`, `TestUnattendedMidStageQuestionAssumes`, `TestAttendedClarifyUnchanged` |
| 8.3 approval and escalation gate, milestone line (R3c, R11) | 2, 7, 14 | `TestUnattendedGateApprovesFullCoverage`, `TestUnattendedGateRefusesGap`, `TestUnattendedGateEscalateStops`, `TestMilestoneLine`, `TestUnattendedApproveBindsHash` |
| 8.4 stop table (R3d) | 9 | `TestEveryStopConditionHasReasonAndCode` |
| 9 state table, clear order, `--print-state-paths` (R4) | 6, 14 | `TestStatePathsListEverything`, `TestStatePathsNoSideEffects`, `TestClearedStateRerunsClean` |
| 9 repo override (R9) | 1 | `TestRepoOverrideUsedForRunFolder`, `TestRepoOverrideMustMatchOnResume` |
| 10 dev binary, version in report (R5) | 7, 13 | `TestReportCarriesBinaryVersion`, `TestDevBinaryScriptRefusesDirtyTree`, `TestDevBinaryScriptPrintsLdflags` |
| 11 report, no model text in print (R10) | 7 | `TestPrintedReportHasNoModelText`, `TestReportJSONRoundTrip` |
| 12 v1 renamed (R2) | 11 | `TestPlanV1CommandsRenamed`, `TestPlanV1PrintsDeprecation` |
| 12 tui `/project` | 12 | `TestSlashProjectRunsProjectrun`, `TestSlashProjectRefusesAttended`, `TestSlashProjectRejectsV1Form`, `TestSlashProjectEscCancels`, `TestSlashProjectSecondRunRefused`, `TestSlashProjectRegistry` |
| 15 tests: secrets canary, e2e | 14 | `TestNoSecretValueAnywhere`, `TestProjectE2EGreeter`, `TestProjectPlanFailureExit1`, `TestProjectEscalationExit4`, `TestProjectInterruptedExit5` |
| 16 human prerequisites | 15 | Step 5 (rehearsal, reported not asserted) |

---

### Task 1: Planner extension: repo override, unattended mode, exported readers

**Files:**
- Modify: `gophermind-lib/briefv2/planner/planner.go` (`Options`, `callAsking`), `load.go` (repo override), `clarify.go` (unattended branch), `exported.go` (new readers)
- Create tests: `planner/unattended_test.go`, `planner/repo_override_test.go`
- Create fixture: `planner/testdata/greeter-ask/` (a variant folder over `testdata/greeter`, used as the first directory of `FixtureProvider`)

**Interfaces:**

```go
// planner.go
type Options struct {
	// ...existing fields...
	Repo       string // plan only: replaces the brief's repo field for the run folder and the run record
	Unattended bool   // no human: Clarify takes each question's default, mid-stage questions use the conservative rule
}

// exported.go
func ResolveRepo(repo string) (string, error)       // wraps resolveRepo; same errors (brief.InvalidError)
type Answer struct {
	ID, Stage, Question, Answer string
	Assumed                     bool
}
func ReadAnswers(runDir string) ([]Answer, error)    // [] when answers.json is absent
type ApprovalInfo struct{ ApprovedAt, ApprovedBy, PlanHash string }
func ReadApproval(runDir string) (ApprovalInfo, bool, error) // false when approval.json is absent
```

**Behavior rules:**
- `load` (plan): `repoField := b.Front.Repo`; when `o.Repo != ""` use it instead; `resolveRepo(repoField)`; the run folder and `RunRecord.Repo` use the resolved path. `load` with `o.RunID` and `o.Repo != ""`: resolve `o.Repo` and compare with `rec.Repo`; a mismatch is `planner: run <id> was planned in <rec.Repo>, not <resolved>`. `RenderPlan` and the approval hash are unchanged (they print the brief's own `repo:`).
- `clarify`: the take-default branch is entered when `OnAmbiguity == "assume_and_document"` **or** `r.opts.Unattended`. Under `Unattended` the parse closure also calls `requireDefaults(qs)`, which returns `clarify reply: question <n> has no default_if_unanswered` for a blank default (so the router retries once and a second failure fails the stage). Under `Unattended` an empty default found in the take-default branch (questions saved by an earlier attended run) returns `planner: clarify: question <id> has no default and no human is available`; the fixed "most conservative option" sentence stays for `assume_and_document` only. Every answer is `Assumed: true`.
- `callAsking`: the conservative branch condition becomes `OnAmbiguity == "assume_and_document" || r.opts.Unattended`.
- No other planner behavior changes; with `Unattended` false every existing test passes untouched.

**Tests:**
- `TestUnattendedClarifyTakesDefaults`: fixture `greeter-ask/clarify.txt` has two questions with defaults; brief front matter `on_ambiguity: halt`; `Options{Unattended: true, StopAfter: "clarify"}`, nil gate; `answers.json` holds two answers, both `Assumed`, text equal to the defaults; no `QUESTIONS.md`; outcome `Done`.
- `TestUnattendedClarifyRequiresDefaults`: first reply omits a default, `clarify.2.txt` supplies them; the ledger shows one `malformed` then one `ok` clarify row; answers are the defaults.
- `TestUnattendedClarifyFailsWithoutDefaults`: both replies lack a default; `Run` returns an error containing `default_if_unanswered`; no `answers.json`.
- `TestUnattendedClarifyEmptyDefaultFromSavedQuestions`: `_state/clarify.json` with a blank default is placed before the run; the error contains `no default and no human`, and no answer is written.
- `TestUnattendedMidStageQuestionAssumes`: a contract reply `QUESTION: ...` with `Unattended` and `halt`, nil gate; the second request's prompt contains `No human is available`; no `_state/question.json` remains; the run proceeds.
- `TestAttendedClarifyUnchanged`: `Unattended` false, `halt`, the file gate: outcome `Waiting`, `QUESTIONS.md` exists.
- `TestRepoOverrideUsedForRunFolder`: brief `repo:` names a missing directory; `Options.Repo` is a temp repo; the run folder is `<override>/.gophermind/<id>` and `LookupRun(id).Repo` equals the override.
- `TestRepoOverrideMustMatchOnResume`: resume with a different `Options.Repo` errors with `was planned in`; resume with the same path succeeds.
- `TestReadAnswersAndApproval`: `ReadAnswers` round-trips two answers; absent file gives an empty slice; `ReadApproval` returns `ApprovedBy` and `PlanHash`, and `ok == false` before approval.
- `TestResolveRepo`: `~` expansion, a URL refused with `brief.InvalidError`, a missing directory refused.

- [ ] **Step 1: Write the fixture folder and the failing tests.** Read `planner/harness_test.go` and `planner/clarify_test.go` first and reuse their helpers; copy the front matter handling of the existing greeter brief.
- [ ] **Step 2: Run** `go test ./gophermind-lib/briefv2/planner/ -run 'Unattended|RepoOverride|ReadAnswers|ResolveRepo|AttendedClarify'` and see them fail (unknown fields, missing functions).
- [ ] **Step 3: Implement** in this order: `exported.go`, `planner.go` Options, `load.go`, `clarify.go`, `callAsking`. Run the focused tests after each file.
- [ ] **Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/planner/... && go test -race ./gophermind-lib/briefv2/planner/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/planner
git commit -m "feat(briefv2): planner repo override, unattended mode and exported readers" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

(`git add gophermind-lib/briefv2/planner` stages only that folder; check `git diff --cached --stat` first and unstage anything the executor work put there.)

---

### Task 2: Options, argument parsing, exit codes, and the unattended gate

**Files:**
- Create: `gophermind-lib/briefv2/projectrun/options.go`, `args.go`, `gate.go`
- Create tests: `args_test.go`, `gate_test.go`

**Interfaces:**

```go
// options.go
const (
	ExitVerified, ExitFailed, ExitInvalid, ExitEscalated, ExitInterrupted, ExitPreflight = 0, 1, 2, 4, 5, 6
)
type Options struct {
	BriefPath          string
	Repo               string            // --repo
	Attended           bool
	Resume             bool
	RequirePrivate     bool
	ExpectHead         string
	ExpectBinaryCommit string
	Generate           map[string]string // secret name -> "hex32" | "placeholder"
	PreflightOnly      bool
	PrintStatePaths    bool
	In                 *os.File          // nil unless attended
	Out, Err           io.Writer         // Out: final report; Err: progress and preflight
}
type Result struct{ ExitCode int; Status, StopReason string }

// args.go
func ParseArgs(args []string, allowAttended bool) (Options, error)

// gate.go
var ErrUnattended = errors.New("projectrun: no human is available in an unattended run")
type unattendedGate struct{}            // implements human.Gate
```

**Behavior rules:**
- `ParseArgs`: exactly one positional, the brief path, before or after the flags. Flags as in spec section 6. `--generate` is repeatable; each value is `NAME=KIND` with `NAME` matching `^[A-Z][A-Z0-9_]*$` and `KIND` one of `hex32`, `placeholder`; a repeated name is an error. `--attended` with `allowAttended` false: `attended runs need a terminal and are only available from the gophermind project command`. `--preflight-only` with `--print-state-paths`: error. Two positionals: `the v1 form "/project <name> <brief>" is gone: give the brief path only (the v1 planner is /plan-v1)`. No positional: usage error. Unknown flag: error naming the flag. Errors never echo a flag value that could be a secret (`--generate` errors name the flag and the position only).
- Gate: `Ask` returns `ErrUnattended`. `Approve` reads `Requirements covered: (\d+) of (\d+)` (first match in `plan.Markdown`, multiline, anchored to the line); `Approved` is true only when a match exists, `N > 0` and `C == N`; `By` is `unattended`; `Note` is `plan <first 12 hex of plan.Hash>` when approved and `coverage <C> of <N>` or `no coverage line` when refused. `Escalate` returns `Resolution{Action: human.ActionStop}`.

**Tests:** `TestParseArgsTable` (flags before and after the positional; repeated `--generate`; bad kind; bad name; duplicate name; `--attended` refused when not allowed and accepted when allowed; both query flags; two positionals gives the v1 message; none gives usage; unknown flag), `TestUnattendedGateApprovesFullCoverage`, `TestUnattendedGateRefusesGap` (C less than N, N zero, line missing), `TestUnattendedGateEscalateStops`, `TestUnattendedGateAskFailsLoudly` (`errors.Is(err, ErrUnattended)`), `TestExitCodes` (constants are 0, 1, 2, 4, 5, 6 and 3 is unused: a test asserts no constant equals 3).

- [ ] **Step 1: Write the failing tests.** **Step 2: Run** `go test ./gophermind-lib/briefv2/projectrun/` and see compile failures. **Step 3: Implement** `options.go`, `args.go`, `gate.go`.
- [ ] **Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/projectrun/... && go test -race ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun
git commit -m "feat(briefv2): projectrun options, argument parsing and the unattended gate" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 3: Secret provisioning

**Files:** Create `gophermind-lib/briefv2/projectrun/secrets.go`, `secrets_test.go`.

**Interfaces:**

```go
type SecretStore interface {
	Get(scope, name string) (string, bool)
	Set(scope, name, value string) error
}
type Source string
const (
	SourceVault       Source = "vault"
	SourceHex32       Source = "generated:hex32"
	SourcePlaceholder Source = "generated:placeholder"
	SourcePrompt      Source = "prompt"
	SourceKept        Source = "kept"      // resume: the run scope already held it
)
type Provisioned struct{ Name string; Source Source }
type Missing struct{ Name, Purpose string }

func Generate(kind string, rnd io.Reader) (string, error)
// Plan is read only: for each declared secret, where its value would come from.
func Plan(b *brief.Brief, store SecretStore, gen map[string]string, canPrompt bool) ([]Provisioned, []Missing)
func Provision(store SecretStore, b *brief.Brief, gen map[string]string, prompt func(name, purpose string) (string, error), resume bool, rnd io.Reader) ([]Provisioned, error)
```

**Behavior rules:**
- Order per secret: harness scope value (`vault.HarnessScope`) then `gen[name]` then `prompt` (only when non-nil, i.e. attended). Anything else is missing. A harness value beats a generated one.
- `Generate("hex32")` reads 32 bytes and returns 64 lowercase hex characters; `"placeholder"` returns `gm-placeholder-` plus 32 hex characters (16 bytes); any other kind is an error; a short read is an error.
- `Provision` with `resume == false` writes every resolved value to `vault.RunScope(b.Front.ID)`, overwriting. With `resume == true` a name already in the run scope is left alone and reported as `SourceKept`; a missing one is resolved as usual. Returned errors list missing names (and purposes), never values. A nil `store` with no declared secrets is fine; with declared secrets it is an error.
- `Plan` never calls `Set` and never calls `prompt`.

**Tests:** `TestProvisionSources` (vault wins over generate; generate used when the vault lacks it; prompt used only when given; missing listed with purposes), `TestGenerateShapes` (lengths, charset, prefix, unknown kind, failing reader), `TestProvisionOverwritesRunScope` (stale run-scope value replaced after the harness value changes), `TestProvisionKeepsOnResume`, `TestPlanReadOnly` (a store whose `Set` fails the test), `TestProvisionMissingListsAllNames` (two missing names both in the error, a canary value in another secret absent from it).

- [ ] **Step 1: Write the failing tests** with an in-memory `SecretStore` and a deterministic `io.Reader`. **Step 2: Run and see failures. Step 3: Implement.**
- [ ] **Step 4: Verify and commit** (same three commands as Task 2; message `feat(briefv2): projectrun secret provisioning from the vault or generated`).

---

### Task 4: Preflight, part 1: the check framework, repo, state and landing

**Files:** Create `gophermind-lib/briefv2/projectrun/preflight.go`, `preflight_repo.go`, `preflight_repo_test.go`.

**Interfaces:**

```go
type Check struct {
	Name   string
	OK     bool
	Detail string // ids, paths, counts; never a secret, never command output
	Fix    string
}
func Failed(cs []Check) []Check
func PrintPreflight(w io.Writer, cs []Check)   // "preflight: ok (N checks)" or "preflight: FAILED (K missing)" then one "  missing: <name>: <detail>. Fix: <fix>" line each, then the not-a-run-failure sentence
func Preflight(ctx context.Context, o Options, env Env, b *brief.Brief) []Check   // runs every check, fixed order, no early exit
// preflight_repo.go
func checkRepo(ctx context.Context, o Options, env Env, b *brief.Brief) []Check
func checkStale(ctx context.Context, o Options, env Env, b *brief.Brief) []Check
func checkLanding(b *brief.Brief) Check
```

`Env` is defined fully in Task 8; this task adds only the fields it needs to a first version of `env.go` (`Git func(ctx, repo string, args ...string) (string, error)`, `Getenv`, `Version`) and Task 8 extends it.

**Behavior rules:**
- `Preflight` order: repo, stale state, landing, then the checks of Task 5. Each check function returns its own `[]Check`. The aggregate calls no model, opens no database, and writes nothing.
- repo: resolved path (`planner.ResolveRepo(o.Repo or brief repo)`), a git work tree whose top level is that path, `refs/heads/<base_branch>` exists, the current branch (`symbolic-ref --short HEAD`) equals the base branch (a detached HEAD fails), `status --porcelain -uall` has no line other than paths under `.gophermind/`, and with `ExpectHead` `rev-parse HEAD` equals `rev-parse --verify <rev>^{commit}`. Details name the branch, the rev or a count of dirty paths, never file contents. With `o.Resume` the clean-tree and branch checks are skipped (the executor applies its own resume dirt policy) but the path and work-tree checks stay.
- stale: fresh run: `<repo>/.gophermind/<id>` must not exist and `branch --list gm/<id>` must be empty; the Fix text is `clear the attempt (see docs/briefv2/project-runbook.md): switch to <base>, delete branch gm/<id>, reset, clean, or pass --resume`. With `--resume`: the run folder must exist and `planner.LookupRun(id)` must succeed with a matching repo, else `nothing to resume`.
- landing: `gitland.ValidateLanding(b.Front.Landing)`.
- `Env.Git` accepts only these argument vectors (anything else returns an error): `rev-parse` (any of `--is-inside-work-tree`, `--show-toplevel`, `--verify ...`, `HEAD`), `symbolic-ref --short HEAD`, `status --porcelain -uall`, `branch --list <name>`. It runs `git -c core.hooksPath=/dev/null -c core.fsmonitor=false --no-optional-locks ...` with `Dir` set and no shell.

**Tests** (real `git` in temp repos; `skipIfNoGit(t)`): `TestPreflightRepoChecks` (clean repo on `main` passes; each of dirty tracked file, untracked file, other branch checked out, detached HEAD, missing base branch, not a git repo, `--expect-head` mismatch and match fails or passes as named; `.gophermind/` dirt ignored), `TestPreflightStaleStateAndBranch` (run folder present without `--resume` fails; branch `gm/<id>` present fails; both absent passes; with `--resume` a missing folder fails and a present one with a run record passes), `TestPreflightLanding` (`commit` passes; `pull_request` fails naming the field), `TestPreflightGitArgsAllowList` (`Env.Git` refuses `push`, `reset`, `checkout`, `branch -D`, and a `status` with extra flags), `TestPrintPreflight` (ok and failed formats; a failed block ends with the not-a-run-failure sentence; no detail contains a path outside the repo).

- [ ] **Step 1: Write the failing tests. Step 2: Run and see failures. Step 3: Implement** `env.go` (partial), `preflight.go`, `preflight_repo.go`. **Step 4: Verify and commit** (`feat(briefv2): projectrun preflight framework and the repo, stale-state and landing checks`).

---

### Task 5: Preflight, part 2: settings, tools, providers, binary, vault, secrets, databases

**Files:** Create `gophermind-lib/briefv2/projectrun/preflight_env.go`, `preflight_env_test.go`, `preflight_test.go`.

**Interfaces:**

```go
func checkSettings(env Env, o Options) (*settings.Config, []Check)   // never creates the file
func checkTools(ctx context.Context, env Env, cfg *settings.Config) []Check
func checkProviders(ctx context.Context, env Env, cfg *settings.Config, o Options) []Check
func checkBinary(env Env, o Options) Check
func checkVaultAndSecrets(env Env, o Options, b *brief.Brief) (SecretStore, []Check)
func checkDatabases(ctx context.Context, env Env, b *brief.Brief, store SecretStore) []Check
```

`Env` gains `SettingsPath`, `LoadSettings`, `BuildProviders`, `LookPath`, `SandboxPreflight`, `Dial`, `VaultPath`, `OpenVault` (Task 8 completes the production values).

**Behavior rules:**
- settings: `os.Stat(SettingsPath())`; missing is `gophermind.yaml not found at <path>` (never `settings.Load`, which would write defaults); otherwise `LoadSettings`; an error is the check's detail (settings errors carry no secrets). Every tier name in `cfg.Models` must resolve (each entry `provider/model` names a configured provider). With `RequirePrivate`: `privacy.mode` must be `private_only` and every entry's provider must be `settings.Private`; the detail lists provider names.
- tools: `env.LookPath("go", cfg.Toolchain["PATH"])` and `git`; on darwin with `cfg.Executor.Sandbox != "off"` `env.SandboxPreflight(ctx)` must pass; off darwin `executor.sandbox` must be `off` explicitly. The detail names the `toolchain.PATH` setting.
- providers: dial `host:port` of each provider used by a tier (default ports 80 and 443 by scheme) through `env.Dial` with a 5 second bound (no HTTP request). Then `env.BuildProviders(cfg, lookup)` where `lookup` reads the harness scope of the opened vault when available and otherwise returns an error; its error text is the detail. An unreachable provider names the provider and address.
- binary: with `ExpectBinaryCommit` set, `Version().Commit` must start with it and must not be `none`; otherwise ok. The detail prints version and commit.
- vault and secrets: when `len(b.Front.Secrets) == 0` no check. Otherwise `env.Getenv("GOPHERMIND_VAULT_PASSPHRASE")` must be non-empty (name the variable), the vault must open (`env.OpenVault(path, pass)`; a missing file opens empty, a wrong passphrase fails), then `Plan(...)` yields one check per secret (`secret <NAME>`: ok with its source, or missing with Fix `gophermind brief vault set <NAME>`; with `Attended` a prompt counts as a source). The opened store is returned for the database check and never stored in a `Check`.
- databases: for every declared secret whose harness-scope value parses with `net/url` as scheme `postgres` or `postgresql`: the host must be loopback (`localhost`, `127.0.0.0/8`, `::1`), else `non-loopback host <host:port>: the sandbox denies it; tunnel it to 127.0.0.1` (host and port only, never userinfo or database name); `env.Dial` to `host:port` must succeed; two such secrets with the same host, port and database name fail as `<A> and <B> name the same database`. After the checks one informational check `databases` states `note: database emptiness is not checked` with `OK: true`. A secret that is only generated is skipped.
- Warnings: `b.UndeclaredSecrets()` becomes `OK: true` checks named `warning` (printed as `warning:` lines).

**Tests:** `TestPreflightToolsAndSettingsNotCreated` (missing settings file: failed check, and the config dir still has no `gophermind.yaml`; `go` not on the PATH; sandbox refused on darwin unless `off`), `TestPreflightRequirePrivate` (public entry fails; `privacy.mode: need_to_know` fails; all private passes), `TestPreflightProvidersReachable` (loopback listener up passes, closed port fails naming the provider; a missing provider key surfaces the `BuildProviders` error), `TestPreflightExpectBinaryCommit` (prefix match, mismatch, `none`), `TestPreflightVaultPassphrase` (empty env var fails naming it; wrong passphrase fails; missing file passes), `TestPreflightSecretMatrix` (vault value, `--generate`, both, neither, attended prompt; per-secret lines; a canary value never appears in any `Detail` or `Fix`), `TestPreflightDatabaseLoopbackAndDistinct` (non-loopback host fails, refused port fails, same database twice fails, distinct loopback passes; detail has no password), `TestPreflightMissingListedNoModelCallNothingWritten` (a repo, config dir and Env with a counting fake provider: everything missing at once yields all failed items in one `Preflight` call, the provider call count is 0, `Env.OpenDB` was never called, and a before and after walk of the config dir, the vault directory and the repo shows no new file), `TestPreflightFailureExit6IsNotARun` (covered with `Run` in Task 9; here only `Failed` and the summary sentence).

- [ ] **Step 1: Write the failing tests. Step 2: Run and see failures. Step 3: Implement. Step 4: Verify and commit** (`feat(briefv2): projectrun preflight checks for settings, tools, providers, vault, secrets and databases`).

---

### Task 6: State paths

**Files:** Create `gophermind-lib/briefv2/projectrun/statepaths.go`, `statepaths_test.go`.

**Interfaces:**

```go
type StatePath struct{ Action, Scope, Path string }
// Actions: git_clean, git_branch_delete, git_reset, delete_file, rows_cleared_at_start, keep, external
func StatePaths(o Options, b *brief.Brief, env Env) ([]StatePath, error)
func PrintStatePaths(w io.Writer, ps []StatePath)   // "<action>\t<scope>\t<path>" per line
```

**Behavior rules:** exactly the table of spec section 9, in its order: `git_clean` for `<repo>/.gophermind/<id>` and `<repo>/.gophermind/<id>-scratch`; `keep` for `<repo>/.git/info/exclude`; `git_branch_delete` for `gm/<id>` (path column is the branch name); `git_reset` for the repo work tree (path is the repo); `delete_file` for `<config dir>/runs/<id>.json`; `rows_cleared_at_start` for `<db path>` with scope `global-file-per-run-rows` and the run id in an extra trailing field is not used (the id is in the path column as `<db path>#run_id=<id>`); `keep` for the vault path, the settings path and the module cache (from `cfg.Executor.GoModCache` when the settings file exists, else the default `~/.gophermind/gomodcache` with `~` expanded against the real home); `external` for the databases: path column `declared secrets: <names>; drop and recreate the databases behind the postgres URLs`. Scope column values: `per-project-repo`, `per-project-repo-git`, `per-project-global`, `global`, `external`. Paths come from `Env` (`ConfigDir`, `VaultPath`, `SettingsPath`, `DBPath`) and honor `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH`. The function reads no vault, opens no database, dials nothing, and creates nothing (the settings file is read only if it exists).

**Tests:** `TestStatePathsListEverything` (every row of spec 9 present with the right action and scope, run id and repo substituted, the config dir honored), `TestStatePathsNoSideEffects` (the config dir, repo and vault path are byte-identical and no file is created; no network call through a failing `Dial`), `TestStatePathsNoSecrets` (a canary secret value in a vault next to it never appears), `TestStatePathsRepoOverride` (`--repo` replaces the brief's path in the paths).

- [ ] **Step 1 to 4** as before; commit message `feat(briefv2): projectrun state paths for clearing an attempt`.

---

### Task 7: The project report, the printed report and the progress sink

**Files:** Create `gophermind-lib/briefv2/projectrun/report.go`, `sink.go`, `report_test.go`.

**Interfaces:**

```go
type ProjectReport struct {
	RunID       string        `json:"run_id"`
	Title       string        `json:"title"`
	Binary      BinaryInfo    `json:"binary"`           // version, commit, date
	Mode        string        `json:"mode"`             // unattended | attended
	Resumed     bool          `json:"resumed"`
	Repo        RepoInfo      `json:"repo"`             // path, brief_repo, base_branch, head_at_start
	Preflight   []Check       `json:"preflight"`
	Secrets     []Provisioned `json:"secrets"`
	Ambiguity   AmbiguityInfo `json:"ambiguity"`        // brief_setting, effective, clarify_defaulted [{id,question,answer}], conservative_assumptions, milestone_approvals
	Approval    ApprovalInfo  `json:"approval"`         // by, plan_hash
	Plan        PlanInfo      `json:"plan"`             // functions, waves, warnings, requirements_covered {covered,total}
	Stages      []StageInfo   `json:"stages"`
	Executor    *report.Report `json:"executor,omitempty"`
	Status      string        `json:"status"`
	StopReason  string        `json:"stop_reason"`
	ExitCode    int           `json:"exit_code"`
	StartedAt, FinishedAt string
}
func (p *ProjectReport) Text() string               // the printed report
func WriteReport(runDir string, p *ProjectReport) error // <run>/project.json, mode 0600, temp file and rename
func NewSink(w io.Writer) events.Sink               // progress lines
```

**Behavior rules:**
- `Text()` layout, in order: the binary line (`gophermind <version> (commit <commit>, built <date>)`), `project: <id> <title>`, `mode: unattended|attended (resumed: yes|no)`, `repo: <path> (brief repo: <brief_repo>)`, `preflight: ok (<N> checks)`, `secrets: NAME source; ...` (names and sources only), the ambiguity line (`on_ambiguity=<brief> overridden by unattended policy: <k> clarify question(s) answered by their defaults (<ids>); <m> conservative assumption(s); text in <run>/project.json`), the milestone line (spec 8.3 wording), `approval: <by>, plan <first 12 of hash>`, `plan: <f> functions in <w> wave(s)`, then when the executor ran its `Summary()` block unchanged (its last two lines are the proof lines), otherwise `stopped: <status> <stop_reason>` followed by the two proof lines `Requirements covered: <C> of <N>` (from `coverage.json` when it exists, else `0 of <N>` with N from the brief's requirements) and `Acceptance passed: 0 of <A>`. The two proof lines are always the last two lines of the text.
- `Text()` never includes a question, an answer or any model-written text; `project.json` does (`clarify_defaulted`).
- `NewSink(w)` prints `<stage>: started` and `<stage>: done` for planner stage events, `warning: <message>` for warnings and ledger errors, `coverage gap, <message>`, and for every other kind `<node>: <kind> <message>` when there is a message (executor messages are ids and counts by the executor spec). It never prints `Event.Call`.
- The executor's report is embedded as read by `report.Read(runDir)` by the caller (Task 9); `Text()` only formats.

**Tests:** `TestReportCarriesBinaryVersion` (the version, commit and date appear in `Text()` line 1 and in the JSON), `TestPrintedReportHasNoModelText` (a canary question and answer are in the JSON and absent from `Text()`), `TestPrintedReportEndsWithProofLines` (executor present: last two lines are the proof lines in order), `TestProofLinesWhenExecutorDidNotRun` (planner stopped: `Requirements covered: 0 of 7` and `Acceptance passed: 0 of 2`, still last), `TestMilestoneLine` (declared true gives the exact sentence; false gives none), `TestReportJSONRoundTrip` (mode 0600, parses back equal, secrets hold names and sources only), `TestSinkLines` (table of event kinds; a `Call` payload is never printed).

- [ ] **Step 1 to 4** as before; commit message `feat(briefv2): projectrun report, printed summary and progress sink`.

---

### Task 8: Env and wiring

**Files:** Create `gophermind-lib/briefv2/projectrun/env.go` (complete), `wiring.go`, `env_test.go`.

**Interfaces:**

```go
type VersionInfo struct{ Version, Commit, Date string }
type Env struct {
	SettingsPath     func() (string, error)
	LoadSettings     func(path string) (*settings.Config, error)
	BuildProviders   func(cfg *settings.Config, secret func(name string) (string, error)) (map[string]provider.Provider, error)
	ConfigDir        func() (string, error)
	VaultPath        func() (string, error)
	OpenVault        func(path, passphrase string) (*vault.Vault, error)
	DBPath           func() (string, error)
	OpenDB           func() (*sql.DB, error)
	Git              func(ctx context.Context, repo string, args ...string) (string, error)
	LookPath         func(file, path string) (string, error)
	SandboxPreflight func(ctx context.Context) error
	Dial             func(ctx context.Context, addr string) error
	Getenv           func(string) string
	Now              func() time.Time
	Version          func() VersionInfo
	RunExecutor      func(ctx context.Context, o executor.Options) (executor.Report, error)
	Rand             io.Reader
}
func DefaultEnv() Env

type deps struct { cfg *settings.Config; db *sql.DB; vlt *vault.Vault; rt *router.Router; board blackboard.Blackboard; led ledger.Ledger }
func (e Env) open(ctx context.Context, o Options, b *brief.Brief, sink events.Sink) (*deps, error)
```

**Behavior rules:**
- `DefaultEnv`: `settings.Path`, `settings.Load`, `cfg.BuildProviders(&http.Client{}, ...)` exactly as `brief_plan.go` does, `config.Dir`, `vaultPath` logic of `cmd/gophermind/brief.go` (env `GOPHERMIND_VAULT_PATH`, else `<config dir>/vault.age`) re-implemented here (do not import `main`), `vault.Open(path, pass, vault.Options{})`, `db.DefaultPath` and `db.Open`, real `git` via `exec.LookPath("git")`, `exec.LookPath` with an explicit PATH (set `cmd.Env` PATH by splitting the list), `sandbox.Preflight`, `net.Dialer{Timeout: 5s}`, `os.Getenv`, `time.Now`, `version.*`, `executor.Run`, `crypto/rand.Reader`.
- `open` reads the passphrase from `Getenv("GOPHERMIND_VAULT_PASSPHRASE")` only (never prompts; attended mode prompts only for secret values, not for the passphrase), loads settings (the file exists, preflight proved it), builds providers with a harness-scope lookup over the opened vault, opens the database, builds `router.New(cfg, providers, ledger.NewSQLite(db), sink)` with `WithAllowPublic(false)`, and returns the pieces. It opens the vault only when the brief declares secrets or a provider needs a key.
- The vault is never opened by `StatePaths` or `--print-state-paths`.

**Tests:** `TestDefaultEnvFieldsAreSet` (no nil field), `TestOpenBuildsRouterWithTempState` (an `Env` over a temp config dir and fake providers: database file created under the temp dir only, router usable), `TestVaultPathHonorsOverride`, `TestOpenNeverPromptsForPassphrase` (empty env var: error naming the variable, no read from stdin), `TestDefaultDialTimesOut` (a listener-less port fails within a second on loopback).

- [ ] **Step 1 to 4** as before; commit message `feat(briefv2): projectrun Env seams and production wiring`.

---

### Task 9: `projectrun.Run`

**Files:** Create `gophermind-lib/briefv2/projectrun/run.go`, `run_test.go`.

**Interface:** `func Run(ctx context.Context, o Options, env Env) Result` (spec 4 and 5).

**Behavior rules, in order:**
1. `os.ReadFile(o.BriefPath)`, `brief.Parse`. `*brief.InvalidError`: print `invalid brief: ...` to `o.Err`, `Result{ExitInvalid, "invalid_brief", "invalid_brief"}`. Other read or parse errors: `ExitFailed`.
2. `o.PrintStatePaths`: `StatePaths` then `PrintStatePaths` to `o.Out`, exit 0. Nothing else runs.
3. `Preflight`; `Failed` non-empty: `PrintPreflight` to `o.Err`, return `Result{ExitPreflight, "preflight_failed", "preflight"}`; no project report file, no database, no run folder. `o.PreflightOnly`: print `preflight: ok` to `o.Out`, exit 0.
4. Start a `ProjectReport` (binary from `env.Version`, mode, repo info with `head_at_start` from `Env.Git rev-parse HEAD`, preflight checks, `started_at`). From here on every return path writes `project.json` (once the run folder exists) and prints `Text()` to `o.Out`.
5. `env.open`; then `Provision(store, b, o.Generate, prompt, o.Resume, env.Rand)` (prompt is `vault.ReadSecret` over `o.In` only when `o.Attended`). Record sources in the report.
6. Planner: `planner.New(planner.Deps{Caller: rt, Gate: <unattendedGate{} or human.NewTerminal(o.In, o.Err)>, Sink: NewSink(o.Err), Board, Settings: cfg, OpenSecrets: vault, PromptSecret: (attended only), LedgerErrors: rt.LedgerErrors, ResetRun: db.ClearRun})` and `Run(ctx, planner.Options{BriefPath or RunID (resume), Repo: o.Repo, Unattended: !o.Attended})`. `context.Canceled` or `DeadlineExceeded`: `interrupted`, exit 5, `StopReason: interrupted`. Any other error: `failed`, exit 1, `plan:<stage>` taken from the error text prefix `planner: <stage>:` (else `plan:unknown`). `Waiting`: `failed`, `plan:waiting`. After success read `planner.ReadAnswers`, `ReadApproval`, `ReadCoverage`, `ReadStatus` into the report (`clarify_defaulted` are the `Assumed` answers with `Stage == "clarify"`; `conservative_assumptions` is the number of bullet lines under `## Assumptions` in the markdown returned by `planner.RenderPlan(runDir)` (zero when it says `None.`) minus the clarify answers, which it also lists).
7. Executor: `env.RunExecutor(ctx, executor.Options{RunDir: rec.RunDir, Repo: <resolved repo>, Caller: rt, Board, Ledger, Gate: unattendedGate{} (or the terminal gate when attended), Sink, Settings: cfg, Secrets: vault (nil when none declared), LedgerErrors: rt.LedgerErrors})`. A non-nil error is a harness fault: `failed`, exit 1, `StopReason: harness_fault`, message printed as `error: <text>`. Otherwise read the `Report`, embed it, map `Status` to the exit code with one function `exitForStatus(status, stopReason string) int`: `verified` 0, `failed` 1, `escalated` 4, `interrupted` 5.
8. `Resumed` in the report is `o.Resume || executor.Resumed`. `finished_at`, `status`, `stop_reason`, `exit_code` set; `WriteReport`; print; return.

**Tests** (an `Env` of fakes; the planner runs over the planner `greeter` fixtures, the executor is replaced through `Env.RunExecutor`):
- `TestRunInvalidBriefExit2`; `TestRunPrintStatePathsOnly` (no preflight, no db).
- `TestRunPreflightFailureStopsEverything`: `OpenDB`, `RunExecutor` and the provider are never called; exit 6; no `project.json`; the `Err` block lists the items.
- `TestPreflightFailureExit6IsNotARun`: the failed preflight leaves no run folder, no `runs/<id>.json`, no database file.
- `TestRunPlanStageFailureSkipsExecutor`: the `greeter-gap` fixture; exit 1, `StopReason` starts `plan:`, the executor fake not called, `project.json` exists, the printed text ends with the two proof lines (`0 of N` acceptance).
- `TestRunCallsExecutorWithPlanArtifacts`: the fake executor records its `Options`: `RunDir` is the planner's run folder, `Repo` is the override, `Gate` is the unattended gate (`Escalate` returns stop), `Secrets` is non-nil only when the brief declares one, `Board`, `Ledger` and `Caller` are the same instances the planner used.
- `TestRunMapsExecutorStatusesToExitCodes`: table verified 0, failed 1, escalated 4, interrupted 5; a returned error gives 1 and `harness_fault`.
- `TestRunInterruptedContextExit5`: the context is cancelled during the planner (fake provider blocks): exit 5, `interrupted`, the printed text says `resume with --resume`.
- `TestEveryStopConditionHasReasonAndCode`: drives the rows of spec 8.4 that can be simulated (preflight, invalid brief, clarify without defaults, coverage gap, executor statuses, interrupted) and asserts each has a non-empty `StopReason`, the listed `Status` and exit code.
- `TestRunAttendedUsesTerminalGate`: with `Attended` the planner's `Deps.Gate` is the terminal gate (an `Env` hook records the type) and unattended runs never read `o.In` (a closed pipe as `In` stays unread).
- `TestRunResumeSkipsFinishedPlannerStages`: a first `Run` stopped with a fake executor returning `interrupted`; a second with `Resume: true` makes zero planner calls (provider count unchanged), calls the executor, and the report has `resumed: true`.

- [ ] **Step 1: Write the failing tests. Step 2: Run** `go test ./gophermind-lib/briefv2/projectrun/ -run 'TestRun|TestEveryStop|TestPreflightFailure'`. **Step 3: Implement** `run.go`. **Step 4: Verify and commit** (`feat(briefv2): projectrun.Run plans and builds a brief in one process`).

---

### Task 10: `gophermind project` (the CLI form)

**Files:** Create `cmd/gophermind/project.go`, `cmd/gophermind/project_test.go`. Modify `cmd/gophermind/main.go` (one routing block after the `brief` block, and the usage text) and nothing else there.

**Interfaces:**

```go
// projectEnv is nil in production; a test replaces it with an Env of fakes.
var projectEnv func() projectrun.Env

// runProject implements `gophermind project <brief-path> [flags]`; returns the exit code.
func runProject(args []string, in *os.File, out, errw io.Writer) int
```

**Behavior rules:** `runProject` calls `projectrun.ParseArgs(args, true)`; a parse error prints `error: <message>` plus the project usage and returns 1. Otherwise set `o.In = in` only when `o.Attended`, `o.Out = out`, `o.Err = errw`, build the context with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`, and return `projectrun.Run(ctx, o, env).ExitCode`, with `env` from `projectEnv` or `projectrun.DefaultEnv()`. `main.go`: `if cmd == "project" { os.Exit(runProject(args[1:], os.Stdin, os.Stdout, os.Stderr)) }` directly after the `brief` block (before `cfg.Validate`, so it needs no chat endpoint), and these usage lines after the `brief` ones:

```text
  gophermind project <brief.md> [--repo <path>] [--generate NAME=hex32|placeholder]... [--require-private]
                      [--expect-head <rev>] [--expect-binary-commit <sha>] [--resume] [--attended]
                      [--preflight-only] [--print-state-paths]
                                plan and build a v2 brief in one run (same as /project in the TUI)
```

**Tests:** `TestProjectCLIUsage` (no args and a bad flag print the usage and exit 1), `TestProjectCLIExitCodes` (table over an `Env` of fakes: verified 0, failed 1, invalid brief 2, escalated 4, interrupted 5, preflight 6; stdout ends with the proof lines for 0, 1, 4, 5), `TestProjectCLIPrintStatePaths` (tab separated lines, exit 0, no provider call), `TestProjectCLIPreflightOnly` (exit 6 lists missing items on stderr, nothing on stdout; exit 0 prints `preflight: ok`), `TestProjectCLINeverReadsStdinUnattended` (a pipe closed by the test is not read).

- [ ] **Step 1: Write the failing tests. Step 2: Run** `go test ./cmd/gophermind/ -run TestProjectCLI`. **Step 3: Implement. Step 4: Verify and commit:**

```bash
gofmt -l cmd gophermind-lib/briefv2 && go vet ./cmd/... ./gophermind-lib/briefv2/... && go test -race ./cmd/gophermind/... && go build ./cmd/... ./gophermind-lib/...
git add cmd/gophermind/project.go cmd/gophermind/project_test.go cmd/gophermind/main.go
git commit -m "feat(briefv2): gophermind project runs the v2 planner and executor in one command" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 11: Rename the v1 TUI commands to `/plan-v1` and `/plan-v1-execute`

A mechanical change with one new test and one new message. It is its own task so the diff is reviewable as a rename.

**Files (modify):** `gophermind-lib/tui/commands_registry.go`, `update.go`, `project.go`, `approve.go`, `execute.go`, `questions.go`, `question_round.go`, `commands.go`, `model.go`, `run.go`, and these test files in the same folder: `project_flow_test.go`, `project_fixwave_test.go`, `execute_test.go`, `execute_guard_test.go`, `questions_test.go`, `question_round_test.go`, `attention_test.go`, `phase_test.go`, `fixwave_test.go`, `e2e_test.go`, `update_test.go`, `project_test.go` (only where a file really contains the literal).

**Behavior rules:**
- In the files above replace the literal `/project-execute` with `/plan-v1-execute` first, then the literal `/project` (when not followed by `-` or a word character) with `/plan-v1`, in string literals and comments alike. Use `perl -pi -e 's{/project-execute\b}{/plan-v1-execute}g; s{/project(?![-\w])}{/plan-v1}g'` on exactly those files, never on `project_v2.go` (Task 12), `docs/`, `phaseflow/assets`, or `gophermind-lib/plantree`.
- Routing in `update.go` tests `/plan-v1` and `/plan-v1-execute` for the v1 handlers. The registry entries become `/plan-v1 <name> <brief>` ("v1 planner: plan a brief into phases, tasks and steps (deprecated, use /project)") and `/plan-v1-execute`. Function and type names are unchanged (surgical).
- When the v1 flow starts (`startProject`) and when `/plan-v1-execute` starts, the first transcript line is `deprecated: /plan-v1 and /plan-v1-execute are the v1 planner and will be removed after the AI Venture Studio release; /project runs the v2 planner and executor`.
- Acceptance grep after the change: `grep -rn '"/project\|/project ' gophermind-lib/tui cmd --include='*.go'` returns nothing except the new registry entry for v2 `/project` (added in Task 12).

**Tests:** `TestPlanV1CommandsRenamed` (the registry lists `/plan-v1` and `/plan-v1-execute`; `helpLine()` contains both; routing a line starting `/plan-v1 name brief.md` reaches `handleProjectCommand`; `/plan-v1-execute` reaches the execute handler), `TestPlanV1PrintsDeprecation` (the first appended line after starting either command is the deprecation sentence). The whole existing `tui` suite passes with only the literals changed.

- [ ] **Step 1: Write the two new tests (they fail). Step 2: Run the perl replacement; run `git diff --stat` and read the diff of the non-test files in full; run the acceptance grep. Step 3: Add the deprecation line. Step 4: Verify and commit:**

```bash
gofmt -l gophermind-lib/tui && go vet ./gophermind-lib/tui/... && go test -race ./gophermind-lib/tui/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/tui
git commit -m "refactor(tui): rename the v1 planner commands to /plan-v1 and /plan-v1-execute" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

(Check `git diff --cached --stat` first: if the executor work left changes under `gophermind-lib/tui`, stage only the files listed above.)

---

### Task 12: `/project` in the TUI (the slash form)

**Files:** Create `gophermind-lib/tui/project_v2.go`, `project_v2_test.go`. Modify `commands_registry.go` (new `/project` entry), `update.go` (route `/project` to `handleProjectV2Command`, after the `/plan-v1` routes), `model.go` (two fields: `projV2 bool`, reuse `m.cancel`).

**Interfaces:**

```go
// projectRunFn and projectEnvFn are package variables so tests replace them.
var projectRunFn = projectrun.Run
var projectEnvFn = projectrun.DefaultEnv

type projectV2LineMsg string          // one printed line
type projectV2DoneMsg struct{ res projectrun.Result }

func (m model) handleProjectV2Command(text string) (model, tea.Cmd)
```

**Behavior rules:** split the text into fields; `projectrun.ParseArgs(fields[1:], false)`; an error (including the v1 two-token form and `--attended`) is appended to the transcript and nothing starts. A second `/project` while `m.projV2` is true appends `a project run is already working; wait for it or press Esc` and starts nothing. Otherwise set `m.projV2 = true`, `m.st = stateWorking`, create `ctx, cancel := context.WithCancel(context.Background())`, store `m.cancel = cancel`, and run `projectRunFn(ctx, o, projectEnvFn())` on a goroutine with `o.Out` and `o.Err` set to a writer that posts each line as `projectV2LineMsg` on `m.sub` and `o.In = nil`. When it returns, post `projectV2DoneMsg`; `Update` appends `project: exit <code> (<status>)`, clears `projV2`, `m.st`, `m.cancel`. Esc or Ctrl-C while running cancels the context (the existing interrupt path); the done line then says `interrupted; continue with /project <brief> --resume`. The TUI never prompts for anything. Follow `execute.go` for the goroutine and message pattern and `project_flow_test.go` for driving `Update` in tests (read both first).

The registry entry: `{Name: "/project", Arg: "<brief> [flags]", Desc: "plan and build a v2 brief in one run, no second command"}`.

**Tests:** `TestSlashProjectRunsProjectrun` (a fake `projectRunFn` prints two lines and returns exit 0: both lines reach the transcript, the `Options` carry the brief path and `--repo`, the done line shows exit 0), `TestSlashProjectRefusesAttended`, `TestSlashProjectRejectsV1Form` (`/project name brief.md` errors with the v1 message and calls nothing), `TestSlashProjectEscCancels` (the fake blocks on `ctx.Done()`; Esc cancels; the transcript has the resume hint), `TestSlashProjectSecondRunRefused`, `TestSlashProjectRegistry` (registry entry and help line).

- [ ] **Step 1 to 4** as before (commit message `feat(tui): /project plans and builds a v2 brief through projectrun`).

---

### Task 13: The dev binary

**Files:** Create `scripts/build-dev-binary.sh`, `cmd/gophermind/devbinary_test.go`. Modify `Makefile` (one target).

**Behavior rules:** `scripts/build-dev-binary.sh [--dry-run]` with env overrides `GM_REPO` (default: the script's repo root) and `GM_DEV_BIN` (default `$HOME/.gophermind/bin/gophermind-dev`). Steps: `set -euo pipefail`; refuse when `git -C "$GM_REPO" status --porcelain` is non-empty (`refusing to build: the tree is dirty`); refuse when the commit is not on any remote branch (`git branch -r --contains HEAD` empty: `refusing to build: HEAD is not pushed`); `sha=$(git rev-parse --short HEAD)`; `date=$(date -u +%Y-%m-%dT%H:%M:%SZ)`; ldflags `-X gophermind/gophermind-lib/version.Version=dev+$sha -X gophermind/gophermind-lib/version.Commit=$sha -X gophermind/gophermind-lib/version.Date=$date`; `--dry-run` prints the ldflags and the output path and exits 0; otherwise `mkdir -p "$(dirname "$GM_DEV_BIN")"`, `go build -ldflags "$ldflags" -o "$GM_DEV_BIN" ./cmd/gophermind`, then `"$GM_DEV_BIN" version` and a check that the printed commit equals `$sha` (else exit 1). It never touches `/opt/homebrew/bin` and deletes nothing. Makefile target `dev-binary: ## Build the stamped dev binary for graded runs` calling the script.

**Tests:** `TestDevBinaryScriptRefusesDirtyTree` (a temp git repo with an uncommitted file: non-zero exit and the message), `TestDevBinaryScriptRefusesUnpushedCommit` (a clean repo with no remote containing HEAD), `TestDevBinaryScriptPrintsLdflags` (`--dry-run` in a clean repo with a bare remote the test pushed to: output has `Version=dev+<sha>`, the path under `GM_DEV_BIN`, exit 0). The test skips when `bash` or `git` is missing.

- [ ] **Step 1: Write the failing tests. Step 2: Run them. Step 3: Write the script and target; `chmod +x`. Step 4: Verify and commit:**

```bash
gofmt -l cmd && go vet ./cmd/... && go test -race ./cmd/gophermind/ -run DevBinary && bash -n scripts/build-dev-binary.sh
git add scripts/build-dev-binary.sh cmd/gophermind/devbinary_test.go Makefile
git commit -m "build: stamped dev binary script for graded /project runs" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 14: End to end on the greeter, clear and rerun

Needs executor plan Task 16 (the greeter rig, `impl/` replies) committed. No new production code; a failing assertion caused by an earlier task is fixed in that task's file in its own `fix(briefv2): ...` commit with a test added there first.

**Files:** Create `gophermind-lib/briefv2/projectrun/testhelp_test.go`, `e2e_test.go`.

**Helpers (`testhelp_test.go`):** `fixtureDir(t)` resolves `executor/testdata/greeter` with `runtime.Caller`. `newProjectRig(t, opts...)` copies the fixture into `t.TempDir()`, edits the brief text to `on_ambiguity: halt`, `milestone_approvals: true`, a second declared secret `GREETER_SALT`, and writes a `planner/clarify.txt` with two questions carrying defaults; makes the target repo as the executor rig does (`git init -b main`, `go.mod`, seed commit, tag `baseline`); sets `GOPHERMIND_CONFIG_DIR`, `GOPHERMIND_VAULT_PATH`, `GOPHERMIND_VAULT_PASSPHRASE`; seeds the vault's harness scope with `GREETER_TOKEN = CANARY-SECRET-VALUE`; returns an `Env` of fakes (real `git`, real executor through `RunExecutor: executor.Run`, `SandboxPreflight` real on darwin and `executor.sandbox: off` elsewhere with a log line, `Dial` real over loopback, `Version` fixed to `dev+abc1234`/`abc1234`). `comboProvider(t, dir)` is one `provider.Fake` whose function sends planner requests (`planner.StageOf(req) != ""`) to `planner.FixtureProvider(<fixture>/planner, <override>)` and executor requests (system message prefix `packer.SystemPrefix`) to the next file `<fixture>/impl/good.<node>.txt` (or the scripted bad ones for the failure tests), counting requests per stage.

**Tests (`e2e_test.go`):**
- `TestProjectE2EGreeter`: one `Run` call, `Options{BriefPath, Repo, Generate: {"GREETER_SALT": "hex32"}, RequirePrivate: false, ExpectHead: "baseline", ExpectBinaryCommit: "abc1234"}`, `In` nil. Asserts in order: (1) `ExitCode == 0`, `Status == "verified"`, exactly one call of the real executor (a counting wrapper on `RunExecutor`) and it happened after the last planner request (request order from the fake); (2) `approval.json` has `approved_by: unattended` and `plan_hash` equal to `planner.RenderPlan(runDir)`'s hash (`TestUnattendedApproveBindsHash` is this assertion's name in its own subtest); (3) `answers.json` has two `Assumed` answers equal to the fixture defaults, `project.json` `clarify_defaulted` has both, the printed text lists ids `q1, q2` and contains neither question text; (4) `project.json`: `mode: unattended`, `resumed: false`, secrets `GREETER_TOKEN` source `vault` and `GREETER_SALT` source `generated:hex32`, `binary.commit: abc1234`, `milestone_approvals` line present, `executor.status: verified`; (5) stdout's last two lines are `Requirements covered: 7 of 7` and `Acceptance passed: 2 of 2`; (6) `main` fast-forwarded and branch `gm/<id>` exists; (7) no `QUESTIONS.md`, no `APPROVAL.md` in the run folder.
- `TestNoSecretValueAnywhere`: `CANARY-SECRET-VALUE` and the generated salt are absent from stdout, stderr, `project.json`, `report.json`, every file under the run folder, the sqlite database and its WAL, and `git log -p --all` of the target repo.
- `TestProjectPlanFailureExit1`: the `greeter-gap` planner variant: exit 1, `stop_reason` `plan:coverage`, executor never called, printed text ends with the proof lines, `project.json` exists.
- `TestProjectEscalationExit4`: `fn-hello` always wrong: exit 4, `escalated`, printed text names the leaf.
- `TestProjectInterruptedExit5`: the context cancelled while `fn-hello` is being implemented: exit 5, the report says resume with `--resume`.
- `TestProjectRefusesStaleStateWithoutResume`: run once (verified), run again without clearing: exit 6, the stale-state check named, no provider call; with only the branch deleted the run folder still blocks; with `--resume` and a finished run the preflight passes.
- `TestResumeFlagContinuesAndIsRecorded`: first run ended `interrupted` by the context; second run with `Resume: true` makes no planner request for finished stages, builds the rest, exit 0, `project.json` `resumed: true`, `report.json` `resumed: true`.
- `TestClearedStateRerunsClean`: after a verified run, perform the clear exactly as spec 9 and as `--print-state-paths` lists it (switch to `main`, delete branch `gm/<id>`, reset hard to `baseline`, clean `-fdx`, then the guarded delete of `runs/<id>.json` inside the test's config dir; real `git` with the test's own command path, bypassing any wrapper via `GITLAND_TEST_GIT`/`/usr/bin/git` when present); `Preflight` passes (`--expect-head baseline`); a second full `Run` is verified again with the same commit tree as the first (`git rev-parse main^{tree}` equal) and the ledger holds only the second run's rows (`calls` for the run id equal the second run's count). A sub-test skips the branch delete and asserts exit 6 naming `gm/<id>`.
- `TestPreflightMissingItemsEndToEnd`: an `Env` where the vault lacks `GREETER_TOKEN` and the passphrase env var is empty: exit 6, both items in one listing, the fake provider's call count is 0.

- [ ] **Step 1: Write the helpers and the failing tests. Step 2: Run** `go test ./gophermind-lib/briefv2/projectrun/ -run 'E2E|Canary|Stale|Resume|Cleared|EndToEnd'` and see each assertion fail for the right reason. Do not loosen an assertion; if an assertion contradicts the spec, fix the assertion and say why in the commit message. **Step 3: Fix defects** in the responsible task's files (separate `fix(briefv2): ...` commits). **Step 4: Stability:** `go test -race -count=3 ./gophermind-lib/briefv2/projectrun/ -run E2E` and `go test -race ./gophermind-lib/briefv2/... ./gophermind-lib/tui/... ./cmd/...` pass; the E2E finishes in under 2 minutes (find the slow step, do not raise a timeout). **Step 5: Verify and commit:**

```bash
gofmt -l gophermind-lib/briefv2 cmd && go vet ./gophermind-lib/briefv2/... ./cmd/... && go test -race ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun/testhelp_test.go gophermind-lib/briefv2/projectrun/e2e_test.go
git commit -m "test(briefv2): /project end to end on the greeter, unattended policy, clear and rerun" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 15: Documentation, the runbook, and the rehearsal

**Files:** Modify `docs/briefv2/README.md`, `CHANGELOG.md`. Create `docs/briefv2/project-runbook.md`. The README is prose without em dashes; keep that.

- [ ] **Step 1: README.** Add a "One command: /project" section: what it does (section 5 of the spec), the two forms, flags, exit codes (0, 1, 2, 4, 5, 6; 3 unused), the unattended policy in five sentences (defaults, generated secrets, approval by hash, milestone line, what stops a run), and that the v1 planner is `/plan-v1` until it is removed after the AI Venture Studio release. Change the intro sentence so `brief plan` and `brief run` are described as the stepwise commands that `/project` chains.
- [ ] **Step 2: Runbook** `docs/briefv2/project-runbook.md` (new, no em dashes): (a) the human prerequisites of spec 16 as a checklist with the exact commands (`gophermind brief vault set DATABASE_URL`, `ssh -N -L 127.0.0.1:55432:127.0.0.1:5432 mini`, the `gophermind.yaml` lines for `privacy.mode: private_only`, tiers, `toolchain.PATH`); (b) the graded command line for the real brief, `gophermind-dev project gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md --repo "$HOME/OtherProjects/AIVentureStudio" --generate JWT_SIGNING_KEY=hex32 --generate STUDIO_LLM_API_KEY=placeholder --require-private --expect-head goal-baseline --expect-binary-commit "$(git rev-parse --short HEAD)"` with output sent to a log file under the scratchpad; (c) the state table and the ordered clear procedure of spec 9 with the guarded delete; (d) how to read `project.json`, `report.json` and `gophermind brief calls <id>` before clearing, because the next fresh `/project` deletes that run's database rows; (e) what exit 6 means and that it does not count as an attempt.
- [ ] **Step 3: CHANGELOG** entry under Unreleased: `/project now runs the v2 planner and executor in one command; the v1 planner is /plan-v1; gophermind project <brief>; preflight exit 6; scripts/build-dev-binary.sh`.
- [ ] **Step 4: Verify and commit:**

```bash
grep -nP '\x{2014}' docs/briefv2/README.md docs/briefv2/project-runbook.md || true     # must print nothing
git add docs/briefv2/README.md docs/briefv2/project-runbook.md CHANGELOG.md
git commit -m "docs: /project single entry point README section, runbook and changelog" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

- [ ] **Step 5: Rehearsal, reported not asserted (nothing here runs a model).** Purpose: learn, before the graded loop, exactly what a human still has to provide. Build the dev binary (Task 13), then run the preflight against the real brief and print the state paths:

```bash
SP=/private/tmp/claude-501/-Users-jbrahy-OtherProjects-PMSLLC-gophermind-com/b7a69b80-eaca-4766-ae46-bc9098948066/scratchpad
make dev-binary
B=gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md
"$HOME/.gophermind/bin/gophermind-dev" project "$B" --repo "$HOME/OtherProjects/AIVentureStudio" \
  --generate JWT_SIGNING_KEY=hex32 --generate STUDIO_LLM_API_KEY=placeholder --require-private \
  --expect-head goal-baseline --preflight-only > "$SP/preflight.log" 2>&1; echo "exit $?"
"$HOME/.gophermind/bin/gophermind-dev" project "$B" --repo "$HOME/OtherProjects/AIVentureStudio" --print-state-paths
```

Report to John: the exit code, the list of missing items from `preflight.log` (expect: the vault passphrase, `DATABASE_URL`, `TEST_DATABASE_URL`, the `gophermind.yaml`, `toolchain.PATH`), and the state paths, with no value of any secret. Make no change to `~/.gophermind`, the target repo or the mini. The log lives in the scratchpad; remove it with the guarded delete when done:

```bash
target="$SP/preflight.log"
case "$target" in ""|"/"|"$HOME") echo "refusing to delete: bad target" >&2 ;;
  /private/tmp/claude-501/*/scratchpad/preflight.log) rm -f -- "${target:?}" ;;
  *) echo "refusing to delete: outside the scratchpad" >&2 ;; esac
```
