---
name: phase:ai-integration-phase
description: Generate an AI-SPEC.md design contract for phases that involve building AI systems.
argument-hint: "[phase number]"
allowed-tools:
  - Read
  - Write
  - Bash
  - Glob
  - Grep
  - Agent
  - WebFetch
  - WebSearch
  - AskUserQuestion
  - mcp__context7__*
requires: [phase]
---
<objective>
Create an AI design contract (AI-SPEC.md) for a phase involving AI system development.
Orchestrates phase-framework-selector → phase-ai-researcher → phase-domain-researcher → phase-eval-planner.
Flow: Select Framework → Research Docs → Research Domain → Design Eval Strategy → Done
</objective>

<execution_context>
@~/.claude/phaseflow/workflows/ai-integration-phase.md
@~/.claude/phaseflow/references/ai-frameworks.md
@~/.claude/phaseflow/references/ai-evals.md
</execution_context>

<context>
Phase number: $ARGUMENTS — optional, auto-detects next unplanned phase if omitted.
</context>

<process>
Execute end-to-end.
Preserve all workflow gates.
</process>
