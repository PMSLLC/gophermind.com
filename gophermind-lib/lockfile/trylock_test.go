package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireRefusesAHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	release, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	// The point of TryAcquire: a second holder is refused at once, not queued.
	done := make(chan error, 1)
	go func() {
		_, err := TryAcquire(path)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("TryAcquire = %v, want ErrBusy", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TryAcquire blocked; it must never wait")
	}
	release()
	again, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	again()
}

func TestTryAcquireAndAcquireShareTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	release, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	go func() {
		free, err := Acquire(path)
		if err == nil {
			free()
		}
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("Acquire did not wait for the lock TryAcquire holds")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire never got the released lock")
	}
}
