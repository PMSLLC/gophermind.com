# Roadmap: gophermind-osx

## Phases

- [ ] **Phase 1: gophermind-lib Foundation** - Move existing internal packages into gophermind-lib as a standalone Go module; add WireGuard userspace packages
- [ ] **Phase 2: gophermind-server** - Build the standalone server binary that imports gophermind-lib
- [ ] **Phase 3: gophermind-osx Core** - Build the libui-ng desktop app skeleton and core connection layer
- [ ] **Phase 4: gophermind-osx UI** - Build the full UI: chat, panels, settings, pipeline
- [ ] **Phase 5: Integration and Polish** - End-to-end testing, error handling, performance

## Phase Details

### Phase 1: gophermind-lib Foundation
**Goal**: A standalone Go module (gophermind-lib) containing all shared packages (agent, llm, session, modelcat, skills, phaseflow, serve, config, safety, stream, wireguard) that both gophermind-server and gophermind-osx import. Existing tests pass with the new module path. WireGuard userspace client and server packages are available for use.

**Depends on**: none

Plans:
- [ ] 01-01: Create gophermind-lib/ module, move existing internal packages, fix imports
- [ ] 01-02: Add userspace WireGuard client and server packages to gophermind-lib
- [ ] 01-03: Define serve.Deps interface and SSE event types as the API contract

### Phase 2: gophermind-server
**Goal**: A standalone gophermind-server binary that imports gophermind-lib, serves all HTTP endpoints (session CRUD, SSE stream, approvals, models, skills, pipeline, run, devices), manages WireGuard peer registration, and passes integration tests.

**Depends on**: 1

Plans:
- [ ] 02-01: Server entry point, config loading, flag/env parsing, graceful shutdown
- [ ] 02-02: Wire serve.Deps, start HTTP listener, register all routes
- [ ] 02-03: WireGuard server init, peer registration endpoint, per-backend tunnel management
- [ ] 02-04: Integration tests for all HTTP endpoints and WG registration

### Phase 3: gophermind-osx Core
**Goal**: A libui-ng macOS desktop app that launches, connects to gophermind-server (local or remote via userspace WireGuard), authenticates via gocloak OAuth2, and can send/receive chat messages with streaming.

**Depends on**: 1

Plans:
- [ ] 03-01: Create gophermind-osx/ module, main.go, app lifecycle, window creation
- [ ] 03-02: HTTP/SSE service client for all server endpoints
- [ ] 03-03: Connection manager: local/remote mode, WG tunnel lifecycle, health checks
- [ ] 03-04: gocloak OAuth2 client, Keychain storage, token refresh

### Phase 4: gophermind-osx UI
**Goal**: Full desktop UI with chat transcript (streaming, tool calls, code blocks), inline approvals (Y/N shortcut), right panel (model picker, sessions, pipeline), settings panel, and native macOS features (menu bar, dark mode, shortcuts, dialogs).

**Depends on**: 3

Plans:
- [ ] 04-01: Main window chat transcript, streaming display, input field, tool call rendering
- [ ] 04-02: Inline approval cards, Y/N keyboard shortcut, approval bar, timeout
- [ ] 04-03: Collapsible right panel with model picker, sessions list, pipeline panel
- [ ] 04-04: Model catalogue dropdown with filters, preference order, auto-cycling
- [ ] 04-05: Session list with CRUD, rename, delete, resume, modes, root
- [ ] 04-06: Pipeline/phaseflow panel with file browser, live task/wave/attempt status
- [ ] 04-07: Settings panel: backends, gocloak, model prefs, skills, all config
- [ ] 04-08: Native macOS: menu bar, dark mode, shortcuts, dialogs, dock, window state

### Phase 5: Integration and Polish
**Goal**: End-to-end tested, error-resilient, performant desktop app. All flows work in local and remote mode. Error scenarios handled gracefully. Performance meets or exceeds Wails baseline.

**Depends on**: 2, 4

Plans:
- [ ] 05-01: E2E integration tests: login, chat, approvals, pipeline, logout (local and remote)
- [ ] 05-02: Error scenario tests: network drops, token expiry, server unavailable, WG reconnect
- [ ] 05-03: Performance: message latency, streaming throughput, WG overhead measurement
- [ ] 05-04: UI refinement, accessibility, final QA
