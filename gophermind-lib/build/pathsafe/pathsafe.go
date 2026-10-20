// Package pathsafe holds the path rules for every file the harness or a leaf
// writes in the target repository. The model never names a path; these checks
// stay in force even for a path the harness derived. Errors never contain the
// path text, only its length.
package pathsafe

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// protectedName reports whether a path component is .git or .gophermind, in
// any case and with trailing dots or spaces, which some file systems ignore.
func protectedName(part string) bool {
	part = strings.TrimRight(part, ". ")
	return strings.EqualFold(part, ".git") || strings.EqualFold(part, ".gophermind")
}

func hasGitName(parts []string) bool {
	for _, p := range parts {
		if protectedName(p) {
			return true
		}
	}
	return false
}

// maxBase keeps the base name plus the temp file suffix under 255 bytes.
const maxBase = 200

var sourceName = regexp.MustCompile(`^[A-Za-z0-9_./-]+\.go$`)

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ResolveTest returns where rel lands inside repo, refusing anything that is
// not a test file strictly inside the repository, including a path that would
// pass through a symbolic link pointing out of it.
func ResolveTest(repo, rel string) (string, error) {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("test file path (%d bytes) is not a clean path inside the repository", len(rel))
	}
	if !strings.HasSuffix(rel, "_test.go") {
		return "", fmt.Errorf("test file path (%d bytes) does not end in _test.go", len(rel))
	}
	return resolve(repo, rel, "test file", false)
}

// ResolveSource is for non-test Go files the harness or a leaf writes.
func ResolveSource(repo, rel string) (string, error) {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("source file path (%d bytes) is not a clean path inside the repository", len(rel))
	}
	if !sourceName.MatchString(rel) {
		return "", fmt.Errorf("source file path (%d bytes) is not an allowed Go source path", len(rel))
	}
	if strings.HasSuffix(rel, "_test.go") {
		return "", fmt.Errorf("source file path (%d bytes) is a test file", len(rel))
	}
	if hasGitName(strings.Split(rel, "/")) {
		return "", fmt.Errorf("source file path (%d bytes) is under .git or .gophermind", len(rel))
	}
	return resolve(repo, rel, "source file", true)
}

// resolve checks that rel really lands inside repo. The deepest existing
// ancestor decides where the file would be written; no existing component may
// be a symbolic link (the final one too when checkFinal).
func resolve(repo, rel, what string, checkFinal bool) (string, error) {
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", errors.New("the repository root cannot be resolved")
	}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	dir := filepath.Dir(abs)
	for !exists(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%s path (%d bytes) cannot be resolved inside the repository", what, len(rel))
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%s path (%d bytes) resolves outside the repository", what, len(rel))
	}
	parts := strings.Split(path.Dir(rel), "/")
	if checkFinal {
		parts = strings.Split(rel, "/")
	}
	cur := repo
	for _, part := range parts {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			break
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s path (%d bytes) passes through a symbolic link", what, len(rel))
		}
	}
	return abs, nil
}

// InsideRepo refuses a directory whose real location is outside the repository.
func InsideRepo(repo, dir string) error {
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return errors.New("the repository root cannot be resolved")
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || (real != root && !strings.HasPrefix(real, root+string(filepath.Separator))) {
		return errors.New("directory resolves outside the repository")
	}
	return nil
}

// osReason is the reason of a file system error without the path it names.
func osReason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "file system error"
}

// Replace writes data to rel atomically: a temp file in the destination's
// directory, fsync, then rename over the destination. It refuses a destination
// that is a symbolic link or a directory and leaves no temp file behind.
func Replace(repo, rel string, data []byte) (err error) {
	return replaceFile(repo, rel, data, ResolveSource, false)
}

// ReplaceTest is Replace for a test file: the path must pass ResolveTest (a
// name that ends in _test.go). The revise stage uses it to put a rewritten test
// over the one Wave 0 committed. It refuses a link or a directory as the
// destination and leaves no temp file behind. The file keeps its permission bits.
// Only a kill between the temp file and the rename leaves the temp file, named
// ReplaceTestTempPrefix+<base>+"."+<hex> beside the test (IsReplaceTestTemp).
func ReplaceTest(repo, rel string, data []byte) (err error) {
	return replaceFile(repo, rel, data, ResolveTest, true)
}

// ReplaceTestTempPrefix starts the name of the temp file of ReplaceTest.
const ReplaceTestTempPrefix = ".gm-replace-"

// IsReplaceTestTemp says the slash path p, relative to the repository, is a
// temp file ReplaceTest makes for the test file testRel: same directory, name
// ReplaceTestTempPrefix + the test's base name + "." + hex digits.
func IsReplaceTestTemp(p, testRel string) bool {
	if path.Dir(p) != path.Dir(testRel) {
		return false
	}
	rest, ok := strings.CutPrefix(path.Base(p), ReplaceTestTempPrefix+path.Base(testRel)+".")
	if !ok || rest == "" {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

func replaceFile(repo, rel string, data []byte, resolveFn func(repo, rel string) (string, error), keepPerm bool) (err error) {
	abs, err := resolveFn(repo, rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if len(filepath.Base(abs)) > maxBase {
		return fmt.Errorf("source file path (%d bytes) has a name that is too long", len(rel))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating the folder for %s: %s", path.Base(rel), osReason(err))
	}
	if _, err := resolveFn(repo, rel); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if fi, lerr := os.Lstat(abs); lerr == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			return fmt.Errorf("source file path (%d bytes) is a symbolic link or a directory", len(rel))
		}
		if keepPerm {
			perm = fi.Mode().Perm() // a replaced test file keeps the bits it had
		}
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return errors.New("the destination directory cannot be resolved")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return errors.New("no random source for a temp file name")
	}
	tmp := filepath.Join(dir, "."+filepath.Base(abs)+".tmp"+hex.EncodeToString(suffix[:]))
	if keepPerm { // ReplaceTest: a name a resume can recognize
		tmp = filepath.Join(dir, ReplaceTestTempPrefix+filepath.Base(abs)+"."+hex.EncodeToString(suffix[:]))
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|NoFollow, perm)
	if err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = InsideRepo(repo, dir); err != nil {
		return err
	}
	if err = f.Chmod(perm); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	beforeRename()
	// A parent swapped for a link since the checks above would make the
	// rename land somewhere else, .git inside the repository included.
	if err = checkParents(repo, rel); err != nil {
		return err
	}
	if err = os.Rename(tmp, abs); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
	}
	afterRename()
	if now, rerr := filepath.EvalSymlinks(filepath.Dir(abs)); rerr != nil || now != realDir {
		// Undo only the file this call created.
		if strings.HasPrefix(abs, repo+string(filepath.Separator)) {
			os.Remove(abs)
		}
		err = fmt.Errorf("source file path (%d bytes) changed while it was written", len(rel))
		return err
	}
	return nil
}

// checkParents refuses a path whose existing directory components are
// symbolic links or protected names, checked one component at a time.
func checkParents(repo, rel string) error {
	cur := repo
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if protectedName(part) {
			return fmt.Errorf("source file path (%d bytes) is under .git or .gophermind", len(rel))
		}
		fi, err := os.Lstat(cur)
		if err != nil {
			return fmt.Errorf("source file path (%d bytes) changed while it was written", len(rel))
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("source file path (%d bytes) passes through a symbolic link", len(rel))
		}
	}
	return nil
}

// Remove deletes rel. A missing file is not an error; a directory is refused.
func Remove(repo, rel string) error {
	abs, err := ResolveSource(repo, rel)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("removing %s: %s", path.Base(rel), osReason(err))
	}
	if fi.IsDir() {
		return fmt.Errorf("source file path (%d bytes) is a directory", len(rel))
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %s", path.Base(rel), osReason(err))
	}
	return nil
}

// CopyFile copies rel from the directory srcRoot to the same place under
// dstRoot, for a private tree (sub-project F). rel is a clean relative slash
// path with no .git or .gophermind component. A regular file is copied with
// its executable bits (mode 0644 or 0755), opened without following a link. A
// symbolic link is copied as a link with the same target text, only when the
// target is relative and stays inside the tree; it is never followed. Anything
// else, a parent directory that is a link in either tree, or an existing
// destination is refused. Errors name the path by its length only.
func CopyFile(srcRoot, dstRoot, rel string) error {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("copied path (%d bytes) is not a clean path inside the tree", len(rel))
	}
	if hasGitName(strings.Split(rel, "/")) {
		return fmt.Errorf("copied path (%d bytes) is under .git or .gophermind", len(rel))
	}
	if err := noLinkedParent(srcRoot, rel); err != nil {
		return err
	}
	src := filepath.Join(srcRoot, filepath.FromSlash(rel))
	dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
	fi, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
	}
	if err := noLinkedParent(dstRoot, rel); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
	}
	if err := noLinkedParent(dstRoot, rel); err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		joined := path.Clean(path.Join(path.Dir(rel), filepath.ToSlash(target)))
		if filepath.IsAbs(target) || dotDotAfterSegment(target) || joined == ".." || strings.HasPrefix(joined, "../") || hasGitName(strings.Split(joined, "/")) {
			return fmt.Errorf("a symbolic link (%d bytes of path) points outside the tree", len(rel))
		}
		if err := os.Symlink(target, dst); err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		return nil
	case fi.Mode().IsRegular():
		perm := os.FileMode(0o644)
		if fi.Mode().Perm()&0o111 != 0 {
			perm = 0o755
		}
		in, err := os.OpenFile(src, os.O_RDONLY|NoFollow, 0)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|NoFollow, perm)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dst)
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		if err := out.Chmod(perm); err != nil {
			out.Close()
			os.Remove(dst)
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		return out.Close()
	}
	return fmt.Errorf("copied path (%d bytes) is not a regular file or a link", len(rel))
}

// dotDotAfterSegment reports a link target with a .. component after a named
// segment. The lexical check cannot see where such a target lands when the
// segment is itself a link (a/b -> . with a/c -> b/../../z), so it is refused.
// Leading .. components stay allowed: they walk real directories.
func dotDotAfterSegment(target string) bool {
	seen := false
	for _, part := range strings.Split(filepath.ToSlash(target), "/") {
		switch part {
		case "", ".":
		case "..":
			if seen {
				return true
			}
		default:
			seen = true
		}
	}
	return false
}

// noLinkedParent refuses rel when an existing directory on its way under root
// is a symbolic link.
func noLinkedParent(root, rel string) error {
	cur := root
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil // not there yet
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copied path (%d bytes) passes through a symbolic link", len(rel))
		}
	}
	return nil
}

var beforeRename = func() {}
var afterRename = func() {}
