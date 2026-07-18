package main

import (
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/session"
)

// sessionModelPath returns the path of the plaintext sidecar file that stores
// id's chosen model, next to its session history (<id>.jsonl -> <id>.model).
func sessionModelPath(id string) (string, error) {
	p, err := session.Path(id)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, ".jsonl") + ".model", nil
}

// writeSessionModel records id's chosen model in its sidecar file. An empty
// model removes the sidecar (best-effort) so the session falls back to the
// server default model.
func writeSessionModel(id, model string) error {
	p, err := sessionModelPath(id)
	if err != nil {
		return err
	}
	if model == "" {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(model), 0o600)
}

// readSessionModel returns id's stored model, or "" if none is set or the
// sidecar can't be read.
func readSessionModel(id string) string {
	p, err := sessionModelPath(id)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
