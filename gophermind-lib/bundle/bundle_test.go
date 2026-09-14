package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportImportRoundtrip(t *testing.T) {
	src := t.TempDir()
	cfg := filepath.Join(src, ".gophermind")
	os.MkdirAll(filepath.Join(cfg, "prompts"), 0o755)
	os.WriteFile(filepath.Join(cfg, "policy"), []byte(`{"allow_list":["ls"]}`), 0o644)
	os.WriteFile(filepath.Join(cfg, "prompts", "reviewer.md"), []byte("be strict"), 0o644)
	// This should be skipped from the bundle.
	os.MkdirAll(filepath.Join(cfg, "sessions"), 0o755)
	os.WriteFile(filepath.Join(cfg, "sessions", "s1.jsonl"), []byte("secret"), 0o644)

	dst := filepath.Join(t.TempDir(), "team.tar.gz")
	if err := Export(src, dst); err != nil {
		t.Fatal(err)
	}

	into := t.TempDir()
	if err := Import(dst, into); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(into, ".gophermind", "policy")); err != nil || string(b) != `{"allow_list":["ls"]}` {
		t.Errorf("policy not imported: %q err=%v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(into, ".gophermind", "prompts", "reviewer.md")); err != nil || string(b) != "be strict" {
		t.Errorf("prompt not imported: %q err=%v", b, err)
	}
	// Session data must NOT be in the bundle.
	if _, err := os.Stat(filepath.Join(into, ".gophermind", "sessions", "s1.jsonl")); err == nil {
		t.Error("sessions should be excluded from the bundle")
	}
}

func TestExportMissing(t *testing.T) {
	if err := Export(t.TempDir(), filepath.Join(t.TempDir(), "x.tar.gz")); err == nil {
		t.Error("exporting with no .gophermind dir should error")
	}
}
