package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gophermind/internal/safety"
)

// slugRe collapses non-alphanumeric runs into single underscores for a
// filesystem-safe migration slug.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// CreateMigration returns a gated tool that scaffolds a timestamped SQL
// migration file (with up/down sections) under a migrations directory, so
// schema changes follow a consistent, ordered convention.
func CreateMigration(root string) Tool {
	return Tool{
		Name:        "create_migration",
		Description: "Scaffold a new timestamped SQL migration file with up/down sections under a migrations directory. Returns the created path.",
		Schema: object(map[string]any{
			"name": str("Human name for the migration, e.g. 'add users table'."),
			"dir":  str("Migrations directory relative to the repo root (default: migrations)."),
		}, "name"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Name string `json:"name"`
				Dir  string `json:"dir"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			slug := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(a.Name)), "_")
			slug = strings.Trim(slug, "_")
			if slug == "" {
				return "", fmt.Errorf("migration name is empty")
			}
			relDir := a.Dir
			if relDir == "" {
				relDir = "migrations"
			}
			fullDir, err := safety.SafeJoin(root, relDir)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(fullDir, 0o755); err != nil {
				return "", fmt.Errorf("create migrations dir: %w", err)
			}

			fname := fmt.Sprintf("%s_%s.sql", time.Now().UTC().Format("20060102150405"), slug)
			full := filepath.Join(fullDir, fname)
			body := fmt.Sprintf("-- Migration: %s\n-- Created: %s\n\n-- +migrate Up\n\n\n-- +migrate Down\n\n",
				a.Name, time.Now().UTC().Format(time.RFC3339))
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				return "", fmt.Errorf("write migration: %w", err)
			}
			return fmt.Sprintf("created migration %s", filepath.Join(relDir, fname)), nil
		},
	}
}
