# gophermind — 100 More Feature Ideas (Batch 3)

A third backlog, **distinct from `todo.md` (batch 1) and `todo-2.md` (batch 2)**.
This batch is grounded in what shipped recently: the `--print` **stream-json
protocol** and OpenCoven manifest (`internal/stream`, `coven/`), **session
persistence** (`internal/session`), **npm + Homebrew distribution**
(`.goreleaser.yaml`, `npm/`), the **startup banner + fortunes**
(`internal/banner`, `internal/fortune`), and the **setup wizard**
(`internal/setup`). Each item names what it builds on, what it does, and its
value. **Priority/milestone TBD — backlog, not a committed plan.**

---

## S. OpenCoven & Streaming-Runtime Depth (builds on `internal/stream`, `--print`, `coven/gophermind.json`)

- [ ] **1. Token-delta streaming.** Emit `content_block_delta` events from the `token` events the encoder currently drops. Value: true incremental output for drivers, closer Claude-Code parity.
- [x] **2. think/speed toggles.** Implement `--think` (reasoning effort) and `--speed` (faster fallback model) so those capabilities can be declared `true`. Value: first-class streaming runtime.
- [x] **3. Richer result subtypes.** `error_max_turns`, `error_during_execution`, with codes. Value: drivers branch on failure mode.
- [ ] **4. Stdin control messages.** Accept `system`/control lines (interrupt, set-model) on the stream-json input. Value: mid-session control from the driver.
- [x] **5. `--output-format json` for run/ask.** Machine-readable one-shot output beyond print mode. Value: scriptability everywhere.
- [ ] **6. Conformance CI.** A GitHub Action that builds `conjure` and runs `validate`/`test` on the manifest each PR. Value: the manifest never drifts from the binary.
- [ ] **7. Multi-adapter manifest.** A `gophermind-plan` read-only variant alongside the default. Value: Coven exposes both access levels.
- [x] **8. Cost in result line.** Surface `total_cost_usd` and token counts from the meter per turn. Value: drivers track spend.
- [x] **9. Mid-turn cancel over stream.** Honor a driver-sent cancel control. Value: responsive interruption.
- [x] **10. Protocol version field.** Advertise the stream-json subset version in the init line. Value: forward-compatible drivers.

## T. Sessions & Continuity (builds on `internal/session`, `--session-id`/`--resume`)

- [x] **11. `gophermind sessions` command.** list / show / rm saved sessions. Value: manage the store from the CLI.
- [ ] **12. Session branching.** Fork a session at a turn into a new id. Value: explore alternatives without losing state.
- [x] **13. Session TTL / GC.** Auto-expire old sessions. Value: bounded disk footprint.
- [ ] **14. Named sessions.** Human aliases mapping to ids (`--resume my-refactor`). Value: no uuids to remember.
- [x] **15. Session export/import.** Pack history + metadata into a shareable file. Value: hand off a debugging session.
- [x] **16. Auto session titles.** Summarize a session for the list view. Value: find the right one fast.
- [ ] **17. Interactive resume in the TUI.** Pick a saved session at chat startup. Value: continuity in interactive mode.
- [ ] **18. Session diff.** Show files/messages changed across a session. Value: review an agent's work.
- [ ] **19. Session-scoped scratchpad.** Durable notes per session. Value: task state survives resume.
- [ ] **20. Encrypted session store.** Optional at-rest encryption for histories. Value: sensitive-repo safety.

## U. Distribution & Packaging II (builds on `.goreleaser.yaml`, `npm/`, `homebrew-tap`)

- [ ] **21. Scoop manifest** (Windows). Value: native Windows installs.
- [ ] **22. AUR package** (Arch). Value: reaches Arch users.
- [ ] **23. deb/rpm via nfpm.** Value: apt/dnf installs.
- [ ] **24. winget manifest.** Value: Microsoft Store CLI installs.
- [ ] **25. Docker image + GHCR.** Value: consistent CI/container use.
- [ ] **26. `gophermind upgrade`.** Self-update from GitHub Releases with signature check. Value: stay current without a package manager.
- [ ] **27. SLSA provenance / build attestation.** Value: verifiable supply chain.
- [ ] **28. cosign-signed checksums.** Value: tamper-evident downloads.
- [ ] **29. npm optionalDependencies variant.** Per-platform packages, no postinstall. Value: works under `--ignore-scripts`/offline.
- [ ] **30. Nix flake / package.** Value: reproducible installs for Nix users.

## V. Prompt System & Personas (builds on `PLAN.md`, `systemPrompt`, `--append-system-prompt`)

- [ ] **31. Build the structured prompt system.** Ship `PLAN.md`: `internal/prompt` template parser + builder (YAML frontmatter + XML sections). Value: composable, maintainable prompts.
- [x] **32. Persona presets.** `--persona reviewer|architect|tester`. Value: task-tuned behavior on demand.
- [x] **33. `CLAUDE.md`/`AGENTS.md` auto-load.** Inject repo instruction files into the system prompt. Value: per-repo conventions respected.
- [x] **34. Per-repo prompt overrides.** `.gophermind/prompt.md`. Value: project-specific behavior.
- [ ] **35. Prompt fragments / includes.** Reusable snippets composed into the prompt. Value: DRY prompt maintenance.
- [ ] **36. Dynamic context injection.** Git status + a compact repo map at session start. Value: the model orients without tool calls.
- [ ] **37. Prompt linting.** Warn on overly long or conflicting instructions. Value: catch prompt bloat/contradiction.
- [ ] **38. A/B prompt experiments.** Run variants against fixtures and score. Value: data-driven prompt tuning.
- [ ] **39. Skill files.** Discover `.gophermind/skills/*.md` and inject relevant ones. Value: modular capability packs.
- [ ] **40. Prompt token-budget guardrail.** Cap injected context to a share of the window. Value: leaves room for the task.

## W. Startup, Branding & Delight (builds on `internal/banner`, `internal/fortune`, `internal/version`)

- [ ] **41. Themeable banner.** Color profiles honoring terminal capabilities. Value: taste + accessibility.
- [x] **42. `--no-banner` / `--quiet`.** Suppress the splash. Value: clean output in scripts/CI.
- [ ] **43. HubTou historical fortunes.** Add the historical set (license-checked) as a second embedded source. Value: richer variety.
- [x] **44. Fortune categories / `--fortune off`.** Value: user control over startup flavor.
- [ ] **45. Tip-of-the-day.** Rotating tips with docs links under the banner. Value: progressive discovery of features.
- [ ] **46. First-run welcome tour.** A short guided intro after the setup wizard. Value: faster onboarding.
- [x] **47. `gophermind doctor`.** Check endpoint/model/`rg`/git/tap reachability. Value: fast setup diagnosis.
- [x] **48. Update-available notice.** Compare version to latest release on startup (opt-in). Value: nudge upgrades.
- [ ] **49. ASCII-art variants.** Seasonal/random gopher poses. Value: delight.
- [ ] **50. Shell prompt integration.** Emit session status for PS1/starship. Value: ambient awareness.

## X. Agent Orchestration & Multi-Agent (builds on `internal/agent`, the fleet-mode idea)

- [x] **51. `spawn_agent` tool.** A focused child agent with its own context. Value: parallel decomposition without context pollution.
- [ ] **52. Fleet/overseer mode.** Supervise multiple sessions via `onEvent` + `ApprovalFunc` with spec rules. Value: coordinated multi-agent runs.
- [x] **53. Plan-then-execute mode.** Emit a reviewable plan before acting. Value: redirect before changes happen.
- [x] **54. Parallel tool execution.** Run independent tool calls in a turn concurrently. Value: faster multi-file reads/searches.
- [ ] **55. Agent-to-agent (A2A) client.** Speak an A2A protocol to other agents. Value: interop beyond OpenCoven.
- [ ] **56. Task queue / job runner.** Enqueue and run tasks with status. Value: batch/background work.
- [x] **57. Reflection-on-failure.** Inject a "what went wrong / next step" after a tool error. Value: better recovery than blind retry.
- [ ] **58. Verifier pass.** A second agent checks the result before finalizing. Value: fewer wrong "done"s.
- [x] **59. Budgeted autonomy.** Wall-clock/token ceilings that abort with partial progress. Value: bounded unattended runs.
- [x] **60. Conversation checkpoints.** Snapshot/rollback `msgs` to a prior turn. Value: undo a bad branch.

## Y. Web, Network & External Data (builds on `internal/safety`, the fetch gap)

- [x] **61. `fetch_url` tool.** Gated, egress-controlled URL fetch → readable text. Value: safe external context (vs. shelling out to `curl`).
- [ ] **62. `web_search` tool.** Pluggable search provider. Value: current information.
- [ ] **63. Docs-retrieval tool.** Context7-style library docs lookup. Value: accurate API usage.
- [ ] **64. HTTP API caller.** OpenAPI-aware request tool. Value: integrate external services.
- [ ] **65. Sandboxed headless browser.** Render/interact with pages under egress control. Value: debug UI / scrape safely.
- [ ] **66. Changelog/RSS watcher.** Trigger a run on upstream changes. Value: event-driven maintenance.
- [ ] **67. Webhook trigger.** Start a one-shot run from an inbound webhook. Value: CI/automation entry point.
- [ ] **68. Egress allowlist enforcement.** Restrict all network tools to approved hosts. Value: exfiltration guardrail.
- [ ] **69. Network budget.** Rate/byte limits on network tools. Value: bounded, predictable usage.
- [ ] **70. Offline docs cache.** Persist fetched docs for reuse offline. Value: speed + air-gapped support.

## Z. Data, DB & Structured Tools (builds on the tool registry)

- [ ] **71. SQL query tool.** Read-only by default, parameterized. Value: reason over real data safely.
- [x] **72. Tabular inspector.** CSV/JSON/Parquet schema + preview tool. Value: understand data files without dumping them.
- [ ] **73. Embeddings retrieve tool.** Vector store over the repo/docs. Value: relevant context without exhaustive search.
- [ ] **74. Migration helper.** Draft schema migrations in the project's format. Value: schema changes follow conventions.
- [ ] **75. Schema-constrained output.** JSON-schema-guided responses. Value: reliable structured results.
- [ ] **76. Spreadsheet edit tool.** Read/modify tabular files. Value: data-wrangling tasks.
- [x] **77. Log analyzer tool.** Parse and summarize logs. Value: incident/debugging support.
- [ ] **78. Metrics query tool.** Prometheus/PromQL reads. Value: observability-aware agents.
- [x] **79. Structured git tool.** blame/log/diff as data (not shell text). Value: reliable VCS reasoning.
- [x] **80. Symbol index tool.** tree-sitter "find function/type X". Value: semantic navigation beyond grep.

## AA. Security, Audit & Compliance II (builds on `internal/safety`, `--permission-mode`)

- [ ] **81. Tamper-evident audit log.** Every tool call + decision + result hash, appended locally. Value: traceability of what the agent did.
- [x] **82. Policy file.** `.gophermind/policy` for deny/allow patterns and gated-tool config. Value: per-repo tuning without recompiling.
- [x] **83. Secret-scanning on writes.** Block/warn on credential patterns in `write_file`/`edit_file`. Value: stops committing secrets.
- [ ] **84. Per-tool approval policies.** "always allow read, ask on write, never auto shell". Value: matches real trust boundaries.
- [ ] **85. Subprocess resource limits.** CPU/mem/fd caps on `run_shell`. Value: contains runaway commands.
- [ ] **86. Network-disabled shell.** Run commands with no network. Value: exfiltration prevention.
- [x] **87. Read-only repo mode.** A flag disabling all mutating tools. Value: safe exploration by construction.
- [x] **88. PII redaction.** Redact secrets/PII from transcripts and sessions. Value: data-handling compliance.
- [ ] **89. Approval by a judge model.** Route ambiguous approvals to a small local model against a spec. Value: smarter-than-regex gating.
- [ ] **90. Container-exec backend.** Run `run_shell` in a container. Value: stronger isolation than process limits.

## AB. Developer Experience & Community (builds on README/CONTRIBUTING, the public repo)

- [ ] **91. Issue/PR templates + Code of Conduct.** Value: smoother contributions.
- [ ] **92. Labeled "good first issues".** Seed starter tasks from these backlogs. Value: onboarding contributors.
- [ ] **93. Landing page (gophermind.com).** A real site for the project. Value: discoverability.
- [ ] **94. Demo recording.** asciinema/GIF of a real session in the README. Value: shows, not tells.
- [ ] **95. Eval harness + scoreboard.** Task fixtures scored across models. Value: measurable quality over time.
- [ ] **96. Plugin/tool SDK + docs.** Stable interface + loader for third-party tools. Value: ecosystem growth.
- [ ] **97. Push toward homebrew/core.** Graduate from the tap once popular. Value: `brew install gophermind` with no tap.
- [ ] **98. Discussions / community channel.** Value: a place for users to gather.
- [ ] **99. Contributor CI gates.** Lint, `go vet`, tests, coverage on every PR. Value: quality stays high as contributors grow.
- [ ] **100. Public roadmap / project board.** Turn these backlogs into a tracked board. Value: transparency and coordination.
