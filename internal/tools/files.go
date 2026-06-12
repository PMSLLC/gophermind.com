package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/internal/safety"
)

// ignoredDirs are skipped when listing the repository tree.
var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"bin":          true,
	".idea":        true,
	".vscode":      true,
}

// ReadFile returns the read_file tool, which reads a file relative to root.
func ReadFile(root string) Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read a UTF-8 text file and return its full contents. Path is relative to the repository root.",
		Schema:      object(map[string]any{"path": str("File path relative to the repository root.")}, "path"),
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
			b, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}
			return string(b), nil
		},
	}
}

// WriteFile returns the write_file tool, which creates or overwrites a file.
func WriteFile(root string) Tool {
	return Tool{
		Name:        "write_file",
		Description: "Create or overwrite a file with the given content. Creates parent directories as needed. Path is relative to the repository root.",
		Schema: object(map[string]any{
			"path":    str("File path relative to the repository root."),
			"content": str("Full file content to write."),
		}, "path", "content"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return "", fmt.Errorf("mkdir for %s: %w", a.Path, err)
			}
			if err := os.WriteFile(full, []byte(a.Content), 0o644); err != nil {
				return "", fmt.Errorf("write %s: %w", a.Path, err)
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
		},
	}
}

// EditFile returns the edit_file tool, which replaces an exact string that
// must occur exactly once — failing loudly on 0 or >1 matches so the model
// re-anchors rather than making a silent wrong edit.
func EditFile(root string) Tool {
	return Tool{
		Name:        "edit_file",
		Description: "Replace an exact substring in a file. The 'old' string must appear exactly once; the call fails if it is missing or ambiguous. Include enough surrounding context to make 'old' unique.",
		Schema: object(map[string]any{
			"path": str("File path relative to the repository root."),
			"old":  str("Exact existing text to replace. Must match exactly once."),
			"new":  str("Replacement text."),
		}, "path", "old", "new"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
				Old  string `json:"old"`
				New  string `json:"new"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			full, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			b, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", a.Path, err)
			}
			content := string(b)
			n := strings.Count(content, a.Old)
			switch {
			case n == 0:
				return "", fmt.Errorf("'old' string not found in %s", a.Path)
			case n > 1:
				return "", fmt.Errorf("'old' string matches %d times in %s; add more context to make it unique", n, a.Path)
			}
			updated := strings.Replace(content, a.Old, a.New, 1)
			if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
				return "", fmt.Errorf("write %s: %w", a.Path, err)
			}
			return fmt.Sprintf("edited %s", a.Path), nil
		},
	}
}

// ListFiles returns the list_files tool, which produces a sorted relative-path
// listing of the repository, skipping ignored directories.
func ListFiles(root string) Tool {
	return Tool{
		Name:        "list_files",
		Description: "List files in the repository (recursively) as relative paths, skipping common ignored directories like .git and node_modules. Optionally restrict to a subdirectory.",
		Schema:      object(map[string]any{"path": str("Optional subdirectory to list, relative to the repository root. Omit for the whole repo.")}),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			base, err := safety.SafeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			rootAbs, _ := filepath.Abs(root)

			const maxEntries = 2000
			var entries []string
			err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && ignoredDirs[d.Name()] && path != base {
					return filepath.SkipDir
				}
				if d.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(rootAbs, path)
				if err != nil {
					return err
				}
				entries = append(entries, rel)
				if len(entries) >= maxEntries {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			sort.Strings(entries)
			if len(entries) == 0 {
				return "(no files)", nil
			}
			out := strings.Join(entries, "\n")
			if len(entries) >= maxEntries {
				out += fmt.Sprintf("\n... [truncated at %d files]", maxEntries)
			}
			return out, nil
		},
	}
}
