---
name: phase-workflow
description: "workflow | discuss plan execute verify phase progress"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
requires: [discuss-phase, spec-phase, plan-phase, execute-phase, verify-work, phase, progress, ultraplan-phase, plan-review-convergence]
---

Route to the appropriate phase-pipeline skill based on the user's intent.
Sub-skill names below are post-#2790 consolidated targets — `phase-phase`
absorbs the former add/insert/remove/edit-phase commands and `phase-progress`
absorbs the former next/do commands.

| User wants | Invoke |
|---|---|
| Gather context before planning | phase-discuss-phase |
| Clarify what a phase delivers | phase-spec-phase |
| Create a PLAN.md | phase-plan-phase |
| Execute plans in a phase | phase-execute-phase |
| Verify built features through UAT | phase-verify-work |
| Add / insert / remove / edit a phase | phase-phase |
| Advance to the next logical step | phase-progress |
| Offload planning to the ultraplan cloud | phase-ultraplan-phase |
| Cross-AI plan review convergence loop | phase-plan-review-convergence |

Invoke the matched skill directly using the Skill tool.
