package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/build/router"
)

// candidatePrefix names the saved private pass of a leaf: _state/candidate-<id>
// (decision F10).
const candidatePrefix = "candidate-"

// candidate is a reply that passed its leaf check in a private tree, saved
// before the leaf waits for its turn, so a kill between the pass and the
// landing costs no model call. It holds gate-passed source and the ladder entry
// that wrote it: no prompt, no check output, no secret (the reply gate refuses
// a reply that holds one, and landing runs the gate again).
type candidate struct {
	Revision int    `json:"revision"` // the row revision the reply was written for
	Entry    string `json:"entry"`    // "provider/model"
	Tier     string `json:"tier"`
	Pos      int    `json:"pos"`
	SHA256   string `json:"reply_sha256"`
	Source   string `json:"source"`
}

// entry is the ladder entry the pass attempt is recorded against at landing.
func (c candidate) entry() ladderEntry {
	return ladderEntry{Name: c.Entry, Tier: router.Tier(c.Tier), Pos: c.Pos}
}

func saveCandidate(runDir, id string, c candidate) error {
	if !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing a candidate for an unsafe leaf id")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("executor: the candidate of %s could not be encoded", id)
	}
	return writeStateBytes(runDir, candidatePrefix+id, raw)
}

// readCandidate returns the saved candidate of the leaf. A file that does not
// parse, or does not hold what a saved pass holds, is removed and read as
// absent: it is derived data (ruling 10).
func readCandidate(runDir, id string) (candidate, bool, error) {
	if !treeIDRe.MatchString(id) {
		return candidate{}, false, errors.New("executor: refusing a candidate for an unsafe leaf id")
	}
	raw, found, err := readStateBytes(runDir, candidatePrefix+id)
	if err != nil || !found {
		return candidate{}, false, err
	}
	var c candidate
	// A pass always has a reply hash, a ladder position from 1 and a row
	// revision from 0 (rows start at 0). The hash is of the raw reply, not of
	// Source, so it cannot be checked against Source; the gate at landing is
	// what judges the source.
	if json.Unmarshal(raw, &c) != nil || c.Source == "" || c.Entry == "" || c.SHA256 == "" || c.Pos < 1 || c.Revision < 0 {
		return candidate{}, false, removeCandidate(runDir, id)
	}
	return c, true, nil
}

// removeCandidate deletes _state/candidate-<id>. The guard: a leaf id, and a
// regular file directly in _state. A missing file is not an error.
func removeCandidate(runDir, id string) error {
	if !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing to remove a file that is not a saved candidate")
	}
	dir, ok, err := stateDir(runDir, false)
	if err != nil || !ok {
		return err
	}
	path := filepath.Join(dir, candidatePrefix+id)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("executor: the candidate of %s is not a regular file", id)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("executor: the candidate of %s could not be removed", id)
	}
	return nil
}
