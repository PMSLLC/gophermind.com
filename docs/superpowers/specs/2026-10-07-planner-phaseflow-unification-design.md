# One System: Planner, Executor, PhaseFlow and Run State

Status: draft for review. Date: 2026-10-07. Replaces the earlier draft of this file ("Planner and PhaseFlow Bridge"), which was wrong: it proposed building an executor on PhaseFlow's wave loop, but the v2 executor already exists.
Scope: make GopherMind have exactly one planner, one executor, and one store of run state, and make PhaseFlow a view of that one system instead of a second one.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (planner), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (executor; both on branch `feat/briefv2-planner-core`, not yet merged to `main`), `docs/GOAL.md` task 4 (`/project` as the single entry point, plus a ruling on the v1 planner).

## 1. Why this exists

John asked for the planner to be tightly integrated with PhaseFlow, then set two rules: nodes must be written to a browsable filesystem, not an SQL database, and there must be one version, not two.

Today there are two of everything:

| Thing | Version 1 | Version 2 |
|---|---|---|
| Planner | `plantree` (tree in `.planning/plan`), driven by `/project` in the TUI (`gophermind-lib/tui/project.go`) | briefv2 `planner` (`gophermind brief plan`) |
| Executor | `phaseflow.ExecuteWithReviser`, called only from `gophermind-lib/tui/execute.go` | briefv2 `executor.Run` (`gophermind brief run`) |
| Run state | `.planning/` Markdown and `assignments.json` | SQLite `~/.gophermind/blackboard.db` (tables `rows`, `events`, `calls`) |

The rule "one version" is read here as: each row ends with one entry. That reading is an assumption (section 8, item 1).

## 2. Decisions

| # | Question | Decision | Source |
|---|---|---|---|
| 1 | Which planner | briefv2 `planner`. `/project` stops using `plantree` | John, "only one version"; GOAL task 4 asks for this ruling |
| 2 | Which executor | briefv2 `executor.Run`. `/project` stops calling `phaseflow.ExecuteWithReviser` | same |
| 3 | Where run state lives | Files inside the run folder `.gophermind/<run>/`. No SQLite anywhere in briefv2, and no option to switch it back on | John, "browsable filesystem" |
| 4 | What PhaseFlow becomes | A read-only view generated from the run folder. It has no state of its own for a v2 run | the one-version rule |
| 5 | Model fallback | The planner's router owns it | John, 2026-10-07 |
| 6 | Order | Files first, then `/project`, then the PhaseFlow view | section 3 |

## 3. Three sub-projects, in order

Each gets its own plan and its own build. All code work happens on `feat/briefv2-planner-core` (worktree `~/OtherProjects/PMSLLC/gophermind-planner-core`), because `main` has neither the planner nor the executor.

| # | Sub-project | Depends on |
|---|---|---|
| A | Filesystem run state replaces SQLite | nothing |
| B | `/project` plans and builds through v2 | A, so that what `/project` shows is the files |
| C | PhaseFlow view: `.planning/` files and the pipeline endpoints read the run folder | A and B |

This document specifies A in full and B and C in outline. B and C get their own spec sections once A is built and the field mapping is known.

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
8. **Prompt text is still never stored**, only hash and size, as the ledger spec requires.

### 4.4 Other code that must learn the new files

- `tree.Store.Load` must skip `*.state.json`, `*.claim`, `*.lock`, and `_state/`. Today it skips a fixed list of named files (`notNodes`) and a state sidecar would otherwise load as a node.
- `report` and `brief status`, `brief calls`, `brief report` read the same interfaces, so they need no change beyond the constructors in `cmd/gophermind/brief_plan.go` and `brief_run.go`, which currently open the database.
- The executor's own `_state/leaf-results.json` and `_state/executor.json` already live in the run folder and stay as they are.

### 4.5 Existing data

Runs recorded only in `~/.gophermind/blackboard.db` are not migrated. Whether any such run matters is unverified (section 8, item 3). The planner and executor exist only on the unmerged branch, so no released version wrote that file.

### 4.6 Tests (all offline)

1. The existing blackboard and ledger interface tests run unchanged against the filesystem implementation. That is the proof that the executor sees no difference.
2. Two goroutines claim one ready node: exactly one wins.
3. A claim with an old heartbeat is reclaimed after `stale_claim_seconds`.
4. A kill between the temp-file write and the rename leaves the old state intact.
5. 100 concurrent appends to `events.jsonl` produce 100 intact lines.
6. `tree.Store.Load` on a run folder full of `.state.json`, `.claim`, and `.lock` files returns only the plan nodes.
7. The executor end-to-end test (`executor/e2e_test.go`) passes with the filesystem backend and no SQLite file is created anywhere.
8. A grep gate: no `briefv2` package imports `database/sql` or `modernc.org/sqlite`.

## 5. Sub-project B outline: `/project` through v2

`/project <name> <brief>` in `gophermind-lib/tui/project.go` currently builds the v1 `plantree`, asks questions, approves, exports to `.planning/` and runs `phaseflow.ExecuteWithReviser`. After B it runs the v2 flow: load and validate the brief, `planner` stages, the human gates through the `human.Gate` interface (a TUI adapter, so the question round and approval stay in the terminal), then `executor.Run`. GOAL task 4's requirement holds: one command, no second command, no human step beyond the gates.

The ruling on v1: `/project` no longer calls `plantree` or `phaseflow.Execute*`. Deleting those packages is a separate decision, because other code uses them (section 8, item 2).

## 6. Sub-project C outline: PhaseFlow as a view

For a v2 run, `ROADMAP.md`, `STATE.md`, `assignments.json`, and the pipeline state endpoints (`GET /pipeline/state`, the `run-report` event) are generated from the run folder (nodes, `.state.json` files, `report.json`). They are written by one function and never edited by hand, so there is no second copy of the state to disagree. The mapping needs these decisions, taken when C is specified: how a component maps to a PhaseFlow phase; the wave numbering gap (the v2 tree gives a dependency-free function wave 0, PhaseFlow's `AssignWaves` gives it wave 1); and which PhaseFlow commands (`verify`, `milestone`) still apply to a v2 run.

## 7. Not in scope

- Deleting `plantree`, `orchestrate`, or the PhaseFlow executor code.
- Any change to the planner's stages or the executor's ladder.
- Git landing, the proxy and the sandbox (already built on the branch).
- Cross-run queries in SQL. A cross-run report becomes a scan of run folders, added only if asked.

## 8. Open items

1. **Meaning of "one version".** This spec reads it as one planner, one executor, and one state store. If John meant only one of these, sections 2 and 3 shrink to that one.
2. **Callers of the v1 code.** `phaseflow.ExecuteWithReviser` has one caller outside its package on the branch (`tui/execute.go`). `plantree` has more (the TUI approve and question flow, `plantree/export`, the server pipeline, the desktop app). A full caller inventory is needed before anything is deleted.
3. **Old database.** Check whether `~/.gophermind/blackboard.db` exists on John's machines or on the mini and holds a run that must survive.
4. **Merge state.** `feat/briefv2-planner-core` is 108 commits ahead of `main`. Decide whether it merges before A starts or A is built on the branch and merged with it. The plan assumes the branch.
5. **Locking under many workers.** Section 4.3 rule 3 uses lock files. If the executor's worker count grows beyond the current `--workers` limit, measure the contention before trusting it.
