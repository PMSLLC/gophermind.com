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
	today := time.Now().UTC().Format("20060102")
	rec, _, err := m.startVersion("csvstat", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "csvstat-v1-" + today + "-r001"; rec.ID != want {
		t.Fatalf("id %q want %q", rec.ID, want)
	}
	waitIdle(t, m)
	rec, _, err = m.startVersion("csvstat", "v1", false)
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
	rec, _, err = m.startVersion("csvstat", "v1.1", false)
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
	if _, _, err := m.startVersion("csvstat", "v1", false); err != nil {
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
	rec, _, err := m.startVersion("csvstat", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	var busy *briefBusyError
	if _, _, err := m.startVersion("csvstat", "v1.1", false); !errors.As(err, &busy) {
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
	rec, warns, err := m.startVersion("csvstat", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	_ = warns
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
