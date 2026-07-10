# gophermind — 100 More Feature Ideas (Batch 4)

A fourth backlog, **distinct from `todo.md` (batch 1), `todo-2.md` (batch 2), and
`todo-3.md` (batch 3)**. This batch is grounded in what shipped across those
batches: the **structured prompt system** (`internal/prompt`), **web_search**
(Brave) + **sql_query** (SQLite) + the fetch cache (`internal/tools`), the
**verifier pass / fleet mode / task queue** (`internal/agent`, `internal/jobs`),
the **tamper-evident audit log / judge model / policy** (`internal/safety`),
**rich sessions** (branch/diff/alias/encrypt/resume, `internal/session`), the
**webhook `serve` + `ab` harness** (`internal/abtest`), and the **stream-json
control protocol** (`internal/stream`). Each item names what it builds on, what
it does, and its value. **Priority/milestone TBD — backlog, not a committed plan.**

---

## AC. Retrieval, Embeddings & Knowledge (builds on `web_search`, `sql_query`, `find_symbol`, the fetch cache)

- [x] **1. Embeddings-backed repo index.** An `embed_index` builder + `semantic_search` tool over a local vector store (sqlite-vec on top of the existing SQLite dep). Value: relevance-ranked context without exhaustive grep.
- [x] **2. Pluggable embeddings provider.** An `internal/embed` interface with OpenAI-compatible `/v1/embeddings` and a local fallback. Value: the vector store works against any endpoint the LLM client already talks to.
- [x] **3. RAG context injection.** At turn start, retrieve top-k chunks for the user's task and inject them under a `<retrieved_context>` prompt section. Value: grounded answers, fewer tool round-trips.
- [x] **4. Docs-retrieval tool.** A `docs_lookup` tool that fetches + caches library docs (Context7-style) keyed by `library@version`. Value: accurate API usage over stale training data.
- [x] **5. Web-search result re-ranking.** Score `web_search` hits by embedding similarity to the query before returning. Value: the top result is actually the most relevant, not just Brave's default order.
- [x] **6. Answer-with-citations mode.** When `web_search`/`docs_lookup` were used, require the final answer to cite the source URLs it relied on. Value: verifiable, traceable answers.
- [x] **7. Incremental index updates.** Re-embed only files changed since the last index (via `git_info` diff). Value: fast re-index on large repos.
- [ ] **8. Hybrid search.** Combine BM25 (SQLite FTS5) with vector similarity for `semantic_search`. Value: recall on exact terms + concepts together.
- [x] **9. Knowledge packs.** Import a folder of markdown/PDF into the vector store as a named pack the model can query. Value: bring domain docs into the agent's reach.
- [x] **10. Retrieval eval.** Score retrieval quality (hit@k) against fixtures using the `ab`/`abtest` harness. Value: tune chunking/embeddings on data, not vibes.

## AD. Data, Databases & Analytics (builds on `sql_query`, `inspect_data`, `set_csv_cell`, `analyze_log`)

- [ ] **11. Multi-engine SQL.** Extend `sql_query` with read-only Postgres/MySQL drivers behind a DSN allowlist. Value: reason over real app databases, not just SQLite.
- [x] **12. Schema explorer tool.** A `db_schema` tool returning tables, columns, types, and FKs as structured JSON. Value: the model orients before querying.
- [x] **13. Query plan / EXPLAIN helper.** Surface `EXPLAIN QUERY PLAN` and flag full-table scans. Value: performance-aware SQL suggestions.
- [x] **14. Safe write migrations preview.** Pair `create_migration` with a dry-run that runs the up-migration against a throwaway copy and diffs the schema. Value: catch broken migrations before commit.
- [x] **15. Dataframe tool.** A `data_transform` tool for filter/group/aggregate over CSV/JSONL (builds on `inspect_data`). Value: analysis without writing a script.
- [x] **16. Chart/spark output.** Render a compact ASCII/Unicode sparkline or bar chart from a query result. Value: at-a-glance trends in the terminal.
- [ ] **17. Parquet & Arrow support.** Teach `inspect_data`/`data_transform` to read columnar formats. Value: modern data-lake files.
- [x] **18. Log-to-metrics.** `analyze_log` emits time-bucketed counts (errors/min) as a series. Value: spot spikes, not just totals.
- [x] **19. Fixture/seed generator.** Generate realistic seed rows from a schema for tests. Value: faster test-data setup.
- [x] **20. Query result caching.** Cache `sql_query` results by (db-mtime, query, params) hash. Value: instant repeats during analysis.

## AE. Agent Reasoning & Multi-Agent II (builds on the verifier pass, fleet mode, `spawn_agent`, checkpoints)

- [ ] **21. Debate/consensus mode.** Two agents argue a solution; a judge picks or synthesizes. Value: higher-quality answers on ambiguous tasks.
- [ ] **22. Planner/executor split.** A dedicated planner agent emits a task graph; executor agents (fleet) run leaves in dependency order. Value: parallelism with correct ordering.
- [ ] **23. Tool-use critic.** A lightweight critic reviews each proposed tool call against the task before it runs. Value: fewer wasted/wrong tool calls.
- [x] **24. Self-consistency sampling.** Run N samples of a turn and majority-vote the answer. Value: robustness on reasoning tasks.
- [ ] **25. Episodic memory.** Persist "what worked / what failed" summaries per repo and retrieve them at task start. Value: the agent learns across sessions.
- [ ] **26. Cost-aware routing.** Route easy subtasks to `--speed` model, hard ones to the strong model (uses the capability probe). Value: quality where it matters, cheap elsewhere.
- [ ] **27. Reflexion loop.** On a failed verification, generate a structured lesson and retry with it appended. Value: better recovery than a blind re-run.
- [ ] **28. Subtask budget allocation.** Split the turn's cost/token budget across `spawn_agent` children. Value: no single child blows the whole budget.
- [ ] **29. Interruptible long runs.** Checkpoint after each plan step so a cancelled run resumes from the last step. Value: no lost work on interruption.
- [x] **30. Deterministic replay tests.** Record a session's LLM responses and replay them offline to test agent logic. Value: fast, hermetic agent tests.

## AF. Prompt Engineering & Evaluation II (builds on `internal/prompt`, `internal/abtest`, `--persona`, skills)

- [x] **31. Prompt registry & versioning.** Store named prompt templates with versions and a `prompts` subcommand to list/diff/rollback. Value: manage prompts like code.
- [ ] **32. Eval harness + scoreboard.** Extend `ab` to score variants across multiple **models** and print a leaderboard (builds on `abtest`). Value: pick the best prompt×model pair on data.
- [x] **33. LLM-as-judge scoring.** Add a rubric-based judge scorer to `abtest` beyond substring match. Value: grade open-ended answers.
- [ ] **34. Regression gate for prompts.** Fail CI if a prompt change drops the eval score below a threshold. Value: prompts can't silently regress.
- [x] **35. Persona authoring UX.** A `persona new` scaffolder that writes a template + registers it. Value: easy custom personas beyond the built-ins.
- [x] **36. Few-shot example bank.** Attach curated examples to a template section, auto-selected by task similarity. Value: better in-context steering.
- [x] **37. Prompt token accounting.** Report per-section token cost of the built prompt (builds on the token estimator). Value: see what's eating the window.
- [x] **38. Structured-output schemas library.** Ship reusable JSON schemas for `--schema` (diff, review, plan). Value: consistent machine-readable results.
- [ ] **39. Auto prompt-compression.** Summarize long injected context to fit the budget instead of hard truncation (`CapContext`). Value: keep meaning, lose bytes.
- [x] **40. Golden-transcript tests.** Snapshot a canonical session and diff on prompt changes. Value: catch unintended behavior shifts.

## AG. Memory, Sessions & Continuity III (builds on the session store, branch/diff/alias, scratchpad, encryption)

- [x] **41. Long-term vector memory.** Persist salient facts across sessions in the vector store, retrieved by relevance. Value: the agent remembers project context long-term.
- [x] **42. Session merge.** Combine two branched sessions' histories with conflict handling. Value: reconcile parallel explorations.
- [x] **43. Session search.** Full-text (FTS5) search across all saved sessions. Value: "where did we discuss the parser bug?"
- [x] **44. Auto-checkpoint on risk.** Snapshot the session before any gated mutation so it's trivially revertible. Value: undo an agent's bad change.
- [ ] **45. Shared session store.** Optional remote (S3/HTTP) backend for the session dir so a team shares sessions. Value: hand off work across machines.
- [x] **46. Session replay viewer.** A TUI/`--print` mode that steps through a saved session turn by turn. Value: review an agent's reasoning.
- [x] **47. Redacted session export.** Combine `export` with the existing redactor to share a scrubbed session. Value: safe sharing of debugging sessions.
- [x] **48. Session tags & filters.** Tag sessions and filter `sessions list` by tag/date/repo. Value: organize a growing store.
- [x] **49. Continuity across repos.** A global "profile memory" separate from per-repo session state. Value: user preferences follow you everywhere.
- [x] **50. TTL policies per tag.** Different GC windows for tagged sessions (keep "important", expire "scratch"). Value: smarter retention.

## AH. Integrations, Webhooks & the Outside World II (builds on `http_request`, `web_search`, the `serve` webhook, `fetch_url`)

- [x] **51. GitHub tool.** Read-only issues/PRs/reviews via the GitHub API (token-gated), plus a gated "comment" action. Value: work with the repo's collaboration surface.
- [x] **52. Slack/Discord notifier.** Post run results/approvals to a channel (egress-allowlisted). Value: team visibility and remote approvals.
- [x] **53. Jira/Linear tool.** Read tickets and (gated) transition status. Value: close the loop from task to tracker.
- [ ] **54. OAuth device flow.** Acquire and refresh tokens for integrations securely. Value: first-class auth without pasting long-lived tokens.
- [x] **55. Webhook signature verification.** HMAC-verify inbound `serve` payloads (GitHub/Stripe style). Value: trust the trigger source, not just the bearer token.
- [ ] **56. Scheduled runs.** A `cron`-like scheduler that fires a task file on an interval (builds on `queue`). Value: recurring maintenance without external cron.
- [ ] **57. Event-driven triggers.** Watch a branch/RSS/file and enqueue a run on change (fulfills the batch-3 watcher idea, now concrete). Value: proactive automation.
- [x] **58. Response streaming over `serve`.** Server-Sent Events from the webhook so callers see tokens live. Value: responsive remote UIs.
- [x] **59. Rate limiting & quotas per caller.** Token-bucket limits on `serve` keyed by token. Value: safe multi-tenant exposure.
- [x] **60. OpenAPI request tool.** Load an OpenAPI spec and expose typed operations as callable actions (builds on `http_request`). Value: correct, discoverable API calls.

## AI. Security, Sandboxing & Governance III (builds on the audit log, judge model, policy, redaction, `SafeJoin`)

- [ ] **61. Container-exec backend.** Run `run_shell` inside a disposable container (Docker/Podman) with mounts limited to the repo. Value: strong isolation beyond ulimits.
- [ ] **62. Network-namespace shell.** A truly network-disabled `run_shell` on Linux via `unshare -n`. Value: exfiltration-proof command execution.
- [x] **63. Secrets vault integration.** Resolve credentials from Vault/1Password/env-file at call time, never storing them. Value: no secrets in config or transcripts.
- [x] **64. RBAC for tools.** Roles map to allowed tool sets (reviewer = read-only, operator = shell). Value: least privilege per user.
- [x] **65. Signed audit log.** Sign the tamper-evident chain with a key so integrity is externally verifiable (extends AA#81). Value: non-repudiable audit trail.
- [x] **66. Audit log shipping.** Stream audit entries to syslog/OTLP/a file collector. Value: central security monitoring.
- [x] **67. Policy-as-code tests.** Unit-test `.gophermind/policy` decisions against scenarios. Value: prove the guardrails do what you think.
- [x] **68. Prompt-injection defense.** Detect and neutralize tool-output that tries to hijack instructions. Value: resist malicious repo/web content.
- [x] **69. Data-egress classifier.** Warn/deny when a network tool would send content matching secret/PII patterns (builds on the redactor). Value: stop accidental exfiltration.
- [x] **70. Approval delegation & timeout policy.** Route approvals to a second person or auto-deny after a window (extends `ApprovalWithTimeout`). Value: safe unattended operation.

## AJ. Observability, Cost & Operations (builds on the usage meter, guardrails, cost-in-result, audit log)

- [ ] **71. OpenTelemetry tracing.** Emit spans for turns, tool calls, and LLM requests. Value: end-to-end latency/why-is-it-slow visibility.
- [x] **72. Prometheus metrics endpoint.** Export tokens, cost, tool counts, and error rates on `serve`. Value: dashboards and alerting.
- [x] **73. Cost dashboard.** A `usage report` subcommand summarizing spend by day/model/session. Value: understand where budget goes.
- [x] **74. Budget alerts.** Warn (or halt) at configurable spend thresholds beyond per-run guardrails. Value: no surprise bills.
- [ ] **75. Live status TUI.** A `top`-style view of active fleet workers, their tasks, and spend. Value: operate multi-agent runs at a glance.
- [x] **76. Structured JSON logs.** Optional machine-readable logs for every event. Value: pipe into log tooling.
- [x] **77. Slow-request tracing.** Flag and record LLM/tool calls over a latency threshold. Value: find the bottleneck.
- [x] **78. Health/readiness endpoints.** `/healthz` and `/readyz` on `serve`. Value: deploy behind a load balancer / k8s.
- [x] **79. Run report artifact.** After `run`/`queue`, write a self-contained HTML report (transcript + diffs + cost). Value: shareable record of what happened.
- [x] **80. Anomaly detection on cost.** Flag turns whose token use is a statistical outlier. Value: catch runaway loops early.

## AK. Developer Experience & Editor Integration (builds on the TUI, `status`, `doctor`, `git_info`, the CLI)

- [ ] **81. Language Server (LSP) client.** Use a project's LSP for go-to-def/references instead of grep (upgrades `find_symbol`). Value: precise, semantic navigation.
- [ ] **82. Unified-diff review UI.** Render proposed edits as a colored diff with per-hunk approve/reject in the TUI. Value: reviewable, surgical changes.
- [ ] **83. Editor plugins.** Thin VS Code / Neovim clients that drive `--print` stream-json. Value: gophermind inside the editor.
- [x] **84. Inline patch application.** Apply a model-produced unified diff atomically with rollback (hardens `apply_patch`). Value: safe multi-file edits.
- [ ] **85. TUI command palette.** Fuzzy-find slash commands, sessions, and tools. Value: fast discovery in chat.
- [ ] **86. Rich markdown/code rendering.** Syntax-highlight code blocks in the TUI transcript. Value: readable output.
- [x] **87. `doctor --fix`.** Offer to auto-remediate common setup issues doctor finds. Value: from diagnosis to fixed in one step.
- [x] **88. Shell completions.** Generate bash/zsh/fish completion for subcommands and flags. Value: ergonomic CLI.
- [ ] **89. Interactive setup for integrations.** Extend the wizard to configure Brave/GitHub/DB credentials. Value: guided, not doc-hunting, onboarding.
- [x] **90. Dry-run mode.** `--dry-run` shows the tool calls the agent *would* make without executing mutations. Value: preview before committing to a run.

## AL. Distribution, Ecosystem & Platform II (builds on GoReleaser/npm/Homebrew, the OpenCoven manifest, the plugin idea)

- [ ] **91. Plugin/tool SDK.** A stable Go interface + out-of-process (gRPC/stdio) protocol so third parties ship tools. Value: an ecosystem, not a fork.
- [ ] **92. Tool marketplace/registry.** Discover and install community tools/skills by name. Value: capabilities without recompiling.
- [ ] **93. Ship gophermind as an MCP server.** Expose its tools over the Model Context Protocol. Value: any MCP client (incl. Claude) can drive it.
- [ ] **94. WASM tool sandbox.** Run untrusted community tools in a WASM runtime with capability grants. Value: safe third-party extensions.
- [ ] **95. Config profiles bundle.** Shareable, versioned config+policy+prompt bundles per team. Value: consistent setup across a team.
- [ ] **96. deb/rpm/scoop/winget packaging.** Finish the packaging matrix via nfpm + manifests. Value: native installs everywhere.
- [ ] **97. `gophermind upgrade`.** Signed self-update from GitHub Releases (fulfills batch-3 U#26 with cosign verification). Value: stay current safely.
- [ ] **98. Reproducible builds + SBOM.** Deterministic builds with a published SBOM and attestation. Value: verifiable supply chain.
- [ ] **99. Telemetry (opt-in, privacy-first).** Aggregate anonymous feature/latency stats with a hard off switch. Value: data-driven roadmap without surveillance.
- [ ] **100. Public benchmark suite.** A standardized task set + harness to compare gophermind across models/versions over time. Value: measurable, honest progress.
