package executor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/sandbox"
)

func TestSnapshotAndMakeTreeCopyTheLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	write(t, filepath.Join(g.repo, "notes.txt"), "untracked, not ignored\n")
	write(t, filepath.Join(g.repo, ".git", "info", "exclude"), "*.log\n")
	write(t, filepath.Join(g.repo, "build.log"), "ignored\n")
	before, err := g.git.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	commits := g.gitCmd("rev-list", "--count", "HEAD")
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	wt, err := rc.makeTree(leafID)
	if err != nil {
		t.Fatal(err)
	}
	l := rc.plan.Leaf(leafID)
	if wt.Repo != filepath.Join(rc.scratch, "trees", leafID) || wt.Scratch != filepath.Join(rc.scratch, "w", leafID) || !wt.Private || wt.ID != leafID {
		t.Fatalf("tree = %+v", wt)
	}
	for _, rel := range []string{"go.mod", "notes.txt", l.StubFile, l.TestFile, "cmd/greeter/main.go", ".git"} {
		if !fileExists(filepath.Join(wt.Repo, filepath.FromSlash(rel))) {
			t.Errorf("%s is missing from the tree", rel)
		}
	}
	for _, rel := range []string{"build.log", ".gophermind"} {
		if fileExists(filepath.Join(wt.Repo, rel)) {
			t.Errorf("%s was copied", rel)
		}
	}
	if d, err := wt.Git.Dirty(); err != nil || len(d) != 0 {
		t.Fatalf("the tree is dirty after its baseline: %q %v", d, err)
	}
	after, _ := g.git.Dirty()
	if !reflect.DeepEqual(before, after) || g.gitCmd("rev-list", "--count", "HEAD") != commits {
		t.Fatalf("the main tree changed: %q -> %q", before, after)
	}
	rc.dropTree(wt)
	rc.endLevel()
	for _, d := range []string{wt.Repo, wt.Scratch, filepath.Join(rc.scratch, "base")} {
		if fileExists(d) {
			t.Errorf("%s was left behind", d)
		}
	}
}

func TestAPrivateTreeHasItsOwnHomeAndProfile(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree(leafID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	env, err := rc.envAt(wt.Scratch, leafID, envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k, want := range map[string]string{"HOME": wt.Scratch, "TMPDIR": wt.Scratch, "GOPATH": filepath.Join(wt.Scratch, "gopath"), "GOCACHE": rc.goCache, "GOMODCACHE": rc.modCache} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if wt.Chk != Checker(fc) {
		t.Fatal("with the sandbox off a tree must use the run's checker")
	}
	rc.profile = &sandbox.Profile{Repo: g.repo, RunDir: g.runDir, Scratch: rc.scratch, GoCache: rc.goCache, ReadOnly: []string{"/usr/bin"}}
	defer func() { rc.profile = nil }()
	p := rc.treeProfile(wt.Repo, wt.Scratch)
	if p.Repo != wt.Repo || p.Scratch != wt.Scratch || p.RunDir != g.runDir || p.GoCache != rc.goCache || !reflect.DeepEqual(p.ReadOnly, []string{"/usr/bin"}) {
		t.Fatalf("profile = %+v", p)
	}
	p.ReadOnly[0] = "/changed"
	if rc.profile.ReadOnly[0] != "/usr/bin" {
		t.Fatal("the tree profile shares the run's ReadOnly list")
	}
	if _, ok := rc.checkerFor(wt.Repo, wt.Scratch).(*runner.Runner); !ok {
		t.Fatal("with a profile a tree gets its own runner")
	}
}

// Review Focus 1: a link out of the tree, and a tree over the limit, are
// refused before a private tree exists; the errors name no path.
func TestSnapshotLevelRefusesALinkOutOfTheTreeAndAnOversizedTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	link := filepath.Join(g.repo, "hosts-link")
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Fatal(err)
	}
	err := rc.snapshotLevel()
	if err == nil || err.Error() != "executor: a file of the repository cannot be copied into a private tree" {
		t.Fatalf("a link out of the tree: %v", err)
	}
	if fileExists(filepath.Join(rc.scratch, "base")) {
		t.Fatal("a refused base was left behind")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(g.repo, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTreeBytes + 1); err != nil { // sparse: no disk is used
		t.Fatal(err)
	}
	f.Close()
	if err := rc.snapshotLevel(); err == nil || err.Error() != "executor: the tree copy is over 256 MiB" {
		t.Fatalf("an oversized tree: %v", err)
	}
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
	if err := rc.snapshotLevel(); err != nil {
		t.Fatalf("a clean tree: %v", err)
	}
	rc.endLevel()
}

// Review Focus 3: what a killed run leaves (a read-only directory in an old
// tree, a link planted in place of w/) is removed at the next start without
// following the link; the guard refuses anything else.
func TestLeftoverTreesOfAKilledRunAreRemovedWithoutFollowingALink(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	ro := filepath.Join(scratch, "trees", "fn-old", "pkg")
	write(t, filepath.Join(ro, "a.go"), "package pkg\n")
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep.txt"), "keep\n")
	if err := os.Symlink(outside, filepath.Join(scratch, "w")); err != nil {
		t.Fatal(err)
	}
	rc, _ := g.leafRC(t, Script{}, true) // startRun's preflight cleans the scratch directory
	for _, d := range []string{"trees", "w", "base"} {
		if _, err := os.Lstat(filepath.Join(rc.scratch, d)); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", d, err)
		}
	}
	if !fileExists(filepath.Join(outside, "keep.txt")) {
		t.Fatal("the removal followed the link")
	}
	for _, c := range [][2]string{{"trees", "../x"}, {"bin", ""}, {"trees", "Fn"}, {"w", "a/b"}, {"", ""}} {
		if err := rc.removeScratch(c[0], c[1]); err == nil {
			t.Errorf("removeScratch(%q, %q) was allowed", c[0], c[1])
		}
	}
	saved := rc.scratch
	rc.scratch = t.TempDir()
	if err := rc.removeScratch("trees", ""); err == nil {
		t.Error("a scratch directory that is not the run's was accepted")
	}
	rc.scratch = saved
}

func TestTwoTreesAreMadeAtOnce(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	ids := []string{"fn-farewell", "fn-greet"}
	trees := make([]*workTree, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trees[i], errs[i] = rc.makeTree(id)
		}()
	}
	wg.Wait()
	for i := range trees {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		defer rc.dropTree(trees[i])
	}
	if trees[0].Repo == trees[1].Repo || trees[0].Scratch == trees[1].Scratch {
		t.Fatal("two leaves share a tree")
	}
}

// Ruling C6: a resume refused by a live claim leaves the trees, the HOMEs and
// the base copy of the live run in place; once the claim is stale, the
// accepted resume removes them.
func TestARefusedResumeLeavesTheTreesOfTheLiveRun(t *testing.T) {
	t.Parallel()
	g := newRig(t, staleOne)
	g.started(t)
	g.plantDead(t, "fn-greet", true)
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	planted := []string{
		filepath.Join(scratch, "trees", "fn-greet", "go.mod"),
		filepath.Join(scratch, "w", "fn-greet", "gopath", "x"),
		filepath.Join(scratch, "base", "go.mod"),
	}
	for _, p := range planted {
		write(t, p, "x\n")
	}
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		rc.close()
	}
	if err == nil || !strings.Contains(err.Error(), "held by a live worker") {
		t.Fatalf("the resume was not refused: %v", err)
	}
	for _, p := range planted {
		if !fileExists(p) {
			t.Errorf("the refused resume removed %s", strings.TrimPrefix(p, scratch))
		}
	}
	age()
	rc, err = startRun(context.Background(), g.options())
	if rc != nil {
		rc.close()
	}
	if err != nil {
		t.Fatalf("the resume after the claim went stale: %v", err)
	}
	for _, d := range []string{"trees", "w", "base"} {
		if _, err := os.Lstat(filepath.Join(scratch, d)); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the accepted resume: %v", d, err)
		}
	}
}

// Task 4 review: when <scratch>/trees is itself a link, removing one tree must
// not follow it. removeScratch refuses, makeTree fails, and the start-of-run
// cleanup removes the link alone.
func TestRemoveScratchRefusesAKindDirectoryThatIsALink(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	outside := t.TempDir()
	keep := filepath.Join(outside, "fn-greet", "keep.txt")
	write(t, keep, "keep\n")
	link := filepath.Join(rc.scratch, "trees")
	if err := rc.removeScratch("trees", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rc.scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := rc.removeScratch("trees", "fn-greet"); err == nil {
		t.Error("removeScratch went through a link at trees/")
	}
	if wt, err := rc.makeTree("fn-greet"); err == nil {
		rc.dropTree(wt)
		t.Error("makeTree made a tree under a link at trees/")
	}
	if !fileExists(keep) {
		t.Fatal("the removal followed the link")
	}
	if err := rc.cleanTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) || !fileExists(keep) {
		t.Fatalf("cleanTrees: link %v, target kept %v", err, fileExists(keep))
	}
}
