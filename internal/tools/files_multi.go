package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/safety"
)

// EditFileMulti returns an edit tool that supports multi-occurrence replacement.
func EditFileMulti(root string) Tool {
	return Tool{
		Name:        "edit_file",
		Description: "Replace an exact substring in a file. The 'old' string must appear exactly once by default; set replace_all=true to replace all occurrences.",
		Schema: object(map[string]any{
			"path":        str("File path relative to the repository root."),
			"old":         str("Exact existing text to replace. Must match exactly once (or use replace_all)."),
			"new":         str("Replacement text."),
			"replace_all": map[string]any{"type": "boolean", "description": "When true, replace all occurrences instead of requiring exactly one match."},
		}, "path", "old", "new"),
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path       string `json:"path"`
				Old        string `json:"old"`
				New        string `json:"new"`
				ReplaceAll *bool  `json:"replace_all"`
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
			replaceAll := a.ReplaceAll != nil && *a.ReplaceAll
			if !replaceAll {
				switch {
				case n == 0:
					return "", fmt.Errorf("'old' string not found in %s", a.Path)
				case n > 1:
					return "", fmt.Errorf("'old' string matches %d times in %s; set replace_all=true for bulk replacement, or add more context to make it unique", n, a.Path)
				}
				updated := strings.Replace(content, a.Old, a.New, 1)
				if err := atomicWrite(full, []byte(updated)); err != nil {
					return "", fmt.Errorf("write %s: %w", a.Path, err)
				}
				return fmt.Sprintf("edited %s (1 occurrence)", a.Path) + secretWarning(updated), nil
			}
			// replace_all mode.
			if n == 0 {
				return "", fmt.Errorf("'old' string not found in %s", a.Path)
			}
			updated := strings.ReplaceAll(content, a.Old, a.New)
			if err := atomicWrite(full, []byte(updated)); err != nil {
				return "", fmt.Errorf("write %s: %w", a.Path, err)
			}
			return fmt.Sprintf("edited %s (%d occurrences replaced)", a.Path, n) + secretWarning(updated), nil
		},
	}
}

// atomicWrite writes data to a temp file in the same directory and renames it
// into place, keeping a .bak backup of the original.
func atomicWrite(dst string, data []byte) error {
	// Create backup of original if it exists.
	if _, err := os.Stat(dst); err == nil {
		bak := dst + ".bak"
		if err := copyFile(dst, bak); err != nil {
			// Non-fatal: continue with the write.
		}
		_ = bak
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dst)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
