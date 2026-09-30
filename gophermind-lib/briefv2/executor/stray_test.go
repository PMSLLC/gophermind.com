package executor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStrayFileFailsAttempt(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	testFile := g.plan.Leaf("fn-greet").TestFile
	snap, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	write(t, g.abs("notes.txt"), "stray\n")
	write(t, g.abs("cmd/greeter/main.go"), g.read("cmd/greeter/main.go")+"\n// tampered\n")
	write(t, g.abs(testFile), g.read(testFile)+"\n// tampered\n")

	got, err := snap.Stray(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/greeter/main.go", testFile, "notes.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stray = %v, want %v", got, want)
	}

	got, _ = snap.Stray(g.repo, g.git, "notes.txt")
	if !reflect.DeepEqual(got, []string{"cmd/greeter/main.go", testFile}) {
		t.Fatalf("allowed path still stray: %v", got)
	}

	if err := Revert(g.git, want); err != nil {
		t.Fatal(err)
	}
	if fileExists(g.abs("notes.txt")) || strings.Contains(g.read("cmd/greeter/main.go"), "tampered") || strings.Contains(g.read(testFile), "tampered") {
		t.Fatal("Revert left the stray changes")
	}
	if d, _ := g.git.Dirty(); len(d) != 0 {
		t.Fatalf("dirty after Revert: %v", d)
	}
}

func TestStrayIgnoresAlreadyDirtyUnlessChanged(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	write(t, g.abs("scratch.txt"), "v1\n")
	snap, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := snap.Stray(g.repo, g.git); len(got) != 0 {
		t.Fatalf("a file dirty before and unchanged is stray: %v", got)
	}
	write(t, g.abs("scratch.txt"), "v2\n")
	if got, _ := snap.Stray(g.repo, g.git); !reflect.DeepEqual(got, []string{"scratch.txt"}) {
		t.Fatalf("a changed file is not stray: %v", got)
	}
	// Reverting a file that was dirty before is not stray.
	os.Remove(g.abs("scratch.txt"))
	if got, _ := snap.Stray(g.repo, g.git); len(got) != 0 {
		t.Fatalf("a file that vanished is stray: %v", got)
	}
}

func TestStrayCatchesNewDirectoriesSymlinksAndDeletes(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	snap, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	write(t, g.abs("newdir/deep/x.go"), "package x\n")
	if err := os.Symlink("/etc", g.abs("link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(g.abs("cmd/greeter/main.go")); err != nil {
		t.Fatal(err)
	}
	got, err := snap.Stray(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/greeter/main.go", "link", "newdir/deep/x.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stray = %v, want %v", got, want)
	}
	if err := Revert(g.git, got); err != nil {
		t.Fatal(err)
	}
	if fileExists(g.abs("link")) {
		if _, err := os.Lstat(g.abs("link")); err == nil {
			t.Fatal("Revert left the symlink")
		}
	}
	if d, _ := g.git.Dirty(); len(d) != 0 {
		t.Fatalf("dirty after Revert: %v", d)
	}
}

func TestSnapshotHashesSymlinkWithoutFollowing(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	secret := filepath.Join(t.TempDir(), "s")
	write(t, secret, "one")
	if err := os.Symlink(secret, g.abs("lnk")); err != nil {
		t.Fatal(err)
	}
	a, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	write(t, secret, "two")
	if got, _ := a.Stray(g.repo, g.git); len(got) != 0 {
		t.Fatalf("a change behind a symlink (not followed) is stray: %v", got)
	}
	os.Remove(g.abs("lnk"))
	if err := os.Symlink(filepath.Join(t.TempDir(), "other"), g.abs("lnk")); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Stray(g.repo, g.git); !reflect.DeepEqual(got, []string{"lnk"}) {
		t.Fatalf("a retargeted symlink is not stray: %v", got)
	}
}

func TestStrayCatchesIgnoredFiles(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	write(t, filepath.Join(g.repo, ".git", "info", "exclude"), "*.log\nbuild/\n")
	write(t, g.abs("pre.log"), "v1\n")
	snap, err := TakeSnapshot(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap["pre.log"]; !ok {
		t.Fatalf("the snapshot missed an ignored file: %v", snap)
	}
	write(t, g.abs("out.log"), "x\n")
	write(t, g.abs("build/x.bin"), "x\n")
	write(t, g.abs(".gophermind/run/z"), "x\n")
	got, err := snap.Stray(g.repo, g.git)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"build/x.bin", "out.log"}) {
		t.Fatalf("Stray = %v, want the two ignored files and not the run folder", got)
	}
	got, _ = snap.Stray(g.repo, g.git, "out.log", "build/x.bin")
	if len(got) != 0 {
		t.Fatalf("declared ignored files are stray: %v", got)
	}
}

func TestUnreadableFileIsNotADeletedFile(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.startWave0()
	write(t, g.abs("a.txt"), "secret\n")
	if err := os.Chmod(g.abs("a.txt"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(g.abs("a.txt"), 0o644)
	unreadable := hashPath(g.repo, "a.txt")
	os.Chmod(g.abs("a.txt"), 0o644)
	readable := hashPath(g.repo, "a.txt")
	os.Remove(g.abs("a.txt"))
	deleted := hashPath(g.repo, "a.txt")
	if unreadable == deleted || unreadable == readable || deleted == readable {
		t.Fatalf("hashes collide: unreadable=%q deleted=%q readable=%q", unreadable, deleted, readable)
	}
	// A dirty file that turns unreadable is stray.
	write(t, g.abs("b.txt"), "v\n")
	snap, _ := TakeSnapshot(g.repo, g.git)
	os.Chmod(g.abs("b.txt"), 0)
	defer os.Chmod(g.abs("b.txt"), 0o644)
	if got, _ := snap.Stray(g.repo, g.git); !reflect.DeepEqual(got, []string{"b.txt"}) {
		t.Fatalf("a file that became unreadable is not stray: %v", got)
	}
}
