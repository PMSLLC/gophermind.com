package pathsafe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/pathsafe"
)

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestResolveTestTable(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "internal", "escape")); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}

	good := []string{"a_test.go", "internal/real/x_test.go", "internal/not/yet/made/x_test.go"}
	for _, rel := range good {
		abs, err := pathsafe.ResolveTest(repo, rel)
		if err != nil || abs != filepath.Join(repo, filepath.FromSlash(rel)) {
			t.Errorf("ResolveTest(%q) = %q, %v", rel, abs, err)
		}
	}
	if exists(filepath.Join(repo, "internal", "not")) {
		t.Error("ResolveTest created a directory; it must only answer")
	}
	bad := map[string]string{
		"../x_test.go":                     "not a clean path",
		"internal/../../x_test.go":         "not a clean path",
		"/etc/x_test.go":                   "not a clean path",
		`internal\x_test.go`:               "not a clean path",
		"":                                 "not a clean path",
		"internal/real/x.go":               "does not end in _test.go",
		"internal/escape/x_test.go":        "resolves outside the repository",
		"internal/escape/deeper/x_test.go": "resolves outside the repository",
	}
	for rel, want := range bad {
		if _, err := pathsafe.ResolveTest(repo, rel); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ResolveTest(%q) = %v, want an error containing %q", rel, err, want)
		}
	}
}

func TestResolveTestRefusesSymlinkedParent(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(repo, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := pathsafe.ResolveTest(repo, "link/x_test.go"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("err = %v", err)
	}
}

func TestPathsafeTable(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	for _, d := range []string{"internal/greet", "real"} {
		if err := os.MkdirAll(filepath.Join(repo, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "real", "f.go"), []byte("package real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "out")); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}
	if err := os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, "back")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "real", "f.go"), filepath.Join(repo, "internal", "greet", "link.go")); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"internal/greet/greet.go", "a/b/c.go", "main.go"} {
		abs, err := pathsafe.ResolveSource(repo, rel)
		if err != nil || abs != filepath.Join(repo, filepath.FromSlash(rel)) {
			t.Errorf("ResolveSource(%q) = %q, %v", rel, abs, err)
		}
	}
	if exists(filepath.Join(repo, "a")) {
		t.Error("ResolveSource created a directory; it must only answer")
	}

	bad := []struct{ rel, want string }{
		{"", "is not a clean path inside the repository"},
		{"/etc/x.go", "is not a clean path inside the repository"},
		{"../x.go", "is not a clean path inside the repository"},
		{"a/../../x.go", "is not a clean path inside the repository"},
		{"a//b.go", "is not a clean path inside the repository"},
		{"./a.go", "is not a clean path inside the repository"},
		{`a\b.go`, "is not a clean path inside the repository"},
		{"a b.go", "is not an allowed Go source path"},
		{"a.txt", "is not an allowed Go source path"},
		{"a\x00b.go", "is not an allowed Go source path"},
		{"a_test.go", "is a test file"},
		{"internal/greet/x_test.go", "is a test file"},
		{".git/x.go", "is under .git or .gophermind"},
		{".gophermind/run/x.go", "is under .git or .gophermind"},
		{".git", "is not an allowed Go source path"},
		{"out/x.go", "resolves outside the repository"},
		{"out/deeper/x.go", "resolves outside the repository"},
		{"back/x.go", "passes through a symbolic link"},
		{"back/f.go", "passes through a symbolic link"},
		{"internal/greet/link.go", "passes through a symbolic link"},
	}
	const canary = "CANARYPATHTEXT"
	for _, c := range bad {
		_, err := pathsafe.ResolveSource(repo, c.rel)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "source file path (") {
			t.Errorf("ResolveSource(%q) = %v, want %q", c.rel, err, c.want)
		}
	}
	for _, rel := range []string{"../" + canary + ".go", canary + ".txt", ".git/" + canary + ".go", canary + "_test.go", "out/" + canary + ".go"} {
		if _, err := pathsafe.ResolveSource(repo, rel); err == nil || strings.Contains(err.Error(), canary) {
			t.Errorf("ResolveSource(%q) = %v", rel, err)
		}
	}
}

func TestReplaceIsAtomicAndClean(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	dest := filepath.Join(repo, "pkg", "x.go")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.Replace(repo, "pkg/x.go", []byte("package pkg\n")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "package pkg\n" {
		t.Errorf("content = %q", got)
	}
	fi, _ := os.Stat(dest)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	// A new nested directory is made.
	if err := pathsafe.Replace(repo, "n/e/w.go", []byte("package w\n")); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(repo, "n", "e", "w.go")) {
		t.Error("nested file missing")
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	dir := filepath.Dir(dest)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := pathsafe.Replace(repo, "pkg/x.go", []byte("new")); err == nil {
		t.Fatal("write into a read-only directory succeeded")
	}
	os.Chmod(dir, 0o755)
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if got, _ := os.ReadFile(dest); string(got) != "package pkg\n" {
		t.Errorf("failed replace changed the file: %q", got)
	}
}

func TestReplaceRefusesSymlinkDestination(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	outside := filepath.Join(t.TempDir(), "victim.go")
	if err := os.WriteFile(outside, []byte("victim"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "x.go")); err != nil {
		t.Skip(err)
	}
	if err := pathsafe.Replace(repo, "x.go", []byte("evil")); err == nil {
		t.Fatal("symlink destination accepted")
	}
	if got, _ := os.ReadFile(outside); string(got) != "victim" {
		t.Errorf("victim changed: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(repo, "d.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.Replace(repo, "d.go", []byte("x")); err == nil {
		t.Error("directory destination accepted")
	}
	ents, _ := os.ReadDir(repo)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestRemoveMissingIsNil(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	if err := pathsafe.Remove(repo, "nope/x.go"); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.Remove(repo, "x.go"); err != nil || exists(filepath.Join(repo, "x.go")) {
		t.Errorf("remove: %v", err)
	}
}

func TestRemoveRefusesTestFile(t *testing.T) {
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(repo, "x_test.go")
	if err := os.WriteFile(p, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.Remove(repo, "x_test.go"); err == nil || !exists(p) {
		t.Errorf("err = %v, exists = %v", err, exists(p))
	}
}
