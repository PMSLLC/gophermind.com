package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Branch forks a saved session into a new id, keeping the first atTurn messages
// (a non-positive or too-large atTurn copies the whole history). The source is
// left untouched, so alternatives can be explored without losing state.
func Branch(srcID, newID string, atTurn int) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return branchIn(dir, srcID, newID, atTurn)
}

func branchIn(dir, srcID, newID string, atTurn int) error {
	if err := validID(srcID); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if err := validID(newID); err != nil {
		return fmt.Errorf("new: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, srcID+".jsonl"))
	if err != nil {
		return fmt.Errorf("session %q not found", srcID)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if atTurn > 0 && atTurn < len(lines) {
		lines = lines[:atTurn]
	}
	out := strings.Join(lines, "\n") + "\n"

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, newID+".jsonl"), []byte(out), 0o600)
}
