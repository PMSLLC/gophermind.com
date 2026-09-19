# Changelog

All notable changes to GopherMind are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.8.0] - 2026-09-18

### Added

- **gophermind-osx** — a native macOS desktop app built on libui-ng, a lighter-weight alternative to the Wails-based desktop app for people who just want a window: connection manager (local/remote/multi-tunnel, gocloak OAuth2, Keychain-backed credentials), a chat transcript with streaming, an approval tracker with a Y/N/timeout flow, a collapsible right panel, the model catalogue picker, full session-list CRUD, a pipeline panel with breakdown-seed and live task status, and a settings panel for backends/model preferences/skills/cache. Ships as a signed, dock-icon-bearing `.app` bundle with the server binary embedded in `Resources`; settings persist under `~/.gophermind` and survive upgrades and reinstalls. The menu bar gets a real status item too — a gopher-head glyph (not placeholder text), a File > Examples menu pointing at sample pipeline briefs, and the window launches maximized by default.
- **Run a session on another machine.** The desktop app can now point a session at a remote `gophermind serve` instead of its own embedded one, so the work happens on a server while the window stays local. Backends are configured in `~/.gophermind/backends.json`; the file does not exist by default and the app behaves exactly as before without one. Which machine a session runs on is shown in the status bar and, crucially, inside every approval prompt: a gated command approved in the window may execute on a server that hosts everything else, so it must never be ambiguous which box a prompt is about.
- **A session list across every backend**, so an existing conversation on a remote box can be resumed rather than always starting fresh. One unreachable backend costs only its own rows. Sessions can be renamed or deleted from the list, and now auto-name themselves from the first user message instead of staying "Untitled."
- **Skill sources.** `internal/skills` installs capability packs from a GitHub repository and injects only the ones switched on, managed from a panel in the desktop settings. Nothing fetched is enabled by arriving on disk: adding a source makes its content reviewable, and switching any of it on is a separate decision. Sources pin a commit rather than track a branch, because a repository can be rewritten after it was reviewed. A pack committed to a repo's own `.gophermind/skills` stays always on, which is the consent `CLAUDE.md` already carries.
- **`humanize` tool** — rewrites prose to remove AI writing tells, using the vendored [blader/humanizer](https://github.com/blader/humanizer) skill as its system prompt. It is a tool rather than an injected skill so its ~7k tokens of guidance cost nothing until it is called.
- **Vendored skill packs** for tdd, code review and bug diagnosis, from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT).
- **Each session gets its own working directory** on the server (`/serve`), rather than sharing one process-wide root.
- **`/project` can seed its interview from a brief file** — `/project <name> <path-to-brief>` reads the file and asks questions that fill its gaps instead of starting cold. Sample briefs (a CLI tool, a REST API, a menu-bar app) ship for both the desktop app's Pick Brief... button and the CLI/TUI (`examples/phase-flow/briefs/`). `/project`'s own generated plan now asks for `depends_on`/`is_contract` up front, matching what the wave-based executor (0.7.0) already knows how to run.
- **`/project-execute` shows live per-task tool activity** as it runs — each task's tool calls/results stream to the transcript prefixed with the task id, instead of only appearing after the whole task finishes. It also carries the same `<phaseflow-context>` preamble (resolved config, roadmap progress) every interactive `/phase` step already gets, and a Ctrl-C mid-run now reports a tally of what finished before the cancel instead of a bare "cancelled." `/config` can change the speed-tier model live, without a restart.
- **A 429 from the model provider now falls through to the next candidate model** instead of failing the whole turn.
- Verbose debug mode showing every event with readable, unescaped formatting; a status bar that says what a turn is doing and for how long.

### Fixed

- **gophermind-osx would crash on launch outside of `open`** — `main()` never pinned the process to its start-up OS thread, so a blocking HTTP call during backend connect could resume the main goroutine on a different thread than Cocoa's, and the first AppKit call afterward (creating the menu-bar status item) crashed with "NSWindow should only be instantiated on the main thread!"
- gophermind-osx: local mode no longer lets the spawned server default its root to the current working directory; window edge margin, bundled-server-binary lookup, and panel captions corrected.
- **DELETE /session mapped every error to 404**, including a real disk failure deleting an existing session, not just "the session doesn't exist." Now distinguished via a `session.ErrNotFound` sentinel; a genuine failure reports 500.
- **HMAC signature verification ran on bodyless GET/DELETE session requests.** Signing an always-empty body isn't weaker protection, it's a broken one: the resulting signature is a fixed value for a given secret, replayable on any other GET/DELETE afterward. HMAC now only applies to POST/PATCH, where there's an actual payload to protect.
- iOS: pull-to-refresh silently did nothing when the session list was empty (`.refreshable` needs a scrollable container, and the empty state had none); a second approval push for the session already open pushed a duplicate nav layer instead of updating it.
- Conversation history no longer grows unbounded (trimmed to prevent stale errors from accumulating); the context window widened to 50 messages with an explicit turn-completion marker; a tool call now aborts after 3+ consecutive failures or once its result stops changing between retries, instead of looping.
- An empty skill catalogue serialized as `null` instead of `[]`, which broke the settings panel.
- A contract-flagged task in wave 0 could be dropped by `AssignWaves`; the run report now sees pre-revision attempts it previously lost; a contract flag stops the run rather than just failing the pass.
- Several model-attribution bugs: two paths misreported which model actually served a turn; the free-usage odometer metered the configured model rather than the one that ran; a corrupt model-settings file was swallowed instead of reported; the exclusion list wasn't applied to the model already in use.

### Security

- SSE error events are sanitized to prevent injection via error message content.
- The shell safety check now properly parses redirect targets (a redirect to `/dev/null` was previously misclassified as unsafe); `GIT_*` environment variables are stripped from both shell tools' environments so an inherited `GIT_DIR` can't redirect a git subprocess.
- A skill source URL whose path escaped the skill cache is rejected before install, both at the source-id derivation step and with a second check on the final destination.

## [0.7.1] - 2026-09-11

### Fixed

- **The desktop app declared itself version 1.0.0.** Wails renders `Info.plist` from `{{.Info.ProductVersion}}`, `wails.json` never set that key, and Wails substituted its own default — so every build this app has produced, including the one shipped as 0.7.0, reported 1.0.0 in Get Info, in the About box, and to anything else that reads a bundle version. The version is now written into the bundle at build time from the same argument that names the artifact and the cask, so the three cannot disagree, and it is written before codesigning because the signature covers `Info.plist`.
- **A freshly built app showed an old date in Finder.** `wails build -clean` rewrites the bundle's contents but leaves the `.app` directory's own mtime alone, and `ditto` preserves that through the zip, so the stale date reached every user rather than only the machine that built it.
- **The cask template still used the deprecated `depends_on macos: ">= :big_sur"`.** The fix had been applied to the published cask but not to the template it is generated from, so the next release would have regenerated the deprecated form and undone it.
- **`scripts/deploy.sh` hardcoded the development version to `0.5.0+dev`.** It was written when 0.5.0 was current and never updated, so every local and server deploy between then and 0.7.0 reported a version it was not. It now derives from the latest tag.

## [0.7.0] - 2026-09-11

### Added

- **Desktop application** — a Wails shell around the harness, with a chat screen, an approvals screen, a model picker and a settings panel. The frontend speaks only HTTP to an embedded instance of the same server the CLI runs; there is exactly one native binding, and all it does is tell the frontend where that server is. Ships as a signed, notarized and **stapled** universal app: `brew install --cask jbrahy/tap/gophermind-desktop`.
- **Model picker** — a dropdown of every available model with its remaining usage, a cycle-on-capacity switch, preference ordering, reachability and capacity filters, terms-based exclusions, and custom provider/model links. Every judgment call is a setting rather than a hardcoded rule.
- **Per-model metering.** The odometer introduced in 0.6.0 now records usage per model as well as per provider, and every model is measured against the rate limit *it* publishes. A model whose provider publishes no limit shows a bare count, never a borrowed one.
- **Contract-first task pipeline** — tasks gain `depends_on`, `candidate_models`, an attempt history and revision rounds. Waves are derived from the dependency graph and run concurrently (bounded at four); a task tries its candidate models in order, recording a specific reason for each failure; a task that exhausts every model has its definition revised rather than re-run, twice at most, then escalates to a human. A task that finds the contract wrong flags it and stops the run instead of improvising around it.
- **Live pipeline dashboard** at `GET /pipeline`, fed by SSE from the project's assignments file, with an end-of-run report attributing wins and losses to every model actually tried.

### Fixed

- **`run_shell` no longer passes `GIT_*` variables to the commands it runs.** 0.6.0 fixed this for git commands gophermind builds itself, but `run_shell` is the path an agent actually runs `git init` and `git commit` through, and it handed the whole parent environment to bash. Both shell tools are fixed, and the model-supplied `env_allow_list` is filtered too, so naming `GIT_DIR` there cannot reinstate it.
- **Each model is metered against its own published quota.** Every model on a provider was measured against the *default* model's rate limit, so `gemini-2.5-pro` (really 5 RPM, 50 RPD) displayed as 15 RPM / 1,500 RPD, and models publishing no limit at all were given one.
- **Candidate models stay on the endpoint that can serve them.** A project run against a private endpoint was handed two dozen hosted-provider routing ids and never tried the model the plan asked for.
- **Every turn is metered, not only the CLI's one-shot path.** Turns served over HTTP, through a session, or in the desktop app spent free-tier allowance that no counter saw, so the model picker's usage figures stayed at full forever and cycle-on-capacity could never fire.
- **Typing no longer decides a pending approval.** The Y/N approval shortcut was bound to the window with no check on the focused element, so a keystroke typed into a settings field could approve a gated shell command — and `preventDefault` hid it.
- **`POST /run` and `/run/stream` no longer auto-approve gated tools** in the desktop app, which made the approvals screen bypassable by using a different route with the same token.
- **Concurrent turns and concurrent tasks no longer share one mutable LLM client**, which let a turn run on a model another turn had selected while the recorded history named the model it asked for.
- **A contract flag stops the run**, rather than only the current pass, and is rendered as itself rather than as a success.
- **A corrupt or unreadable settings file is reported** instead of silently yielding defaults, which re-enabled providers excluded for legal reasons; a failed odometer read no longer overwrites the lifetime reading with zeros.
- **`Ctrl-C` reaches the server's graceful shutdown path**, which was unreachable because the serve command passed a context that never cancels.


## [0.6.0] - 2026-09-10

### Added

- **Free LLM providers: run gophermind with no API key and no local server.** A vendored, CC0 registry of 16 free providers (from [mnfst/awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis)) is embedded in the binary, so it works offline and is byte-reproducible. Each entry surfaces as a `free-*` profile resolved through the existing `--profile` machinery, which means per-profile env overrides, the setup wizard and validation all work unchanged. **Two providers need no API key at all and were verified completing real turns: `free-ovhcloud` (2 RPM per IP, EU-hosted) and `free-kilocode` (200 requests/hour).** Start with `gophermind free list`, then `gophermind --profile free-ovhcloud ask "hello"`. API keys are still read only from `GOPHERMIND_PROFILE_<NAME>_API_KEY` and are never baked into the registry.

- **`gophermind free`** — `list` shows every provider with its default model, free-tier terms and link, no-key ones first; `show <profile>` prints the full card plus the exact `export` lines to use it; `check <profile>` probes the endpoint's `/models` so compatibility is proven rather than assumed; `usage` prints the odometer. It runs before endpoint validation, so it works with nothing configured — which is exactly the state of the user who needs it.

- **A free-usage odometer and per-quota trip meters.** A lifetime, monotonic count of tokens and requests served by free providers, kept in its own state file so it does not depend on `GOPHERMIND_USAGE_LOG` (which is off by default). Alongside it, rolling trip meters show consumption inside each provider's own published window. Free tiers are not token-denominated — Groq's limit is 1,000 requests/day, Cohere's is 1,000 calls/month, Cloudflare's is neurons — so both requests and tokens are tracked, and **a denominator is only ever shown when upstream published one**: a provider whose limit could not be parsed shows a bare count rather than an invented fraction.

- **Provider attribution.** The model in use is named with its provider and a link, in the startup banner, in the TUI status line (with an optional OSC 8 hyperlink), and in full via a new `/provider` command. Everything is conditional on the active profile being a free one, so a paid or unset profile renders exactly as before.

- **`GOPHERMIND_CHAT_PATH` and `GOPHERMIND_MODELS_PATH`** — explicit path overrides for an OpenAI-compatible endpoint whose base URL already includes the API version segment. Empty (the default) means the historical `/v1/chat/completions` and `/v1/models`, so existing configurations are byte-identical.

- **RAG + memory injection now applies to `serve`.** `GOPHERMIND_RAG` / `GOPHERMIND_MEMORY` previously only took effect in the one-shot `run`/`ask` commands — the served agent (`POST /run`, `/run/stream`, and session turns) silently ignored them, so a phone-driven or webhook-driven turn was never grounded. All three serve paths now inject the same blocks from the same stores. Injection is **per turn**, keyed to that turn's text rather than the session's first message, so later turns on a new topic are grounded too; for persisted sessions the system prompt is restored before the session is saved, so a session never accumulates a copy of each turn's retrieved context. Still opt-in and inert when embeddings are unconfigured.

### Fixed

- **No provider whose base URL ended in `/v1` could complete a turn.** `llm.Client` unconditionally appended `/v1/chat/completions` to `BaseURL`, producing `/v1/v1/chat/completions` and a 404. Every hosted provider publishes its OpenAI-compatible root in that form, and the built-in `openai` profile had the same bug. The same fault in capability probing (`/v1/models`) failed silently rather than loudly, so affected endpoints fell back to a default 8K context when the endpoint actually offered 131K. Fixed via the additive `ChatPath`/`ModelsPath` above; `local-llama` and every existing caller are unchanged.

- **An inherited `GIT_DIR` could redirect gophermind's git subprocesses at the wrong repository.** Code that ran `git` set the working directory but not the environment, and git ignores the working directory when `GIT_DIR` is set. Any gophermind command invoked from inside a git hook — or from any process exporting `GIT_DIR` — therefore read or wrote a different repository than the user intended. The worst case was `gophermind doctor fix`, which runs `git init`: with `GIT_DIR` set and no `GIT_WORK_TREE`, that reinitializes the pointed-at repository as **bare**, after which every ordinary git command in it fails. All eight call sites now build their commands through a new `internal/gitenv` that strips `GIT_*`.

- **`embed_index` no longer fails on prose-heavy repos, and can index large ones.** Chunks were split by line count only (50 lines) with no size bound, so a single dense Markdown chunk could exceed the embedding model's context and make the server reject the *entire* request — one oversized file failed the whole index build. Chunks are now also capped by length (splitting on line boundaries, and rune-safely inside a single over-long line, so minified files are handled). Separately, every chunk used to go out in one HTTP request against a 60s client timeout, which put a hard ceiling on repo size; requests are now sent in bounded batches. Both the full (`BuildIndex`) and incremental (`UpdateIndex`) paths are covered.

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
