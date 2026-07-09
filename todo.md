# gophermind — 100 Feature Ideas & Upgrades

Enhancements and extensions grounded in the **current** system: an OpenAI-compatible
LLM client (`internal/llm`), a tool-calling agent loop (`internal/agent`), the
read/write/edit/list/search/shell tools (`internal/tools`), the safety sandbox
(`internal/safety` — symlink-aware `SafeJoin`, shell deny-list, approval gate),
the Bubble Tea TUI (`internal/tui`), and env+flag config (`internal/config`).

Each item names the existing feature it builds on, what the upgrade does, and its
value. **Priority/milestone TBD — this is a backlog, not a committed plan.**

---

## A. Model & Provider (existing: `llm.Client`, OpenAI-compatible chat, model auto-discovery, bearer auth, InsecureTLS)

- [x] **1. Streaming token usage & cost meter.** Extend `llm.Client` to read the `usage` block (prompt/completion tokens) from each response and surface a running per-session token + estimated-cost counter. Value: visibility into spend and context pressure during a session.
- [x] **2. Provider profiles.** Upgrade single-endpoint config into named profiles (`local-llama`, `openai`, `anthropic-proxy`) selectable by flag, each with its own base URL/key/model/timeout. Value: switch backends without editing env vars.
- [x] **3. Automatic retry with backoff.** Wrap `Complete`/`Stream` with bounded exponential-backoff retries on 429/5xx and transient network errors. Value: resilience against flaky local servers and rate limits.
- [x] **4. Response caching by prompt hash.** Cache identical `(messages, tools)` completions to disk keyed by hash, with a TTL. Value: instant, free re-runs during iterative development and tests.
- [x] **5. Model capability probe.** Extend `DiscoverModel` to also probe context window, tool-calling support, and max output, caching results per endpoint. Value: adapt truncation/iteration limits to the actual model.
- [x] **6. Fallback model chain.** Allow an ordered list of models; on error or refusal, fall through to the next. Value: graceful degradation when a primary model is down.
- [x] **7. Per-request temperature/top-p override.** Surface sampling params through config and a `/temp` TUI command (currently hard-coded `Temperature: 0`). Value: creative vs. deterministic modes on demand.
- [x] **8. Prompt/response transcript export.** Add a flag to dump the full wire-level message history (JSONL) for a session. Value: debugging, evals, and fine-tuning datasets.
- [x] **9. mTLS / client-cert auth.** Extend the `InsecureTLS` path into proper optional client-certificate auth for internal endpoints. Value: secure internal deployments without disabling verification.
- [x] **10. Request/response middleware hooks.** A small interface to intercept outgoing requests and incoming responses (for logging, redaction, header injection). Value: extensibility without forking the client.
- [x] **11. Streaming cancellation polish.** Ensure a mid-stream Ctrl-C cleanly aborts the in-flight `Stream` and discards the partial assistant turn. Value: snappy interruption without corrupting conversation state.
- [ ] **12. Token-aware request trimming.** Before sending, estimate tokens and drop/summarize the oldest turns to fit the model's window. Value: long sessions stop hard-failing on context-length errors.

## B. Agent Loop & Reasoning (existing: `agent.Agent`, `Send`, `maxIter`, `onEvent`, persistent `msgs`, tool dispatch)

- [ ] **13. Plan-then-execute mode.** Add an optional first pass where the model emits a structured plan, shown to the user, before tool execution begins. Value: visibility and a chance to redirect before changes happen.
- [ ] **14. Per-turn tool-call budget.** Cap tool calls per turn (separate from `maxIter`) with a warning when approached. Value: prevents runaway loops on ambiguous tasks.
- [ ] **15. Sub-agent dispatch tool.** A `spawn_agent` tool that runs a focused child `Agent` with its own context for an isolated subtask, returning only the result. Value: parallel decomposition without polluting the main context.
- [ ] **16. Conversation checkpoints.** Let the user snapshot and roll back `msgs` to a prior turn. Value: undo a bad exploration branch without restarting.
- [ ] **17. Tool-result summarization.** When a tool result is large, auto-summarize it into the context while keeping the full text available on demand. Value: keeps long sessions within budget.
- [ ] **18. Reflection step on failure.** After a tool error, inject a structured "what went wrong / next step" reflection before retrying. Value: better recovery than blind retry.
- [ ] **19. Deterministic replay.** Record events and tool I/O so a session can be replayed for debugging or testing the loop logic. Value: reproducible bug reports.
- [ ] **20. Configurable system prompt.** Load the system prompt from a file/flag (currently constant `systemPrompt`), with project-level overrides. Value: tailor behavior per repo.
- [ ] **21. Tool-choice forcing.** Support forcing a specific tool (or none) for a turn via the API's `tool_choice`. Value: scripted, predictable steps.
- [ ] **22. Stop conditions / goals.** Let a one-shot run declare a success predicate (e.g. "tests pass") the loop checks each iteration. Value: autonomous task completion with a clear exit.
- [ ] **23. Parallel tool execution.** Execute independent tool calls in a single assistant turn concurrently (currently sequential in `dispatch`). Value: faster multi-file reads/searches.
- [ ] **24. Token streaming to events.** Already emits `token` events — add structured deltas (role, index) so the TUI can render reasoning vs. answer separately. Value: clearer live output.
- [ ] **25. Cost/time guardrails.** Abort a turn that exceeds a wall-clock or token ceiling, returning partial progress. Value: bounded autonomous runs.
- [ ] **26. Multi-step diff preview.** Accumulate all proposed file edits in a turn and present a combined diff before applying. Value: review-before-write safety.

## C. Tools — Files & Editing (existing: `read_file`, `write_file`, `edit_file` exact-unique match, `list_files` 2000-cap)

- [ ] **27. Ranged/`head`/`tail` read.** Extend `read_file` with optional line ranges so the model can read part of a large file (currently full-content, unbounded). Value: context efficiency on big files.
- [ ] **28. Read with line numbers.** Optional `cat -n`-style output for `read_file` so edits can be anchored by line. Value: more reliable `edit_file` targeting.
- [ ] **29. Multi-occurrence edit.** Add an explicit `replace_all` mode to `edit_file` (currently fails on >1 match). Value: safe bulk rename within a file.
- [ ] **30. Patch/unified-diff apply tool.** A tool that applies a unified diff atomically across files. Value: large coordinated edits in one call.
- [ ] **31. Atomic write with backup.** `write_file`/`edit_file` write to a temp file and rename, keeping a `.bak`. Value: no half-written files on crash; easy revert.
- [ ] **32. Dry-run mode.** A flag making all mutating tools report what they *would* do without writing. Value: safe previews and planning.
- [ ] **33. Binary/large-file guard.** Detect binary or oversized files in `read_file` and refuse with a helpful message. Value: avoids dumping garbage into context.
- [ ] **34. File metadata tool.** A `stat` tool returning size, mtime, mode, line count. Value: lets the model decide how to read a file.
- [ ] **35. Rename/move/delete tools.** Add gated `move_file` and `delete_file` tools (containment + approval). Value: full file lifecycle within the sandbox.
- [ ] **36. Directory creation tool.** Explicit `mkdir` tool rather than relying on `write_file`'s implicit `MkdirAll`. Value: clearer intent and structure scaffolding.
- [ ] **37. Glob-aware `list_files`.** Accept include/exclude globs and an optional depth limit. Value: targeted listings on large repos.
- [ ] **38. Respect `.gitignore`.** Honor `.gitignore` in `list_files`/`search` in addition to the hard-coded `ignoredDirs`. Value: cleaner, repo-accurate views.
- [ ] **39. Truncation transparency for reads.** When a read is trimmed, report exact byte/line counts dropped. Value: the model knows it saw a partial file.
- [ ] **40. Symlink-report in listings.** Mark symlinks in `list_files` output (now that `SafeJoin` is symlink-aware). Value: clarity about what is/isn't followed.

## D. Tools — Search & Shell (existing: `search` rg/grep with `--` guard, `run_shell` `bash -lc` + timeout + deny-list)

- [ ] **41. Search context lines.** Add `-A/-B/-C`-style context to `search` results. Value: matches are easier to act on without a second read.
- [ ] **42. Scoped search.** Optional path/glob/file-type filters on `search` (e.g. `--type go`). Value: faster, less noisy results on big trees.
- [ ] **43. Case/whole-word/literal flags.** Expose `search` options for case-insensitivity, word boundaries, and fixed-string mode. Value: precision without regex gymnastics.
- [ ] **44. Result pagination.** Page large `search`/`list_files` output instead of truncating at a fixed cap. Value: nothing important silently dropped.
- [ ] **45. Structural/AST search.** Integrate a tree-sitter-based query for "find functions named X" style searches. Value: semantic navigation beyond text grep.
- [ ] **46. Streaming shell output.** Stream `run_shell` stdout/stderr as `token` events instead of buffering until exit. Value: live feedback on long builds/tests.
- [ ] **47. Per-command timeout override.** Let `run_shell` accept a timeout argument within a hard ceiling (currently fixed). Value: long test suites without raising the global limit.
- [ ] **48. Working-directory argument.** Optional subdir for `run_shell` (contained via `SafeJoin`). Value: run tools in subprojects without `cd`.
- [ ] **49. Environment allow-list.** Pass a curated, minimal env to `run_shell` rather than inheriting everything. Value: reduces secret leakage to subprocesses.
- [ ] **50. `bash -lc` → `bash -c` option.** Make login-shell sourcing opt-in. Value: faster, more predictable command execution.
- [ ] **51. Exit-code-aware result shaping.** Tag `run_shell` results with structured success/failure so the loop can branch reliably. Value: cleaner stop-condition logic (see #22).
- [ ] **52. Output artifact capture.** Optionally write full (untruncated) `run_shell`/`search` output to a file and return a path + preview. Value: nothing lost to the 12k truncation cap.

## E. Safety & Sandbox (existing: `SafeJoin` symlink-aware, `CheckCommand` deny-list, `ApprovalFunc` gate, `Gated` tools)

- [ ] **53. Allow-list command mode.** Optional strict mode permitting only an explicit command allow-list (vs. the current deny-list). Value: lock down auto-approval environments.
- [ ] **54. Policy file.** Load deny/allow patterns and gated-tool config from a `.gophermind/policy` file instead of compiled-in constants. Value: per-repo security tuning without recompiling.
- [ ] **55. Approval with full diff.** The `ApprovalFunc` shows a real diff (for edits) or parsed command summary before prompting. Value: informed yes/no, fewer rubber-stamps.
- [ ] **56. Per-tool approval policies.** Allow "always allow read, ask on write, never auto shell" granularity. Value: matches real trust boundaries.
- [ ] **57. Audit log of tool calls.** Append every tool call + decision + result hash to a tamper-evident local log. Value: traceability of what the agent did.
- [ ] **58. Resource limits on subprocesses.** Apply CPU/memory/file-descriptor limits to `run_shell` children. Value: contains runaway commands.
- [ ] **59. Network egress controls.** Optionally run shell commands with network disabled or via an allow-listed proxy. Value: prevents data exfiltration from the sandbox.
- [ ] **60. Secret-scanning on writes.** Scan `write_file`/`edit_file` content for credential patterns and warn/block. Value: stops the agent committing secrets.
- [ ] **61. Read-only repo mode.** A flag that disables all mutating/gated tools entirely. Value: safe "ask"/exploration sessions by construction.
- [ ] **62. Path allow-list / sub-root.** Restrict the agent to a subdirectory of the repo. Value: tighter blast radius on large monorepos.
- [ ] **63. Approval timeouts / defaults.** Configurable default decision when no human answers within N seconds. Value: predictable unattended behavior.
- [ ] **64. Command normalization hardening tests.** Property-based tests fuzzing `CheckCommand` for whitespace/quoting bypasses (builds on the recent fix). Value: keeps the deny-list robust as it grows.
- [ ] **65. Approval delegation to a policy model.** Route ambiguous approvals to a small local "judge" model checking against a spec. Value: smarter-than-regex gating, human-out-of-loop.
- [ ] **66. Containment for symlink *creation*.** Ensure any future symlink-creating tool can't plant an escape link (pairs with symlink-aware `SafeJoin`). Value: closes the write-side of the symlink class.

## F. Context & Memory (existing: persistent `msgs`, `Reset`)

- [ ] **67. Repo map injection.** Build and inject a compact file/symbol map at session start. Value: the model orients without spending tool calls.
- [ ] **68. `CLAUDE.md`/`AGENTS.md` auto-load.** Read project instruction files into the system prompt automatically. Value: per-repo conventions respected by default.
- [ ] **69. Persistent session store.** Save/restore full sessions to disk by name. Value: resume long tasks across restarts.
- [ ] **70. Automatic conversation summarization.** When context fills, compress older turns into a running summary (pairs with #12/#17). Value: effectively unbounded sessions.
- [ ] **71. Retrieval over the repo.** Embed files and add a `retrieve` tool for semantic lookup. Value: relevant context without exhaustive search.
- [ ] **72. Pinned facts / scratchpad.** A persistent notes area the model can write to and always sees. Value: durable task state across turns.
- [ ] **73. Git-aware context.** Inject current branch, status, and recent diff at session start. Value: the agent knows the working state.
- [ ] **74. Open-files context.** Track recently read/edited files and keep their summaries warm. Value: coherence across a multi-file change.
- [ ] **75. Cross-session memory.** Optional long-term store of decisions/gotchas keyed by repo. Value: the agent "remembers" prior sessions' lessons.
- [ ] **76. Context budget dashboard.** A `/context` command showing token usage by category (system, history, tool output). Value: diagnose and trim bloat.
- [ ] **77. Selective history pruning.** Let the user drop specific turns (e.g. a giant tool dump) from context. Value: manual recovery from context blowups.
- [ ] **78. Instruction-priority handling.** Formalize precedence (user > project files > system) consistently in prompt assembly. Value: predictable behavior when guidance conflicts.

## G. TUI & UX (existing: Bubble Tea, alt-screen, scrollable viewport, scrollback)

- [ ] **79. Slash-command palette.** In-TUI commands (`/reset`, `/model`, `/context`, `/save`, `/diff`). Value: control without leaving the session.
- [ ] **80. Syntax-highlighted diffs.** Render file edits as colorized diffs in the viewport (glamour is already a dep). Value: readable change review.
- [ ] **81. Collapsible tool output.** Fold long tool results behind a one-line summary, expandable on key. Value: a clean, scannable transcript.
- [ ] **82. Status/spinner bar.** Show current model, token count, elapsed time, and a working spinner. Value: at-a-glance session health.
- [ ] **83. Approval UI inline.** Render approval prompts as a focused modal with allow/deny/always keys. Value: faster, clearer decisions.
- [ ] **84. Searchable scrollback.** `/` to search the transcript. Value: find that earlier output without scrolling.
- [ ] **85. Copy-to-clipboard.** Yank a code block or message to the clipboard (clipboard dep present). Value: move output into editors/PRs.
- [ ] **86. Themes & color profiles.** Light/dark/high-contrast themes honoring terminal capabilities. Value: accessibility and preference.
- [ ] **87. Resumable input history.** Up/down through prior prompts, persisted across runs. Value: re-run and tweak commands quickly.
- [ ] **88. Multi-pane layout.** Optional side pane for file tree or diff while chatting. Value: context without losing the conversation.
- [ ] **89. Mouse + keyboard scroll parity.** Smooth viewport scrolling with both inputs and page/half-page keys. Value: comfortable navigation of long sessions.
- [ ] **90. Non-interactive progress UI.** A clean, line-based progress renderer for `run`/`ask` (non-TTY) mode. Value: good output in CI/pipes.

## H. Config, CLI, Sessions & Observability (existing: env+flags, `run`/`ask`/chat modes, `internal/ui`)

- [ ] **91. Config file support.** Load `.gophermind.toml` (flags > file > env > defaults). Value: per-project settings without long flag lists.
- [ ] **92. `--print`/JSON output mode.** Machine-readable result output for `run`/`ask`. Value: scripting and integration with other tools.
- [ ] **93. Init/doctor command.** `gophermind doctor` checks endpoint reachability, model, ripgrep, git. Value: fast diagnosis of setup issues.
- [ ] **94. Shell completions.** Generate bash/zsh/fish completions. Value: discoverability of flags and subcommands.
- [ ] **95. Structured logging.** Optional leveled, file-based logs (`slog`) separate from the TUI. Value: debugging without cluttering the UI.
- [ ] **96. Hooks system.** Pre/post-tool and session-start/stop hooks running user scripts. Value: lint-on-write, notify-on-finish, custom automation.
- [ ] **97. Metrics export.** Emit per-session metrics (tokens, tool counts, durations) as JSON or Prometheus textfile. Value: track agent behavior over time.
- [ ] **98. Eval harness.** A `gophermind eval` mode running task fixtures and scoring pass/fail. Value: regression-test the agent itself as prompts/models change.
- [ ] **99. Fleet/overseer mode.** A supervisor that watches multiple gophermind (and external) sessions via a shared status channel and enforces spec rules through the `onEvent` + `ApprovalFunc` seams. Value: the "watch over several building sessions" goal — coordinated, policy-checked multi-agent runs.
- [ ] **100. Self-update & version pinning.** `gophermind version`/`update` with reproducible builds and a pinned model+prompt manifest. Value: trustworthy, repeatable agent behavior across machines.
