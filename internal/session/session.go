// Package session persists an agent conversation to disk keyed by id, so a
// print-mode session can be pre-assigned an id and resumed across processes
// (OpenCoven's preassigned_session_id / --resume). Histories are stored as the
// JSONL the agent already reads and writes.
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gophermind/internal/agent"
	"gophermind/internal/config"
)

// idRe constrains ids to a safe charset so an id can never become a path
// component that escapes the sessions directory.
var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validID(id string) error {
	if id == "" {
		return fmt.Errorf("session id is empty")
	}
	if id == "." || id == ".." || !idRe.MatchString(id) {
		return fmt.Errorf("invalid session id %q: use only letters, digits, '.', '_' and '-'", id)
	}
	return nil
}

// Dir is the directory holding session histories (a "sessions" subdirectory of
// the global config directory; honors GOPHERMIND_CONFIG_DIR).
func Dir() (string, error) {
	p, err := config.ConfigFilePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "sessions"), nil
}

// Path returns the file path for a session id.
func Path(id string) (string, error) {
	if err := validID(id); err != nil {
		return "", err
	}
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, id+".jsonl"), nil
}

// Exists reports whether a session with the given id has been saved.
func Exists(id string) bool {
	p, err := Path(id)
	if err != nil {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// Save writes the agent's conversation to the session file (0600 file, 0700 dir).
func Save(id string, a *agent.Agent) error {
	p, err := Path(id)
	if err != nil {
		return err
	}
	return a.WriteTranscript(p)
}

// Load restores a saved session's conversation into the agent.
func Load(id string, a *agent.Agent) error {
	p, err := Path(id)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return a.LoadHistory(f)
}
