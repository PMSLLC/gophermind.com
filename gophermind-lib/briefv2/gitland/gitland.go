// Package gitland is the executor's git layer: a work branch, exact-path commits and a
// fast-forward of the base branch. The Repo interface cannot express push, force, reset or rebase.
package gitland

import (
	"errors"
	"fmt"
)

// Repo is the whole surface the executor may use. There is no Push, Force, Rebase or Reset.
type Repo interface {
	// Start verifies base exists. When work does not exist it requires every dirty path to be one of allowDirty
	// (exact repo-relative paths; nil means the tree must be clean) and creates work from base's tip; when it exists
	// it only switches to it (resume: the executor applies the dirt policy).
	Start(baseBranch, workBranch string, allowDirty []string) error
	CommitWave0(paths []string) (string, error)
	CommitLeaf(nodeID, title string, add, remove []string) (string, error)
	CommitRepair(nodeID string, round int, add []string) (string, error)
	// Finish makes the empty final commit on the work branch, then fast-forwards base to it.
	Finish(msg string) (string, error)
	// Dirty lists sorted repo-relative paths: tracked changes and untracked files.
	Dirty() ([]string, error)
	// Diff is the working tree against base, including new files, for diff_only.
	Diff(base string) ([]byte, error)
	// Restore puts tracked paths back to HEAD (index and tree) and removes untracked files.
	Restore(paths []string) error
	Head() (string, error)
	Branch() (string, error)
	// LeafCommit finds the leaf commit for nodeID on the current branch. ok is false when none exists.
	LeafCommit(nodeID string) (hash string, ok bool, err error)
}

var (
	ErrLandingBlocked   = errors.New("gitland: landing_blocked: base branch moved, fast-forward is not possible")
	ErrDirtyTree        = errors.New("gitland: working tree is not clean")
	ErrNothingToCommit  = errors.New("gitland: nothing to commit for the named paths")
	ErrIndexNotClean    = errors.New("gitland: the index holds changes the commit did not name")
	ErrForbiddenGitArgs = errors.New("gitland: refused git arguments")
)

// ValidateLanding is called by the executor before Start. "" is treated as commit.
func ValidateLanding(mode string) error {
	switch mode {
	case "", "diff_only", "commit":
		return nil
	case "pull_request":
		return fmt.Errorf("brief field landing: %q is not supported by this executor", mode)
	}
	return errors.New("brief field landing: value is not supported by this executor")
}
