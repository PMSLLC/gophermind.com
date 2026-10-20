package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateRoundTripAndGuards(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := candidate{Revision: 1, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "abc", Source: "package greet\n"}
	if err := saveCandidate(dir, "fn-greet", c); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "_state", candidatePrefix+"fn-greet"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("candidate file %v %v", fi, err)
	}
	got, found, err := readCandidate(dir, "fn-greet")
	if err != nil || !found || got != c {
		t.Fatalf("read = %+v %v %v", got, found, err)
	}
	if got.entry() != (ladderEntry{Name: "a/m1", Tier: "standard", Pos: 1}) {
		t.Fatalf("entry = %+v", got.entry())
	}
	if err := removeCandidate(dir, "fn-greet"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := readCandidate(dir, "fn-greet"); found {
		t.Fatal("a removed candidate is still read")
	}
	if err := removeCandidate(dir, "fn-greet"); err != nil {
		t.Fatalf("removing a missing candidate: %v", err)
	}
	// A file that does not parse is derived data: removed and read as absent.
	if err := writeStateBytes(dir, candidatePrefix+"fn-greet", []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readCandidate(dir, "fn-greet"); found || err != nil {
		t.Fatalf("garbage read as %v %v", found, err)
	}
	if fileExists(filepath.Join(dir, "_state", candidatePrefix+"fn-greet")) {
		t.Fatal("the garbage file was kept")
	}
	// A candidate that cannot be a saved pass (no reply hash, no ladder position,
	// a negative revision) is removed and read as absent too.
	for _, bad := range []candidate{
		{Revision: 0, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "", Source: "package greet\n"},
		{Revision: 0, Entry: "a/m1", Tier: "standard", Pos: 0, SHA256: "abc", Source: "package greet\n"},
		{Revision: -1, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "abc", Source: "package greet\n"},
	} {
		if err := saveCandidate(dir, "fn-greet", bad); err != nil {
			t.Fatal(err)
		}
		if _, found, err := readCandidate(dir, "fn-greet"); found || err != nil {
			t.Fatalf("candidate %+v read as %v %v", bad, found, err)
		}
		if fileExists(filepath.Join(dir, "_state", candidatePrefix+"fn-greet")) {
			t.Fatalf("candidate %+v was kept", bad)
		}
	}
	for _, id := range []string{"../x", "Fn", ""} {
		if err := saveCandidate(dir, id, c); err == nil {
			t.Errorf("saveCandidate(%q) accepted", id)
		}
		if err := removeCandidate(dir, id); err == nil {
			t.Errorf("removeCandidate(%q) accepted", id)
		}
	}
}
