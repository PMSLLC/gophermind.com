---
name: phase:execute-phase
description: Execute all plans in a phase with wave-based parallelization
argument-hint: "<phase-number> [--wave N] [--gaps-only] [--interactive] [--tdd]"
allowed-tools:
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - Bash
  - Agent
  - TodoWrite
  - AskUserQuestion
requires: [phase, verify-work]
---
<objective>
Execute plans with wave-based parallelization, TDD-first: tests first, then code, then verify. Escalate after 2+ failures.

Orchestrator: discover, group into waves, spawn subagents (each handles one plan). Wave verification: completion only when no incomplete plans remain after selected wave finishes.

TDD: write tests (< 30 min per subtask), implement to pass, verify, commit. Ask user to decompose large plans into subtasks.

Flags: `--wave N` (execute only wave N), `--gaps-only` (fix plans only), `--interactive` (inline, no subagents).
</objective>

<context>
Phase: $ARGUMENTS

Flags active only if literal token in $ARGUMENTS: `--wave N`, `--gaps-only`, `--interactive`.
</context>

<process>
Execute end-to-end. Preserve workflow gates and state updates.
</process>
