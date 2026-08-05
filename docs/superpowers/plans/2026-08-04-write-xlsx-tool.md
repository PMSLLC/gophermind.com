# write_xlsx Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an agent-callable `write_xlsx` tool that writes a multi-sheet Excel `.xlsx` workbook from data the agent hands it, so it can combine information gathered mid-conversation into one file.

**Architecture:** A new `internal/tools/xlsx.go` file follows the existing `internal/tools` constructor pattern (`func WriteXLSX(root string) Tool`) used by `SetCSVCell`. It uses `github.com/xuri/excelize/v2` to build the workbook in memory (one sheet per entry in the `sheets` argument, optional header row, then data rows written as-is) and save it to a sandboxed path. It is registered in `cmd/gophermind/main.go`'s tool list and added to the gated-tool switch in `internal/safety/safety.go`, matching every other file-writing tool.

**Tech Stack:** Go 1.25, `github.com/xuri/excelize/v2` (new dependency), stdlib `encoding/json`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-04-write-xlsx-tool-design.md`
- No `.xlsx` reading, appending, or file-merge support — creation only, overwrites `path` if it exists.
- No cell styling/formulas/charts.
- `path` must go through `safety.SafeJoin(root, path)` before any disk access.
- New tool is added to the gated list in `internal/safety/safety.go` (write tools require approval).
- Ship target: merge to `main`, no version bump / release cut.

---

### Task 1: Implement and test the `write_xlsx` tool

**Files:**
- Create: `internal/tools/xlsx.go`
- Create: `internal/tools/xlsx_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: `tools.Tool` struct and `object`/`str` schema helpers (`internal/tools/tool.go:16`, `:68`, `:80`); `safety.SafeJoin(root, rel string) (string, error)` (`internal/safety/safety.go:16`); test helper `run(t *testing.T, tool Tool, args string) (string, error)` (`internal/tools/batch3_files_test.go:13`).
- Produces: `func WriteXLSX(root string) Tool` with `Tool.Name == "write_xlsx"` — Task 2 registers this in `main.go` and gates it by that exact name in `safety.go`.

- [ ] **Step 1: Add the excelize dependency**

Run: `go get github.com/xuri/excelize/v2`

This adds `github.com/xuri/excelize/v2` (and its transitive deps) to `go.mod`/`go.sum`.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/xlsx_test.go`:

```go
package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteXLSXMultiSheet(t *testing.T) {
	dir := t.TempDir()
	tool := WriteXLSX(dir)

	args := `{
		"path": "report.xlsx",
		"sheets": [
			{"name": "Leads", "headers": ["id", "name"], "rows": [[1, "alice"], [2, "bob"]]},
			{"name": "Totals", "rows": [["count", 2]]}
		]
	}`
	out, err := run(t, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "report.xlsx") {
		t.Errorf("result should mention the file: %q", out)
	}

	f, err := excelize.OpenFile(filepath.Join(dir, "report.xlsx"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Leads" || sheets[1] != "Totals" {
		t.Fatalf("unexpected sheets: %v", sheets)
	}

	if got, _ := f.GetCellValue("Leads", "A1"); got != "id" {
		t.Errorf("Leads A1 = %q, want %q", got, "id")
	}
	if got, _ := f.GetCellValue("Leads", "B1"); got != "name" {
		t.Errorf("Leads B1 = %q, want %q", got, "name")
	}
	if got, _ := f.GetCellValue("Leads", "A2"); got != "1" {
		t.Errorf("Leads A2 = %q, want %q", got, "1")
	}
	if got, _ := f.GetCellValue("Leads", "B2"); got != "alice" {
		t.Errorf("Leads B2 = %q, want %q", got, "alice")
	}
	if got, _ := f.GetCellValue("Leads", "B3"); got != "bob" {
		t.Errorf("Leads B3 = %q, want %q", got, "bob")
	}
	if got, _ := f.GetCellValue("Totals", "A1"); got != "count" {
		t.Errorf("Totals A1 = %q, want %q", got, "count")
	}
	if got, _ := f.GetCellValue("Totals", "B1"); got != "2" {
		t.Errorf("Totals B1 = %q, want %q", got, "2")
	}
}

func TestWriteXLSXRequiresSheets(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, WriteXLSX(dir), `{"path":"x.xlsx","sheets":[]}`); err == nil {
		t.Error("empty sheets should error")
	}
}

func TestWriteXLSXRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	args := `{"path":"../escape.xlsx","sheets":[{"name":"S","rows":[["x"]]}]}`
	if _, err := run(t, WriteXLSX(dir), args); err == nil {
		t.Error("path escaping repo root should error")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tools/... -run TestWriteXLSX -v`
Expected: FAIL — `WriteXLSX` is undefined.

- [ ] **Step 4: Implement `WriteXLSX`**

Create `internal/tools/xlsx.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xuri/excelize/v2"

	"gophermind/internal/safety"
)

// WriteXLSX returns a gated tool that writes an Excel .xlsx workbook, one
// sheet per entry in the sheets argument. It creates path if absent and
// overwrites it if present — there is no append/edit mode. This is the sink
// for combining information the agent has assembled mid-conversation (tool
// output, file contents, query results) into one spreadsheet.
func WriteXLSX(root string) Tool {
	return Tool{
		Name:        "write_xlsx",
		Description: "Write an Excel .xlsx workbook, one sheet per entry in sheets, in order. Overwrites path if it already exists.",
		Schema: object(map[string]any{
			"path": str("Output .xlsx file path, relative to the repo root."),
			"sheets": map[string]any{
				"type":        "array",
				"description": "One sheet per entry, written in order.",
				"items": object(map[string]any{
					"name": str("Sheet name."),
					"headers": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional header row, written as row 1.",
					},
					"rows": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "array"},
						"description": "Row data; each row is an array of cell values (string/number/bool), written as-is.",
					},
				}, "name", "rows"),
			},
		}, "path", "sheets"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path   string `json:"path"`
				Sheets []struct {
					Name    string   `json:"name"`
					Headers []string `json:"headers"`
					Rows    [][]any  `json:"rows"`
				} `json:"sheets"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if len(a.Sheets) == 0 {
				return "", fmt.Errorf("sheets must not be empty")
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}

			f := excelize.NewFile()
			defer f.Close()

			for i, s := range a.Sheets {
				if s.Name == "" {
					return "", fmt.Errorf("sheet %d: name is required", i)
				}
				if i == 0 {
					if err := f.SetSheetName("Sheet1", s.Name); err != nil {
						return "", fmt.Errorf("sheet %q: %w", s.Name, err)
					}
				} else if _, err := f.NewSheet(s.Name); err != nil {
					return "", fmt.Errorf("sheet %q: %w", s.Name, err)
				}

				row := 1
				if len(s.Headers) > 0 {
					headerRow := make([]any, len(s.Headers))
					for j, h := range s.Headers {
						headerRow[j] = h
					}
					if err := f.SetSheetRow(s.Name, "A1", &headerRow); err != nil {
						return "", fmt.Errorf("sheet %q: write header: %w", s.Name, err)
					}
					row = 2
				}
				for _, r := range s.Rows {
					cell, err := excelize.CoordinatesToCellName(1, row)
					if err != nil {
						return "", fmt.Errorf("sheet %q: %w", s.Name, err)
					}
					rowCopy := r
					if err := f.SetSheetRow(s.Name, cell, &rowCopy); err != nil {
						return "", fmt.Errorf("sheet %q: write row %d: %w", s.Name, row, err)
					}
					row++
				}
			}

			if err := f.SaveAs(full); err != nil {
				return "", fmt.Errorf("save %s: %w", a.Path, err)
			}
			return fmt.Sprintf("wrote %s (%d sheet(s))", a.Path, len(a.Sheets)), nil
		},
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/... -run TestWriteXLSX -v`
Expected: PASS for `TestWriteXLSXMultiSheet`, `TestWriteXLSXRequiresSheets`, `TestWriteXLSXRejectsPathEscape`.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/tools/xlsx.go internal/tools/xlsx_test.go
go vet ./internal/tools/...
git add go.mod go.sum internal/tools/xlsx.go internal/tools/xlsx_test.go
git commit -m "feat(tools): add write_xlsx tool for multi-sheet Excel output"
```

---

### Task 2: Register and gate `write_xlsx`

**Files:**
- Modify: `cmd/gophermind/main.go:749` (insert after `tools.SetCSVCell(cfg.RootDir)`)
- Modify: `internal/safety/safety.go:132`

**Interfaces:**
- Consumes: `tools.WriteXLSX(root string) Tool` from Task 1; `safety.Gated(tool string) bool` switch at `internal/safety/safety.go:127-134`.
- Produces: nothing further downstream — this is the last task.

- [ ] **Step 1: Register the tool in the toolset**

In `cmd/gophermind/main.go`, immediately after the `tools.SetCSVCell(cfg.RootDir)` line (line 749), add:

```go
		tools.WriteXLSX(cfg.RootDir),                                          // gated: write a multi-sheet .xlsx workbook
```

- [ ] **Step 2: Write the failing test**

`internal/safety/mcp_gate_test.go` has `TestGatedBuiltinsUnchanged`, which lists gated builtins in a `[]string` literal:

```go
	for _, name := range []string{"write_file", "run_shell", "edit_file", "fetch_url"} {
```

Add `"write_xlsx"` to that list:

```go
	for _, name := range []string{"write_file", "run_shell", "edit_file", "fetch_url", "write_xlsx"} {
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/safety/... -run TestGatedBuiltinsUnchanged -v`
Expected: FAIL — `write_xlsx` should be gated, but isn't yet.

- [ ] **Step 4: Add `write_xlsx` to the gated-tool switch**

In `internal/safety/safety.go`, change line 132 from:

```go
	case "write_file", "edit_file", "run_shell", "move_file", "delete_file", "mkdir", "apply_patch", "fetch_url", "http_request", "create_migration", "set_csv_cell":
```

to:

```go
	case "write_file", "edit_file", "run_shell", "move_file", "delete_file", "mkdir", "apply_patch", "fetch_url", "http_request", "create_migration", "set_csv_cell", "write_xlsx":
```

- [ ] **Step 5: Run test to verify it passes, then run the full suite**

Run: `go test ./internal/safety/... -run TestGatedBuiltinsUnchanged -v`
Expected: PASS

Run: `go build ./... && go test ./...`
Expected: build succeeds, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/gophermind/main.go internal/safety/safety.go internal/safety/mcp_gate_test.go
git commit -m "feat(tools): wire write_xlsx into the toolset and gate it"
```

- [ ] **Step 7: Push to main**

```bash
git push origin main
```
