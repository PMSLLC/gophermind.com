package serve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/session"
)

// sessionRootPath is the sidecar holding a session's working directory, next
// to its history (<id>.jsonl -> <id>.root), matching how the pinned model is
// stored.
func sessionRootPath(id string) (string, error) {
	p, err := session.Path(id)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, ".jsonl") + ".root", nil
}

// WriteSessionRoot records the directory a session's tools work in.
//
// The root is what every tool's containment check is computed against, so it
// is validated here rather than discovered on the first tool call: an
// absolute path that is a real directory, or an error. A relative path would
// resolve against the server's working directory, which is not something the
// person picking a folder is thinking about.
func WriteSessionRoot(id, root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("session root: needs a path")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("session root: %q is not an absolute path", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("session root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("session root: %q is not a directory", root)
	}
	p, err := sessionRootPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(root), 0o600)
}

// ReadSessionRoot returns a session's working directory, or "" when it has
// none and should use the server's own root. Empty is the state of every
// session created before this existed, and of every session the user never
// pointed anywhere.
func ReadSessionRoot(id string) string {
	p, err := sessionRootPath(id)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ClearSessionRoot reverts a session to the server's own root.
func ClearSessionRoot(id string) error {
	p, err := sessionRootPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
