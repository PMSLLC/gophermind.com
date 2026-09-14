---
name: phase-context
description: "codebase intelligence | map graphify docs learnings"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
requires: [map-codebase, graphify, docs-update, extract-learnings]
---

Route to the appropriate codebase-intelligence skill based on the user's intent.
`phase-scan` and `phase-intel` were folded into `phase-map-codebase` flags by #2790.

| User wants | Invoke |
|---|---|
| Map the full codebase structure | phase-map-codebase |
| Quick lightweight codebase scan | phase-map-codebase --fast |
| Query mapped intelligence files | phase-map-codebase --query |
| Generate a knowledge graph | phase-graphify |
| Update project documentation | phase-docs-update |
| Extract learnings from a completed phase | phase-extract-learnings |

Invoke the matched skill directly using the Skill tool.
