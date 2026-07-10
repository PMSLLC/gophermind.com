package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// symbolNameRe restricts a symbol query to a single identifier so the compiled
// definition pattern can't be turned into arbitrary regex.
var symbolNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// symbolIgnoreDirs are directories never worth scanning for definitions.
var symbolIgnoreDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".idea": true, ".vscode": true, "target": true,
}

// FindSymbol returns a tool that locates where a named symbol is *defined*
// across the repository — functions, types, classes, structs, enums, traits,
// and const/var/assignment-style definitions in Go, Python, JS/TS, Rust, Java,
// and similar languages. It is definition-aware (unlike plain search): call
// sites and other uses are not reported. Pure Go, no external indexer.
func FindSymbol(root string) Tool {
	return Tool{
		Name:        "find_symbol",
		Description: "Find where a function/type/class/struct named X is DEFINED across the repository (not call sites). Returns file:line: matches. Language-aware for Go/Python/JS/TS/Rust/Java.",
		Schema: object(map[string]any{
			"name": str("The symbol identifier to find definitions of (letters, digits, underscore)."),
		}, "name"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			name := strings.TrimSpace(a.Name)
			if !symbolNameRe.MatchString(name) {
				return "", fmt.Errorf("invalid symbol name %q: use a single identifier", a.Name)
			}
			re := definitionPattern(name)

			var matches []string
			const maxMatches = 200
			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // skip unreadable entries
				}
				if d.IsDir() {
					if symbolIgnoreDirs[d.Name()] {
						return filepath.SkipDir
					}
					return nil
				}
				if len(matches) >= maxMatches {
					return filepath.SkipAll
				}
				scanFileForSymbol(root, path, re, &matches, maxMatches)
				return nil
			})
			if walkErr != nil {
				return "", fmt.Errorf("scan repository: %w", walkErr)
			}
			if len(matches) == 0 {
				return fmt.Sprintf("(no definitions of %q found)", name), nil
			}
			return truncate(strings.Join(matches, "\n")), nil
		},
	}
}

// definitionPattern builds a regexp matching common definition forms for name.
func definitionPattern(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	pat := `\b(?:func|type|def|class|struct|enum|trait|interface|fn)\s+(?:\([^)]*\)\s*)?` + q + `\b` +
		`|\b(?:const|let|var|val)\s+` + q + `\b` +
		`|\b` + q + `\s*(?::=|=\s*(?:async\s+)?(?:function\b|\())`
	return regexp.MustCompile(pat)
}

// scanFileForSymbol appends "path:line: text" for each definition line, skipping
// binary and oversized files.
func scanFileForSymbol(root, path string, re *regexp.Regexp, matches *[]string, max int) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 { // skip files > 1 MiB
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data, 0) >= 0 { // skip binary
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			*matches = append(*matches, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			if len(*matches) >= max {
				return
			}
		}
	}
}
