package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/safety"
)

// ListFilesGlob returns a glob-aware list_files tool with .gitignore support.
func ListFilesGlob(root string) Tool {
	return Tool{
		Name:        "list_files",
		Description: "List files in the repository (recursively) as relative paths. Supports include/exclude globs, depth limit, and .gitignore.",
		Schema: object(map[string]any{
			"path":      str("Optional subdirectory to list, relative to the repository root. Omit for the whole repo."),
			"include":   str("Include glob pattern (e.g. '*.go'). Omit for no filter."),
			"exclude":   str("Exclude glob pattern (e.g. 'vendor/*'). Omit for no filter."),
			"max_depth": map[string]any{"type": "integer", "description": "Maximum directory depth from the starting path. 0 = unlimited."},
		}, "path"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path     string `json:"path"`
				Include  string `json:"include"`
				Exclude  string `json:"exclude"`
				MaxDepth *int   `json:"max_depth"`
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

			// Load .gitignore patterns.
			gitignorePatterns := loadGitignore(root)

			const maxEntries = 2000
			var entries []string
			err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && ignoredDirs[d.Name()] && path != base {
					return filepath.SkipDir
				}
				// Check .gitignore.
				rel, _ := filepath.Rel(rootAbs, path)
				if isGitignored(rel, gitignorePatterns) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				// Check depth.
				if a.MaxDepth != nil && *a.MaxDepth > 0 {
					depth := strings.Count(rel, string(filepath.Separator))
					if depth > *a.MaxDepth {
						if d.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
				}
				if d.IsDir() {
					return nil
				}
				// Apply include/exclude globs.
				if a.Include != "" && !globMatch(rel, a.Include) {
					return nil
				}
				if a.Exclude != "" && globMatch(rel, a.Exclude) {
					return nil
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

// loadGitignore reads .gitignore files from the root and returns patterns.
func loadGitignore(root string) []string {
	patterns := []string{}
	gitignorePath := filepath.Join(root, ".gitignore")
	f, err := os.Open(gitignorePath)
	if err != nil {
		return patterns
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// isGitignored checks if a path matches any gitignore pattern.
func isGitignored(rel string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(rel, p) {
			return true
		}
	}
	return false
}

// globMatch checks if a path matches a glob pattern.
func globMatch(path, pattern string) bool {
	// Simple glob matching: support *, **, and ?
	// For simplicity, we use filepath.Match which supports *, ?, [seq], [!seq].
	matched, err := filepath.Match(pattern, filepath.Base(path))
	if err != nil {
		return false
	}
	if matched {
		return true
	}
	// Also check against the full relative path.
	matched, err = filepath.Match(pattern, path)
	return err == nil && matched
}

// ListFilesSymlink returns a list_files tool that marks symlinks.
func ListFilesSymlink(root string) Tool {
	return Tool{
		Name:        "list_files",
		Description: "List files in the repository (recursively) as relative paths, skipping ignored directories. Symlinks are marked with [link].",
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
				// Check if it's a symlink.
				isLink := d.Type()&os.ModeSymlink != 0
				if isLink {
					rel = "[link] " + rel
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
