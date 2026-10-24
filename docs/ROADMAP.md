# Roadmap: the summit, and every path to it

Written 2026-10-23 for John. One page that says where GopherMind, its web interface, Burrow and the graded run meet, in what order, and what blocks what. Detail lives in the linked documents; this page does not repeat it. Decisions John has taken are in section 7.

## 1. The summit (confirmed by John, 2026-10-23)

All five true, with evidence in one report:

1. **Graded run.** GopherMind builds AI Venture Studio from its brief in one clean `/project` run, using only the Mac mini's local model (`docs/GOAL.md`).
2. **Hosted and signed in.** `https://gophermind.brahy.com/` is a web interface behind Google sign-in for `@brahy.com`, where a brief is uploaded and its whole decomposition, progress and sources are watched live.
3. **Reuse.** Generic leaves come from Burrow when a verified function exists, and every new generic leaf is stored in Burrow with tests (`~/OtherProjects/PMSLLC/burrow`).
4. **Shipped.** Releases reach GitHub, Homebrew and npm; server, desktop app and iOS client run the same contract.
5. **Hands off.** A brief goes in at the web interface and a tested repo comes out, with the build on the OCI server, planning on the mini's model, and Cline allowed only in non-graded runs.

## 2. Map of the work

| Stream | What it is | State (2026-10-23) |
|---|---|---|
| A. Engine | One planner, one executor, one file-based run state (`docs/unification-overview.md`). PhaseFlow and `/phase` are retired; the run view replaces them. | Built and merged. Fixes still coming from real runs (three this week). |
| B. Providers | Router tiers, the mini's llama, Cline (`kind: cline`) for executor leaves | Cline merged (cbb9554). Mini reachable from the OCI server over WireGuard. |
| C. Hosted server | `gophermind-server` on OCI development-jb, public at `gophermind.brahy.com`, run as `gmrun` | Live; landing page, API and run view up. Runs briefs on the server. |
| D. Web interface | Google sign-in, home, chat, runs (`docs/design/web-ui.md`) | Code committed locally (451e01a to b8a6d1b), not pushed or deployed. |
| E. Brief tracking | Upload a brief, start a run from the browser, see the full decomposition, waves, attempts and sources | Designed in outline only. Needs the run-launch design. |
| F. Burrow | Function library keyed by language-neutral contract | Design approved; private repo PMSLLC/burrow. Phases 1 and 2 done and deployed (2026-10-23); Phase 3 (Go plug-in and sandbox) on its last task; Phases 4 to 8 not started. No network API yet: lookup is a Go library and the CLI has only exact-key `burrow find`. |
| G. Clients | Desktop app, gophermind-osx, iOS | Released with 0.10.0; desktop install and mini deploy of 0.10.0 pending. |
| H. Release | 0.10.0 out; 0.10.1 holds the Cline provider and four fixes | 0.10.1 not cut. npm OTP, install steps from 0.10.0 pending. |
| I. Graded run | AI Venture Studio from its brief, mini model only | Blocked on prerequisites (below). |

## 3. How a brief flows, and where Burrow plugs in

Today: `brief plan` (load, clarify, confirm, contract, decompose, enrich, coverage, approve, testwriter) then `brief run` (waves of leaves; each leaf: build, verify with tests, repair or revise; wave checks; acceptance). State is files under `<root>/.gophermind/<runid>`; the server's `/pipeline` reads them.

Burrow touches two stages, nothing else:

- **Contract stage (planner).** Every leaf that is generic must carry a Burrow contract (verb, object, inputs, outputs, effects, traits, failures; `burrow-contract/v1`). Gap: today's node contract holds a Go signature, not that shape. Phase 8 work: extend the contract stage to emit it, and have `burrow key` derive the intent key.
- **Leaf stage (executor).** Before a model builds a generic leaf: look up by key, then top 3 by embedding, then verify. A hit is written into the leaf as its file and verified by the same gates as a built one. A miss builds as today, then stores the result with its tests, but only for pure Go leaves (no effects, stdlib only, no os, net, os/exec, syscall or unsafe imports) whose tests can be written as input and output vectors. Record the outcome in a per-leaf `origin` field (`built`, `reused`, `lookup_missed`, `not_eligible`) so the web interface can show it; a `reused` record also carries the stored key and version. Until Burrow Phase 4 (embedding and LLM verify, which needs a model decision from John), a reuse is an exact-key hit only. After a hit, call the function with the parameter order of the stored contract: the key ignores parameter names and order.

Rule that keeps this safe: a reused body passes the same reply gates, scan and tests as a built one. Burrow never bypasses the executor's checks.

## 4. Phases, in order

Each phase ends on a verifiable result. Streams run in parallel only where they share no files.

**Phase 1. Land what exists (now).**
- Push main (runs the 10 minute gate), deploy to development-jb, set the Google OAuth values in the server env (John creates the client), confirm sign-in in a browser.
- Resume the csvstat run on the server with the `fn-run` gate fix (7dbba43). Done when: 5 of 5 leaves verified, wave checks pass, acceptance 6 of 6, or a new named blocker.
- Cut 0.10.1 with the Cline provider and the fixes.

**Phase 2. Brief tracking in the web interface (E).**
- Run-launch design: the server starts `brief plan` and `brief run` as `gmrun`, one run at a time, a log per run, a stop button. Written and approved before building.
- Pages: upload, decomposition tree (requirements, waves, leaves, contracts, status), per-leaf attempts with model and provider, wave checks, repairs, acceptance. Live over the existing events stream.
- Add the empty per-leaf `origin` field so Burrow can fill it later without a contract change.
- Done when: a brief uploaded in the browser runs to completion on the server and every step is visible without the terminal.

**Phase 3. Graded run prerequisites and the run (I).**
- Prerequisites John owns: `~/.gophermind/vault.age`, `gophermind-dev`, the Postgres forwarder (missing as of 2026-10-20).
- Run the clean-run loop of `docs/GOAL-UNIFICATION.md` task 11.2 with the mini model only (no Cline). Done when: one clean run, report written, perfect-release gate of `docs/GOAL.md` met.
- Graded evidence matters more than speed. Cline stays out (local model only). Burrow lookups are allowed (John, 2026-10-23), provided a reused function passes the same gates and tests; the run report records which leaves were reused.

**Phase 4. Burrow foundation (F, parallel with Phase 2 and 3).**
- Burrow phases 1 to 3: contract keys, catalog (Postgres, pgvector, exact key then embedding then verify), Go plug-in with sandbox, first function verified end to end. One writer at a time in the Burrow repo.
- OCI host decided (Burrow session, 2026-10-23): catalog on the `burrow` VM (10.40.10.131, reachable only inside the VCN or VPN); sandbox (rootless podman, user `burrowrun`) on development-jb, the same 1 OCPU host as gophermind-server, because OCI refused a new VM (LimitExceeded). Heavy sandbox jobs are paused while a gophermind run is active.
- Done when: `burrow` stores and returns a tested Go function by key on an OCI host. Burrow will send the CLI output and response shape when Phase 3 completes. A small HTTP service (`POST /v1/lookup`, `/v1/store`, bearer token, VCN-internal) is proposed and needs John's OK.

**Phase 5. Join Burrow to GopherMind.**
- Burrow phase 8 pulled forward as soon as phase 3 of Burrow is done: contract stage emits Burrow contracts; leaf stage looks up and stores; web interface shows the `origin` per leaf and the lookup trail (key, candidates, verdict).
- Done when: a second brief reuses a function the first brief produced, shown in the interface, with tests passing.

**Phase 6. Breadth and quality.**
- Burrow phases 4 to 7: on-demand languages (PHP, Python, Perl, Rust, SQL), ratings, usage graph with tree testing, telemetry.
- Engine hardening from real runs; Cline allowed in non-graded runs once ClinePass terms for automated use are confirmed.

**Phase 7. Ship and operate.**
- Release the joined system; desktop and iOS on the same contract; tunnel persistence on the laptop; stale notes removed.
- Done when: the five summit statements in section 1 are each true with a link to the evidence.

## 5. Dependencies and blockers

| Blocked | By | Owner |
|---|---|---|
| Sign-in live | Google OAuth client created, values placed in the server env | John |
| Graded run | vault.age, gophermind-dev, Postgres forwarder | John |
| Cline in unattended runs | ClinePass terms and usage limits for automated use (unconfirmed) | John to confirm |
| Burrow on a host | Host choice and capacity check | Decided: catalog on the `burrow` VM, sandbox on development-jb |
| Joined system | Burrow phases 1 to 3 complete; contract shape extended in the planner | Claude, subagent-driven |
| npm and install steps of 0.10.0 | npm OTP, John's go for the mini deploy and desktop install | John |

## 6. Rules that hold on every path

- Subagent-driven development, one writer at a time per repo, a commit per task, no push, deploy, tag or install without John's word.
- The graded run uses only the mini's model.
- Deploy by git only. Secrets never in tracked files or printed.
- AWS is deprecated; hosting is OCI; nothing is hosted from the laptop.
- No em dashes or emojis in anything written.

## 7. Decisions

Taken by John on 2026-10-23:

1. The five summit statements in section 1 are confirmed.
2. Burrow lookups are allowed in the graded run (my reading of "no" to "should they stay out"). The run report must say which leaves were reused.
3. Until a Google OAuth client exists, the web interface signs in with a temporary username and password set in the server env. It is isolated in one file and removed once Google sign-in is live.

Still open:

- Burrow: John's OK for the HTTP service, and the local model for Burrow Phase 4 (embeddings and verify); the graded run allows only the mini's local model.
- ClinePass terms and limits for automated use.
- The Google OAuth client (redirect URI `https://gophermind.brahy.com/auth/callback`), needed to retire the temporary login.

## 8. Record keeping

This file is the single consolidated plan. When a phase finishes, change its state in section 2 and add a dated line below; do not rewrite history.

- 2026-10-23: created; summit confirmed; Burrow allowed in the graded run; temporary password login chosen for the web interface.
- 2026-10-23: web interface with temporary login deployed to development-jb; csvstat run built 5 of 5 leaves, acceptance 3 of 6 (the built `run` leaf ignored the brief: `-f` instead of `--format`, `-` not read as stdin; fixed in 7f83b9f, 3c55ffa, d24c92b, re-run pending).
- 2026-10-23: phase 2 built (not deployed): upload, one-at-a-time launch with an injectable command, stop, run state JSON and run pages under `/app/briefs`; sudo/`gmrun` launch documented in `docs/SERVER-SETUP.md` but not applied; browser rendering unchecked; clarify answering and a live event stream on the run page are not built.
- 2026-10-23: synced with the Burrow session: its status, host decision, `origin` values (`built`, `reused`, `lookup_missed`, `not_eligible`) and the pure-Go eligibility limits recorded above.
