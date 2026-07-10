package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"gophermind/internal/safety"

	_ "modernc.org/sqlite"
)

// seedDefaultRows is the row count used when none is requested.
const seedDefaultRows = 5

// seedMaxRows caps how many rows seed_data will generate in one call.
const seedMaxRows = 1000

// SeedData returns a read-only tool that generates plausible INSERT statements
// for a table based on its column types, for quickly populating test data. It
// reads the schema (via PRAGMA table_info) but never writes to the database —
// the statements are returned as text for the caller to review and run.
func SeedData(root string) Tool {
	return Tool{
		Name:        "seed_data",
		Description: "Generate plausible INSERT statements for a SQLite table from its schema (for test seed data). Returns SQL text; does not modify the database. Integer primary keys are omitted (autoincrement).",
		Schema: object(map[string]any{
			"db":    str("Path to the SQLite database file, relative to the repo root."),
			"table": str("Table to generate rows for."),
			"rows":  map[string]any{"type": "integer", "description": "How many rows to generate (default 5)."},
		}, "db", "table"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				DB    string `json:"db"`
				Table string `json:"table"`
				Rows  int    `json:"rows"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			n := a.Rows
			if n <= 0 {
				n = seedDefaultRows
			}
			if n > seedMaxRows {
				n = seedMaxRows
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

			cols, err := tableColumns(ctx, db, a.Table)
			if err != nil {
				return "", err
			}
			if len(cols) == 0 {
				return "", fmt.Errorf("table %q not found or has no columns", a.Table)
			}

			// Columns to populate: everything except an integer primary key, which
			// SQLite autoincrements.
			var use []schemaColumn
			for _, c := range cols {
				if c.PrimaryKey && isIntegerType(c.Type) {
					continue
				}
				use = append(use, c)
			}
			if len(use) == 0 {
				return "", fmt.Errorf("table %q has no insertable columns", a.Table)
			}

			names := make([]string, len(use))
			for i, c := range use {
				names[i] = quoteIdent(c.Name)
			}

			var b strings.Builder
			for i := 0; i < n; i++ {
				vals := make([]string, len(use))
				for j, c := range use {
					vals[j] = seedValue(c, i)
				}
				fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES (%s);\n",
					quoteIdent(a.Table), strings.Join(names, ", "), strings.Join(vals, ", "))
			}
			return b.String(), nil
		},
	}
}

// isIntegerType reports whether a SQLite declared type is an integer affinity.
func isIntegerType(t string) bool {
	return strings.Contains(strings.ToUpper(t), "INT")
}

// seedValue produces a SQL literal for a column based on its declared type and
// the row index (so values are varied but deterministic-ish).
func seedValue(c schemaColumn, row int) string {
	t := strings.ToUpper(c.Type)
	switch {
	case strings.Contains(t, "INT"):
		return fmt.Sprintf("%d", row+1)
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return fmt.Sprintf("%.2f", rand.Float64()*100)
	case strings.Contains(t, "BOOL"):
		return fmt.Sprintf("%d", row%2)
	case strings.Contains(t, "DATE"), strings.Contains(t, "TIME"):
		return "'2024-01-" + fmt.Sprintf("%02d", (row%28)+1) + "'"
	default: // TEXT / CHAR / CLOB / unknown
		return "'" + c.Name + "_" + fmt.Sprintf("%d", row+1) + "'"
	}
}
