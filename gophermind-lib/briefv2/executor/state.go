package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	Wave0Done      bool              `json:"wave0_done"` // the Wave 0 commit was made
	Resumed        bool              `json:"resumed"`    // sticky: once a run has resumed, every later report says so (R11)
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

// AppendEscalation records one escalation, in order.
func AppendEscalation(runDir string, e report.Escalation) error {
	notesMu.Lock()
	defer notesMu.Unlock()
	list, err := LoadEscalations(runDir)
	if err != nil {
		return err
	}
	return writeStateJSON(runDir, stateEscalations, append(list, e))
}

// readStateJSON decodes one of the executor's files. An error names the file
// and the kind of failure, never its content.
func readStateJSON(runDir, name string, v any) (found bool, err error) {
	raw, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("executor: reading %s failed", name)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("executor: %s is not readable (%s)", name, planner.JSONErr(err))
	}
	return true, nil
}

// writeStateJSON stores v as indented JSON: a temp file in the same folder,
// flushed, then renamed over the target, so a reader never sees half a file.
// Files are 0600 and a folder the call creates is 0700.
func writeStateJSON(runDir, name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("executor: encoding %s failed", name)
	}
	path := filepath.Join(runDir, filepath.FromSlash(name))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("executor: writing %s failed", name)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("executor: writing %s failed", name)
	}
	tmpName := tmp.Name()
	fail := func() error {
		tmp.Close()
		removeOwnTemp(dir, tmpName)
		return fmt.Errorf("executor: writing %s failed", name)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail()
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
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
	return nil
}

// removeOwnTemp deletes a temp file this package just made. The guard is the
// whole rule: the path must be directly inside dir and carry the temp prefix.
func removeOwnTemp(dir, path string) {
	if filepath.Dir(path) != dir || len(filepath.Base(path)) <= len(".tmp-") || filepath.Base(path)[:5] != ".tmp-" {
		return
	}
	_ = os.Remove(path)
}
