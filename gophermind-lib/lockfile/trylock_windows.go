//go:build windows

package lockfile

import (
	"os"
	"time"
)

// TryAcquire takes the same lock as Acquire, but never waits: when the lock
// file exists and is not stale it returns ErrBusy immediately. A lock file
// left behind by a process that died holding it is taken over once its mtime
// exceeds staleLockAge, exactly as in Acquire, so a crash cannot wedge the
// lock permanently.
//
// A second TryAcquire on the same path in THIS process also reports ErrBusy,
// because the lock file is already there. A caller that needs re-entrancy
// keeps its own count (see plan.AcquireRun).
func TryAcquire(path string) (func(), error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return func() {
				_ = f.Close()
				_ = os.Remove(path)
			}, nil
		}
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) <= staleLockAge {
			return nil, ErrBusy
		}
		// Abandoned: remove it and make exactly one more attempt. If another
		// process wins that race, the second attempt reports ErrBusy.
		_ = os.Remove(path)
	}
	return nil, ErrBusy
}
