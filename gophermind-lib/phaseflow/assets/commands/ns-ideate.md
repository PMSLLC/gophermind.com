---
name: phase-ideate
description: "exploration capture | explore sketch spike spec capture"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
requires: [capture, explore, sketch, spike, spec-phase]
---

Route to the appropriate exploration / capture skill based on the user's intent.
`phase-note`, `phase-add-todo`, `phase-add-backlog`, and `phase-plant-seed` were folded
into `phase-capture` (with `--note`, default, `--backlog`, `--seed` modes) by
#2790. The capture target lists pending todos via `--list`.

| User wants | Invoke |
|---|---|
| Explore an idea or opportunity | phase-explore |
| Sketch out a rough design or plan | phase-sketch |
| Time-boxed technical spike | phase-spike |
| Write a spec for a phase | phase-spec-phase |
| Capture a thought (todo / note / backlog / seed) | phase-capture |

Invoke the matched skill directly using the Skill tool.
