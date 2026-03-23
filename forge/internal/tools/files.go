package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadFile(root, relPath string) (string, error) {
	full, err := safeJoin(root, relPath)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", relPath, err)
	}
	return string(b), nil
}

func WriteFile(root, relPath, content string) error {
	full, err := safeJoin(root, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file %s: %w", relPath, err)
	}
	return nil
}

func ReplaceInFile(root, relPath, find, replace string) error {
	content, err := ReadFile(root, relPath)
	if err != nil {
		return err
	}
	if !strings.Contains(content, find) {
		return fmt.Errorf("target block not found in %s", relPath)
	}
	updated := strings.Replace(content, find, replace, 1)
	return WriteFile(root, relPath, updated)
}

func safeJoin(root, rel string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs root: %w", err)
	}
	full := filepath.Join(cleanRoot, rel)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}
	if abs != cleanRoot && !strings.HasPrefix(abs, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repo root: %s", rel)
	}
	return abs, nil
}
