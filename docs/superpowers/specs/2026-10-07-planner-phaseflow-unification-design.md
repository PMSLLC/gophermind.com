# One System: Planner, Executor, PhaseFlow and Run State

Status: draft for review. Date: 2026-10-07 (updated the same day after John's answers to the review). Replaces the earlier draft of this file ("Planner and PhaseFlow Bridge"), which was wrong: it proposed building an executor on PhaseFlow's wave loop, but the v2 executor already exists.
Scope: make GopherMind have exactly one planner, one executor, and one store of run state, make every plan node carry everything needed to build, debug and analyse it, and make PhaseFlow a view of that one system instead of a second one.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (planner), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (executor), `docs/superpowers/specs/2026-09-30-project-single-entry-design.md` (`/project`, GOAL task 4), all three on branch `feat/briefv2-planner-core`, and `docs/GOAL.md`.
Evidence: `docs/reviews/2026-10-07-planner-executor-v1-v2-review.md` (what v1 and v2 do, defects, keep/drop verdicts, the new node fields) and `docs/reviews/2026-10-07-grilling-and-the-question-model.md` (the question model).

## 1. Why this exists

John asked for the planner to be tightly integrated with PhaseFlow, then set rules: nodes must be written to a browsable filesystem, not an SQL database; there must be one version, not two; and the best of v1 and v2 must be merged so that a brief is fully decomposed into granular nodes that carry construction, error handling, model-performance tracking, performance options, portability, security, success and failing tests, typed inputs and outputs, alternate approaches, logging, refactoring opportunities and anything else useful for profiling, debugging and analysis.

Today there are two of everything:

| Thing | Version 1 | Version 2 |
|---|---|---|
| Planner | `plantree` (tree in `.planning/plan`), driven by `/project` in the TUI (`gophermind-lib/tui/project.go`) | briefv2 `planner` (`gophermind brief plan`) |
| Executor | `phaseflow.ExecuteWithReviser`, called only from `gophermind-lib/tui/execute.go` | briefv2 `executor.Run` (`gophermind brief run`) |
| Run state | `.planning/` Markdown and `assignments.json` | SQLite `~/.gophermind/blackboard.db` (tables `rows`, `events`, `calls`) plus `_state/*.json` |

"One version" is read here as: each row ends with one entry. That reading is an assumption (section 10, item 1).

## 2. Decisions

| # | Question | Decision | Source |
|---|---|---|---|
| 1 | Which planner | briefv2 `planner`. `/project` becomes v2 | John; GOAL task 4 |
| 2 | Which executor | briefv2 `executor.Run` | John |
| 3 | Where run state lives | Files inside the run folder `.gophermind/<run>/`. No SQLite anywhere in briefv2, and no option to switch it back on | John, "browsable filesystem" |
| 4 | What PhaseFlow becomes | A read-only view generated from the run folder. It has no state of its own for a v2 run | the one-version rule |
| 5 | Model fallback | The planner's router owns it | John, 2026-10-07 |
| 6 | Order of work | Files, then node widening with the question model, then `/project` on v2, then the PhaseFlow view, then the planner revise stage, then parallel waves | John, "use your order", 2026-10-07 |
| 7 | Attempt artifacts | Save them: scrubbed reply and compiler or test output per attempt, in the run folder, with a setting to turn it off | John, "save artifacts" |
| 8 | Node content | All groups on every node (rationale, construction, errors, tests, security, performance, portability, observability, alternatives, refactoring), not only on some node classes | John, "require all groups everywhere" |
| 9 | Branch | Do not merge. `main` stays docs-only; all code work stays on `feat/briefv2-planner-core` in the worktree. Supersedes John's earlier "merge first" | John, 2026-10-07 (later the same day) |
| 10 | Question model | Rounds with a dependency tree, fact probe, confirm-understanding gate, decision records, caps of 4 model calls and 30 questions | John, 2026-10-07 (yes to all four). Specified in `2026-10-07-node-widening-and-question-model-design.md` |
| 11 | Sequencing against GOAL.md | Finish the graded run first. None of A to F starts until the GOAL.md graded run has finished; until then this document and the two reviews are the only output of this work | John, 2026-10-07 |

## 3. The work, in order

Each item gets its own plan and its own build. All code work happens on `feat/briefv2-planner-core` (worktree `~/OtherProjects/PMSLLC/gophermind-planner-core`), one writer at a time, and only after the GOAL.md graded run has finished (decision 11). Nothing here may change the planner, the executor, the node schema or the run-state format while that run is in progress, because a graded attempt is one clean run on the final system and a change in between invalidates the rehearsals.

| # | Sub-project | Depends on | Spec |
|---|---|---|---|
| A | Filesystem run state replaces SQLite, with attempt artifacts | nothing | section 4 of this document; plan written 2026-10-07: `docs/superpowers/plans/2026-10-07-filesystem-run-state.md` (9 tasks, not started) |
| B | Node widening and the question model | A (decision records, attempts) | `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md` (sections 5 and 6 of this document are the summary); plan written 2026-10-08: `docs/superpowers/plans/2026-10-08-node-widening-and-question-model.md` (13 tasks, not started) |
| C | `/project` plans and builds through v2 | A, B | the existing `2026-09-30-project-single-entry-design.md` and plan (14 tasks, not built), amended by section 7 |
| D | PhaseFlow view: `.planning/` files and the pipeline endpoints read the run folder | A, C | section 8 |
| E | Planner revise stage: rewrite a node after a failure | B | its own spec; neither v1 nor v2 does this today |
| F | Parallel waves | A | its own spec; the executor runs one worker today |

## 4. Sub-project A: filesystem run state

### 4.1 What changes

The blackboard (`briefv2/blackboard`), the call ledger (`briefv2/ledger`), and the shared database opener (`briefv2/db`) are used through Go interfaces by the executor (11 files), `planner`, `router` (3 files), `report`, `events`, and the `cmd/gophermind` brief commands. Verified by grep on the branch. The executor and planner do not change; the interface implementations do.

Removed: `briefv2/db`, `briefv2/blackboard/sqlite.go`, and the SQLite implementation in `briefv2/ledger`. Added: filesystem implementations of both interfaces. `modernc.org/sqlite` stays in `go.mod` because `gophermind-lib/tools` (the SQL tools) uses it.

### 4.2 Layout

```text
.gophermind/<run>/
  brief.md  contracts.json  approval.json  coverage.json   unchanged
  <component>/component.json                               plan, unchanged
  <component>/<fn-id>.json                                 plan, unchanged
  <component>/<fn-id>.state.json                           NEW: status, revision, wave, attempts, result, updated_at
  <component>/<fn-id>.claim                                NEW: exists only while a worker holds the node (worker, claimed_at, heartbeat_at)
  root.state.json, <component>/component.state.json        NEW: same, for non-function nodes
  _state/events.jsonl                                      NEW: one JSON object per line, append only (replaces table events)
  _state/calls.jsonl                                       NEW: one JSON object per line, append only (replaces table calls)
  attempts/<node-id>/<n>/                                  NEW: reply.go, check-output.txt, attempt.json (scrubbed; decision 7)
```

Run state is never written into the plan node files. The executor hashes the plan files and refuses to resume when they changed ("plan files changed since the run started"), so writing status into them would trip that check.

### 4.3 Rules the filesystem implementation must keep

1. **Atomic writes.** Every state file is written to a temporary file and renamed, as `tree.Store.Write` does. Files are mode 0600 and directories 0700.
2. **Atomic claim.** The SQL `UPDATE ... WHERE status='ready'` is replaced by an exclusive create of `<fn-id>.claim` (`O_CREATE|O_EXCL`). Exactly one worker succeeds.
3. **Transitions.** The allowed-transition table in `blackboard.go` is unchanged and enforced by `SetStatus`. A read-modify-write of a node's state file happens under an exclusive-create `<fn-id>.lock`, retried for a bounded time, and a lock older than `stale_claim_seconds` is reclaimed.
4. **Heartbeat.** A worker refreshes `heartbeat_at` inside the claim file. A claim older than `stale_claim_seconds` is stale, as today.
5. **Append-only logs.** `events.jsonl` and `calls.jsonl` are opened with `O_APPEND`, one write per line, so concurrent appends do not interleave. Ledger write failures keep the existing rule: the call result is kept, a `ledger_error` event is emitted, and the run is marked incomplete.
6. **Watch.** The blackboard `Watch` is implemented by following `events.jsonl` (poll for new lines). No file-system notification library is added.
7. **Path safety.** State paths derive from node IDs, which match `^[a-z0-9][a-z0-9-]*$`, joined through the `pathsafe` rules the executor already uses. A state path that escapes the run folder is refused.
8. **Attempt artifacts (decision 7).** After each attempt the executor writes the model's reply and the combined compiler, vet and test output under `attempts/<node-id>/<n>/`, passed through the existing scrubbers (`packer` secret scrubbing, which covers raw, `%q`, JSON, URL, base64 and whitespace-split forms of declared vault secrets) before they touch disk. The size of each file is capped by a setting. A setting `artifacts: off` restores today's behaviour (hashes and sizes only). Prompt text is still never stored: the ledger keeps its hash and size, because a prompt carries the brief slice and dependency signatures and adds no debugging value over the reply. This changes executor spec rule R15 and the "Replies are never stored" note in `CONTEXT.md`; it extends the opt-in `GOPHERMIND_DEBUG_DUMP_DIR` dump (`planner/debugdump.go`), which stays developer-only.
9. **Planner artifacts.** The same rule applies to planner calls: the reply of each stage is saved under `_state/replies/<stage>/` when artifacts are on. Briefs that declare secrets still go through the scrubbers.

### 4.4 Other code that must learn the new files

- `tree.Store.Load` must skip `*.state.json`, `*.claim`, `*.lock`, `attempts/` and `_state/`. Today it skips a fixed list of named files (`notNodes`) and a state sidecar would otherwise load as a node.
- `report` and `brief status`, `brief calls`, `brief report` read the same interfaces, so they need no change beyond the constructors in `cmd/gophermind/brief_plan.go` and `brief_run.go`, which currently open the database.
- The executor's own `_state/leaf-results.json` and `_state/executor.json` already live in the run folder and stay as they are.

### 4.5 Existing data

Runs recorded only in `~/.gophermind/blackboard.db` are not migrated. The rehearsal runs on the real brief (for example `gm-2026-09-29-002`) are recorded there; whether any must survive is open (section 10, item 4). The planner and executor exist only on the unmerged branch, so no released version wrote that file.

### 4.6 Tests (all offline)

1. The existing blackboard and ledger interface tests run unchanged against the filesystem implementation. That is the proof that the executor sees no difference.
2. Two goroutines claim one ready node: exactly one wins.
3. A claim with an old heartbeat is reclaimed after `stale_claim_seconds`.
4. A kill between the temp-file write and the rename leaves the old state intact.
5. 100 concurrent appends to `events.jsonl` produce 100 intact lines.
6. `tree.Store.Load` on a run folder full of `.state.json`, `.claim`, `.lock` files and an `attempts/` tree returns only the plan nodes.
7. The executor end-to-end test (`executor/e2e_test.go`) passes with the filesystem backend and no SQLite file is created anywhere.
8. A grep gate: no `briefv2` package imports `database/sql` or `modernc.org/sqlite`.
9. Artifacts: a canary secret in a fake reply and in fake test output never appears in any file under `attempts/` (extends the existing canary-leak tests); `artifacts: off` writes none; the size cap truncates with a marker.

## 5. Sub-project B, part 1: node widening

The node schema has `additionalProperties: false`, so each new field must be added to `schema/task-node.schema.json`, to the Decompose and Test-writer prompt output lists, and to the leaf-defect checks where code can validate it. The complete field list, with who fills each field and what code validates, is `docs/reviews/2026-10-07-planner-executor-v1-v2-review.md`, section 5. In summary:

- **Plan fields (planner writes, covered by the plan hash):** `node_class` (moved from `_state/classes.json` onto the node), `requirement_ids[]`, `rationale`, `construction{steps, approach_chosen}`, `alternatives[]`, `contract.inputs[].type_ref`, `contract.errors[].kind` and `.test`, `tests[].polarity` and `.covers`, `portability`, `security`, `performance`, `observability`, `refactor_notes[]`, `profile_hooks[]`, `assumptions[]`, `open_questions[]`, `decision_ids[]`.
- **Run fields (executor writes to the sidecar state file):** per attempt tokens, provider and model served, ladder position, failure stage (gate, scan, build, vet, test) and failing test names, and a pointer to the saved artifacts; per node final model, attempts used, wall time, first-try pass, files changed.
- **Decision 8 (all groups on every node)** means the validators treat the groups as required for every function node, with a minimal legal value for a group that truly does not apply (for example `security.threats: []` with a stated `trust_boundary: none`), so a missing group is a defect and a "not applicable" group is an explicit answer. Components and the root get the groups that make sense for them (rationale, requirement ids, assumptions, open questions, decision ids) and the leaf-defect checks now run on them too.
- **Cost, stated plainly:** a function node becomes several times larger and each Decompose and Test-writer call must write more, on a slow local model (the mini serves one request at a time). Batch sizes (8 functions per Decompose call today) will need to shrink, and the planner's wall time on the real brief will grow. This is measured on the csvstat brief before the real brief, and the batch size is a setting.
- **Other planner fixes that belong in the same change:** route planning calls by `model_tier` or `node_class` instead of the hard-coded strong tier; make coverage map a feature only to nodes that carry its requirement id; fill the dead `assumptions` field; run leaf checks on component and root nodes.

## 6. Sub-project B, part 2: the question model

The proposal, from `docs/reviews/2026-10-07-grilling-and-the-question-model.md`, is to adopt the `grilling` skill's protocol in Go: a design tree of questions with `depends_on`, asked in rounds by frontier with a recommended answer on each; facts answered by a deterministic fact probe, never asked; decisions saved as `decisions/<id>.md` records; a `ConfirmUnderstanding` gate whose hash goes into `approval.json`; node `decision_ids[]` so changing an answer re-plans only the nodes that cite it (v1's `ChangeAnswer` semantics). It is decision 10 above, open until John answers the four questions in section 7 of that document.

Interaction with the single-entry spec: `/project` must reach the end with no human step. In the unattended policy (`project-single-entry` spec, section 8) every question takes its recommendation, marked `Assumed` and recorded on each node it affects, and `ConfirmUnderstanding` is recorded `approved_by: unattended` with the understanding hash. The rounds still run, so dependent questions still appear and are still answered by their recommendations.

## 7. Sub-project C: `/project` through v2

Already designed and planned on the branch: `2026-09-30-project-single-entry-design.md` and its plan (14 tasks, one implementer and one reviewer each, first task extracts `briefv2/envcheck`). It makes `/project` take a brief, run the v2 planner and `executor.Run`, take every human decision by a stated unattended rule or check it in a preflight, and ship a final report. Its ruling R2 keeps v1 available as `/plan-v1` and `/plan-v1-execute` until after the perfect release, because the server, the desktop app and `/phase` import `plantree`, `phaseflow` and `orchestrate`.

This document therefore does not specify C again. It amends it in three places once A and B are built: the preflight and state-clearing sections must name the file layout of section 4 (no database to clear); the unattended policy gains `ConfirmUnderstanding`; and the report gains the node-class dimension from the review. The ruling on the v1 planner that GOAL task 4 asks for is R2 as written: `/project` is v2 only, v1 is renamed, and deletion waits for a caller inventory.

## 8. Sub-project D: PhaseFlow as a view

For a v2 run, `ROADMAP.md`, `STATE.md`, `assignments.json`, and the pipeline state endpoints (`GET /pipeline/state`, the `run-report` event) are generated from the run folder (nodes, `.state.json` files, `report.json`). They are written by one function and never edited by hand, so there is no second copy of the state to disagree. Live progress reuses v1's file-watcher to SSE feed (`serve/pipeline_watch.go`), which fits file-based state directly; its non-blocking hub and event names (`task-status`, `task-attempt`, `wave-changed`, `run-report`) are kept so the desktop app's pipeline panel keeps working. The mapping needs these decisions, taken when D is specified: how a component maps to a PhaseFlow phase; the wave numbering gap (the v2 tree gives a dependency-free function wave 0, PhaseFlow's `AssignWaves` gives it wave 1); and which PhaseFlow commands (`verify`, `milestone`) still apply to a v2 run.

## 9. Not in scope

- Deleting `plantree`, `orchestrate`, or the PhaseFlow executor code (needs the caller inventory: TUI, server pipeline, desktop app, `/phase`).
- Any change to the executor's ladder or sandbox beyond the artifact writes in section 4.3.
- Git landing, the proxy and the sandbox (already built on the branch).
- Cross-run queries in SQL. A cross-run report becomes a scan of run folders, added only if asked.

## 10. Open items

1. **Meaning of "one version".** This spec reads it as one planner, one executor, and one state store. If John meant only one of these, sections 2 and 3 shrink to that one.
2. **Merge (decided).** Not merging. `main` stays docs-only, as `CONTEXT.md` describes; the branch stays in its worktree. The dry-run merge of the committed histories on 2026-10-07 found no conflicts (main 4 ahead, branch 108 ahead), and the uncommitted files on `main` that would have collided with a merge (an untracked copy of the planner-core plan, `planner/testdata/aivs`, a one-line edit to the planner-core spec) were identical to or contained in the branch versions. A test run of the branch was started and stopped partway at the decision (7 packages passed and none failed; the executor package had not run), so the branch's test state is unverified by this work. Revisit the merge after the graded run.
3. **Sequencing against GOAL.md (decided).** Finish the graded run first (decision 11). Consequence for this document: sub-project A's plan, and every later plan, is written but not started until the graded run ends. If the graded run's attempts reveal planner or executor defects, they are fixed under the GOAL.md rules (fix GopherMind, never the target repo) and may change the premises in the review; re-check the review's section 8 claims then.
4. **Old database.** Check whether `~/.gophermind/blackboard.db` (laptop and the mini) holds a run that must survive, in particular the real-brief rehearsal `gm-2026-09-29-002`.
5. **Locking under many workers.** Section 4.3 rule 3 uses lock files. If sub-project F raises the worker count, measure the contention before trusting it.
6. **Executor spec changes.** R15 (output never stored) and the executor spec's "Not in scope" line on the planner revise stage change with this work; the executor spec on the branch is edited when A and E are built, not before.
