package runview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIDValidators(t *testing.T) {
	for _, id := range []string{"gm-2026-10-08-001", "gm-1999-01-31-999"} {
		if !ValidRunID(id) {
			t.Errorf("ValidRunID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "gm-2026-10-08-01", "../gm-2026-10-08-001", "gm-2026-10-08-001/..", "GM-2026-10-08-001", "gm-2026-10-08-001\n", "gm-2026-10-08-001 "} {
		if ValidRunID(id) {
			t.Errorf("ValidRunID(%q) = true", id)
		}
	}
	for _, id := range []string{"fn-greet", "a", "0abc", "gm-2026-10-08-001"} {
		if !ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "-a", "A", "a_b", "a/b", "..", "a.b", "a\n"} {
		if ValidNodeID(id) {
			t.Errorf("ValidNodeID(%q) = true", id)
		}
	}
	for _, n := range []string{"reply.go", "reply.txt", "check-output.txt", "attempt.json"} {
		if !ValidArtifactName(n) {
			t.Errorf("ValidArtifactName(%q) = false", n)
		}
	}
	for _, n := range []string{"", "../reply.go", "reply.go/", "REPLY.GO", "prompt.txt", "attempt.json.bak", "reply.go\x00"} {
		if ValidArtifactName(n) {
			t.Errorf("ValidArtifactName(%q) = true", n)
		}
	}
}

func mkRun(t *testing.T, root, id string) string {
	t.Helper()
	dir := filepath.Join(root, ".gophermind", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), []byte("# b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunDirOf(t *testing.T) {
	root := t.TempDir()
	want := mkRun(t, root, "gm-2026-10-08-001")
	got, err := runDirOf(root, "gm-2026-10-08-001")
	if err != nil || got != want {
		t.Fatalf("runDirOf = %q, %v; want %q", got, err, want)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-002"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing run: %v, want ErrNotFound", err)
	}
	for _, id := range []string{"..", "../x", "gm-2026-10-08-001/../..", ""} {
		if _, err := runDirOf(root, id); !errors.Is(err, ErrBadID) {
			t.Errorf("runDirOf(%q) = %v, want ErrBadID", id, err)
		}
	}
	// A run folder that is a symbolic link is refused.
	if err := os.Symlink(want, filepath.Join(root, ".gophermind", "gm-2026-10-08-003")); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-003"); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlinked run: %v, want ErrNotFound", err)
	}
	// So is a symlinked .gophermind folder.
	other := t.TempDir()
	if err := os.Symlink(filepath.Join(root, ".gophermind"), filepath.Join(other, ".gophermind")); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(other, "gm-2026-10-08-001"); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlinked .gophermind: %v, want ErrNotFound", err)
	}
	// A folder without brief.md is not a run.
	if err := os.MkdirAll(filepath.Join(root, ".gophermind", "gm-2026-10-08-004"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runDirOf(root, "gm-2026-10-08-004"); !errors.Is(err, ErrNotFound) {
		t.Errorf("folder without brief.md: %v, want ErrNotFound", err)
	}
}

func TestReadLimited(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, cut, err := readLimited(p, 100)
	if err != nil || cut || string(data) != "0123456789" {
		t.Fatalf("readLimited = %q, %v, %v", data, cut, err)
	}
	data, cut, err = readLimited(p, 4)
	if err != nil || !cut || string(data) != "0123" {
		t.Fatalf("readLimited(4) = %q, %v, %v", data, cut, err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLimited(link, 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("symlink: %v, want ErrNotFound", err)
	}
	if _, _, err := readLimited(filepath.Join(dir, "missing"), 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v, want ErrNotFound", err)
	}
}
