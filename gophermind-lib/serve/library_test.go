//go:build !windows

package serve

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/library"
)

// versionBody is the example brief with its id and repo lines removed, the
// shape a library version is stored in.
func (e *briefEnv) versionBody() []byte {
	e.t.Helper()
	s := e.briefText("gm-2026-10-23-001")
	s = regexp.MustCompile(`(?m)^(id|repo):.*\n`).ReplaceAllString(s, "")
	return []byte(s)
}

func (e *briefEnv) manager() *briefManager {
	e.t.Helper()
	cmd := func(args []string) *exec.Cmd {
		e.mu.Lock()
		e.calls = append(e.calls, append([]string(nil), args...))
		e.mu.Unlock()
		return exec.Command("sh", "-c", e.script(args))
	}
	m, err := newBriefManager(Options{BriefsDir: e.dir, BriefCommand: cmd}, e.root)
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

// libEnv is a manager with the fake command and a saved csvstat v1.
func libEnv(t *testing.T) (*briefEnv, *briefManager) {
	e := newBriefEnv(t)
	m := e.manager()
	if _, err := m.lib.Save("csvstat", "", "first", e.versionBody()); err != nil {
		t.Fatal(err)
	}
	return e, m
}

func TestStartVersionNamesRun(t *testing.T) {
	_, m := libEnv(t)
	today := m.now().UTC().Format("20060102")
	rec, _, err := m.startVersion("csvstat", "v1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "csvstat-v1-" + today + "-r001"; rec.ID != want {
		t.Fatalf("id %q want %q", rec.ID, want)
	}
	waitIdle(t, m)
	rec, _, err = m.startVersion("csvstat", "v1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "csvstat-v1-" + today + "-r002"; rec.ID != want {
		t.Fatalf("second id %q want %q", rec.ID, want)
	}
	waitIdle(t, m)
	if _, err := m.lib.Save("csvstat", "v1", "next", []byte(string(mustGet(t, m, "v1")))); err != nil {
		t.Fatal(err)
	}
	rec, _, err = m.startVersion("csvstat", "v1.1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "csvstat-v1.1-" + today + "-r001"; rec.ID != want {
		t.Fatalf("other version id %q want %q", rec.ID, want)
	}
	waitIdle(t, m)
}

func mustGet(t *testing.T, m *briefManager, v string) []byte {
	t.Helper()
	_, text, err := m.lib.Get("csvstat", v)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func waitIdle(t *testing.T, m *briefManager) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m.activeID() == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run still active")
}

func TestStartVersionLocks(t *testing.T) {
	_, m := libEnv(t)
	if _, _, err := m.startVersion("csvstat", "v1", false, ""); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	if err := m.lib.Update("csvstat", "v1", []byte("x")); !errors.Is(err, library.ErrLocked) {
		t.Fatalf("Update = %v, want ErrLocked", err)
	}
}

func TestStartVersionBusyDoesNotLock(t *testing.T) {
	e, m := libEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	if _, err := m.lib.Save("csvstat", "v1", "b", mustGet(t, m, "v1")); err != nil {
		t.Fatal(err)
	}
	rec, _, err := m.startVersion("csvstat", "v1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	var busy *briefBusyError
	if _, _, err := m.startVersion("csvstat", "v1.1", false, ""); !errors.As(err, &busy) {
		t.Fatalf("err = %v, want busy", err)
	}
	v, _, _ := m.lib.Get("csvstat", "v1.1")
	if v.Locked {
		t.Error("busy start locked the version")
	}
	if err := m.lib.Update("csvstat", "v1.1", mustGet(t, m, "v1.1")); err != nil {
		t.Errorf("Update after busy = %v", err)
	}
	_ = m.stop(rec.ID)
	waitIdle(t, m)
}

func TestStartedBriefHasStamp(t *testing.T) {
	e, m := libEnv(t)
	rec, _, err := m.startVersion("csvstat", "v1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	run, err := os.ReadFile(filepath.Join(e.dir, rec.ID, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(run), "\nid: "+rec.ID+"\n") || !strings.Contains(string(run), "\nrepo: "+e.root+"\n") {
		t.Errorf("run copy lacks the stamp:\n%.300s", run)
	}
	if got := mustGet(t, m, "v1"); regexp.MustCompile(`(?m)^(id|repo):`).Match(got) {
		t.Errorf("stored version text has id or repo:\n%.300s", got)
	}
	r, ok := m.readRecord(rec.ID)
	if !ok || r.Family != "csvstat" || r.Version != "v1" {
		t.Errorf("record %+v", r)
	}
}

func TestLibraryFolderIsNotARun(t *testing.T) {
	e := newBriefEnv(t)
	if _, err := os.Stat(filepath.Join(e.dir, "library")); err != nil {
		t.Fatalf("library dir not made: %v", err)
	}
	w := e.get("/app/api/briefs")
	if w.Code != 200 || strings.Contains(w.Body.String(), "library") {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
}

func TestStampFrontCases(t *testing.T) {
	// Block scalar with a blank line survives; old id/repo lines go.
	in := "---\nspec_version: \"2.0\"\nid: old\nnote: |\n  a\n\n  b\nrepo: /x\ntitle: T\n---\nbody\n"
	got := string(stampFront([]byte(in), "fam-v1-20261101-r001", "/r"))
	want := "---\nid: fam-v1-20261101-r001\nrepo: /r\nspec_version: \"2.0\"\nnote: |\n  a\n\n  b\ntitle: T\n---\nbody\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	// CRLF keeps CRLF line ends.
	got = string(stampFront([]byte("---\r\ntitle: T\r\nid: o\r\n---\r\nb\r\n"), "i", "/r"))
	if want := "---\r\nid: i\r\nrepo: /r\r\ntitle: T\r\n---\r\nb\r\n"; got != want {
		t.Errorf("crlf got %q want %q", got, want)
	}
	// No frontmatter: unchanged.
	if got := string(stampFront([]byte("just text\n"), "i", "/r")); got != "just text\n" {
		t.Errorf("no frontmatter changed: %q", got)
	}
}

func TestStartVersionTooLongIDLeavesUnlocked(t *testing.T) {
	e := newBriefEnv(t)
	m := e.manager()
	fam := strings.Repeat("a", 59)
	// Save only yields short names, so lay the version out on disk directly.
	name := "v1" + strings.Repeat(".1", 30)
	vdir := filepath.Join(e.dir, "library", fam, name)
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"family":"` + fam + `","name":"` + name + `","parent":"","label":"x","created":"2026-11-01T00:00:00Z","locked_at":""}`
	if err := os.WriteFile(filepath.Join(vdir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "brief.md"), e.versionBody(), 0o644); err != nil {
		t.Fatal(err)
	}
	v := library.Version{Name: name}
	_, _, err := m.startVersion(fam, v.Name, false, "")
	if err == nil || !strings.Contains(err.Error(), "not a valid run id") {
		t.Fatalf("err = %v", err)
	}
	if got, _, _ := m.lib.Get(fam, v.Name); got.Locked {
		t.Error("version locked despite the error")
	}
}

func TestStartVersionWithReposDirNamesRunRepo(t *testing.T) {
	e := newBriefEnv(t, func(o *Options, d *Deps) { o.ReposDir = t.TempDir() })
	e.script = func(args []string) string {
		if len(args) > 1 && args[1] == "init-repo" {
			return "mkdir -p " + args[2]
		}
		return "echo ran"
	}
	m := e.manager()
	m.repos = e.reposDir
	if _, err := m.lib.Save("csvstat", "", "x", e.versionBody()); err != nil {
		t.Fatal(err)
	}
	rec, _, err := m.startVersion("csvstat", "v1", false, "")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, m)
	run, _ := os.ReadFile(filepath.Join(e.dir, rec.ID, "brief.md"))
	if want := "\nrepo: " + filepath.Join(e.reposDir, rec.ID) + "\n"; !strings.Contains(string(run), want) {
		t.Errorf("run copy lacks %q:\n%.300s", want, run)
	}
}
