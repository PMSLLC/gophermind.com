//go:build windows

package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An old lock file is still held: TryAcquire must not reclaim by age, since a
// run lock is held for minutes. (Written on a non-Windows machine; compiled
// with GOOS=windows go vet but not run there.)
func TestTryAcquireDoesNotReclaimAnOldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	release, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	old := time.Now().Add(-10 * staleLockAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := TryAcquire(path); !errors.Is(err, ErrBusy) {
		t.Fatalf("TryAcquire = %v, want ErrBusy for an old but live lock", err)
	}
}
