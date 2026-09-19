//go:build !windows

package lockfile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// TryAcquire takes the same exclusive advisory lock as Acquire, but never
// waits: when another process holds the lock it returns ErrBusy immediately.
// It is for work a second process must be told about rather than queued
// behind, such as a long planning run over one tree.
//
// The lock is per open file description, so a second TryAcquire on the same
// path in THIS process also reports ErrBusy. A caller that needs re-entrancy
// keeps its own count (see plan.AcquireRun).
func TryAcquire(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lockfile: open lock: %w", err)
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		f.Close()
		return nil, ErrBusy
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lockfile: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
