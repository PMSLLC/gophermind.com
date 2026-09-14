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
Execute plans in a phase with wave-based parallelization, TDD-first workflow.

Orchestrator: discover plans, group into waves, spawn subagents. Each subagent handles its own plan end-to-end.

**Per plan: TDD-first**
1. Write tests first (acceptance criteria define what to test)
2. Implement code to pass tests
3. Verify tests pass, commit when done
4. Escalate if plan fails 2+ times

Flags: `--wave N` (execute only wave N), `--gaps-only` (fix plans only), `--interactive` (inline, no subagents).
</objective>

<context>
Phase: $ARGUMENTS

Flags active only if literal token in $ARGUMENTS: `--wave N`, `--gaps-only`, `--interactive`.
</context>

<process>
Execute end-to-end. Preserve workflow gates and state updates.
</process>
