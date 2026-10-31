# Chat brief tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web chat agent can list and inspect briefs, find open questions, digest recent failures, and (with approval) create, answer, stop and resubmit briefs.

**Architecture:** `serve` builds a set of `tools.Tool` values around the in-process `briefManager` and `harness`, and hands them to the session turn through the request context. `gophermind-server`'s `sessionTurn` adds them to the turn's registry. Write tools are named in `safety.Gated`, so `RemoteApprovalGate` stops for John on each call; the chat's approval card shows the full arguments.

**Tech Stack:** Go (`gophermind-lib` modules `serve`, `tools`, `safety`; `gophermind-server`), the existing chat page `serve/assets/chat.html`.

**Spec:** `docs/superpowers/specs/2026-10-30-chat-brief-tools-design.md`

## Global Constraints

- No em dashes, no emojis in anything written.
- Tools exist only on `gophermind-server` and only when `BriefsDir` is set. No other entry point registers them (they auto-approve elsewhere).
- Write tools: `brief_create`, `brief_answer`, `brief_stop`, `brief_resubmit`. All four are in `safety.Gated`. Nothing else is.
- Tool output is capped (default 8 KiB per call) and tails long text; the cap note names the file to read.
- Secrets: tool output passes through `safety.NewSecretScanner().Redact` before it is returned.
- One instance of `briefManager` and `harness` is shared by the HTTP routes and the tools.
- Commit per task with explicit `git add` paths. No push, deploy or install.
- Build and test from `gophermind-lib` and `gophermind-server`: `go build ./... && go vet ./... && gofmt -l . && go test ./...`.

## Review Focus

1. Unknown or malformed brief id (path-like, empty, `../x`): tool returns an error string, never reads outside `BriefsDir`.
2. A run is already active when `brief_create` or `brief_resubmit` is called: tool reports the active id (the `briefBusyError`), starts nothing.
3. Answering a question that does not exist or is already filled: tool says so, writes nothing.
4. A huge `run.log` or artifact: output is the tail, under the cap, with the note.
5. No briefs, no questions, empty harness: tools answer "none", not an error.
6. Approval denied or timed out (`RemoteApprovalGate` default is deny): the write does not happen. (Covered by Task 6 gating test.)

## File Structure

- `gophermind-lib/tools/tool.go` (modify): `Registry.With`.
- `gophermind-lib/safety/safety.go` (modify): `Gated` list.
- `gophermind-lib/serve/chattools.go` (create): context plumbing, tool set constructor, shared helpers (cap, redact, id check).
- `gophermind-lib/serve/chattools_read.go` (create): the read tools.
- `gophermind-lib/serve/chattools_write.go` (create): the write tools.
- `gophermind-lib/serve/briefs.go`, `briefanswer.go` (modify): extract method-level helpers the HTTP handlers and tools both call.
- `gophermind-lib/serve/webhook.go` (modify): create the tool set after `newBriefManager`/`newHarness`, attach it to session requests.
- `gophermind-lib/serve/assets/chat.html` (modify): approval card shows full args.
- `gophermind-server/server.go` (modify): `sessionTurn` adds the tools, system prompt suffix.
- `CHANGELOG.md` (modify).

---

### Task 1: Plumbing, with an empty tool set

**Files:**
- Modify: `gophermind-lib/tools/tool.go`, `gophermind-lib/serve/webhook.go`, `gophermind-server/server.go`
- Create: `gophermind-lib/serve/chattools.go`
- Test: `gophermind-lib/tools/tool_test.go`, `gophermind-lib/serve/chattools_test.go`

**Interfaces:**
- Produces: `func (r *Registry) With(ts ...Tool) *Registry` (copy, original untouched; a same-named tool replaces).
- Produces: `func ChatToolsFrom(ctx context.Context) []tools.Tool` and unexported `withChatTools(ctx, []tools.Tool) context.Context` in `serve`.
- Produces: `type chatTools struct{ bm *briefManager; h *harness /* may be nil */ }` and `func (c *chatTools) all() []tools.Tool` (empty for now).

- [ ] **Step 1: Write failing tests.** `TestRegistryWithCopies` (original has no new tool, copy has it, both keep existing tools). `TestSessionStreamCarriesChatTools`: build the handler with `BriefsDir` set and a `SessionTurn` that records `ChatToolsFrom(ctx)`; POST `/session/{id}/stream`; assert the turn saw a non-nil slice (empty is fine) and, with `BriefsDir` unset, saw nil.
- [ ] **Step 2: Run both, expect compile failure.** `go test ./tools ./serve -run 'TestRegistryWith|TestSessionStreamCarries'`
- [ ] **Step 3: Implement** `Registry.With`; the context helpers; `chatTools`; in `webhook.go` after the brief manager (and harness, if any) is created, wrap `mux.Handle("POST /session/{id}/stream", ...)` so `r = r.WithContext(withChatTools(r.Context(), ct.all()))` when the brief routes are on. The harness is created after the brief routes, so build `ct` after both and register the session route accordingly (move the registration if it currently precedes them).
- [ ] **Step 4: Wire `gophermind-server/server.go`:** in `sessionTurn`, `turnReg = turnReg.With(serve.ChatToolsFrom(ctx)...)` (after the `ReadSessionRoot` branch, so it applies to both registries).
- [ ] **Step 5: Run** `go test ./...` in `gophermind-lib` and `gophermind-server`; expect PASS.
- [ ] **Step 6: Commit** `git add gophermind-lib/tools gophermind-lib/serve/chattools.go gophermind-lib/serve/chattools_test.go gophermind-lib/serve/webhook.go gophermind-server/server.go`

### Task 2: Shared helpers and `briefs_list`, `brief_state`

**Files:**
- Modify: `gophermind-lib/serve/chattools.go`
- Create: `gophermind-lib/serve/chattools_read.go`
- Test: `gophermind-lib/serve/chattools_read_test.go`

**Interfaces:**
- Produces: `func capOutput(s string, max int, pointer string) string` (keeps the tail; appends `[tail of N bytes; full text at <pointer>]`), `func (c *chatTools) clean(s string) string` (secret redact then cap), `func validBriefID(id string) bool` (non-empty, `questionIDRE`-safe set, no path separators).
- Produces: tools `briefs_list` (no args; id, title, phase, started, one per line, newest first) and `brief_state` (arg `id`; phase, error, nodes with status, failing attempts, open questions, log tail). Both reuse `bm.list` data (`BriefListItem`) and `bm.buildState`.

- [ ] **Step 1: Write failing tests** with a temp `BriefsDir` holding two run records: `TestBriefsListNewestFirst`, `TestBriefsListEmpty` (returns "no briefs"), `TestBriefStateRejectsBadID` (`../x`, empty, `a/b`), `TestBriefStateCapsLog` (200 KiB log yields under 8 KiB plus the note), `TestOutputRedactsSecrets` (a log line with a bearer token is redacted).
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** the helpers and both tools as methods returning `tools.Tool`; register them in `all()`.
- [ ] **Step 4: Run** `go test ./serve -run 'TestBrief|TestOutput'`; expect PASS.
- [ ] **Step 5: Commit** with explicit paths.

### Task 3: `questions_pending` and `harness_state`

**Files:**
- Modify: `gophermind-lib/serve/chattools_read.go`
- Test: `gophermind-lib/serve/chattools_read_test.go`

**Interfaces:**
- Produces: tool `questions_pending` (no args): every unanswered question across all briefs, one block each: `source` (`brief` or `harness`), brief id, question id, text, options, whether a default exists, and whether it can be answered through the chat (`BriefQuestion` plus `markAskable`/`gateQuestion` already say which). Tool `harness_state` (no args): `harness.snapshot()` summarised; "harness is off" when `h == nil`.

- [ ] **Step 1: Write failing tests:** `TestQuestionsPendingAcrossBriefs` (two runs with `QUESTIONS.md` gates, one question answered, one open each: two results), `TestQuestionsPendingNone` ("nothing is waiting"), `TestQuestionsPendingHarnessSource` (a brief the harness started is tagged `harness`; read `harness.go:81-260` first to see how the harness records which briefs it owns and whether it holds questions of its own; if it does, merge them in), `TestHarnessStateOff`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** Use `readGateQuestions(root, id)` for gate questions and `questionsOf` for the node question store.
- [ ] **Step 4: Run, expect PASS. Step 5: Commit.**

### Task 4: `brief_artifact` and `runs_digest`

**Files:**
- Modify: `gophermind-lib/serve/chattools_read.go`
- Test: `gophermind-lib/serve/chattools_read_test.go`

**Interfaces:**
- Produces: `brief_artifact` (args `id`, optional `node`, `attempt`, `file`): without `node` lists nodes; with `node` lists artifact names (`runview.View.ArtifactNames`); with `file` returns the content (`View.Artifact`). `View` from `runview.Open(root, id)`.
- Produces: `runs_digest` (optional arg `limit`, default 20 runs, max 50): over the newest runs, group failed nodes by failure reason text (the `malformedReasonOf` / attempt failure fields in `BriefState`), and print: failure class, count, run ids, example node names; then the runs that ended `failed` or `interrupted` with their `Error`. Evidence only; no advice text.

- [ ] **Step 1: Write failing tests:** `TestBriefArtifactListsThenReads`, `TestBriefArtifactRejectsTraversal` (`file` of `../../run.json`), `TestRunsDigestGroupsSameReason` (three runs, two share a failure reason: one group with count 2 citing both run ids and the node), `TestRunsDigestLimit`, `TestRunsDigestNoRuns`.
- [ ] **Step 2: Run, expect FAIL. Step 3: Implement. Step 4: Run, expect PASS. Step 5: Commit.**

### Task 5: Extract method-level helpers from the HTTP handlers

**Files:**
- Modify: `gophermind-lib/serve/briefs.go`, `gophermind-lib/serve/briefanswer.go`
- Test: existing `briefs_test.go`, `briefanswer_test.go` must pass unchanged.

**Interfaces:**
- Produces: `func (m *briefManager) createText(src []byte, ask bool) (rec briefRecord, warnings []brief.Problem, err error)` (what `create` does after reading the body: parse, `withRepo`/fresh-id handling, `start`; returns `*briefBusyError` when a run is active); `func (m *briefManager) resubmitID(id string) (briefRecord, error)` (what `resubmitHandler` does); `func (m *briefManager) answerText(id, qid, text string) (remaining int, err error)` (`writeAnswerAs` under `answerMu`, then `resumeAnswered` when `remaining == 0`, exactly as `answerHandler` does).
- The HTTP handlers call these; their status codes and bodies do not change.

- [ ] **Step 1: Run** `go test ./serve -run 'Brief|Answer'` and record that it passes (baseline).
- [ ] **Step 2: Extract** the three helpers; handlers become thin wrappers that map errors to the same status codes.
- [ ] **Step 3: Run the same tests; expect PASS with no test edits.**
- [ ] **Step 4: Commit.**

### Task 6: Write tools, gated

**Files:**
- Create: `gophermind-lib/serve/chattools_write.go`
- Modify: `gophermind-lib/safety/safety.go`, `gophermind-lib/serve/chattools.go`
- Test: `gophermind-lib/safety/safety_test.go`, `gophermind-lib/serve/chattools_write_test.go`

**Interfaces:**
- Consumes: `createText`, `resubmitID`, `answerText`, `bm.stop(id)`, `validBriefID`.
- Produces: tools `brief_create` (args `text` required, `ask` bool default true so open questions stop for John), `brief_answer` (`id`, `question_id`, `text`), `brief_stop` (`id`), `brief_resubmit` (`id`). Each returns one plain sentence of outcome, including the new run id or the busy run id.

- [ ] **Step 1: Write failing tests:** `TestGatedBriefWriteTools` (the four names gated; every read tool name not gated), `TestBriefCreateStartsRun` (fake `BriefCommand`, assert a record exists), `TestBriefCreateBusy` (reports active id, no second record), `TestBriefAnswerUnknownQuestion` and `TestBriefAnswerAlreadyFilled` (no file change), `TestBriefStopNotActive`, `TestBriefResubmitBadID`.
- [ ] **Step 2: Add a gating end-to-end test** `TestWriteToolDeniedDoesNothing`: run the agent loop (or call the registry through `agent` with `RemoteApprovalGate` and a denying `approved:false`) and assert no run record is created when approval is denied. Use the same pattern as `approval_test.go`.
- [ ] **Step 3: Run, expect FAIL. Step 4: Implement the tools and add the four names to `safety.Gated`. Step 5: Run `go test ./safety ./serve`, expect PASS.**
- [ ] **Step 6: Commit.**

### Task 7: Approval card shows the full arguments

**Files:**
- Modify: `gophermind-lib/serve/assets/chat.html` (`addApproval`, currently `clip(ev.args, 1000)`)
- Test: `gophermind-lib/serve/pages_test.go`

- [ ] **Step 1: Write failing test** `TestChatApprovalNotClipped`: the served chat page does not contain `clip(ev.args, 1000)` and does contain a scrollable style for the approval `pre` (`max-height` with `overflow`).
- [ ] **Step 2: Run, expect FAIL.** 
- [ ] **Step 3: Implement:** show `ev.args` in full (JSON arguments pretty-printed when they parse), the `pre` scrolls past a fixed height. Cap only at 200000 characters.
- [ ] **Step 4: Run, expect PASS. Step 5: Commit.**

### Task 8: Guidance for the agent, changelog, manual check

**Files:**
- Modify: `gophermind-server/server.go`, `CHANGELOG.md`
- Test: `gophermind-server/server_test.go`

- [ ] **Step 1: Write failing test** `TestSessionTurnHasBriefToolsAndPrompt`: with `BriefsDir` configured, a new session's system prompt contains the brief guidance sentence and the registry exposes `brief_state`; without `BriefsDir`, neither.
- [ ] **Step 2: Implement** a short suffix appended for new sessions when the tool set is non-empty: use `questions_pending` when asked what needs an answer; use `runs_digest` and `brief_artifact` before claiming a cause, and cite run ids and node names; observations are discussion, change nothing without a write tool; never claim a write happened before the tool result says so. Existing sessions keep the prompt they were saved with (tool descriptions still guide them); note that in the changelog.
- [ ] **Step 3: Run** `go build ./... && go vet ./... && gofmt -l . && go test ./...` in `gophermind-lib` and `gophermind-server` (add `-race` for `serve`); expect clean.
- [ ] **Step 4: Add the CHANGELOG entry. Commit.**
- [ ] **Step 5: Manual check, only when John asks for a dev-server install:** in the web chat ask "anything waiting on me?", "why did the last run fail?", then "stop the active run" and confirm the approval card shows the full arguments and that Deny stops it.

## Out of scope, recorded

- Burrow lookup tool: no Burrow client exists in this repo (only `docs/specs/gopher-burrow-gophermind-shared-spec.md`), so per the spec it is a follow-up once the client lands.
- CLI and desktop chat get no tools.
- `harness_start/stop/reset` as chat tools: not in the spec.
