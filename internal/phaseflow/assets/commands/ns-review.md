---
name: phase-quality
description: "quality gates | code review debug audit security eval ui"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
requires: [code-review, audit-uat, secure-phase, eval-review, ui-review, validate-phase, debug, forensics]
---

Route to the appropriate quality / review skill based on the user's intent.
`phase-code-review-fix` was absorbed by `phase-code-review --fix` in #2790.

| User wants | Invoke |
|---|---|
| Review code for quality and correctness | phase-code-review |
| Auto-fix code review findings | phase-code-review --fix |
| Audit UAT / acceptance testing | phase-audit-uat |
| Security review of a phase | phase-secure-phase |
| Evaluate AI response quality | phase-eval-review |
| Review UI for design and accessibility | phase-ui-review |
| Validate phase outputs | phase-validate-phase |
| Debug a failing feature or error | phase-debug |
| Forensic investigation of a broken system | phase-forensics |

Invoke the matched skill directly using the Skill tool.
