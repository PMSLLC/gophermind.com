package library

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// AliasesFile is the name of the old-id to new-id map inside a library dir.
const AliasesFile = "aliases.json"

// Aliases maps an old run id (gm-YYYY-MM-DD-NNN) to the id the library
// migration gave it.
type Aliases map[string]string

// LoadAliases reads <dir>/aliases.json. A missing file is an empty map.
func LoadAliases(dir string) (Aliases, error) {
	b, err := os.ReadFile(filepath.Join(dir, AliasesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Aliases{}, nil
	}
	if err != nil {
		return nil, err
	}
	a := Aliases{}
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return a, nil
}

// SaveAliases writes <dir>/aliases.json atomically.
func SaveAliases(dir string, a Aliases) error {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, AliasesFile), append(b, '\n'))
}

// Resolve returns the new id for an old one, or id itself when it is unknown.
func (a Aliases) Resolve(id string) string {
	if n, ok := a[id]; ok && n != "" {
		return n
	}
	return id
}
