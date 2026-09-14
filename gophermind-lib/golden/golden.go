// Package golden provides snapshot ("golden file") assertions: compare an actual
// string against a stored expected file, and regenerate the file when the
// GOLDEN_UPDATE environment variable is set. This catches unintended behavior
// shifts (e.g. a changed system prompt) in a diff-reviewable way.
package golden

import (
	"os"
	"path/filepath"
	"testing"
)

// Assert compares actual against the golden file at path. With GOLDEN_UPDATE set
// (to any non-empty value), it (re)writes the golden file instead of asserting,
// so intentional changes are recorded by rerunning the tests once.
func Assert(t *testing.T, path, actual string) {
	t.Helper()
	if os.Getenv("GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("golden: write: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: read %s (run with GOLDEN_UPDATE=1 to create): %v", path, err)
	}
	if string(want) != actual {
		t.Errorf("output does not match golden %s.\nRe-run with GOLDEN_UPDATE=1 if this change is intended.\n--- got ---\n%s", path, actual)
	}
}
