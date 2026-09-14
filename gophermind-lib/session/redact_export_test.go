package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportRedactedScrubsSecrets(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "s1",
		`{"role":"user","content":"my key is AKIA0123456789ABCDEF and email a@b.com"}`,
		`{"role":"assistant","content":"noted"}`)

	dst := filepath.Join(t.TempDir(), "out.jsonl")
	if err := exportRedactedIn(dir, "s1", dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "AKIA0123456789ABCDEF") {
		t.Errorf("secret not redacted:\n%s", out)
	}
	if strings.Contains(out, "a@b.com") {
		t.Errorf("email not redacted:\n%s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("expected a redaction placeholder:\n%s", out)
	}
	// Non-sensitive content is preserved.
	if !strings.Contains(out, "noted") {
		t.Errorf("clean content should survive:\n%s", out)
	}
}

func TestExportRedactedBadID(t *testing.T) {
	if err := exportRedactedIn(t.TempDir(), "../evil", "/tmp/x.jsonl"); err == nil {
		t.Error("bad id should be rejected")
	}
}

func TestExportRedactedMissing(t *testing.T) {
	if err := exportRedactedIn(t.TempDir(), "nope", filepath.Join(t.TempDir(), "o.jsonl")); err == nil {
		t.Error("missing session should error")
	}
}
