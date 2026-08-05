# Design: `write_xlsx` tool

## Problem

Gophermind's tabular tools (`inspect_data`, `read_parquet`, `set_csv_cell`,
`data_transform`) each read exactly one CSV/JSON/JSONL/Parquet file. None of
them write `.xlsx`, and none combine multiple sources into a single output.
There is no way for the agent to take information it has gathered — from
other tool calls, files it read, query results — and hand it to the user as
one spreadsheet.

## Goal

A single agent-callable tool that writes a `.xlsx` workbook, with one sheet
per data set the agent hands it. That's what "combining information from
multiple sources" means here: the agent assembles the data in-conversation
(it is not required to come from existing files on disk), and this tool is
the sink that turns it into one file with multiple sheets.

## Non-goals

- Reading existing `.xlsx` files.
- Appending to / editing an existing workbook.
- Merging or joining existing CSV/JSON/Parquet files on disk (that's a
  different, file-based feature; not this one).
- Cell styling, formulas, formatting, charts.

## Interface

New tool, following the existing `internal/tools` constructor pattern
(`func XxxTool(root string) Tool` returning a `Tool{Name, Description,
Schema, Run}` literal — see `internal/tools/csv_edit.go`):

```
Name: write_xlsx
Args:
  path:   string            — output file path, relative to repo root
  sheets: array of {
    name:    string         — sheet name
    headers: []string       — optional; written as row 1 if present
    rows:    [][]any        — row data; values pass through as-is
                               (string/number/bool), no coercion
  }
```

- Creates the workbook at `path`, one sheet per entry in `sheets`, in order.
- Overwrites `path` if it already exists (matches how the agent produces a
  fresh artifact each time; no partial-update mode).
- `path` is sandboxed through `safety.SafeJoin(root, path)` before any disk
  access, same as every other file-writing tool.

## Safety

`write_xlsx` writes files, so it is added to the gated-tool list in
`internal/safety/safety.go` next to `set_csv_cell` — same permission gate as
every other mutating tool.

## Dependency

Nothing in the current module graph (`go.mod`/`go.sum`) can write OOXML.
Adds `github.com/xuri/excelize/v2`.

## Files

- `internal/tools/xlsx.go` — the tool (`WriteXLSX(root string) Tool`)
- `internal/tools/xlsx_test.go` — multi-sheet write + re-read via excelize to
  verify cell values; path-traversal rejection test (mirrors existing
  `csv_edit_test.go` safety test)
- `cmd/gophermind/main.go` — register `tools.WriteXLSX(cfg.RootDir)`
  alongside the other data tools (~line 745-783)
- `internal/safety/safety.go` — add `write_xlsx` to the gated list (~line 132)

## Shipping

Merge to `main` on completion. No version bump / release cut for this pass.
