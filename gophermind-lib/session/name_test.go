package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nameTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	// Sessions live in a "sessions" subdir of the config dir; create it so the
	// sidecar has somewhere to land.
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeSession drops a minimal session file so List has something to overlay.
func writeNamedSession(t *testing.T, id, firstUserMsg string) {
	t.Helper()
	p, err := Path(id)
	if err != nil {
		t.Fatal(err)
	}
	line := `{"role":"user","content":` + jsonString(firstUserMsg) + "}\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// TestSetAndGetName round-trips a custom name.
func TestSetAndGetName(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "abc", "hello there")

	if err := SetName("abc", "My Project"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	if got := Name("abc"); got != "My Project" {
		t.Errorf("Name = %q, want %q", got, "My Project")
	}
}

// TestNameEmptyWhenUnset: no sidecar means no custom name.
func TestNameEmptyWhenUnset(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "abc", "hello")
	if got := Name("abc"); got != "" {
		t.Errorf("Name = %q, want empty", got)
	}
}

// TestClearName removes the custom name, reverting to the derived title.
func TestClearName(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "abc", "hello")
	if err := SetName("abc", "Custom"); err != nil {
		t.Fatal(err)
	}
	if err := ClearName("abc"); err != nil {
		t.Fatalf("ClearName: %v", err)
	}
	if got := Name("abc"); got != "" {
		t.Errorf("Name after clear = %q, want empty", got)
	}
}

// TestSetEmptyNameClears: renaming to blank is how the UI reverts to the title.
func TestSetEmptyNameClears(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "abc", "hello")
	SetName("abc", "Custom")
	if err := SetName("abc", "   "); err != nil {
		t.Fatalf("SetName empty: %v", err)
	}
	if got := Name("abc"); got != "" {
		t.Errorf("Name = %q, want empty after blanking", got)
	}
}

// TestListOverlaysCustomName is the point of the feature: a renamed session
// shows its custom name, an un-renamed one shows the first-message title.
func TestListOverlaysCustomName(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "renamed", "some first message")
	writeNamedSession(t, "plain", "keep this title")
	if err := SetName("renamed", "Chosen Name"); err != nil {
		t.Fatal(err)
	}

	infos, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]Info{}
	for _, i := range infos {
		byID[i.ID] = i
	}
	if byID["renamed"].Name != "Chosen Name" {
		t.Errorf("renamed Name = %q, want the custom name", byID["renamed"].Name)
	}
	if byID["renamed"].Title != "some first message" {
		t.Errorf("renamed Title = %q, want the derived title preserved", byID["renamed"].Title)
	}
	if byID["plain"].Name != "" {
		t.Errorf("plain Name = %q, want empty", byID["plain"].Name)
	}
}

// TestRemoveDeletesSidecar: a renamed-then-deleted session must not leave an
// orphan .name file that would attach to a later session reusing the id.
func TestRemoveDeletesSidecar(t *testing.T) {
	dir := nameTestDir(t)
	writeNamedSession(t, "abc", "hello")
	if err := SetName("abc", "Custom"); err != nil {
		t.Fatal(err)
	}
	if err := Remove("abc"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "abc.name")); !os.IsNotExist(err) {
		t.Error("sidecar .name file survived session deletion")
	}
}

// TestSetNameRejectsBadID keeps the sidecar inside the sessions directory.
func TestSetNameRejectsBadID(t *testing.T) {
	nameTestDir(t)
	if err := SetName("../escape", "x"); err == nil {
		t.Error("expected an error for an invalid id")
	}
}

// TestSetNameStripsControlChars and caps length, so a pathological name cannot
// break the list rendering or the sidecar file.
func TestSetNameSanitizes(t *testing.T) {
	nameTestDir(t)
	writeNamedSession(t, "abc", "hello")
	if err := SetName("abc", "a\nb\tc\x00d"); err != nil {
		t.Fatal(err)
	}
	got := Name("abc")
	if strings.ContainsAny(got, "\n\t\x00") {
		t.Errorf("control chars survived: %q", got)
	}

	long := strings.Repeat("x", 500)
	if err := SetName("abc", long); err != nil {
		t.Fatal(err)
	}
	if len([]rune(Name("abc"))) > maxNameLen {
		t.Errorf("name length %d exceeds cap %d", len([]rune(Name("abc"))), maxNameLen)
	}
}
