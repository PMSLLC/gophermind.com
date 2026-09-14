# ROADMAP: gophermind-osx

## Phase 1: Shared Library Foundation (gophermind-lib)

Move existing `internal/` packages into `gophermind-lib/` as a standalone Go module. Add the WireGuard userspace client/server packages. This is the foundation everything else imports.

**Tasks:**
- `lib-scaffold`: Create `gophermind-lib/` module, move existing packages, fix imports
- `lib-wireguard`: Implement userspace WG client + server packages in `gophermind-lib/wireguard/`
- `lib-serve-contract`: Define the serve.Deps interface and all HTTP handler signatures as the API contract

## Phase 2: Server Binary (gophermind-server)

Build the standalone server that wires the service layer, HTTP mux, and WireGuard server together.

**Tasks:**
- `server-main`: Entry point, config loading, flag/env parsing
- `server-http`: Wire serve.Deps, start HTTP listener, all endpoints
- `server-wg`: WireGuard server init, peer registration endpoint, tunnel management
- `server-test`: Integration tests for all endpoints + WG registration

## Phase 3: Desktop Core (gophermind-osx)

Build the libui-ng desktop application: connection management, auth, HTTP/SSE client, and main window with chat.

**Tasks:**
- `osx-scaffold`: Create `gophermind-osx/` module, main.go, app lifecycle, libui-ng init
- `osx-auth`: gocloak OAuth2 client, Keychain storage, token refresh
- `osx-connection`: Connection manager (local/remote mode, WG tunnel lifecycle, health checks)
- `osx-client`: HTTP/SSE service client for all server endpoints
- `osx-main-window`: Main window layout (status bar, transcript, composer, right panel)
- `osx-chat`: Chat transcript with streaming, tool calls, approvals (Y/N + buttons)

## Phase 4: Desktop Panels & Features

Build the right-side panel (model picker, sessions, pipeline) and remaining features.

**Tasks:**
- `osx-model-picker`: Model catalogue picker with filters, preference order, auto-cycling
- `osx-sessions`: Session list (CRUD, rename, resume, modes, root)
- `osx-pipeline`: Pipeline/phaseflow panel (file browser, live task status)
- `osx-settings`: Settings panel (backends, gocloak, model prefs, skills)
- `osx-skills`: Skills panel (install sources, toggle skills)
- `osx-native`: Native macOS polish (menus, dark mode, shortcuts, dialogs, dock)

## Phase 5: Integration & Polish

End-to-end testing, error handling, performance, and final polish.

**Tasks:**
- `int-e2e`: Integration tests (login → chat → approvals → pipeline → logout)
- `int-error`: Error scenarios (network drops, token expiry, server unavailable, reconnection)
- `int-perf`: Performance profiling (latency, streaming throughput, memory)
- `int-polish`: UI refinement, accessibility, final QA
