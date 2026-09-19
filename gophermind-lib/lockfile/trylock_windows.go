//go:build windows

package lockfile

import (
	"errors"
	"fmt"
	"os"
)

// TryAcquire takes an exclusive lock as an atomically created lock file, and
// never waits: when the file already exists it returns ErrBusy immediately.
//
// Unlike Acquire it NEVER reclaims a lock file by age. Acquire's staleLockAge
// is sized for a tiny locked write; TryAcquire guards work that runs for
// minutes (model calls), and reclaiming a live run's lock after 60 seconds
// would let a second run in and have the first run's release delete the
// second's file. The cost is that Windows has no automatic release on death:
// a run that crashed leaves the lock file behind, and the user must delete it
// by hand before the next run can start.
//
// A second TryAcquire on the same path in THIS process also reports ErrBusy,
// because the lock file is already there. A caller that needs re-entrancy
// keeps its own count (see plan.AcquireRun).
//
// ErrBusy is returned only when the file really exists; any other failure to
// create it is wrapped and returned distinctly.
func TryAcquire(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		return func() {
			_ = f.Close()
			_ = os.Remove(path)
		}, nil
	}
	if errors.Is(err, os.ErrExist) {
		return nil, ErrBusy
	}
	return nil, fmt.Errorf("lockfile: open lock: %w", err)
}
