package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/lockfile"
)

// OverviewCapBytes is the soft size limit of the running overview (about
// 1,500 tokens). It is prose for the model, not an authority for the tree.
const OverviewCapBytes = 6000

const (
	overviewFile = "overview.md"
	truncMarker  = "\n[overview truncated]"
)

// ReadOverview returns the overview stored in dir, or "" if there is none.
func ReadOverview(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, overviewFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// WriteOverview atomically replaces the overview stored in dir.
func WriteOverview(dir, text string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := strings.TrimRight(text, "\n") + "\n"
	return lockfile.WriteAtomic(filepath.Join(dir, overviewFile), []byte(body), 0o644)
}

// FitOverview returns text unchanged when it fits in cap bytes. Otherwise it
// cuts at the last paragraph or line end that fits and appends a marker, so the
// result is at most cap bytes and never ends mid-word or mid-rune.
func FitOverview(text string, cap int) string {
	if len(text) <= cap {
		return text
	}
	limit := cap - len(truncMarker)
	if limit < 1 {
		limit = 1
	}
	cut := cutPoint(text, limit)
	return strings.TrimRight(text[:cut], "\n ") + truncMarker
}
