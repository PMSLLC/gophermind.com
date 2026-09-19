# Brief workflow: design

Approved by the project owner on 2026-09-19. Supersedes the delivery order of
`2026-09-19-assignments-tree-v4-spec.md` (Slice 1) while reusing its tree
layout, statuses and packet idea. The v4 spec and its addendum remain the
target for the deferred layers.

## Goal

Upload a brief to gophermind. It reads the brief, breaks it into phases,
tasks and steps, keeps a terse running overview, asks its questions in one
round, and after any error resumes from the exact unfinished node.

## Why this exists

`/project <name> <brief>` overflowed a 98,304-token window at iteration 11
because one interview turn read whole documents. The context fixes are
already committed (`2e761ac`, `47a94c6`, `51ba527`). This design removes the
root cause: no pass ever needs the whole brief or the whole plan in context.

## Principles

1. **Fresh context per pass.** Each pass is a new `agent.Agent` with no
   history. It receives the instruction, the overview, and one bounded slice
   of work. Nothing else.
2. **The tree on disk is the state.** `.planning/plan/` holds the plan.
   Resuming means reading it and deriving the next action. No cursor is
   trusted.
3. **Terse overview.** `overview.md`, capped near 1,500 tokens, rewritten by
   each pass and re-compressed when it grows past the cap.
4. **Questions are collected, then asked once**, with options, multi-select,
   a recommendation, and free text.
5. **The tree is canonical.** `assignments.json` is generated from it at
   approval, replacing the model writing that file directly.

## Statuses

Lifecycle `status` (leaves only; structural nodes derive theirs):
`untouched | reviewed | in_progress | needs_revision | completed | blocked |
delayed | skipped | failed | escalated`.

Planning `stage` (separate, per v4 addendum section 1):
`skeleton | inspected | drafted | awaiting_answers | needs_reconciliation |
approved`.

- `inspected` = read or assessed, not approved.
- `reviewed` = the specification was approved. In this version the human
  approves the whole plan; independent-reviewer authority is a later layer.
- Opening a node never downgrades its stage.

## Tree

Layout and ids follow v4 section 4: `phase-NNN`, `task-NNN`, `step-NNN`,
three digits, each at `<container>/<id>/meta.json`, root at `plan.json`.
Steps are the leaves. Substeps and composites are a later layer.

## Milestones

See `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md`.

## Deferred (v4 layers, added later without changing these documents)

Reviewer-session authority, the rollback slot and commit manifest, the
1,000-leaf benchmarks, the 17 typed leaf specs (this version has one generic
leaf spec), fenced claims, promotion, live legacy cutover.

## Non-goals

A new executor, a new dashboard, per-leaf dispatch.
