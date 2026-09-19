# Assignment tree Slice 1, S1-0: integration facts and compatibility ledger

Checkout: `51ba527` on `main`, 2026-09-19. Go toolchain `go1.26.5 darwin/arm64`;
`go.work` (go 1.26.0) uses `.`, `./bubblecomplete`, `./gophermind-lib`,
`./gophermind-osx`; `gophermind-lib` is module `gophermind/gophermind-lib`
(go 1.25.0). Status per item: VERIFIED (read in this checkout), UNKNOWN, or
BLOCKER.

## 1. Planning root and placement

| Item | Fact | Status |
| --- | --- | --- |
| Planning root | `phaseflow.PlanningDir(root)` = `<root>/.planning` (`PlanningDirName`, `phaseflow/phaseflow.go:28,40`). The tree goes in `<root>/.planning/plan/`. Reuse this; add no second resolver. | VERIFIED |
| Legacy file | `phaseflow.AssignmentsPath(root)` = `.planning/assignments.json` (`phaseflow/assignments.go:138`). | VERIFIED |
| Package convention | Flat, single-purpose packages under `gophermind-lib/` (`phaseflow`, `orchestrate`, `lockfile`, `session`...). | VERIFIED |
| Proposed placement | New package `gophermind-lib/plantree` (models, pure rules) and `gophermind-lib/plantree/store` (repository). Adapter wiring in `tui` and a command entry. Proposal, to confirm in the plan. | PROPOSAL |
| Test commands | `make test` = `go test ./...`; `make vet` exists; no race target, so use `go test -race ./gophermind-lib/plantree/...` explicitly. | VERIFIED |

## 2. Locking and durability primitives

- `lockfile.Acquire(path)` (`lockfile/lock_unix.go:13`): blocking exclusive
  `flock` on a file it creates. On Windows an mtime-based takeover
  (`lock_windows.go:26`). Slice 1 supports macOS and Linux only; Windows must
  fail closed rather than silently weaker.
- `lockfile.WriteAtomic` (`lockfile/lockfile.go:16`): temp file in the same
  directory, `Sync`, `Rename`. **It does not fsync the containing directory.**
  Spec 12.3 requires directory durability, so a new helper is needed. BLOCKER
  for S1-2 only, not for S1-1.
- Existing precedent for lock plus atomic write: `phaseflow.Update`
  (`assignments.go:200`) locks `assignments.json.lock`.
- Supported filesystem boundary to document and test: local APFS (macOS) and
  local ext4/xfs (Linux). Network filesystems unsupported. UNKNOWN until tested.

## 3. Legacy `assignments.json`

Type: `phaseflow.Assignments{Tasks []Task}` (`assignments.go:133`). `Task`
fields: `id, phase, title, description, acceptance_criteria, agent,
agent_addendum, model, status` plus omitempty `wave, depends_on,
candidate_models, attempts, revision_rounds, is_contract`.

### Statuses (all VERIFIED, `assignments.go:70-79`)

`pending, running, done, failed, corrected, needs_revision, escalated,
contract_flagged`.

| Value | Real behavior |
| --- | --- |
| `corrected` | Set by the verification pass when a task first failed its check, was fixed, and now passes (`orchestrate/verify.go:20,42`). Treated as a pass, equal to `done`, in `orchestrate/fallback.go:69`, `phaseflow/contextdoc.go:75`, `serve/pipeline_watch.go:171`, `phaseflow/execute.go:191,610`. It is an accepted-success equivalent in legacy. |
| `contract_flagged` | Task-scope mismatch. `normalizeStatus` (`execute.go:726`) passes it through unchanged so the executor pauses on it rather than requeueing. Not limited to `api_binding`. |
| `needs_revision`, `escalated` | Pass through `normalizeStatus` unchanged; not requeued. |
| any other unknown value | Folded to `failed` by `normalizeStatus`. |

### Readers (VERIFIED, non-test)

| Consumer | Path | Role |
| --- | --- | --- |
| Executor | `phaseflow/execute.go:180,230,295,339,589` | schedules, claims whole tasks |
| Validator | `phaseflow/validate.go:104` | reads |
| Context doc | `phaseflow/contextdoc.go` | reads statuses |
| TUI execute | `tui/execute.go:55` | reads, gates `/project-execute` |
| TUI project | `tui/project.go:416` (`writeProjectDoc`) | reads to render PROJECT.md |
| Pipeline API | `serve/pipeline.go:184,209` | reads for `/pipeline` |
| Pipeline watcher | `serve/pipeline_watch.go:44,77` | fs watch, feeds SSE; runs in `gophermind-server/main.go:188` (a separate process) and `cmd/gophermind/main.go:1255,1289` |
| Desktop / macOS | `desktop/frontend/src/App.tsx:558`, `gophermind-osx/ui/breakdown.go:11-44` | mention the file in prompt text and read it via `/pipeline`. They do not parse it directly. |

### Writers (VERIFIED)

1. Executor via `phaseflow.Update` / `Save` (`execute.go:264,304,355,393,682,696`),
   under the `.lock` flock.
2. **The model itself.** `/project` generation prompts (`gophermind-osx/ui/breakdown.go`,
   `tui` generation prompt) instruct the agent to write `assignments.json` with
   `write_file`. This bypasses `Update` and its lock. It is an unguarded writer
   and a blocker for any future live mode; irrelevant to Slice 1 shadow mode.

Assignment unit is the **task** everywhere. The executor claims whole tasks, so
per-leaf legacy rows would be wrong (matches v4 3.2 and 10.4).

## 4. Question and review mechanisms

| Item | Fact | Status |
| --- | --- | --- |
| Question flow | `tui/interview.go`: one free-text question per turn as JSON `{question, why, suggested, done}`; transcript held in memory (`projTranscript`), not persisted. No options, multi-select, recommendation, or resume. All of v4 section 9 is new. | VERIFIED |
| Session identity | `session` package and `serve.validSessionID` identify chat sessions. No role or reviewer concept exists. | VERIFIED |
| `secaudit.Reviewer` | An unrelated interface for verifying security findings. Not reusable as the planning reviewer. | VERIFIED |
| Separate reviewer session | `agent.New` yields an independent `Agent` with its own message history, and `agent/spawn.go` spawns a child agent. Candidate for the reviewer boundary. Whether that satisfies "session id differs from every author session" needs a design decision. | UNKNOWN |
| Trusted actor identity | No existing trusted-caller context. The adapter must inject actor kind and session id, never accept them from JSON. | BLOCKER for S1-1 review tests until designed in the plan |

## 5. Compatibility mapping ledger

| Legacy concept | Treatment in Slice 1 | State |
| --- | --- | --- |
| Whole-task claim | Tree task maps to one legacy task; no per-leaf rows | verified semantics |
| `agent/model/wave/candidate_models` | Copied into the task `execution` block using the verified Go types | verified types |
| `is_contract` | Wave-0 interface task; copied as-is, never treated as a spec revision | verified (`assignments.go` comment) |
| `corrected` | Accepted-success equivalent in legacy; preview may show it, tree never derives acceptance from it | verified, preview only |
| `contract_flagged` (task) | Task-scope; preserved in `contract_flagged`/`contract_flag` at task scope | verified |
| `revision_rounds` | Planner-rewrite history; preserved opaque, not mapped to `spec_revision` | verified shape, mapping pending |
| `attempts` | Preserved opaque | verified shape, mapping pending |

## 6. Blockers and unknowns for the plan

1. New directory-fsync helper (S1-2).
2. Trusted actor and reviewer-session boundary design (S1-1 authorization tests).
3. Unguarded model-written legacy file (live mode only).
4. Supported filesystem list needs a test run (S1-2).
5. `revision_rounds` and `attempts` preview mapping deferred until a fixture is extracted.

None blocks independent Slice 1 work in S1-1 (pure models and rules).
