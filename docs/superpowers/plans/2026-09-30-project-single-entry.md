# `/project` Single Entry Point Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is one implementer dispatch and one reviewer dispatch; do not combine tasks.

**Goal:** Make `/project` the single entry point of the v2 engine: one library call, `projectrun.Run`, behind the slash command `/project <brief>` and the CLI `gophermind project <brief-path>`, that runs load, clarify, confirm, contract, decompose, enrich, coverage, approve and test-writer with the v2 planner, then `executor.Run`, then prints one final report, in one process, with no second command, no human step and no restart. A preflight fails fast, before any model call, with a numbered list of what a human must provide and the exact fix command for each; an unattended policy takes every Clarify recommendation, confirms the understanding by rule (`ConfirmUnderstanding`), provisions secrets from the vault or generates them, and approves the plan by hash; the v1 planner moves to `/plan-v1`; the state of an attempt (a run folder of files, no database) is printable, archivable and clearable; the binary that runs a graded attempt is built from a clean pushed commit and stamps its version into the report, and the report carries a node-class table.

**Architecture:** One new shared package `gophermind-lib/briefv2/envcheck` (the environment checks that today live in `package main`), one new package `gophermind-lib/briefv2/projectrun` (env, args, preflight, secrets, unattended gate, state paths, report, `Run`), two thin callers (`cmd/gophermind/project.go`, `gophermind-lib/tui/project_v2.go`), a small planner extension (`Options.Repo`, `Options.Unattended` read through `takesRecommendations()`, `ResolveRepo`, `ReadAnswers`, `ReadApproval`, `ReadUnderstanding`, `ReadQuestionCounts`), a mechanical rename of the v1 TUI commands, a dev-binary guard script, and docs. The executor is built (`executor.Run(ctx, Options) (Report, error)`) and is called as is. Nothing depends on `projectrun` except the two callers.

**Tech Stack:** Go (the module's version), the standard library (`net`, `net/url`, `crypto/rand`, `flag`), `gopkg.in/yaml.v3` (already a dependency), the `git` CLI through `gitenv`, bash for one script. No new dependency, and no database: run state is files (`blackboard.FS`, `ledger.FS`), and `briefv2/nosql_test.go` fails the build if any `briefv2` package, `projectrun` included, imports `database/sql`.

**Spec:** `docs/superpowers/specs/2026-09-30-project-single-entry-design.md` (binding; read all of it first). Goal and bar: `docs/GOAL.md` in the main checkout (`/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com`), tasks 4 and 5. Executor spec: `docs/superpowers/specs/2026-09-30-v2-executor-design.md`.

**Prerequisites:** sub-projects A (run state as files) and B (node widening and the question model) are built on this branch and this plan was amended against them on 2026-10-08 (the record of every change is `.superpowers/sdd/2026-09-30-project-single-entry/amend-log.md`). The planner, the executor and the file backends are committed: `executor.Run`, `executor.Options`, `report.Report`, `report.ExitCode`, `blackboard.NewFS`, `ledger.NewFS`, the greeter fixtures under `gophermind-lib/briefv2/executor/testdata/greeter` (including `clarify.more.txt` and the Enrich replies) and `planner/testdata/greeter`, `planner.FixtureProvider`, `provider.Fake`, `human.Gate.Confirm`. Read `cmd/gophermind/brief_run.go`, `brief_preflight.go`, `brief_plan.go` and `brief_state.go` before Task 1: they are the working reference for everything `projectrun` repeats. If any symbol this plan names is missing when you reach its task, stop and report it; do not re-create A or B code.

## Global Constraints

- Pure Go, no cgo, no new dependency. New code lives under `gophermind-lib/briefv2/envcheck/`, `gophermind-lib/briefv2/projectrun/`, `cmd/gophermind/project.go`, `gophermind-lib/tui/project_v2.go`, `scripts/build-dev-binary.sh`. Do not modify an existing package except the named exceptions: Task 1 (`cmd/gophermind/brief_preflight.go`, `brief_run.go`, `diskfree_*.go` moved), Task 3 (`planner`: `planner.go`, `load.go`, `clarify.go`, `exported.go`, new test files; no fixture folder, the tests build their replies with the existing `fixtureDir` and `variant` helpers), Task 9 (`cmd/gophermind/main.go`: one routing block and the usage text), Tasks 10 and 11 (`gophermind-lib/tui`: the listed files), Task 14 (`docs/briefv2/README.md`, `CHANGELOG.md`).
- Tests never use the real network (loopback listeners and `httptest` are allowed) and never read or write the real `~/.gophermind`: use `t.TempDir()`, `t.Setenv("GOPHERMIND_CONFIG_DIR", ...)`, `t.Setenv("GOPHERMIND_VAULT_PATH", ...)`, and the vault's low work factor option (`vault.Options{WorkFactor: n}`; `cmd/gophermind/brief.go` declares the package variable `vaultOptions` that its tests set).
- **Git.** Production code and tests run git only through `gitenv.Command` or `gitenv.CommandContext` (every `GIT_*` variable stripped) and the git on `PATH`. The user's wrapper `~/.local/bin/git-wrapper/git` blocks `git worktree add`, `git checkout`, `git stash` and `--gw-force`. Never write any of those, never `git switch --discard-changes`, never set or read `GITLAND_TEST_GIT`, never hard-code `/usr/bin/git`. Every `_test.go` file in a new package that runs git has a `TestMain` that unsets every `GIT_*` variable (copy the loop from `cmd/gophermind` tests or `executor/main_test.go`); the pre-push hook exports `GIT_DIR`.
- No reply text, prompt text, command output or secret value in an error, event, log line, ledger row, blackboard row or printed report. Printed reports carry ids, counts, names, statuses and hashes only; model-written text (clarify questions and answers) lives in `answers.json` and `_state/project.json` (mode 0600). Secret values appear only inside the vault and in a command's environment. A generated secret value is never printed and never logged.
- Any delete, in code or in a shell block, assigns its target to a variable, tests it (non-empty, expected prefix and parent, never `/` or the home directory) and only then deletes; the shell form is `rm -rf -- "${target:?}"` (or `rm -f -- "${target:?}"` for one file). `projectrun` itself deletes nothing.
- Preflight writes nothing, builds no state backend (no run folder exists yet), makes no model call, and never creates a settings file.
- **Run state is files.** A run is the folder `.gophermind/<id>/` in the target repo (tree node JSON, a `*.runtime.json` beside each node file, `_state/events.jsonl`, `_state/calls.jsonl`, `attempts/`, `decisions/`, `UNDERSTANDING.md`, `_state/clarify/questions.json`, and the rest listed in spec section 9). There is no database: no `projectrun` file imports `database/sql` or any `briefv2/db` package (none exists), and `briefv2/nosql_test.go` enforces it. Clearing a run means removing the run folder (and `<id>-scratch`) with the guarded `rm -rf` of spec section 9. `projectrun` writes only `_state/project.json` into the run folder (not a root-level `project.json`: `tree.Store.Load` rejects an unlisted root-level `.json` file as a node).
- **Test command.** Every test command below is run through `gt`, which gives the tests an empty `HOME` and keeps the real Go caches. Define it in the shell before the first command: `gt() { local gc gm h rc; gc="$(go env GOCACHE)"; gm="$(go env GOMODCACHE)"; h="$(mktemp -d)"; HOME="$h" GOCACHE="$gc" GOMODCACHE="$gm" go test "$@"; rc=$?; rm -rf -- "${h:?}"; return $rc; }`.
- Tests are TDD: write the failing test, see it fail for the right reason, then implement. Tests pass with `-race`; code is gofmt-clean and `go vet` clean. No em dashes and no emojis in code, comments, docs or commit messages.
- **Test time.** The briefv2 tests need `-timeout 30m`; the `executor` package alone takes about 11 minutes. Never run `./gophermind-lib/briefv2/executor/...` inside a task except in Task 12 Step 5. Per task run only the packages the task touches, always with `-race -timeout 30m`. Each end-to-end test in Task 12 must finish in under 3 minutes with `-count=1`; find the slow step rather than raising a timeout.
- In a git worktree the ignored `desktop/frontend/dist` folder is missing: build with `go build ./cmd/... ./gophermind-lib/...` in `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`.
- The worktree may hold unrelated uncommitted work. Stage explicit files only (a path per file, never a directory, never `git add -A` or `.`); never `git stash`; never `git checkout`; never a force flag. Run `git diff --cached --stat` before each commit and unstage anything not listed in the task.
- Commit messages end with two trailers, each on its own line after a blank line: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ`. After each task's commit run `git push origin feat/briefv2-planner-core` (if the pre-push gate fails only on files the task did not touch, wait and retry; the iOS step can flake once, retry unchanged; never bypass). Never push to another branch.
- Working directory for every command: `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`, branch `feat/briefv2-planner-core`.

## Decisions specific to this plan

| # | Choice | Why |
|---|---|---|
| P1 | The package is `briefv2/projectrun`, not `project` | `gophermind-lib/project` already exists; two packages named `project` invite wrong imports |
| P2 | `Env` is defined once, in Task 2, with every seam (settings, providers, probe, vault, state backends, git, dial, clock, version, executable, executor call); `DefaultEnv()` fills production values; tests replace fields | No global hooks; every later task builds on it; `cmd` and `tui` tests pass an `Env` |
| P3 | `Unattended` is a planner `Options` field in addition to the gate, read in one place: `run.takesRecommendations()` (`clarify.go`) | Clarify, Confirm and the mid-stage question (`callAsking`) all ask that one method whether a human is available; the gate alone cannot change the planner's prompt, the answer records or the Confirm rule. `Options.Yes` (approval and confirmation `flag`) is untouched and unused here |
| P4 | Clarify under `Unattended` needs a recommended answer on every question. No new helper: with `takesRecommendations()` true the existing parser option `qparseOpts.RequireRecommended` already rejects a question without one, and the router's malformed retry repairs it | Fewer lines changed; the old reply field `default_if_unanswered` is read as the recommendation, so older fixtures still parse |
| P5 | Generated secrets are written into the HARNESS scope by a thin pre-step (`--generate NAME=KIND`); the planner's `storeSecrets` does the rest | `storeSecrets` already copies harness values into the run scope; a second provisioning path would drift |
| P6 | Database checks parse the vault value, require loopback, dial `127.0.0.1:<port>`, and print the exact tunnel command on failure; no driver, no emptiness check; no tunnel is started by code | No new dependency; the human owns the tunnel |
| P7 | Environment checks that `brief run --check-env` and `projectrun` both need move to `briefv2/envcheck` with no behavior change | `cmd/gophermind` is `package main`; `projectrun` and the TUI cannot import it |
| P8 | Read-only git in preflight goes through one `Env.Git` function that accepts a fixed set of argument vectors and runs through `gitenv.CommandContext` | Same discipline as `gitland`: no shell, no write verb, no inherited `GIT_*` |
| P9 | Preflight and progress print to stderr, the final report to stdout | The orchestrator redirects both into one log; stdout ends with the two proof lines |
| P10 | The dev binary path is `~/.gophermind/bin/gophermind-dev`, built by a guard script around the Makefile's own ldflags | The brew cask at `/opt/homebrew/bin/gophermind` stays 0.9.0 and untouched; `make rebuild-all` also commits and deploys the desktop app, so it is not reused |
| P11 | `--print-state-paths` output is tab separated lines, `action scope path` | The orchestrator reads it with `awk`; JSON adds nothing |
| P12 | `--graded` is implied by `--expect-head`; it refuses `--resume`, leftover state and a dirty or moved target, and the report says `resumed: no` | A graded attempt is one invocation from a cleared state |
| P13 | The end-to-end tests use their own small rig in `projectrun/testhelp_test.go` | `executor/rig_test.go` is a test file of another package and cannot be imported |
| P14 | `ConfirmUnderstanding` unattended is decided by the planner (`confirm.go`: store complete, nothing open, `confirmed_by: unattended`); `unattendedGate.Confirm` returns `ErrUnattended` if ever reached | The stage already has the rule for `assume_and_document`; a gate that approved on its own would confirm text nobody saw |
| P15 | `projectrun` writes `<run>/_state/project.json`, not `<run>/project.json` | `tree.Store.Load` treats every root-level `.json` that is not in its `notNodes` list as a node and fails; `_state/` is skipped. Changing `tree` for a report file would widen this plan's exceptions |
| P16 | State backends are an `Env.Backends` seam returning `blackboard.NewFS` and `ledger.NewFS` over `planner.LookupRun` | `cmd/gophermind/brief_state.go` does the same in `package main`, which `projectrun` cannot import; there is nothing to open or close |
| P17 | The node-class table of the report is built in `projectrun/classes.go` from blackboard rows, `planner.ReadClasses` and the ledger; the executor's `report.json` is not changed | A new `report.json` key is a schema bump that D and G also read |

### Facts verified against the code (do not re-derive)

- `executor.Run(ctx, Options) (Report, error)`; `Report = report.Report`; `report.Report.Resumed` is the executor state's sticky flag (once any invocation resumed, every later report says so). `executor.Options` has `RunDir, Repo, Caller, Board, Ledger, Gate, Sink, Settings, Secrets, Git, Proxy, LedgerErrors, Now, EnvNotes`.
- `report.ExitCode(status, stopReason)` returns verified 0, failed 1, escalated 4 (3 when `stopReason == "waiting_on_human"`), interrupted 5, anything else 1. Exit 6 (preflight) and 7 (harness fault: a returned executor error) are the `cmd/gophermind` constants `exitPreflight` and `exitFault`; `projectrun.ExitCode` in Task 2 wraps all of it. Exit 3 is used (waiting).
- `planner.Options` has `BriefPath`, `RunID`, `Yes` (approval and confirmation `flag`), `AllowPublic`, `StopAfter`. It has no `Repo` and no `Unattended` yet (Task 3). `planner.Deps` has `Caller, Gate, Sink, Board, Settings, OpenSecrets, PromptSecret, LedgerErrors, Now`; it has no `ResetRun` (removed with the database). `planner.ReadCoverage(runDir) (CoverageFile, error)`, `ReadRequirements`, `ReadClasses`, `VerifyApproval`, `RenderPlan(runDir) (markdown, hash string, err error)`, `LookupRun`, `ReadStatus`, `ChangeAnswer`, `IgnoredDuplicates` exist; `answer`, `approval`, `understandingRec` and the question store (`qstore`, `qrec`) are unexported (Task 3 adds readers).
- The planner stages after Load, in order (`planner/planner.go` `stages`): `clarify`, `confirm`, `contract`, `decompose`, `enrich`, `coverage`, `approve`, `testwriter`. A stage error is wrapped `planner: <stage>: ...`. `Options.StopAfter` accepts those names and `load`.
- `run.takesRecommendations()` (`planner/clarify.go`) is `Front.OnAmbiguity == "assume_and_document"`. It decides Clarify (the frontier takes `recommended`, settled `unattended-default`, parser option `RequireRecommended`), Confirm (no gate call: store complete and nothing open, `confirmed_by: unattended`) and, once Task 3 changes it, `callAsking` (which tests the brief field directly today, `planner.go` line 286). Clarify settles through `writeDecision` (`decisions/<id>.md`) and `writeAnswersView` (`answers.json`, `assumed` is `answered_by == unattended-default`).
- `human.Gate` has `Ask`, `Approve`, `Confirm`, `Escalate`. `human.Understanding{Markdown, Hash}`. `human.Terminal.Confirm` prints the text and `Understanding hash:` and asks `[y/N]`; `human.File.Confirm` writes `UNDERSTANDING.md` with a decision block and returns `ErrWaiting`; `human.Programmatic.Confirm` publishes `KindConfirm`.
- `approval.json` carries `understanding_hash`; `planner.VerifyApproval` refuses an approval without it. `approve` requires `confirmDone`.
- `planner.storeSecrets` skips a name that is already in the run scope and otherwise copies the harness value; it never overwrites a run-scope value.
- `human.AnsweredByUnattended == "unattended-default"`; `human.Decision` has `Approved`, `By`, `Note` and no AnsweredBy; `human.Resolution` has `Action`, `Note`, `AnsweredBy`.
- The four planner warning counts reach only the event sink (`events.KindWarning`, message prefix `leaf_defaulted: N`, `doc_defaulted: N`, `leaf_normalized: N`, `outline_id_normalized: N`, each N the count of that emission, in `decompose_fix.go`, `schemafix.go`, `idnorm.go`); duplicates come from `planner.IgnoredDuplicates` and `report.PlannerWarnings`.
- Run state is files. `blackboard.NewFS(resolve blackboard.RunDirFunc) *blackboard.FS` and `ledger.NewFS(resolve ledger.RunDirFunc) *ledger.FS`, `resolve func(runID string) (string, error)`; `cmd/gophermind/brief_state.go` builds both over `planner.LookupRun(id).RunDir` (`runDirOf`, unexported in `package main`). `ledger.Ledger.Amend` takes the run id. There is no `briefv2/db` package; `briefv2/nosql_test.go` forbids `database/sql` under `briefv2`.
- `tree.Store.Load` reads `root.json`, `<component>/component.json` and `<component>/<fn-id>.json` under the run folder; it skips `logs`, `_state`, `attempts`, `*.runtime.json`, and these root-level files: `contracts.json`, `answers.json`, `approval.json`, `report.json`, `requirements.json`, `coverage.json`, `dependencies.json`, `acceptance.json`. There is no `tree/` folder.
- `settings.Default()` carries the planner keys `defaults.clarify_max_calls` (4), `clarify_max_questions` (30), `enrich_batch_size` (4), `route_by_node_tier` (unset means true) and the executor keys `executor.artifacts`, `artifact_max_bytes`; `settings.Tiers` is `strong`, `standard`, `any`.
- `gophermind-lib/gitenv` (not under `briefv2`) holds `gitenv.Command` and `CommandContext`.
- `db.ClearRun` and `ResetRun` do not exist. A fresh run starts from no run folder (the `stale state` preflight check guarantees it), so there is nothing to clear at start.
- `main.go` already has the `version` verb and `--version`; the Makefile `rebuild-all` target already stamps `Version=dev+<sha>`, `Commit`, `Date`.

### Spec gaps and the ruling taken

| # | Gap found while planning | Ruling |
|---|---|---|
| G1 | `planner.resolveRepo` is unexported and takes the brief's `repo:`; the preflight needs the same resolution | Task 3 exports `planner.ResolveRepo` (a wrapper, behavior unchanged) |
| G2 | `answers.json`, `approval.json`, `_state/understanding.json` and the question store have unexported types; the report needs all four | Task 3 exports `ReadAnswers`, `ReadApproval`, `ReadUnderstanding` and `ReadQuestionCounts` |
| G3 | Spec 5 says the executor's gate is the unattended gate; executor spec 7.4 says a nil gate also stops | Pass the unattended gate anyway so the stop is explicit and testable (`AnsweredBy` is recorded) |
| G4 | `settings.BuildProviders` fails on the first missing key | The preflight calls `Env.BuildProviders` with a harness-scope lookup and reports its error text (it names the secret, never a value) |
| G5 | The planner keeps a run-scope value that already exists, so a stale value of an earlier attempt would mask a changed harness value | Task 4's thin pre-step also overwrites a differing run-scope value with the harness value on a fresh run (one `Set`) |
| G6 | `gitland` forbids `branch -D`, and the git wrapper may block `reset --hard`, `clean` and `branch -D` | The preflight refuses a leftover branch; the runbook gives the orchestrator's commands; the orchestrator tests them in the target repo first and asks John before any override (spec 9) |
| G7 | A generated secret written to the harness scope persists, so a rerun finds it as a vault value | Accepted: reruns reuse the same key (stable); the report says `generated:<kind>` the first time and `vault` after; the vault has no delete |
| G8 | The earlier draft cleared a run's database rows at start through a `ResetRun` hook; A removed the database and the hook | Nothing to clear: the preflight guarantees no run folder, so ledger and blackboard start empty. Clearing is the removal of the run folder (spec 9), after an archive |
| G9 | `human.Gate` gained `Confirm`, so the unattended gate must implement it, and the planner decides unattended confirmation by its own rule | Task 2 gate returns `ErrUnattended` from `Confirm`; Task 3 makes `takesRecommendations()` follow `Options.Unattended`; a test pins both |
| G10 | A root-level `project.json` would fail `tree.Store.Load` (it parses every unlisted root `.json` as a node) | The file is `_state/project.json` (P15) |
| G11 | `report.json` has no node-class dimension; `ledger.Summary` and `Call.NodeClass` do | Task 7 builds the table in `projectrun` (P17) |

## Review Focus

1. A fresh run never resumes: a leftover run folder, scratch folder, run record or branch is refused unless `--resume`, and `--graded` never resumes. (Task 5 `TestPreflightStaleStateAndBranch`, `TestPreflightGraded`; Task 12 `TestProjectRefusesStaleStateWithoutResume`, `TestHealthyPathNeverResumes`)
2. No human step: in unattended mode nothing reads stdin, the gate's `Ask` and `Confirm` fail loudly, and the planner calls neither (Clarify takes recommendations, Confirm decides by rule). (Task 2 `TestUnattendedGateAskFailsLoudly`, `TestUnattendedGateConfirmFailsLoudly`; Task 3 `TestUnattendedConfirmRecordsUnattendedAndBindsApproval`; Task 12 `TestProjectE2EGreeter` runs with a nil `In`)
3. Every defaulted or assumed decision is recorded and visible: answers with `Assumed`, counts by `answered_by` in the report, `confirmed_by` and the understanding hash, the override of `halt` named, the planner warning counts. (Task 3 `TestUnattendedClarifyTakesRecommendationsOverHalt`; Task 7 `TestPrintedReportHasNoModelText`, `TestSinkCountsPlannerWarnings`; Task 12 `TestProjectE2EGreeter`)
4. Approval binds to the plan hash and the understanding hash and refuses below N of N, decided from `planner.ReadCoverage`, not from markdown. (Task 2 `TestUnattendedGateRefusesGap`; Task 12 `TestUnattendedApproveBindsHash`)
5. Preflight lists every missing item in one pass with its fix command, calls no model, builds no backend, writes nothing, and exits 6. (Task 5 `TestPreflightMissingListedNoModelCallNothingWritten`, `TestPreflightHumanListNumbered`; Task 8 `TestRunPreflightFailureStopsEverything`)
6. No secret value anywhere. (Task 4 `TestGeneratedValueNeverPrinted`; Task 12 `TestNoSecretValueAnywhere`)
7. Every stop condition has a status, a reason and an exit code. (Task 8 `TestEveryStopConditionHasReasonAndCode`, `TestRunMapsExecutorStatusesToExitCodes`)
8. The printed report always ends with the two proof lines, also when the planner stopped. (Task 7 `TestPrintedReportEndsWithProofLines`, `TestProofLinesWhenExecutorDidNotRun`)
9. The extraction changes no behavior: the existing `cmd/gophermind` tests pass byte-unchanged. (Task 1)
10. v1 still works under its new names. (Task 10 `TestPlanV1CommandsRenamed`)
11. The node-class table adds up: leaves per class sum to the leaves of the plan and attempts per class sum to the executor's counted attempts. (Task 7 `TestClassStatsAggregatesRowsClassesAndCalls`)
12. No database anywhere and clearing is a folder removal: `briefv2/nosql_test.go` passes and the state table has no database row. (Task 6 `TestStatePathsListEverything`; Task 12 `TestClearedStateRerunsClean`)

## File Structure

```text
gophermind-lib/briefv2/
  envcheck/    envcheck.go, diskfree_unix.go, diskfree_windows.go, envcheck_test.go   (Task 1)
  planner/     planner.go, load.go, clarify.go, exported.go                             modified (Task 3)
               unattended_test.go, repo_override_test.go                                new (Task 3)
  projectrun/  options.go, args.go, gate.go, env.go,
               args_test.go, gate_test.go, env_test.go                                  (Task 2)
               gensecrets.go, gensecrets_test.go                                        (Task 4)
               preflight.go, preflight_repo.go, preflight_env.go, preflight_secrets.go,
               preflight_test.go, preflight_repo_test.go, preflight_env_test.go,
               preflight_secrets_test.go                                                (Task 5)
               statepaths.go, statepaths_test.go                                        (Task 6)
               report.go, sink.go, classes.go, report_test.go, sink_test.go, classes_test.go  (Task 7)
               run.go, run_test.go                                                      (Task 8)
               testhelp_test.go, e2e_test.go                                            (Task 12)
cmd/gophermind/  brief_preflight.go, brief_run.go modified, diskfree_* moved            (Task 1)
                 project.go, project_test.go, main.go                                   (Task 9)
gophermind-lib/tui/  commands_registry.go, update.go, project.go, approve.go, execute.go, questions.go,
                     question_round.go, commands.go, model.go, run.go + their tests     renamed literals (Task 10)
                     project_v2.go, project_v2_test.go                                  (Task 11)
scripts/build-dev-binary.sh, cmd/gophermind/devbinary_test.go                           (Task 13)
docs/briefv2/README.md, docs/briefv2/project-runbook.md, CHANGELOG.md                   (Task 14)
```

## Spec traceability

| Spec item | Task | Tests |
|---|---|---|
| 4 architecture, `ParseArgs`, `Run`, `Env` | 1, 2, 8, 9, 11 | `TestParseArgsTable`, `TestDefaultEnvFieldsAreSet`, `TestProjectCLIExitCodes`, `TestSlashProjectRunsProjectrun` |
| 5 flow, one process, planner then executor | 8, 12 | `TestRunCallsExecutorWithPlanArtifacts`, `TestProjectE2EGreeter` |
| 5 `--resume`, no-restart rule | 8, 12 | `TestRunResumeSkipsFinishedPlannerStages`, `TestResumeFlagContinuesAndIsRecorded`, `TestHealthyPathNeverResumes` |
| 6 flags and exit codes | 2, 8, 9 | `TestParseArgsTable`, `TestExitCodes`, `TestProjectCLIExitCodes` |
| 6, 7 graded mode | 2, 5, 8, 12 | `TestParseArgsGraded`, `TestPreflightGraded`, `TestRunGradedReportsResumedNo`, `TestGradedRefusesSecondInvocation` |
| 7 pre-plan preflight, every check, human list | 5 | `TestPreflightRepoChecks`, `TestPreflightStaleStateAndBranch`, `TestPreflightToolsAndSettingsNotCreated`, `TestPreflightRequirePrivate`, `TestPreflightProvidersFallback`, `TestPreflightExpectBinaryCommit`, `TestPreflightVaultPassphrase`, `TestPreflightSecretMatrix`, `TestPreflightDatabaseLoopbackAndDistinct`, `TestPreflightDatabaseTunnelHint`, `TestPreflightHumanListNumbered` |
| 7 preflight writes nothing, is not a run failure (R7) | 5, 8 | `TestPreflightMissingListedNoModelCallNothingWritten`, `TestPreflightFailureExit6IsNotARun`, `TestRunPreflightFailureStopsEverything` |
| 7 shared checks extracted | 1 | the unchanged `cmd/gophermind` suite, `TestResolveBaseURLsFallbackUsed` |
| 8.1 secrets, generated into the harness scope (R3a) | 4 | `TestGeneratedWrittenToHarness`, `TestVaultBeatsGenerated`, `TestGenerateShapes`, `TestRunScopeOverwrittenOnFreshRun`, `TestRunScopeKeptOnResume`, `TestGeneratedValueNeverPrinted` |
| 8.2 clarify recommendations over `halt`, required recommendations, mid-stage rule (R3b) | 3 | `TestUnattendedClarifyTakesRecommendationsOverHalt`, `TestUnattendedClarifyRequiresRecommendations`, `TestUnattendedClarifyFailsWithoutRecommendations`, `TestUnattendedClarifySavedQuestionWithoutRecommendation`, `TestUnattendedMidStageQuestionAssumes`, `TestAttendedClarifyUnchanged` |
| 8.2a ConfirmUnderstanding (R14) | 2, 3, 12 | `TestUnattendedGateConfirmFailsLoudly`, `TestUnattendedConfirmRecordsUnattendedAndBindsApproval`, `TestUnattendedConfirmRefusesOpenQuestions`, `TestAttendedConfirmUsesGate` |
| 8.3 approval and escalation gate, milestone line (R3c, R11) | 2, 7, 12 | `TestUnattendedGateApprovesFullCoverage`, `TestUnattendedGateRefusesGap`, `TestUnattendedGateEscalateStops`, `TestMilestoneLine`, `TestUnattendedApproveBindsHash` |
| 8.4 stop table (R3d) | 8 | `TestEveryStopConditionHasReasonAndCode` |
| 9 state table (files, no database), archive then clear order, `--print-state-paths` (R4) | 6, 12 | `TestStatePathsListEverything`, `TestStatePathsNoSideEffects`, `TestClearedStateRerunsClean` |
| 9 repo override (R9) | 3 | `TestRepoOverrideUsedForRunFolder`, `TestRepoOverrideMustMatchOnResume` |
| 10 dev binary, version in report (R5) | 7, 13 | `TestReportCarriesBinaryVersion`, `TestDevBinaryScriptRefusesDirtyTree`, `TestDevBinaryScriptRefusesUnpushedCommit`, `TestDevBinaryScriptPrintsLdflags` |
| 11 report, warnings, defaults, base URL host, no model text in print (R10) | 7, 8 | `TestPrintedReportHasNoModelText`, `TestReportJSONRoundTrip`, `TestSinkCountsPlannerWarnings`, `TestReportCarriesPlannerWarnings`, `TestReportRecordsAnsweringHost` |
| 11 node-class dimension (R15) | 7, 8, 12 | `TestClassStatsAggregatesRowsClassesAndCalls`, `TestReportCarriesNodeClassTable`, `TestPrintedReportNodeClassLines` |
| 12 v1 renamed (R2) | 10 | `TestPlanV1CommandsRenamed`, `TestPlanV1PrintsDeprecation` |
| 12 tui `/project` | 11 | `TestSlashProjectRunsProjectrun`, `TestSlashProjectRefusesAttended`, `TestSlashProjectRejectsV1Form`, `TestSlashProjectEscCancels`, `TestSlashProjectSecondRunRefused`, `TestSlashProjectRegistry` |
| 15 tests: secrets canary, e2e | 12 | `TestNoSecretValueAnywhere`, `TestProjectE2EGreeter`, `TestProjectPlanFailureExit1`, `TestProjectEscalationExit4`, `TestProjectInterruptedExit5` |
| 16 human prerequisites | 14 | Step 5 (rehearsal, reported not asserted) |

---

### Task 1 (M): Extract the environment checks into `briefv2/envcheck`

**Why first:** `cmd/gophermind/brief_preflight.go` and `brief_run.go` are `package main`; `projectrun` and the TUI cannot import them. One small move, no behavior change, then both callers use the shared package.

**Files:**
- Create: `gophermind-lib/briefv2/envcheck/envcheck.go`, `envcheck_test.go`
- Move with `git mv`: `cmd/gophermind/diskfree_unix.go` and `diskfree_windows.go` to `gophermind-lib/briefv2/envcheck/` (package line becomes `package envcheck`, `freeBytes` stays unexported)
- Modify: `cmd/gophermind/brief_preflight.go`, `cmd/gophermind/brief_run.go` (delete the moved bodies, keep thin same-name wrappers)
- Test files in `cmd/gophermind` are NOT modified.

**Interfaces (envcheck.go):**

```go
const DefaultProbeTimeout = 3 * time.Second
const DefaultMinFreeBytes uint64 = 2 << 30

type ProbeResult struct {
	Provider string
	Host     string // host name only of the base URL that answered
	Answered bool
	Fallback bool // a base_url_fallbacks entry answered, not base_url
}

func ResolveBaseURLs(ctx context.Context, cfg *settings.Config, client *http.Client, timeout time.Duration) (*settings.Config, []ProbeResult)
func ProbeBaseURL(ctx context.Context, client *http.Client, base string, timeout time.Duration) bool
func EnvNotes(res []ProbeResult) []string
func LookInPath(name, path string) (string, bool)
func LoopbackAddr(val string) (string, bool)
func ModuleCacheFree(modCache string) (uint64, error)
func DirtyOutsideTests(repo, runDir string) (int, error)
func SafeLine(s string) string
```

**Behavior rules:**
- Move the bodies of `resolveBaseURLs`, `probeBaseURL`, `envNotes`, `lookInPath`, `loopbackAddr`, `moduleCacheFree`, `dirtyOutsideTests`, `safeLine`, `freeBytes` and the type `probeResult` verbatim. The only signature change: the two probe functions take the timeout as an argument instead of reading the package variable `probeTimeout` (the cmd tests assign `probeTimeout` and `minFreeBytes`; those variables must keep working, so they stay in `package main`).
- `cmd/gophermind` keeps these names so its tests compile unchanged: `var probeTimeout = envcheck.DefaultProbeTimeout`, `var minFreeBytes = envcheck.DefaultMinFreeBytes`, `type probeResult = envcheck.ProbeResult`, and one-line wrappers `resolveBaseURLs(ctx, cfg, client)` (passes `probeTimeout`), `probeBaseURL(ctx, client, base)` (passes `probeTimeout`), `envNotes`, `lookInPath`, `loopbackAddr`, `moduleCacheFree`, `dirtyOutsideTests`, `safeLine`. `briefPreflight` and `briefRun` keep calling the wrappers, so `brief run --check-env` now calls the shared package.
- `DirtyOutsideTests` uses `gitenv.Command` exactly as today. The one addition: an empty `runDir` means no allowed test files (today it would read `_state/test_files.json` relative to the working directory). The pre-plan preflight in Task 5 passes `""`.
- If a cmd test calls `freeBytes` directly, keep a one-line wrapper too; run `grep -n freeBytes cmd/gophermind/*_test.go` first.
- `envcheck` imports only `settings`, `gophermind-lib/gitenv` and the standard library. No `main`, no `projectrun`. `providerHTTPClient` (it needs `provider`) stays in `cmd/gophermind`; `projectrun` builds the same client with `provider.NewHTTPClient(cfg.MaxCallTimeout() + time.Minute)`.

**Tests (`envcheck_test.go`, new, small):** `TestResolveBaseURLsFallbackUsed` (an `httptest` server on loopback as the fallback and a closed port as `base_url`: the result answers on the fallback, `Fallback` true, the copy of the config carries the fallback, the original config is untouched), `TestResolveBaseURLsSkipsPublicFallbackForPrivate` (a public fallback host is never dialled; use a failing `http.Client` transport), `TestProbeTimeoutIsAParameter` (a listener that never answers returns false within twice a 100 ms timeout), `TestLoopbackAddr` (table: `127.0.0.1`, `localhost`, `::1`, a LAN address, no port with `postgres` scheme gives 5432), `TestLookInPath` (relative and empty PATH entries skipped), `TestDirtyOutsideTestsEmptyRunDir` (a temp repo with one untracked file and `runDir == ""` counts 1; a file under `.gophermind/` counts 0). `TestMain` unsets `GIT_*`.

- [ ] **Step 1: Baseline.** `gt -race -timeout 30m ./cmd/gophermind/ 2>&1 | tail -5`; write down the pass count (`gt -count=1 -v ./cmd/gophermind/ | grep -c '^--- PASS'` or `-json`).
- [ ] **Step 2: Write `envcheck_test.go`** (it fails to compile).
- [ ] **Step 3: Implement** `envcheck.go`, `git mv` the two diskfree files and fix their package line, replace the cmd bodies with the wrappers.
- [ ] **Step 4: Verify.** The cmd suite passes with the same count and `git diff --stat -- 'cmd/gophermind/*_test.go'` prints nothing.
- [ ] **Step 5: Commit.**

```bash
gofmt -l cmd gophermind-lib/briefv2/envcheck && go vet ./cmd/... ./gophermind-lib/briefv2/envcheck/... && gt -race -timeout 30m ./cmd/gophermind/ ./gophermind-lib/briefv2/envcheck/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/envcheck/envcheck.go gophermind-lib/briefv2/envcheck/envcheck_test.go gophermind-lib/briefv2/envcheck/diskfree_unix.go gophermind-lib/briefv2/envcheck/diskfree_windows.go cmd/gophermind/brief_preflight.go cmd/gophermind/brief_run.go
git status --short | grep -i diskfree   # the two cmd paths show as renamed or deleted: stage them too
git add cmd/gophermind/diskfree_unix.go cmd/gophermind/diskfree_windows.go
git commit -m "refactor(briefv2): move the environment checks to briefv2/envcheck, no behavior change" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 2 (M): `Env`, `Options`, argument parsing, exit codes, and the unattended gate

**Files:**
- Create: `gophermind-lib/briefv2/projectrun/options.go`, `args.go`, `gate.go`, `env.go`
- Create tests: `args_test.go`, `gate_test.go`, `env_test.go`

**Interfaces:**

```go
// options.go
const (
	ExitVerified, ExitFailed, ExitInvalid, ExitWaiting, ExitEscalated, ExitInterrupted, ExitPreflight, ExitFault = 0, 1, 2, 3, 4, 5, 6, 7
)
// ExitCode: status "preflight_failed" -> 6, "harness_fault" -> 7, "invalid_brief" -> 2, else report.ExitCode(status, stopReason).
func ExitCode(status, stopReason string) int

type Options struct {
	BriefPath          string
	Repo               string            // --repo
	Attended           bool
	Resume             bool
	Graded             bool              // --graded, implied by ExpectHead
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
func newUnattendedGate(runDir func() (string, error)) human.Gate

// env.go  (the ONE definition of Env)
type VersionInfo struct{ Version, Commit, Date string }
type Env struct {
	SettingsPath     func() (string, error)
	LoadSettings     func(path string) (*settings.Config, error)
	BuildProviders   func(cfg *settings.Config, secret func(name string) (string, error)) (map[string]provider.Provider, error)
	Probe            func(ctx context.Context, cfg *settings.Config) (*settings.Config, []envcheck.ProbeResult)
	ConfigDir        func() (string, error)
	VaultPath        func() (string, error)
	OpenVault        func(path, passphrase string) (*vault.Vault, error)
	// Backends returns the run-state stores: blackboard.NewFS and ledger.NewFS over planner.LookupRun,
	// the same wiring as cmd/gophermind/brief_state.go. Nothing to open or close, no database.
	Backends         func() (blackboard.Blackboard, ledger.Ledger)
	Git              func(ctx context.Context, repo string, args ...string) (string, error)
	LookPath         func(file, path string) (string, bool)
	SandboxPreflight func(ctx context.Context) error
	Dial             func(ctx context.Context, addr string) error
	Getenv           func(string) string
	Now              func() time.Time
	Version          func() VersionInfo
	Executable       func() (string, error)
	RunExecutor      func(ctx context.Context, o executor.Options) (executor.Report, error)
	Rand             io.Reader
}
func DefaultEnv() Env
```

**Behavior rules:**
- `ParseArgs`: exactly one positional, the brief path, before or after the flags. Flags as in spec section 6, plus `--graded`. `--generate` is repeatable; each value is `NAME=KIND` with `NAME` matching `^[A-Z][A-Z0-9_]*$` and `KIND` one of `hex32`, `placeholder`; a repeated name is an error. `--attended` with `allowAttended` false: `attended runs need a terminal and are only available from the gophermind project command`. `--preflight-only` with `--print-state-paths`: error. `--expect-head` sets `Graded = true`. `--graded` without `--expect-head`: error `--graded needs --expect-head <rev>`. `--graded` with `--resume`: error `a graded attempt never resumes`. `--graded` with `--attended`: error. Two positionals: `the v1 form "/project <name> <brief>" is gone: give the brief path only (the v1 planner is /plan-v1)`. No positional: usage error. Unknown flag: error naming the flag. Errors never echo a flag value that could be a secret (`--generate` errors name the flag and the position only).
- Gate (`unattendedGate`, implements all four methods of `human.Gate`): `Ask` returns `ErrUnattended`. `Confirm` returns `ErrUnattended` too: unattended confirmation is decided by the planner's own rule (Task 3, spec 8.2a), so the gate is never asked on the healthy path and a call to it is a defect that must fail loudly, never a confirmation nobody saw. `Approve`: resolve the run folder with the injected `runDir()`; `planner.ReadRequirements` and `planner.ReadCoverage` decide: approved only when there is at least one requirement and every requirement id appears as `Covered[i].Requirement`; no markdown is parsed. `Decision.By` is `unattended`; `Note` is `plan <first 12 hex of plan.Hash>` when approved, `coverage <C> of <N>` or `no coverage file` when refused. `Escalate` returns `Resolution{Action: human.ActionStop, AnsweredBy: human.AnsweredByUnattended}`.
- `DefaultEnv`: `settings.Path`, `settings.Load`, `cfg.BuildProviders(client, secret)` with `client := provider.NewHTTPClient(cfg.MaxCallTimeout() + time.Minute)` (no redirects, no proxy environment: what `providerHTTPClient` returns in `cmd/gophermind/brief_run.go`; a bare `&http.Client{}` would follow redirects), `Probe` is `envcheck.ResolveBaseURLs(ctx, cfg, client, envcheck.DefaultProbeTimeout)` with the same client, `config.Dir`, `VaultPath` re-implements the logic of `cmd/gophermind/brief.go` `vaultPath()` (env `GOPHERMIND_VAULT_PATH`, else `<config dir>/vault.age`; do not import `main`), `vault.Open(path, pass, vault.Options{})`, `Backends` returns `blackboard.NewFS(runDirOf)` and `ledger.NewFS(runDirOf)` with `runDirOf` a closure over `planner.LookupRun(id).RunDir` (copy `brief_state.go`), `Git` as below, `LookPath` is `envcheck.LookInPath`, `sandbox.Preflight`, `Dial` is `net.Dialer{Timeout: 5s}`, `os.Getenv`, `time.Now`, `version.*`, `os.Executable`, `executor.Run`, `crypto/rand.Reader`.
- `Env.Git` (the only git in preflight) accepts only these argument vectors, anything else returns an error: `rev-parse` with `--is-inside-work-tree`, `--show-toplevel`, `HEAD` or `--verify <rev>^{commit}`; `symbolic-ref --short HEAD`; `status --porcelain -uall`; `branch --list <name>`. It runs `gitenv.CommandContext(ctx, repo, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "--no-optional-locks", args...)` (the git on `PATH`, no shell, `GIT_*` stripped, `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1` appended). No `/usr/bin/git`, no `GITLAND_TEST_GIT`.

**Tests:** `TestParseArgsTable` (flags before and after the positional; repeated `--generate`; bad kind; bad name; duplicate name; `--attended` refused when not allowed and accepted when allowed; both query flags; two positionals gives the v1 message; none gives usage; unknown flag), `TestParseArgsGraded` (`--expect-head` implies `Graded`; `--graded` alone, with `--resume`, with `--attended` each error), `TestUnattendedGateApprovesFullCoverage` (a temp run folder with `requirements.json` and `coverage.json`), `TestUnattendedGateRefusesGap` (a requirement missing from `Covered`; zero requirements; no coverage file), `TestUnattendedGateEscalateStops` (`ActionStop` and `AnsweredBy == human.AnsweredByUnattended`), `TestUnattendedGateAskFailsLoudly` (`errors.Is(err, ErrUnattended)`), `TestUnattendedGateConfirmFailsLoudly` (`Confirm` returns `ErrUnattended` and an unapproved decision, for any `human.Understanding`), `TestExitCodes` (constants are 0 to 7; `ExitCode` table: verified 0, failed 1, escalated 4, `escalated`+`waiting_on_human` 3, interrupted 5, `preflight_failed` 6, `harness_fault` 7, `invalid_brief` 2), `TestDefaultEnvFieldsAreSet` (no nil field), `TestDefaultEnvBackendsAreFilesystem` (the two returned values are `*blackboard.FS` and `*ledger.FS`; a planner run record in a temp config dir resolves, and calling `Backends` creates no file), `TestEnvGitUsesCleanEnvAndAllowList` (with `GIT_DIR` set to another repo in the test environment `Env.Git` still reads the repo it is given; `push`, `reset`, `checkout`, `branch -D`, and a `status` with extra flags are refused), `TestVaultPathHonorsOverride`, `TestDefaultDialTimesOut` (a closed loopback port fails within a second). `TestMain` unsets `GIT_*`.

- [ ] **Step 1: Write the failing tests. Step 2: Run** `gt -race -timeout 30m ./gophermind-lib/briefv2/projectrun/` and see compile failures. **Step 3: Implement** `options.go`, `args.go`, `env.go`, `gate.go`.
- [ ] **Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/projectrun/... && gt -race -timeout 30m ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun/options.go gophermind-lib/briefv2/projectrun/args.go gophermind-lib/briefv2/projectrun/gate.go gophermind-lib/briefv2/projectrun/env.go gophermind-lib/briefv2/projectrun/args_test.go gophermind-lib/briefv2/projectrun/gate_test.go gophermind-lib/briefv2/projectrun/env_test.go
git commit -m "feat(briefv2): projectrun env, options, argument parsing and the unattended gate" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 3 (M): Planner extension: repo override, unattended mode and exported readers

**Files:**
- Modify: `gophermind-lib/briefv2/planner/planner.go` (`Options`; `callAsking` line 286), `load.go` (repo override), `clarify.go` (`takesRecommendations`), `exported.go` (new readers)
- Create tests: `planner/unattended_test.go`, `planner/repo_override_test.go` (package `planner_test`, using the helpers of `harness_test.go`: `newRig`, `approving`, `scriptGate`, `writeBrief`, `variant`; of `clarify_rounds_test.go`: `fixtureDir`, `chainQuestions`, `readStore`, `assumeBrief`; of `confirm_test.go`: `readUnderstandingRec`; the constants `greeter` and `greeterID`)
- No fixture folder. The greeter fixture under `planner/testdata/greeter` already holds `clarify.txt`, `clarify.more.txt`, the Enrich replies and the whole pipeline, and its brief says `on_ambiguity: halt`, which is what the unattended tests need.

`answer`, `approval` and the question store stay unexported; the exported readers below are how `projectrun` reads them. `Options.Yes` and the `flag` paths of Confirm and Approve are untouched.

**Interfaces:**

```go
// planner.go
type Options struct {
	// ...existing fields (BriefPath, RunID, Yes, AllowPublic, StopAfter)...
	Repo       string // plan only: replaces the brief's repo field for the run folder and the run record
	Unattended bool   // no human: Clarify, Confirm and a mid-stage question follow takesRecommendations()
}

// clarify.go: the ONE place the policy is read (was: OnAmbiguity == "assume_and_document")
func (r *run) takesRecommendations() bool {
	return r.brief.Front.OnAmbiguity == "assume_and_document" || r.opts.Unattended
}

// exported.go
func ResolveRepo(repo string) (string, error)        // wraps resolveRepo; same errors (brief.InvalidError)
type Answer struct {
	ID, Stage, Question, Answer string
	Assumed                     bool                  // answered_by == unattended-default
}
func ReadAnswers(runDir string) ([]Answer, error)     // [] when answers.json is absent
type ApprovalInfo struct{ ApprovedAt, ApprovedBy, PlanHash, UnderstandingHash string }
func ReadApproval(runDir string) (ApprovalInfo, bool, error)       // false when approval.json is absent
type UnderstandingInfo struct{ ConfirmedAt, ConfirmedBy, Hash string }
func ReadUnderstanding(runDir string) (UnderstandingInfo, bool, error) // _state/understanding.json; false when absent
type QuestionCounts struct {
	Total, Settled, Open int
	Rounds, Calls        int            // rounds put to the gate, Clarify model calls (qstore.Round, qstore.Calls)
	ByAnsweredBy         map[string]int // human, accepted, unattended-default, probe
	MidStageAssumed      int            // settled unattended-default questions raised by a stage other than clarify
}
func ReadQuestionCounts(runDir string) (QuestionCounts, error)     // zero value when the store is absent
```

**Behavior rules:**
- `load` (plan): `repoField := b.Front.Repo`; when `o.Repo != ""` use it instead; `resolveRepo(repoField)`; the run folder and `RunRecord.Repo` use the resolved path. `loadExisting` (resume) with `o.Repo != ""`: resolve `o.Repo` and compare with `rec.Repo`; a mismatch is `planner: run <id> was planned in <rec.Repo>, not <resolved>`. `RenderPlan` and the approval hash are unchanged (they print the brief's own `repo:`).
- `takesRecommendations()` is the only change in `clarify.go`. Because `clarify` (`unattended := r.takesRecommendations()`), `clarifyRound` (frontier takes `q.Recommended`, settled `byUnattended`) and `confirm` (`case r.takesRecommendations()`: store complete and nothing unsettled, else `the understanding still has open questions`; `by = "unattended"`) already read it, `Unattended` reaches all three with no further edit. `clarifyCall` passes `RequireRecommended: unattended`, so a question without a recommendation is rejected by the parser and takes the router's malformed retry once; a second failure fails the stage. A saved question store from an earlier attended run whose open question has no recommendation fails with the existing error `question <id> has no recommended answer and this run takes recommendations`.
- `callAsking` (`planner.go`, the line `if r.brief.Front.OnAmbiguity == "assume_and_document" {`) becomes `if r.takesRecommendations() {`. Nothing else in it changes.
- `ReadQuestionCounts` reads `_state/clarify/questions.json` (`qstore`). `ReadAnswers` reads `answers.json` (a view the planner derives from the store). Neither reads a secret.
- No other planner behavior changes; with `Unattended` false every existing test passes untouched.

**Tests:**
- `TestUnattendedClarifyTakesRecommendationsOverHalt`: `newRig(t, gate, fixtureDir(t, {"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))`, the brief left at `on_ambiguity: halt`, `Options{Unattended: true, StopAfter: "clarify"}`; `len(gate.rounds) == 0`; `readStore` shows q1 `memory`, q2 `pgx`, q3 `10`, all `answered_by: unattended-default`, `Calls == 2`; `answers.json` has three answers, all `Assumed`; `decisions/q1.md` exists; no `QUESTIONS.md`.
- `TestUnattendedClarifyRequiresRecommendations`: `variant` with `clarify.txt` = `[{"id":"q1","question":"Which store?"}]` and `clarify.2.txt` = a question with a `recommended`; `g.led.List(ctx, greeterID, ledger.Filter{})` shows one `OutcomeMalformed` then one `OutcomeOK` clarify row; the answer is the recommendation.
- `TestUnattendedClarifyFailsWithoutRecommendations`: both replies lack a recommendation; `plan` returns an error containing `needs a recommended answer`; no `answers.json`.
- `TestUnattendedClarifySavedQuestionWithoutRecommendation`: run once attended to `StopAfter: "load"`, write `_state/clarify/questions.json` with one open question and no recommendation, then resume with `Unattended: true`; the error contains `no recommended answer`; nothing is answered.
- `TestUnattendedMidStageQuestionAssumes`: the pattern of `TestAQuestionInAssumeModeGoesBackToTheModel` (`decompose_test.go`) without editing the brief: `variant` with `decompose.greeting.txt` = the `question` const and `decompose.greeting.2.txt` = the real reply, `Options{Unattended: true, StopAfter: "decompose"}`, a gate that records asks; the gate is not asked; the second request's prompt contains `No human is available. Choose the most conservative option`; the store holds a settled `decompose-...` question `answered_by: unattended-default`; no `_state/question.json` remains.
- `TestUnattendedConfirmRecordsUnattendedAndBindsApproval`: the brief at `halt`, `Options{Unattended: true, StopAfter: "approve"}`, `approving()` gate (its decision `By` is `test`); `len(gate.understandings) == 0` and `plans` has one entry (Approve is still the gate's); `readUnderstandingRec` gives `confirmed_by: unattended` and a non-empty hash; `UNDERSTANDING.md` exists; `ReadUnderstanding` and `ReadApproval` agree: `ApprovalInfo.UnderstandingHash == UnderstandingInfo.Hash`, `ApprovedBy == "test"`.
- `TestUnattendedConfirmRefusesOpenQuestions`: plan with `Unattended` to `StopAfter: "clarify"`, edit `_state/clarify/questions.json` so one question is `open` and the store is still `complete`, resume with `RunID` and `Unattended`; the error contains `open questions`; no `_state/understanding.json`.
- `TestAttendedClarifyUnchanged`: `Unattended` false, `halt`, the file gate (`human.NewFile(dir)`): outcome `Waiting`, `QUESTIONS.md` exists. `TestAttendedConfirmUsesGate`: `Unattended` false, `halt`, `approving()`: `len(gate.understandings) == 1` and `confirmed_by` is the gate's `By`.
- `TestRepoOverrideUsedForRunFolder`: the brief's `repo:` names a missing directory; `Options.Repo` is a temp repo; the run folder is `<override>/.gophermind/<id>` and `LookupRun(id).Repo` equals the override.
- `TestRepoOverrideMustMatchOnResume`: resume with a different `Options.Repo` errors with `was planned in`; resume with the same path succeeds.
- `TestReadAnswersApprovalUnderstandingAndCounts`: after a full unattended plan to `approve`, `ReadAnswers` returns the settled answers with `Assumed`; absent file gives an empty slice; `ReadApproval` returns `ApprovedBy`, `PlanHash`, `UnderstandingHash`, and `ok == false` before approval; with `Options.Yes` it reads `flag`; `ReadQuestionCounts` gives `ByAnsweredBy["unattended-default"]` equal to the settled count, `Rounds >= 1`, `Calls == 2`, and `MidStageAssumed == 0`; a store with one stage-raised assumed question gives `MidStageAssumed == 1`.
- `TestResolveRepo`: `~` expansion, a URL refused with `brief.InvalidError`, a missing directory refused.

- [ ] **Step 1: Write the failing tests.** Read `planner/harness_test.go`, `clarify_rounds_test.go`, `confirm_test.go` and the two QUESTION tests in `decompose_test.go` first and reuse their helpers.
- [ ] **Step 2: Run** `gt -race -timeout 30m ./gophermind-lib/briefv2/planner/ -run 'Unattended|RepoOverride|ReadAnswers|ResolveRepo|AttendedClarify|AttendedConfirm'` and see them fail (unknown fields, missing functions, a policy that still follows the brief).
- [ ] **Step 3: Implement** in this order: `exported.go`, `planner.go` Options, `load.go`, `clarify.go` (`takesRecommendations`), `callAsking`. Run the focused tests after each file.
- [ ] **Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/planner/... && gt -race -timeout 30m ./gophermind-lib/briefv2/planner/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/planner/planner.go gophermind-lib/briefv2/planner/load.go gophermind-lib/briefv2/planner/clarify.go gophermind-lib/briefv2/planner/exported.go gophermind-lib/briefv2/planner/unattended_test.go gophermind-lib/briefv2/planner/repo_override_test.go
git diff --cached --stat
git commit -m "feat(briefv2): planner repo override, unattended mode through takesRecommendations and exported readers" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 4 (S): Thin generated-secret provisioning

**Files:** Create `gophermind-lib/briefv2/projectrun/gensecrets.go`, `gensecrets_test.go`.

The planner's `storeSecrets` (load.go) already copies each declared secret from the harness scope into the run scope. This task only makes the harness scope hold a value for names the command line asked GopherMind to generate, and keeps a stale run-scope value from masking a changed harness value.

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
	SourceMissing     Source = "missing"
)
type Provisioned struct{ Name string; Source Source }

func Generate(kind string, rnd io.Reader) (string, error)
// Sources is read only: where each declared secret's value would come from.
func Sources(store SecretStore, b *brief.Brief, gen map[string]string, canPrompt bool) []Provisioned
// ProvisionGenerated writes generated values into the harness scope, before the planner runs.
func ProvisionGenerated(store SecretStore, b *brief.Brief, gen map[string]string, resume bool, rnd io.Reader) ([]Provisioned, error)
```

**Behavior rules:**
- `Generate("hex32")` reads 32 bytes and returns 64 lowercase hex characters; `"placeholder"` returns `gm-placeholder-` plus 32 hex characters (16 bytes); any other kind is an error; a short read is an error.
- `ProvisionGenerated`, for each declared secret in order: a harness-scope value exists, so nothing is generated (`SourceVault`); else `gen[name]` exists, so generate and `Set(vault.HarnessScope, name, value)` (`SourceHex32` or `SourcePlaceholder`); else `SourceMissing` (the preflight already reported it; no error here). A harness value always beats a generated one.
- Fresh run only (`resume == false`): after the harness step, for every declared secret with a harness value whose run-scope value (`vault.RunScope(b.Front.ID)`) exists and differs, `Set` the run scope to the harness value (this is the only duplicate of `storeSecrets` and exists to defeat a stale value). A run-scope value that does not exist is left for `storeSecrets`. With `resume == true` neither scope is touched except that a missing harness value may still be generated.
- `Sources` never calls `Set` and never prompts. A nil `store` with no declared secrets is fine; with declared secrets `ProvisionGenerated` returns an error.
- Returned errors and the returned `Provisioned` list name secrets and sources only; a generated value is never printed, logged, returned or put in an error.

**Tests:** `TestGeneratedWrittenToHarness`, `TestVaultBeatsGenerated`, `TestGenerateShapes` (lengths, charset, prefix, unknown kind, failing reader), `TestRunScopeOverwrittenOnFreshRun` (stale run-scope value replaced after the harness value changed), `TestRunScopeKeptOnResume`, `TestSourcesReadOnly` (a store whose `Set` fails the test), `TestGeneratedValueNeverPrinted` (a deterministic reader, a canary generated value; it is absent from every returned error and from `fmt.Sprint` of the returned list), `TestProvisionNilStore`. Use an in-memory `SecretStore`.

- [ ] **Step 1: Write the failing tests. Step 2: Run and see failures. Step 3: Implement. Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/projectrun/... && gt -race -timeout 30m ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun/gensecrets.go gophermind-lib/briefv2/projectrun/gensecrets_test.go
git commit -m "feat(briefv2): projectrun writes generated secrets into the harness scope" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 5 (L): The pre-plan preflight and the human-prerequisites list

**Files:** Create `gophermind-lib/briefv2/projectrun/preflight.go`, `preflight_repo.go`, `preflight_env.go`, `preflight_secrets.go` and tests `preflight_test.go`, `preflight_repo_test.go`, `preflight_env_test.go`, `preflight_secrets_test.go`.

This is the PRE-PLAN variant of `brief run --check-env`: it runs before any run folder exists, so there is no approval check and secrets are read from the harness scope (not the run scope). It reuses `envcheck` and the check names of `brief_preflight.go` where they overlap.

**Interfaces:**

```go
type Check struct {
	Name   string
	OK     bool
	Detail string // ids, paths, counts; never a secret, never command output
	Fix    string // the exact command or setting line to fix it; empty when OK
}
type PreflightResult struct {
	Checks []Check
	Config *settings.Config       // resolved: base_url replaced by the host that answered; nil when settings failed
	Probes []envcheck.ProbeResult // which host answered, per provider
	Store  SecretStore            // the opened vault, nil when none was needed or it did not open; never put in a Check
}
func Failed(cs []Check) []Check
// PrintPreflight: "preflight: ok (N checks)", or "preflight: FAILED (K missing)",
// "What a human must provide, in this order:", then per failed check
//   "  <n>. <name>: <detail>" and "     Fix: <fix>",
// then the not-a-run-failure sentence. Warnings print as "warning: ..." lines.
func PrintPreflight(w io.Writer, cs []Check)
func Preflight(ctx context.Context, o Options, env Env, b *brief.Brief) PreflightResult // every check, fixed order, no early exit
```

**Check names and order (fixed words):** `repo`, `repo head`, `clean tree`, `stale state`, `landing`, `graded`, `sandbox`, `go toolchain`, `git`, `disk space`, `settings`, `privacy`, `provider <name>`, `binary`, `vault passphrase`, `vault`, `secret <NAME>`, `database <NAME>`, then `warning` entries. The names `sandbox`, `go toolchain`, `clean tree`, `vault passphrase`, `secret <NAME>`, `provider <name>` and `disk space` match `brief_preflight.go`.

**Behavior rules:**
- Nothing here calls a model, calls `Env.Backends` (no run folder exists yet, and there is no database), or writes a file. Provider probes are the two GETs of `envcheck.ProbeBaseURL` (version and models endpoints), never a completion request.
- repo: path from `planner.ResolveRepo(o.Repo or brief repo)`; a git work tree whose top level is that path (`Env.Git`); `refs/heads/<base_branch>` exists; HEAD is on the base branch (`symbolic-ref --short HEAD`; a detached HEAD fails). `repo head`: with `ExpectHead`, `rev-parse HEAD` equals `rev-parse --verify <rev>^{commit}`. `clean tree`: `envcheck.DirtyOutsideTests(repo, "")` is 0 (paths under `.gophermind/` ignored); skipped with `--resume`. Details name branch, rev, or a count of dirty paths, never file contents. Fix texts: `git -C <repo> status` to read the dirt, `git -C <repo> switch <base>` is NOT offered (wrapper): say `put the repo on branch <base> at <rev>`.
- stale state: fresh run: the run folder `<repo>/.gophermind/<id>` and the scratch folder `<repo>/.gophermind/<id>-scratch` must not exist (only the folders are tested; nothing inside them is read), `branch --list gm/<id>` must be empty, and `<config dir>/runs/<id>.json` must not exist; the Fix is `clear the attempt: run the state paths with --print-state-paths and follow docs/briefv2/project-runbook.md, or pass --resume`. With `--resume`: the run folder must exist and `planner.LookupRun(id)` must succeed with a matching repo, else `nothing to resume`.
- graded (only when `o.Graded`): `ExpectHead` is set, `Resume` is false, and the three stale-state items above are absent (a graded attempt starts from a cleared state; there is no `--resume` rescue). A failure names the first leftover.
- landing: `gitland.ValidateLanding(b.Front.Landing)`.
- sandbox, go toolchain, git, disk space: `sandbox.Required` and `env.SandboxPreflight` as `brief_preflight.go` does; `env.LookPath("go", cfg.Toolchain["PATH"])` with the Fix `add <dir of the go found on PATH> to toolchain.PATH in <settings path>`; `git` found on the process PATH; `envcheck.ModuleCacheFree(cfg.Executor.GoModCache) >= minFreeBytes`.
- settings: `os.Stat(env.SettingsPath())`; missing is `gophermind.yaml not found at <path>` with Fix `create it (see docs/briefv2/project-runbook.md)` (never `settings.Load`, which would write defaults); otherwise `env.LoadSettings`; an error is the detail (the decoder refuses unknown keys, so a settings file with a misspelled planner key such as `clarify_max_call` fails here, not mid-run). privacy: with `RequirePrivate`, `privacy.mode` must be `private_only` and every tier entry's provider must be `settings.Private`; the detail lists provider names.
- providers: `cfg2, probes := env.Probe(ctx, cfg)`: this is `ResolveBaseURLs`, so `base_url_fallbacks` are tried in order and the mini's VPN address `10.8.0.6` answers when the LAN address `192.168.1.35` does not. One `provider <name>` check per probe (`ResolveBaseURLs` walks `settings.Tiers`, so a provider that only the `standard` or `any` chain names is probed too, which node-scope planner stages need under `defaults.route_by_node_tier`): ok with `answered on fallback host <host>` or the plain host; failed with `no base_url answered` and Fix `start the model server (the mini: ssh mini, then check ollama) or fix base_url and base_url_fallbacks for <name> in <settings path>`. Then `env.BuildProviders(cfg2, lookup)` where `lookup` reads the harness scope of the opened vault; its error text (it names a secret, never a value) fails the check `provider <name> key`. `PreflightResult.Config` is `cfg2`; `Probes` are kept for the report.
- binary: with `ExpectBinaryCommit` set, `Version().Commit` must start with it and must not be `none`; Fix `rebuild with scripts/build-dev-binary.sh`. The detail prints version and commit.
- vault passphrase and vault: when the brief declares no secret and no provider has `api_key_secret`, one passing `vault passphrase: not needed`. Otherwise `env.Getenv("GOPHERMIND_VAULT_PASSPHRASE")` must be non-empty (Fix `export GOPHERMIND_VAULT_PASSPHRASE=<passphrase>`; the name only) and `env.OpenVault` must open (a missing file opens empty; a wrong passphrase fails). Opening must not create the file.
- secret `<NAME>`: `Sources(store, b, o.Generate, o.Attended)`; ok with its source, or missing with Fix `gophermind brief vault set <NAME>` (HARNESS scope; the pre-plan variant never reads the run scope).
- database `<NAME>`: for every declared secret whose harness value (or generated source skipped) parses with `net/url` as scheme `postgres` or `postgresql`. `DATABASE_URL` and `TEST_DATABASE_URL` are the two the real brief declares. Host must be loopback (`localhost`, `127.0.0.0/8`, `::1`), else detail `non-loopback host <host:port>: the sandbox denies it` (host and port only, never userinfo or database name) and the same tunnel Fix below. `env.Dial(ctx, "127.0.0.1:<port>")` must succeed; on failure the Fix is the exact tunnel command `ssh -N -L <port>:127.0.0.1:5432 mini` (the URL's port as the local port; run by the human, kept up for the whole attempt; code never starts it). Two such secrets with the same host, port and database name fail as `<A> and <B> name the same database`. One passing informational check `database note` states `database emptiness is not checked`. `loopbackAddr` semantics come from `envcheck.LoopbackAddr`.
- Warnings: `b.UndeclaredSecrets()` becomes `OK: true` checks named `warning`.

**Tests** (real `git` in temp repos, `skipIfNoGit(t)`; `TestMain` unsets `GIT_*`):
- `TestPreflightRepoChecks` (clean repo on `main` passes; each of dirty tracked file, untracked file, other branch checked out, detached HEAD, missing base branch, not a git repo, `--expect-head` mismatch fails; match passes; `.gophermind/` dirt ignored).
- `TestPreflightStaleStateAndBranch` (run folder present fails, naming the folder and not listing its files; scratch folder present fails; branch `gm/<id>` present fails; run record present fails; all absent passes; with `--resume` a missing folder fails and a present one with a run record passes).
- `TestPreflightGraded` (graded with each leftover fails; graded without `ExpectHead` fails; graded clean passes).
- `TestPreflightLanding` (`commit` passes; `pull_request` fails naming the field).
- `TestPreflightToolsAndSettingsNotCreated` (missing settings file: failed check, and the config dir still has no `gophermind.yaml`; `go` not on the PATH; sandbox refused on darwin unless `off`).
- `TestPreflightRequirePrivate` (public entry fails; `privacy.mode: need_to_know` fails; all private passes).
- `TestPreflightProvidersFallback` (base_url a closed port, fallback an `httptest` server: passes with `answered on fallback host`, `Config` carries the fallback URL; both closed fails naming the provider; a missing provider key surfaces the `BuildProviders` error).
- `TestPreflightExpectBinaryCommit` (prefix match, mismatch, `none`).
- `TestPreflightVaultPassphrase` (empty env var fails naming it; wrong passphrase fails; missing file passes and is not created).
- `TestPreflightSecretMatrix` (vault value, `--generate`, both, neither, attended prompt; per-secret lines; the check reads the harness scope and ignores a run-scope value; a canary value never appears in any `Detail` or `Fix`).
- `TestPreflightDatabaseLoopbackAndDistinct` (non-loopback fails, refused port fails, same database twice fails, distinct loopback passes; detail has no password).
- `TestPreflightDatabaseTunnelHint` (a refused `127.0.0.1:55432` URL: the Fix is exactly `ssh -N -L 55432:127.0.0.1:5432 mini`).
- `TestPreflightHumanListNumbered` (three failures print `1.`, `2.`, `3.` each followed by a `Fix:` line, the heading, and the not-a-run-failure sentence last; ok output is one line; no detail contains a path outside the repo and the config dir).
- `TestPreflightMissingListedNoModelCallNothingWritten` (a repo, config dir and `Env` with a counting fake provider: everything missing at once yields all failed items in one `Preflight` call, the provider call count is 0, `Env.Backends` was never called, and a before and after walk of the config dir, the vault directory and the repo shows no new file).

- [ ] **Step 1: Write the failing tests. Step 2: Run and see failures. Step 3: Implement** in the order repo, env, secrets, aggregate and print. **Step 4: Verify and commit.**

```bash
gofmt -l gophermind-lib/briefv2 && go vet ./gophermind-lib/briefv2/projectrun/... && gt -race -timeout 30m ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun/preflight.go gophermind-lib/briefv2/projectrun/preflight_repo.go gophermind-lib/briefv2/projectrun/preflight_env.go gophermind-lib/briefv2/projectrun/preflight_secrets.go gophermind-lib/briefv2/projectrun/preflight_test.go gophermind-lib/briefv2/projectrun/preflight_repo_test.go gophermind-lib/briefv2/projectrun/preflight_env_test.go gophermind-lib/briefv2/projectrun/preflight_secrets_test.go
git commit -m "feat(briefv2): projectrun pre-plan preflight with the numbered list of what a human must provide" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 6 (S): State paths

**Files:** Create `gophermind-lib/briefv2/projectrun/statepaths.go`, `statepaths_test.go`.

**Interfaces:**

```go
type StatePath struct{ Action, Scope, Path string }
// Actions: delete_dir, git_branch_delete, git_reset, delete_file, keep, external
func StatePaths(o Options, b *brief.Brief, env Env) ([]StatePath, error)
func PrintStatePaths(w io.Writer, ps []StatePath)   // "<action>\t<scope>\t<path>" per line
```

**Behavior rules:** exactly the table of spec section 9, in its order, so the orchestrator never guesses and `--print-state-paths` prints every path to remove: `delete_dir` for the run folder `<repo>/.gophermind/<id>` (the whole tree of node files, `*.runtime.json`, `_state/`, `attempts/`, `decisions/`, `UNDERSTANDING.md`: one line, the folder) and for `<repo>/.gophermind/<id>-scratch`; `keep` for `<repo>/.git/info/exclude`; `git_branch_delete` for `gm/<id>` (path column is the branch name); `git_reset` for the repo work tree (path is the repo); `delete_file` for `<config dir>/runs/<id>.json`; `keep` for the vault path, the settings path and the module cache (from `cfg.Executor.GoModCache` when the settings file exists, else the default `~/.gophermind/gomodcache` expanded against the real home); `external` for the databases: path column `declared secrets: <names>; drop and recreate the databases behind the postgres URLs`. Scope column values: `per-project-repo`, `per-project-repo-git`, `per-project-global`, `global`, `external`. There is no line for a database or for ledger rows: the ledger, the blackboard and the event log are files inside the run folder. Paths come from `Env` (`ConfigDir`, `VaultPath`, `SettingsPath`) and honor `GOPHERMIND_CONFIG_DIR` and `GOPHERMIND_VAULT_PATH`. The function reads no vault, calls no `Backends`, dials nothing, runs no git, and creates nothing (the settings file is read only if it exists).

**Tests:** `TestStatePathsListEverything` (every row of spec 9 present with the right action and scope, run id and repo substituted, the config dir honored), `TestStatePathsNoSideEffects` (the config dir, repo and vault path are byte-identical and no file is created; a failing `Dial`, `Backends` and `Git` are never called), `TestStatePathsNoSecrets` (a canary secret value in a vault next to it never appears), `TestStatePathsRepoOverride` (`--repo` replaces the brief's path in the paths), `TestStatePathsHasNoDatabaseRow` (no line names a database, a `rows` clear or a `git_clean` action; the run folder is one `delete_dir` line).

- [ ] **Step 1 to 4** as before; commit `git add gophermind-lib/briefv2/projectrun/statepaths.go gophermind-lib/briefv2/projectrun/statepaths_test.go`, message `feat(briefv2): projectrun state paths for clearing an attempt`.

---

### Task 7 (M): The project report, the printed report, the binary stamp and the progress sink

**Files:** Create `gophermind-lib/briefv2/projectrun/report.go`, `sink.go`, `classes.go`, `report_test.go`, `sink_test.go`, `classes_test.go`.

**Interfaces:**

```go
type BinaryInfo struct{ Path, Version, Commit, Date string }
type Counts struct { // planner warning counts; from the sink and planner.IgnoredDuplicates
	LeafDefaulted, DocDefaulted, LeafNormalized, OutlineIDNormalized, DuplicatesIgnored int
}
type ProjectReport struct {
	RunID       string         `json:"run_id"`
	Title       string         `json:"title"`
	Binary      BinaryInfo     `json:"binary"`        // path, version, commit, date
	Mode        string         `json:"mode"`          // unattended | attended
	Graded      bool           `json:"graded"`
	Resumed     bool           `json:"resumed"`
	Repo        RepoInfo       `json:"repo"`          // path, brief_repo, base_branch, head_at_start
	Preflight   []Check        `json:"preflight"`
	Providers   []ProviderInfo `json:"providers"`     // name, host that answered, fallback bool
	Secrets     []Provisioned  `json:"secrets"`
	Ambiguity   AmbiguityInfo  `json:"ambiguity"`     // brief_setting, effective, clarify_defaulted [{id,question,answer}], by_answered_by {unattended-default, probe, human, accepted}, rounds, clarify_calls, conservative_assumptions (planner.QuestionCounts.MidStageAssumed), milestone_approvals
	Understanding UnderstandingInfo `json:"understanding"` // confirmed_by, hash (planner.ReadUnderstanding)
	Approval    ApprovalInfo   `json:"approval"`      // by, plan_hash, understanding_hash
	Plan        PlanInfo       `json:"plan"`          // functions, waves, warnings, requirements_covered {covered,total}
	Warnings    Counts         `json:"planner_warnings"`
	PlannerWarningLines []string `json:"planner_warning_lines"` // report.PlannerWarnings style lines for the duplicates
	Stages      []StageInfo    `json:"stages"`
	ByNodeClass []ClassStat    `json:"by_node_class"` // classes.go; empty when the executor did not run
	Executor    *report.Report `json:"executor,omitempty"`
	Status      string         `json:"status"`
	StopReason  string         `json:"stop_reason"`
	ExitCode    int            `json:"exit_code"`
	StartedAt, FinishedAt string
}
func (p *ProjectReport) Text() string               // the printed report
func WriteReport(runDir string, p *ProjectReport) error // <run>/_state/project.json, mode 0600, temp file and rename (not a root-level project.json: tree.Store.Load would parse it as a node)

// classes.go: the node-class dimension (spec 11). Built from the blackboard rows, planner.ReadClasses and the ledger;
// the executor's report.json is not changed.
type ClassStat struct {
	Class            string  `json:"class"` // pure, validation, handler, client, storage, concurrency, wiring, other, unclassified
	Leaves           int     `json:"leaves"`
	Verified         int     `json:"verified"`
	Failed           int     `json:"failed"`
	Escalated        int     `json:"escalated"`
	NotRun           int     `json:"not_run"` // leaves - verified - failed - escalated (blocked, ready, never started)
	Attempts         int     `json:"attempts"` // verdict != error, counted as report.Build counts them
	Passes           int     `json:"passes"`
	FirstTryWins     int     `json:"first_try_wins"` // a leaf whose first counted attempt passed
	FirstPassRate    float64 `json:"first_pass_rate"` // FirstTryWins / Leaves, four places; 0 with no leaves
	Calls            int     `json:"calls"`            // implement calls of the class (ledger Call.NodeClass)
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
}
func ClassStats(rows []blackboard.Row, classes map[string]string, calls []ledger.Call) []ClassStat // fixed class order

type Sink struct{ /* implements events.Sink */ }
func NewSink(w io.Writer) *Sink
func (s *Sink) Counts() Counts                      // warning counts summed from warning events
```

**Behavior rules:**
- `Text()` layout, in order: the binary line (`gophermind <version> (commit <commit>, built <date>)`, then `binary: <path>`), `project: <id> <title>`, `mode: unattended|attended, graded: yes|no, resumed: yes|no`, `repo: <path> (brief repo: <brief_repo>)`, `model server: <provider> answered on <host>` per provider (host name only), `preflight: ok (<N> checks)`, `secrets: NAME source; ...` (names and sources only), the ambiguity line (`on_ambiguity=<brief> overridden by unattended policy: <k> clarify question(s) answered by their recommendations (<ids>) in <r> round(s), <p> by the fact probe; <m> conservative assumption(s); text in <run>/_state/project.json`), the understanding line (`understanding: confirmed by unattended, hash <first 12>` or the gate's `By`), the milestone line (spec 8.3 wording), `planner warnings: duplicates ignored <d>, leaf_defaulted <n>, doc_defaulted <n>, leaf_normalized <n>, outline_id_normalized <n>`, `approval: <by>, plan <first 12 of hash>, understanding <first 12 of hash>`, `plan: <f> functions in <w> wave(s)`, then one `node class <class>: <n> leaves, <v> verified, <w> first try, <a> attempts` line per entry of `ByNodeClass` (a single line `node classes: executor did not run` when it is empty), then when the executor ran its `Summary()` block unchanged (its last two lines are the proof lines), otherwise `stopped: <status> <stop_reason>` followed by the two proof lines `Requirements covered: <C> of <N>` (from `coverage.json` when it exists, else `0 of <N>` with N from the brief's requirements) and `Acceptance passed: 0 of <A>`. The two proof lines are always the last two lines of the text. When `Graded` and the executor report says `Resumed` true, a line `graded: INVALID (the run resumed)` precedes the executor block.
- `Text()` never includes a question, an answer or any model-written text; `_state/project.json` does (`clarify_defaulted`). Clarify defaults appear on stdout as ids only.
- `Sink` prints `<stage>: started` and `<stage>: done` for planner stage events, `warning: <message>` for warnings and ledger errors, `coverage gap, <message>`, and for every other kind `<node>: <kind> <message>` when there is a message (executor messages are ids and counts by the executor spec). It never prints `Event.Call`. `Counts()` sums, over every `KindWarning` event whose message starts with `leaf_defaulted: `, `doc_defaulted: `, `leaf_normalized: ` or `outline_id_normalized: `, the integer after the prefix (the count of that emission); read `planner/schemafix.go`, `decompose_fix.go` and `idnorm.go` for the exact prefixes. `DuplicatesIgnored` is filled by `Run` from `planner.IgnoredDuplicates`, not by the sink.
- `ClassStats` rules: one entry per class that occurs among the rows, in the order `pure, validation, handler, client, storage, concurrency, wiring, other, unclassified` (a row whose id is not in `classes`, or whose class is not in that list, is `unclassified`); `Attempts`, `Passes` and `FirstTryWins` use the loop of `report.Build` (attempts sorted by revision then order, `VerdictError` skipped, the first counted attempt of a row is the first-try one); `Calls`, `PromptTokens` and `CompletionTokens` sum the ledger calls whose `NodeClass` equals the class (the caller passes `ledger.List(runID, Filter{TaskType: "implement"})`); the sum of `Leaves` over the entries equals `len(rows)`. `Run` (Task 8) fills `ByNodeClass` from `Board.List(ctx, id, blackboard.Filter{})`, `planner.ReadClasses(runDir)` and the ledger, only when the executor ran. Ids, counts and class names only.
- The binary path is `Env.Executable()`; version, commit and date are `Env.Version()`.
- The executor's report is embedded as returned by `executor.Run` or read by `report.Read(runDir)` by the caller (Task 8); `Text()` only formats.

**Tests:** `TestReportCarriesBinaryVersion` (version, commit, date and path appear in `Text()` and in the JSON), `TestPrintedReportHasNoModelText` (a canary question and answer are in the JSON and absent from `Text()`), `TestPrintedReportEndsWithProofLines` (executor present: last two lines are the proof lines in order), `TestProofLinesWhenExecutorDidNotRun` (planner stopped: `Requirements covered: 0 of 7` and `Acceptance passed: 0 of 2`, still last), `TestMilestoneLine` (declared true gives the exact sentence; false gives none), `TestReportJSONRoundTrip` (mode 0600, the file is `_state/project.json`, `tree.Store.Load` of the run folder still succeeds with it present, parses back equal, secrets hold names and sources only), `TestClassStatsAggregatesRowsClassesAndCalls` (a table of rows with attempts across revisions and a `VerdictError`, a class map with one unknown id and one off-list class, and ledger calls of two classes: per-class leaves, verdict counts, attempts, passes, first-try wins, rate, calls and tokens; leaves sum to the rows; the order is fixed), `TestReportCarriesNodeClassTable` (the table is in the JSON and in `Text()`; empty gives the `executor did not run` line), `TestPrintedReportNodeClassLines` (one line per class, counts only, no node id), `TestReportCarriesUnderstanding` (confirmed_by and the hash prefix in the text and the JSON), `TestReportCarriesPlannerWarnings` (counts and lines in JSON and the one printed line), `TestReportRecordsAnsweringHost` (provider name and host, never a full URL), `TestSinkLines` (table of event kinds; a `Call` payload is never printed), `TestSinkCountsPlannerWarnings` (two `leaf_defaulted` events of 2 and 3 give 5; each of the four prefixes; an unrelated warning counts nothing), `TestGradedInvalidLine`.

- [ ] **Step 1 to 4** as before; commit with `git add gophermind-lib/briefv2/projectrun/report.go gophermind-lib/briefv2/projectrun/sink.go gophermind-lib/briefv2/projectrun/classes.go gophermind-lib/briefv2/projectrun/report_test.go gophermind-lib/briefv2/projectrun/sink_test.go gophermind-lib/briefv2/projectrun/classes_test.go`, message `feat(briefv2): projectrun report with the node-class table, printed summary and progress sink`.

---

### Task 8 (L): `projectrun.Run`

**Files:** Create `gophermind-lib/briefv2/projectrun/run.go`, `run_test.go`.

**Interface:** `func Run(ctx context.Context, o Options, env Env) Result` (spec 4 and 5). Add one unexported `type deps struct { cfg *settings.Config; vlt *vault.Vault; rt *router.Router; board blackboard.Blackboard; led ledger.Ledger }` (no database handle; nothing to close) and `func (e Env) open(ctx context.Context, o Options, pre PreflightResult, b *brief.Brief, sink events.Sink) (*deps, error)`.

**Behavior rules, in order:**
1. `os.ReadFile(o.BriefPath)`, `brief.Parse`. `*brief.InvalidError`: print `invalid brief: ...` to `o.Err`, `Result{ExitInvalid, "invalid_brief", "invalid_brief"}`. Other read or parse errors: `ExitFailed`.
2. `o.PrintStatePaths`: `StatePaths` then `PrintStatePaths` to `o.Out`, exit 0. Nothing else runs.
3. `Preflight`; `Failed` non-empty: `PrintPreflight` to `o.Err`, return `Result{ExitPreflight, "preflight_failed", "preflight"}`; no project report file, no state backend, no run folder. `o.PreflightOnly`: print `preflight: ok` to `o.Out`, exit 0.
4. Start a `ProjectReport` (binary from `env.Version` and `env.Executable`, mode, `graded`, repo info with `head_at_start` from `Env.Git rev-parse HEAD`, preflight checks, providers from `pre.Probes`, `started_at`). From here on every return path writes `_state/project.json` (once the run folder exists) and prints `Text()` to `o.Out`.
5. `env.open` (it uses `pre.Config`, the config with the base URL that answered, never the settings file's own URLs): reads the passphrase from `Getenv("GOPHERMIND_VAULT_PASSPHRASE")` only (never prompts), builds providers with a harness-scope lookup over the opened vault, takes `board, led := env.Backends()` (`blackboard.FS` and `ledger.FS` over `planner.LookupRun`; they resolve the run folder lazily, after Load wrote the run record), builds `router.New(cfg, providers, led, sink, router.WithAllowPublic(false))`, opens the vault only when the brief declares secrets or a provider needs a key. Then `ProvisionGenerated(store, b, o.Generate, o.Resume, env.Rand)`; record sources.
6. Planner: `planner.New(planner.Deps{Caller: rt, Gate: <newUnattendedGate(runDir lookup by b.Front.ID)> or human.NewTerminal(o.In, o.Err), Sink, Board, Settings: cfg, OpenSecrets: vault, PromptSecret: (attended only), LedgerErrors: rt.LedgerErrors})` (no reset hook exists; a fresh run starts from no run folder) and `Run(ctx, planner.Options{BriefPath or RunID (resume), Repo: o.Repo, Unattended: !o.Attended})`. `context.Canceled` or `DeadlineExceeded`: `interrupted`, exit 5, `StopReason: interrupted`, and the text says `resume with --resume`. Any other error: `failed`, exit 1, `plan:<stage>` taken from the error text prefix `planner: <stage>:` where `<stage>` is one of `clarify`, `confirm`, `contract`, `decompose`, `enrich`, `coverage`, `approve`, `testwriter` (else `plan:unknown`; a Load error has no stage prefix and is `plan:load`). `Waiting`: `failed`, `plan:waiting`. After success read `planner.ReadAnswers`, `ReadApproval`, `ReadUnderstanding`, `ReadQuestionCounts`, `ReadCoverage`, `ReadStatus` and `planner.IgnoredDuplicates` into the report (`clarify_defaulted` are the `Assumed` answers with `Stage == "clarify"`; `by_answered_by`, `rounds` and `clarify_calls` come from `QuestionCounts`; `conservative_assumptions` is `QuestionCounts.MidStageAssumed`, not a parse of the plan markdown); `Warnings` come from `sink.Counts()` plus the duplicates total.
7. Executor: `env.RunExecutor(ctx, executor.Options{RunDir: rec.RunDir, Repo: <resolved repo>, Caller: rt, Board, Ledger, Gate: the same unattended gate (or the terminal gate when attended), Sink, Settings: cfg, Secrets: vault (nil when none declared), LedgerErrors: rt.LedgerErrors, EnvNotes: envcheck.EnvNotes(pre.Probes)})`. A non-nil error is a harness fault: status `harness_fault`, exit 7, message printed as `error: <text>` (`*brief.InvalidError` is exit 2 as `brief run` does). Otherwise embed the `Report`, build `ByNodeClass` (`ClassStats(rows, classes, calls)` with `rows` from `board.List(ctx, id, blackboard.Filter{})`, `classes` from `planner.ReadClasses(runDir)` and `calls` from `led.List(ctx, id, ledger.Filter{TaskType: "implement"})`; a read error leaves the table empty and adds a `warning` line, never fails the run) and map with `ExitCode(report.Status, report.StopReason)`.
8. `Resumed` in the report is `o.Resume || executor Report.Resumed`; in `--graded` it is expected `false` and the report prints `resumed: no`; if the executor says `Resumed` true the graded line of Task 7 is printed. `finished_at`, `status`, `stop_reason`, `exit_code` set; `WriteReport`; print; return.
9. No-restart rule: the healthy path is one `planner.Run` with `BriefPath` set (never `RunID`) and one `executor.Run`. Nothing on that path may print or require `--resume`; only the interrupted path prints it.

**Tests** (an `Env` of fakes; the planner runs over the planner `greeter` fixtures, the executor is replaced through `Env.RunExecutor`; read `planner/harness_test.go` for the fixture setup):
- `TestRunInvalidBriefExit2`; `TestRunPrintStatePathsOnly` (no preflight, no backend).
- `TestRunPreflightFailureStopsEverything`: `Backends`, `RunExecutor` and the provider are never called; exit 6; no `_state/project.json`; the `Err` block lists the items numbered.
- `TestPreflightFailureExit6IsNotARun`: the failed preflight leaves no run folder, no `runs/<id>.json`, and no file at all under the repo or the config dir (a before and after walk).
- `TestRunPlanStageFailureSkipsExecutor`: `planner/testdata/greeter-stuck` placed first (two fill rounds that leave acceptance bullet A3 uncovered, as `planner.TestCoverageStopsWhenAGapIsLeft` uses it), found with `runtime.Caller` from `projectrun` as `../planner/testdata/greeter-stuck`; exit 1, `StopReason` `plan:coverage`, the executor fake not called, `_state/project.json` exists, `by_node_class` is empty and the text says `node classes: executor did not run`, the printed text ends with the two proof lines (`0 of N` acceptance).
- `TestRunCallsExecutorWithPlanArtifacts`: the fake executor records its `Options`: `RunDir` is the planner's run folder, `Repo` is the override, `Gate` is the unattended gate (`Escalate` returns stop with `AnsweredByUnattended`), `Secrets` is non-nil only when the brief declares one, `Board`, `Ledger` and `Caller` are the same instances the planner used (the instances `Env.Backends` returned), `EnvNotes` names the answering host.
- `TestRunMapsExecutorStatusesToExitCodes`: table verified 0, failed 1, escalated 4, interrupted 5; a returned error gives 7 and `harness_fault`.
- `TestRunInterruptedContextExit5`: the context is cancelled during the planner (fake provider blocks): exit 5, `interrupted`, the printed text says `resume with --resume`.
- `TestEveryStopConditionHasReasonAndCode`: drives the rows of spec 8.4 that can be simulated (preflight, invalid brief, clarify without recommendations, confirm with an open question, coverage gap, executor statuses, harness fault, interrupted) and asserts each has a non-empty `StopReason`, the listed `Status` and exit code.
- `TestRunUnattendedPlansOverHaltWithoutAskingTheGate`: the planner greeter brief (`on_ambiguity: halt`) with the real planner and a fake executor; exit 0; `ReadApproval` gives `ApprovedBy == "unattended"` and a non-empty `UnderstandingHash` equal to `ReadUnderstanding(...).Hash`, whose `ConfirmedBy` is `unattended`; the unattended gate's `Ask` and `Confirm` were never called (a wrapper counts them); `_state/project.json` has `ambiguity.effective` naming the override and the printed text has the understanding line.
- `TestRunAttendedUsesTerminalGate`: with `Attended` the planner's `Deps.Gate` is the terminal gate (a scripted `In` answering `accept`, `y` for the understanding and `y` for the plan completes the planner; `Confirm` was asked once) and unattended runs never read `o.In` (a closed pipe as `In` stays unread).
- `TestRunResumeSkipsFinishedPlannerStages`: a first `Run` stopped with a fake executor returning `interrupted`; a second with `Resume: true` makes zero planner calls, calls the executor, and the report has `resumed: true`.
- `TestRunGradedReportsResumedNo`: a graded `Run` over fakes prints `graded: yes` and `resumed: no`, and the healthy path made exactly one planner `Run` with an empty `RunID`.
- `TestGradedRefusesSecondInvocation`: after a first graded run a second graded `Run` without clearing exits 6 naming `stale state`.
- `TestRunReportsAnsweringHostAndCounts`: the fake probe answers on a fallback host; the printed text and `_state/project.json` name it; a planner warning event is counted in `planner_warnings`.

- [ ] **Step 1: Write the failing tests. Step 2: Run** `gt -race -timeout 30m ./gophermind-lib/briefv2/projectrun/ -run 'TestRun|TestEveryStop|TestPreflightFailure|TestGraded'`. **Step 3: Implement** `run.go`. **Step 4: Verify and commit** (`git add gophermind-lib/briefv2/projectrun/run.go gophermind-lib/briefv2/projectrun/run_test.go`; message `feat(briefv2): projectrun.Run plans and builds a brief in one process`).

---

### Task 9 (S): `gophermind project` (the CLI form)

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
                      [--expect-head <rev>] [--graded] [--expect-binary-commit <sha>] [--resume] [--attended]
                      [--preflight-only] [--print-state-paths]
                                plan and build a v2 brief in one run (same as /project in the TUI)
```

**Tests:** `TestProjectCLIUsage` (no args and a bad flag print the usage and exit 1), `TestProjectCLIExitCodes` (table over an `Env` of fakes: verified 0, failed 1, invalid brief 2, escalated 4, interrupted 5, preflight 6, harness fault 7; stdout ends with the proof lines for 0, 1, 4, 5), `TestProjectCLIPrintStatePaths` (tab separated lines, exit 0, no provider call), `TestProjectCLIPreflightOnly` (exit 6 lists missing items on stderr, nothing on stdout; exit 0 prints `preflight: ok`), `TestProjectCLINeverReadsStdinUnattended` (a pipe closed by the test is not read), `TestProjectCLIGradedFlags` (`--expect-head` alone sets graded; `--graded --resume` exits 1 with the message).

- [ ] **Step 1: Write the failing tests. Step 2: Run** `gt -race -timeout 30m ./cmd/gophermind/ -run TestProjectCLI`. **Step 3: Implement. Step 4: Verify and commit:**

```bash
gofmt -l cmd gophermind-lib/briefv2 && go vet ./cmd/... ./gophermind-lib/briefv2/... && gt -race -timeout 30m ./cmd/gophermind/... && go build ./cmd/... ./gophermind-lib/...
git add cmd/gophermind/project.go cmd/gophermind/project_test.go cmd/gophermind/main.go
git commit -m "feat(briefv2): gophermind project runs the v2 planner and executor in one command" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 10 (S): Rename the v1 TUI commands to `/plan-v1` and `/plan-v1-execute`

A mechanical change with one new test and one new message. It is its own task so the diff is reviewable as a rename.

**Files (modify):** `grep -rln '/project' gophermind-lib/tui` is the truth. On 2026-10-08 it lists, non-test: `commands_registry.go`, `update.go`, `project.go`, `approve.go`, `execute.go`, `questions.go`, `commands.go`, `model.go`, `run.go`, `config.go`, `view.go`; test files: `project_flow_test.go`, `project_fixwave_test.go`, `project_test.go`, `execute_test.go`, `completion_providers_test.go`, `completion_wire_test.go`, `config_test.go`. (There is no `question_round.go`; `attention_test.go`, `phase_test.go`, `fixwave_test.go`, `e2e_test.go` and `update_test.go` hold no such literal.) Rename only files that really contain it. Comments about `/project-execute` outside `gophermind-lib/tui` (`agent/config.go`, `setup/setup.go`, `cmd/gophermind/main.go`) are left alone: the acceptance grep below does not cover them.

**Behavior rules:**
- In the files above replace the literal `/project-execute` with `/plan-v1-execute` FIRST, then the literal `/project` (when not followed by `-` or a word character) with `/plan-v1`, in string literals and comments alike. Use `perl -pi -e 's{/project-execute\b}{/plan-v1-execute}g; s{/project(?![-\w])}{/plan-v1}g'` on exactly those files, never on `project_v2.go` (Task 11), `docs/`, `phaseflow/assets`, or `gophermind-lib/plantree`.
- Routing in `update.go` tests `/plan-v1` and `/plan-v1-execute` for the v1 handlers. The registry entries become `/plan-v1 <name> <brief>` ("v1 planner: plan a brief into phases, tasks and steps (deprecated, use /project)") and `/plan-v1-execute`. Function and type names are unchanged (surgical).
- When the v1 flow starts (`startProject`) and when `/plan-v1-execute` starts, the first transcript line is `deprecated: /plan-v1 and /plan-v1-execute are the v1 planner and will be removed after the AI Venture Studio release; /project runs the v2 planner and executor`.
- Acceptance grep after the change: `grep -rn '"/project\|/project ' gophermind-lib/tui cmd --include='*.go'` returns nothing except the new registry entry for v2 `/project` (added in Task 11) and Task 9's usage text.

**Tests** (new file `gophermind-lib/tui/plan_v1_test.go`): `TestPlanV1CommandsRenamed` (the registry lists `/plan-v1` and `/plan-v1-execute`; `helpLine()` contains both; routing a line starting `/plan-v1 name brief.md` reaches `handleProjectCommand`; `/plan-v1-execute` reaches the execute handler), `TestPlanV1PrintsDeprecation` (the first appended line after starting either command is the deprecation sentence). The whole existing `tui` suite passes with only the literals changed.

- [ ] **Step 1: Write the two new tests (they fail). Step 2: Run the perl replacement; run `git diff --stat` and read the diff of the non-test files in full; run the acceptance grep. Step 3: Add the deprecation line. Step 4: Verify and commit:**

```bash
gofmt -l gophermind-lib/tui && go vet ./gophermind-lib/tui/... && gt -race -timeout 30m ./gophermind-lib/tui/... && go build ./cmd/... ./gophermind-lib/...
git diff --name-only -- gophermind-lib/tui   # must be a subset of the Files list above; anything else is unrelated work: leave it unstaged
git add gophermind-lib/tui/commands_registry.go gophermind-lib/tui/update.go gophermind-lib/tui/project.go gophermind-lib/tui/approve.go gophermind-lib/tui/execute.go gophermind-lib/tui/questions.go gophermind-lib/tui/commands.go gophermind-lib/tui/model.go gophermind-lib/tui/run.go gophermind-lib/tui/config.go gophermind-lib/tui/view.go gophermind-lib/tui/project_flow_test.go gophermind-lib/tui/project_fixwave_test.go gophermind-lib/tui/project_test.go gophermind-lib/tui/execute_test.go gophermind-lib/tui/completion_providers_test.go gophermind-lib/tui/completion_wire_test.go gophermind-lib/tui/config_test.go gophermind-lib/tui/plan_v1_test.go
git diff --cached --stat   # only the files changed by the rename and the two new tests
git commit -m "refactor(tui): rename the v1 planner commands to /plan-v1 and /plan-v1-execute" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

(Stage only paths that `git diff --name-only` showed as changed by the rename. If a listed file was not changed, drop it from the `git add` line; if the worktree holds unrelated changes under `gophermind-lib/tui`, they stay unstaged.)

---

### Task 11 (M): `/project` in the TUI (the slash form)

**Files:** Create `gophermind-lib/tui/project_v2.go`, `project_v2_test.go`. Modify `commands_registry.go` (new `/project` entry), `update.go` (route `/project` to `handleProjectV2Command`, after the `/plan-v1` routes), `model.go` (one field: `projV2 bool`; reuse `m.cancel`). The TUI package imports `gophermind-lib/briefv2/projectrun`; it must not import `main` (that is why Task 1 exists).

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

- [ ] **Step 1: Write the failing tests. Step 2: Run** `gt -race -timeout 30m ./gophermind-lib/tui/ -run SlashProject`. **Step 3: Implement. Step 4: Verify and commit:**

```bash
gofmt -l gophermind-lib/tui && go vet ./gophermind-lib/tui/... && gt -race -timeout 30m ./gophermind-lib/tui/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/tui/project_v2.go gophermind-lib/tui/project_v2_test.go gophermind-lib/tui/commands_registry.go gophermind-lib/tui/update.go gophermind-lib/tui/model.go
git commit -m "feat(tui): /project plans and builds a v2 brief through projectrun" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 12 (L): End to end on the greeter, clear and rerun

Depends only on Tasks 1 to 8 (not on the CLI, the TUI, the rename or the dev binary). No new production code; a failing assertion caused by an earlier task is fixed in that task's file in its own `fix(briefv2): ...` commit with a test added there first.

**Files:** Create `gophermind-lib/briefv2/projectrun/testhelp_test.go`, `e2e_test.go`.

**Rig (`testhelp_test.go`): its own small rig.** `projectrun` tests cannot import `executor/rig_test.go`. The rig has a `TestMain` that unsets every `GIT_*` variable, makes one shared `GOCACHE` and one `GOPHERMIND_CONFIG_DIR` under `os.TempDir()` and removes them at exit (copy the pattern of `executor/main_test.go`, including its removal guard). Helpers: `fixtureDir(t)` resolves `executor/testdata/greeter` with `runtime.Caller`. `newProjectRig(t, opts...)` copies the fixture (`repo/`, `brief.md`, `planner/`, `impl/`) into `t.TempDir()`, replaces `REPO_DIR` in the brief, edits the brief text to `on_ambiguity: halt`, `milestone_approvals: true`, a second declared secret `GREETER_SALT`, and writes a variant `clarify.txt` with two questions that carry `recommended` answers, the second with `"depends_on": ["q1"]` so a second round runs (the variant folder is placed first in `FixtureProvider`; the fixture's own `clarify.more.txt`, `[]`, answers the follow-up call, and its `enrich*.txt` replies already exist); makes the target repo with `gitenv.Command` and the PATH git (`init -b main`, `go.mod`, `.gitignore`, seed commit, tag `baseline`, `commit.gpgsign false`, `GIT_CONFIG_GLOBAL=/dev/null`); sets `GOPHERMIND_VAULT_PATH`, `GOPHERMIND_VAULT_PASSPHRASE`; seeds the vault's harness scope with `GREETER_TOKEN = CANARY-SECRET-VALUE` using the low work factor; returns an `Env` of fakes (real git through `Env.Git`, real executor through `RunExecutor: executor.Run`, `SandboxPreflight` real on darwin and `executor.sandbox: off` elsewhere with a log line, `Probe` answering without a network, `Dial` real over loopback, `Version` fixed to `dev+abc1234`/`abc1234`, `Executable` a fixed path). `comboProvider(t, dir)` is one `provider.Fake` whose function sends planner requests (`planner.StageOf(req) != ""`) to `planner.FixtureProvider(<override>, <fixture>/planner)` and executor requests (system message prefix `packer.SystemPrefix`) to the next file `<fixture>/impl/good.<node>.txt` (or the scripted `bad.<node>.<n>.txt` ones for the failure tests), counting requests per stage. The settings are `settings.Default()` edited to one private provider `a` with model `m1`, `executor.sandbox` as above and `toolchain.PATH` holding the `go` the test run uses (copy `testToolchainPATH`'s idea, not the file).

**Tests (`e2e_test.go`):**
- `TestProjectE2EGreeter`: one `Run` call, `Options{BriefPath, Repo, Generate: {"GREETER_SALT": "hex32"}, ExpectHead: "baseline", ExpectBinaryCommit: "abc1234"}` (so graded), `In` nil. Asserts in order: (1) `ExitCode == 0`, `Status == "verified"`, exactly one call of the real executor (a counting wrapper on `RunExecutor`) and it happened after the last planner request; (2) `approval.json` has `approved_by: unattended`, `plan_hash` equal to the hash from `planner.RenderPlan(runDir)` (a subtest named `TestUnattendedApproveBindsHash`) and an `understanding_hash` equal to `planner.ReadUnderstanding(runDir).Hash`, whose `ConfirmedBy` is `unattended`; `UNDERSTANDING.md` exists and holds no decision block; (3) `answers.json` has two `Assumed` answers equal to the fixture's recommendations, `_state/project.json` `clarify_defaulted` has both and `ambiguity.rounds` is 2, the printed text lists ids `q1, q2` and contains neither question text; (4) `_state/project.json`: `mode: unattended`, `graded: true`, `resumed: false`, secrets `GREETER_TOKEN` source `vault` and `GREETER_SALT` source `generated:hex32`, `binary.commit: abc1234`, `binary.path` set, the answering provider host, the `planner_warnings` object present, `milestone_approvals` line present, `executor.status: verified`, `understanding.confirmed_by: unattended`, and a non-empty `by_node_class` whose `leaves` sum to 5 and whose `verified` sum to 5; (5) stdout's last two lines are `Requirements covered: 7 of 7` and `Acceptance passed: 2 of 2`, and it contains `resumed: no`; (6) `main` fast-forwarded and branch `gm/<id>` exists; (7) no `QUESTIONS.md`, no `APPROVAL.md` in the run folder, and no database anywhere: a walk of the run folder, the repo and the config dir finds no `*.db`, `*.db-wal` or `*.sqlite*` file, while `_state/calls.jsonl`, `_state/events.jsonl` and a `*.runtime.json` beside each leaf exist.
- `TestHealthyPathNeverResumes`: on the same run, the planner was started once with an empty `RunID`, the executor report `resumed` is false, and neither stdout nor stderr contains `--resume`.
- `TestNoSecretValueAnywhere`: `CANARY-SECRET-VALUE` and the generated salt are absent from stdout, stderr, `_state/project.json`, `report.json`, every file under the run folder (including `_state/calls.jsonl`, `_state/events.jsonl`, every `*.runtime.json`, `attempts/`, `decisions/` and `_state/replies/`), and `git log -p --all` of the target repo.
- `TestProjectPlanFailureExit1`: a stuck-coverage variant folder that the test writes (the executor greeter's `planner/coverage.txt` with the root test of acceptance bullet A2 removed and a `coverage_fill.txt` of `{"map": [], "root_tests": []}`, so both fill rounds leave A2 uncovered; the construction of `planner/testdata/greeter-stuck`, see `planner.TestCoverageStopsWhenAGapIsLeft`): exit 1, `stop_reason` `plan:coverage`, executor never called, printed text ends with the proof lines, `_state/project.json` exists.
- `TestProjectEscalationExit4`: `fn-hello` always wrong (the `bad.fn-hello.*` replies): exit 4, `escalated`, printed text names the leaf.
- `TestProjectInterruptedExit5`: the context cancelled while `fn-hello` is being implemented: exit 5, the report says resume with `--resume`.
- `TestProjectRefusesStaleStateWithoutResume`: run once (verified), run again without clearing: exit 6, the `stale state` check named, no provider call; with only the branch deleted the run folder still blocks; with `--resume` (not graded) and a finished run the preflight passes.
- `TestResumeFlagContinuesAndIsRecorded`: first run (not graded) ended `interrupted` by the context; second run with `Resume: true` makes no planner request for finished stages, builds the rest, exit 0, `_state/project.json` `resumed: true`, `report.json` `resumed: true`.
- `TestClearedStateRerunsClean`: after a verified run, perform the clear as spec 9 and `--print-state-paths` list it, with `gitenv.Command` and the PATH git only: `git symbolic-ref HEAD refs/heads/main` (no checkout, no switch), `git branch -D gm/<id>`, `git reset --hard baseline`, `git clean -fdx -e .remember`, then the guarded delete of `runs/<id>.json` inside the test's config dir. Step 0 first archives the run folder (a Go copy of the tree into a second temp dir, or `tar`), and the test asserts the archive holds `_state/calls.jsonl` and `report.json`; after the clear the test asserts the run folder and the scratch folder no longer exist. First probe the wrapper in a throwaway temp repo: if `reset --hard`, `clean` or `branch -D` is refused, `t.Skip` with `the git wrapper blocks the clear commands; the orchestrator must test them in the target repo (spec 9)`. Then `Preflight` passes (`--expect-head baseline`); a second full `Run` is verified again with the same commit tree as the first (`git rev-parse main^{tree}` equal) and the second run's `_state/calls.jsonl` starts at id 1 and holds exactly the second run's call count (the first run's folder was removed, so nothing carried over; no reset hook is involved). A sub-test skips the branch delete and asserts exit 6 naming `gm/<id>`.
- `TestPreflightMissingItemsEndToEnd`: an `Env` where the vault lacks `GREETER_TOKEN` and the passphrase env var is empty: exit 6, both items in one numbered listing, the fake provider's call count is 0.

- [ ] **Step 1: Write the helpers and the failing tests. Step 2: Run** `gt -race -timeout 30m -count=1 ./gophermind-lib/briefv2/projectrun/ -run 'E2E|Canary|Stale|Resume|Cleared|EndToEnd|Healthy'` and see each assertion fail for the right reason. Do not loosen an assertion; if an assertion contradicts the spec, fix the assertion and say why in the commit message. **Step 3: Fix defects** in the responsible task's files (separate `fix(briefv2): ...` commits). **Step 4: Timing:** each end-to-end test finishes in under 3 minutes with `-count=1` (find the slow step; do not raise a timeout). **Step 5: Whole-tree check (this is the only place the executor package runs, about 11 minutes):** `gt -race -timeout 30m ./gophermind-lib/briefv2/... ./gophermind-lib/tui/... ./cmd/...`. **Step 6: Verify and commit:**

```bash
gofmt -l gophermind-lib/briefv2 cmd && go vet ./gophermind-lib/briefv2/... ./cmd/... && gt -race -timeout 30m -count=1 ./gophermind-lib/briefv2/projectrun/... && go build ./cmd/... ./gophermind-lib/...
git add gophermind-lib/briefv2/projectrun/testhelp_test.go gophermind-lib/briefv2/projectrun/e2e_test.go
git commit -m "test(briefv2): /project end to end on the greeter, unattended policy, clear and rerun" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 13 (S): The dev binary guard script

The Makefile's `rebuild-all` already stamps `Version=dev+<sha>`, `Commit` and `Date`, and `gophermind version` (main.go) prints them. `rebuild-all` also runs `gofmt -w`, commits, and deploys the desktop app, so it is not reused. This task is only a guard around the same ldflags. The Makefile is not modified.

**Files:** Create `scripts/build-dev-binary.sh`, `cmd/gophermind/devbinary_test.go`.

**Behavior rules:** `scripts/build-dev-binary.sh [--dry-run]` with env overrides `GM_REPO` (default: the script's repo root) and `GM_DEV_BIN` (default `$HOME/.gophermind/bin/gophermind-dev`). The script defines `git() { env -u GIT_DIR -u GIT_INDEX_FILE -u GIT_WORK_TREE git "$@"; }` and uses only the git on `PATH` (no `/usr/bin/git`, no checkout, no stash, no worktree). Steps: `set -euo pipefail`; refuse unless the worktree is clean (`git -C "$GM_REPO" status --porcelain` empty: `refusing to build: the tree is dirty`); refuse unless HEAD is pushed (`git -C "$GM_REPO" branch -r --contains HEAD` non-empty: `refusing to build: HEAD is not pushed`); `sha=$(git rev-parse --short HEAD)`; `date=$(date -u +%Y-%m-%dT%H:%M:%SZ)`; the same ldflags as the Makefile: `-X gophermind/gophermind-lib/version.Version=dev+$sha -X gophermind/gophermind-lib/version.Commit=$sha -X gophermind/gophermind-lib/version.Date=$date`; `--dry-run` prints the ldflags and the output path and exits 0; otherwise `mkdir -p "$(dirname "$GM_DEV_BIN")"`, `go build -ldflags "$ldflags" -o "$GM_DEV_BIN" ./cmd/gophermind`, then `"$GM_DEV_BIN" version` and a check that the printed commit equals `$sha` (else exit 1). It prints the binary path, version and commit (the project report records the same three, Task 7). It never touches `/opt/homebrew/bin` and deletes nothing.

**Tests:** `TestDevBinaryScriptRefusesDirtyTree` (a temp git repo with an uncommitted file: non-zero exit and the message), `TestDevBinaryScriptRefusesUnpushedCommit` (a clean repo with no remote containing HEAD), `TestDevBinaryScriptPrintsLdflags` (`--dry-run` in a clean repo with a bare remote the test pushed to: output has `Version=dev+<sha>`, the path under `GM_DEV_BIN`, exit 0). The tests skip when `bash` or `git` is missing, run git through `gitenv.Command`, and unset `GIT_*` for the script's environment.

- [ ] **Step 1: Write the failing tests. Step 2: Run them. Step 3: Write the script; `chmod +x`. Step 4: Verify and commit:**

```bash
gofmt -l cmd && go vet ./cmd/... && gt -race -timeout 30m ./cmd/gophermind/ -run DevBinary && bash -n scripts/build-dev-binary.sh
git add scripts/build-dev-binary.sh cmd/gophermind/devbinary_test.go
git commit -m "build: guard script for the stamped dev binary used by graded /project runs" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

---

### Task 14 (S): Documentation, the runbook, and the rehearsal

Depends on Tasks 9 and 13. **Files:** Modify `docs/briefv2/README.md`, `CHANGELOG.md`. Create `docs/briefv2/project-runbook.md`. The README is prose without em dashes; keep that.

- [ ] **Step 1: README.** Add a "One command: /project" section: what it does (section 5 of the spec), the two forms, flags (including `--graded`), exit codes (0, 1, 2, 3, 4, 5, 6, 7), the unattended policy in six sentences (recommendations taken in every Clarify round, the understanding confirmed by rule with `confirmed_by: unattended` and its hash in `approval.json`, generated secrets, approval by hash, milestone line, what stops a run), the node-class table in the report, and that the v1 planner is `/plan-v1` until it is removed after the AI Venture Studio release. Change the intro sentence so `brief plan` and `brief run` are described as the stepwise commands that `/project` chains.
- [ ] **Step 2: Runbook** `docs/briefv2/project-runbook.md` (new, no em dashes): (a) the human prerequisites of spec 16 as a checklist with the exact commands (`gophermind brief vault set DATABASE_URL`, `ssh -N -L 55432:127.0.0.1:5432 mini`, the `gophermind.yaml` lines for `privacy.mode: private_only`, tiers, `base_url_fallbacks` with `http://10.8.0.6:11434/v1`, `toolchain.PATH`); (b) the graded command line for the real brief, `gophermind-dev project gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md --repo "$HOME/OtherProjects/AIVentureStudio" --generate JWT_SIGNING_KEY=hex32 --generate STUDIO_LLM_API_KEY=placeholder --require-private --expect-head goal-baseline --expect-binary-commit "$(git rev-parse --short HEAD)"` (graded by `--expect-head`; one invocation; no `--resume`) with output sent to a log file under the scratchpad; (c) the state table and the ordered clear procedure of spec 9 including the wrapper warning (the orchestrator tests `git reset --hard goal-baseline`, `git clean -fdx -e .remember` and `git branch -D gm/<id>` in the target repo first and asks John for explicit sign-off before any override) and the guarded delete; (d) how to read `_state/project.json`, `UNDERSTANDING.md`, `report.json`, `gophermind brief status <id>` and `gophermind brief calls <id>` before clearing, and how to archive the whole run folder (the ledger `_state/calls.jsonl`, the events `_state/events.jsonl`, `attempts/`), because clearing removes that folder and there is no database to read afterwards; (e) what exit 6 means (it does not count as an attempt) and what exit 7 means (a harness fault).
- [ ] **Step 3: CHANGELOG** entry under Unreleased: `/project now runs the v2 planner and executor in one command; the v1 planner is /plan-v1; gophermind project <brief>; unattended runs take Clarify recommendations and confirm the understanding by rule; preflight exit 6 with a numbered human list; --graded; a node-class table in the project report; scripts/build-dev-binary.sh`.
- [ ] **Step 4: Verify and commit:**

```bash
grep -nP '\x{2014}' docs/briefv2/README.md docs/briefv2/project-runbook.md || true     # must print nothing
git add docs/briefv2/README.md docs/briefv2/project-runbook.md CHANGELOG.md
git commit -m "docs: /project single entry point README section, runbook and changelog" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01PvnozsdVNnSEdgNydajDZZ"
git push origin feat/briefv2-planner-core
```

- [ ] **Step 5: Rehearsal, reported not asserted (nothing here runs a model).** Purpose: learn, before the graded loop, exactly what a human still has to provide. The rehearsal is read-only against `~/.gophermind` (no file there is created or changed, except that the guard script writes the dev binary file `~/.gophermind/bin/gophermind-dev`) and read-only against the target repo `~/OtherProjects/AIVentureStudio`, the vault and the mini. Build the dev binary with the script (Task 13), then run the preflight against the real brief and print the state paths:

```bash
SP=/private/tmp/claude-501/-Users-jbrahy-OtherProjects-PMSLLC-gophermind-com/b7a69b80-eaca-4766-ae46-bc9098948066/scratchpad
scripts/build-dev-binary.sh
B=gophermind-lib/briefv2/testdata/ai-venture-studio-server-brief.md
"$HOME/.gophermind/bin/gophermind-dev" project "$B" --repo "$HOME/OtherProjects/AIVentureStudio" \
  --generate JWT_SIGNING_KEY=hex32 --generate STUDIO_LLM_API_KEY=placeholder --require-private \
  --expect-head goal-baseline --preflight-only > "$SP/preflight.log" 2>&1; echo "exit $?"
"$HOME/.gophermind/bin/gophermind-dev" project "$B" --repo "$HOME/OtherProjects/AIVentureStudio" --print-state-paths
```

Report to John: the exit code, the numbered list of missing items from `preflight.log` (expect: the vault passphrase, `DATABASE_URL`, `TEST_DATABASE_URL` with the tunnel command, the `gophermind.yaml`, `toolchain.PATH`), and the state paths, with no value of any secret. Make no change to `~/.gophermind` (beyond the dev binary), the target repo or the mini. The log lives in the scratchpad; remove it with the guarded delete when done:

```bash
target="$SP/preflight.log"
case "$target" in ""|"/"|"$HOME") echo "refusing to delete: bad target" >&2 ;;
  /private/tmp/claude-501/*/scratchpad/preflight.log) rm -f -- "${target:?}" ;;
  *) echo "refusing to delete: outside the scratchpad" >&2 ;; esac
```
