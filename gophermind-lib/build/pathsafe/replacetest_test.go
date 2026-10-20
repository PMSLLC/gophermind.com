package pathsafe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/pathsafe"
)

func TestReplaceTestReplacesAnExistingTestFileAtomically(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	dest := filepath.Join(repo, "pkg", "x_test.go")
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, []byte("old"), 0o644)
	if err := pathsafe.ReplaceTest(repo, "pkg/x_test.go", []byte("package pkg\n")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "package pkg\n" {
		t.Fatalf("content = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Fatalf("a temp file was left behind: %v", entries)
	}
}

func TestReplaceTestRefusesWhatIsNotAPlainTestFile(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(repo, "pkg"), 0o755)
	elsewhere := filepath.Join(t.TempDir(), "outside_test.go")
	os.WriteFile(elsewhere, []byte("outside"), 0o644)
	os.Symlink(elsewhere, filepath.Join(repo, "pkg", "link_test.go"))
	for name, rel := range map[string]string{
		"a source file":    "pkg/x.go",
		"a parent escape":  "../x_test.go",
		"an absolute path": "/tmp/x_test.go",
		"under .git":       ".git/x_test.go",
		"a symbolic link":  "pkg/link_test.go",
	} {
		if err := pathsafe.ReplaceTest(repo, rel, []byte("package pkg\n")); err == nil {
			t.Errorf("%s: ReplaceTest accepted %q", name, rel)
		} else if strings.Contains(err.Error(), rel) {
			t.Errorf("%s: the error quotes the path: %v", name, err)
		}
	}
	if got, _ := os.ReadFile(elsewhere); string(got) != "outside" {
		t.Fatal("ReplaceTest wrote through a link")
	}
	// A directory swapped for a link to the outside is refused too.
	os.Symlink(t.TempDir(), filepath.Join(repo, "swapped"))
	if err := pathsafe.ReplaceTest(repo, "swapped/x_test.go", []byte("package pkg\n")); err == nil {
		t.Error("ReplaceTest followed a linked directory")
	}
}

func TestReplaceTestKeepsThePermissionBitsOfTheFileItReplaces(t *testing.T) {
	for _, mode := range []os.FileMode{0o755, 0o600, 0o644} {
		repo, _ := filepath.EvalSymlinks(t.TempDir())
		dest := filepath.Join(repo, "x_test.go")
		os.WriteFile(dest, []byte("old"), 0o644)
		os.Chmod(dest, mode)
		if err := pathsafe.ReplaceTest(repo, "x_test.go", []byte("package x\n")); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dest)
		if err != nil || fi.Mode().Perm() != mode {
			t.Errorf("mode = %v, want %v (%v)", fi.Mode().Perm(), mode, err)
		}
	}
	// A new file still gets 0644.
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	if err := pathsafe.ReplaceTest(repo, "n_test.go", []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(repo, "n_test.go")); fi.Mode().Perm() != 0o644 {
		t.Errorf("new file mode = %v", fi.Mode().Perm())
	}
}

// A kill between the temp file and the rename leaves the temp file in the
// repository. Its name says what it is, so a resume can tell it apart from a
// person's file (pathsafe.IsReplaceTestTemp).
func TestReplaceTestNamesItsTempFileRecognizably(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	dest := filepath.Join(repo, "pkg", "x_test.go")
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, []byte("old"), 0o644)
	var seen []string
	defer pathsafe.SetBeforeRename(func() {
		entries, _ := os.ReadDir(filepath.Dir(dest))
		for _, e := range entries {
			if e.Name() != "x_test.go" {
				seen = append(seen, "pkg/"+e.Name())
			}
		}
	})()
	if err := pathsafe.ReplaceTest(repo, "pkg/x_test.go", []byte("package pkg\n")); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || !pathsafe.IsReplaceTestTemp(seen[0], "pkg/x_test.go") {
		t.Fatalf("temp files = %v, want one the resume can recognize", seen)
	}
	for _, other := range []string{"pkg/x_test.go", "pkg/.x_test.go.tmp0011", "other/" + strings.TrimPrefix(seen[0], "pkg/"), "pkg/.gm-replace-y_test.go.00"} {
		if pathsafe.IsReplaceTestTemp(other, "pkg/x_test.go") {
			t.Errorf("%s was taken for a temp file of pkg/x_test.go", other)
		}
	}
}
