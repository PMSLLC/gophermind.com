# Who uses v1: caller inventory for sub-project G

Date: 2026-10-08. Status: inventory for John. Read-only; no code was changed. Source: grep of the `main` checkout (the branch has the same v1 code).

Purpose: John wants the server, the desktop app and `/phase` upgraded to the one system, and v1 removed (umbrella spec, decision 12). This lists exactly what each of them uses.

## 1. v1 packages and their importers

| Package | Importers outside itself (non-test) |
|---|---|
| `gophermind-lib/plantree` (and `plan`, `export`) | `gophermind-lib/tui` (5 files); `plantree/export` itself bridges to `phaseflow` |
| `gophermind-lib/phaseflow` | `cmd/gophermind` (1 file: `phase.go`), `gophermind-lib/orchestrate` (7), `gophermind-lib/serve` (3), `gophermind-lib/tui` (7), `gophermind-osx` (6 files across `main.go`, `client`, `ui`, `pipelinepanel.go`) |
| `gophermind-lib/orchestrate` | `gophermind-lib/tui` (2) |

Not importers: the Wails app in `desktop/` (no pipeline code), `ios/` (no pipeline references), `gophermind-server` (it only wires `gophermind-lib/serve`).

## 2. The server (`gophermind-lib/serve`, wired by `gophermind-server`)

- It runs nothing from v1. It only **reads** `.planning/assignments.json` and serves three endpoints plus a dashboard page: `GET /pipeline/state`, `GET /pipeline/events` (SSE), `GET /pipeline/report`, `GET /pipeline` (`webhook.go:418-422`).
- Files: `pipeline.go` (state and report handlers, the hub), `pipeline_watch.go` (polls the file every 750 ms and publishes diffs), `events.go`.
- v1 symbols used: `phaseflow.Task`, `Attempt`, `RunReport`, `LoadAssignments`, `AssignmentsPath`, `BuildRunReport`, and the status constants (`StatusDone`, `StatusFailed`, `StatusNeedsRevision`, `StatusEscalated`, `StatusContractFlagged`).
- The wire shape is `phaseflow.Task` (id, phase, title, description, acceptance_criteria, agent, model, status, wave, depends_on, candidate_models, attempts, revision_rounds, is_contract) and `RunReport` (per-model attempts, passes, pass rate, durations).
- Event names: `task-status`, `task-attempt`, `wave-changed`, `run-report`. The hub is non-blocking with a per-subscriber buffer.

## 3. The desktop app (`gophermind-osx`, a separate Go module)

- It only **decodes** the server's JSON into `phaseflow` types: `client/service.go` (`PipelineState`, `PipelineReport`), `client/sse.go` (`Event.RunReport`), `ui/pipeline.go` (state held in `PipelineState`), `ui/pipelinepump.go`, `pipelinepanel.go` (formatting), `main.go:398` (fetch hook).
- So its dependence on v1 is the **wire contract plus the Go types it borrows**. It has no planner or executor of its own.
- Signed releases of the app are installed on machines; a contract change needs the server and the app to move together.

## 4. `/phase` (`cmd/gophermind/phase.go`, 145 lines, and the `phaseflow` Engine)

- It is PhaseFlow's own workflow, separate from the brief pipeline: `init`, `done`, `sync`, `archive`, `commands` act on `.planning/` Markdown and JSON (ROADMAP.md, STATE.md, PROJECT.md, `phases/`), and the loop steps `roadmap`, `plan <phase>`, `execute <phase>`, `verify <phase>`, `milestone` run the agent with embedded PhaseFlow prompts (`phaseflow/assets/`).
- Its input is a roadmap and phases, not a brief. Its state is the `.planning/` tree. That is a third planning model beside v1's `plantree` and v2's tree.

## 5. The TUI (`gophermind-lib/tui`)

- `/project` and `/project-execute` are v1: `project.go`, `questions.go`, `question_round.go`, `approve.go` use `plantree`; `execute.go` and `update.go` use `phaseflow` and `orchestrate`.
- The `/project` single-entry spec already plans to make `/project` v2 and to rename the v1 commands `/plan-v1` and `/plan-v1-execute` until after the perfect release.

## 6. What "upgrade to the latest" means for each

| Consumer | Upgrade | Depends on |
|---|---|---|
| Server | Serve the same three endpoints from the one system's run folder: nodes, `.runtime.json` files, `report.json`, `_state/events.jsonl`. The watcher follows the event log instead of polling a file | A (run state as files), D (the view) |
| Desktop app | Decode the new contract; show the richer data (node groups, decisions, attempt artifacts) where it helps. Ship with the server | the server's new contract |
| `/phase` | Open question 2 below | a decision from John |
| TUI `/project` | Becomes the single entry point (project-single-entry spec, 14 tasks); v1 commands renamed, then removed | C |
| v1 removal and rename | Delete `plantree`, the PhaseFlow executor and the v1 runner; drop the `briefv2` name | all of the above |

## 7. Decisions needed

1. **The wire contract.** (a) Keep today's JSON shape and map nodes to it (`phaseflow.Task` fields), so the app changes only where it imports the types; or (b) define one new contract around nodes (with a `schema_version`), richer, with the server and the app released together. Recommended: (b), because there is no v1 to stay compatible with once v1 is removed, both clients are yours, and the node carries the data the app would otherwise lack. Keep field names where the meaning is unchanged so the change is mostly additive.
2. **What `/phase` becomes.** (a) A thin front end over the one system: `phase roadmap` and `phase plan` feed the planner, `phase execute` runs the executor, `phase verify` runs acceptance, `phase milestone` archives a run, and ROADMAP.md and STATE.md are generated views; (b) keep PhaseFlow as a separate manual workflow (a second planning model, against decision 12); or (c) retire `/phase`. Recommended: (a). The open design point is that `/phase` starts from a roadmap and the pipeline from a brief; the mapping is to treat a phase as a brief section.
