---
spec_version: "2.0"
id: gm-2026-09-29-004
title: retryx
language: go
repo: ~/src/retryx
base_branch: main
work_branch: gm/retryx-lib
landing: commit
on_ambiguity: halt
milestone_approvals: false
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
budget:
  max_context_tokens: 6000
  max_revisions: 1
---

## Overview

`retryx` is a small Go library for retrying an operation that can fail temporarily: a network call, a lock
acquisition, a queue read. A caller passes a function and a policy, and the library calls the function until it
succeeds, the caller gives up, the context ends, or the function reports that the failure is permanent. It is meant
to be imported by other Go programs, so its exported names and its behaviour under cancellation matter more than any
command-line surface. There is no command-line surface at all.

This example brief shows a library rather than a program. The harness commits each verified task to its own work
branch (`landing: commit`, `work_branch: gm/retryx-lib`) and stops to ask whenever the brief is unclear
(`on_ambiguity: halt`). Approval between waves is off (`milestone_approvals: false`), and one revision round is
allowed.

## Features

### Retry with backoff

`retryx.Do(ctx, fn, opts...)` calls `fn` until it returns nil, and returns the last error when it gives up.

Acceptance criteria:

- `fn` has the signature `func(ctx context.Context, attempt int) error`, where `attempt` starts at 1.
- `Do` returns nil on the first attempt that returns nil, and never calls `fn` again after that.
- The default is 5 attempts with exponential backoff starting at 100 milliseconds and doubling each time, capped
  at 5 seconds.
- Between attempts `Do` waits using a timer that stops as soon as the context is done, so cancelling the context
  returns within a few milliseconds and never leaves a goroutine or timer behind.
- When attempts run out, the returned error wraps the last error from `fn` and satisfies
  `errors.Is(err, retryx.ErrExhausted)`.
- When the context ends first, the returned error wraps `ctx.Err()` and the last error from `fn`, and satisfies
  `errors.Is(err, context.Canceled)` or `errors.Is(err, context.DeadlineExceeded)` as appropriate.

### Policies

Options change how long `Do` waits and how many times it tries.

Acceptance criteria:

- `WithAttempts(n)` sets the maximum number of calls; a value below 1 is treated as 1.
- `WithConstant(d)` waits the same duration between every pair of attempts.
- `WithExponential(base, max)` waits `base`, then `2*base`, then `4*base`, never more than `max`.
- `WithJitter(fraction)` multiplies each wait by a random factor between `1-fraction` and `1`, and the fraction is
  clamped to the range 0 to 1.
- `WithRand(r *rand.Rand)` makes jitter deterministic so tests can assert exact waits.
- `WithSleeper(fn)` replaces the real timer so tests run without waiting; the default sleeper is used when unset.

### Failure classification

The function can say which failures should not be retried.

Acceptance criteria:

- `retryx.Permanent(err)` wraps an error so that `Do` returns it immediately without further attempts, and
  `errors.Unwrap` on the result gives back the original error.
- `retryx.After(err, d)` wraps an error with a suggested wait; the next wait is `d` instead of the policy's wait,
  but never more than the configured maximum.
- `WithRetryIf(func(error) bool)` restricts retrying to errors for which the function returns true; other errors
  are returned immediately.
- Passing a nil error to `Permanent` or `After` returns nil.

## Architecture

A single package, `retryx`, at the module root, with these files:

- `retryx.go`: `Do`, the loop, and the exported errors.
- `options.go`: the `Option` type and every `With...` function.
- `backoff.go`: pure functions that compute the wait for an attempt number, with no timers and no randomness of
  their own.
- `errors.go`: `Permanent`, `After`, and the wrapper types.

Data flow: `Do` builds a config from the options, then loops. Each iteration calls `fn`, classifies the error,
asks `backoff.go` for the wait, and hands the wait to the sleeper.

Patterns to follow:

- Options are functions over an unexported config struct. The zero configuration is valid.
- Every exported identifier has a doc comment that starts with its name.
- No package-level mutable state and no `init` functions.

## Data

Config (unexported):

- `attempts int`, default 5.
- `wait func(attempt int) time.Duration`, default exponential from 100ms capped at 5s.
- `jitter float64`, default 0.
- `retryIf func(error) bool`, default nil meaning retry everything except permanent errors.
- `sleeper func(ctx context.Context, d time.Duration) error`, default a timer that honours the context.

Errors:

- `ErrExhausted`, a sentinel returned (wrapped) when attempts run out.
- `permanentError{err error}` and `afterError{err error; d time.Duration}`, unexported wrappers that implement
  `Unwrap`.

## Constraints

- Standard library only, and no third-party modules in the test files either.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table-driven, deterministic, and never sleep for real: they use `WithSleeper`.
- The package passes `go test -race`.
- No goroutines are started by the library.

## Out of scope

- Circuit breakers, rate limiting, and bulkheads.
- Retrying HTTP requests specifically; callers wrap their own client.
- Metrics or logging hooks.
- Generic return values: `fn` returns only an error in this version.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test -race ./...` passes.
- An example test shows `Do` succeeding on the third attempt with `WithConstant(time.Millisecond)`.
- A test cancels the context during a long wait and asserts that `Do` returns in under 50 milliseconds.
- A test asserts `errors.Is(err, retryx.ErrExhausted)` after 3 failed attempts with `WithAttempts(3)`.
