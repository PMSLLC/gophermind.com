// Package rundir creates the .gophermind/<brief-id>/ layout inside a target
// repository and keeps it out of git via .git/info/exclude (never the repo's
// tracked .gitignore).
package rundir

import (
	"errors"
	"fmt"
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
	dir := filepath.Join(repoRoot, ".gophermind", briefID)
	if _, err := os.Stat(dir); err == nil {
		return "", ErrExists
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), briefSrc, 0o600); err != nil {
		return "", err
	}
	if err := exclude(repoRoot); err != nil {
		return "", err
	}
	return dir, nil
}

func exclude(repoRoot string) error {
	info := filepath.Join(repoRoot, ".git", "info")
	if fi, err := os.Stat(info); err != nil || !fi.IsDir() {
		return nil
	}
	p := filepath.Join(info, "exclude")
	cur, _ := os.ReadFile(p)
	if strings.Contains(string(cur), ".gophermind/") {
		return nil
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
