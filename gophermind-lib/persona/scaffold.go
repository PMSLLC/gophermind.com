package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nameRe constrains custom persona names so they can't escape the personas dir.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// personasDir is the repo-local directory holding custom persona files.
func personasDir(root string) string {
	return filepath.Join(root, ".gophermind", "personas")
}

// customPath returns the file path for a custom persona, validating the name.
func customPath(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || !nameRe.MatchString(name) {
		return "", fmt.Errorf("invalid persona name %q: use only letters, digits, '.', '_' and '-'", name)
	}
	return filepath.Join(personasDir(root), name+".md"), nil
}

// scaffoldTemplate is the starter content for a new custom persona.
const scaffoldTemplate = `You are acting as the "%s" persona.

Describe the behavior you want here: priorities, tone, what to emphasize or
avoid. This whole file is appended to the system prompt when you run with
--persona %s.
`

// Scaffold writes a starter persona template to .gophermind/personas/<name>.md
// and returns its path. It refuses to overwrite an existing persona.
func Scaffold(root, name string) (string, error) {
	path, err := customPath(root, name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("persona %q already exists at %s", name, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf(scaffoldTemplate, name, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// loadCustom reads a custom persona file's content (trimmed) if present.
func loadCustom(root, name string) (string, bool) {
	path, err := customPath(root, name)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", false
	}
	return text, true
}

// Resolve returns the instruction block for a persona name, checking the
// built-in presets first, then repo-local custom personas.
func Resolve(root, name string) (string, bool) {
	if text, ok := Preset(name); ok {
		return text, true
	}
	return loadCustom(root, name)
}
