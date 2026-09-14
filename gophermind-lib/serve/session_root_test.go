package serve

import (
	"os"
	"path/filepath"
	"testing"
)

// A session's root is where its tools read, write and run commands. Storing
// it per session is what lets one window work on several projects at once.
func TestSessionRootRoundTrips(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	if err := WriteSessionRoot("s1", dir); err != nil {
		t.Fatal(err)
	}
	if got := ReadSessionRoot("s1"); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

// No root means "use the server's own", which is every session that predates
// this and every session the user never pointed anywhere.
func TestSessionRootEmptyWhenUnset(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	if got := ReadSessionRoot("nope"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// The root is a path the user picks, and every tool's containment check is
// computed against it. A path that is not a real directory would put the
// agent somewhere that does not exist, so it is refused when it is set
// rather than discovered on the first tool call.
func TestSessionRootRejectsANonDirectory(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{file, filepath.Join(t.TempDir(), "does-not-exist"), "relative/path", ""} {
		if err := WriteSessionRoot("s2", bad); err == nil {
			t.Errorf("root %q was accepted", bad)
		}
	}
}

// Clearing it reverts the session to the server's own root.
func TestSessionRootCanBeCleared(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	if err := WriteSessionRoot("s3", dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearSessionRoot("s3"); err != nil {
		t.Fatal(err)
	}
	if got := ReadSessionRoot("s3"); got != "" {
		t.Errorf("root survived clearing: %q", got)
	}
}
