package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/config"
)

// Files of a run folder (.gophermind/<brief-id>/ inside the target repo).
const (
	fileBrief        = "brief.md"
	fileRequirements = "requirements.json"
	fileAnswers      = "answers.json"
	fileContracts    = "contracts.json"
	fileCoverage     = "coverage.json"
	fileApproval     = "approval.json"

	stateDir          = "_state"
	stateStatus       = "_state/status.json"
	stateClarify      = "_state/clarify.json"
	stateQuestion     = "_state/question.json"
	stateContract     = "_state/contract.json"
	stateDecomposed   = "_state/decomposed.json"
	stateClasses      = "_state/classes.json"
	stateTestwriter   = "_state/testwriter.json"
	stateTestFiles    = "_state/test_files.json"
	stateLeafTests    = "_state/leaf_tests.json"
	stateAcceptTests  = "_state/acceptance_tests.json"
	stagePrefixSystem = "GopherMind planner. Stage: "

	fileQuestions      = "_state/clarify/questions.json"
	fileFacts          = "_state/clarify/facts.json"
	fileRounds         = "_state/clarify/rounds.jsonl"
	stateUnderstanding = "_state/understanding.json"
	stateEnriched      = "_state/enriched.json"
	fileUnderstanding  = "UNDERSTANDING.md"
	dirDecisions       = "decisions"
)

// RunRecord is how `resume`, `status` and `calls` find a run from its id
// alone: the run folder lives inside the target repo, and the id does not say
// where that is.
type RunRecord struct {
	RunID     string `json:"run_id"`
	Repo      string `json:"repo"`
	RunDir    string `json:"run_dir"`
	BriefPath string `json:"brief_path"`
	StartedAt string `json:"started_at"`
}

func runRecordPath(runID string) (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runs", runID+".json"), nil
}

// LookupRun reads <config dir>/runs/<run-id>.json.
func LookupRun(runID string) (RunRecord, error) {
	var rec RunRecord
	if !ValidRunID(runID) {
		return rec, fmt.Errorf("planner: %q is not a run id (want gm-YYYY-MM-DD-NNN)", runID)
	}
	path, err := runRecordPath(runID)
	if err != nil {
		return rec, err
	}
	found, err := readJSON(path, &rec)
	if err != nil {
		return rec, err
	}
	if !found {
		return rec, fmt.Errorf("planner: no run %s on this machine (looked for %s)", runID, path)
	}
	return rec, nil
}

// runStatus is _state/status.json.
type runStatus struct {
	AllowPublic  bool   `json:"allow_public"`
	LedgerErrors int    `json:"ledger_errors"`
	Waiting      string `json:"waiting"`
	PlannedAt    string `json:"planned_at"`
}

// writeFileAtomic writes data beside path and renames it into place, so a
// reader never sees half a file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeJSON stores v as indented JSON, mode 0600.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'), 0o600)
}

// readJSON decodes path into v. found is false, with no error, when the file
// does not exist.
func readJSON(path string, v any) (found bool, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
