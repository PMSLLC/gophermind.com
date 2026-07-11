# Changelog

All notable changes to GopherMind are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

**PhaseFlow — native spec-driven workflow** (`internal/phaseflow`)
- Integrated [PhaseFlow](https://github.com/jbrahy/metaphaseflow) as a first-class, native subsystem: the **Roadmap → Phases → Plan → Execute → Verify → Milestone** loop, with workflow state persisted under `.planning/` (`ROADMAP.md`, `STATE.md`, `PROJECT.md`, `config.json`) — interchangeable with upstream PhaseFlow.
- `gophermind phase <cmd>` CLI and `/phase <cmd>` TUI slash command. State commands (`init`, `status`, `next`, `commands`) run locally; loop steps (`roadmap`, `plan`, `execute`, `verify`, `milestone`) build a state-seeded prompt from the embedded upstream command and run it through gophermind's agent.
- Go roadmap parser with decimal-phase (inserted-phase) ordering, progress computation, and in-place checkbox mutation that preserves human edits.
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
