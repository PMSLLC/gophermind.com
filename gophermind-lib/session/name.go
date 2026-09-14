package session

import (
	"os"
	"path/filepath"
	"strings"
)

// A session's display title is normally derived from its first user message.
// A custom name, when set, is stored in a sidecar "<id>.name" file next to the
// session's ".jsonl" and overlaid onto Info.Title by List. The transcript file
// is never touched, so existing sessions and every transcript reader are
// unaffected.

// maxNameLen caps a custom name so it cannot break list rendering.
const maxNameLen = 120

// namePath returns the sidecar path for id, validating the id so the file can
// never escape the sessions directory.
func namePath(id string) (string, error) {
	if err := validID(id); err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".name"), nil
}

// SetName stores a custom display name for a session. A blank name (after
// trimming) clears it, reverting the session to its derived title.
func SetName(id, name string) error {
	name = sanitizeName(name)
	if name == "" {
		return ClearName(id)
	}
	path, err := namePath(id)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o600)
}

// Name returns the custom name for a session, or "" if none is set.
func Name(id string) string {
	path, err := namePath(id)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sanitizeName(string(data))
}

// ClearName removes any custom name. A missing sidecar is not an error.
func ClearName(id string) error {
	path, err := namePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// sanitizeName strips control characters (so a name cannot inject newlines into
// the sidecar or corrupt the list) and caps the length.
func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxNameLen {
		s = strings.TrimSpace(string(r[:maxNameLen]))
	}
	return s
}

// nameInDir reads a sidecar from a specific directory, used by listDir which
// already holds the sessions dir. It mirrors Name but avoids re-resolving Dir.
func nameInDir(dir, id string) string {
	if validID(id) != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".name"))
	if err != nil {
		return ""
	}
	return sanitizeName(string(data))
}
