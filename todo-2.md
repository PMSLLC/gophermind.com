# gophermind — 100 More Feature Ideas (Batch 2)

A second backlog, **distinct from `todo.md`** (batch 1 covered model/provider, the
agent loop, file/search/shell tools, the safety sandbox, context/memory, TUI/UX,
and config/CLI). This batch pushes into adjacent areas: version control, language
& build intelligence, code generation, docs, integrations, evaluation,
performance internals, distribution, enterprise, and multimodal.

Each item names the existing feature/component it builds on, what it does, and
its value. **Priority/milestone TBD — backlog, not a committed plan.** This is a
`todo.md`-style backlog, intentionally **not** a `todo.pm` work-list.

---

## I. Version Control & Collaboration (builds on `run_shell` + git, `internal/agent`)

- [ ] **1. Native git tool.** A first-class `git` tool (status/diff/log/blame) instead of shelling out, with structured output the model can reason over. Value: reliable VCS awareness without parsing raw text.
- [ ] **2. Auto-commit with generated messages.** After a successful change+test cycle, draft a Conventional-Commits message from the diff and commit on approval. Value: clean, atomic history for free.
- [ ] **3. Branch-per-task isolation.** Optionally create and switch to a working branch when a task starts, matching the audit-branch convention. Value: contained, reviewable work.
- [ ] **4. PR description generator.** Summarize a branch's commits/diff into a PR body with rationale and test notes. Value: faster, higher-quality pull requests.
- [ ] **5. Inline code-review tool.** Feed a diff to the model for a structured review (bugs, style, security) before commit. Value: a second pair of eyes in-loop.
- [ ] **6. Conflict-resolution assistant.** Detect merge conflicts and propose resolutions with explanations. Value: unblocks rebases/merges quickly.
- [ ] **7. Pre-commit gate integration.** Run existing pre-commit hooks and surface failures to the loop as actionable results. Value: respects repo conventions automatically.
- [ ] **8. Commit-scope guard.** Warn when a commit touches files unrelated to the stated task. Value: keeps changes surgical and traceable.
- [ ] **9. Blame-aware edits.** Before editing, show who last touched the lines and why (commit message). Value: context for risky changes.
- [ ] **10. Stash/restore around experiments.** Auto-stash uncommitted work before a speculative branch and restore after. Value: never lose in-progress edits.
- [ ] **11. Changelog maintenance.** Append to `CHANGELOG.md` from commit history on demand (Keep-a-Changelog format). Value: release notes stay current.
- [ ] **12. Signed-commit support.** Honor GPG/SSH commit signing configured in the repo. Value: provenance in security-sensitive projects.

## J. Language & Build Intelligence (builds on `internal/tools`, `run_shell`)

- [ ] **13. LSP client integration.** Connect to language servers for go-to-def, references, hover types as tools. Value: precise navigation the model can query directly.
- [ ] **14. Diagnostics ingestion.** Surface compiler/linter diagnostics (gopls, tsc, eslint) as structured results after edits. Value: immediate feedback on broken code.
- [ ] **15. Auto-format on write.** Run the repo's formatter (gofmt, prettier, black) on files the agent writes. Value: edits always match project style.
- [ ] **16. Build-system detection.** Detect make/go/npm/cargo/gradle and expose a uniform `build`/`test` interface. Value: stack-agnostic loop logic.
- [ ] **17. Test-runner adapters.** Parse test output (go test, jest, pytest) into pass/fail/coverage structures. Value: reliable stop conditions and reporting.
- [ ] **18. Incremental test selection.** Run only tests affected by changed files when the toolchain supports it. Value: faster feedback loops.
- [ ] **19. Coverage-gap reporting.** After tests, report which new/changed lines lack coverage. Value: targets where to add tests.
- [ ] **20. Dependency manager tool.** Add/upgrade/remove dependencies via the native tool (go get, npm install) with lockfile awareness. Value: safe, correct dependency edits.
- [ ] **21. Compile-check fast path.** A lightweight `go build`/`tsc --noEmit` tool to validate edits without a full test run. Value: cheap correctness checks mid-task.
- [ ] **22. Type-aware edit validation.** Re-run diagnostics on just the edited file and report errors back into the loop. Value: catches breakage before moving on.
- [ ] **23. Monorepo workspace awareness.** Understand workspace/module boundaries (go.work, pnpm workspaces) for scoped builds. Value: correct behavior in large repos.
- [ ] **24. Toolchain version pinning.** Read and respect `.tool-versions`/`go.mod` toolchain directives when invoking builds. Value: reproducible results.

## K. Code Generation & Refactoring (builds on `edit_file`, `write_file`, agent loop)

- [ ] **25. Rename-symbol refactor.** A semantic rename across the repo via LSP, not text replace. Value: safe, complete renames.
- [ ] **26. Extract-function/method tool.** Structured refactor to extract a selection into a named function. Value: mechanical refactors done correctly.
- [ ] **27. Scaffold from template.** Generate boilerplate (handler, test file, module) from project-detected templates. Value: consistent new-file structure.
- [ ] **28. Test stub generation.** Generate table-driven test skeletons for a target function matching repo conventions. Value: faster test coverage.
- [ ] **29. Boilerplate codemod runner.** Apply a described transformation across many files with a preview/diff. Value: large mechanical changes safely.
- [ ] **30. Interface/impl sync.** When an interface changes, list and scaffold the implementations that must update. Value: nothing silently left unimplemented.
- [ ] **31. Dead-code finder.** Identify unused functions/vars/imports and propose removal. Value: keeps the codebase lean.
- [ ] **32. Import organizer.** Add/remove/group imports as edits change usage. Value: clean, compiling files.
- [ ] **33. Migration generator.** Draft DB migration files from a described schema change in the project's migration format. Value: schema changes follow conventions.
- [ ] **34. API-client generation.** Generate a typed client from an OpenAPI/proto spec in the repo. Value: integration code without hand-rolling.
- [ ] **35. Error-handling normalizer.** Detect inconsistent error wrapping/handling and propose a uniform pattern. Value: predictable error behavior.
- [ ] **36. Comment/docstring generation.** Generate or update doc comments for exported symbols on demand. Value: better in-code documentation.

## L. Documentation & Knowledge (builds on `read_file`, repo context)

- [ ] **37. README generator/updater.** Draft or refresh a README from the actual code structure and entry points. Value: docs that match reality.
- [ ] **38. Architecture diagram export.** Emit a component/dependency diagram (Mermaid) from the module graph. Value: onboarding and review aid.
- [ ] **39. API reference extraction.** Generate reference docs from exported symbols and comments. Value: usable docs without a separate pipeline.
- [ ] **40. Inline "explain this file" command.** Summarize a file's purpose, key types, and dependencies. Value: fast comprehension of unfamiliar code.
- [ ] **41. Doc freshness checker.** Flag docs that reference renamed/removed symbols (stale links). Value: prevents misleading documentation.
- [ ] **42. ADR drafting.** Capture a design decision into an Architecture Decision Record template. Value: durable rationale for choices.
- [ ] **43. Glossary/term consistency.** Detect inconsistent terminology across docs and code. Value: clearer, uniform language.
- [ ] **44. Runbook generator.** Produce an operational runbook from build/deploy/test commands found in the repo. Value: reproducible ops steps.
- [ ] **45. Onboarding tour.** A guided "start here" walkthrough generated for a new contributor. Value: faster ramp-up.
- [ ] **46. Doc-test extraction.** Pull runnable examples from docs and verify they compile/run. Value: examples that never rot.

## M. Integrations & Extensibility (builds on `internal/tools` registry, `llm.Client`)

- [ ] **47. MCP client.** Consume Model Context Protocol servers as tool sources, registered into the tool registry at startup. Value: a whole ecosystem of tools without bespoke code.
- [ ] **48. MCP server mode.** Expose gophermind's tools over MCP so other agents can use them. Value: interoperability both directions.
- [ ] **49. Plugin/tool SDK.** A stable interface + loader for third-party tools (binary or script). Value: extend capabilities without forking.
- [ ] **50. Editor bridge.** A protocol so an editor can drive gophermind on the open file/selection. Value: agent assistance inside the IDE.
- [ ] **51. Webhook triggers.** Start a one-shot run from an inbound webhook (e.g. CI failure). Value: event-driven automation.
- [ ] **52. Issue-tracker tool.** Read/comment on GitHub/GitLab issues as a tool. Value: agent works directly from tickets.
- [ ] **53. HTTP fetch tool (sandboxed).** A gated, egress-controlled tool to fetch docs/specs by URL. Value: pull in external context safely.
- [ ] **54. Notification sinks.** Send session-complete/blocked notices to Slack/email/desktop. Value: know when unattended runs finish.
- [ ] **55. Secrets-manager integration.** Pull endpoint keys from a vault/keychain instead of env. Value: no plaintext credentials.
- [ ] **56. Container-exec backend.** Optionally run `run_shell` inside a container for stronger isolation. Value: stronger sandboxing than process limits.
- [ ] **57. Remote-execution backend.** Run tools against a remote dev box over a secure channel. Value: heavy builds off the laptop.
- [ ] **58. Tool marketplace manifest.** A signed manifest format for sharing curated tool bundles. Value: trustworthy capability distribution.

## N. Testing, Quality & Evaluation (builds on the agent loop, `run_shell`)

- [ ] **59. Self-test on changes.** After edits, automatically run the affected package's tests and report. Value: tight correctness feedback.
- [ ] **60. Mutation-testing hook.** Optionally run mutation testing on changed code to gauge test strength. Value: tests that actually catch bugs.
- [ ] **61. Flaky-test detector.** Re-run failing tests to classify flaky vs. real failures. Value: avoids chasing nondeterminism.
- [ ] **62. Benchmark regression guard.** Run and compare Go benchmarks against a baseline. Value: catch performance regressions early.
- [ ] **63. Golden-file test support.** First-class handling for snapshot/golden tests with update flow. Value: easy snapshot maintenance.
- [ ] **64. Prompt/behavior eval suite.** Fixtures that score the agent on real tasks across models. Value: measure quality as prompts/models change.
- [ ] **65. Regression replay tests.** Replay recorded sessions to detect loop-logic regressions. Value: stable core behavior.
- [ ] **66. Lint-as-tool.** Surface linter findings (golangci-lint, eslint) as structured, fixable items. Value: cleaner code in-loop.
- [ ] **67. Test-impact map.** Maintain a file→test mapping to drive selective runs. Value: speed without losing coverage.
- [ ] **68. Coverage trend tracking.** Persist coverage over time and flag drops. Value: quality direction visible.
- [ ] **69. Fuzz-target scaffolding.** Generate Go fuzz harnesses for parsing/validation functions. Value: find edge-case bugs cheaply.

## O. Performance & Internals (builds on `llm.Client`, tool execution, TUI)

- [ ] **70. Concurrent file reads.** Batch and parallelize multiple `read_file` calls in one turn. Value: faster multi-file context gathering.
- [ ] **71. Prompt-cache integration.** Use provider prompt-caching headers when available to cut latency/cost on stable prefixes. Value: cheaper long sessions.
- [ ] **72. Lazy tool-schema loading.** Advertise tool schemas on demand to keep request payloads small. Value: lower token overhead per call.
- [ ] **73. Output ring-buffer for shell.** Bounded streaming buffer so huge outputs don't balloon memory. Value: stable on noisy builds.
- [ ] **74. Incremental repo index.** Maintain a file/symbol index updated on writes, not rebuilt each session. Value: fast startup on large repos.
- [ ] **75. Connection pooling/keep-alive tuning.** Reuse HTTP connections to the model endpoint. Value: lower per-request latency.
- [ ] **76. TUI render throttling.** Coalesce rapid token events to reduce redraw cost. Value: smooth UI under fast streams.
- [ ] **77. Memory-bounded history.** Cap in-memory `msgs` size with spill-to-disk for old turns. Value: long sessions without OOM.
- [ ] **78. Profiling mode.** A flag to emit pprof/trace for the harness itself. Value: diagnose gophermind's own hotspots.
- [ ] **79. Startup time budget.** Defer non-critical init (model probe, index) to background. Value: instant interactive prompt.

## P. Distribution, Packaging & Updates (builds on `cmd/gophermind`, `bin/build.sh`)

- [ ] **80. Cross-platform release builds.** Goreleaser config for macOS/Linux/Windows binaries. Value: easy installation everywhere.
- [ ] **81. Homebrew / package manifests.** Publish formulae/packages for one-line install. Value: low-friction adoption.
- [ ] **82. Reproducible builds.** Pin toolchain and flags for byte-identical binaries. Value: verifiable provenance.
- [ ] **83. Self-update command.** `gophermind update` with signature verification. Value: stay current safely.
- [ ] **84. Versioned config migration.** Migrate config files across schema versions automatically. Value: upgrades don't break setups.
- [ ] **85. Telemetry opt-in (privacy-first).** Anonymous, opt-in usage metrics with a clear disclosure. Value: data to improve, by consent only.
- [ ] **86. Offline/air-gapped mode.** Bundle everything needed to run against a local model with no internet. Value: secure/isolated environments.
- [ ] **87. Docker image.** A ready-to-run container with the binary and common toolchains. Value: consistent environments and CI use.
- [ ] **88. Shell one-liner installer.** A verified install script with checksum pinning. Value: quick, trustworthy setup.

## Q. Enterprise, Multi-tenant & Compliance (builds on `internal/safety`, audit logging)

- [ ] **89. Role-based tool access.** Map users/roles to allowed tools and approval policies. Value: safe delegation in teams.
- [ ] **90. Centralized policy distribution.** Pull org-wide safety/policy config from a signed source. Value: consistent guardrails across machines.
- [ ] **91. Tamper-evident audit export.** Ship the tool-call audit log to a SIEM in a standard format. Value: compliance and incident response.
- [ ] **92. PII redaction in transcripts.** Redact secrets/PII before any transcript is stored or exported. Value: data-handling compliance.
- [ ] **93. Approval delegation chains.** Escalate high-risk approvals to a designated reviewer. Value: human oversight where it matters.
- [ ] **94. Session attribution.** Tag every commit/action with the operator and model used. Value: accountability and traceability.
- [ ] **95. Data-residency controls.** Restrict which endpoints/regions a session may use. Value: meet residency requirements.
- [ ] **96. Compliance report generator.** Produce a per-session report of what was accessed/changed against a control set. Value: audit-ready evidence.

## R. Multimodal, Voice & Accessibility (builds on TUI, `llm.Client`)

- [ ] **97. Image input for vision models.** Attach screenshots/diagrams to a turn when the model supports vision. Value: debug UI/visual tasks.
- [ ] **98. Voice input/output.** Optional speech-to-text prompts and TTS readback. Value: hands-free and accessibility.
- [ ] **99. Screen-reader-friendly mode.** A linear, ARIA-like text mode with semantic markers. Value: accessibility for visually-impaired users.
- [ ] **100. Localization (i18n).** Translate UI strings and prompts; detect locale. Value: usable beyond English-first users.
