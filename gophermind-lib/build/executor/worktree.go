package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gophermind/gophermind-lib/build/gitland"
	"gophermind/gophermind-lib/build/pathsafe"
	"gophermind/gophermind-lib/build/runner"
	"gophermind/gophermind-lib/build/sandbox"
)

// workTree is where one leaf reads and writes the repository: the main tree,
// or a private copy of it (sub-project F, spec section 5). The leaf loop reaches
// the repository only through it.
type workTree struct {
	ID      string       // the leaf of a private tree; "" for the main tree
	Repo    string       // absolute root: rc.o.Repo or <scratch>/trees/<id>
	Git     gitland.Repo // rc.git or the tree's own (gitland.NewScratch)
	Chk     Checker      // rc.chk or a runner with the tree's sandbox profile
	Scratch string       // HOME and TMPDIR of the tree's commands: rc.scratch or <scratch>/w/<id>
	Private bool
	cli     *gitland.CLI // the private tree's CLI, closed by dropTree
}

// maxTreeBytes is the copy limit of decision F12: a level whose files are
// larger runs one leaf at a time.
const maxTreeBytes = 256 << 20

// treeIDRe is the shape of a leaf id that may name a scratch directory.
var treeIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// mainTree is the repository itself. It reads rc.chk when called, so a test
// that replaced the checker after startRun is honoured.
func (rc *runCtx) mainTree() *workTree {
	return &workTree{Repo: rc.o.Repo, Git: rc.git, Chk: rc.chk, Scratch: rc.scratch}
}

// snapshotLevel copies the main tree once, before a parallel level starts, into
// <scratch>/base, and keeps the file list for makeTree (ruling 2). It runs in a
// serial phase, so no leaf is landing while it reads. Every error is fixed text
// with no path; the caller then runs the level one leaf at a time.
func (rc *runCtx) snapshotLevel() error {
	if err := rc.cleanTrees(); err != nil {
		return err
	}
	files, err := rc.git.Files()
	if err != nil {
		return errors.New("executor: the repository's file list could not be read")
	}
	var total int64
	for _, f := range files {
		fi, err := os.Lstat(filepath.Join(rc.o.Repo, filepath.FromSlash(f)))
		if err != nil {
			return errors.New("executor: a file of the repository could not be read")
		}
		if total += fi.Size(); total > maxTreeBytes {
			return errors.New("executor: the tree copy is over 256 MiB")
		}
	}
	base := filepath.Join(rc.scratch, "base")
	if err := ensureDir(base); err != nil {
		return err
	}
	for _, f := range files {
		if err := pathsafe.CopyFile(rc.o.Repo, base, f); err != nil {
			rc.endLevel()
			return errors.New("executor: a file of the repository cannot be copied into a private tree")
		}
	}
	rc.baseFiles = files
	return nil
}

// endLevel removes the level's base copy. Safe to call when there is none.
func (rc *runCtx) endLevel() {
	rc.baseFiles = nil
	if err := rc.removeScratch("base", ""); err != nil {
		rc.emit("warning", "", "the base copy of a level could not be removed")
	}
}

// makeTree makes the private tree of one leaf from the level's base copy: the
// files, a throwaway repository with one baseline commit, the leaf's HOME and
// TMPDIR with a GOPATH, and the checker of the tree. A failure leaves nothing.
func (rc *runCtx) makeTree(id string) (*workTree, error) {
	if !treeIDRe.MatchString(id) {
		return nil, errors.New("executor: a leaf id is not a safe directory name")
	}
	fail := func() (*workTree, error) {
		_ = rc.removeScratch("trees", id)
		_ = rc.removeScratch("w", id)
		return nil, fmt.Errorf("executor: the private tree of %s could not be made", id)
	}
	if rc.removeScratch("trees", id) != nil || rc.removeScratch("w", id) != nil {
		return fail()
	}
	repo := filepath.Join(rc.scratch, "trees", id)
	home := filepath.Join(rc.scratch, "w", id)
	for _, d := range []string{filepath.Join(rc.scratch, "trees"), repo, filepath.Join(rc.scratch, "w"), home, filepath.Join(home, "gopath")} {
		if err := ensureDir(d); err != nil {
			return fail()
		}
	}
	base := filepath.Join(rc.scratch, "base")
	for _, f := range rc.baseFiles {
		if err := pathsafe.CopyFile(base, repo, f); err != nil {
			return fail()
		}
	}
	if rc.treeFault != nil && rc.treeFault(id) {
		return fail()
	}
	cli, err := gitland.NewScratch(repo, rc.plan.RunID)
	if err != nil {
		return fail()
	}
	return &workTree{ID: id, Repo: repo, Git: cli, cli: cli, Chk: rc.checkerFor(repo, home), Scratch: home, Private: true}, nil
}

// dropTree closes a private tree's git layer and removes the tree and its HOME.
func (rc *runCtx) dropTree(wt *workTree) {
	if wt == nil || !wt.Private {
		return
	}
	if wt.cli != nil {
		_ = wt.cli.Close()
	}
	for _, kind := range []string{"trees", "w"} {
		if err := rc.removeScratch(kind, wt.ID); err != nil {
			rc.emit("warning", wt.ID, "a private tree could not be removed")
		}
	}
}

// cleanTrees removes what a killed run left of its private trees and base copy:
// derived data only (a candidate lives in _state, decision F10).
func (rc *runCtx) cleanTrees() error {
	for _, kind := range []string{"trees", "w", "base"} {
		if err := rc.removeScratch(kind, ""); err != nil {
			return err
		}
	}
	return nil
}

// removeScratch deletes <scratch>/<kind> or <scratch>/<kind>/<id>, the only
// directories parallel levels make. The target is built and checked before
// anything is deleted: rc.scratch must be absolute, end in -scratch and sit in
// a .gophermind directory; kind must be trees, w or base; id empty or a leaf
// id; the target strictly inside rc.scratch. It is emitted (relative path) and
// only then removed. A link in its place is removed itself, never followed;
// a link (or a file) in place of <scratch>/<kind> refuses the removal of an id.
func (rc *runCtx) removeScratch(kind, id string) error {
	if !filepath.IsAbs(rc.scratch) || !strings.HasSuffix(rc.scratch, "-scratch") || filepath.Base(filepath.Dir(rc.scratch)) != ".gophermind" {
		return errors.New("executor: refusing to delete under a directory that is not the run's scratch directory")
	}
	if kind != "trees" && kind != "w" && kind != "base" {
		return errors.New("executor: refusing to delete an unknown scratch directory")
	}
	if id != "" && !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing to delete a scratch directory with an unsafe name")
	}
	rel := kind
	if id != "" {
		rel = kind + "/" + id
	}
	target := filepath.Join(rc.scratch, filepath.FromSlash(rel))
	if target == "/" || target == rc.home || !strings.HasPrefix(target, rc.scratch+string(filepath.Separator)) {
		return errors.New("executor: refusing to delete a path outside the scratch directory")
	}
	if kd, err := os.Lstat(filepath.Join(rc.scratch, kind)); id != "" && err == nil && (kd.Mode()&os.ModeSymlink != 0 || !kd.IsDir()) {
		return errors.New("executor: refusing to delete under a scratch directory that is a link") // Lstat below would follow it
	}
	fi, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("executor: a scratch directory could not be inspected")
	}
	rc.emit("debug", id, "removing scratch "+rel)
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		if err := os.Remove(target); err != nil {
			return errors.New("executor: a scratch entry could not be removed")
		}
		return nil
	}
	makeWritable(target)
	if err := os.RemoveAll(target); err != nil {
		return errors.New("executor: a scratch directory could not be removed")
	}
	return nil
}

// makeWritable gives the owner write access to every directory under root (a
// tool may leave read-only ones), never through a link.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if d != nil && d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
}

// treeProfile is the run's sandbox profile with the tree as the repository and
// the leaf's directory as HOME and TMPDIR; nil when the sandbox is off. RunDir,
// GoCache, GoModCache, ReadOnly and Home are the run's, so a leaf can write
// neither a sibling's tree nor the main repository.
func (rc *runCtx) treeProfile(repo, scratch string) *sandbox.Profile {
	if rc.profile == nil {
		return nil
	}
	p := *rc.profile
	p.Repo, p.Scratch = repo, scratch
	p.ReadOnly = append([]string(nil), rc.profile.ReadOnly...)
	p.LoopbackPorts = append([]int(nil), rc.profile.LoopbackPorts...)
	return &p
}

// checkerFor is the Checker of a private tree: a runner with the tree's profile
// when the sandbox is on (cheap: no process), else rc.chk, which takes the
// directory from each call; a test's fake Checker therefore stays in place.
func (rc *runCtx) checkerFor(repo, scratch string) Checker {
	p := rc.treeProfile(repo, scratch)
	if p == nil {
		return rc.chk
	}
	return runner.New(runner.Config{Sandbox: p, OutputCap: rc.cfg.Executor.OutputCapBytes, Grace: 2 * time.Second})
}

// leafCheckIn is runner.LeafCheck of the named files of the leaf in a work tree.
func (rc *runCtx) leafCheckIn(ctx context.Context, wt *workTree, l *Leaf, files []string) runner.Verdict {
	env, err := rc.envAt(wt.Scratch, l.ID, envLeaf)
	if err != nil {
		return runner.Verdict{Class: runner.ClassHarness, Err: err}
	}
	return wt.Chk.CheckLeaf(ctx, runner.LeafCheck{
		Repo: wt.Repo, Dir: l.Dir, TestFunc: l.TestFunc, Files: files, Env: env, TestTimeout: rc.testTimeout(),
	})
}
