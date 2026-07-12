---
name: phase:explore
description: Socratic ideation and idea routing — think through ideas before committing to plans
allowed-tools:
  - Read
  - Write
  - Bash
  - Grep
  - Glob
  - Agent
  - AskUserQuestion
---
<objective>
Open-ended Socratic ideation session. Guides the developer through exploring an idea via
probing questions, optionally spawns research, then routes outputs to the appropriate PhaseFlow
artifacts (notes, todos, seeds, research questions, requirements, or new phases).

Accepts an optional topic argument: `/phase:explore authentication strategy`
</objective>

<execution_context>
@~/.claude/phaseflow/workflows/explore.md
</execution_context>

<process>
Execute end-to-end.
</process>
