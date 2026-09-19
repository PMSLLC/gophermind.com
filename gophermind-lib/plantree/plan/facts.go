package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// FactsCapBytes bounds the project facts shown in a pass-2 prompt.
const FactsCapBytes = 1200

const factsFile = "facts.md"

// ReadFacts returns the project facts stored beside the tree (the language,
// how to build and test, where things live), or "" if there are none. The text
// is cut to FactsCapBytes at a rune boundary, so a large file cannot grow a
// prompt.
func ReadFacts(repo *plantree.Repo) (string, error) {
	b, err := os.ReadFile(filepath.Join(repo.Dir(), factsFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cutBytes(strings.TrimSpace(string(b)), FactsCapBytes), nil
}

// WriteFacts atomically replaces the project facts stored beside the tree.
func WriteFacts(repo *plantree.Repo, text string) error {
	if err := os.MkdirAll(repo.Dir(), 0o755); err != nil {
		return err
	}
	body := strings.TrimSpace(text) + "\n"
	return lockfile.WriteAtomic(filepath.Join(repo.Dir(), factsFile), []byte(body), 0o644)
}
