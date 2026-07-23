# Changelog

All notable changes to GopherMind are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **`GOPHERMIND_LLM_TIMEOUT`** — per-attempt timeout for LLM completion requests, kept separate from `GOPHERMIND_HTTP_TIMEOUT_S` (which now governs only the one-shot startup calls: model discovery/listing and the capability probe). Accepts a bare number of seconds (`900`) or a Go duration (`15m`); unset means it inherits `GOPHERMIND_HTTP_TIMEOUT_S`, so existing configs are unchanged. Streaming turns remain governed by `GOPHERMIND_STREAM_IDLE_TIMEOUT_S`.
- **`/project-execute` autonomous executor** — autonomously runs every `pending` task in an approved project plan (`.planning/assignments.json`), each in a fresh isolated agent with its assigned model and catalog prompt. Tasks are verified against acceptance criteria (verify-and-correct, one round); failed tasks are marked `failed` and execution continues to a summary. Task agents run in auto-approval mode (unattended). Ctrl-C aborts with graceful cleanup (in-flight tasks revert to `pending`). Requires an approved plan, gated like `/phase execute`.

## [0.5.0] - 2026-07-16

### Added

- **`/project` guided new-project flow (TUI)** — `/project <name>` opens a dialog that interviews you (iterating with the LLM) to build a comprehensive spec, then generates a validated plan: `SPEC.md`, a `ROADMAP.md`, and a machine-readable `assignments.json` that assigns **each task to an agent type** (a `prompt.md` from a per-project catalog, seeded from the embedded PhaseFlow agents) **and a model** (per-type default, overridable). You approve the plan (with a revise loop) before it's marked ready.
- Approval **gate**: in the TUI, `/phase plan|execute|verify|milestone` are blocked until the project plan is approved (CLI `gophermind phase …` is unaffected).
- `internal/phaseflow` plan backbone: assignments schema, agent catalog loader + seeding, `ValidatePlan`, and an approval marker. (Orchestrated verify-and-correct execution over these assignments is a planned follow-on.)
- **Predictive text & autocomplete** — hybrid ghost-text (inline suggestion, accept with Tab or →) and popup menu (for slash commands, file paths, and multiple history matches; navigate with ↑/↓, dismiss with Esc). Built on the reusable `bubblecomplete` library with four providers: slash-command, file/path, whole-prompt history recall, and Markov (n-gram) next-word prediction.
- **Prompt history** — submitted prompts saved to `<config dir>/gophermind/history` (plaintext JSONL, capped at 500 entries). Disable with `GOPHERMIND_HISTORY=off`.
- **Multi-line input** — input box grows 1–4 rows, then scrolls. **Enter** submits; **Shift+Enter** newline (fallbacks **Alt+Enter**, **Ctrl+J** for terminals that don't distinguish Shift+Enter).
- **`/goal` command** — set a session-scoped persistent steering goal (`/goal <text>` to set, bare `/goal` to show, `/goal clear` to remove) injected into every subsequent turn.
- **Native terminal text selection** — mouse capture dropped; now you can select and copy text with click-drag. Keyboard transcript navigation (**PgUp**/**PgDn**) unchanged.

### Fixed

- **Streaming turns no longer time out mid-response.** The LLM client previously shared one `http.Client` whose overall `Timeout` (default 300s, `GOPHERMIND_HTTP_TIMEOUT_S`) bounded the *entire* request including reading the streamed body, so long or heavy turns were killed with `read stream: … context deadline exceeded`. Streaming now runs without a total-request cap, guarded instead by a connect/response-header timeout plus an **idle/stall watchdog** that aborts only when tokens actually stop arriving (default 300s, configurable via `GOPHERMIND_STREAM_IDLE_TIMEOUT_S`). Non-streaming `Complete` and the startup model-probe calls remain bounded by a per-request deadline.

## [0.4.0] - 2026-07-13

### Added

- **Animated gopher intro** — a short (~1.5s), dependency-free truecolor gopher plays before the interactive chat TUI (fade-in → eyes-open reflection sweep → settle). It self-gates to interactive truecolor terminals ≥80×30, honors `--quiet`/`--no-banner`/`NO_COLOR`, is skippable by any keypress, and can be disabled with `GOPHERMIND_INTRO=off`.
- **Interactive `/config` wizard** — configure the endpoint, API key, model, approval mode, max iterations, and optional integrations from inside the chat TUI. It runs via `tea.Exec` (Bubble Tea cleanly hands over the terminal), persists to the config file, and applies changes to the running session live (switching approval mode to `ask` applies on next launch).
- New agent configuration API (`Config`, `SetBaseURL`/`SetModel`/`SetAPIKey`/`SetMaxIter`/`SetApprovalMode`) backing the wizard.

### Changed

- Refreshed the README header with a new full-body gopher.

## [0.3.0] - 2026-07-12

### Added

**PhaseFlow — native spec-driven workflow** (`internal/phaseflow`)
- Integrated [PhaseFlow](https://github.com/jbrahy/metaphaseflow) as a first-class, native subsystem: the **Roadmap → Phases → Plan → Execute → Verify → Milestone** loop, with workflow state persisted under `.planning/` (`ROADMAP.md`, `STATE.md`, `PROJECT.md`, `config.json`) — interchangeable with upstream PhaseFlow.
- `gophermind phase <cmd>` CLI and `/phase <cmd>` TUI slash command. State commands (`init`, `status`, `next`, `commands`) run locally; loop steps (`roadmap`, `plan`, `execute`, `verify`, `milestone`) build a state-seeded prompt from the embedded upstream command and run it through gophermind's agent.
- Go roadmap parser with decimal-phase (inserted-phase) ordering, progress computation, and in-place checkbox mutation that preserves human edits.
- **Deterministic bookkeeping ported to Go** (no model calls, cannot drift from the checkboxes): `phase done <plan-id>` marks a plan complete, auto-ticks a finished phase, and recomputes STATE.md's position/progress; `phase sync` refreshes STATE.md from the roadmap; `phase archive <version>` snapshots a shipped milestone and appends a stat-bearing entry to `.planning/MILESTONES.md`.
- The full embedded PhaseFlow command surface (not just the five loop steps) is runnable by name, e.g. `phase map-codebase`, `phase code-review`, `phase ship`.
- Vendored and embedded PhaseFlow's phase commands, subagent definitions, and templates (MIT, © 2025 Lex Christopherson — see `internal/phaseflow/assets/LICENSE.upstream` and `CREDITS.md`).

## [0.2.0] - 2026-07-11

A large feature release (170 commits since 0.1.0) spanning retrieval, data
tooling, multi-agent reasoning, observability, security hardening, and
distribution — plus several security fixes. All new integration/service tools
are configuration-gated (inert until you provide a token/endpoint).

### Added

**Retrieval, embeddings & knowledge** (pure-Go, no CGO)
- Local semantic index: `embed_index` + `semantic_search` over an OpenAI-compatible embeddings provider (`GOPHERMIND_EMBED_MODEL`), with **RAG context injection** (`GOPHERMIND_RAG`), **incremental** git-diff re-indexing, and `retrieval_eval` (hit@k).
- `hybrid_search` — BM25 (SQLite FTS5) + vector fused via reciprocal rank fusion.
- **Knowledge packs** (`import_pack` + `semantic_search pack=…`), long-term **vector memory** (`remember_fact`), global **profile memory** (`remember_profile`), and **episodic memory** (`record_episode`), injected at task start under `GOPHERMIND_MEMORY`.
- `docs_lookup` — fetch + per-`library@version` cache of library docs; answer-with-citations when web search is used.

**Data, databases & analytics**
- `db_schema`, `db_explain` (full-scan warnings), `data_transform` (filter/group/aggregate over CSV/JSONL), `log_metrics` (time-bucketed), `chart` (sparkline/bar), `detect_anomalies` (robust z-score), `seed_data`, and an opt-in `sql_query` result cache.
- `db_query` — read-only Postgres/MySQL behind a DSN allowlist; `migration_dryrun` (schema diff on a throwaway copy); `read_parquet` for columnar files.

**Agent reasoning & multi-agent**
- Turn strategies: `--debate` (two candidates synthesized), `--samples N` (self-consistency), `--reflexion` (retry with a structured lesson); a task-graph planner/executor, a heuristic tool-use **critic**, cost-aware model routing, subtask budget allocation, and **resumable** step execution.

**Prompt engineering & evaluation**
- Named prompt registry (`prompts` subcommand), per-section token accounting (`prompt-tokens`), a built-in `--schema @diff|@review|@plan` library, a few-shot example bank, extractive context compression, golden-transcript tests, and the `ab` harness gained an LLM-judge scorer, a multi-model **scoreboard**, and a `--min-score` CI gate.

**Sessions**
- `sessions` gained `merge`, `search`, tags + `--tag/--since/--until` filters, `replay`, `export --redact`, per-tag GC (`--keep-tag`), a remote store (`push`/`pull`), and auto-checkpoint before gated mutations.

**Serve, observability & operations**
- Webhook `serve` gained `/healthz`, `/readyz`, `/metrics` (Prometheus), an SSE `/run/stream`, per-caller rate limiting, and HMAC payload verification.
- Dependency-free span **tracing**, JSON + slow-request HTTP logging, a `usage report` cost dashboard, budget alerts, a `--report` HTML run artifact, and cost-anomaly detection.

**Security, sandboxing & governance**
- RBAC per role (`GOPHERMIND_ROLE`), a secrets-file vault (`@name` refs), HMAC-signed + shippable audit logs, prompt-injection defense, a data-egress classifier, policy-as-code tests (`policy test`), container/network-namespace `run_shell` isolation, and approval-timeout auto-deny.

**Distribution, ecosystem & platform**
- **MCP server** (`gophermind mcp`) exposing the tools over the Model Context Protocol; an out-of-process **plugin SDK** + marketplace (`plugins install`); a **WASM sandbox** (`run_wasm`, wazero); shareable config **bundles**; signed self-`upgrade`; opt-in local **telemetry**; and a **benchmark** suite.
- Packaging matrix: **deb/rpm/apk** (nfpm), Scoop, and winget configs; **SBOMs** (syft) and reproducible (`-trimpath`) builds.

**Developer experience**
- LSP-backed `find_definition`, colorized diffs + markdown rendering, a fuzzy `commands` palette, thin **VS Code / Neovim** clients, `doctor fix`, shell completions, `--dry-run`, a `persona new` scaffolder, `--every` (scheduler) and `--watch` (event-driven) triggers.

**Earlier in this cycle**
- `gophermind --print` non-interactive **stream-json** protocol (Claude-Code-compatible), session persistence (`--session-id`/`--resume`), print-mode `--append-system-prompt`/`--permission-mode`, an OpenCoven runtime manifest, npm distribution, a `--version` flag, gated file-mutation tools, `--read-only`, `--plan`/`--parallel`/`--tool-budget`, and auto-load of `CLAUDE.md`/`AGENTS.md`.

### Changed
- Requires **Go 1.25**; builds for **macOS, Linux, and Windows** (amd64/arm64) — releases now include Linux `.deb`/`.rpm`/`.apk` packages and SBOMs.

### Fixed
- **Security:** `serve` sibling-path auth/rate-limit parity (`/run/stream` now enforces HMAC + the shared limiter) and SSE frame injection; `migration_dryrun` ATTACH/`VACUUM INTO` sandbox escape; RBAC fail-open on an unknown role; the VS Code extension no longer honors a workspace-set binary path (ACE); `db_query` data-modifying-CTE bypass + substring DSN-allowlist escape; and path traversal in the LSP `find_definition` tool.
- `apply_patch` is now genuinely atomic — it computes all edits before writing and rolls back on failure.

## [0.1.0] - 2026-07-09
### Added
- First-run setup wizard — endpoint, API key, model picker, approval mode, and max iterations — saved to a global config, plus a `gophermind config` command to re-run it.
- Signed + notarized macOS release pipeline (GoReleaser) distributed via a Homebrew cask, and a `gophermind version` command.
- A random fortune, the version, and recent changes shown under the gopher banner on startup.
- A redrawn gopher ASCII banner.

### Changed
- No endpoint is baked into the binary anymore — configure it via the wizard, a provider profile, or `GOPHERMIND_BASE_URL`.

### Fixed
- The terminal OSC-11 background-color query no longer leaks escape codes into the input box.
