package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The brief is read by the menu handler, not by the agent, because the
// agent's file tools are contained to the project root and a brief almost
// never lives inside the project it describes. This pins the read itself: the
// handler needs a Wails context to open a dialog, so the test exercises the
// part that does not.
func TestBriefIsReadWholeAndCapped(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "brief.md")
	body := "# CYOA\n\nBuild a branching narrative platform.\n"
	if err := os.WriteFile(small, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readBrief(small)
	if err != nil {
		t.Fatal(err)
	}
	if got != body {
		t.Errorf("got %q, want the whole file", got)
	}

	// A file past the cap is truncated rather than refused: a very long brief
	// is still worth starting from, and crowding the prompt out entirely
	// would be worse than losing its tail.
	big := filepath.Join(dir, "big.md")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", maxBriefBytes+5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = readBrief(big)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxBriefBytes {
		t.Errorf("read %d bytes, want the cap of %d", len(got), maxBriefBytes)
	}
}

func TestReadBriefReportsAMissingFile(t *testing.T) {
	if _, err := readBrief(filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Error("a missing brief returned no error")
	}
}
