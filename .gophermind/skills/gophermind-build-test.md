# GopherMind Build & Test Workflow

Workflow for building, testing, and verifying changes in the gophermind repo.

## Build Commands

Always run from the repo root (directory containing `go.mod`).

```bash
# Quick build (no stamping, local use only)
go build ./...              # Build entire workspace
make build                  # Same, via Makefile (produces ./gophermind)

# Full verify gate (run before commit)
go test ./...               # Full test suite
go vet ./...                # Static analysis
gofmt -l .                  # Check formatting (report differences)
gofmt -w .                  # Auto-format (write changes)

# iOS verification (requires Xcode, iOS simulator or device)
make ios-test               # iOS unit tests on auto-picked simulator
make ios-deploy             # Build + install on connected iPhone
```

## Testing Patterns

### Run specific tests
```bash
go test -run TestName ./path/to/package
go test -v ./internal/agent/        # Verbose output for one package
go test -cover ./...                 # Coverage summary per package
```

### Golden file tests (prompt, rendering, ASCII art)
- Located in `internal/prompt/testdata/`
- Re-bless with `GOLDEN_UPDATE=1 go test ./internal/prompt/`
- **CRITICAL**: Always visually inspect the output before updating golden files. Golden update will bake in bugs if you don't look at what's being baked.

### Flaky test troubleshooting
- Check for goroutine leaks: look for `*test.go` files calling `t.Parallel()` with shared state
- Test isolation: ensure each test cleans up (temp dirs, files, connections)
- Timing: if a test flakes intermittently, it likely relies on timing assumptions — add `time.Sleep` or use synchronization primitives

## Verification Before Commit

```bash
# Full gate (always run this before git commit)
go test ./... && \
go vet ./... && \
gofmt -l . && \
echo "✓ All checks pass"
```

If any check fails:
- **Test failure**: `go test -run <TestName> -v` to inspect
- **Vet failure**: Read the diagnostic; it's usually an unused var or type issue
- **Format**: Run `gofmt -w .` to fix, then re-commit

## Release Build

Requires `MACOS_SIGN_IDENTITY` and `MACOS_NOTARY_PROFILE` environment variables (only set up in CI and John's build machine).

```bash
make release
```

See `docs/RELEASING.md` for full release process.

## Common Issues

**"go: cannot find module"** — Ensure you're in the repo root (where `go.mod` lives). Check with `pwd` and `ls go.mod`.

**"rsvg-convert not found"** (during logo rendering) — Uses ImageMagick 7 at `/opt/homebrew/bin/magick`. If running elsewhere, the script must fail loudly, not silently produce blank PNGs.

**iOS tests fail with "device not available"** — `make ios-test` auto-picks a simulator. If none exist, create one via Xcode (Preferences > Devices and Simulators > Simulators > +).

**"golden file mismatch"** — The test output changed. Run `GOLDEN_UPDATE=1 go test ./internal/prompt/` to re-bless, but **inspect the output first** (`cat internal/prompt/testdata/<test>.golden`).

## Anatomy of a Test

```go
// internal/agent/loop_test.go
func TestAgentDecomposesTask(t *testing.T) {
    // Setup
    ctx := context.Background()
    mockProvider := newMockProvider(t)
    mockProvider.OnComplete = func(prompt) (string, error) {
        return `{"tasks":[{"name":"read","path":"main.go"}]}`, nil
    }
    
    // Execute
    ag := &Agent{Provider: mockProvider}
    tasks, err := ag.Decompose(ctx, "fix the bug in main.go")
    
    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(tasks) != 1 {
        t.Errorf("expected 1 task, got %d", len(tasks))
    }
}
```

- Table-driven for multiple scenarios
- Mocks for external services (model provider, file system where needed)
- Assert on behavior, not implementation details
- Use `t.Fatalf` for must-pass checks; `t.Errorf` for non-fatal assertions
