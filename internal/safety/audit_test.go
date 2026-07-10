package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditChainVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	al := NewAuditLog(path)
	al.Record("read_file", `{"path":"x"}`, "auto", "contents")
	al.Record("write_file", `{"path":"y"}`, "approved", "ok")
	al.Record("run_shell", `{"command":"ls"}`, "denied", "")

	entries := al.Entries()
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	// Chain links: each PrevHash equals the prior Hash; first PrevHash empty.
	if entries[0].PrevHash != "" {
		t.Errorf("first PrevHash should be empty, got %q", entries[0].PrevHash)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].PrevHash != entries[i-1].Hash {
			t.Errorf("entry %d PrevHash does not link to prior Hash", i)
		}
	}
	// The result hash is stored, not the raw result.
	if entries[0].ResultHash == "" || entries[0].ResultHash == "contents" {
		t.Errorf("result should be hashed, got %q", entries[0].ResultHash)
	}

	if err := VerifyAuditFile(path); err != nil {
		t.Errorf("intact chain should verify: %v", err)
	}
}

func TestAuditTamperDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	al := NewAuditLog(path)
	al.Record("write_file", `{"path":"a"}`, "approved", "ok")
	al.Record("write_file", `{"path":"b"}`, "approved", "ok")

	// Tamper: rewrite the file with an altered decision on the first entry.
	data, _ := os.ReadFile(path)
	tampered := []byte(replaceFirst(string(data), `"decision":"approved"`, `"decision":"denied"`))
	os.WriteFile(path, tampered, 0o600)

	if err := VerifyAuditFile(path); err == nil {
		t.Error("tampered audit chain should fail verification")
	}
}

func TestAuditEmptyFileVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.jsonl")
	if err := VerifyAuditFile(path); err != nil {
		t.Errorf("missing/empty audit file should verify (nothing to check): %v", err)
	}
}

// replaceFirst replaces the first occurrence of old with new.
func replaceFirst(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
