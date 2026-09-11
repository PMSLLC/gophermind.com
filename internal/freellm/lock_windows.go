//go:build windows

package freellm

import (
	"fmt"
	"os"
	"time"
)

// staleLockAge is how old a lock file's mtime must be before lockFile treats
// it as abandoned rather than held. Unix flock releases automatically when
// the holding process dies; Windows' O_EXCL has no equivalent, so without
// this takeover a process that dies mid-Add would wedge the odometer for
// every future Add, forever. 60 seconds is comfortably longer than a single
// Add (a read-modify-write of a small JSON file) can legitimately take, so a
// lock file older than that is treated as abandoned rather than contended.
const staleLockAge = 60 * time.Second

// lockFile emulates an exclusive lock with an atomically created lock file,
// retrying briefly. Windows has no flock; O_EXCL creation is the portable
// equivalent for this use. A lock file left behind by a process that died
// while holding it is taken over once its mtime exceeds staleLockAge, so a
// crash cannot wedge the odometer permanently.
func lockFile(path string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return func() {
				_ = f.Close()
				_ = os.Remove(path)
			}, nil
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			// Best effort: if another process wins the removal race and
			// recreates the file first, the next OpenFile attempt above
			// simply fails again and we fall through to the normal
			// retry/backoff below.
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("freellm: odometer lock busy: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
