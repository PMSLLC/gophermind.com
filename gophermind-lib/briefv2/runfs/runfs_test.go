package runfs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
)

func TestWriteFileAtomicReplacesTheWholeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "a.json")
	if err := runfs.WriteFileAtomic(p, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := runfs.WriteFileAtomic(p, []byte("second")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "second" {
		t.Fatalf("read = %q, %v", b, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(p))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Errorf("left %d entries beside the file, want only the file", len(ents))
	}
}

func TestWriteFileAtomicLeavesNoTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runfs.WriteFileAtomic(target, []byte("x")); err == nil {
		t.Fatal("writing over a directory must fail")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "adir" {
		t.Errorf("entries after the failed write: %v", ents)
	}
}

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "n.lock")
	counter := filepath.Join(dir, "count")
	if err := os.WriteFile(counter, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := runfs.Lock(lock, 10*time.Second, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			defer unlock()
			b, _ := os.ReadFile(counter)
			n, _ := strconv.Atoi(string(b))
			_ = os.WriteFile(counter, []byte(strconv.Itoa(n+1)), 0o600)
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(counter)
	if string(b) != "20" {
		t.Errorf("counter = %s, want 20: the lock let two holders in", b)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after every unlock: %v", err)
	}
}

func TestLockTimesOutWhileHeld(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "n.lock")
	unlock, err := runfs.Lock(lock, time.Second, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = runfs.Lock(lock, 50*time.Millisecond, time.Hour)
	if err != runfs.ErrLockTimeout {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

func TestLockReclaimsAStaleLock(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "n.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := runfs.Lock(lock, time.Second, time.Minute)
	if err != nil {
		t.Fatalf("a lock older than stale must be reclaimed: %v", err)
	}
	unlock()
}

func TestAppendLineKeepsConcurrentLinesIntact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "_state", "events.jsonl")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, _ := json.Marshal(map[string]any{"n": i, "pad": strings.Repeat("x", 200)})
			if err := runfs.AppendLine(p, b); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	lines, _, err := runfs.ReadLines(p, 0)
	if err != nil || len(lines) != 100 {
		t.Fatalf("lines = %d, %v; want 100", len(lines), err)
	}
	seen := map[int]bool{}
	for _, ln := range lines {
		var v struct{ N int }
		if err := json.Unmarshal(ln, &v); err != nil {
			t.Fatalf("a line is not intact JSON: %q", ln)
		}
		seen[v.N] = true
	}
	if len(seen) != 100 {
		t.Errorf("distinct lines = %d, want 100", len(seen))
	}
}

func TestAppendLineRefusesANewline(t *testing.T) {
	if err := runfs.AppendLine(filepath.Join(t.TempDir(), "x.jsonl"), []byte("a\nb")); err == nil {
		t.Fatal("a line containing a newline must be refused")
	}
}

func TestReadLinesStopsAtAPartialLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	if err := os.WriteFile(p, []byte("a\nb"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, next, err := runfs.ReadLines(p, 0)
	if err != nil || len(lines) != 1 || string(lines[0]) != "a" || next != 2 {
		t.Fatalf("lines = %q next = %d err = %v; want [a] 2", lines, next, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("\n")
	f.Close()
	lines, next, _ = runfs.ReadLines(p, next)
	if len(lines) != 1 || string(lines[0]) != "b" || next != 4 {
		t.Fatalf("second read: lines = %q next = %d", lines, next)
	}
}

func TestReadLinesOfAMissingFile(t *testing.T) {
	lines, next, err := runfs.ReadLines(filepath.Join(t.TempDir(), "none"), 7)
	if err != nil || len(lines) != 0 || next != 7 {
		t.Fatalf("lines = %v next = %d err = %v", lines, next, err)
	}
	if got := runfs.Size(filepath.Join(t.TempDir(), "none")); got != 0 {
		t.Errorf("Size of a missing file = %d, want 0", got)
	}
}

func TestTimestampsRoundTripAndSortAsText(t *testing.T) {
	a := time.Date(2026, 10, 7, 12, 0, 0, 5, time.UTC)
	b := a.Add(time.Nanosecond)
	if runfs.TS(a) >= runfs.TS(b) {
		t.Errorf("TS must sort as text: %s %s", runfs.TS(a), runfs.TS(b))
	}
	back, err := runfs.ParseTS(runfs.TS(a))
	if err != nil || !back.Equal(a) {
		t.Errorf("round trip = %v, %v", back, err)
	}
	if z, err := runfs.ParseTS(""); err != nil || !z.IsZero() {
		t.Errorf("empty string must parse to the zero time: %v %v", z, err)
	}
}

func TestFixedResolvesEveryRunToTheSameFolder(t *testing.T) {
	r := runfs.Fixed("/some/dir")
	for _, id := range []string{"a", "gm-2026-10-07-001"} {
		got, err := r(id)
		if err != nil || got != "/some/dir" {
			t.Errorf("Fixed(%q) = %q, %v", id, got, err)
		}
	}
}

// A lock left by a killed process must be reclaimed inside one wait, so the
// default wait has to outlast the default stale age.
func TestDefaultLockWaitOutlastsTheStaleAge(t *testing.T) {
	if runfs.LockWait <= runfs.LockStaleAfter {
		t.Fatalf("LockWait %v <= LockStaleAfter %v", runfs.LockWait, runfs.LockStaleAfter)
	}
	lock := filepath.Join(t.TempDir(), "x.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-runfs.LockStaleAfter - time.Second)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := runfs.Lock(lock, runfs.LockWait, runfs.LockStaleAfter)
	if err != nil {
		t.Fatalf("a lock past the stale age was not reclaimed: %v", err)
	}
	unlock()
}
