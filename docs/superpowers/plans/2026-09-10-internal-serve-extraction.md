# internal/serve Extraction Implementation Plan (Desktop App, Phase 0)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move gophermind's HTTP serve layer out of `package main` into an importable `internal/serve` package, splitting mux construction from listening, so a desktop app can embed it in-process and so `serve` gains graceful shutdown.

**Architecture:** Nine files (1,596 lines) move verbatim from `cmd/gophermind` to `internal/serve`. `runServe`'s eight positional parameters become a `Deps` struct. `NewMux(Deps, Options) (*http.ServeMux, error)` builds and validates without listening; `Serve(ctx, net.Listener, http.Handler) error` listens with graceful shutdown. `cmd/gophermind` becomes a thin caller.

**Tech Stack:** Go 1.26.5, module `gophermind`. Standard library only. No new dependency.

**Spec:** `docs/superpowers/specs/2026-09-10-desktop-app-design.md` (the "Phase 0 (prerequisite): extract `internal/serve`" section)

## Global Constraints

- Module is `gophermind`; import paths are `gophermind/internal/...`.
- Go 1.26.5. Standard library only. Do NOT add a dependency. `go.mod` and `go.sum` must stay byte-unchanged.
- **This is a PURE REFACTOR. No behavior change.** The eleven moving test files must pass with their assertions UNCHANGED. That is the gate on every task. If a moved test fails, the refactor is wrong, not the test.
- NO em dashes and NO emoji on any line you ADD. Files being moved may already contain them; moving a file verbatim preserves its content and is correct. Do not introduce new ones.
- Every exported symbol gets a doc comment. Most symbols here are currently unexported and must STAY unexported unless the plan says otherwise; a wholesale export would balloon the package's API surface.
- Preserve git history: move files with `git mv`, never delete-and-recreate.
- Run `go build ./... && go test ./...` before every commit. Both green.
- Commit after each task, message ending with exactly:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw
  ```

## The extraction set

Verified self-contained: the only `package main` symbols these nine files reference from outside their own set are `readSessionMode`/`writeSessionMode` (`session_mode.go`), `readSessionModel`/`writeSessionModel` (`session_model.go`), and `writeSSEEvent` (`sse.go`) - which is why those three move too. Nothing else in `package main` is needed.

| File | Lines | Role |
|---|---|---|
| `apns.go` | 423 | APNs push for remote approval |
| `session_serve.go` | 371 | Session CRUD + stream handlers |
| `webhook.go` | 308 | `runServe`, mux assembly, `/run` handlers |
| `approval.go` | 163 | Approval registry and remote gate |
| `session_mode.go` | 93 | Per-session mode persistence |
| `ratelimit.go` | 86 | Token-keyed rate limiter |
| `sse.go` | 61 | SSE frame writers |
| `session_model.go` | 51 | Per-session model persistence |
| `metrics.go` | 40 | Counters for `/metrics` |

Eleven test files move with them: `apns_test.go`, `approval_test.go`, `approval_timeout_test.go`, `metrics_test.go`, `ratelimit_test.go`, `session_messages_test.go`, `session_mode_test.go`, `session_model_test.go`, `session_serve_test.go`, `sse_test.go`, `webhook_test.go`.

---

### Task 1: Move the nine files and their tests, unchanged

**Files:**
- `git mv` 9 source files and 11 test files from `cmd/gophermind/` to `internal/serve/`
- Modify: `cmd/gophermind/main.go` (the single `runServe` call site, line ~1198)

**Interfaces:**
- Consumes: nothing new.
- Produces: package `serve` containing the moved symbols. `runServe` is temporarily exported as `Run` with its existing eight-parameter signature so `main.go` can still call it. Task 2 replaces that signature.

This task is deliberately mechanical: move, fix the package clause, export the minimum needed to compile, and prove the tests still pass. Do NOT restructure anything yet.

- [ ] **Step 1: Move the files with git mv**

```bash
mkdir -p internal/serve
for f in apns approval metrics ratelimit session_mode session_model session_serve sse webhook; do
  git mv cmd/gophermind/$f.go internal/serve/$f.go
done
git mv cmd/gophermind/approval_timeout_test.go internal/serve/
git mv cmd/gophermind/session_messages_test.go internal/serve/
for f in apns approval metrics ratelimit session_mode session_model session_serve sse webhook; do
  [ -f cmd/gophermind/${f}_test.go ] && git mv cmd/gophermind/${f}_test.go internal/serve/${f}_test.go
done
ls internal/serve/ | wc -l   # expect 20
```

- [ ] **Step 2: Change the package clause in all moved files**

```bash
sed -i '' 's/^package main$/package serve/' internal/serve/*.go
grep -c '^package serve' internal/serve/*.go | grep -v ':1' && echo "SOME FILE HAS THE WRONG CLAUSE" || echo ok
```

- [ ] **Step 3: Build and read the errors**

Run: `go build ./... 2>&1 | head -40`

Expect two classes of error, and fix ONLY these:
1. `internal/serve` referencing symbols still in `package main`. There should be none, but if the compiler finds one, that symbol's file belongs in the extraction set: `git mv` it too and record which one in your report.
2. `cmd/gophermind/main.go` no longer finding `runServe` and friends.

For (2): export `runServe` as `Run` in `internal/serve/webhook.go`, keeping its exact eight-parameter signature, and give it a doc comment. Export any other symbol `main.go` genuinely still needs, and **list every symbol you had to export in your report** - that list is the real API surface and Task 2 will shrink it.

Update `cmd/gophermind/main.go`'s call site to `serve.Run(...)` and add the `gophermind/internal/serve` import.

- [ ] **Step 4: Run the moved tests**

Run: `go test ./internal/serve/ -v 2>&1 | tail -30`
Expected: every test passes with NO assertion edited. Type and identifier accommodations (a now-exported name) are fine; a changed assertion is not.

- [ ] **Step 5: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(serve): move the HTTP serve layer to internal/serve

Nine files and eleven test files move verbatim from package main so the
serve layer can be imported. runServe is exported as Run with its
existing signature; Task 2 replaces that with a Deps struct.

Pure refactor, no behavior change: every moved test passes with its
assertions unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 2: Split construction from listening

**Files:**
- Modify: `internal/serve/webhook.go` (`Run` becomes `NewMux` + `Serve`)
- Modify: `cmd/gophermind/main.go` (call site)
- Test: `internal/serve/serve_test.go` (new)

**Interfaces:**
- Consumes: the package from Task 1.
- Produces:
```go
// Deps is everything the mux needs. A nil func or pointer disables the
// routes that depend on it, exactly as the old positional nils did.
type Deps struct {
	Run             func(ctx context.Context, task string) (string, error)
	Stream          func(ctx context.Context, task string, emit func(string)) error
	Metrics         *serveMetrics
	SessionTurn     SessionTurn
	Approvals       *approvalRegistry
	Devices         *deviceStore
	SessionMessages func(id string) ([]json.RawMessage, bool, error)
	ListModels      func() ([]string, error)
}

// Options carries per-deployment settings that used to be read from the
// environment inside runServe.
type Options struct {
	// Token is the bearer token every task-running route requires. Empty
	// means "read GOPHERMIND_SERVE_TOKEN". NewMux returns an error if both
	// are empty: this endpoint runs shell commands and must never be open.
	Token string
}

func NewMux(d Deps, opt Options) (*http.ServeMux, error)
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error
func Addr() string // the old serveAddr, exported for cmd/gophermind
```

- [ ] **Step 1: Write the failing test**

Create `internal/serve/serve_test.go`:

```go
package serve

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMuxRejectsEmptyToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "")
	_, err := NewMux(Deps{}, Options{})
	if err == nil {
		t.Fatal("NewMux accepted an empty token; this endpoint runs shell commands")
	}
}

func TestNewMuxAcceptsExplicitToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "")
	mux, err := NewMux(Deps{}, Options{Token: "explicit"})
	if err != nil {
		t.Fatalf("NewMux with an explicit token: %v", err)
	}
	if mux == nil {
		t.Fatal("NewMux returned a nil mux and no error")
	}
}

func TestNewMuxFallsBackToEnvToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "from-env")
	if _, err := NewMux(Deps{}, Options{}); err != nil {
		t.Fatalf("NewMux should read the env token: %v", err)
	}
}

// Two calls must yield independent muxes, so one process can serve more
// than one listener.
func TestNewMuxReturnsIndependentMuxes(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	a, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("NewMux returned the same mux twice")
	}
}

// Serve must return when its context is cancelled, which is the graceful
// shutdown the old runServe had no way to do.
func TestServeReturnsOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, http.NewServeMux()) }()

	// Let the server come up, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Returned, which is the point. Either nil or a shutdown error is fine.
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of context cancel")
	}
}

// The health endpoint needs no token, so it is the cheapest proof that a
// mux built by NewMux actually serves.
func TestNewMuxServesHealthz(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/serve/ -run 'TestNewMux|TestServe' -v`
Expected: FAIL to build, `undefined: NewMux`.

- [ ] **Step 3: Implement**

In `internal/serve/webhook.go`, split `Run` into `NewMux` and `Serve`:

- `NewMux` takes everything `Run` did up to and including the `mux` assembly, with the positional parameters read from `d` instead. Resolve the token as `firstNonEmpty(opt.Token, os.Getenv("GOPHERMIND_SERVE_TOKEN"))` and return an error when the result is empty, preserving `serveToken`'s existing refusal and its message.
- `Serve` wraps `http.Server{Handler: h}`, calls `srv.Serve(ln)` in a goroutine, waits on `ctx.Done()`, then calls `srv.Shutdown` with a bounded timeout (use 10 seconds) and returns.
- Keep the existing `fmt.Fprintf(os.Stderr, ...)` startup banner, but move it to the caller in `cmd/gophermind` so the library does not print. Note in your report that you moved it.
- Export `Addr()` wrapping the existing `serveAddr()`.
- Delete the now-unused `Run` wrapper.

In `cmd/gophermind/main.go`, replace the `serve.Run(...)` call with:

```go
		mux, err := serve.NewMux(serve.Deps{
			Run: run, Stream: stream, Metrics: metrics,
			SessionTurn: sessionTurn, Approvals: approvals, Devices: devStore,
			SessionMessages: loadMessages, ListModels: listModels,
		}, serve.Options{})
		if err != nil {
			return err
		}
		addr := serve.Addr()
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "gophermind serving on %s (POST /run, /run/stream; /healthz /readyz)\n", addr)
		return serve.Serve(context.Background(), ln, mux)
```

Match the surrounding error-handling style; read the neighbouring code first.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/serve/ -v`
Expected: all green, INCLUDING the eleven moved test files with unchanged assertions.

- [ ] **Step 5: Verify serve still works end to end**

```bash
go build -o /tmp/gm-serve ./cmd/gophermind
GOPHERMIND_SERVE_TOKEN=testtoken GOPHERMIND_SERVE_ADDR=127.0.0.1:8099 /tmp/gm-serve serve &
sleep 2
curl -s -o /dev/null -w 'healthz=%{http_code}\n' http://127.0.0.1:8099/healthz
curl -s -o /dev/null -w 'no-token=%{http_code}\n' -X POST http://127.0.0.1:8099/run
kill %1 2>/dev/null
rm -f /tmp/gm-serve
```
Expected: `healthz=200` and a non-2xx for the unauthenticated POST. Paste the real output into your report.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(serve): split NewMux from Serve, add graceful shutdown

runServe built its mux and called ListenAndServe in one function, so the
serve layer could not be embedded and had no way to stop. NewMux builds
and validates without listening; Serve takes a net.Listener and shuts
down on context cancel.

Options.Token lets a caller supply a token without setting a process-wide
env var. The refusal to start without one is preserved: NewMux errors
when both the option and the env var are empty.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

### Task 3: Shrink the exported surface

**Files:**
- Modify: `internal/serve/*.go` (unexport what Task 1 exported for expedience)
- Test: the existing suite is the proof

**Interfaces:**
- Produces: a package whose exported API is exactly `Deps`, `Options`, `NewMux`, `Serve`, `Addr`, `SessionTurn`, and whatever `Deps`'s field types require.

Task 1 exported symbols to make things compile. Most should not be part of the API.

- [ ] **Step 1: List what is exported**

```bash
go doc -all ./internal/serve | grep -E '^(func|type|var|const) ' | head -40
```

- [ ] **Step 2: Unexport everything not required by Deps, Options, NewMux, Serve, or Addr**

For each exported symbol not in that set, lowercase it and fix references. `SessionTurn` stays exported because it is a `Deps` field type. A type used as a `Deps` field must stay exported; a helper used only inside the package must not.

- [ ] **Step 3: Verify the surface**

```bash
go doc -all ./internal/serve | grep -E '^(func|type) ' 
```
Expected: only the intended set. Paste it into your report.

- [ ] **Step 4: Full suite**

Run: `go build ./... && go test ./...`
Expected: all green, assertions unchanged.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(serve): shrink the package's exported surface

Task 1 exported symbols to make the move compile. Only Deps, Options,
NewMux, Serve, Addr and the types Deps' fields require belong in the API;
everything else is package-internal again.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013bRLGHm9RiQnECTY4Cxzsw"
```

---

## Final verification

- [ ] `go build ./... && go test ./... -race` green.
- [ ] `go.mod` and `go.sum` byte-unchanged: `git diff 9ff5150..HEAD -- go.mod go.sum` is empty.
- [ ] The eleven moved test files have no assertion changes: review `git log -p` for them and confirm only package clauses and identifier casing moved.
- [ ] `gophermind serve` starts, answers `/healthz` with 200, and rejects an unauthenticated `POST /run`.
- [ ] `git log --follow internal/serve/webhook.go` shows history from before the move.
- [ ] `go doc -all ./internal/serve` lists only the intended exported surface.
