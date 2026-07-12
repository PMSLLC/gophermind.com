---
name: phase-manage
description: "config workspace | workstreams thread update ship inbox"
argument-hint: ""
allowed-tools:
  - Read
  - Skill
requires: [config, workspace, workstreams, thread, pause-work, resume-work, update, ship, inbox, pr-branch, undo]
---

Route to the appropriate management skill based on the user's intent.
`phase-config` (settings + advanced + integrations + profile) and `phase-workspace`
(new + list + remove) are post-#2790 consolidated entries.

| User wants | Invoke |
|---|---|
| Configure PhaseFlow settings (basic / advanced / integrations / profile) | phase-config |
| Manage workspaces (create / list / remove) | phase-workspace |
| Manage parallel workstreams | phase-workstreams |
| Continue work in a fresh context thread | phase-thread |
| Pause current work | phase-pause-work |
| Resume paused work | phase-resume-work |
| Update the PhaseFlow installation | phase-update |
| Ship completed work | phase-ship |
| Process inbox items | phase-inbox |
| Create a clean PR branch | phase-pr-branch |
| Undo the last PhaseFlow action | phase-undo |

Invoke the matched skill directly using the Skill tool.
