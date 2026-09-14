# Design Spec: macOS Desktop Conversion from Wails to libui-ng

**Date:** 2026-09-13  
**Status:** Design Phase  
**Scope:** Complete rewrite of desktop UI from Wails (React/WebView) to libui-ng (native Go UI)

---

## Overview

Convert the gophermind-desktop application from Wails + React to a native macOS application using libui-ng. This achieves a native look-and-feel while improving functionality (150% feature parity) and maintaining secure communication with the gophermind server via WireGuard tunneling and gocloak authentication.

**Goals:**
- Native macOS aesthetic and behavior (no more "WebView hack" appearance)
- 150% feature parity: preserve all current functionality plus improvements
- Secure remote communication via WireGuard + gocloak authentication
- Clean backend architecture supporting future headless/other clients
- Zero performance regressions vs. current Wails app

**Non-Goals:**
- Windows or Linux support (macOS only)
- Backwards compatibility with Wails build artifacts
- Preserving React component code (full rewrite)

---

## Architecture

### Overall Design

```
┌──────────────────────────────────────────────┐
│   libui-ng Desktop (macOS)                   │
│  ┌──────────────────────────────────────┐   │
│  │ gocloak Client Integration           │   │
│  │ (login, token management, OAuth2)    │   │
│  └──────────────────────────────────────┘   │
│           │                                  │
│           ▼                                  │
│  ┌──────────────────────────────────────┐   │
│  │ Connection Manager                   │   │
│  │ • Local mode (dev/testing)           │   │
│  │ • Remote mode (WireGuard tunnel)     │   │
│  └──────────────────────────────────────┘   │
│           │                                  │
│           ▼                                  │
│  ┌──────────────────────────────────────┐   │
│  │ libui-ng UI Layer                    │   │
│  │ • Main window (chat, code, etc.)     │   │
│  │ • Dialogs (approvals, settings)      │   │
│  │ • Native menus and controls          │   │
│  └──────────────────────────────────────┘   │
│           │                                  │
│           ▼                                  │
│  ┌──────────────────────────────────────┐   │
│  │ Service Client Layer                 │   │
│  │ (HTTP/SSE over local or WireGuard)   │   │
│  └──────────────────────────────────────┘   │
└──────────────────────────────────────────────┘
                    │
        (local or WireGuard tunnel)
                    ▼
┌──────────────────────────────────────────┐
│   gophermind Server                      │
│  ┌──────────────────────────────────────┐
│  │ WireGuard Server (peer management)   │
│  └──────────────────────────────────────┘
│  ┌──────────────────────────────────────┐
│  │ HTTP Server                          │
│  │ (serves content locally & over WG)   │
│  └──────────────────────────────────────┘
│  ┌──────────────────────────────────────┐
│  │ Service Layer (extraction phase)     │
│  │ • Chat operations                    │
│  │ • Project management                 │
│  │ • Model/backend selection            │
│  │ • Approvals and execution            │
│  └──────────────────────────────────────┘
└──────────────────────────────────────────┘
```

### Connection Modes

**Local Mode** (development/single-machine):
- Desktop and server running on same macOS machine
- No WireGuard needed
- Desktop communicates with server on `127.0.0.1:<port>`
- Direct HTTP/SSE calls from libui-ng
- No gocloak required (or gocloak used for credentials only)

**Remote Mode** (primary production use case):
- Desktop connects to gophermind server elsewhere
- gocloak handles authentication and identity
- Desktop establishes WireGuard tunnel to server
- All communication over encrypted tunnel
- Token-based authentication for tunnel access

### Backend Refactoring

The current HTTP server stays, but we extract a clean **service layer** that decouples business logic from transport:

**New file structure:**
```
internal/
  service/          ← NEW: extracted business logic
    chat.go
    approvals.go
    models.go
    project.go
    ...
  wireguard/        ← NEW: peer/key management
    server.go
    config.go
    ...
  serve/
    http.go         ← calls service layer
    ...
```

**Key principle:** The HTTP server calls the service layer. A future direct-call interface could also call the service layer without going through HTTP.

---

## Desktop Application Components

### 1. gocloak Integration (`internal/desktop/auth/gocloak.go`)

**Responsibility:** Handle OAuth2 authentication with gocloak, store credentials securely.

**Features:**
- Accept gocloak server URL + realm
- Implement OAuth2 Authorization Code flow (within-app login UI, no browser popup)
- Store access token in macOS Keychain
- Refresh token when expired
- Logout and credential cleanup

**Inputs:**
- gocloak URL (e.g., `https://auth.example.com`)
- Client ID and secret (from gocloak realm)
- Redirect URI (app-specific, e.g., `gophermind://auth/callback`)

**Outputs:**
- Access token (validated, unexpired)
- Token refresh mechanism
- User identity (for display in UI)

### 2. Connection Manager (`internal/desktop/connection/manager.go`)

**Responsibility:** Handle mode selection, WireGuard setup, and service endpoint management.

**Features:**
- Detect local vs. remote server (user-configurable)
- For remote: establish WireGuard tunnel using gocloak token
- For local: connect directly to localhost
- Manage TUN device on macOS (may require elevated permissions)
- Graceful reconnection on network change
- Health checks to detect disconnections

**Inputs:**
- Mode: "local" or "remote"
- Server endpoint (for remote)
- gocloak token (for remote)

**Outputs:**
- Active service endpoint (`http://127.0.0.1:<local-wg-port>` or `http://<wg-ip>:<port>`)
- Connection status (connected/disconnected/error)

### 3. libui-ng UI Layer

**Responsibility:** Display the user interface using native macOS widgets.

**Key Windows/Dialogs:**

- **Main Window:** Chat interface, code display, streaming output
  - Message list (scrollable, text + code blocks)
  - Input field for chat
  - Model/backend selector
  - Real-time streaming display
  
- **Approval Modal:** For operation approvals
  - Operation details
  - Approve/Reject buttons
  
- **Project Dialog:** New/open projects
  - Project browser
  - Project creation form
  
- **Settings Window:** Configuration
  - Server URL (for remote mode)
  - gocloak realm settings
  - Model preferences
  - Cache/history options
  
- **Login Window:** Authentication (shown before main window if not authenticated)
  - Username field
  - Password field
  - Login button
  - Error messages

**Architecture:**
- Each window is a separate libui-ng Box/Group with native controls
- Event handlers call service client methods (see below)
- Async operations use goroutines, update UI on completion via channels
- Dark mode support via native macOS appearance detection

### 4. Service Client Layer (`internal/desktop/client/service.go`)

**Responsibility:** HTTP/SSE client for communicating with gophermind server.

**Methods (correspond to current server endpoints):**
- `Chat(ctx, message) → Stream[ChatResponse]` (SSE for streaming)
- `Approve(ctx, approvalID, decision) → error`
- `GetModels(ctx) → []Model`
- `SelectModel(ctx, modelID) → error`
- `GetProjects(ctx) → []Project`
- `NewProject(ctx, spec) → Project`
- `ExecutePhase(ctx, phaseID) → Stream[ExecutionEvent]` (SSE)
- `GetSessionInfo(ctx) → SessionInfo`

**Implementation:**
- HTTP client with token header for auth
- SSE client for streaming responses
- Request timeout and retry logic
- Error handling and user-facing error messages

---

## Data Flow Examples

### Example 1: User Logs In (Remote Mode)

1. **libui-ng:** Show login window
2. **User:** Enters gocloak credentials
3. **gocloak client:** POST to gocloak, get access token
4. **gocloak client:** Store token in Keychain
5. **Connection Manager:** Receive token, initiate WireGuard connection
6. **WireGuard Client:** Use token to auth with WireGuard server, establish tunnel
7. **Connection Manager:** Verify connectivity with health check
8. **libui-ng:** Hide login, show main window
9. **Service Client:** All subsequent requests include token in Authorization header

### Example 2: User Sends Chat Message

1. **libui-ng:** User types message, clicks Send
2. **libui-ng:** Call `client.Chat(ctx, message)`
3. **Service Client:** POST to `http://<endpoint>/api/chat` with message (over local connection or WireGuard tunnel)
4. **Server:** Receive request, validate token (remote mode only), process chat
5. **Service Client:** Receive SSE stream, read chunks
6. **libui-ng:** Append each chunk to message display, update in real-time
7. **libui-ng:** Mark message complete when stream ends

### Example 3: User Approves Operation

1. **Server:** Long-running operation needs approval, sends SSE event to desktop
2. **Service Client:** Receive event, parse approval details
3. **libui-ng:** Show approval modal with operation details
4. **User:** Click Approve
5. **libui-ng:** Call `client.Approve(ctx, approvalID, "approved")`
6. **Service Client:** POST to `http://<endpoint>/api/approvals/<id>/respond`
7. **Server:** Receive approval, resume operation

---

## Implementation Phases

### Phase 1: Backend Service Extraction (Non-Breaking)

Extract business logic from the current HTTP server without breaking existing clients.

**Files to create:**
- `internal/service/` package hierarchy
- `internal/service/chat.go` — extract chat operations
- `internal/service/approvals.go` — extract approval logic
- `internal/service/models.go` — model/backend selection
- `internal/service/projects.go` — project operations
- (Similar for other major domains)

**Files to modify:**
- `desktop/server.go` — refactor HTTP handlers to call service layer
- Tests: add `internal/service/*_test.go` files

**Verification:**
- All existing tests pass
- HTTP API remains unchanged
- `make test` succeeds
- No behavioral changes to Wails app

### Phase 2: WireGuard Server Integration

Add WireGuard peer management to gophermind server.

**Files to create:**
- `internal/wireguard/` package
- `internal/wireguard/server.go` — peer/interface management
- `internal/wireguard/config.go` — config generation
- `internal/wireguard/auth.go` — token-based peer validation

**Files to modify:**
- `desktop/server.go` — initialize WireGuard server at startup
- GOPHERMIND.toml — add wireguard config parameters

**Verification:**
- WireGuard interface comes up at startup
- `wg show` displays active interface
- Peers can connect (using test harness)

### Phase 3: Desktop libui-ng Application (Parallel to Phase 2)

Build the new desktop UI in libui-ng.

**Files to create:**
- `desktop/libui-ng/` (top-level desktop app, supersedes current desktop/)
- `internal/desktop/auth/gocloak.go` — gocloak client
- `internal/desktop/connection/manager.go` — connection management
- `internal/desktop/client/service.go` — HTTP/SSE client
- `desktop/libui-ng/main.go` — entry point
- `desktop/libui-ng/window/` — UI windows
  - `login.go`
  - `main.go`
  - `approvals.go`
  - `projects.go`
  - `settings.go`
- `desktop/libui-ng/ui/` — reusable widgets
  - `message_list.go`
  - `code_display.go`
  - `input_field.go`

**Dependencies:**
- `github.com/andlabs/ui` (libui-ng Go bindings)
- `golang.org/x/oauth2` (OAuth2 client)
- `golang.zx2c4.com/wireguard` (WireGuard Go client)

**Verification:**
- App launches without panic
- Login window appears
- Can connect to local server in dev mode
- Chat works end-to-end

### Phase 4: WireGuard Client Integration

Wire up WireGuard tunneling in desktop app.

**Files to modify:**
- `internal/desktop/connection/manager.go` — add WireGuard tunnel establishment
- `internal/desktop/auth/gocloak.go` — token passed to connection manager

**Verification:**
- Desktop can establish WireGuard tunnel to remote server
- Traffic is encrypted (verify with tcpdump)
- Server receives requests over tunnel
- Tunnel reconnects on network change

### Phase 5: Testing & Polish

End-to-end testing, performance profiling, UI refinement.

**Activities:**
- Integration tests: login → chat → approvals → logout
- Performance: measure message latency, streaming throughput
- Error scenarios: network drops, token expiry, server unavailable
- macOS-specific: keyboard shortcuts, menu integration, dark mode
- Accessibility: VoiceOver support if feasible

### Phase 6: Deprecation & Migration

Retire Wails app, communicate migration to users.

**Activities:**
- Update build system (remove Wails/React frontend)
- Update documentation
- Release notes highlighting native improvements
- Support for transitioning Wails users

---

## Key Features & Functional Requirements

### 1. Chat Interface

- **Display:** Scrollable message history with alternating user/assistant styles
- **Streaming:** Display tokens in real-time as they arrive via SSE
- **Code Blocks:** Syntax highlighting for code snippets (use libui or custom rendering)
- **Copy Buttons:** Quick-copy for code blocks
- **Input:** Multi-line text input, send on Cmd+Enter

### 2. Model Selection

- **Dropdown/Picker:** Display available models from server
- **Selected Model Display:** Show current model in main window
- **Settings Integration:** Model preference persisted across sessions

### 3. Approvals

- **Modal Popup:** Non-dismissible dialog for approval decisions
- **Details Display:** Full operation details (what is being approved)
- **Async:** User can approve while chat is streaming
- **Timeout:** If approval is pending >5 min, show warning

### 4. Project Management

- **Browser:** List projects, filter by name
- **New Project:** Form for project creation (metadata, initial prompt)
- **Open Project:** Load project state, pre-populate chat history

### 5. Error Handling & Resilience

- **Network Errors:** Display user-facing error, allow retry
- **Token Expiry:** Refresh token transparently, or re-show login if refresh fails
- **Server Unavailable:** Show status banner, retry with exponential backoff
- **Graceful Degradation:** Partial functionality if some features unavailable

### 6. Settings & Configuration

- **Server URL:** For remote mode, editable in settings
- **Cache Location:** Where to store local project data
- **Model Defaults:** Default model on startup
- **Dark Mode:** Follow system preference

---

## Testing Strategy

### Unit Tests

- **gocloak client:** Token request/refresh, error cases
- **Connection Manager:** Mode selection, reconnection logic
- **Service Client:** Request formatting, SSE parsing, error handling

### Integration Tests

- **Full Login Flow:** gocloak → token → connection → authenticated request
- **Chat End-to-End:** Login → send message → receive streaming response → display
- **Approval Flow:** Server sends approval → desktop shows modal → user approves → response received
- **Remote Mode:** WireGuard tunnel established, traffic encrypted

### Manual Testing

- macOS Ventura/Sonoma/Sequoia (latest 3 versions)
- Network conditions: WiFi, wired, VPN active, switching networks
- Long sessions: 8+ hours without disconnection
- Large messages: 100K+ tokens of streaming output

---

## Success Criteria

1. **Native Look:** Application is indistinguishable from other macOS apps (standard menus, native buttons, proper window chrome)
2. **Feature Parity + 150%:** All Wails features work, plus improvements (better error messaging, settings UI, keyboard shortcuts)
3. **Performance:** Chat latency <100ms, streaming throughput ≥ Wails baseline
4. **Reliability:** 99.9% uptime in production use, <5 second reconnection on network change
5. **Security:** All communication over WireGuard when remote, tokens never logged, Keychain used for storage
6. **Code Quality:** >80% test coverage, linting passes, no memory leaks detected

---

## Risks & Mitigation

| Risk | Impact | Mitigation |
|------|--------|-----------|
| libui-ng feature gaps (e.g., rich text, custom rendering) | UI polish regresses | Prototype early (Phase 3), identify gaps before deep investment |
| WireGuard permission model (macOS requires sudo) | Poor UX, user friction | Use privileged helper (SwiftUI overlay) for TUN device setup |
| gocloak token refresh timing | Auth interruptions | Refresh token 1 min before expiry, handle refresh errors gracefully |
| Service layer extraction breaks existing API | Wails app breaks | Extract behind HTTP API, test both old and new paths in parallel |
| macOS API changes between OS versions | Breakage on newer macOS | Target Ventura (13.0+), test on latest 3 versions |
| Streaming over WireGuard latency | Poor UX for chat | Profile early, optimize SSE batching if needed |

---

## Files Modified/Created

### Backend (Server)

**Created:**
- `internal/service/` (package)
- `internal/service/chat.go`
- `internal/service/approvals.go`
- `internal/service/models.go`
- `internal/service/projects.go`
- `internal/service/*_test.go`
- `internal/wireguard/` (package)
- `internal/wireguard/server.go`
- `internal/wireguard/config.go`
- `internal/wireguard/auth.go`

**Modified:**
- `desktop/server.go` (refactor to use service layer)
- `desktop/main.go` (initialize WireGuard)
- `GOPHERMIND.toml` (add WireGuard config)

### Desktop (UI)

**Created:**
- `desktop/libui-ng/` (new top-level desktop app)
- `desktop/libui-ng/main.go`
- `desktop/libui-ng/window/` (package)
- `desktop/libui-ng/window/login.go`
- `desktop/libui-ng/window/main.go`
- `desktop/libui-ng/window/approvals.go`
- `desktop/libui-ng/window/projects.go`
- `desktop/libui-ng/window/settings.go`
- `desktop/libui-ng/ui/` (package)
- `desktop/libui-ng/ui/message_list.go`
- `desktop/libui-ng/ui/code_display.go`
- `internal/desktop/` (package)
- `internal/desktop/auth/gocloak.go`
- `internal/desktop/connection/manager.go`
- `internal/desktop/client/service.go`

**Removed (after migration):**
- `desktop/frontend/` (React app)
- `desktop/app.go`, `desktop/backend.go`, etc. (Wails-specific)

**Deprecated (still in repo, but not shipped):**
- Old `desktop/` Wails entry point

---

## Success Definition

Ship macOS app that:
1. Looks and behaves like a native macOS application
2. Supports both local (dev) and remote (gocloak + WireGuard) connections
3. Has all Wails features plus measurable improvements
4. Passes test suite and manual QA
5. Receives user validation: "This looks and feels right for macOS"

---

## Open Questions / Decisions Needed

1. **WireGuard Permissions:** Use privileged helper (HelperTool pattern) or ask for sudo at startup?
2. **Offline Mode:** Should desktop cache chat history and work offline, or require persistent connection?
3. **Keyboard Shortcuts:** Any macOS-specific shortcuts beyond Cmd+Enter for send?
4. **Error Messages:** How verbose should in-app errors be vs. logging to file?

