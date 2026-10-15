package runview

import (
	"io"
	"os"
	"path/filepath"
	"regexp"

	"gophermind/gophermind-lib/briefv2/planner"
)

// nodeIDRE is the node schema's id pattern.
var nodeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// artifactFiles are the only files an attempt folder serves.
var artifactFiles = []string{"reply.go", "reply.txt", "check-output.txt", "attempt.json"}

// maxReadBytes caps any single file read for a response.
const maxReadBytes = 4 << 20

// ValidRunID reports whether s is a run id.
func ValidRunID(s string) bool { return planner.ValidRunID(s) }

// ValidNodeID reports whether s is a node id.
func ValidNodeID(s string) bool { return nodeIDRE.MatchString(s) }

// ValidArtifactName reports whether s is one of the four servable artifact files.
func ValidArtifactName(s string) bool {
	for _, n := range artifactFiles {
		if s == n {
			return true
		}
	}
	return false
}

// realDir reports whether path is a directory and not a symbolic link.
func realDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// realFile reports whether path is a regular file and not a symbolic link.
func realFile(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// runDirOf maps a run id to <root>/.gophermind/<id>. It refuses a bad id
// (ErrBadID) and a missing run, a symlinked .gophermind or run folder, or a
// folder without brief.md (ErrNotFound).
func runDirOf(root, runID string) (string, error) {
	if !ValidRunID(runID) {
		return "", ErrBadID
	}
	base := filepath.Join(root, ".gophermind")
	dir := filepath.Join(base, runID)
	if !realDir(base) || !realDir(dir) || !realFile(filepath.Join(dir, "brief.md")) {
		return "", ErrNotFound
	}
	return dir, nil
}

// readLimited reads a regular file (never a link) up to max bytes and says
// whether it cut the file short.
func readLimited(path string, max int64) (data []byte, truncated bool, err error) {
	if !realFile(path) {
		return nil, false, ErrNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, ErrNotFound
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > max {
		return data[:max], true, nil
	}
	return data, false, nil
}
