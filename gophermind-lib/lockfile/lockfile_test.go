package lockfile

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The property the whole pipeline rests on: N concurrent writers, none lost.
func TestAcquireSerializesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "state.lock")
	data := filepath.Join(dir, "state")

	if err := os.WriteFile(data, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}

	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := Acquire(lock)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			// Read-modify-write under the lock. Without it, increments are lost.
			b, err := os.ReadFile(data)
			if err != nil {
				t.Error(err)
				return
			}
			var cur int
			for _, c := range b {
				cur = cur*10 + int(c-'0')
			}
			if err := WriteAtomic(data, []byte(itoa(cur+1)), 0o600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	b, _ := os.ReadFile(data)
	var got int
	for _, c := range b {
		got = got*10 + int(c-'0')
	}
	if got != n {
		t.Fatalf("counter = %d after %d locked increments, want %d: writes were lost", got, n, n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}

func TestWriteAtomicReplacesContentWholesale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteAtomic(path, []byte("a longer first value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "short" {
		t.Errorf("got %q: a rename must replace, never overlay", b)
	}
}

func TestWriteAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want just the target file", names)
	}
}

func TestWriteAtomicHonorsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}
