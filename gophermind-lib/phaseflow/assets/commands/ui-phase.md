---
name: phase:ui-phase
description: Generate UI design contract (UI-SPEC.md) for frontend phases
argument-hint: "[phase]"
allowed-tools:
  - Read
  - Write
  - Bash
  - Glob
  - Grep
  - Agent
  - WebFetch
  - AskUserQuestion
  - mcp__context7__*
requires: [phase]
---
<objective>
Create a UI design contract (UI-SPEC.md) for a frontend phase.
Orchestrates phase-ui-researcher and phase-ui-checker.
Flow: Validate → Research UI → Verify UI-SPEC → Done
</objective>

<execution_context>
@~/.claude/phaseflow/workflows/ui-phase.md
@~/.claude/phaseflow/references/ui-brand.md
</execution_context>

<context>
Phase number: $ARGUMENTS — optional, auto-detects next unplanned phase if omitted.
</context>

<process>
Execute end-to-end.
Preserve all workflow gates.
</process>
