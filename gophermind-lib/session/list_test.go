package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSession(t *testing.T, dir, id, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "alpha",
		`{"role":"system","content":"you are gophermind"}`+"\n"+
			`{"role":"user","content":"fix the parser bug please"}`+"\n"+
			`{"role":"assistant","content":"done"}`+"\n")
	// make beta newer so ordering (newest-first) is deterministic
	time.Sleep(10 * time.Millisecond)
	writeSession(t, dir, "beta",
		`{"role":"system","content":"sys"}`+"\n"+
			`{"role":"user","content":"add tests"}`+"\n")
	// a non-session file must be ignored
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o600)

	infos, err := listDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("want 2 sessions, got %d: %+v", len(infos), infos)
	}
	// newest first
	if infos[0].ID != "beta" || infos[1].ID != "alpha" {
		t.Errorf("order = %s,%s; want beta,alpha", infos[0].ID, infos[1].ID)
	}
	if infos[1].Messages != 3 {
		t.Errorf("alpha messages = %d, want 3", infos[1].Messages)
	}
	if infos[1].Title != "fix the parser bug please" {
		t.Errorf("alpha title = %q", infos[1].Title)
	}
}

func TestListDirMissing(t *testing.T) {
	infos, err := listDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir should be empty, not error: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("want 0 infos, got %d", len(infos))
	}
}

func TestRemoveDir(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "gone", `{"role":"system","content":"x"}`+"\n")

	if err := removeIn(dir, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.jsonl")); !os.IsNotExist(err) {
		t.Error("session file should be deleted")
	}
	// removing a missing session is an error the caller can report
	if err := removeIn(dir, "missing"); err == nil {
		t.Error("removing a missing session should error")
	}
	// path-escape ids are rejected
	if err := removeIn(dir, "../etc"); err == nil {
		t.Error("invalid id should be rejected")
	}
}

// TestRemoveInMissingSessionWrapsErrNotFound is the deferred follow-up from
// feat/ios-serve (S2 Minor): DELETE /session/{id} mapped every error from
// Remove to 404, including a real disk failure on the os.Remove call itself.
// The caller (serve.sessionDeleteHandler) needs to tell "does not exist" apart
// from "exists but couldn't be deleted", which means the not-found case must
// be identifiable via errors.Is, not by matching error text.
func TestRemoveInMissingSessionWrapsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	err := removeIn(dir, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("removeIn on a missing session = %v, want it to wrap ErrNotFound", err)
	}
}

// TestRemoveInInvalidIDDoesNotWrapErrNotFound: a rejected id is a bad
// request, not "session not found" -- the two must map to different HTTP
// statuses (400 vs 404), so they must be distinguishable here.
func TestRemoveInInvalidIDDoesNotWrapErrNotFound(t *testing.T) {
	dir := t.TempDir()
	err := removeIn(dir, "../etc")
	if errors.Is(err, ErrNotFound) {
		t.Errorf("an invalid id should not wrap ErrNotFound: %v", err)
	}
}

func TestTitleTruncation(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	dir := t.TempDir()
	writeSession(t, dir, "big",
		`{"role":"system","content":"s"}`+"\n"+
			`{"role":"user","content":"`+long+`"}`+"\n")
	infos, _ := listDir(dir)
	if n := len([]rune(infos[0].Title)); n > titleMax+1 { // +1 for the ellipsis rune
		t.Errorf("title not truncated: runes=%d", n)
	}
}
