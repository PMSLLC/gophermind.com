# SPEC: gophermind-osx — Native macOS Desktop + Standalone Server

## Overview

Replace the Wails/React desktop app with a native macOS application built on libui-ng, and extract the server into a standalone `gophermind-server` binary. Shared business logic lives in `gophermind-lib/`. The desktop supports local mode (embedded server, random bearer token) and remote mode (gocloak OAuth2 + userspace WireGuard tunnel per backend).

## Repository Layout

```
gophermind-lib/          # shared Go packages (module: gophermind-lib)
  agent/                 # agent loop, tools, events
  llm/                   # LLM client, streaming, retry, fallback
  session/               # session storage, list, branch, merge
  modelcat/              # model catalogue, settings, selection
  skills/                # skill packs, sources, enablement
  phaseflow/             # pipeline engine, assignments, waves
  serve/                 # HTTP mux, SSE, approvals, pipeline hub
  wireguard/             # userspace WG client + server packages
  config/                # config loading, profiles
  safety/                # approval gates, RBAC, redaction
  stream/                # SSE frame writing, session IDs
  ...                    # (existing internal/ packages move here)

gophermind-server/       # server binary (module: gophermind-server)
  main.go                # entry point, flag/env parsing
  server.go              # wires serve.Deps, starts HTTP + WG
  wireguard.go           # WG server init, peer management
  go.mod                 # requires gophermind-lib

gophermind-osx/          # libui-ng desktop app (module: gophermind-osx)
  main.go                # entry point, app lifecycle
  window/
    main.go              # main window: chat + right panel
    login.go             # remote connect flow (gocloak + WG)
    settings.go          # settings panel (all config)
    pipeline.go          # pipeline/phaseflow panel
  ui/
    message_list.go      # scrollable transcript
    code_display.go      # syntax-highlighted code blocks
    input_field.go       # multi-line input
    model_picker.go      # model dropdown + filters
    session_list.go      # session browser
    approval_card.go     # inline approval prompt
  auth/
    gocloak.go           # OAuth2 client, Keychain storage
  connection/
    manager.go           # local/remote mode, WG tunnel lifecycle
  client/
    service.go           # HTTP/SSE client for all server endpoints
  go.mod                 # requires gophermind-lib
```

## Data Model & API Contract

### Server HTTP API (gophermind-server)

All endpoints are behind bearer-token auth (constant-time compare). Open paths: `/healthz`, `/readyz`, `/metrics`.

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/session` | Create or update session (id, model, profile, mode, root) |
| GET | `/session` | List sessions |
| DELETE | `/session/{id}` | Delete session |
| PATCH | `/session/{id}` | Rename session |
| GET | `/session/{id}/config` | Get session model/mode/root |
| GET | `/session/{id}/messages` | Get session conversation history |
| POST | `/session/{id}/stream` | Run one turn (SSE stream) |
| POST | `/session/{id}/approve` | Resolve pending approval |
| GET | `/models` | List available models |
| GET | `/models/catalogue` | Full catalogue with reachability/quota |
| GET | `/models/settings` | Get model picker settings |
| PATCH | `/models/settings` | Update model picker settings |
| GET | `/modes` | List available session modes |
| GET | `/skills` | List skills + sources |
| PATCH | `/skills` | Toggle skill on/off |
| POST | `/skills/sources` | Add skill source (clone repo) |
| DELETE | `/skills/sources/{id}` | Remove skill source |
| GET | `/pipeline` | Serve pipeline dashboard HTML |
| GET | `/pipeline/state` | Current task/wave/status |
| GET | `/pipeline/events` | SSE: live pipeline events |
| GET | `/pipeline/report` | Run report |
| POST | `/run` | One-shot task (webhook) |
| POST | `/run/stream` | One-shot task (SSE stream) |
| POST | `/devices` | APNs device registration |
| GET | `/backends` | List configured backends (router) |
| GET | `/backend-status` | LLM backend resolution status |
| POST | `/wg/register` | Register WG peer (gocloak token → peer config) |

### SSE Event Types (session stream)

| Event | Data | Meaning |
|-------|------|---------|
| `token` | text delta | Streaming token |
| `assistant` | full text | Complete assistant message |
| `tool_call` | `{name, args}` | Agent invoking a tool |
| `tool_result` | `{name, text}` | Tool completed |
| `usage` | `{...}` | Token usage stats |
| `approval-needed` | `{approval_id, tool, args}` | Gated tool call awaiting decision |
| `model-switched` | `{profile, model, reason}` | Auto-cycling changed model |
| `error` | message | Turn failed |
| `done` | (empty) | Turn complete |

### SSE Event Types (pipeline)

| Event | Data | Meaning |
|-------|------|---------|
| `task-status` | `{id, status, wave}` | Task state changed |
| `task-attempt` | `{task_id, model, duration, verdict, reason}` | Attempt completed |
| `wave-changed` | `{wave, state}` | Wave started/finished |
| `run-report` | `{...}` | Run ended |

### WireGuard Protocol

- Each remote backend runs its own WG interface (server side in gophermind-server).
- Desktop (client side) uses userspace WG (no TUN device): `golang.zx2c4.com/wireguard` with a memory TUN, traffic routed through a local HTTP proxy.
- Peer auth: gocloak access token presented to server's `/wg/register` endpoint → server issues a peer public key + config → desktop configures its userspace WG interface.
- Each tunnel gets a unique IP (e.g., 10.66.N.1 client, 10.66.N.2 server, /32 or /16 per backend).

### Keychain Storage (macOS)

- gocloak access/refresh tokens per backend
- WG peer private keys per backend
- Local mode bearer token (per-launch, not persisted)

## Desktop App UI Layout

```
┌─────────────────────────────────────────────────────────────────────┐
│ [●] status text  activity 12s  [folder]  [on: backend▼]  [model▼ ⚙] │  ← status bar
├──────────────────────────────────────────┬──────────────────────────┤
│                                          │  ┌────────────────────┐  │
│                                          │  │ Model Picker       │  │
│         Chat Transcript                  │  │ (dropdown+filters) │  │
│         (scrollable)                     │  ├────────────────────┤  │
│                                          │  │ Sessions           │  │
│  [user] message                          │  │ (list, expandable) │  │
│  [assistant] streaming...                │  ├────────────────────┤  │
│  [approval] tool: shell  [Y] [N]         │  │ Pipeline           │  │
│  [assistant] more text...                │  │ (file browser +    │  │
│                                          │  │  live task status) │  │
│                                          │  └────────────────────┘  │
├──────────────────────────────────────────┤                          │
│ [input field..........................] [send]                      │  ← composer
└──────────────────────────────────────────┴──────────────────────────┘
```

- Right panel is collapsible (toggle button or Cmd+\).
- Model picker is at the top of the right panel; gear icon to its right opens settings.
- Sessions list is below model picker, expandable.
- Pipeline panel is below sessions, with file browser for specs and live task status.
- Approvals appear inline in the transcript AND as a persistent bar at the bottom of the transcript area.
- Settings is a modal/overlay panel (not a separate window).

## Connection Modes

### Local Mode
- Server runs embedded (or as a separate process on localhost).
- Per-launch random 32-byte bearer token.
- No gocloak, no WireGuard.
- Desktop connects to `http://127.0.0.1:<port>`.

### Remote Mode
- User adds backend in settings (server URL, gocloak realm).
- On connect: gocloak OAuth2 (in-app login form) → access token.
- Token sent to server's `/wg/register` → server returns WG peer config.
- Desktop establishes userspace WG tunnel.
- All HTTP/SSE traffic goes through the tunnel.
- Token refresh: 1 min before expiry; on refresh failure, re-prompt login.
- Multiple remote backends: each has its own tunnel, its own token, its own IP.

## Feature List (150% Parity)

### Core (must have at launch)
1. Chat with real-time SSE streaming
2. Tool call/result display in transcript
3. Remote approvals (inline + Y/N keyboard shortcut + timeout)
4. Session CRUD (create, list, rename, delete, resume)
5. Session modes (coding, conversational, reviewer, architect, tester)
6. Session root (folder picker, native dialog)
7. Model catalogue picker (filters: reachability, capacity, terms, provider, modality)
8. Model preference order + auto-cycling on capacity
9. Model pinning per session
10. Skills panel (install sources, toggle skills, pinned commits)
11. Pipeline/phaseflow panel (file browser for specs, live task/wave/attempt status)
12. New Project from brief (file picker → seed session)
13. Multi-backend (local + N remote, switch mid-session)
14. Backend status (which LLM endpoint/model is active, fallback detection)
15. Settings panel (all config in one place)
16. Webhook/one-shot run endpoints (server-side)
17. APNs device registration (server-side)
18. Metrics, health, readiness endpoints
19. Rate limiting (per-token)
20. HMAC payload verification (optional)

### Native macOS (150% improvements)
21. Native menu bar (File, Edit, View, Window, Help) with standard shortcuts
22. Dark mode following system appearance
23. Keyboard shortcuts (Cmd+Enter send, Y/N approve, Cmd+\ toggle panel, Cmd+, settings)
24. Native dialogs (folder picker, file picker, confirm)
25. Dock icon + app lifecycle (activate, deactivate, minimize)
26. Window state persistence (size, position, panel collapsed state)
27. Connection status indicator in status bar (WG tunnel health)
28. Error handling: user-facing messages, retry, exponential backoff
29. Token expiry: transparent refresh, re-login on failure
30. Graceful degradation: partial functionality if some features unavailable

## Non-Goals
- Windows or Linux support
- Backwards compatibility with Wails build artifacts
- Offline mode (persistent connection required)
- Multiple windows (single main window + overlays)

## Success Criteria
1. Native macOS look: standard menus, native controls, proper window chrome
2. All 30 features above working
3. Chat latency <100ms, streaming throughput ≥ Wails baseline
4. <5s reconnection on network change
5. All communication over WG when remote; tokens in Keychain; never logged
6. >80% test coverage on gophermind-lib and gophermind-server
7. Linting passes, no memory leaks
