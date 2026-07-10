package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChangedDetectsModification(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("v1"), 0o644)

	base, err := LatestModTime(dir)
	if err != nil {
		t.Fatal(err)
	}
	// No change yet.
	changed, _, _ := Changed(dir, base)
	if changed {
		t.Error("no change should be reported immediately")
	}

	// Modify a file with a newer mtime.
	future := time.Now().Add(time.Second)
	os.Chtimes(f, future, future)
	changed, newMod, _ := Changed(dir, base)
	if !changed {
		t.Error("a newer mtime should be detected as a change")
	}
	if !newMod.After(base) {
		t.Errorf("new mod time should advance: %v vs %v", newMod, base)
	}
}

func TestLatestModTimeMissing(t *testing.T) {
	ts, err := LatestModTime(filepath.Join(t.TempDir(), "nope"))
	if err != nil || !ts.IsZero() {
		t.Errorf("missing path should be (zero,nil), got (%v,%v)", ts, err)
	}
}
