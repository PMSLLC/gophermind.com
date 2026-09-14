package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPatchApplyAtomicOnMissingFile verifies that when a multi-file patch
// references a non-existent file, NO file is modified — the whole patch is
// applied atomically or not at all.
func TestPatchApplyAtomicOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.txt")
	orig := "line1\nline2\nline3\n"
	if err := os.WriteFile(aPath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Patch touches a.txt (exists) and c.txt (missing) — must fail wholesale.
	patch := "diff --git a/a.txt b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n" +
		" line1\n-line2\n+line2-modified\n line3\n" +
		"diff --git a/c.txt b/c.txt\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-x\n+y\n"
	args, _ := json.Marshal(map[string]string{"patch": patch})
	if _, err := run(t, PatchApply(dir), string(args)); err == nil {
		t.Fatal("patch referencing a missing file should fail")
	}

	// a.txt must be untouched (no partial application).
	b, _ := os.ReadFile(aPath)
	if string(b) != orig {
		t.Errorf("a.txt was modified despite the patch failing: %q", b)
	}
}
