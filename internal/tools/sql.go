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

// sqlMaxRows caps how many result rows the tool returns.
const sqlMaxRows = 100

// readOnlyVerbs are the statement kinds allowed by the read-only SQL tool.
var readOnlyVerbs = map[string]bool{"SELECT": true, "WITH": true, "PRAGMA": true, "EXPLAIN": true}

// SQLQuery returns a read-only, parameterized SQLite query tool. The database is
// opened read-only (mode=ro) AND the statement must begin with a read-only verb
// (SELECT/WITH/PRAGMA/EXPLAIN), so it can reason over real data without any risk
// of mutation. The db path is contained to the repository root.
func SQLQuery(root string) Tool {
	return Tool{
		Name:        "sql_query",
		Description: "Run a READ-ONLY, parameterized SQL query against a SQLite database file and return the rows. Only SELECT/WITH/PRAGMA/EXPLAIN are allowed.",
		Schema: object(map[string]any{
			"db":     str("Path to the SQLite database file, relative to the repo root."),
			"query":  str("The SQL query (SELECT/WITH/PRAGMA/EXPLAIN only)."),
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
			if verb := firstVerb(a.Query); !readOnlyVerbs[verb] {
				return "", fmt.Errorf("only read-only queries are allowed (got %q)", verb)
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

			rows, err := db.QueryContext(ctx, a.Query, a.Params...)
			if err != nil {
				return "", fmt.Errorf("query: %w", err)
			}
			defer rows.Close()
			return formatRows(rows)
		},
	}
}

// firstVerb returns the upper-cased first SQL keyword of a query (ignoring
// leading whitespace and a leading comment line).
func firstVerb(q string) string {
	q = strings.TrimSpace(q)
	// Skip a leading -- comment line.
	for strings.HasPrefix(q, "--") {
		if i := strings.IndexByte(q, '\n'); i >= 0 {
			q = strings.TrimSpace(q[i+1:])
		} else {
			return ""
		}
	}
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

// formatRows renders result rows as a compact header + pipe-separated table,
// capped at sqlMaxRows.
func formatRows(rows *sql.Rows) (string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(strings.Join(cols, " | ") + "\n")

	n := 0
	for rows.Next() {
		if n >= sqlMaxRows {
			fmt.Fprintf(&b, "… [capped at %d rows]\n", sqlMaxRows)
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			cells[i] = cellString(v)
		}
		b.WriteString(strings.Join(cells, " | ") + "\n")
		n++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		b.WriteString("(no rows)\n")
	}
	return truncate(b.String()), nil
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
