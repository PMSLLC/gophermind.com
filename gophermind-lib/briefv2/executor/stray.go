package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/pathsafe"
)

// Snapshot maps a dirty or ignored path to the SHA-256 of its bytes: "" for a
// file that is deleted, "unreadable" for one that cannot be read, "dir" for a directory and "link:" plus the hash of
// the target text for a symbolic link (the link is never followed).
type Snapshot map[string]string

// TakeSnapshot records every path git reports as dirty (tracked changes and
// untracked files, found one by one inside new directories). Git reads the
// tree through gitland's runner, which neutralises the repository's own
// configuration and hooks.
func TakeSnapshot(repo string, g gitland.Repo) (Snapshot, error) {
	dirty, err := g.Dirty()
	if err != nil {
		return nil, errors.New("executor: cannot read the working tree status")
	}
	ignored, err := g.Ignored()
	if err != nil {
		return nil, errors.New("executor: cannot read the ignored files")
	}
	s := Snapshot{}
	for _, p := range append(dirty, ignored...) {
		s[p] = hashPath(repo, p)
	}
	return s, nil
}

func sumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashPath hashes one repo-relative path without following a link.
func hashPath(repo, rel string) string {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "unreadable"
	}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	fi, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		return "unreadable"
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(abs)
		if err != nil {
			return "unreadable"
		}
		return "link:" + sumHex([]byte(target))
	case fi.IsDir():
		return "dir"
	case !fi.Mode().IsRegular():
		return "special"
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|pathsafe.NoFollow, 0)
	if err != nil {
		return "unreadable"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unreadable"
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Stray returns the paths dirty now that are not in before, or whose content
// hash changed since before, excluding allowed. The result is sorted. A path
// that was dirty before and is unchanged is not stray; one that is clean now
// was put back and is not stray either.
func (s Snapshot) Stray(repo string, g gitland.Repo, allowed ...string) ([]string, error) {
	now, err := TakeSnapshot(repo, g)
	if err != nil {
		return nil, err
	}
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var out []string
	for p, h := range now {
		if ok[p] {
			continue
		}
		if was, had := s[p]; had && was == h {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Revert puts tracked paths back to HEAD and removes untracked ones.
func Revert(g gitland.Repo, paths []string) error { return g.Restore(paths) }
