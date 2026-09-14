// Package watch provides simple mtime-based change detection over a path (file
// or directory tree), so a run can be triggered when watched files change —
// proactive automation without an external file-watcher dependency.
package watch

import (
	"os"
	"path/filepath"
	"time"
)

// LatestModTime returns the most recent modification time under path (walking a
// directory, or the file's own mtime). A missing path returns the zero time.
func LatestModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	if !info.IsDir() {
		return info.ModTime(), nil
	}
	var latest time.Time
	err = filepath.Walk(path, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
		return nil
	})
	return latest, err
}

// Changed reports whether path has been modified since the given time, returning
// the new latest mod time to carry forward.
func Changed(path string, since time.Time) (bool, time.Time, error) {
	latest, err := LatestModTime(path)
	if err != nil {
		return false, since, err
	}
	return latest.After(since), latest, nil
}
