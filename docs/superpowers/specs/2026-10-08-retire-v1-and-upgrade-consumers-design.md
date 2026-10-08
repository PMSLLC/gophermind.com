# Retire v1 and Upgrade the Server, the Desktop App and `/phase`: Design (sub-project G, with D)

Status: draft for review. Date: 2026-10-08.
Scope: sub-projects D and G of `2026-10-07-planner-phaseflow-unification-design.md`. After this work there is one planner, one executor and one store of run state; the server and the desktop app show that one system through one new contract; `/phase` and every v1 package are gone; the version names are gone.
Builds on: `docs/reviews/2026-10-08-v1-caller-inventory.md` (who uses v1 and how), the unification spec (decisions 12 to 15), A (`2026-10-07-filesystem-run-state.md`: run state as files), B (`2026-10-07-node-widening-and-question-model-design.md`: widened nodes, questions and answers in the tree) and the `/project` single-entry spec (C, on branch `feat/briefv2-planner-core`).
Sequencing: written, not started. Nothing here starts until the GOAL.md graded run has finished (decision 11). Dependency order inside the whole effort: A, B, C, then D and G (this document).

## 1. Why this exists

John asked for the server, the desktop app and `/phase` to use the latest system (2026-10-08), and for "one version" to mean no v1 and v2 (decision 12). The inventory found that all three still sit on v1 types and files:

- the server reads `.planning/assignments.json` and serves it as `phaseflow.Task` and `RunReport`;
- the desktop app only decodes those types from the server;
- `/phase` is PhaseFlow's own roadmap workflow, a third planning model.

## 2. Decisions

| # | Question | Decision | Source |
|---|---|---|---|
| 12 | One version | No v1 and v2: v1 is removed and the version names go | John, 2026-10-08 |
| 14 | The wire contract | One new node-based contract with a `schema_version`; server and app released together; no compatibility layer for the old shape | John, 2026-10-08 ("1 b") |
| 15 | `/phase` | Retired. PhaseFlow's workflow, its embedded prompts and its `.planning/` files are removed | John, 2026-10-08 ("2 c") |
| 16 | Consequence for D | Because `/phase` is retired, nothing generates `.planning/ROADMAP.md`, `STATE.md` or `assignments.json` any more. Sub-project D becomes the **run view**: one library and the pipeline endpoints that read the run folder | follows from 15 |

## 3. Goals and non-goals

Goals:

- A library, `runview`, that reads a run folder (plan nodes, runtime files, event log, report, decisions, attempt artifacts) and follows it live, and is the only place that knows the folder's layout for readers.
- The pipeline endpoints serve the new contract from `runview`; the desktop app consumes it through the same types.
- `/phase`, `plantree`, the PhaseFlow executor and the v1 runner are deleted, with every reference, and the `briefv2` name is dropped.
- A person who runs `gophermind phase` after the removal is told, once per release, what replaced it.

Non-goals:

- Any new way to start a run from the server or the app. The server and the app stay read-only for the pipeline, as today.
- A migration of old `.planning/` projects or old `assignments.json` files. They are not read.
- Changes to the iOS app (it has no pipeline code) or to the Wails `desktop/` app beyond one help text (section 8).
- A compatibility period for the old endpoint shape (decision 14).

## 4. The contract (`schema_version` 1)

Every JSON body carries `"schema_version": 1`; every response carries the header `X-Gophermind-Pipeline-Schema: 1`. A client that sees a different number shows "this server and this app disagree; update the older one" instead of a broken panel. The paths are unchanged so the dashboard and the auth wrapper (`pipeWrap`, bearer plus HMAC) stay as they are; the shapes are new.

| Endpoint | Returns |
|---|---|
| `GET /pipeline/runs` | `{runs: [RunSummary]}`, newest first |
| `GET /pipeline/state?run=<id>` | `{generated_at, run: RunSummary, nodes: [NodeSummary]}`; `run` defaults to the newest |
| `GET /pipeline/node?run=<id>&id=<node>` | `{node, runtime, artifacts}`: the full plan node (all groups and its embedded `decisions`), its runtime file, and the list of saved attempt artifacts |
| `GET /pipeline/artifact?run=<id>&id=<node>&attempt=<n>&file=<name>` | the text of one saved artifact; `file` is one of `reply.go`, `reply.txt`, `check-output.txt`, `attempt.json` (already scrubbed by the executor) |
| `GET /pipeline/decisions?run=<id>` | `{decisions: [record]}`: the root node's records, the whole conversation |
| `GET /pipeline/report?run=<id>` | the executor's `report.json` verbatim; while a run is still going, a partial report built from the call log and the runtime files with `"complete": false` |
| `GET /pipeline/events?run=<id>&since=<seq>` | an SSE stream, section 4.3 |
| `GET /pipeline` | the dashboard page (HTML shell, no data), rewritten for nodes |

### 4.1 `RunSummary`

`run_id`, `title`, `repo_name` (the folder name, never a path), `status` (`planning`, `waiting`, `planned`, `building`, `verified`, `failed`, `escalated`, `interrupted`, `harness_fault`), `stop_reason`, `planned_at`, `started_at`, `updated_at`, `waves` (count), `counts` (`nodes`, and a map from node status to count), `coverage` (`covered`, `total` from the requirements).

### 4.2 `NodeSummary`

`id`, `kind` (`root`, `component`, `function`), `parent`, `title`, `status` (the blackboard statuses: pending, ready, claimed, in_progress, verified, failed, needs_revision, escalated), `wave`, `model_tier`, `node_class`, `depends_on`, `requirement_ids`, `revision`, `claimed_by`, `attempt_count`, `last_attempt` (`model`, `provider`, `verdict`, `failure_reason`, `duration_ms`, `started_at`), `result` (`commit`, `files_changed`), `tests` (`total`). The groups and the embedded decisions are deliberately not in the list view; they come from `/pipeline/node`.

### 4.3 Events

Each event is `id: <seq>`, `event: <type>`, `data: <json>`. `seq` is the 1-based line number of `_state/events.jsonl`. A client that reconnects sends `Last-Event-ID` (or `since`) and the server replays from that line, then follows. This is new: the old hub could only drop frames for a slow client. Types:

| Event | Data |
|---|---|
| `node-status` | `{id, status, wave}` |
| `node-attempt` | `{id, attempt}` where `attempt` is the `last_attempt` shape plus `order`, `revision`, `tests_passed`, `tests_total`, `failed_tests` |
| `node-result` | `{id, result}` |
| `wave-changed` | `{wave, state}` (`started`, `finished`) |
| `run-status` | `{status, stop_reason}` |
| `run-report` | the report summary (at finish) |

Comment lines (`: keepalive`) every 15 seconds, as today. The number of concurrent event streams is capped (16 by default, a server setting); a stream over the cap gets 503.

## 5. The `runview` library

New package `gophermind-lib/runview`, importable by the server and by the desktop app (which already depends on `gophermind-lib`). It owns the wire types of section 4 (`types.go`) and reads the run folder:

- `Runs(root string) ([]RunSummary, error)`: scans `<root>/.gophermind/*/` for folders holding `brief.md`; derives `status` from the files (no `approval.json`: planning or waiting; approval and no executor state: planned; `_state/executor.json` and no `report.json`: building; `report.json`: its status and stop reason).
- `Open(root, runID string) (*View, error)` with `State()`, `Node(id)`, `Decisions()`, `Report()`, `ArtifactNames(id)`, `Artifact(id, attempt, file)`. It reads plan nodes through `tree.Store.Load`, runtime state through the file blackboard, and the report through the `report` package; it never writes.
- `(*View).Follow(ctx, since int64) (<-chan Event, error)`: reads `_state/events.jsonl` from line `since`, turns each line into a wire event by reading the node's runtime file, then polls (250 ms) for more.
- Path safety: run ids match the planner's run id pattern; node ids match `^[a-z0-9][a-z0-9-]*$`; `file` is checked against the four names; `attempt` is an integer; a folder that resolves outside `<root>/.gophermind` is refused.

## 6. The server

- `serve.PipelineDeps` becomes `{Root string, MaxStreams int}`; the hub, `StartPipelineWatcher`, `PipelineHub` and `pipeline_watch.go` are deleted. Each event stream follows the file itself through `runview`.
- `pipeline.go` is rewritten to the endpoints of section 4; `pipelineDashboardHandler`'s page is rewritten for nodes (a tree by component, a report block, a decisions list, a node detail pane).
- Wiring changes in `gophermind-server/server.go` and `cmd/gophermind/main.go` (the lines that create the hub and start the watcher go; `Root` is the repo root as today, and the runs are found under its `.gophermind/`).
- The file `serve/events.go` comment that mentions `phaseflow.RunReport` is updated.
- Deployment follows the standing rule: the server runs on the Mac mini as a launchd job; deploy with `git pull` in the checkout there and `sudo launchctl kickstart -k system/com.home.<service>`, then verify the endpoints with a bearer token.

## 7. The desktop app (`gophermind-osx`)

- `client/service.go`: `PipelineRuns`, `PipelineState(run)`, `PipelineNode`, `PipelineReport`, `PipelineDecisions`; `client/sse.go`: decode the new events, remember the last event id, resume on reconnect; a schema mismatch is a typed error.
- `ui/pipeline.go` holds the run summary and the node tree instead of tasks; `ui/pipelinepump.go` applies the new events; `pipelinepanel.go` formats: a run header (status, waves, coverage), the node tree grouped by component with status, wave, model and attempt count per function, a report block (per task type and model), and a detail view for the selected node (its groups, its decisions, its attempts, and the saved check output of a failed attempt). `main.go:398` uses the new fetch.
- No `gophermind-lib/phaseflow` import remains in the module; the types come from `runview`.
- `e2e_pipeline_test.go` runs against a server serving a real run folder.
- The app is a signed, notarised release installed on machines; it ships in the same window as the server. A server and an app that disagree show the schema message of section 4, and chat keeps working because the pipeline panel is the only part that reads the contract.

## 8. Removing v1 and retiring `/phase`

Removed (after `/project` is the single entry point, sub-project C, and `grep` finds no importer left):

| Removed | Notes |
|---|---|
| `gophermind-lib/plantree/` (with `plan/` and `export/`) | the v1 planner |
| `gophermind-lib/phaseflow/` (with `assets/`, the embedded prompts and `LICENSE.upstream`) | the PhaseFlow engine, executor, workflow prompts and the `.planning/` model |
| `gophermind-lib/orchestrate/` | the v1 task runner, fallback and reviser; the executor uses the router |
| `cmd/gophermind/phase.go` and the dispatch in `main.go` | replaced by a notice (below) |
| in `gophermind-lib/tui/`: `commands.go` (`handlePhaseCommand`, `phaseSlashHelp`), `commands_registry.go` entries for `/phase`, `/project-execute`, `/plan-v1`, `/plan-v1-execute`, `/questions`, and the files `approve.go`, `questions.go`, `question_round.go`, `execute.go` and what remains of the v1 flow in `project.go`, `update.go`, `model.go` | `/project` is the v2 flow by then |
| `.gophermind/skills/gophermind-phaseflow.md` and the PhaseFlow references in `.gophermind/skills-packaging/` | the skill pack is regenerated without them |
| `README.md`, `PROJECT.md`, `GOPHERMIND.toml`, `INDEX.md`, `docs/ARCHITECTURE-INDEX.md`, `docs/GOPHERMIND-SETUP.md`, `docs/CONTEXT-HANDOFF.md` | text and index entries for PhaseFlow and `/phase` updated or removed |
| `desktop/frontend/src/App.tsx:564` | one example prompt mentions `gophermind phase validate`; reworded |

`CHANGELOG.md` gets a breaking-change entry: `gophermind phase` and `/phase` are removed; PhaseFlow's roadmap workflow has no replacement; the brief pipeline (`gophermind brief ...`, `/project`) is the one way to plan and build. `CREDITS.md` keeps a line thanking PhaseFlow's author, marked as no longer part of the code (open item 2).

**The notice.** For one release `gophermind phase ...` and `/phase` print: `gophermind phase was removed in <version>. Plan and build a brief with "gophermind brief plan" and "gophermind brief run", or with /project. Existing .planning/ folders are not read.` and exit 2. The release after that removes the stub.

## 9. Dropping the version names

One mechanical commit, last: `gophermind-lib/briefv2/` becomes `gophermind-lib/build/` (the planner, executor, router, ledger, blackboard, tree and the rest keep their sub-package names; the brief parser package keeps `brief`), imports and `go.work` are rewritten, and `docs/briefv2/` becomes `docs/build/`. The command names do not change (`gophermind brief ...`, `/project`). The name `build` is a proposal (open item 1). Historical documents keep the old names; the overview page maps them.

## 10. Order of work inside G and D

| Step | What | Depends on |
|---|---|---|
| G1 | `runview` library and its tests against real run folders | A, B (nodes with groups, decisions) |
| G2 | Server endpoints, dashboard, removal of the hub and the watcher | G1 |
| G3 | Desktop app | G2 |
| G4 | Release the server and the app together | G2, G3 |
| G5 | Remove v1 (tui, `plantree`, `orchestrate`) | C, G4 |
| G6 | Retire `/phase`: stub, delete `phaseflow`, docs and skills | G5 |
| G7 | Rename `briefv2` and `docs/briefv2` | G6 |

## 11. Tests

- `runview`: a run folder produced by the offline planner and executor fixtures; every endpoint's data; status derivation for each run state; `Follow` replays from a given line and then follows new lines; a corrupt event line is skipped and reported, never fatal; path-safety cases (traversal in run, node, file and attempt; a symlinked run folder).
- Server (`httptest`): each endpoint with and without a token; the schema header and field; run selection; `since` replay; the stream cap; the artifact endpoint refuses anything but the four names; a run with no report gives a partial report.
- Desktop app: client decoding against golden JSON from the server tests; the schema-mismatch message; panel formatting for a tree with failed, escalated and verified nodes; reconnect resumes from the last event id.
- Removal gates: a test fails if any package imports `plantree`, `phaseflow` or `orchestrate`; `go build ./...` and `go vet ./...` pass in the root module, `gophermind-lib` and `gophermind-osx`; a grep gate finds no `briefv2` path after G7; the stub prints the notice and exits 2.

## 12. Risks

- **Server and app move together.** A signed app release plus a server deploy is a coordinated step. Mitigation: the schema check turns a mismatch into a message; chat is unaffected.
- **Deleting the PhaseFlow workflow loses a feature** that has no replacement (a roadmap-driven manual loop). That is decision 15; the notice says so.
- **Large mechanical changes** (deleting packages, the rename) are easy to get wrong silently. Each is its own commit behind the gates of section 11.
- **The graded run.** G changes what the run is graded on only after it has finished (decision 11); G5 to G7 also change `main`, which the merge decision (unification spec, item 2) governs.

## 13. Open items

1. **The name of the root package after the rename.** `build` is proposed; any short neutral name works.
2. **PhaseFlow credit.** Keep a thank-you line in `CREDITS.md` after the code is gone (proposed), or remove the entry.
3. **Stream cap and polling interval** (16 streams, 250 ms) are defaults; measure with the app and the dashboard open together.
4. **Run retention.** The server lists every run under `.gophermind/`; nothing prunes old runs. Decide when it matters.
