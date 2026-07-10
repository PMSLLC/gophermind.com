package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// aliasFile is the name of the JSON map (alias -> session id) in the store dir.
const aliasFile = "aliases.json"

// SetAlias maps a human-friendly name to a session id in the store, so
// `--resume my-refactor` can stand in for a uuid.
func SetAlias(name, id string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return setAliasIn(dir, name, id)
}

func setAliasIn(dir, name, id string) error {
	if name == "" {
		return fmt.Errorf("alias name is empty")
	}
	if err := validID(id); err != nil {
		return fmt.Errorf("alias target: %w", err)
	}
	m := loadAliases(dir)
	m[name] = id
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, aliasFile), data, 0o600)
}

// Resolve maps an alias to its session id, or returns the input unchanged when
// it isn't a known alias (so a real id passes straight through).
func Resolve(nameOrID string) string {
	dir, err := Dir()
	if err != nil {
		return nameOrID
	}
	return resolveIn(dir, nameOrID)
}

func resolveIn(dir, nameOrID string) string {
	if id, ok := loadAliases(dir)[nameOrID]; ok {
		return id
	}
	return nameOrID
}

// loadAliases reads the alias map, returning an empty map when absent/corrupt.
func loadAliases(dir string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, aliasFile))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}
