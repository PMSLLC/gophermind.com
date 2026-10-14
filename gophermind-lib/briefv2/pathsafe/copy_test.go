package pathsafe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/pathsafe"
)

func writeFileMode(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileKeepsBytesAndTheExecutableBit(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, "a", "x.go"), "package a\n", 0o644)
	writeFileMode(t, filepath.Join(src, "run.sh"), "#!/bin/sh\n", 0o755)
	for _, rel := range []string{"a/x.go", "run.sh"} {
		if err := pathsafe.CopyFile(src, dst, rel); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a", "x.go")); string(b) != "package a\n" {
		t.Fatalf("content = %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dst, "run.sh")); fi.Mode().Perm()&0o111 == 0 {
		t.Fatal("the executable bit was lost")
	}
	if fi, _ := os.Stat(filepath.Join(dst, "a", "x.go")); fi.Mode().Perm()&0o111 != 0 {
		t.Fatal("a plain file became executable")
	}
	if err := pathsafe.CopyFile(src, dst, "a/x.go"); err == nil {
		t.Fatal("an existing destination was overwritten")
	}
}

func TestCopyFileCopiesALinkInsideTheTreeAndNeverFollowsOne(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, "a", "x.go"), "package a\n", 0o644)
	if err := os.Symlink("x.go", filepath.Join(src, "a", "y.go")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "a/y.go"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(dst, "a", "y.go"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was not copied as a link")
	}
	if target, _ := os.Readlink(filepath.Join(dst, "a", "y.go")); target != "x.go" {
		t.Fatalf("target = %q", target)
	}
	outside := t.TempDir()
	writeFileMode(t, filepath.Join(outside, "secret.txt"), "outside\n", 0o644)
	for name, target := range map[string]string{
		"abs.go": filepath.Join(outside, "secret.txt"),
		"up.go":  "../../" + filepath.Base(outside) + "/secret.txt",
		"git.go": "../.git/config",
	} {
		if err := os.Symlink(target, filepath.Join(src, "a", name)); err != nil {
			t.Fatal(err)
		}
		err := pathsafe.CopyFile(src, dst, "a/"+name)
		if err == nil {
			t.Errorf("a link to %s was copied", target)
			continue
		}
		if strings.Contains(err.Error(), outside) || strings.Contains(err.Error(), "secret") {
			t.Errorf("the error names the path: %v", err)
		}
	}
	// A parent directory that is a link is refused in the source and in the destination.
	if err := os.Symlink(outside, filepath.Join(src, "lnk")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "lnk/secret.txt"); err == nil {
		t.Error("a file under a linked source directory was copied")
	}
	if err := os.Symlink(outside, filepath.Join(dst, "out")); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(src, "out", "f.go"), "package out\n", 0o644)
	if err := pathsafe.CopyFile(src, dst, "out/f.go"); err == nil {
		t.Error("a file was written through a linked destination directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "f.go")); err == nil {
		t.Error("the copy landed outside the tree")
	}
}

func TestCopyFileRefusesUnsafeNamesAndDirectories(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, ".git", "config"), "x", 0o644)
	writeFileMode(t, filepath.Join(src, ".gophermind", "r", "x"), "x", 0o644)
	if err := os.MkdirAll(filepath.Join(src, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".git/config", ".gophermind/r/x", "../x", "/etc/hosts", "a/../b", "", "d"} {
		if err := pathsafe.CopyFile(src, dst, rel); err == nil {
			t.Errorf("CopyFile(%q) was accepted", rel)
		}
	}
}

func TestCopyFileRefusesAChainOfLinksThatEscapesTheTree(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a/b -> . is harmless alone; a/c -> b/../../z passes through it and
	// lands outside the tree although it looks inside lexically.
	if err := os.Symlink(".", filepath.Join(src, "a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b/../../z", filepath.Join(src, "a", "c")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "a/b"); err != nil {
		t.Fatalf("a link to the folder it sits in was refused: %v", err)
	}
	if err := pathsafe.CopyFile(src, dst, "a/c"); err == nil {
		t.Error("a link with .. after a path segment was copied")
	}
	if _, err := os.Lstat(filepath.Join(dst, "a", "c")); err == nil {
		t.Error("the refused link is in the destination tree")
	}
	// A plain relative target in the tree, and a leading ../, stay allowed.
	writeFileMode(t, filepath.Join(src, "x.go"), "package x\n", 0o644)
	if err := os.Symlink("../x.go", filepath.Join(src, "a", "up.go")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "a/up.go"); err != nil {
		t.Errorf("a plain relative link was refused: %v", err)
	}
}
