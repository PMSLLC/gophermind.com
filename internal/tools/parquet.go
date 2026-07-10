package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gophermind/internal/safety"

	"github.com/parquet-go/parquet-go"
)

// parquetPreviewRows caps how many rows read_parquet returns.
const parquetPreviewRows = 20

// ReadParquet returns a read-only tool that reads a Parquet file's schema and a
// row preview — support for modern columnar data-lake files alongside CSV/JSON.
func ReadParquet(root string) Tool {
	return Tool{
		Name:        "read_parquet",
		Description: "Read a Parquet file: reports the columns and a preview of rows (capped). Read-only.",
		Schema:      object(map[string]any{"path": str("Path to a .parquet file, relative to the repo root.")}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
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
			cols, rows, err := parseParquet(data)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "format: parquet\ncolumns (%d): %s\nrows shown: %d\n\n", len(cols), strings.Join(cols, ", "), len(rows))
			b.WriteString(strings.Join(cols, " | ") + "\n")
			for _, r := range rows {
				b.WriteString(strings.Join(r, " | ") + "\n")
			}
			return truncate(b.String()), nil
		},
	}
}

// parseParquet reads a Parquet file's column names and up to parquetPreviewRows
// rows as strings.
func parseParquet(data []byte) ([]string, [][]string, error) {
	pf, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("open parquet: %w", err)
	}
	var cols []string
	for _, path := range pf.Schema().Columns() {
		cols = append(cols, strings.Join(path, "."))
	}
	r := parquet.NewReader(pf)
	defer r.Close()

	var out [][]string
	buf := make([]parquet.Row, parquetPreviewRows)
	n, err := r.ReadRows(buf)
	if err != nil && n == 0 {
		// io.EOF with 0 rows is just an empty file; anything else is a real error.
		if err.Error() != "EOF" {
			return nil, nil, fmt.Errorf("read rows: %w", err)
		}
	}
	for i := 0; i < n; i++ {
		cells := make([]string, len(cols))
		for j := range cells {
			cells[j] = ""
		}
		for _, v := range buf[i] {
			if c := v.Column(); c >= 0 && c < len(cells) {
				cells[c] = v.String()
			}
		}
		out = append(out, cells)
	}
	return cols, out, nil
}
