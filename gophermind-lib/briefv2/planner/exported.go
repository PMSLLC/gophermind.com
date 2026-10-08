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
	if found && ap.UnderstandingHash == "" {
		return errors.New("approval.json predates the confirmed understanding; remove it and resume to confirm the understanding and approve the plan again")
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

// ResolveRepo turns a repo path (a leading ~ is expanded) into an absolute path
// to an existing directory. A URL or a missing directory is a brief.InvalidError.
func ResolveRepo(repo string) (string, error) { return resolveRepo(repo) }

// Answer is one settled answer of answers.json.
type Answer struct {
	ID, Stage, Question, Answer string
	Assumed                     bool // answered_by == unattended-default
}

// ReadAnswers reads answers.json; an absent file is an empty list.
func ReadAnswers(runDir string) ([]Answer, error) {
	as, err := loadAnswers(&run{dir: runDir})
	if err != nil {
		return nil, err
	}
	out := make([]Answer, 0, len(as.Answers))
	for _, a := range as.Answers {
		out = append(out, Answer{ID: a.ID, Stage: a.Stage, Question: a.Question, Answer: a.Answer, Assumed: a.Assumed})
	}
	return out, nil
}

// ApprovalInfo is what approval.json records.
type ApprovalInfo struct{ ApprovedAt, ApprovedBy, PlanHash, UnderstandingHash string }

// ReadApproval reads approval.json; false when it is absent.
func ReadApproval(runDir string) (ApprovalInfo, bool, error) {
	var ap approval
	found, err := readJSON((&run{dir: runDir}).path(fileApproval), &ap)
	if err != nil || !found {
		return ApprovalInfo{}, false, err
	}
	return ApprovalInfo{ApprovedAt: ap.ApprovedAt, ApprovedBy: ap.ApprovedBy, PlanHash: ap.PlanHash, UnderstandingHash: ap.UnderstandingHash}, true, nil
}

// UnderstandingInfo is what _state/understanding.json records.
type UnderstandingInfo struct{ ConfirmedAt, ConfirmedBy, Hash string }

// ReadUnderstanding reads _state/understanding.json; false when it is absent.
func ReadUnderstanding(runDir string) (UnderstandingInfo, bool, error) {
	var u understandingRec
	found, err := readJSON((&run{dir: runDir}).path(stateUnderstanding), &u)
	if err != nil || !found {
		return UnderstandingInfo{}, false, err
	}
	return UnderstandingInfo{ConfirmedAt: u.ConfirmedAt, ConfirmedBy: u.ConfirmedBy, Hash: u.Hash}, true, nil
}

// QuestionCounts summarises the question store.
type QuestionCounts struct {
	Total, Settled, Open int
	Rounds, Calls        int            // rounds put to the gate, Clarify model calls
	ByAnsweredBy         map[string]int // human, accepted, unattended-default, probe
	MidStageAssumed      int            // settled unattended-default questions raised by a stage other than clarify
}

// ReadQuestionCounts reads _state/clarify/questions.json; the zero value when
// the store is absent.
func ReadQuestionCounts(runDir string) (QuestionCounts, error) {
	c := QuestionCounts{ByAnsweredBy: map[string]int{}}
	s := qstore{}
	found, err := readJSON((&run{dir: runDir}).path(fileQuestions), &s)
	if err != nil || !found {
		return c, err
	}
	c.Rounds, c.Calls, c.Total = s.Round, s.Calls, len(s.Questions)
	for _, q := range s.Questions {
		if q.Status != "settled" {
			c.Open++
			continue
		}
		c.Settled++
		c.ByAnsweredBy[q.AnsweredBy]++
		if q.AnsweredBy == byUnattended && q.RaisedBy != "clarify" {
			c.MidStageAssumed++
		}
	}
	return c, nil
}
