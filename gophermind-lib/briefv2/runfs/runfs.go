// Package runfs holds the small file primitives the filesystem run state is
// built on: an atomic write, an exclusive-create lock, an append-only line
// file and its reader. The blackboard, the call ledger and the attempt
// artifacts all use it, so there is one place to get these details right.
package runfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TimeFormat is fixed width and always UTC, so timestamps compare correctly as
// text (RFC3339Nano trims trailing zeros and would not).
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// TS formats t for storage.
func TS(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTS is the inverse of TS; the empty string is the zero time.
func ParseTS(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(TimeFormat, s)
}

// Fixed returns a resolver that maps every run id to dir. Tests use it; the
// commands resolve a run id through the run record instead.
func Fixed(dir string) func(runID string) (string, error) {
	return func(string) (string, error) { return dir, nil }
}

// WriteFileAtomic writes data to path through a temporary file in the same
// folder and a rename, so a reader sees the old file or the new one and never
// half of either. The file is mode 0600 and missing folders are made 0700.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	return nil
}

// ErrLockTimeout is returned by Lock when the lock stayed held for the whole wait.
var ErrLockTimeout = errors.New("runfs: lock wait timed out")

// Lock takes an exclusive lock by creating path with O_EXCL and returns the
// function that releases it. It retries until wait has passed. A lock file
// older than stale belongs to a holder that died and is removed. The window
// between judging a lock stale and removing it is tiny next to a stale age of
// tens of seconds, and a lock is held for milliseconds.
func Lock(path string, wait, stale time.Duration) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	delay := time.Millisecond
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > stale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrLockTimeout
		}
		time.Sleep(delay)
		if delay < 20*time.Millisecond {
			delay *= 2
		}
	}
}

// AppendLine appends line and a newline to path with one write on a file opened
// O_APPEND, so concurrent appenders do not interleave. The line must not hold
// a newline itself (compact JSON never does).
func AppendLine(path string, line []byte) error {
	if bytes.IndexByte(line, '\n') >= 0 {
		return errors.New("runfs: a line must not contain a newline")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(line)+1)
	buf = append(append(buf, line...), '\n')
	_, werr := f.Write(buf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// ReadLines returns the complete lines of path from byte offset off and the
// offset just after the last complete line. A partial last line (an append in
// progress) is left for the next call. A missing file has no lines.
func ReadLines(path string, off int64) (lines [][]byte, next int64, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, off, nil
	}
	if err != nil {
		return nil, off, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, off, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, off, err
	}
	pos := 0
	for {
		i := bytes.IndexByte(data[pos:], '\n')
		if i < 0 {
			break
		}
		lines = append(lines, data[pos:pos+i])
		pos += i + 1
	}
	return lines, off + int64(pos), nil
}

// Size is the byte length of path, 0 when it does not exist.
func Size(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
