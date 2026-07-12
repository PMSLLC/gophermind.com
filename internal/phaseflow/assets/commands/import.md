---
name: phase:import
description: Ingest external plans with conflict detection against project decisions before writing anything.
argument-hint: "--from <filepath> | --from-phaseflow2"
allowed-tools:
  - Read
  - Write
  - Edit
  - Bash
  - Glob
  - Grep
  - AskUserQuestion
  - Agent
---

<objective>
Import external plan files into the PhaseFlow planning system with conflict detection against PROJECT.md decisions.

- **--from**: Import an external plan file, detect conflicts, write as PhaseFlow PLAN.md, validate via phase-plan-checker.
- **--from-phaseflow2**: Reverse-migrate a PhaseFlow-2 project (`.phase/` directory) back to PhaseFlow v1 (`.planning/`) format. Runs `phase-tools.cjs from-phaseflow2`. Pass `--path <dir>` to migrate a project at a different path.
</objective>

<execution_context>
@~/.claude/phaseflow/workflows/import.md
@~/.claude/phaseflow/references/ui-brand.md
@~/.claude/phaseflow/references/gate-prompts.md
@~/.claude/phaseflow/references/doc-conflict-engine.md
</execution_context>

<context>
$ARGUMENTS
</context>

<process>
If `--from-phaseflow2` is in $ARGUMENTS:
Run: `node "$HOME/.claude/phaseflow/bin/phase-tools.cjs" from-phaseflow2`
Pass `--path <dir>` if provided. Present the migration result to the user.
Stop here (do not run the standard import workflow).

Otherwise, execute the import workflow end-to-end.
</process>
