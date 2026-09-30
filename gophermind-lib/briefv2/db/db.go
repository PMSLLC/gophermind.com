// Package db opens the SQLite file shared by the blackboard (rows and their
// change events) and the model-call ledger (calls). One file per harness,
// write-ahead logging, safe for several processes.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"gophermind/gophermind-lib/config"
)

// TimeFormat is fixed width and always UTC, so timestamps compare correctly as
// text (RFC3339Nano trims trailing zeros and would not).
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// TS formats t for storage.
func TS(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTS is the inverse of TS; the empty string is the zero time.
func ParseTS(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(TimeFormat, s)
}

// DefaultPath is ~/.gophermind/blackboard.db (or under GOPHERMIND_CONFIG_DIR).
func DefaultPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "blackboard.db"), nil
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	d.SetMaxOpenConns(8)
	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("db: %w", err)
	}
	return d, nil
}

var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS rows (
		run_id TEXT NOT NULL, node_id TEXT NOT NULL,
		status TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, wave INTEGER NOT NULL DEFAULT 0,
		claim_worker TEXT NOT NULL DEFAULT '', claim_at TEXT NOT NULL DEFAULT '', heartbeat_at TEXT NOT NULL DEFAULT '',
		attempts TEXT NOT NULL DEFAULT '[]', result TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, node_id))`,
	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, node_id TEXT NOT NULL,
		kind TEXT NOT NULL, at TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS events_run ON events (run_id, id)`,
	`CREATE TABLE IF NOT EXISTS calls (
		id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, at TEXT NOT NULL,
		stage TEXT NOT NULL, task_type TEXT NOT NULL DEFAULT '', node_class TEXT NOT NULL DEFAULT '',
		node_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,
		scope TEXT NOT NULL, tier TEXT NOT NULL, chain_pos INTEGER NOT NULL DEFAULT 0,
		provider TEXT NOT NULL, model_requested TEXT NOT NULL, model_served TEXT NOT NULL DEFAULT '',
		prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
		prompt_bytes INTEGER NOT NULL DEFAULT 0, prompt_sha256 TEXT NOT NULL DEFAULT '',
		response_bytes INTEGER NOT NULL DEFAULT 0, response_sha256 TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0,
		outcome TEXT NOT NULL, error_kind TEXT NOT NULL DEFAULT '', retry_after_s INTEGER NOT NULL DEFAULT 0)`,
	`CREATE INDEX IF NOT EXISTS calls_run ON calls (run_id, at)`,
	`CREATE INDEX IF NOT EXISTS calls_node ON calls (run_id, node_id)`,
}

func migrate(d *sql.DB) error {
	var v int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= 1 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range schemaV1 {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = d.Exec(`PRAGMA user_version = 1`)
	return err
}
