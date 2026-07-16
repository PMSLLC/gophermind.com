// Package prompthistory persists submitted TUI prompts to a JSONL file so
// the TUI can offer recall autosuggest and train an n-gram predictor.
//
// It is self-contained and stdlib-only (does not import internal/config)
// to avoid coupling; it replicates config's path-resolution shape for the
// history file instead. It is unrelated to internal/agent's conversation
// message history (internal/agent/history.go), which tracks LLM turns.
//
// Privacy: prompts are stored in plaintext-as-JSON. Set GOPHERMIND_HISTORY
// to "off", "0", "false", or "no" to disable persistence entirely.
package prompthistory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// maxEntries is the cap on retained history entries; oldest are dropped
// first, in memory and on disk.
const maxEntries = 500

// Store holds the in-memory prompt history and persists it to disk.
type Store struct {
	path    string
	enabled bool
	entries []string
}

// New resolves the history file path, honors the GOPHERMIND_HISTORY
// enable/disable knob, and loads any existing history.
func New() (*Store, error) {
	s := &Store{enabled: historyEnabled()}
	if !s.enabled {
		return s, nil
	}
	path, err := historyFilePath()
	if err != nil {
		return nil, err
	}
	s.path = path
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Load reads history entries from disk into memory, oldest first.
// Blank or malformed lines are skipped defensively rather than failing
// the whole load. When disabled, Load is a no-op.
func (s *Store) Load() error {
	if !s.enabled {
		return nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = nil
			return nil
		}
		return err
	}
	defer f.Close()

	var entries []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var prompt string
		if err := json.Unmarshal([]byte(line), &prompt); err != nil {
			continue // skip malformed line
		}
		entries = append(entries, prompt)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	s.entries = entries
	return nil
}

// Append records a submitted prompt, skipping it if it equals the current
// most-recent entry (consecutive-duplicate collapse, shell-history style).
// The in-memory and on-disk history are capped at maxEntries, dropping the
// oldest entries first. When disabled, Append is a no-op and creates no
// file.
func (s *Store) Append(prompt string) error {
	if !s.enabled {
		return nil
	}
	if len(s.entries) > 0 && s.entries[len(s.entries)-1] == prompt {
		return nil
	}
	s.entries = append(s.entries, prompt)
	if len(s.entries) > maxEntries {
		s.entries = s.entries[len(s.entries)-maxEntries:]
	}
	return s.persist()
}

// All returns the history in chronological order, oldest first. When
// disabled, All returns an empty slice.
func (s *Store) All() []string {
	if !s.enabled {
		return nil
	}
	out := make([]string, len(s.entries))
	copy(out, s.entries)
	return out
}

// persist rewrites the history file from the in-memory (already-capped)
// entries. 500 lines is tiny, so a full rewrite on each Append is simplest
// and correct.
func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, e := range s.entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

// historyEnabled reads the GOPHERMIND_HISTORY knob; default enabled.
func historyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GOPHERMIND_HISTORY")))
	switch v {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

// historyFilePath mirrors config.ConfigFilePath's shape but returns the
// history file path:
//   - GOPHERMIND_CONFIG_DIR set  -> <dir>/history
//   - else os.UserConfigDir() ok -> <dir>/gophermind/history
//   - else                       -> .gophermind/history
func historyFilePath() (string, error) {
	if dir := os.Getenv("GOPHERMIND_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "history"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".gophermind", "history"), nil
	}
	return filepath.Join(dir, "gophermind", "history"), nil
}
