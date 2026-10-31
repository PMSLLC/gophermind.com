# Chat brief tools: talking to briefs from the GopherMind chat

Date: 2026-10-30. Status: draft for John's approval.

## Intent

John wants to use the web chat to work with briefs: start a new one, debug a failing one, see what is running, find out whether anything is waiting on an answer, and talk through ways to improve briefs and the planner. Today the server has brief endpoints (`/app/api/briefs`, `/app/briefs/{id}/state|events|answer|stop|resubmit`, `POST /app/briefs`, `/app/api/harness*`), but the chat agent has no tools that reach them. Questions only surface on the Harness page. The chat should be the one place John asks "what needs me?" and "what should we improve?".

## Goals

1. Read tools the agent can call freely: list briefs, brief state, recent events and failures, Harness state, pending questions across all briefs and the Harness, node and attempt artifacts for a failed run, run report.
2. Write tools that always stop for John's confirmation through the existing session approval flow (`POST /session/{id}/approve`): create a brief, answer a question, stop a run, resubmit.
3. "Anything waiting on me?" returns every open question with brief id, question text and options, and John answers in chat; the agent shows the exact answer it will submit before the approval prompt.
4. Optimization conversation: the agent reads run history and brings evidence-backed observations (repeated failure classes, vague or incomplete briefs, setting candidates, repeatedly rebuilt functions Burrow could supply). Every observation cites run ids and node names. Observations are talk; any change is a write tool behind approval.
5. Burrow lookup as a read tool, only once the Burrow client exists on the server side (see Constraints).

## Non-goals

- No autonomous changes: the agent never edits a brief, setting or run without approval.
- No new brief or run storage; tools read the existing run folders and call the existing manager.
- Not the CLI TUI or desktop chat in this round. They get the tools only if they talk to the server.
- No new planner or executor behaviour.

## Anti-goals (failure even if it works)

- A chat message starts, stops or answers anything without a visible confirmation.
- A tool leaks secrets: run artifacts and logs go through the same scrubbing the pipeline endpoints use; nothing under `~/.claude/private` or `credentials.md` is reachable.
- Optimization claims without evidence the user can check.

## Constraints

- Tools are `tools.Tool` values (name, description, JSON schema, `Run`) registered where the server builds the session turn; they exist only when `BriefsDir` is configured, matching the routes.
- Tools call the in-process brief manager and `runview`, not HTTP loopback, so auth is the session's.
- Output size is capped per tool so a long log cannot flood the model context; tools return the tail plus a pointer to the file.
- Burrow: the shared spec (`docs/specs/gopher-burrow-gophermind-shared-spec.md`) says Burrow is not wired into GopherMind yet. The Burrow tool is built only if a client already exists when the builder reaches it; otherwise it is left as a documented follow-up and does not block the rest.
- Repo rules: no em dashes or emojis, subagent-driven plan, commit per task with explicit paths, no push, deploy or install without John asking.
- Success is verifiable: unit tests per tool against temp run folders, a test that every write tool refuses to run without approval, and a manual check in the web chat against the dev server.

## Decided by John

- Web chat is the surface (assumed from conversation; John did not object).
- Pending questions and optimization talk are both in scope.
- Approval before any write.

## Left to the builder

- Exact tool names and schemas (suggested: `briefs_list`, `brief_state`, `brief_events`, `brief_artifact`, `questions_pending`, `harness_state`, `brief_create`, `brief_answer`, `brief_stop`, `brief_resubmit`).
- Where the approval hook for tools lives and how existing tools already use it.
- Whether the optimization observations come from a dedicated read tool (`runs_digest`, summarising failure classes across recent runs) or from the model reading several tools' output. Builder's call; a digest tool is the author's guess.
- A short system-prompt addition telling the agent when to use these tools.

## Settled after the builder check

Author's calls (John can overturn any of them):

- Approval: `gophermind-server` builds every session turn with `serve.RemoteApprovalGate` (`gophermind-server/server.go`), so write tools are gated there. Other entry points auto-approve unless `GOPHERMIND_SERVE_APPROVAL=remote`, so the brief tools are registered only by `gophermind-server`. Write tools must be force-gated: no auto-approve, "always allow" or bypass setting may skip them. If the web chat's approval card shows only the tool name, showing the full arguments (the exact answer, the full brief text) is in scope and is part of this work.
- `brief_create` and `brief_resubmit` take brief text drafted by the model. The approval card shows it in full, with no truncation. Defaults match `POST /app/briefs`; the tool pins nothing.
- The digest tool (`runs_digest`: failure classes, run ids and node names across recent runs) is in scope, because the observations must cite evidence and raw tool output is capped.
- Scrubbing: reuse the scrub the pipeline endpoints and attempt artifacts already use. If it is inline in a handler, extracting it is a first task.
- The brief manager, `runview` and Harness state are shared with the HTTP routes by passing one instance, not building a second. Chat-driven stop and resubmit go through the same manager methods and locks as the HTTP routes.
- `questions_pending` merges the two sources (brief `QUESTIONS.md` gates and Harness questions) into one list with a `source` field.
- The tools and the system-prompt addition exist for every authenticated session on a server with `BriefsDir` set.
- `brief_create` defaults `ask` to true. The spec said defaults match `POST /app/briefs`, where `ask` is false; a chat-started run should stop for John at open questions instead of taking unattended defaults.
- `brief_resubmit` takes only an id, so the approval card does not show the brief text being started again.
- `runs_digest` takes at most 30 runs (`limit` is capped at 30, default 20).
- The chat answer path refuses to overwrite a filled answer, including a pre-filled gate default, whether the write is done by the server or by the launcher (`--no-overwrite`).
