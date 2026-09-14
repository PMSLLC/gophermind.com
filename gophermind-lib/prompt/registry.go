package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// regNameRe constrains registry prompt names so they stay inside the store dir.
var regNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// RegistryDir is the directory holding named prompt templates.
func RegistryDir(root string) string {
	return filepath.Join(root, ".gophermind", "prompts")
}

// SaveNamed stores content under a name, keeping a timestamped backup of any
// previous version so it can be rolled back.
func SaveNamed(root, name, content string) error {
	return saveNamedIn(RegistryDir(root), name, content)
}

// ListNamed returns the sorted names of stored prompts.
func ListNamed(root string) ([]string, error) { return listNamedIn(RegistryDir(root)) }

// ShowNamed returns a stored prompt's content.
func ShowNamed(root, name string) (string, error) { return showNamedIn(RegistryDir(root), name) }

func namedPath(dir, name string) (string, error) {
	if !regNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid prompt name %q: use only letters, digits, '.', '_' and '-'", name)
	}
	return filepath.Join(dir, name+".md"), nil
}

func saveNamedIn(dir, name, content string) error {
	path, err := namedPath(dir, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Back up the existing version (for rollback) before overwriting.
	if old, err := os.ReadFile(path); err == nil {
		bak := filepath.Join(dir, fmt.Sprintf("%s.%d.bak", name, time.Now().UnixNano()))
		_ = os.WriteFile(bak, old, 0o644)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func listNamedIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}

func showNamedIn(dir, name string) (string, error) {
	path, err := namedPath(dir, name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompt %q not found", name)
	}
	return string(data), nil
}
