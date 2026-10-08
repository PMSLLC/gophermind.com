# Planner and PhaseFlow Bridge: Design

Status: draft for review. Date: 2026-10-07.
Scope: sub-project 1 of 3, the shared executor. Sections 1 and 2 were approved in conversation on 2026-10-07. Section 3 (CLI, scope, open items) is a draft awaiting approval.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (the planner) and `gophermind-lib/phaseflow` (the executor). Reference: `docs/briefv2/handoff/BUILD_PLAN.md` items 9 to 13 (the executor the planner spec says is owed).

## 1. Why this exists

John wants the planner (briefv2) tightly integrated with PhaseFlow: a brief goes in one end and a built, verified result comes out the other with no manual hand-off. "Tightly integrated" was answered as all of: a shared executor, shared state, and PhaseFlow commands that drive the planner.

That is three sub-projects. Each gets its own spec, plan, and build, in this order:

| # | Sub-project | Depends on |
|---|---|---|
| 1 | Shared executor: the approved briefv2 tree runs on PhaseFlow's wave loop | nothing; this document |
| 2 | Shared state: one source of truth instead of `.gophermind/<run>/` plus the SQLite blackboard plus `.planning/` | field mapping learned in 1 |
| 3 | Unified commands: `phase plan` calls the planner, `brief plan` hands off to `phase execute`, one flow for CLI and app | 1 and 2 |

This document covers sub-project 1 only. It also fills the "executor plan" that the planner spec lists as owed.

## 2. Decisions taken with John (2026-10-07)

| # | Question | Decision |
|---|---|---|
| 1 | What "tightly integrated" means | All of: shared executor, shared state, two-way commands (split into the three sub-projects above) |
| 2 | How to design it | Derive it from the logic already in the planner and PhaseFlow, not field by field with John |
| 3 | Who owns model fallback | The planner's router. PhaseFlow sees one attempt per task |

## 3. What the two systems already do

- PhaseFlow owns the wave loop, status writes, concurrency, crash recovery (`running` reset to `pending` on start), and revision rounds. It needs a `TaskRunner` with one method: `Run(ctx, Task) (status, detail, err)`.
- PhaseFlow `Task` fields: `id`, `phase`, `title`, `description`, `acceptance_criteria`, `agent`, `model`, `status`, plus additive `omitempty` fields `wave`, `depends_on`, `candidate_models`, `attempts`, `revision_rounds`, `is_contract`.
- The bridge pattern already exists: `plantree/export.ExportLegacy` turns a tree into `assignments.json`. It is the one package importing both `plantree` and `phaseflow`, so the dependency points one way.
- The planner already computes `wave` and `depends_on`. PhaseFlow recomputes waves only when tasks declare dependencies (`AssignWaves`), so the two computations must agree.
- The planner's `Wave 0` contract maps to PhaseFlow's `IsContract` task.
- Model fallback exists twice: PhaseFlow's `FallbackRunner` with `candidate_models`, and the planner's router with tier chains, cooldowns, and the privacy rule. Decision 3 settles it.

## 4. Design

### 4.1 Shape

1. A new package `gophermind-lib/briefv2/phasebridge`. It is the only package that imports both `briefv2/*` and `phaseflow`. Neither side imports the other.
2. **Exporter.** Turns an approved tree into `assignments.json`, one Task per function leaf. `depends_on`, `wave`, and the Wave 0 contract task (`is_contract`) carry over. It refuses to run unless `approval.json`'s plan hash matches the tree.
3. **No new fields on PhaseFlow's `Task`**, provided the node ID passes PhaseFlow's validator. The runner looks up the full node (contract, tests, scope) from the tree by ID. If the validator rejects the node ID format, the bridge writes a small ID map file instead. PhaseFlow stays unmodified either way.
4. **Runner** implements `TaskRunner`:
   - runs the implement call through the planner's router, so privacy scope and ledger rows still apply;
   - requires the node's tests to fail before the implementation exists, then pass after;
   - refuses edits to the files listed in `_state/test_files.json`;
   - returns `done` or one of the statuses in 4.2.
5. **Write-back.** Each result also updates the tree node and the blackboard row, so `brief status` and PhaseFlow's dashboard tell the same story.

### 4.2 Failure paths

| What happened | Status returned | Why |
|---|---|---|
| Tests failed before the implementation, then passed after | `done` | The normal path |
| Tests already pass before any implementation | `needs_revision` | The test cannot fail, so the test is the defect |
| Router retries used up and tests still fail | `needs_revision` | PhaseFlow's revision loop takes over |
| Model edited a protected test file | a failed attempt | The reason is recorded and the file is restored |
| Revision would need a changed signature or `depends_on` | `contract_flagged` | PhaseFlow stops the wave. A signature change alters the approved plan |
| Two revision rounds spent (`MaxRevisionRounds`) | `escalated` | The bridge calls the planner's `Escalate` gate; the run exits with code 4 |
| Every provider unavailable (`*ChainExhausted` after `max_wait`) | task returns to `pending`, run stops | Not the task's fault, so no revision round is burned. Resume later |

**Revision.** The planner's `revise:<node>` stage is the PhaseFlow `Reviser`. It runs at node scope, so the privacy rule is unchanged. It rewrites the node's body (contract text and tests) in the tree. PhaseFlow's `RevisionRounds` is the only round counter; the bridge copies it into the node's `revision` field.

**Attempts.** PhaseFlow records attempts only for its own `FallbackRunner`. The bridge runner is not one, so the bridge writes `Attempts` itself through the exported `phaseflow.Update`, built from the ledger rows. The end-of-run report depends on this.

**Resume.** PhaseFlow resets `running` to `pending` on start. The runner's first action is to release any stale blackboard claim for that node, so a killed run restarts cleanly.

### 4.3 Tests (all offline)

1. One table test per row of 4.2, using the fake provider.
2. Fixture brief, export, then `phaseflow.Execute` with a fake router: every task `done`, ledger rows match the recorded attempts, and the blackboard matches `assignments.json`.
3. Kill mid-wave, then resume: exactly one result per leaf.
4. The revision path and the escalation path end to end.
5. Wave agreement: for every fixture tree, the planner's waves equal what `AssignWaves` computes from the exported tasks.

## 5. CLI surface (draft, awaiting approval)

| Command | Does |
|---|---|
| `gophermind brief export <run-id>` | Writes `assignments.json` and the supporting files for an approved run. Runs nothing |
| `gophermind brief run <run-id>` | Exports if needed, then runs `phaseflow.ExecuteWithReviser` with the bridge runner and reviser. Concurrency comes from the config's `max_parallel_leaves` |
| `gophermind brief status <run-id>` | Unchanged. Reads the blackboard, which the bridge keeps current |

`brief run` accepts the planner's existing flags (`--allow-public`). Exit codes: 0 done, 1 error, 2 invalid brief, 3 waiting on a human, 4 an escalated leaf (the code the planner spec reserved for this plan).

## 6. Out of scope

- Sub-projects 2 and 3 (shared state, unified commands).
- Git landing, the network proxy, the roll-up report, the run service, the app screens, the Gantt view. These stay with the later plans the planner spec lists.
- Any change to the meaning of an existing PhaseFlow field.

## 7. Open items to settle during the spec review

1. **Proxy gate (blocking).** The planner spec, deviation P4, says the executor plan must not run commands before the harness proxy exists. The runner runs `go test`. Either the proxy lands first, or John approves running tests unproxied for this plan, with that written down.
2. **Decision E5.** The optional `Toolchain` variables so commands can run `go test` need the handoff author's confirmation before the executor plan.
3. **Node ID format.** Check that a briefv2 node ID passes PhaseFlow's `ValidatePlan`. If not, use the ID map file (section 4.1, item 3).
4. **Approval hash coverage.** This design assumes `approval.json`'s hash covers the plan summary (components, counts, waves) and not node bodies, so a revision does not invalidate approval. Verify against the planner code. If the hash covers bodies, the rule changes.
5. **Wave agreement.** Confirm the planner's wave numbering and `AssignWaves` agree for dependency-free function nodes (planner: `1 + max` over an empty set; PhaseFlow: wave 1).
6. **`ChainExhausted` handling.** Returning the task to `pending` and stopping the run relies on PhaseFlow's cancel path (an error wrapping `context.Canceled`). Confirm that stopping the whole run is wanted rather than failing the one task.
