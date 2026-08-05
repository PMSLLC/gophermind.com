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
