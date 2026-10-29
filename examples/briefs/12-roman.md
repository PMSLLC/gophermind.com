---
spec_version: "2.0"
id: gm-2026-10-28-007
title: roman
language: go
base_branch: main
landing: commit
on_ambiguity: halt
---

## Overview

`roman` is a small command-line tool that converts between integers and Roman numerals. It is for people who want a quick, scriptable answer. It is one Go binary with no configuration file.

## Features

### Integer to numeral

`roman 1994` prints `MCMXCIV` and `roman 3999` prints `MMMCMXCIX`, each followed by a newline.

- The number is 1 to 3999, written with digits only: no sign, spaces or leading plus.
- Subtractive forms are used: 4 is IV, 9 is IX, 40 is XL, 90 is XC, 400 is CD, 900 is CM.
- Anything else (0, 4000, negative, decimal, letters, empty) prints `roman: invalid number "<value>"` on standard error and exits with code 2.

### Numeral to integer

`roman -r MCMXCIV` prints `1994` followed by a newline.

- Upper or lower case is accepted, so `mcmxciv` is 1994.
- Only the canonical form is valid: a numeral is valid exactly when converting its value back with `ToRoman` gives the same numeral.
- IIII, VX, IC, MMMM and an empty string are invalid and print `roman: invalid numeral "<value>"` on standard error and exit with code 2.
- The two directions round trip for every N from 1 to 3999.
- No arguments, too many arguments, or an unknown flag prints `usage: roman N | roman -r NUMERAL` on standard error and exits with code 2.

## Architecture

- `cmd/roman` (package `main`): calls `cli.Run` with `os.Args[1:]`, `os.Stdout` and `os.Stderr` and exits with the returned code. Nothing else.
- `internal/roman`: pure functions `ToRoman(n int) (string, error)` and `FromRoman(s string) (int, error)`. `FromRoman` parses, then checks the result with `ToRoman`. No printing.
- `internal/cli`: `Run(args []string, stdout, stderr io.Writer) int` parses the arguments, calls `internal/roman`, prints, and returns the exit code. It never calls `os.Exit`.

## Data

- There are no shared types: numbers are `int` and numerals are `string`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table driven, hermetic, and use `bytes.Buffer` for output.

## Out of scope

- Numerals above 3999, fractions, and non-canonical forms such as IIII.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- A test converts every N from 1 to 3999 with `ToRoman` and back with `FromRoman` and asserts the original N.
- `roman 1994` prints `MCMXCIV`.
- `roman -r MCMXCIV` prints `1994`.
- `roman 0` exits with code 2.
- `roman -r IIII` exits with code 2.
