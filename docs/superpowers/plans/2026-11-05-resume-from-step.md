# Resume From Step Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-enter a finished run at a chosen planner stage or executor node, keeping everything before it, from the CLI and the run page.

**Architecture:** A new package `gophermind-lib/build/resume` owns the step grammar, the discard tables and the move-aside; it imports `planner`, `executor` and `blackboard` (the planner cannot import the executor). The CLI's `brief resume --from` and the server's `POST /app/briefs/{id}/resume` both call it, then run the existing planner loop and `brief run`, whose own resume paths do the rest. The blackboard gains one `Reset` method.

**Tech Stack:** Go 1.22+, standard library, the existing `flag` sets, the embedded HTML pages.

**Spec:** `docs/superpowers/specs/2026-11-05-resume-from-step-design.md`

## Global Constraints

- No em dashes, no emojis in code, comments, docs or messages.
- Nothing is deleted from a run directory: discarded state moves to `_state/discarded/<UTC RFC3339, colons replaced by dashes>/` keeping its relative path.
- Every repository reset is guarded: the path is assigned, checked absolute and inside the intended directory, written to the events, then acted on; `rm -rf`-class calls take `"${target:?}"` form in shell and an explicit non-empty, absolute, prefix-checked path in Go.
- Subagents never touch `~/.gophermind`; tests use `t.TempDir()` run directories.
- Commit per task with explicit `git add` paths and the two trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Xdw3puo4RT5ExvAALWc8N3`.
- Build and test from `gophermind-lib`: `go build ./... && go vet ./... && gofmt -l .`; `go test ./build/resume/ ./build/blackboard/ ./serve/`; the CLI module `cmd` builds.

## Review Focus

1. A `node:` resume of a node whose row is `failed` (terminal in the transition table): `Reset` must bypass the table deliberately and the test must prove the row is `ready` with no attempts afterwards (Task 2).
2. A planner-stage resume while the executor already ran: the stale `_state/executor.json` would make the executor refuse with "plan files changed"; the discard set must include every executor file and every runtime row (Task 1 and Task 3 tests list them).
3. A resume requested while the run is active (live claim or server `active`): refused with 409, never two processes on one run dir (Task 2 for the claim check, Task 5 for the server).
4. A `testwriter` resume: `testwriterDone` reads `status.json.PlannedAt`, not a file; the discard must clear that field or the stage is skipped (Task 1 test).
5. A step name that is a stage never reached (for example `coverage` on a run that stopped in `contract`): refused with a message naming the last completed stage, and nothing moved (Task 1 test).

---

### Task 1: The `resume` package: steps, discard tables, planner-stage move-aside

**Files:**
- Create: `gophermind-lib/build/resume/resume.go`
- Create: `gophermind-lib/build/resume/discard.go`
- Create: `gophermind-lib/build/resume/resume_test.go`
- Read first: `gophermind-lib/build/planner/planner.go:123-141` (stages), `planner/state.go:15-44` (file names), each stage's `done` function (`clarifyDone` clarify.go:21, `confirmDone` confirm.go:107, `contractDone` contract_stage.go:194, `decomposeDone` decompose.go:61, `enrichDone` enrich.go:107, `coverageDone` coverage.go:66, `approveDone` approve.go:27, `testwriterDone` testwriter.go:83), `executor/state.go:26-43`, `executor/results.go:10`, `blackboard/fs.go:165-190` (runtime row path), `human/file.go:202` (ESCALATION files).

**Interfaces:**
- Produces:
  - `type Step struct { Stage string; Node string }` with `func ParseStep(s string) (Step, error)`: `s` is one of the eight stage names or `node:<id>`; anything else is an error `resume: unknown step %q (a planner stage or node:<id>)`.
  - `const Stages = []string{"clarify","confirm","contract","decompose","enrich","coverage","approve","testwriter"}` in pipeline order (a `var`, since Go has no slice constants).
  - `func stageFiles(stage string) []string`: the run-dir-relative files and directories that mark the stage done and that it writes. Exact table (verify each against the stage code and extend if a stage writes more; the test pins this list):
    - clarify: `_state/clarify/questions.json`, `_state/clarify/facts.json`, `_state/clarify/rounds.jsonl`, `_state/clarify.json`, `_state/question.json`, `answers.json`, `QUESTIONS.md`
    - confirm: `_state/understanding.json`, `UNDERSTANDING.md`
    - contract: `contracts.json`, `_state/contract.json`, `_state/sources.json`
    - decompose: `_state/decomposed.json`, `_state/classes.json`, `dependencies.json`, and the node plan files decompose writes (find the directory in decompose.go; list it as a directory entry)
    - enrich: `_state/enriched.json`
    - coverage: `coverage.json`
    - approve: `approval.json`, `APPROVAL.md`
    - testwriter: `_state/testwriter.json`, `_state/test_files.json`, `_state/leaf_tests.json`, `_state/acceptance_tests.json`, plus the field clear below
  - `var executorFiles = []string{"_state/executor.json", "_state/leaf-results.json", "_state/notes.json", "_state/escalations.json", "_state/revisions", "report.json", "_state/project.json"}` plus every `*.runtime.json` and `*.runtime.json.lock` under the run dir (walk) and every `ESCALATION-*.md` at the run root.
  - `type Result struct { Discarded []string; DiscardDir string }`.
  - `func FromStage(runDir, stage string, now time.Time) (Result, error)`: refuses when `stage` is not in `Stages`; refuses with `resume: stage %q was never reached; the run completed %q` when the stage's own marker is absent (the "last completed" is the latest stage whose first file exists; a run with nothing done refuses with `resume: nothing to resume; the run never completed a stage`); otherwise moves, for the named stage and every later stage, each existing path in `stageFiles` to `<runDir>/_state/discarded/<ts>/<relative path>` (creating parents; directories moved whole), moves every path in `executorFiles` the same way, removes `_state/planner_error.json` (moved too), and rewrites `_state/status.json` with `PlannedAt` and `Waiting` cleared (read it with the planner's status type; if the type is unexported, add an exported `planner.ClearPlanned(runDir) error` in `planner/state.go` and call it). `Discarded` lists the relative paths moved, sorted.
  - `func discardDir(runDir string, now time.Time) string` = `_state/discarded/` + `now.UTC().Format("2006-01-02T15-04-05Z")`.

- [ ] **Step 1: Write the failing tests** in `resume_test.go`

```go
func TestParseStep(t *testing.T)            // "contract" -> Stage; "node:fn-run" -> Node; "build", "node:" -> error text above
func TestFromStageMovesTheStageAndLater(t *testing.T)
// temp run dir with every stage's marker files present (write empty files), _state/status.json {"planned_at":"x"}, _state/executor.json, comp/fn-a.runtime.json, ESCALATION-fn-a.md, report.json.
// FromStage(dir, "coverage", fixed time) -> Discarded contains coverage.json, approval.json, APPROVAL.md, the four testwriter files, every executor file, comp/fn-a.runtime.json, ESCALATION-fn-a.md, report.json; does not contain contracts.json or _state/enriched.json; each moved path exists under _state/discarded/2026-11-05T22-00-00Z/<same relative path>; status.json planned_at is "".
func TestFromStageRefusesAnUnreachedStage(t *testing.T)   // only clarify+confirm+contract markers: FromStage(dir, "coverage") error mentions "never reached" and "contract"; nothing moved (no _state/discarded dir)
func TestFromStageRefusesAnUnknownStage(t *testing.T)
func TestFromStageKeepsTranscripts(t *testing.T)           // _state/llm/1.json and brief.md and run.log untouched
```

- [ ] **Step 2: Run to verify they fail** (`go test ./build/resume/` from `gophermind-lib`: build failure, package missing)
- [ ] **Step 3: Implement** `resume.go` (`Step`, `ParseStep`, `Stages`, `Result`, `FromStage`) and `discard.go` (`stageFiles`, `executorFiles`, `moveAside(runDir, rel, dest string) (bool, error)` using `os.Rename` after `os.MkdirAll` of the parent, the walk for runtime rows). Package doc: one paragraph on what a resume keeps and moves.
- [ ] **Step 4: Run to verify they pass**; `go vet ./build/resume/`, `gofmt -l build/resume`
- [ ] **Step 5: Commit** `git add gophermind-lib/build/resume/ [gophermind-lib/build/planner/state.go]` with message `feat(resume): steps, discard tables and planner-stage move-aside`

### Task 2: Blackboard `Reset`, node resume and the list of resume points

**Files:**
- Modify: `gophermind-lib/build/blackboard/blackboard.go` (interface), `blackboard/fs.go` (after `SetResult`, ~line 424), `blackboard/fs_test.go` (or the existing test file for FS)
- Modify: `gophermind-lib/build/resume/resume.go`
- Create: `gophermind-lib/build/resume/points.go`
- Test: `gophermind-lib/build/resume/resume_test.go`, `blackboard` tests
- Read first: `blackboard/fs.go:104` (`fsDoc`), `fs.go:198-235` (`mutate`), `cmd/gophermind/brief_run.go` (how the board is opened: constructor and layout option), `executor/results.go` (`LoadLeafResults`, `setResult`), `runview` for node titles (`runview.Open(...).State()` nodes, or the plan files).

**Interfaces:**
- Consumes: `FS.mutate`, `Status` constants, `LoadLeafResults(runDir) (map[string]LeafResult, error)` (confirm the exact name and type in `executor/results.go`).
- Produces:
  - `Blackboard.Reset(ctx, runID, nodeID string) (removed []Attempt, err error)`: sets `Status: StatusReady`, clears `Claim`, `HeartbeatAt`, `Result` and `Attempts`, keeps `Revision` and `Wave`; emits event kind `reset`. It is the one write that ignores `transitionAllowed`, and its doc says why (a human-requested resume). A missing row is an error.
  - `resume.FromNode(ctx, runDir string, board blackboard.Blackboard, runID, node string, now time.Time) (Result, error)`: refuses when `_state/executor.json` is absent (`resume: the run never reached the executor`); refuses when any row has a live claim (`resume: the run is active (node %s is claimed)`, check `List` for claimed or in_progress); reads the row; refuses `verified` (`resume: %s is verified; a verified node cannot be rerun in this round`), `ready`, `pending` and claimed states (`resume: %s has not run yet; resume the run without --from`); for `failed`, `escalated`, `needs_revision`: calls `Reset`, writes the removed attempts as JSON to `<discardDir>/attempts-<node>.json`, removes the node's entry from `_state/leaf-results.json` (rewrite the file), moves `report.json`, `_state/project.json` and `ESCALATION-<node>.md` (sanitized name as in `human/file.go:202`) aside. `Discarded` lists what moved.
  - `type Point struct { Step string \`json:"step"\`; Label string \`json:"label"\` }` and `resume.Points(ctx, runDir string, board blackboard.Blackboard, runID string) ([]Point, error)`: in order, every stage whose first `stageFiles` entry exists (`Label` = the stage name), then, when `_state/executor.json` exists, every row with status failed, escalated or needs_revision as `node:<id>` with `Label` = `<id> (<status>)`; empty when any row is claimed or in_progress. `board` may be nil when there is no executor state.

- [ ] **Step 1: Write the failing tests**

```go
// blackboard: TestResetReturnsARowToReady: InitRun one node, Claim, SetStatus to in_progress then failed, AppendAttempt twice; Reset -> row.Status == StatusReady, len(Attempts)==0, Claim=="", removed has 2; an unknown node errors.
// resume: TestFromNodeResetsAFailedNode (flat-layout FS board in the temp run dir, _state/executor.json present, leaf-results {"fn-a":{"status":"failed","reason":"ladder_exhausted"},"fn-b":{"status":"verified"}}, ESCALATION-fn-a.md, report.json): after FromNode(... "fn-a") the row is ready, leaf-results has only fn-b, attempts-fn-a.json exists in the discard dir, report.json moved, fn-b's row untouched.
// TestFromNodeRefusesVerifiedAndUnrun, TestFromNodeRefusesWithoutExecutorState, TestFromNodeRefusesWhileClaimed.
// TestPointsListsStagesThenNodes: markers through approve, rows fn-a failed, fn-b verified, fn-c escalated -> ["clarify",...,"approve","node:fn-a","node:fn-c"] with labels; with fn-b claimed -> empty.
```

- [ ] **Step 2: Run to verify they fail**
- [ ] **Step 3: Implement** `Reset` in `fs.go` through `mutate`; `FromNode` and `Points`.
- [ ] **Step 4: Run to verify they pass**; `go test ./build/blackboard/ ./build/resume/ ./build/executor/ -run 'Reset|Board'`; vet, gofmt
- [ ] **Step 5: Commit** `feat(resume): blackboard Reset, node resume and resume points`

### Task 3: Repository reset behind a planner-stage resume

**Files:**
- Create: `gophermind-lib/build/resume/repo.go`, `repo_test.go`
- Read first: `executor/resume.go` (`ForeignCommits`, `CanFastForward`, how the branch and base are read from `_state/executor.json` before it is moved: read the state first), `executor/git*.go` (the git helper the executor uses; reuse it, do not shell out separately unless it has no method for the call), `executor/plan.go` (how to list the plan's files: wave 0 files and each `Leaf.File`, `StubFile`, `TestFile`).

**Interfaces:**
- Produces:
  - `type RepoReset int` with `RepoKeep`, `RepoOwnedClone`, `RepoRestoreFiles`.
  - `func ResetRepo(runDir, repo string, mode RepoReset, state executorState) (actions []string, err error)` where `executorState` is read from `_state/executor.json` before `FromStage` moves it (add `func ReadExecutorState(runDir) (branch, base string, started bool, err error)` to the package using the executor's exported `State` type if exported, else a local struct with the two JSON fields `branch` and `started_at`; base comes from the brief frontmatter `base_branch`, read through `brief.Parse` of `<runDir>/brief.md`).
  - `RepoOwnedClone` (the service): `repo` must be absolute and have the service's repos directory as a prefix (passed in as `ownedUnder string`; refuse otherwise with `resume: refusing to reset %s: not under %s`); then `git -C repo checkout -- .`-equivalent reset to base: `git reset --hard <base>` and `git clean -fd`, run through the executor's git helper or `exec.Command("git", "-C", repo, ...)`; `actions` records both commands with the path.
  - `RepoRestoreFiles` (the CLI `--reset-repo`): when `branch` is set and exists, refuse unless the executor's foreign-commit check reports zero foreign commits, then `git branch -D <branch>` after checking out `base`; then for each plan file, `git checkout <base> -- <file>` when the file exists at base, else `os.Remove` of the file (guard: the joined path must stay inside `repo`). `actions` lists each.
  - `RepoKeep`: no action; used for `node:` resumes.
  - `FromStage` gains a parameter `repo RepoRequest{Repo string; Mode RepoReset; OwnedUnder string}`; with `Mode == RepoKeep` and executor state present it refuses with `resume: executor state exists; pass --reset-repo to discard the run's branch and files, or resume from node:<id>` so the Task 1 tests pass a request with `RepoKeep` and no executor state, or update them to `RepoOwnedClone` with a temp clone.

- [ ] **Step 1: Write the failing tests** (`repo_test.go`, each on a temp git repo made with `git init`, one commit on `main`):

```go
func TestOwnedCloneIsResetToBase(t *testing.T)          // dirty file + untracked file + extra commit -> after ResetRepo HEAD == base commit, tree clean
func TestOwnedCloneRefusesAPathOutsideTheOwner(t *testing.T)
func TestRestoreFilesDeletesOnlyARunOwnedBranch(t *testing.T) // branch gm/x with one commit by the run -> deleted; with a foreign commit -> refused, branch kept
func TestRestoreFilesRestoresListedFilesOnly(t *testing.T)   // listed modified file restored, listed untracked removed, unlisted dirty file untouched
func TestFromStageRefusesWithoutARepoModeWhenTheExecutorRan(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail**
- [ ] **Step 3: Implement** `repo.go`; thread `RepoRequest` through `FromStage`.
- [ ] **Step 4: Run to verify they pass**; vet, gofmt; `go test ./build/resume/`
- [ ] **Step 5: Commit** `feat(resume): repository reset behind a planner-stage resume`

### Task 4: CLI `brief resume --from <step> [--reset-repo]`

**Files:**
- Modify: `cmd/gophermind/brief_plan.go:108-150` (flags, dispatch), `cmd/gophermind/brief.go:19-38` (usage text)
- Modify: `cmd/gophermind/brief_run.go` only if the board constructor must be shared (export a small helper `openBoard(runDir) blackboard.Blackboard` in the cmd package)
- Test: `cmd/gophermind/brief_resume_test.go` (the cmd package has tests; follow their style for running a verb against a temp run dir)

**Interfaces:**
- Consumes: `resume.ParseStep`, `resume.FromStage`, `resume.FromNode`, `resume.RepoRequest`, the existing `planner.Options{RunID}` path.
- Produces: flags `--from <step>` and `--reset-repo` on `brief resume`. Behaviour: parse the step; for a stage: `FromStage(runDir, stage, RepoRequest{Repo: rec.Repo, Mode: RepoKeep or RepoRestoreFiles when --reset-repo})`, print one line per discarded path prefixed `discarded: ` and the repo actions, then continue into the normal planner resume (exit codes unchanged: 0 planned, 3 waiting, 1 error); for a node: `FromNode`, print the discarded paths, print `resumed fn-run; run: gophermind brief run <id> --repo <repo>` and exit 0 without calling the planner. A refusal from the package prints its message to stderr and exits 1. `--reset-repo` without `--from <stage>` is an error.

- [ ] **Step 1: Write the failing tests**: `TestBriefResumeFromRefusesAnUnknownStep` (exit 1, stderr holds "unknown step"), `TestBriefResumeFromNodeResetsAndExits0` (temp run dir with executor state and a failed row), `TestBriefResumeRejectsResetRepoWithoutFrom`.
- [ ] **Step 2: Run to verify they fail**
- [ ] **Step 3: Implement**; update the usage text with the two flags and the step grammar.
- [ ] **Step 4: Run to verify they pass**; `go build ./... && go vet ./...` in `cmd`
- [ ] **Step 5: Commit** `feat(cli): brief resume --from <step> and --reset-repo`

### Task 5: Service endpoint, resume points in the state, run page control

**Files:**
- Modify: `gophermind-lib/serve/webhook.go:461-475` (route `POST /app/briefs/{id}/resume`, and the route table comment), `serve/briefs.go` (`briefRecord` fields `resumed_at`, `resumed_from`; a `resumeFrom(rec, step)` beside `resume(rec)` at line 320 that runs `m.run(a, rec, []string{"brief","resume",rec.ID,"--yes","--gate","file","--from",step}, logf)`), `serve/briefstate.go:52-67` (`BriefRun.ResumePoints []resume.Point \`json:"resume_points"\``, filled in `BuildBriefState` from `resume.Points` when `!active`), `serve/assets/brief.html` (line 21 markup, line 166 visibility, handler after line 610)
- Test: `serve/brief_resume_test.go` using `newBriefEnv` and the fake `m.cmd` the existing brief tests use (see how `TestResubmit...` fakes the process)

**Interfaces:**
- Consumes: `resume.Points`, `resume.ParseStep`, `resume.FromStage`, `resume.FromNode`. The service calls the package directly for the discard and, for a stage, the clone reset with `RepoOwnedClone` and `OwnedUnder` set to the directory that holds the per-run clones (`m.repoFor(id)`'s parent); it owns those clones. It then runs `brief resume <id> --yes --gate file` for a stage (the planner continues, then `m.run` runs `brief run` as today), or `brief run <id> --repo <clone>` directly for a node. State this in the handler's comment. The CLI flags from Task 4 are for a person at a terminal, not for the service.
- Produces:
  - `POST /app/briefs/{id}/resume?from=<step>`: 404 unknown run; 409 `{error, active_run_id}` when a run is active; 400 `{error}` with the package's refusal text; 202 `{run_id, url}` with the same id. The record's phase becomes `planning` (stage) or `running` (node), `Error` cleared, `resumed_at`/`resumed_from` set, and the log appended with a line `resume from <step> at <time>`.
  - `GET /app/briefs/{id}/state` includes `resume_points` (empty array, never null, when none).
  - Run page: in `.actions`, `<select id="resume-from" hidden></select><button id="resume" type="button" hidden>Resume from this step</button>`; shown when `run.resume_points.length > 0 && !run.active`; options from the points; the click POSTs `/app/briefs/<id>/resume?from=<encoded step>` with `credentials:'same-origin'`, disables the button during the call, on 202 reloads (`window.location.reload()`), else writes `j.error` to `#stopmsg`. The polling predicate at line 548 already treats `planning` and `running` as live.

- [ ] **Step 1: Write the failing tests**: `TestResumeFromStageStartsThePlannerStep` (fake cmd records argv; expects `brief resume <id> --yes --gate file`; record phase planning; 202 body has the same id), `TestResumeFromNodeStartsTheRunStep` (argv `brief run <id> --repo ...`, phase running), `TestResumeRefusesWhileActive` (409 with `active_run_id`), `TestResumeRefusesAnUnknownStep` (400), `TestStateCarriesResumePoints` (a finished run dir with markers and a failed row lists them; an active run lists none), `TestRunPageHasTheResumeControl` (markup present).
- [ ] **Step 2: Run to verify they fail**
- [ ] **Step 3: Implement** handler, record fields, state field, page control.
- [ ] **Step 4: Run to verify they pass**; `go test ./serve/`; vet, gofmt
- [ ] **Step 5: Commit** `feat(serve): resume a run from a chosen step; resume points on the run page`

### Task 6: Prove it on the recorded hello run shape (fixture, no live model)

**Files:**
- Create: `gophermind-lib/build/resume/hello_shape_test.go`

A test that builds a run dir in the shape r003 left behind (markers through testwriter, `_state/executor.json`, rows: fn-greeting verified, fn-run escalated with two attempts, fn-main pending, leaf-results for the first two, `ESCALATION-fn-run.md`, `report.json`), calls `Points` (expects the eight stages then `node:fn-run`), then `FromNode(... "fn-run")`, then asserts fn-greeting's row and the planner files are byte-identical to before, fn-run is ready with no attempts, fn-main untouched, and the discard dir holds `attempts-fn-run.json`, `report.json` and the escalation file.

- [ ] **Step 1: Write the test**, **Step 2: run, expect pass** (it tests Tasks 1-2 together; if it fails, the fix belongs to the task that owns the behaviour), **Step 3: Commit** `test(resume): the hello r003 shape resumes at fn-run`
