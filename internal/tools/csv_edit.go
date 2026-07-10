package tools

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gophermind/internal/safety"
)

// SetCSVCell returns a gated tool that updates a single cell in a CSV file,
// addressed by 1-based row and column (row 1 is the header). It preserves the
// rest of the file, so tabular data can be edited safely without a full rewrite.
func SetCSVCell(root string) Tool {
	return Tool{
		Name:        "set_csv_cell",
		Description: "Set one cell in a CSV file, addressed by 1-based row and column (row 1 = header). Preserves the rest of the file.",
		Schema: object(map[string]any{
			"path":  str("CSV file path relative to the repo root."),
			"row":   map[string]any{"type": "integer", "description": "1-based row (row 1 is the header)."},
			"col":   map[string]any{"type": "integer", "description": "1-based column."},
			"value": str("New cell value."),
		}, "path", "row", "col", "value"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path  string `json:"path"`
				Row   int    `json:"row"`
				Col   int    `json:"col"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}
			r := csv.NewReader(strings.NewReader(string(data)))
			r.FieldsPerRecord = -1
			records, err := r.ReadAll()
			if err != nil {
				return "", fmt.Errorf("parse csv: %w", err)
			}
			if a.Row < 1 || a.Row > len(records) {
				return "", fmt.Errorf("row %d out of range (1..%d)", a.Row, len(records))
			}
			rowVals := records[a.Row-1]
			if a.Col < 1 || a.Col > len(rowVals) {
				return "", fmt.Errorf("col %d out of range (1..%d)", a.Col, len(rowVals))
			}
			rowVals[a.Col-1] = a.Value

			var b strings.Builder
			w := csv.NewWriter(&b)
			if err := w.WriteAll(records); err != nil {
				return "", fmt.Errorf("write csv: %w", err)
			}
			w.Flush()
			if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
				return "", fmt.Errorf("save %s: %w", a.Path, err)
			}
			return fmt.Sprintf("set %s[row %d, col %d] = %q", a.Path, a.Row, a.Col, a.Value), nil
		},
	}
}
