package executor

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/report"
)

// The executor's own files in the run folder. They are harness files, not
// plan files: the executor may rewrite them, and they are never part of the
// plan hash.
const (
	stateExecutor    = "_state/executor.json"
	stateNotes       = "_state/notes.json"
	stateEscalations = "_state/escalations.json"

	maxNotesPerNode = 5 // spec 7.3 rung 5 allows up to 5 hints
)

// State is _state/executor.json.
type State struct {
	PlanHashes     map[string]string `json:"plan_hashes"` // recorded at first start, compared on every start
	StartedAt      string            `json:"started_at"`  // empty means no state was ever written: a fresh run
	Branch         string            `json:"branch"`
	Wave0Done      bool              `json:"wave0_done"`    // the Wave 0 commit was made
	Resumed        bool              `json:"resumed"`       // sticky: once a run has resumed, every later report says so (R11)
	Tip            string            `json:"tip,omitempty"` // the work branch tip last recorded: a resume refuses a branch that no longer contains it
	ExtraRevisions map[string]int    `json:"extra_revisions"`
	CriticalStreak map[string]int    `json:"critical_streak"` // S10
	RedChecked     map[string]bool   `json:"red_checked"`
	ExtraRepair    map[string]int    `json:"extra_repair"` // wave number as a string -> extra repair rounds granted by a human
}

func (s *State) initMaps() {
	if s.PlanHashes == nil {
		s.PlanHashes = map[string]string{}
	}
	if s.ExtraRevisions == nil {
		s.ExtraRevisions = map[string]int{}
	}
	if s.CriticalStreak == nil {
		s.CriticalStreak = map[string]int{}
	}
	if s.RedChecked == nil {
		s.RedChecked = map[string]bool{}
	}
	if s.ExtraRepair == nil {
		s.ExtraRepair = map[string]int{}
	}
}

// LoadState reads _state/executor.json. A missing file is a zero State with
// initialized maps; a damaged one is an error, never a silent fresh start.
func LoadState(runDir string) (State, error) {
	var s State
	if _, err := readStateJSON(runDir, stateExecutor, &s); err != nil {
		return State{}, err
	}
	s.initMaps()
	return s, nil
}

// Save writes the state atomically, mode 0600.
func (s State) Save(runDir string) error {
	s.initMaps()
	return writeStateJSON(runDir, stateExecutor, s)
}

// notesMu serialises the read-modify-write of the two append-only files.
var notesMu sync.Mutex

// LoadNotes reads _state/notes.json: node id to its human hints. A missing
// file is an empty map. Notes are never logged or put in an event.
func LoadNotes(runDir string) (map[string][]string, error) {
	notes := map[string][]string{}
	if _, err := readStateJSON(runDir, stateNotes, &notes); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = map[string][]string{}
	}
	return notes, nil
}

// AddNote appends a note to a node and keeps only the last 5 for it.
func AddNote(runDir, nodeID, note string) error {
	notesMu.Lock()
	defer notesMu.Unlock()
	notes, err := LoadNotes(runDir)
	if err != nil {
		return err
	}
	list := append(notes[nodeID], note)
	if len(list) > maxNotesPerNode {
		list = list[len(list)-maxNotesPerNode:]
	}
	notes[nodeID] = list
	return writeStateJSON(runDir, stateNotes, notes)
}

// LoadEscalations reads _state/escalations.json. A missing file is empty.
func LoadEscalations(runDir string) ([]report.Escalation, error) {
	list := []report.Escalation{}
	if _, err := readStateJSON(runDir, stateEscalations, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []report.Escalation{}
	}
	return list, nil
}

// AppendEscalation records one escalation, in order. It is idempotent: an
// escalation with the same kind, task type, model, node, revision and round as
// one already recorded is not recorded again (a resumed run repeats the step
// that recorded it), and when the repeat carries an AnsweredBy the record gets
// it. A gate-absent or unattended-default answer is replaced by a later one
// (a person answering on resume) and Answers counts the answers; any other
// recorded answer is kept.
func AppendEscalation(runDir string, e report.Escalation) error {
	notesMu.Lock()
	defer notesMu.Unlock()
	list, err := LoadEscalations(runDir)
	if err != nil {
		return err
	}
	same := func(x report.Escalation) bool {
		return x.Kind == e.Kind && x.TaskType == e.TaskType && x.Model == e.Model && x.NodeID == e.NodeID &&
			x.Revision == e.Revision && x.Round == e.Round
	}
	for i := len(list) - 1; i >= 0; i-- {
		if !same(list[i]) {
			continue
		}
		cur := list[i].AnsweredBy
		replace := cur == human.AnsweredByGateAbsent || cur == human.AnsweredByUnattended
		if e.AnsweredBy == "" || e.AnsweredBy == cur || (cur != "" && !replace) {
			return nil
		}
		list[i].AnsweredBy = e.AnsweredBy
		if list[i].Answers == 0 {
			list[i].Answers = 1
		}
		if cur != "" {
			list[i].Answers++
		}
		return writeStateJSON(runDir, stateEscalations, list)
	}
	return writeStateJSON(runDir, stateEscalations, append(list, e))
}

// stateDir returns <runDir>/_state after checking it is a real directory, not
// a symbolic link. With create it makes a missing one (0700); a directory that
// exists with looser permissions is tightened to 0700.
func stateDir(runDir string, create bool) (dir string, exists bool, err error) {
	dir = filepath.Join(runDir, "_state")
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !create {
			return dir, false, nil
		}
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", false, errors.New("executor: creating _state failed")
		}
		// Chmod, not the Mkdir mode: the process umask can only narrow it,
		// but another process may have made the folder first.
		if fi, err = os.Lstat(dir); err != nil {
			return "", false, errors.New("executor: _state is not readable")
		}
	case err != nil:
		return "", false, errors.New("executor: _state is not readable")
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "", false, errors.New("executor: _state is not a plain directory")
	}
	if create && fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", false, errors.New("executor: tightening _state failed")
		}
	}
	return dir, true, nil
}

// readStateJSON decodes one of the executor's files. A symbolic link, at the
// folder or at the file (dangling ones included), is refused. An error names
// the file and the kind of failure, never its content.
func readStateJSON(runDir, name string, v any) (found bool, err error) {
	dir, ok, err := stateDir(runDir, false)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	path := filepath.Join(dir, filepath.Base(name))
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return false, fmt.Errorf("executor: %s is not a regular file", name)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|pathsafe.NoFollow, 0)
	if err != nil {
		return false, fmt.Errorf("executor: reading %s failed", name)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return false, fmt.Errorf("executor: reading %s failed", name)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("executor: %s is not readable (%s)", name, planner.JSONErr(err))
	}
	return true, nil
}

// writeStateJSON stores v as indented JSON: a new temp file in the same
// folder (O_EXCL, O_NOFOLLOW, 0600), flushed, renamed over the target, and the
// folder flushed, so a reader never sees half a file. A symbolic link at the
// folder or at the target is refused.
func writeStateJSON(runDir, name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("executor: encoding %s failed", name)
	}
	return writeStateBytes(runDir, name, append(raw, '\n'))
}

// writeStateBytes is the atomic, no-follow, mode 0600 writer under _state/.
func writeStateBytes(runDir, name string, data []byte) error {
	dir, _, err := stateDir(runDir, true)
	if err != nil {
		return err
	}
	base := filepath.Base(name)
	path := filepath.Join(dir, base)
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("executor: %s is not a regular file", name)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("executor: writing %s failed", name)
	}
	tmpName := filepath.Join(dir, tempPrefix+base+"-"+hex.EncodeToString(suffix[:]))
	tmp, err := os.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|pathsafe.NoFollow, 0o600)
	if err != nil {
		return fmt.Errorf("executor: writing %s failed", name)
	}
	fail := func() error {
		tmp.Close()
		removeOwnTemp(dir, tmpName)
		return fmt.Errorf("executor: writing %s failed", name)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail()
	}
	if err := tmp.Sync(); err != nil {
		return fail()
	}
	if err := tmp.Close(); err != nil {
		removeOwnTemp(dir, tmpName)
		return fmt.Errorf("executor: writing %s failed", name)
	}
	if err := os.Rename(tmpName, path); err != nil {
		removeOwnTemp(dir, tmpName)
		return fmt.Errorf("executor: writing %s failed", name)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

const tempPrefix = ".tmp-"

// removeOwnTemp deletes a temp file this package just made. The guard is the
// whole rule: the path must sit directly inside dir and its name must start
// with tempPrefix followed by at least one more character. A path that fails
// either test is left alone, so a bug upstream can never turn this into a
// delete of some other file.
func removeOwnTemp(dir, path string) {
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), tempPrefix) || len(filepath.Base(path)) <= len(tempPrefix) {
		return
	}
	_ = os.Remove(path)
}

// readStateBytes reads a raw file of _state (never through a link). A missing
// file is found == false.
func readStateBytes(runDir, name string) (data []byte, found bool, err error) {
	dir, ok, err := stateDir(runDir, false)
	if err != nil || !ok {
		return nil, false, err
	}
	path := filepath.Join(dir, filepath.Base(name))
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false, fmt.Errorf("executor: %s is not a regular file", name)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|pathsafe.NoFollow, 0)
	if err != nil {
		return nil, false, fmt.Errorf("executor: reading %s failed", name)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, false, fmt.Errorf("executor: reading %s failed", name)
	}
	return raw, true, nil
}

// priorPrefix names the files that hold a leaf's verified source while a
// diff_only repair is in flight.
const priorPrefix = "prior-"

// removeStatePrior deletes _state/prior-<id>. The guard: the name must start
// with priorPrefix, and the target must be a regular file directly in _state.
func removeStatePrior(runDir, name string) error {
	if !strings.HasPrefix(name, priorPrefix) || name != filepath.Base(name) {
		return errors.New("executor: refusing to remove a file that is not a saved prior")
	}
	dir, ok, err := stateDir(runDir, false)
	if err != nil || !ok {
		return err
	}
	path := filepath.Join(dir, name)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return errors.New("executor: a saved prior is not a regular file")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("executor: removing a saved prior failed")
	}
	return nil
}

// restorePriors is the resume step of a diff_only repair: a leaf whose saved
// verified source is still in _state was cut off mid-repair, so its file is
// put back (atomically) and the saved copy removed. A run with no saved prior
// does nothing.
func restorePriors(repo, runDir string, leaves []*Leaf) error {
	for _, l := range leaves {
		name := priorPrefix + l.ID
		raw, found, err := readStateBytes(runDir, name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := pathsafe.Replace(repo, l.File, raw); err != nil {
			return fmt.Errorf("executor: restoring the verified file of %s failed", l.ID)
		}
		if err := removeStatePrior(runDir, name); err != nil {
			return err
		}
	}
	return nil
}
