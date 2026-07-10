package session

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GCProtecting is like GC but never deletes a session carrying any of the given
// protected tags, so "important" sessions are retained while "scratch" ones
// expire — per-tag retention on top of the age cutoff.
func GCProtecting(maxAge time.Duration, protectedTags []string) ([]string, error) {
	if maxAge <= 0 {
		return nil, nil
	}
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return gcProtectingIn(dir, maxAge, protectedTags, time.Now())
}

func gcProtectingIn(dir string, maxAge time.Duration, protectedTags []string, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	protected := map[string]bool{}
	for _, t := range protectedTags {
		protected[t] = true
	}
	cutoff := now.Add(-maxAge)

	var removed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if hasProtectedTag(tagsIn(dir, id), protected) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return removed, err
		}
		removed = append(removed, id)
	}
	return removed, nil
}

func hasProtectedTag(tags []string, protected map[string]bool) bool {
	for _, t := range tags {
		if protected[t] {
			return true
		}
	}
	return false
}
