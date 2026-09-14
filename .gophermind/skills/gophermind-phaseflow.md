# GopherMind PhaseFlow Workflow

Use only for explicitly requested PhaseFlow work. Follow the shared rules.
The native workflow lives at `gophermind-lib/phaseflow/` and project state at
`.planning/<PROJECT>/state.json`; verify both in the checkout.

## Verify the interface before changing state

Inspect the current plan, selected project, persisted state, relevant transition
code/tests, and CLI/TUI help. Do not assume the CLI and TUI are exact aliases.
Run slash commands in the TUI, not a shell. Use supported CLI syntax for automation.

| Documented operation | Verify before relying on it |
|---|---|
| `status` | Selected project and whether displayed state is current |
| `next` | Whether it advances a phase, resumes work, or executes a task; side effects and gate enforcement |
| `done` | Completion criteria, state changes, and evidence requirements |
| `sync` | Which project/state is read and whether anything is written |
| `archive` | What is moved/retained and how history remains accessible |
| `init` | Whether it exists, exact syntax, and overwrite behavior |

The original examples conflict about `next` and initialization. Resolve this from
implementation/help; do not choose whichever meaning makes progress easiest.
Do not assume `next` initializes state or invent an initialization command.

## Execute with evidence

1. Confirm the intended project, plan, current phase/task, and authorized scope.
   Distinguish phase completion from one task completing. Read success criteria,
   prerequisites, and retained context, including `context.md` when present.
2. Establish which gates apply and how to observe them. Verify prerequisites before
   advancing. A missing required gate result is not a pass.
3. Execute only the authorized phase/task range using the verified interface.
   Keep required artifact paths and check results. A plan is not authorization
   to deploy, release, or commit.
4. Verify outcomes before requesting completion. Do not equate an agent saying
   "done" with persisted completion or passing tests. Read state again and compare
   the expected transition, progress, and artifacts.
5. Archive only eligible completed work after confirming command scope. Preserve
   the plan, evidence, and history; do not manually move active state as cleanup.

When blocked, record the unmet gate/action and resume only after resolving it.
A state-write failure after external work is not permission to rerun that work:
inspect side effects and idempotency first. Stop on unexpected project selection,
conflicting writers, corrupt state, or unverified transition semantics.

## Plans and recovery

Use existing plan conventions; verify the schema/parser rather than treating an
illustrative Markdown or JSON example as a contract. Define phase goals, tasks,
gates, dependencies, and retained evidence without inventing unsupported fields.
Verify the supported state vocabulary instead of assuming a transition diagram.

Prefer supported recovery commands. Never set `status: "done"` merely to skip a
gate. For an explicitly authorized repair, preserve a backup, prevent concurrent
writes, validate the actual schema/invariants, apply the smallest correction, and
record the reason without falsifying completion evidence. Re-read through the
supported interface before resuming.

Report project/state path, operation, before/after phase and task status, gate
evidence, and the next action or blocker. Do not mark unverified work complete.
