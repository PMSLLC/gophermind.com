// Package rundir creates the .gophermind/<brief-id>/ layout inside a target
// repository and keeps it out of git via .git/info/exclude (never the repo's
// tracked .gitignore).
package rundir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrExists = errors.New("rundir: run directory already exists")

var idRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

// Create makes <repoRoot>/.gophermind/<briefID>/ with logs/ and a copy of the
// brief. It refuses to reuse an existing run directory.
func Create(repoRoot, briefID string, briefSrc []byte) (string, error) {
	if !idRE.MatchString(briefID) {
		return "", fmt.Errorf("rundir: invalid brief id %q", briefID)
	}
	if err := exclude(repoRoot); err != nil {
		return "", err
	}
	base := filepath.Join(repoRoot, ".gophermind")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	dir := filepath.Join(base, briefID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrExists
		}
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), briefSrc, 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

// excludeFile resolves the info/exclude path for repoRoot, following a
// worktree's .git file to the common git dir. ok is false when repoRoot is
// not a git repository at all.
func excludeFile(repoRoot string) (path string, ok bool, err error) {
	dotgit := filepath.Join(repoRoot, ".git")
	fi, err := os.Stat(dotgit)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	gitDir := dotgit
	if !fi.IsDir() {
		raw, err := os.ReadFile(dotgit)
		if err != nil {
			return "", false, err
		}
		line := strings.TrimSpace(string(raw))
		rest, found := strings.CutPrefix(line, "gitdir:")
		rest = strings.TrimSpace(rest)
		if !found || rest == "" {
			return "", false, fmt.Errorf("rundir: cannot parse %s (want a gitdir: line)", dotgit)
		}
		gitDir = rest
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(repoRoot, gitDir)
		}
		if cd, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
			common := strings.TrimSpace(string(cd))
			if common != "" {
				if !filepath.IsAbs(common) {
					common = filepath.Join(gitDir, common)
				}
				gitDir = common
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", false, err
		}
	}
	return filepath.Join(gitDir, "info", "exclude"), true, nil
}

func exclude(repoRoot string) error {
	p, ok, err := excludeFile(repoRoot)
	if err != nil || !ok {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	cur, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(cur), "\n") {
		if strings.TrimRight(l, "\r") == ".gophermind/" {
			return nil
		}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(cur) > 0 && !strings.HasSuffix(string(cur), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + ".gophermind/\n")
	return err
}
