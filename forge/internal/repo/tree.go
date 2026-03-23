package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

func Tree(root string, maxEntries int) (string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if d.IsDir() && ignoredDirs[base] {
			return filepath.SkipDir
		}
		if strings.HasPrefix(rel, ".forge") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entries = append(entries, rel)
		if len(entries) >= maxEntries {
			return fmt.Errorf("max entries reached")
		}
		return nil
	})
	if err != nil && err.Error() != "max entries reached" {
		return "", err
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n"), nil
}
