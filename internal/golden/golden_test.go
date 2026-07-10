package golden

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.golden")
	os.WriteFile(path, []byte("hello"), 0o644)
	// Should pass without failing the test.
	Assert(t, path, "hello")
}

func TestAssertUpdateWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "x.golden")
	t.Setenv("GOLDEN_UPDATE", "1")
	Assert(t, path, "generated content")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file not written: %v", err)
	}
	if string(data) != "generated content" {
		t.Errorf("golden content = %q", data)
	}
}

func TestAssertMismatchFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.golden")
	os.WriteFile(path, []byte("expected"), 0o644)
	// Run Assert against a sub-test so we can observe the failure without failing
	// this test.
	fake := &testing.T{}
	Assert(fake, path, "different")
	if !fake.Failed() {
		t.Error("mismatched content should fail the assertion")
	}
}
