# GopherMind Build & Test Workflow

Use when building, testing, or verifying relevant changes. Follow the shared rules.
Inspect the actual `go.mod`/`go.work`, CI, Makefile, build tags, and required toolchain
before choosing commands. Run from the correct module/workspace location. Record
pre-existing failures and preserve unrelated edits.

## Fast feedback and full verification

Start with the smallest affected package/test, then expand before declaring code
changes verified. Replace example paths/test names with real ones.

```bash
go test -count=1 -run '^TestName$' ./path/to/package
go test -count=1 ./path/to/package
```

For the normal single-module Go gate:

```bash
go build ./... && go test ./... && go vet ./...
```

Follow CI for additional modules, tags, generators, or platform checks; `./...`
is not a guarantee of workspace-wide coverage. `go build ./...` is a compile
check, not a guarantee of a fresh `./gophermind` executable. Read `make build`
before using it or assuming its output/stamping matches a direct Go build.

## Formatting is a separate, failing gate

Use the repository's formatting scope. Otherwise select the intended existing Go
files explicitly, including new files; do not rewrite the entire checkout.
Replace the example paths below. This check does not modify files:

```sh
(
    set -eu
    set -- ./path/to/changed.go ./path/to/another.go
    unformatted=$(gofmt -l "$@")
    if [ -n "$unformatted" ]; then
        printf '%s
' "$unformatted"
        exit 1
    fi
)
```

`gofmt -l` lists differences but does not itself fail for formatting differences.
Require both a successful formatter invocation and empty output. Format only
intended files with `gofmt -w`, inspect their diff, and rerun the check. Syntax,
missing-file, and tool errors must fail rather than look like a clean result.

## Conditional checks

For changed concurrent behavior, run focused `go test -race -count=1` when supported.
For flakes, use bounded repeated/shuffled runs and record the seed. Fix shared
state, ownership, cleanup, or synchronization; do not use arbitrary sleeps as a
permanent fix. Prefer existing fake clocks and synchronization primitives.

For golden tests, inspect actual versus expected output and the intended behavior.
Use `GOLDEN_UPDATE=1` only if the test code implements it, and only for an intended
change. Inspect the generated diff, then rerun with updating disabled and
`-count=1`. Never bless a failure just to make the gate green.

Run `make ios-test` only for relevant changes after verifying the target and Xcode/
simulator availability. TUI rendering changes need an appropriate interactive or
snapshot check. Do not claim either ran in an unsupported environment.

`make ios-deploy` and `make release` are not ordinary verification. Require explicit
authorization and the documented environment; consult `docs/RELEASING.md` rather
than exposing signing/notarization credentials or assuming a machine-specific path.

## Completion evidence

Report exact commands, scope, and passed/failed/blocked/skipped/not-run status.
Separate cached test results from fresh regression runs. Explain relevant missing
checks. For documentation-only changes, validate the changed material and state
why compilation or platform testing was not applicable. Do not commit automatically.
