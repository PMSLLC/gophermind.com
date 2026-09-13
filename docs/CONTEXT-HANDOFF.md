# Context Handoff: gophermind desktop, skills, and cache-efficiency work

**Status:** in-progress
**Created:** 2026-09-13

## What We're Building

Three strands, all on `main` in `/Users/jbrahy/OtherProjects/gophermind.com`.
First, a desktop app that can run a session against a **remote** gophermind
server and against a **per-session folder**, so one window drives several
projects and machines. Second, an opt-in **skill source** system that installs
capability packs from GitHub at a pinned commit and injects only the ones
switched on. Third, and only just started, a **prompt-cache efficiency** pass
on the harness: keep the cached prefix byte-stable so a local LLM's KV cache is
reused instead of rebuilt every turn.

Success for the first two is "a person can do it from the window without
curl". Success for the third is a per-session report of tokens by category and
a cache-hit ratio, with any rebuild spike traceable to a structural cause.

## Current State

### Completed

- [x] Backend registry and router in the desktop, local backend only - `/Users/jbrahy/OtherProjects/gophermind.com/desktop/router.go`, `desktop/backends.go` (commit `fef3f03`)
- [x] Remote backends over plain HTTP, configured in `~/.gophermind/backends.json` - `desktop/backendconfig.go` (`44d7c11`)
- [x] Backend picker in the status bar; the machine name appears in every approval prompt - `desktop/frontend/src/App.tsx` (`59c56b3`)
- [x] Skill catalogue, pinned-commit fetch, HTTP routes - `internal/skills/`, `internal/serve/skills.go` (`526d0ba`)
- [x] Path-traversal fix in the skill cache - `internal/skills/fetch.go` (`0c4a93b`)
- [x] Skills panel and cross-backend session list - `desktop/frontend/src/components/SkillsPanel.tsx` (`1968edc`)
- [x] `humanize` tool, first tool in the tree that makes its own model call - `internal/tools/humanize.go` (`50492ff`)
- [x] Session rename and delete from the list (`8f5659c`, `1926880`)
- [x] Per-session working folder, plus the `PickFolder` binding - `internal/serve/session_root.go`, `desktop/app.go` (`75a442d`)
- [x] Planner emits `depends_on` and `is_contract`; `ValidatePlan` checks the graph - `internal/tui/project.go`, `internal/phaseflow/validate.go` (`be1d0f7`)
- [x] `File > New Project` reads a brief and seeds an interview (`8036fd3`, `e3ceb12`)
- [x] A 429 falls back to a sibling model on the same provider - `internal/modelcat/fallbacks.go` (`8eeba37`)
- [x] Live turn status: what it is doing plus a ticking second counter (`dbf0e03`)
- [x] CI fixed; it had failed on every push since the desktop merge (`06248bf`)
- [x] Test suite no longer writes into the user's real session store (`750fb39`)

### In Progress

- [ ] **Cache-efficiency pass.** Diagnosis done, nothing built. See "What to Do
      Next"; the design was proposed in chat and not yet agreed.

### Not Started

- [ ] Release `v0.8.0`. 25 commits since `v0.7.1`, 5 of them unpushed.
- [ ] npm is at `0.1.0`, two releases behind. Needs `npm login` from John.
- [ ] the second server backend's port is **inferred, never confirmed**, and it
      has no token file, so it shows as unavailable.
- [ ] Signing the desktop app is wired but the app has never been released with
      the gopher icon; `v0.7.0` and `v0.7.1` both shipped the Wails "W".

## Key Files

| File | Role | Status |
|------|------|--------|
| `/Users/jbrahy/OtherProjects/gophermind.com/cmd/gophermind/retrieval.go` | `injectRetrieval`, line 67. **The cache defect.** | needs fixing |
| `/Users/jbrahy/OtherProjects/gophermind.com/internal/llm/types.go` | `Usage`, line 72. No cache categories. | needs fields |
| `/Users/jbrahy/OtherProjects/gophermind.com/internal/tools/tool.go` | `Definitions()` sorts tools by name. | already correct |
| `/Users/jbrahy/OtherProjects/gophermind.com/internal/agent/loop.go` | `a.msgs` append-only, line 127. | already correct |
| `/Users/jbrahy/OtherProjects/gophermind.com/cmd/gophermind/main.go` | `composeSystem(...)` around line 929 builds the system suffix. | needs splitting |
| `/Users/jbrahy/OtherProjects/gophermind.com/desktop/deps.go` | Session turn: model policy, fallbacks, per-turn tool registry. | working |
| `/Users/jbrahy/OtherProjects/gophermind.com/desktop/frontend/src/App.tsx` | Status bar, session list, folder picker, New Project. | working |
| `/Users/jbrahy/OtherProjects/gophermind.com/docs/prompts/compare-models.md` | Prompt for benchmarking reachable models into CSV. | unused so far |

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| Router is a real local HTTP server, not a dialer swap | The WebView's `fetch` goes through the OS network stack; no Go dialer can intercept it. A listener is the only place a WebView request can be redirected to another machine. |
| The frontend holds only the router's token; each backend is called with its own | A remote backend's token authorises shell execution on another machine. A page-level scripting bug with that token in reach is RCE on the server rather than on the laptop. |
| Fetched skills are off until switched on, and sources pin a commit | A skill is instructions for an agent with shell access. A repo can be rewritten after review, so a floating ref gives "I read these" a shelf life of zero. |
| Enablement keys are scoped `owner/repo:skill` | Otherwise a newly added repo shadows a skill the user already trusted by reusing its name. |
| `PickFolder` is a second Wails binding | A native dialog is what a native bridge is for. It takes no arguments, decides nothing, and the server validates the path, so there is one place deciding what a usable root is. |
| The New Project menu reads the brief in Go and sends its **contents** | The agent's file tools are contained to the project root and a brief lives outside it. Choosing the file in a native dialog is the authorisation; containment still covers every file not chosen. |
| 429 fallback stays on one provider | `llm.Client` swaps the model string and keeps the BaseURL and key, so another provider's id is a 404. Crossing providers needs a different client, a between-turns decision. |
| `humanize` became a tool, not an injected skill | Its guidance is ~7k tokens. As a skill it was concatenated into every turn's system prompt; as a tool it costs nothing until called. This halved the always-on injection from ~11.9k to ~4.9k tokens. |
| Repo-local `.gophermind/skills` stays always-on | Committing it to the project is the consent, the same way `CLAUDE.md` works. |

## Blockers / Open Questions

- **Unanswered question to John, asked just before this handoff:** should the
  cache instrumentation measure against his local server? llama.cpp exposes
  `timings` and vLLM exposes `num_cached_tokens`; real numbers beat a hash
  proxy. **I cannot check** because the configured local LLM endpoint is unreachable from this
  network, which is also why the desktop keeps falling back to `free-ovhcloud`.
- OVHcloud's anonymous tier is **2 requests per minute per IP per model**. It
  rate-limited an interview after three questions. The sibling-model fallback
  mitigates this; it does not remove it.
- An OpenAI-compatible endpoint returned `"usage": {}` when probed, so the
  server cannot be relied on to report cache categories at all.

## What to Do Next

1. **Move retrieval out of the system prompt.** In
   `/Users/jbrahy/OtherProjects/gophermind.com/cmd/gophermind/retrieval.go`,
   `injectRetrieval` appends task-specific blocks to message[0] and restores
   them after. Every turn therefore has a different prefix from byte zero,
   which is a full cache rebuild per turn. Put the blocks immediately before
   the new user message instead. Write the test first: assert the system
   prompt is byte-identical across two turns with retrieval enabled.

2. **Add cache categories to `Usage`** in
   `/Users/jbrahy/OtherProjects/gophermind.com/internal/llm/types.go` and
   populate them where the server reports them. Where it does not, hash the
   stable prefix per turn and record hit/miss. Surface per session: tokens by
   category, the hit ratio, and the turn index of any rebuild spike.

3. **Split `composeSystem`** into explicitly ordered blocks with a recorded
   boundary. Today persona, instructions, skills and repo context are
   concatenated into one blob that `CapContext` then truncates, so a growing
   repo shifts the boundary and invalidates everything after it.

4. **Then release `v0.8.0`:**
   ```sh
   cd /Users/jbrahy/OtherProjects/gophermind.com
   git push origin main
   export MACOS_SIGN_IDENTITY=4680437160A64398AA7A9CC611D19E7765BFA4EE
   export MACOS_NOTARY_PROFILE=gophermind
   make publish VERSION=0.8.0
   ```
   John runs `make publish` himself; it prompts at each public step.

## Gotchas

- **Build HEAD in a clean worktree before pushing.** HEAD did not compile once
  this session: a file was written but never staged while its callers were.
  The working tree built happily and hid it.
  ```sh
  git --gw-force worktree add -q --detach /tmp/wt HEAD
  cd /tmp/wt/desktop/frontend && npm ci && npm run build
  cd /tmp/wt && go build ./... && go vet ./... && go test -race ./...
  ```
- **`desktop/frontend/dist` is gitignored and `desktop/main.go` embeds it.** A
  fresh checkout fails with `pattern all:frontend/dist: no matching files
  found` until the frontend is built. CI does this now; a local worktree will
  not.
- **`git` is wrapped by a security shim.** Add `--gw-force` when it refuses.
- **macOS only populates the menu bar for the frontmost app.** Querying it via
  AppleScript from the background reports no `File` menu even when one exists.
  Activate the process first.
- **The frontend has no test runner.** `package.json` has React, TypeScript and
  Vite only. Frontend changes are verified with `npx tsc --noEmit`, a real
  build, and route probes, which is weaker than the Go side.
- **Every commit message must end with:**
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```
- **`/Applications/Gophermind.app` is John's private AppleScript launcher.**
  Never overwrite it. The cask installs `GopherMind Desktop.app`, a different
  bundle.
- **The repo is public.** No email, no server addresses, no Team ID, no home
  paths in tracked files. Scan the diff before pushing.
- **Do not merge `milestone/M001`.** 127 auto-generated commits, last touched
  2026-03-23, and `main` is 483 commits ahead of it.

## Verify Everything Still Works

```sh
cd /Users/jbrahy/OtherProjects/gophermind.com
go build ./... && go vet ./... && go test ./... -race -count=1
cd desktop && wails build -platform darwin/arm64 && open "build/bin/GopherMind Desktop.app"
```

As of this handoff: build and vet clean, full suite green under `-race`, CI
green, 5 commits unpushed, 25 commits since `v0.7.1`.
