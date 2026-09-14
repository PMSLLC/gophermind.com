# ROADMAP: gophermind-osx

## Phase 1: gophermind-lib Foundation

Move existing `internal/` packages into `gophermind-lib/` as a standalone Go module. Add the WireGuard userspace packages. This is the shared foundation everything else imports.

**Tasks:**
- `lib-scaffold`: Create `gophermind-lib/` module, move existing internal packages, fix imports
- `lib-wireguard`: Add userspace WireGuard client + server packages to gophermind-lib
- `lib-serve-contract`: Define the serve.Deps interface and SSE event types as the API contract (pins the data model)

## Phase 2: gophermind-server

Build the standalone server binary that imports gophermind-lib.

**Tasks:**
- `server-main`: Entry point, config loading, flag/env parsing, graceful shutdown
- `server-http`: Wire serve.Deps, start HTTP listener, register all routes
- `server-wg`: WireGuard server init, peer registration endpoint, per-backend tunnel management
- `server-test`: Integration tests for all HTTP endpoints + WG registration

## Phase 3: gophermind-osx Core

Build the libui-ng desktop app skeleton and core connection layer.

**Tasks:**
- `osx-scaffold`: Create `gophermind-osx/` module, main.go, app lifecycle, window creation
- `osx-client`: HTTP/SSE service client (all endpoints from the contract)
- `osx-connection`: Connection manager (local mode, remote mode, WG tunnel lifecycle, health checks)
- `osx-auth`: gocloak OAuth2 client, Keychain storage, token refresh

## Phase 4: gophermind-osx UI

Build the full UI: chat, panels, settings, pipeline.

**Tasks:**
- `osx-chat`: Main window chat transcript, streaming display, input field, tool call rendering
- `osx-approvals`: Inline approval cards, Y/N keyboard shortcut, approval bar, timeout
- `osx-right-panel`: Collapsible right panel with model picker, sessions list, pipeline panel
- `osx-model-picker`: Model catalogue dropdown with filters, preference order, auto-cycling
- `osx-sessions`: Session list (CRUD, rename, delete, resume, modes, root)
- `osx-pipeline`: Pipeline/phaseflow panel with file browser, live task/wave/attempt status
- `osx-settings`: Settings panel (backends, gocloak, model prefs, skills, all config)
- `osx-native`: Native menu bar, dark mode, keyboard shortcuts, native dialogs, dock, window state

## Phase 5: Integration & Polish

End-to-end testing, error handling, performance.

**Tasks:**
- `int-e2e`: Integration tests: login → chat → approvals → pipeline → logout (local + remote)
- `int-error`: Error scenarios: network drops, token expiry, server unavailable, WG reconnect
- `int-perf`: Performance: message latency, streaming throughput, WG overhead measurement
- `int-polish`: UI refinement, accessibility, final QA

## Dependencies

```
lib-scaffold ──┬── lib-wireguard
               └── lib-serve-contract ──┬── server-main ── server-http ── server-wg ── server-test
                                        │
                                        ├── osx-scaffold ── osx-client ──┬── osx-chat
                                        │                                ├── osx-approvals
                                        │                                ├── osx-right-panel
                                        │                                │    ├── osx-model-picker
                                        │                                │    ├── osx-sessions
                                        │                                │    └── osx-pipeline
                                        │                                └── osx-settings
                                        │
                                        ├── osx-connection
                                        └── osx-auth

All Phase 4 UI tasks ── int-e2e ── int-error ── int-perf ── int-polish
```
