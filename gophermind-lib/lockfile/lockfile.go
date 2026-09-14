// Package lockfile provides the file-claim pattern proven in the odometer:
// an exclusive advisory lock (flock on unix, an mtime-based takeover on
// Windows) plus an atomic write, so concurrent writers to a shared JSON
// state file cannot lose or tear each other's updates.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path via a temp file in the same directory,
// fsynced and renamed, so a crash mid-write cannot leave a half-written
// file.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lockfile-*")
	if err != nil {
		return fmt.Errorf("lockfile: create temp file: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
