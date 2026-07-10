package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gophermind/internal/safety"

	_ "modernc.org/sqlite"
)

// attachRe matches SQLite statements that can write files at arbitrary paths
// (ATTACH DATABASE, VACUUM ... INTO), which would escape the repo sandbox.
var attachRe = regexp.MustCompile(`(?is)\b(attach\s+database|vacuum\b.*\binto)\b`)

// MigrationDryRun returns a tool that applies a migration's SQL to a THROWAWAY
// copy of a SQLite database and reports the resulting schema diff
// (tables/columns added or removed), so a broken migration is caught before it
// touches the real database. The original db is never modified.
func MigrationDryRun(root string) Tool {
	return Tool{
		Name:        "migration_dryrun",
		Description: "Apply migration SQL to a throwaway copy of a SQLite database and report the schema diff (added/removed tables and columns). The real database is never modified.",
		Schema: object(map[string]any{
			"db":  str("Path to the SQLite database file, relative to the repo root."),
			"sql": str("The migration (up) SQL to apply."),
		}, "db", "sql"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				DB  string `json:"db"`
				SQL string `json:"sql"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.SQL) == "" {
				return "", fmt.Errorf("sql is empty")
			}
			// SQLite's ATTACH DATABASE can create/write files at arbitrary paths,
			// escaping the repo sandbox — refuse it in the dry-run.
			if attachRe.MatchString(a.SQL) {
				return "", fmt.Errorf("ATTACH DATABASE is not allowed in a migration dry-run")
			}
			full, err := safety.SafeJoin(root, a.DB)
			if err != nil {
				return "", err
			}
			orig, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("database %q not found", a.DB)
			}

			// Copy the db to a throwaway file so the original is never touched.
			tmp, err := os.CreateTemp("", "gm-migration-*.db")
			if err != nil {
				return "", err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			if _, err := tmp.Write(orig); err != nil {
				tmp.Close()
				return "", err
			}
			tmp.Close()

			db, err := sql.Open("sqlite", tmpPath)
			if err != nil {
				return "", fmt.Errorf("open copy: %w", err)
			}
			defer db.Close()

			before, err := snapshotSchema(ctx, db)
			if err != nil {
				return "", err
			}
			if _, err := db.ExecContext(ctx, a.SQL); err != nil {
				return "", fmt.Errorf("migration failed on the throwaway copy: %w", err)
			}
			after, err := snapshotSchema(ctx, db)
			if err != nil {
				return "", err
			}
			return diffSchemas(before, after), nil
		},
	}
}

// snapshotSchema returns a map of table name -> sorted column names.
func snapshotSchema(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	tables, err := listTables(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(tables))
	for _, t := range tables {
		cols, err := tableColumns(ctx, db, t)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		sort.Strings(names)
		out[t] = names
	}
	return out, nil
}

// diffSchemas renders the difference between two schema snapshots.
func diffSchemas(before, after map[string][]string) string {
	var b strings.Builder
	changed := false

	// Tables added / column changes.
	names := sortedKeys(after)
	for _, t := range names {
		bc, existed := before[t]
		if !existed {
			fmt.Fprintf(&b, "+ table %s (%s)\n", t, strings.Join(after[t], ", "))
			changed = true
			continue
		}
		for _, col := range after[t] {
			if !contains(bc, col) {
				fmt.Fprintf(&b, "+ column %s.%s\n", t, col)
				changed = true
			}
		}
		for _, col := range bc {
			if !contains(after[t], col) {
				fmt.Fprintf(&b, "- column %s.%s\n", t, col)
				changed = true
			}
		}
	}
	// Tables removed.
	for _, t := range sortedKeys(before) {
		if _, ok := after[t]; !ok {
			fmt.Fprintf(&b, "- table %s\n", t)
			changed = true
		}
	}

	if !changed {
		return "Migration applied cleanly; no schema changes detected.\n"
	}
	return "Migration applied cleanly. Schema diff:\n" + b.String()
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
