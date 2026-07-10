package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gophermind/internal/safety"

	_ "modernc.org/sqlite"
)

// DBExplain returns a read-only tool that runs EXPLAIN QUERY PLAN for a SQLite
// query and surfaces the plan, flagging full-table scans (SCAN without an
// index) so the model can suggest indexes. The db is opened read-only and its
// path is contained to the repository root; only read-only queries are planned.
func DBExplain(root string) Tool {
	return Tool{
		Name:        "db_explain",
		Description: "Run EXPLAIN QUERY PLAN for a read-only SQLite query and report the plan, flagging full-table scans that may need an index.",
		Schema: object(map[string]any{
			"db":     str("Path to the SQLite database file, relative to the repo root."),
			"query":  str("The SQL query to plan (SELECT/WITH only)."),
			"params": map[string]any{"type": "array", "description": "Positional parameters bound to ? placeholders.", "items": map[string]any{}},
		}, "db", "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				DB     string `json:"db"`
				Query  string `json:"query"`
				Params []any  `json:"params"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if verb := firstVerb(a.Query); verb != "SELECT" && verb != "WITH" {
				return "", fmt.Errorf("only SELECT/WITH queries can be planned (got %q)", verb)
			}
			full, err := safety.SafeJoin(root, a.DB)
			if err != nil {
				return "", err
			}
			if _, err := os.Stat(full); err != nil {
				return "", fmt.Errorf("database %q not found", a.DB)
			}

			db, err := sql.Open("sqlite", "file:"+full+"?mode=ro")
			if err != nil {
				return "", fmt.Errorf("open db: %w", err)
			}
			defer db.Close()

			rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+a.Query, a.Params...)
			if err != nil {
				return "", fmt.Errorf("explain: %w", err)
			}
			defer rows.Close()

			var b strings.Builder
			var warnings []string
			for rows.Next() {
				var (
					id, parent, notused int
					detail              string
				)
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					return "", err
				}
				b.WriteString(detail + "\n")
				if isFullScan(detail) {
					warnings = append(warnings, detail)
				}
			}
			if err := rows.Err(); err != nil {
				return "", err
			}
			if b.Len() == 0 {
				b.WriteString("(no plan)\n")
			}
			for _, w := range warnings {
				fmt.Fprintf(&b, "WARNING: full-table scan — %s (consider an index)\n", w)
			}
			return b.String(), nil
		},
	}
}

// isFullScan reports whether an EXPLAIN QUERY PLAN detail line describes a
// full-table scan (a SCAN that does not use an index).
func isFullScan(detail string) bool {
	d := strings.ToUpper(detail)
	if !strings.Contains(d, "SCAN") {
		return false
	}
	return !strings.Contains(d, "USING INDEX") && !strings.Contains(d, "USING COVERING INDEX")
}
