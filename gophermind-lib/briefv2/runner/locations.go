package runner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Location is a file position; File is repo-relative with forward slashes. It
// never carries message text.
type Location struct {
	File      string
	Line, Col int
}

var (
	diagRE  = regexp.MustCompile(`^(?:vet: )?([^\s:][^:]*\.go):(\d+)(?::(\d+))?:`)
	stackRE = regexp.MustCompile(`^\s+(/\S*\.go):(\d+)(?: \+0x[0-9a-fA-F]+)?\s*$`)
)

// ParseLocations extracts compiler, vet and panic-stack positions, deduplicated
// and in order. Absolute paths under repo are made repo-relative and other
// absolute paths (GOROOT, module cache) are dropped, as is anything that escapes repo.
func ParseLocations(repo, text string) []Location {
	roots := []string{filepath.Clean(repo)}
	if real, err := filepath.EvalSymlinks(repo); err == nil && real != roots[0] {
		roots = append(roots, real)
	}
	var out []Location
	seen := map[Location]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		var file, ln, col string
		if m := diagRE.FindStringSubmatch(line); m != nil {
			file, ln, col = m[1], m[2], m[3]
		} else if m := stackRE.FindStringSubmatch(line); m != nil {
			file, ln = m[1], m[2]
		} else {
			continue
		}
		rel, ok := relToRepo(roots, file)
		if !ok {
			continue
		}
		l, _ := strconv.Atoi(ln)
		c, _ := strconv.Atoi(col)
		loc := Location{File: rel, Line: l, Col: c}
		if !seen[loc] {
			seen[loc] = true
			out = append(out, loc)
		}
	}
	return out
}

func relToRepo(roots []string, file string) (string, bool) {
	if !filepath.IsAbs(file) {
		rel := filepath.Clean(file)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	for _, root := range roots {
		rel, err := filepath.Rel(root, file)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.ToSlash(rel), true
		}
	}
	return "", false
}
