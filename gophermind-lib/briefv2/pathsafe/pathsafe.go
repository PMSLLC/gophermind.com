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
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

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
	first, _, _ := strings.Cut(rel, "/")
	if first == ".git" || first == ".gophermind" {
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
		return "", err
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
		return "", err
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
		return errors.New("test file directory resolves outside the repository")
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
	abs, err := ResolveSource(repo, rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating the folder for %s: %s", path.Base(rel), osReason(err))
	}
	if _, err := ResolveSource(repo, rel); err != nil {
		return err
	}
	if fi, lerr := os.Lstat(abs); lerr == nil && (fi.Mode()&os.ModeSymlink != 0 || fi.IsDir()) {
		return fmt.Errorf("source file path (%d bytes) is a symbolic link or a directory", len(rel))
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return errors.New("no random source for a temp file name")
	}
	tmp := filepath.Join(dir, "."+filepath.Base(abs)+".tmp"+hex.EncodeToString(suffix[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|NoFollow, 0o644)
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
	if err = f.Chmod(0o644); err != nil {
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
	if err = os.Rename(tmp, abs); err != nil {
		return fmt.Errorf("writing %s: %s", path.Base(rel), osReason(err))
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
