---
name: phase-project
description: "project lifecycle | milestones audits summary"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
---

Route to the appropriate project / milestone skill based on the user's intent.
`phase-plan-milestone-gaps` was deleted by #2790 — gap planning now happens
inline as part of `phase-audit-milestone`'s output.

| User wants | Invoke |
|---|---|
| Start a new project | phase-new-project |
| Create a new milestone | phase-new-milestone |
| Complete the current milestone | phase-complete-milestone |
| Audit a milestone for issues | phase-audit-milestone |
| Summarize milestone status | phase-milestone-summary |

Invoke the matched skill directly using the Skill tool.
