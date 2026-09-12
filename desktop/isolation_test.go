package main

import (
	"os"
	"path/filepath"
	"testing"

	"gophermind/internal/config"
	"gophermind/internal/session"
)

// isolate points the config directory, and therefore the session store, at a
// temporary directory for the duration of one test.
//
// Without it a test that starts a server writes real sessions into the user's
// own ~/.gophermind/sessions. That is not theoretical: it had left 485 junk
// sessions there, every one of them a scriptedLLM exchange, and the user
// noticed them as clutter long before anyone noticed the cause.
//
// t.Setenv also fails a test that calls it after t.Parallel, which is the
// right constraint here: these tests share process-wide environment.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	return dir
}

// Every test in this package that starts a server must isolate its config
// directory. This test proves the mechanism works, so the others can rely on
// calling isolate() rather than each re-deriving the path.
func TestIsolateRedirectsTheSessionStore(t *testing.T) {
	dir := isolate(t)

	got, err := session.Dir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "sessions")
	if got != want {
		t.Fatalf("session.Dir() = %q, want %q", got, want)
	}

	cfgDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if cfgDir != dir {
		t.Errorf("config.Dir() = %q, want %q", cfgDir, dir)
	}

	// And the real store is untouched by anything this test does.
	home, _ := os.UserHomeDir()
	real := filepath.Join(home, ".gophermind", "sessions")
	if got == real {
		t.Fatal("session.Dir() still points at the user's real store")
	}
}
