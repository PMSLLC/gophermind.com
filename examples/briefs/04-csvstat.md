---
spec_version: "2.0"
id: gm-2026-09-29-003
title: csvstat
language: go
repo: ~/src/csvstat
base_branch: main
landing: diff_only
on_ambiguity: assume_and_document
env:
  - name: CSVSTAT_MAX_ROWS
    purpose: Stop reading after this many data rows; 0 means no limit
    default: "0"
network:
  - host: proxy.golang.org
    purpose: Module downloads
    critical: true
  - host: sum.golang.org
    purpose: Module checksum database
    critical: true
budget:
  max_context_tokens: 4096
  max_revisions: 0
---

## Overview

`csvstat` is a small command-line tool that reads one CSV file and prints a summary of every column: how many
values it holds, how many are empty, what type the values look like, and basic statistics for numbers. It is for
people who receive an unfamiliar spreadsheet export and want to know what is in it before opening it. It is one
Go binary with no configuration file.

This is the smallest example brief: one package, two features, no secrets, no network calls at run time. The
harness is told to stop at a diff (`landing: diff_only`) and to make its own conservative choices instead of
asking (`on_ambiguity: assume_and_document`), and no revision rounds are allowed (`max_revisions: 0`), so every
task has to pass on the first model that attempts it or move on down the fallback chain.

## Features

### Summarize a file

`csvstat data.csv` reads the file, treating the first row as the header, and prints one block per column.

Acceptance criteria:

- Each block shows the column name, the count of non-empty values, and the count of empty values.
- The type is `int` when every non-empty value parses as a base-10 integer, `float` when every non-empty value
  parses as a number and at least one has a decimal point or exponent, `bool` when every non-empty value is one of
  `true`, `false`, `TRUE`, `FALSE`, and `string` otherwise.
- For `int` and `float` columns the block also shows `min`, `max`, and `mean`, and the mean is printed with two
  decimal places.
- The number of distinct non-empty values is shown for every column.
- A file with a header and no data rows prints each column with a count of 0 and does not fail.
- A row with a different number of fields than the header is skipped, counted, and reported in one line at the
  end: `skipped 3 malformed rows`.
- The exit code is 0 on success and 1 on any error, with the message on standard error.

### Output formats

`csvstat --format json data.csv` prints the same information as a JSON array with one object per column.

Acceptance criteria:

- The accepted values of `--format` are `text` (the default) and `json`; anything else exits with code 2 and a
  usage message.
- The JSON keys are `name`, `count`, `empty`, `type`, `distinct`, and, for numeric columns only, `min`, `max`, `mean`.
- Reading `-` as the file name reads standard input.
- If `CSVSTAT_MAX_ROWS` is set to a positive number, reading stops after that many data rows and the last line of
  the text output says `stopped after N rows`.

## Architecture

Packages:

- `main` in the repository root: flag parsing and printing only.
- `internal/stats`: reads records from an `io.Reader` and returns a `[]ColumnSummary`. No printing.
- `internal/render`: turns a `[]ColumnSummary` into text or JSON.

Data flow: `main` opens the file, `stats.Summarize` reads it once from top to bottom, `render` formats the result.
Type detection keeps one running state per column so the file is never held in memory.

Patterns to follow:

- `stats.Summarize` takes an `io.Reader` and a row limit, so tests never touch the file system.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` and only `main` decides the exit code.

## Data

ColumnSummary:

- `Name string`, taken from the header.
- `Count int`, non-empty values seen.
- `Empty int`, empty values seen.
- `Type string`, one of `int`, `float`, `bool`, `string`.
- `Distinct int`.
- `Min`, `Max`, `Mean float64`, meaningful only when `Type` is `int` or `float`.

## Constraints

- Standard library only.
- Go 1.22 or later.
- `gofmt` clean and `go vet` clean.
- Tests are table-driven and use `strings.NewReader`, not files.
- Memory use does not grow with the number of rows.

## Out of scope

- Reading Excel, TSV with quoting quirks, or compressed files.
- Sorting, filtering, or writing any file.
- Guessing date formats.
- Parallel reading.

## Acceptance

- `go build ./...` succeeds.
- `go vet ./...` reports nothing.
- `go test ./...` passes.
- `printf 'a,b\n1,x\n2,\n' | go run . -` prints two column blocks; `a` is `int` with `min` 1 and `max` 2, `b` is
  `string` with one empty value.
- `printf 'a\n1\n' | go run . --format json -` prints a JSON array with one object.
- `go run . --format yaml x.csv` exits with code 2.
