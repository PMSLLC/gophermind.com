# Unifying the planner, the executor and PhaseFlow: overview

Last updated: 2026-10-08. Status: design and plans written, nothing built. This page is the map; each item links to the document that holds the detail.

## 1. What this is

John asked for the planner to be tightly integrated with PhaseFlow. Reading both systems showed there were two of everything (a v1 and a v2 planner, a v1 and a v2 executor, two stores of run state), so the work became: one planner, one executor, one store of run state, nodes that carry everything needed to build, debug and analyse them, and PhaseFlow as a view of that one system.

## 2. Where everything lives

| Thing | Where |
|---|---|
| These documents | the `main` checkout (`/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com`), which holds docs only by design |
| The code to change | branch `feat/briefv2-planner-core`, in the linked worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core` (108 commits ahead of `main`, not merged, one writer at a time) |
| Local session state | `CONTEXT.md` (gitignored) and the memory index under `~/.claude/projects/...gophermind-com/memory/` |

## 3. The documents

| Document | What it is | Status |
|---|---|---|
| `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md` | The umbrella design: decisions, the six sub-projects, sub-project A in full | draft for review |
| `docs/reviews/2026-10-07-planner-executor-v1-v2-review.md` | Four-agent review of v1 and v2: side-by-side table, ranked defects, keep and drop verdicts, the new node fields | review; eight agent claims still to spot-check (its section 8) |
| `docs/reviews/2026-10-07-grilling-and-the-question-model.md` | The `grilling` skill against the v1 and v2 question models | decided; refined by the B spec |
| `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md` | Sub-project B: the node groups, the Enrich stage, the question rounds, questions and answers in the tree | draft for review |
| `docs/reviews/2026-10-08-v1-caller-inventory.md` | Who uses v1 (server, desktop app, `/phase`, the TUI) and what upgrading each means; two decisions for John | inventory; decisions open |
| `docs/superpowers/plans/2026-10-07-filesystem-run-state.md` | Sub-project A plan: files instead of SQLite, attempt artifacts (9 tasks) | written, not started |
| `docs/superpowers/plans/2026-10-08-node-widening-and-question-model.md` | Sub-project B plan (13 tasks) | written, not started |
| `docs/GOAL.md` and `docs/superpowers/specs/2026-09-30-project-single-entry-design.md` (branch) | The graded run and `/project` as the single entry point, which this work must not disturb | active |

## 4. The seven sub-projects and their order

| # | Sub-project | Detail | State |
|---|---|---|---|
| A | Run state as browsable files in the run folder; no SQLite; scrubbed attempt artifacts | umbrella spec section 4; plan A | plan written |
| B | Node widening (every group on every node) and the question model (rounds, fact probe, confirm gate, decisions, questions and answers recorded in the tree) | B spec; plan B | plan written |
| C | `/project` plans and builds through v2 | already specified and planned on the branch (project-single-entry, 14 tasks) | not built |
| D | PhaseFlow as a read-only view of the run folder, reusing v1's file-watcher feed | umbrella spec section 8 | outline |
| E | A planner revise stage that rewrites a node after a failure (neither v1 nor v2 does this) | none yet | not specified |
| F | Parallel waves in the executor | none yet | not specified |
| G | Retire v1 and drop the version names (no v1 and v2): remove `plantree`, the PhaseFlow executor and the v1 runner, rename `briefv2`, move the server, the desktop app and `/phase` onto the one system | umbrella spec decision 12; needs a caller inventory | not specified; after the graded release |

Nothing in A to F starts until the GOAL.md graded run has finished (decision 11).

## 5. Decisions John has taken

| Date | Decision |
|---|---|
| 2026-10-07 | All of: shared executor, shared state, PhaseFlow commands driving the planner; split into sub-projects |
| 2026-10-07 | Design from what the two codebases already do, not field by field |
| 2026-10-07 | The planner's router owns model fallback |
| 2026-10-07 | One version: one planner, one executor, one state store |
| 2026-10-07 | Nodes and their run state are browsable files, not SQL |
| 2026-10-07 | Order: files, node widening with the question model, `/project` on v2, the PhaseFlow view, the revise stage, parallel waves |
| 2026-10-07 | Save attempt artifacts (scrubbed, in the run folder, with a setting to turn them off) |
| 2026-10-07 | Every node carries all groups |
| 2026-10-07 | Do not merge the branch; `main` stays docs-only |
| 2026-10-07 | Finish the GOAL.md graded run first |
| 2026-10-07 | Clarify as rounds with a dependency tree; the confirm-understanding gate; decision records; caps of 4 model calls and 30 questions |
| 2026-10-08 | One version means no v1 and v2: v1 is removed and the version names go (sub-project G); the rehearsal run `gm-2026-09-29-002` is not needed |
| 2026-10-08 | Explicit, validated not-applicable answers are allowed where the node-group table says so |
| 2026-10-08 | Stages that work on one node follow that node's `model_tier` |
| 2026-10-08 | Every question and answer is also recorded in the node tree |

## 6. What was found along the way

- The v2 executor already exists on the branch (wave scheduler, escalation ladder, sandbox, proxy, git landing, report), so the first design, which proposed building an executor on PhaseFlow's wave loop, was dropped.
- v1: a revision never changes what runs (`phaseflow/revise.go:53-70`); "done" is an LLM self-judgement that fails open; nothing checks that the brief's requirements reach the plan.
- v2: coverage checks that ids exist, not that they fit; planning calls ignore a node's `model_tier`; `assumptions` is a dead field; events are not saved; one worker only.
- Neither version rewrites a node after a failure.
- A node-scope call may reach a public provider, so Test-writer and implement prompts receive a projection of a node that excludes its reasoning groups and its decisions.

## 7. The graded run

`docs/GOAL.md` is a standing goal John set on 2026-09-29: GopherMind must build the AI Venture Studio server (`~/OtherProjects/AIVentureStudio`) from its brief, in one clean run of `/project`, using only the Mac mini's model. The product is AIVentureStudio; GopherMind is the tool that has to get good enough to build it.

- **One attempt** is one `/project` run on the brief, from a cleared target repo to a finished build, in one process with no restart, no resume and no hand edit. An attempt also fails if GopherMind printed an error, skipped or abandoned a task, or stopped to ask a human, or if the coverage report is not `Requirements covered: N of N`.
- **A failed attempt is dead.** It is recorded in `docs/goals/attempts/NN.md` (20 lines at most), the target repo is cleared, GopherMind is fixed (never the target repo), and a new attempt starts.
- **The run stops at a perfect release:** a clean attempt where `go build`, `go vet` and `go test -race` pass in the target repo, every acceptance bullet passes against the built server, the plan covers every requirement, the ledger shows which model did each call, and GopherMind committed the result on `main` there (then the orchestrator tags it `v0.1.0`).
- **Where it stands (from CONTEXT.md, 2026-09-30):** the v2 planner is built and proven on the real brief up to approval; the executor is built with fix waves still open; `/project` as the single entry point is specified and planned (14 tasks) but not built; there have been 0 graded attempts.

This is why the unification work waits: every change to the planner, the executor, the node schema or the run-state format would change what the graded run is graded on.

## 8. Open items for John

1. Sub-project G wire contract (keep today's JSON shape or define one new node-based contract) and what `/phase` becomes: see `docs/reviews/2026-10-08-v1-caller-inventory.md`, section 7.
2. After the graded run: revisit merging the branch, re-check the eight unverified review claims, then start A.
3. Sub-project G needs its own spec once those two are decided.
4. Enrich's cost on the real brief (136 function nodes in the rehearsal) is unmeasured; measure on the csvstat brief first.

## 9. How to resume

1. Read this page, then `CONTEXT.md` (the 2026-10-07 and 2026-10-08 sections at the end).
2. Confirm the graded run has finished (`docs/GOAL.md`, the worktree's `docs/goals/attempts/`).
3. Spot-check the review's section 8 claims.
4. Execute plan A, then plan B, subagent-driven (John's standing rule), on the branch, one writer at a time.
