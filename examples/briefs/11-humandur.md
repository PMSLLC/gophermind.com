---
spec_version: "2.0"
id: gm-2026-10-28-006
title: humandur
language: go
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
---

## Overview

`humandur` is a small command-line tool that converts between a number of seconds and a human duration such as `1h 2m 5s`, in both directions. It is for people who read durations in logs and scripts. It is one Go binary with no configuration file.

## Features

### Format seconds and report usage errors

`humandur SECONDS` prints the duration as the units `d`, `h`, `m`, `s`, in that order, separated by single spaces, with 1 day = 86400 seconds.

- A unit whose value is zero is left out, and days are not split further.
- `0` prints `0s`; `3725` prints `1h 2m 5s`; `90061` prints `1d 1h 1m 1s`; `864000` prints `10d`.
- SECONDS is one or more ASCII digits and nothing else, from 0 to 9223372036854775807; leading zeros are allowed, so `007` prints `7s`.
- No arguments, more than one argument besides `--parse`, `--parse` with no text, or an argument starting with `--` other than `--parse` (a single `-` as in `-5` is a SECONDS value) prints `usage: humandur SECONDS | humandur --parse TEXT` on standard error and exits with code 2.
- A negative number, a sign, a decimal, an empty value, letters, spaces, separators or a value above 9223372036854775807 prints `humandur: invalid seconds "<value>"` on standard error and exits with code 2.

### Parse a duration

`humandur --parse TEXT` prints the total seconds of TEXT, one or more `<number><unit>` parts.

- A number is one or more ASCII digits; a unit is one of `d`, `h`, `m`, `s`, lower case.
- Each unit appears at most once, in any order; `1h 2m 5s` prints `3725`, `2m1h` prints `3720`, `0s` prints `0`.
- Parts are separated by nothing or by exactly one space; a leading, trailing or doubled space is invalid.
- An empty text, a repeated unit, an unknown unit, a part with no number, a number with no unit or a total above 9223372036854775807 prints `humandur: invalid duration "<text>"` on standard error and exits with code 2.
- The directions round trip: formatting the parsed total of a text that `Format` printed returns that same text.

## Architecture

- `cmd/humandur` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr` and exits with the returned code. Nothing else.
- `internal/dur`: pure functions, no printing. `Format(secs int64) string` and `Parse(text string) (int64, error)`, where `Parse` checks overflow before each addition.
- `internal/cli`: `Run(args []string, stdout, stderr io.Writer) int` checks the arguments, checks the seconds text is all digits, converts it with `strconv.ParseInt`, calls `dur`, writes the result and returns the exit code. It never calls `os.Exit`.

## Data

- There are no shared types: durations are `int64` seconds and texts are `string`.

## Constraints

- Standard library only.
- Go 1.22 or later, `gofmt` clean and `go vet` clean.
- Tests are table driven, hermetic, and use `bytes.Buffer` for output; they touch no files or environment.

## Out of scope

- Weeks, months, years, fractions, upper case units or signs.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `humandur 3725` prints `1h 2m 5s`.
- `humandur --parse "1h 2m 5s"` prints `3725`.
- `humandur -5` prints `humandur: invalid seconds "-5"` on standard error and exits with code 2.
- `humandur` prints the usage line on standard error and exits with code 2.
