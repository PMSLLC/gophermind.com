package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/pathsafe"
)

// SwapState is what the disk holds for one leaf.
type SwapState int

const (
	SwapStubOnly SwapState = iota
	SwapRealOnly
	SwapBoth
	SwapNeither
)

func (s SwapState) String() string {
	switch s {
	case SwapStubOnly:
		return "stub only"
	case SwapRealOnly:
		return "real only"
	case SwapBoth:
		return "both"
	}
	return "neither"
}

// Swap is the stub/real file swap of spec 8.1 for one leaf's attempt
// sequence. It has no path parameter: the only files it writes or removes are
// the leaf's own File and StubFile, every write goes through pathsafe (atomic
// temp file and rename, so a half-written real file never exists), and the
// only commit it makes is of those two paths.
type Swap struct {
	repo    string
	leaf    *Leaf
	git     gitland.Repo
	stubSrc []byte
	reopen  bool
	passed  bool
	entered bool // Enter has been called at least once: only then may Fail remove anything

	// diff_only commits nothing, so a failed repair cannot restore the file from
	// git: the swap keeps the last verified content in memory instead.
	// The prior is also saved under <run>/_state/prior-<id> before the first
	// write of a repair, so a crash that leaves the candidate on disk cannot
	// lose the verified file; restorePriors (resume) reads it back.
	noCommit bool
	runDir   string
	prior    []byte
	hasPrior bool

	// In repair mode with commits, the marker _state/reopen-<id> is written
	// before the first candidate reaches the disk and removed once the repair
	// is committed or undone (Track sets what it records).
	track    bool
	round    int
	revision int
}

func NewSwap(repo string, l *Leaf, git gitland.Repo, stubSrc []byte) *Swap {
	return &Swap{repo: repo, leaf: l, git: git, stubSrc: stubSrc}
}

// Enter removes the stub, then writes the real file. When the write fails the
// stub is written back before the error is returned.
func (s *Swap) Enter(source []byte) error {
	s.passed = false
	s.entered = true
	if s.reopen && s.noCommit && !s.hasPrior {
		abs, err := pathsafe.ResolveSource(s.repo, s.leaf.File)
		if err != nil {
			return err
		}
		name := priorPrefix + s.leaf.ID
		// A saved prior that is already there is the verified file of an
		// earlier, cut-off repair; the file on disk may be its candidate.
		prior, found, err := readStateBytes(s.runDir, name)
		if err != nil {
			return err
		}
		if !found {
			if prior, err = os.ReadFile(abs); err != nil {
				return fmt.Errorf("executor: leaf %s: the verified file cannot be read before a repair", s.leaf.ID)
			}
			if err := writeStateBytes(s.runDir, name, prior); err != nil {
				return err
			}
		}
		s.prior, s.hasPrior = prior, true
	}
	if s.reopen && !s.noCommit && s.track {
		if err := writeReopenMarker(s.runDir, reopenMarker{Leaf: s.leaf.ID, Round: s.round, Revision: s.revision}); err != nil {
			return err
		}
	}
	if !s.reopen {
		if err := pathsafe.Remove(s.repo, s.leaf.StubFile); err != nil {
			return err
		}
	}
	if err := pathsafe.Replace(s.repo, s.leaf.File, source); err != nil {
		if !s.reopen {
			if rerr := pathsafe.Replace(s.repo, s.leaf.StubFile, s.stubSrc); rerr != nil {
				return errors.Join(err, rerr)
			}
		}
		return err
	}
	return nil
}

// Fail removes the real file and puts the stub back; both steps run even when
// the first fails, and it is idempotent. In reopen mode the file is restored
// from git instead and no stub is written. After a pass it does nothing.
func (s *Swap) Fail() error {
	if s.passed {
		return nil
	}
	if !s.entered {
		// Nothing of this swap is on disk to undo; the real file may be a committed one.
		return fmt.Errorf("executor: leaf %s: Fail before Enter", s.leaf.ID)
	}
	if s.reopen {
		if s.noCommit {
			if !s.hasPrior {
				return nil
			}
			if err := pathsafe.Replace(s.repo, s.leaf.File, s.prior); err != nil {
				return err
			}
			return removeStatePrior(s.runDir, priorPrefix+s.leaf.ID)
		}
		if err := s.git.Restore([]string{s.leaf.File}); err != nil {
			return err
		}
		if s.track {
			return removeReopenMarker(s.runDir, s.leaf.ID)
		}
		return nil
	}
	err1 := pathsafe.Remove(s.repo, s.leaf.File)
	err2 := pathsafe.Replace(s.repo, s.leaf.StubFile, s.stubSrc)
	return errors.Join(err1, err2)
}

// NoCommit is diff_only mode: nothing is committed, and a failed repair puts
// back the content the file had when the repair began. runDir is where that
// content is saved (<runDir>/_state) before the repair writes.
func (s *Swap) NoCommit(runDir string) { s.noCommit, s.runDir = true, runDir }

// PassNoCommit is Pass for diff_only: the real file stays on disk, the stub is
// gone, and no commit is made.
func (s *Swap) PassNoCommit() error {
	if err := s.ready(); err != nil {
		return err
	}
	s.passed = true
	if s.hasPrior {
		return removeStatePrior(s.runDir, priorPrefix+s.leaf.ID)
	}
	return nil
}

// Track makes a repair leave a marker in <runDir>/_state while a candidate may
// be on disk (round and revision are what it records).
func (s *Swap) Track(runDir string, round, revision int) {
	s.track, s.runDir, s.round, s.revision = true, runDir, round, revision
}

// Reopen is repair mode: the stub is never restored and Fail restores File
// from git.
func (s *Swap) Reopen() { s.reopen = true }

// ready refuses a commit unless the real file is on disk and the stub is not.
func (s *Swap) ready() error {
	st, err := s.State()
	if err != nil {
		return err
	}
	if st != SwapRealOnly {
		return fmt.Errorf("executor: leaf %s is not in the real-file state (%s)", s.leaf.ID, st)
	}
	return nil
}

// Pass commits the leaf: the real file added and the stub removed in one
// commit. On an error it does nothing else; the caller's deferred Fail
// restores the tree. It returns the short hash.
func (s *Swap) Pass(title string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	h, err := s.git.CommitLeaf(s.leaf.ID, title, []string{s.leaf.File}, []string{s.leaf.StubFile})
	if err != nil {
		return "", err
	}
	s.passed = true
	return h, nil
}

// PassRepair commits a repair of an already committed leaf (repair mode).
func (s *Swap) PassRepair(round int) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	h, err := s.git.CommitRepair(s.leaf.ID, round, []string{s.leaf.File})
	if err != nil {
		return "", err
	}
	s.passed = true
	if s.track {
		return h, removeReopenMarker(s.runDir, s.leaf.ID)
	}
	return h, nil
}

// kind reports whether rel exists. A link, a directory or anything but a
// regular file is an error: it is not a state the swap can be in.
func (s *Swap) kind(rel string) (bool, error) {
	abs, err := pathsafe.ResolveSource(s.repo, rel)
	if err != nil {
		return false, err
	}
	fi, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("executor: leaf %s: cannot inspect %s", s.leaf.ID, filepath.Base(abs))
	}
	if !fi.Mode().IsRegular() {
		return false, fmt.Errorf("executor: leaf %s: %s is not a regular file", s.leaf.ID, filepath.Base(abs))
	}
	return true, nil
}

// State reports what the disk holds.
func (s *Swap) State() (SwapState, error) {
	stub, err := s.kind(s.leaf.StubFile)
	if err != nil {
		return SwapNeither, err
	}
	real, err := s.kind(s.leaf.File)
	if err != nil {
		return SwapNeither, err
	}
	switch {
	case stub && real:
		return SwapBoth, nil
	case stub:
		return SwapStubOnly, nil
	case real:
		return SwapRealOnly, nil
	}
	return SwapNeither, nil
}

// Normalize is the resume step of spec 8.1: a leaf with both files has its stub
// removed, one with neither gets the stub, one with only one is left alone.
func (s *Swap) Normalize() error {
	st, err := s.State()
	if err != nil {
		return err
	}
	switch st {
	case SwapBoth:
		return pathsafe.Remove(s.repo, s.leaf.StubFile)
	case SwapNeither:
		return pathsafe.Replace(s.repo, s.leaf.StubFile, s.stubSrc)
	}
	return nil
}
