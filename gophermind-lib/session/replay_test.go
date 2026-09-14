package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayFormatsTurns(t *testing.T) {
	dir := t.TempDir()
	writeSessionLines(t, dir, "s1",
		`{"role":"user","content":"fix the bug"}`,
		`{"role":"assistant","content":"looking into it"}`,
		`{"role":"tool","name":"read_file","content":"file contents"}`)

	out, err := replayIn(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user", "assistant", "tool", "fix the bug", "looking into it", "read_file"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay missing %q:\n%s", want, out)
		}
	}
	// Turns should be numbered.
	if !strings.Contains(out, "1") || !strings.Contains(out, "3") {
		t.Errorf("turns should be numbered:\n%s", out)
	}
}

func TestReplayEncrypted(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "enc.jsonl"), append([]byte(encMagic), 0xDE, 0xAD), 0o600)
	if _, err := replayIn(dir, "enc"); err == nil {
		t.Error("encrypted session should not be replayable without the key")
	}
}

func TestReplayMissing(t *testing.T) {
	if _, err := replayIn(t.TempDir(), "nope"); err == nil {
		t.Error("missing session should error")
	}
}
