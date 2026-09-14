# GopherMind PhaseFlow Workflow

Guide to working with PhaseFlow phases for spec-driven task execution in gophermind.

## What is PhaseFlow?

PhaseFlow is a native port of **github.com/jbrahy/metaphaseflow** (MIT, © 2025 Lex Christopherson) integrated into gophermind at `internal/phaseflow/`. It provides **deterministic, state-driven workflow execution** for multi-step tasks.

- **State**: Stored in `.planning/<PROJECT_NAME>/state.json`
- **User interface**: `gophermind phase <cmd>` or `/phase <cmd>` in TUI
- **Operations**: status, next, done, sync, archive

## Phase Commands

### `/phase status` — Show current state
```
$ /phase status
Project: go-pher-it-logo
Status: in-progress
Current phase: 3 (execute)
Phases:
  ✓ plan (done)
  ✓ design (done)
  → execute (in-progress, 2/5 steps)
  ○ verify (pending)
  ○ release (pending)
```

### `/phase next` — Advance to next phase
```
$ /phase next
Advancing from 'design' to 'execute'...
[agent runs with phase-specific prompt]
```

After execution completes, phase status is updated. If the phase has defined steps, gophermind executes them with agent assistance.

### `/phase done` — Mark current phase complete
```
$ /phase done
Marked 'execute' as done.
Next: /phase next to advance to verify
```

### `/phase archive` — Move completed phases to archive
```
$ /phase archive
Archived: plan, design, execute
Current: verify
```

### `/phase sync` — Resync state from disk
```
$ /phase sync
Synced from .planning/go-pher-it-logo/state.json
```

## Phase Spec Structure

A phase spec defines:
- **name** — phase identifier
- **description** — what this phase accomplishes
- **steps** — list of tasks (optional; phases can be agent-driven without explicit steps)
- **gate** — verification condition (optional)

Example from `docs/superpowers/plans/2026-07-22-go-pher-it-logo.md`:

```markdown
### Phase: Execute

Execute the logo tasks one at a time, with review gates between.

- [ ] Task 1: Create render-logo.sh script
- [ ] Task 2: Generate PNG outputs
- [ ] Task 3: Verify image quality
- [ ] Task 4: Regenerate iOS icon
- [ ] Task 5: Update documentation

**Gate**: All 5 tasks committed and green (tests pass).
```

## State Machine

```
pending → in-progress → done → archived
           ↑           ↓
           └─ blocked (on manual action)
```

State lives in `.planning/<PROJECT>/state.json`:

```json
{
  "project_name": "go-pher-it-logo",
  "status": "in-progress",
  "current_phase_index": 2,
  "phases": [
    {
      "name": "plan",
      "status": "done",
      "started_at": "2026-07-22T10:00:00Z",
      "completed_at": "2026-07-22T11:30:00Z"
    },
    {
      "name": "design",
      "status": "done",
      "started_at": "2026-07-22T11:30:00Z",
      "completed_at": "2026-07-22T14:00:00Z"
    },
    {
      "name": "execute",
      "status": "in-progress",
      "started_at": "2026-07-22T14:00:00Z",
      "completed_at": null,
      "tasks_completed": 2,
      "tasks_total": 5
    },
    ...
  ]
}
```

## Agent Execution in Phases

When you run `/phase next`, gophermind:

1. Reads the phase spec from the plan
2. Generates a **phase-specific system prompt** that includes:
   - The phase goals and description
   - All tasks/steps for that phase
   - Success criteria
   - Context from prior phases (from `.planning/<PROJECT>/context.md`)
3. Runs the agent with that prompt
4. Agent decomposes phase tasks and executes them
5. Updates phase state when done

Example phase prompt injection:

```
You are executing phase 3 of the "Go Pher It Logo" project: Execute

Phase Goal: Generate PNGs and update iOS icon

Tasks:
1. Create scripts/render-logo.sh (SVG rasterizer)
2. Generate design/gopher-it-1024.png (iOS icon)
3. Verify outputs with ImageMagick identify
4. Copy PNG to iOS app icon
5. Run make ios-test

Success criteria:
- All PNGs generated and valid
- iOS tests pass
- Commit includes all changes

Context from prior phases:
[design decisions, artifact paths, etc.]

Proceed with these tasks:
```

## Workflow Example: Logo Project

From `CONTEXT.md`, the "Go Pher It" logo project uses PhaseFlow:

```bash
# 1. View current state
/phase status

# 2. Current phase is 3 (execute), tasks 1-3 are done
# 3. Run next task
/phase next
# → Agent executes Task 4: Regenerate iOS icon

# 4. When task is done
/phase done

# 5. Archive completed phases and move to verify
/phase archive
/phase next
# → Verify phase starts

# 6. When all phases done
/phase done
```

## Creating a PhaseFlow Plan

To create a new PhaseFlow project:

1. **Create the plan document** at `docs/superpowers/plans/<date>-<name>.md`:
   ```markdown
   # Project: <Name>
   
   ## Overview
   <2-3 sentence description>
   
   ## Phases
   
   ### Phase 1: Plan
   <Phase description>
   - [ ] Task 1
   - [ ] Task 2
   
   ### Phase 2: Execute
   <Phase description>
   - [ ] Task 1
   - [ ] Task 2
   
   ...
   ```

2. **Initialize state** (first run of `/phase next` creates `.planning/` directory):
   ```bash
   gophermind phase init <project-name> docs/superpowers/plans/<date>-<name>.md
   ```

3. **Execute phases**:
   ```bash
   /phase status
   /phase next    # Move to first phase
   /phase next    # Move to next phase
   /phase done    # Mark complete
   ```

## Archiving & Cleanup

After a project completes:

```bash
# Archive all done phases
/phase archive

# View archived state
/phase status

# Optional: Move to docs/archive/
mv .planning/<PROJECT> docs/archive/<PROJECT>
git add docs/archive/<PROJECT>
git commit -m "Archive completed phase project"
```

## Troubleshooting

**"Phase state not found"** — `.planning/` directory doesn't exist. Run `/phase next` to initialize, or check the path with `ls -la .planning/`.

**"Phase is blocked"** — A previous phase failed or has an unmet gate condition. Check `/phase status` for details, fix the blocking issue, then run `/phase next` again.

**"Stale state after manual changes"** — Run `/phase sync` to re-read `.planning/state.json` from disk.

**"I want to skip a phase"** — Edit `.planning/<PROJECT>/state.json` manually (set phase `status: "done"`), then run `/phase sync` and `/phase next`.

## Related Docs

- **docs/superpowers/plans/** — All active phase projects
- **docs/CONTEXT-HANDOFF.md** — Session handoff with phase state
- **internal/phaseflow/** — Source code (state, execute, spec)
- **PROJECT.md** — Project conventions
