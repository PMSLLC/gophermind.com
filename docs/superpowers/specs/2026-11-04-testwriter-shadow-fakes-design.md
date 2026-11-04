# Test writer: standard library shadowing, fake tests, prompt noise

Date: 2026-11-04. Source: John's review of `linkcheck-v1-20261104-r001`, call 113 (`testwrite:fn-run`, malformed), confirmed against the planner state and the code.

## Intent

The fn-run test-writer reply did not compile and tested nothing. Two of its causes are harness defects, not model quality:

1. The contract declared a package named `io` (type `io-writer`). `ResolveQualifier` (`build/planner/qualifier.go:63`) resolves contract packages before the standard library, so the prompt told the model `io` = `example.com/linkcheck/io`. A model that follows the prompt imports a package with no `io.Reader` in it.
2. The reply hard-coded every exit code by case name, never called `cli.Run` for most cases, and contained no `t.Error` or `t.Fatal`. Nothing in the harness refuses a test file that cannot fail.

Two prompt defects added noise: "Exactly one rule you must meet" is followed by two requirements, and `dependency_signatures` lists unexported helpers (`parseFlags`, `processFile`) that an external test package cannot call and the prompt forbids testing.

## Goals

- The contract stage refuses a package whose last path element is a standard library qualifier (`io`, `http`, `flag`, ...), with an error that names the package and tells the model to rename it. The refusal goes through the existing outline retry and repair paths, so the model fixes it in the same run.
- The test writer refuses a test file that never names the function under test, or that has no failing assertion. The refusal text reaches the next round through the existing retry note.
- The prompt says "Two rules you must meet".
- The node view sent to the test writer omits unexported function signatures from `dependency_signatures` when the function under test is exported and the package is not `main`. Type declarations stay. Unexported targets and main packages keep the full list, because their tests live in the same package.

### Added from the review of call 114 (the retry)

The retry fixed the one syntax error it was told about and introduced another, still used the real network (`http://example.com/redir`) and still guessed exit codes. Two more harness goals:

- A syntax refusal lists every parse error (up to 10, one per line, cascades collapsed), each with its line and column, the safe detail, and the model's own source line quoted (capped at 120 characters), the way a compile refusal already does. The compile refusal's cap rises from 3 diagnostics to 10.
- The test writer refuses a test file whose string literal names a real host (`http://` or `https://` followed by anything but `localhost`, `127.0.0.1`, `::1`, `0.0.0.0`, or a format verb). The prompt's "Do not" list says so, and tells the model to serve fixtures with `httptest.NewServer` and build URLs from `srv.URL`.

Ruling on quoting: the model's own test file may be quoted, one offending line at a time, in a refusal; the brief is never quoted. The compile refusal made this call on 2026-11-03; the syntax refusal now follows it. The canary test `TestTestwriterErrorsNeverQuoteTheReply` keeps its invariant by placing the canary on a line other than the broken one.

## Non-goals

- Filling `contract.errors` for fn-run and matching the brief section to the function. Those are enrich-stage model quality.
- Moving the worker and timeout constraints to the node they belong to.
- Teaching the model the httptest plus temp file pattern for a CLI `Run`.
- Rejecting a type that duplicates `io.Writer` by shape. Renaming the package is enough.

## Constraints

- Refusal messages never quote reply text. They name only identifiers taken from the contract (bounded with `boundedID`) or fixed words.
- Existing fixtures that stand for a good reply are updated to call the function and assert. Fixtures that stand for a bad reply keep their original defect and gain a call and an assertion, so each test still fails for its original reason.
- No cloud model. No change to anything under `~/.gophermind`.

## Decided

- Standard library names come from `StdQualifiers` (the table `ResolveQualifier` uses), applied to the last path element of `Package` on components, types and functions. `internal/types` is allowed (`types` is not in the table). `cmd/linkcheck` is allowed.
- The exercise check runs in `writeTests` after `parseTestwriteCounted`, not inside it, so the `parseTestwrite` unit tests keep their minimal fixtures.
- "Names the function" means any identifier equal to the function's Go name (from `internalTarget`) anywhere in the file except as a declared function name. That accepts `Run(...)`, `cli.Run(...)`, `s.Get(...)` and `fn := Run`.
- "Can fail" means any call whose selector is `Error`, `Errorf`, `Fatal`, `Fatalf`, `Fail` or `FailNow`, on any receiver.
- `main` is never checked (its test is `_ = main` and bypasses the parser).

## Left to the builder

- File names for the new helpers and their tests.
- Exact refusal wording, within the constraints above.
