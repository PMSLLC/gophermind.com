package rundir_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/rundir"
)

func TestCreateLayoutAndExclude(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("# brief"))
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(repo, ".gophermind", "gm-2026-09-29-001") {
		t.Errorf("dir = %s", dir)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "brief.md")); string(b) != "# brief" {
		t.Error("brief.md not copied")
	}
	if fi, err := os.Stat(filepath.Join(dir, "logs")); err != nil || !fi.IsDir() {
		t.Error("logs/ missing")
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Errorf("exclude = %q", ex)
	}
	if _, err := rundir.Create(repo, "gm-2026-09-29-001", []byte("x")); !errors.Is(err, rundir.ErrExists) {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
	ex, _ = os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex), ".gophermind/") != 1 {
		t.Error("exclude entry duplicated")
	}
}

func TestCreateRejectsBadID(t *testing.T) {
	for _, id := range []string{"", "../x", "gm-1", "gm-2026-09-29-001/../../x"} {
		if _, err := rundir.Create(t.TempDir(), id, nil); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
}

func TestCreateWithoutGitDirIsFine(t *testing.T) {
	if _, err := rundir.Create(t.TempDir(), "gm-2026-09-29-001", []byte("x")); err != nil {
		t.Fatal(err)
	}
}
