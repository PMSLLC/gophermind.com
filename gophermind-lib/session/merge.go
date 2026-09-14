package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Merge combines two sessions (typically branched from a common point) into a
// new session: their shared line prefix, followed by each session's divergent
// tail in order (a's, then b's). This reconciles parallel explorations without
// losing either side's history.
func Merge(aID, bID, destID string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return mergeIn(dir, aID, bID, destID)
}

func mergeIn(dir, aID, bID, destID string) error {
	for _, id := range []string{aID, bID, destID} {
		if err := validID(id); err != nil {
			return err
		}
	}
	aLines, err := readSessionLines(dir, aID)
	if err != nil {
		return err
	}
	bLines, err := readSessionLines(dir, bID)
	if err != nil {
		return err
	}

	// Longest common line prefix, then both divergent tails.
	n := commonPrefixLen(aLines, bLines)
	merged := make([]string, 0, len(aLines)+len(bLines)-n)
	merged = append(merged, aLines[:n]...)
	merged = append(merged, aLines[n:]...)
	merged = append(merged, bLines[n:]...)

	out := strings.Join(merged, "\n") + "\n"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, destID+".jsonl"), []byte(out), 0o600)
}

// readSessionLines reads a session file into its non-terminal-newline lines.
func readSessionLines(dir, id string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// commonPrefixLen returns the number of leading lines a and b share.
func commonPrefixLen(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}
