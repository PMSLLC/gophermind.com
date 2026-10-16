package planner

import "encoding/json"

// StageState is one line of `gophermind brief status`.
type StageState struct {
	Name string
	Done bool
}

// Status is what `gophermind brief status` prints. It is computed from the
// run folder alone.
type Status struct {
	RunID, RunDir, Repo   string
	Stages                []StageState // load, then every later stage in order
	Waiting               string       // the stage a person has to answer for, or ""
	Requirements, Covered int
	AllowPublic           bool
	LedgerErrors          int
}

// ReadStatus reports how far a run has come.
func ReadStatus(runID string) (Status, error) {
	rec, err := LookupRun(runID)
	if err != nil {
		return Status{}, err
	}
	r := &run{id: rec.RunID, dir: rec.RunDir, repo: rec.Repo}
	if _, err := readJSON(r.path(stateStatus), &r.status); err != nil {
		return Status{}, err
	}
	st := Status{RunID: rec.RunID, RunDir: rec.RunDir, Repo: rec.Repo, Waiting: r.status.Waiting,
		AllowPublic: r.status.AllowPublic, LedgerErrors: r.status.LedgerErrors}
	st.Stages = append(st.Stages, StageState{Name: "load", Done: exists(r.path(fileRequirements))})
	for _, s := range stages {
		st.Stages = append(st.Stages, StageState{Name: s.name, Done: s.done(r)})
	}
	var reqs []Requirement
	if _, err := readJSON(r.path(fileRequirements), &reqs); err != nil {
		return Status{}, err
	}
	st.Requirements = len(reqs)
	var cov struct {
		Covered []json.RawMessage `json:"covered"`
	}
	if _, err := readJSON(r.path(fileCoverage), &cov); err != nil {
		return Status{}, err
	}
	st.Covered = len(cov.Covered)
	return st, nil
}
