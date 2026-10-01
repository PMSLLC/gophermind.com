package planner

import (
	"errors"
	"os"
)

// The read-only accessors the executor uses to read a planned run folder.

// LeafTest is where a function node's test lives, and the hash of the file as
// the planner wrote it.
type LeafTest struct {
	TestFile string `json:"test_file"` // repo-relative, forward slashes
	TestFunc string `json:"test_func"`
	SHA256   string `json:"sha256"`
}

// VerifyApproval is the check the Test-writer makes: approval.json exists and
// binds the plan as it stands now.
func VerifyApproval(runDir string) error {
	var ap approval
	found, err := readJSON((&run{dir: runDir}).path(fileApproval), &ap)
	if err != nil {
		return err
	}
	_, hash, err := RenderPlan(runDir)
	if err != nil {
		return err
	}
	if !found || ap.PlanHash != hash {
		return errors.New("approval.json does not match the plan as it stands; remove it and resume to approve the plan again")
	}
	return nil
}

// ReadDependencies reads dependencies.json; an absent file is an empty list.
func ReadDependencies(runDir string) ([]Dependency, error) {
	deps := []Dependency{}
	if _, err := readJSON((&run{dir: runDir}).path(fileDependencies), &deps); err != nil {
		return nil, err
	}
	if deps == nil {
		deps = []Dependency{}
	}
	return deps, nil
}

// ReadLeafTests reads _state/leaf_tests.json: function node id to its test.
func ReadLeafTests(runDir string) (map[string]LeafTest, error) {
	out := map[string]LeafTest{}
	if _, err := readJSON((&run{dir: runDir}).path(stateLeafTests), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadAcceptanceTests reads _state/acceptance_tests.json: acceptance requirement
// id to the Go test the Test-writer wrote for it (the file as the planner wrote
// it, its test function and its hash). An absent file is an empty map.
func ReadAcceptanceTests(runDir string) (map[string]LeafTest, error) {
	out := map[string]LeafTest{}
	if _, err := readJSON((&run{dir: runDir}).path(stateAcceptTests), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadClasses reads _state/classes.json: node id to node class.
func ReadClasses(runDir string) (map[string]string, error) {
	return loadClasses(&run{dir: runDir})
}

// ReadRequirements reads requirements.json.
func ReadRequirements(runDir string) ([]Requirement, error) {
	var reqs []Requirement
	found, err := readJSON((&run{dir: runDir}).path(fileRequirements), &reqs)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &os.PathError{Op: "read", Path: fileRequirements, Err: os.ErrNotExist}
	}
	return reqs, nil
}

// JSONErr describes a JSON decode failure without quoting the input.
func JSONErr(err error) string { return jsonErr(err) }

// SyntaxErr describes a Go parse failure by position only.
func SyntaxErr(err error) string { return syntaxErr(err) }

// IgnoredDuplicates reads the duplicate emissions the Contract stage dropped
// (_state/contract.json): the stored entries (ids only, at most 200), the exact
// total, and whether the stored list was cut. An absent file is an empty list.
func IgnoredDuplicates(runDir string) ([]string, int, bool, error) {
	var st contractState
	if _, err := readJSON((&run{dir: runDir}).path(stateContract), &st); err != nil {
		return nil, 0, false, err
	}
	total, truncated := st.IgnoredTotal, st.IgnoredTruncated
	if total < len(st.IgnoredDuplicates) {
		total = len(st.IgnoredDuplicates)
	}
	// A state written before the total existed: a full list was cut.
	if st.IgnoredTotal == 0 && len(st.IgnoredDuplicates) >= maxIgnoredRecorded {
		truncated = true
	}
	return append([]string{}, st.IgnoredDuplicates...), total, truncated, nil
}
