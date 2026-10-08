# Planner and Executor: v1 vs v2 Review and Merge Verdicts

Date: 2026-10-07. Status: review for John. No code was changed.

## 1. Goal and method

Goal (John, 2026-10-07): merge v1 and v2 into one system that takes the best of each. A brief must be fully decomposed into granular nodes, each carrying everything needed for construction, error handling, model performance tracking, performance options, portability, security, success and failing tests, typed inputs and outputs, alternate approaches, logging, refactoring opportunities, and anything else useful for profiling, debugging and analysis.

Method: four read-only review agents, one per area, each told to cite file:line and to write "unverified" when unsure. v1 was read in the main checkout. v2 was read on branch `feat/briefv2-planner-core` (worktree `~/OtherProjects/PMSLLC/gophermind-planner-core`, 108 commits ahead of `main`, not merged). No tests were run.

Caveat: these findings are the agents' reports. I have not re-read every cited line. Section 8 lists the claims that drive decisions and should be spot-checked before anything is built on them.

Abbreviations: v1 planner = `plantree` (+ `plan`, `export`) and the TUI `/project` flow. v1 executor = `phaseflow` Execute* plus `orchestrate` and `tui/execute.go`. v2 planner = `briefv2/planner` and friends. v2 executor = `briefv2/executor` and friends.

## 2. Side by side

| Concern | v1 | v2 | Verdict |
|---|---|---|---|
| Brief intake | Chunks the brief (12000 bytes) and plans chunk by chunk; sees only titles of earlier nodes | Parses requirements (C/A/F ids) by code; Contract built in passes with continuation | v2 |
| Decomposition | Pass 1 skeleton (phase, task, step titles), pass 2 specifies steps in batches of 6 | Contract per component, then Decompose in batches of 8 to function leaves | v2 |
| Node content | Step: description, target paths, criteria, test command | Function: contract (typed in/out, errors), tests, signatures, constraints, budget | v2, then extend (section 5) |
| Brief coverage | None. Requirements can vanish silently | Hard gate: every requirement is covered or listed as a gap; gaps stop the run | v2, then harden (section 4) |
| Questions to the human | Options 2 to 8, recommendations, dedup, bounded history, `ChangeAnswer` re-plans only affected steps | `QUESTION:` protocol, max 3 per call, answers cached; Clarify stage | v2 base, adopt v1's `ChangeAnswer` and option model |
| Approval | Bare `.outline-approved` marker | Rendered plan hashed; hash re-verified before any write | v2 |
| Derived vs model-written | Model writes most fields | Code derives waves, signatures, test commands, ids, status | v2 |
| Privacy | None | Scope per call; public providers see node scope only | v2 |
| Model fallback | `FallbackRunner` with candidate list | Router: tier chains, cooldowns, privacy, malformed repair | v2 |
| Verification of work | LLM judge on the same model; fails open on error | Real `gofmt`, `go build`, `go vet`, `go test -json` with strict pass rule; reply gate on the AST | v2 |
| File protection | None per task | Model never names a path; only `contract.file` is written; test files hash-checked; stray-file snapshot | v2 |
| Sandbox, proxy, secrets | Opt-in container, best-effort shell deny-list | `sandbox-exec` profile, allowlist proxy, scrubbing, exact env | v2 (with honest limits) |
| Wave scheduling | Concurrent waves (limit 4), wave 0 solo | Waves, one worker only | v2 logic, v1's concurrency idea later |
| Retry and revision | Retry rounds; reviser output is never applied (section 3) | Ladder with identical-reply guard, repair of reopened leaves, escalation | v2 |
| Integration checks | None | Per-wave build, vet, race tests; attribution to nodes by code | v2 |
| Acceptance | Tasks have criteria; no executable check | N derived from requirements; proof triples; `Acceptance passed: N of N` | v2 |
| Git | None | Restricted surface, work branch, ff-only | v2 |
| Resume | Reset `running` to `pending` | Adopt-on-disk, plan hash check, claim release | v2 |
| Run state | `assignments.json` (locked atomic Update) | SQLite blackboard, ledger, events, plus `_state/*.json` | Files (decision 3 of the unification spec) |
| Model tracking | Per model: attempts, pass rate, duration | Per call ledger with task type and node class; report by task type and model | v2, then extend (section 6) |
| Live view | File watcher to SSE (`task-status`, `task-attempt`, `wave-changed`, `run-report`), desktop consumer | Stderr lines; events not saved | v1's feed design, fed from files |

## 3. Defects found

Ranked by how much they hurt the merged goal.

### v1

1. **A revision never changes what runs.** `ApplyRevision` records the new definition and resets the task to pending (`phaseflow/revise.go:53-70`), but `Deliverable` and `Test` are never written to the task's description or criteria. The next attempt uses the old text. The reviser's work is discarded.
2. **"Done" is a self-judged answer.** No test, build or vet is run. The judge uses the task model's own client, and a verifier error or a malformed verdict counts as satisfied (`agent/verify.go:83-84`, `orchestrate/verify.go:31,41`).
3. **The planner can drop requirements silently.** No coverage check. A task can be created with zero steps (prompt rule, no code enforcement) and then dead-ends at approval. A question whose `affects` titles do not match is never injected into any prompt but still blocks approval.
4. **The export is lossy.** Step `depends_on` and `target_paths` are dropped, a task description is cut to 8000 bytes, and every task is exported without dependencies, so execution is serial.
5. **Repository facts are never written in production** (`WriteFacts` has no non-test caller), so pass 2 always sees "(not provided)".
6. **`contract_flagged` has no real producer,** and the retry-feedback loop is effectively unreachable on the TUI path.
7. **No file protection and no cross-process run lock** in the executor.

### v2

1. **Coverage checks that ids exist, not that they fit.** A model can map a feature to any function node and pass (the spec admits this). Requirement parsing reads only top-level bullets under Constraints and Acceptance and `###` headings under Features.
2. **Constraints are stamped on every node** instead of being mapped to the nodes that must obey them. A constraint can be "covered" by a root test alone.
3. **`model_tier` is never used to route a planning call** (every planner call is `TierStrong`, `planner/planner.go:203`), and `node_class` lives in `_state/classes.json`, not on the node.
4. **`assumptions` is a dead field.** It is prompted for in assume mode but is not in the decompose output list.
5. **Almost nothing is saved for debugging a bad run.** Events go to stderr only. Prompts and replies are stored as hashes only, and failure output is reduced to a class and test names. The one exception is the opt-in developer dump `GOPHERMIND_DEBUG_DUMP_DIR` (`planner/debugdump.go`), which is never set in graded runs, so a bad generation cannot normally be inspected afterwards.
6. **Some passes are unproven.** A root acceptance command that is not `go test` passes on exit 0. A truncated `go test` output skips the `-run` name check. `diff_only` reports `verified` with no commits. The red-before-green check only warns.
7. **State is split:** a global SQLite file but per-run `_state/executor.json`. A cleared database with `Wave0Done` true makes resume re-create every row as pending while git holds committed leaves (untested).
8. **One worker only** (`workerCount` returns 1; the parallel branch is unreachable). Large files: `leaf.go` 1297 lines, `acceptance.go` 1166, `start.go` 883.
9. **Neither system rewrites a node after failure.** v1's reviser output is unused; the v2 executor only adds hint notes and escalates, and the planner's revise stage is out of scope in the executor spec.
10. **Components and root have no leaf checks,** their descriptions come from the brief's first paragraph, and the coverage warnings do not block.

## 4. The merged design in one paragraph

Take the v2 planner and executor as the engine. Add the v1 pieces that v2 lacks: the question UX and targeted re-plan (`ChangeAnswer`), the file-watcher SSE feed for the live view, and concurrent waves later. Remove the v1 planner and executor from `/project`. Store all run state as files in the run folder (browsable). Make PhaseFlow a read-only view generated from that folder. Widen the node so one file holds the whole story of a function: why it exists, how to build it, how it can fail, how it was tested, who built it, and how it performed.

## 5. Target node model

The node schema has `additionalProperties: false`, so each field below must be added to `schema/task-node.schema.json`, to the decompose prompt's output list, and to the leaf-defect checks where code can validate it. Plan fields are written by the planner and are covered by the plan hash. Run fields are written by the executor into the sidecar `.state.json`, never into the node file.

### 5.1 Plan fields (new or changed)

| Field | Type | Filled by | Code validation |
|---|---|---|---|
| `node_class` | enum (pure, validation, handler, client, storage, concurrency, wiring, other) | decompose model | enum; moves from `_state/classes.json` onto the node |
| `requirement_ids[]` | C/A/F ids | coverage, written by code from the map | ids exist in `requirements.json`; every function has at least one |
| `rationale` | string | decompose | present |
| `construction.steps[]`, `construction.approach_chosen` | strings | decompose | present, minimum count |
| `alternatives[{approach, rejected_because}]` | objects | decompose | at least one for strong-tier nodes |
| `contract.inputs[].type_ref` | contract id or builtin | decompose | resolves against `contracts.json` |
| `contract.errors[].kind` and `.test` | enum; test name | decompose, testwriter | enum; the named test exists |
| `tests[].polarity` | enum success, negative, boundary, property | testwriter | enum; at least one negative per `errors` entry |
| `tests[].covers` | error index or input name | testwriter | exists |
| `portability{os, arch, go_min, cgo, build_tags, deps}` | object | decompose, or code from imports | imports vs `deps`; `go_min` vs `go.mod` |
| `security{trust_boundary, untrusted_inputs, authz, secret_use, threats[{threat, mitigation}]}` | object | decompose | `secret_use` vs the root's `secrets`; present for handler, client, storage |
| `performance{complexity, max_latency_ms, alloc_budget, concurrency, hot_path}` | object | decompose | present; units checkable |
| `observability{log_events[{level, msg, fields}], metrics, trace_span}` | object | decompose | present for non-pure classes; consistent with `conventions.logging` |
| `refactor_notes[{what, why, when}]` | objects | decompose, later the executor | present |
| `profile_hooks[]` | enum bench, pprof, trace | decompose | enum; a `bench` entry needs a benchmark in the test file |
| `assumptions[]`, `open_questions[]` | strings | decompose | `open_questions` empty once approved; add `assumptions` to the prompt output list |

Also required: run the leaf-defect checks on component and root nodes, route planning calls by `model_tier` or `node_class`, and let coverage map a feature only to nodes that carry its requirement id.

### 5.2 Run fields (sidecar `.state.json` and run folder)

| Field | Why |
|---|---|
| per attempt: tokens in and out, provider, model served, ladder position, verdict, failure class | attempt-to-call join and cost; today tokens exist only in the ledger |
| per attempt: where it failed (gate, scan, build, vet, test), failing test names | compare models by failure stage |
| per attempt: saved reply and the compiler or test output, scrubbed, as files under `<run>/attempts/<node>/<n>/` | debugging; today only hashes exist |
| per node: final model, attempts used, wall time, first-try pass | model performance by node class |
| per node: files changed and diff stat from the commit | profiling and review |
| run level: persisted `events.jsonl`, wave timings | timeline of a bad run |

The saved reply and test output conflict with the executor spec's rule R15 (output never stored). That rule exists to keep secrets and large text out of state. It is kept in spirit by scrubbing before write, using the existing scrubbing code, and by making storage a setting; the opt-in `GOPHERMIND_DEBUG_DUMP_DIR` dump is the existing precedent. John decided this on 2026-10-07 (section 10).

### 5.3 Reports

Add a node-class dimension to `report.json`, split attempts by failure stage per model, add first-pass rate by class, and add a cross-run summary as a scan of run folders only if asked.

## 6. What to keep, by source

**Keep from v1 (adopt into the merged system)**
- Question model: 2 to 8 options, recommendations validated against option labels, dedup by text, bounded answer history, `ChangeAnswer` re-planning only the affected steps and leaving started steps alone (`plantree/plan/questions*.go`).
- Prompt-injection hygiene: fenced-block neutralization of brief text and quoting of the decisions block as data (`prompt.go:23`, `decisions.go:67`). Check whether v2 already does this (unverified).
- The file-watcher to SSE feed with its non-blocking hub (`serve/pipeline_watch.go`). It fits file-based state directly.
- Optimistic concurrency on node updates and a cross-process write lock (`plantree/store.go:162`, `store.go:56-62`) as the model for the file backend.
- Wave concurrency (limit 4), after the mini's one-request-at-a-time constraint is lifted or a second backend exists.
- Revision-history archiving that preserves every model tried, and the report rule that a model never tried is not counted as a failure.

**Keep from v2 (everything in its strengths list)**
- Requirements parsed by code, coverage as a hard gate, derived-not-model-written fields, leaf checks with a repair loop, privacy scopes, approval hash, contract in passes, `QUESTION:` protocol, per-stage resume predicates, ledger rows with task type and node class.
- Executor: stub-and-swap with stray snapshot, identical-reply guard, code-only attribution, N from `requirements.json`, proof triples, single decision point for final status, adopt-on-disk resume, restricted git, exact env, AST reply gate, sandbox and proxy.

**Drop**
- v1: `phaseflow.Execute*` loop, `FallbackRunner`, the LLM-judge verifier, `assignments.json` as run state, the lossy legacy export, the bare approval marker, and the TUI `/project` v1 flow (after `/project` is moved to v2).
- v2: the SQLite backend and its opener; the unreachable parallel branch (or finish it); the stderr-only event path.

**Fix after the merge (new work)**
- Node richness (section 5), semantic coverage, a planner revise stage that rewrites a node (neither system has one), proof for non-`go test` acceptance commands, `diff_only` verification honesty, persisted events and attempt artifacts, splitting `leaf.go`, `acceptance.go` and `start.go`.

## 7. Decisions for John

1. **Order.** Recommended: (1) file-based run state, (2) node schema and prompt widening with the new validations, (3) `/project` on v2 with v1's question UX, (4) PhaseFlow view and live feed, (5) the planner revise stage, (6) parallel waves. Reason: state and schema are the base everything else writes to.
2. **Store attempt artifacts?** Recommended: yes, scrubbed, in `<run>/attempts/`, with a setting to turn it off. Changes executor spec rule R15.
3. **Node file size.** Section 5 makes a function node several times larger. Confirm that a larger node and a larger planning cost (more output per call on a slow local model) are acceptable, or choose which groups (security, performance, portability, observability, refactoring) to require on every node versus only for some node classes. Recommended: require all groups for strong-tier nodes and handler, client and storage classes; require rationale, errors, tests and observability on every node.
4. **Merge state.** Build on `feat/briefv2-planner-core` and merge it to `main` together, or merge first. Recommended: merge the branch to `main` first (108 commits; run the full test suite), so there is one line of history before the schema changes.
5. **Scope of deletion.** Retiring v1 from `/project` is in scope. Deleting the `plantree` and PhaseFlow executor packages is not, until a caller inventory (TUI, server pipeline, desktop app, `cmd`) is done.

## 8. Claims to spot-check before building

1. v1: `ApplyRevision` never feeds `Deliverable` or `Test` back into the task prompt (`phaseflow/revise.go:53-70`, and a grep for consumers).
2. v1: `agent/verify.go:83-84` and `orchestrate/verify.go:31,41` fail open.
3. v2: `workerCount` always returns 1 (`executor/schedule.go:82`).
4. v2: events are not persisted and `Blackboard.Watch` has no non-test caller.
5. v2: planner calls are all `TierStrong` (`planner/planner.go:203`).
6. v2: the resume-with-cleared-database scenario in defect 7 (no test seen).
7. v2: whether prompt-injection hygiene like v1's exists in the planner's prompts.
8. The v1 export's loss of `depends_on` and `target_paths` (`plantree/export`).

## 9. Effect on the unification spec

Done on 2026-10-07 after John's answers (section 10): `2026-10-07-planner-phaseflow-unification-design.md` now has the agreed order, the attempt artifacts in sub-project A's layout, node widening and the question model as sub-project B, and the v1 keepers under B and the PhaseFlow view.

## 10. Decisions recorded (John, 2026-10-07)

| Section 7 question | Answer |
|---|---|
| 1. Order | Use the recommended order: file state, node widening, `/project` on v2, PhaseFlow view, planner revise stage, parallel waves |
| 2. Attempt artifacts | Save them (scrubbed, in the run folder, with a setting to turn it off) |
| 3. Node size | Require all groups on every node, not only on some node classes |
| 4. Merge state | First answer: merge the branch to `main` first. Later the same day, after the main-is-docs-only arrangement and the active GOAL.md run came to light: do not merge, leave `main` docs-only, and finish the graded run before any of this work starts |

## 11. Corrections after reading CONTEXT.md

1. A GOAL.md graded run is active on the branch (executor fix waves open, `/project` not built, 0 attempts). Decided: finish it first; this work starts after.
2. `/project` on v2 is already specified and planned on the branch (`2026-09-30-project-single-entry-design.md`, 14 tasks). The merge does not need a new `/project` design; v1 is kept as `/plan-v1` until after the perfect release (that spec's ruling R2).
3. Defect 5 of v2 originally said nothing was saved; the opt-in `GOPHERMIND_DEBUG_DUMP_DIR` dump exists and is now noted there.
