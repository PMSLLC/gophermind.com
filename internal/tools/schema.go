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

// schemaColumn describes one column of a table.
type schemaColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"primary_key"`
	NotNull    bool   `json:"not_null"`
}

// schemaFK describes one foreign-key relationship.
type schemaFK struct {
	Column     string `json:"column"`
	References string `json:"references"`
}

// schemaTable groups a table's columns and foreign keys.
type schemaTable struct {
	Name        string         `json:"name"`
	Columns     []schemaColumn `json:"columns"`
	ForeignKeys []schemaFK     `json:"foreign_keys"`
}

// DBSchema returns a read-only tool that reports a SQLite database's structure
// (tables, columns with types/PK/NOT NULL, and foreign keys) as structured JSON,
// so the model can orient before querying. The db is opened read-only and its
// path is contained to the repository root.
func DBSchema(root string) Tool {
	return Tool{
		Name:        "db_schema",
		Description: "Report a SQLite database's structure (tables, columns with types/PK/NOT NULL, and foreign keys) as JSON.",
		Schema: object(map[string]any{
			"db": str("Path to the SQLite database file, relative to the repo root."),
		}, "db"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				DB string `json:"db"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
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

			tables, err := listTables(ctx, db)
			if err != nil {
				return "", err
			}
			out := struct {
				Tables []schemaTable `json:"tables"`
			}{Tables: make([]schemaTable, 0, len(tables))}
			for _, name := range tables {
				cols, err := tableColumns(ctx, db, name)
				if err != nil {
					return "", err
				}
				fks, err := tableForeignKeys(ctx, db, name)
				if err != nil {
					return "", err
				}
				out.Tables = append(out.Tables, schemaTable{Name: name, Columns: cols, ForeignKeys: fks})
			}

			b, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
}

// listTables returns user table names (excluding SQLite internal tables), sorted.
func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// tableColumns returns the columns of a table via PRAGMA table_info.
func tableColumns(ctx context.Context, db *sql.DB, table string) ([]schemaColumn, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(table)))
	if err != nil {
		return nil, fmt.Errorf("table_info(%s): %w", table, err)
	}
	defer rows.Close()
	var cols []schemaColumn
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, schemaColumn{
			Name:       name,
			Type:       ctype,
			PrimaryKey: pk > 0,
			NotNull:    notnull != 0,
		})
	}
	return cols, rows.Err()
}

// tableForeignKeys returns the foreign keys of a table via PRAGMA foreign_key_list.
func tableForeignKeys(ctx context.Context, db *sql.DB, table string) ([]schemaFK, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoteIdent(table)))
	if err != nil {
		return nil, fmt.Errorf("foreign_key_list(%s): %w", table, err)
	}
	defer rows.Close()
	var fks []schemaFK
	for rows.Next() {
		// Columns: id, seq, table, from, to, on_update, on_delete, match
		var (
			id, seq                      int
			refTable, from, to           string
			onUpdate, onDelete, matchStr string
		)
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &matchStr); err != nil {
			return nil, err
		}
		fks = append(fks, schemaFK{
			Column:     from,
			References: fmt.Sprintf("%s(%s)", refTable, to),
		})
	}
	return fks, rows.Err()
}

// quoteIdent double-quotes a SQL identifier for safe interpolation into PRAGMA
// statements (which don't accept bound parameters).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
