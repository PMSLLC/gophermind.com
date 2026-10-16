package planner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxAnswerBytes = 4000

// ChangeReport says what ChangeAnswer undid: the point the planner restarts
// from and the run-folder files it removed.
type ChangeReport struct {
	Reset   string   // "contract", or the component whose drafts were reset
	Removed []string // relative to the run folder
}

// ChangeAnswer changes the answer to a settled question and undoes the work
// that used it. It is deliberately coarse: a Clarify answer (or one given to
// a repair stage) restarts the planner from the Contract stage and removes the
// confirmation; a Decompose answer resets that component's drafts and
// enrichment; an Enrich answer resets only that component's (or the root's)
// Enrich output. Nothing is changed once the Test-writer has written tests or
// the executor has started.
func ChangeAnswer(runID, questionID, text string, now time.Time) (ChangeReport, error) {
	rec, err := LookupRun(runID)
	if err != nil {
		return ChangeReport{}, err
	}
	return changeAnswerIn(&run{id: rec.RunID, dir: rec.RunDir, repo: rec.Repo}, questionID, text, now)
}

func changeAnswerIn(r *run, questionID, text string, now time.Time) (ChangeReport, error) {
	if exists(r.path(stateTestwriter)) || exists(r.path(stateLeafTests)) || exists(r.path("_state/executor.json")) {
		return ChangeReport{}, errors.New("this run has written tests or started building; start a new run to change an answer")
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxAnswerBytes {
		return ChangeReport{}, fmt.Errorf("the new answer must be 1 to %d bytes", maxAnswerBytes)
	}
	s, err := loadQStore(r)
	if err != nil {
		return ChangeReport{}, err
	}
	q := s.get(questionID)
	if q == nil || q.Status != qSettled {
		return ChangeReport{}, errors.New("there is no settled question with that id")
	}
	if q.AnsweredBy == byProbe {
		return ChangeReport{}, errors.New("this answer was established by the harness from the repository; change the repository instead")
	}
	if q.Answer == text {
		return ChangeReport{}, errors.New("that is already the answer")
	}
	// The reset comes first and is safe to repeat: a crash after it but before
	// the new answer is saved leaves the old answer, and running the same
	// command again redoes the reset and saves the answer. The nodes that cite
	// the question are read before the reset, so the record lists what it shaped.
	cited, err := citedByOf(r, q.ID)
	if err != nil {
		return ChangeReport{}, err
	}
	var rep ChangeReport
	comp := componentOfStage(q.RaisedBy)
	kind, _, _ := strings.Cut(q.RaisedBy, ":")
	switch {
	case q.RaisedBy == "enrich_root":
		removed, err := resetEnrich(r, "")
		if err != nil {
			return ChangeReport{}, err
		}
		rep.Reset, rep.Removed = "root", removed
	case comp != "" && (kind == "enrich" || kind == "enrich_comp"):
		removed, err := resetEnrich(r, comp)
		if err != nil {
			return ChangeReport{}, err
		}
		rep.Reset, rep.Removed = comp, removed
	case comp != "":
		removed, err := resetComponent(r, comp)
		if err != nil {
			return ChangeReport{}, err
		}
		rep.Reset, rep.Removed = comp, removed
	default:
		rep.Reset, rep.Removed = "contract", resetFromContract(r)
	}
	q.History = append(q.History, qhistory{At: q.SettledAt, Answer: q.Answer})
	if len(q.History) > maxHistory {
		q.History = q.History[len(q.History)-maxHistory:]
	}
	q.Answer, q.AnsweredBy, q.SettledAt = text, byHuman, stamp(now)
	if err := s.save(r); err != nil {
		return ChangeReport{}, err
	}
	if err := writeAnswersView(r, s); err != nil {
		return ChangeReport{}, err
	}
	if err := writeDecision(r, *q, cited); err != nil {
		return ChangeReport{}, err
	}
	_ = removeFile(r.path(stateQuestion))
	if _, err := readJSON(r.path(stateStatus), &r.status); err == nil && r.status.Waiting != "" {
		r.status.Waiting = ""
		if err := r.saveStatus(); err != nil {
			return ChangeReport{}, err
		}
	}
	return rep, nil
}

// citedByOf lists the nodes (functions, components, the root) whose
// decision_ids name the question.
func citedByOf(r *run, id string) ([]string, error) {
	cited, err := citedByAll(r)
	if err != nil {
		return nil, err
	}
	return cited[id], nil
}

// componentOfStage is the component a stage name refers to
// (decompose:<comp>, enrich:<comp>, enrich_comp:<comp>), or "" when the stage
// is not about one component (a repair stage, Contract, the root, Coverage).
func componentOfStage(stage string) string {
	kind, rest, ok := strings.Cut(stage, ":")
	if !ok || (kind != "decompose" && kind != "enrich" && kind != "enrich_comp") {
		return ""
	}
	comp, _, _ := strings.Cut(rest, ":")
	if comp == "" || comp == "_fix" {
		return ""
	}
	return comp
}

// resetFromContract removes everything the Contract stage and the stages after
// it wrote, keeping the brief, the requirements, the questions and the answers.
func resetFromContract(r *run) []string {
	var doc struct {
		Components []struct {
			ID string `json:"id"`
		} `json:"components"`
	}
	_, _ = readJSON(r.path(fileContracts), &doc)
	files := []string{fileContracts, fileCoverage, fileApproval, fileDependencies, stateUnderstanding, stateContract, stateDecomposed, stateClasses, stateEnriched, "root.json"}
	for _, c := range doc.Components {
		files = append(files, c.ID+"/component.json")
	}
	var removed []string
	for _, f := range files {
		if filepath.IsLocal(f) && exists(r.path(f)) && os.Remove(r.path(f)) == nil {
			removed = append(removed, f)
		}
	}
	for _, c := range doc.Components {
		if filepath.IsLocal(c.ID) {
			_ = os.Remove(r.path(c.ID)) // only an empty folder goes
		}
	}
	return removed
}

// resetComponent forgets one component's function drafts, classes and
// enrichment, so the planner decomposes and enriches it again, and removes the
// coverage and the approval, which depend on the plan.
func resetComponent(r *run, comp string) ([]string, error) {
	dec, err := loadDecomposed(r)
	if err != nil {
		return nil, err
	}
	classes, err := loadClasses(r)
	if err != nil {
		return nil, err
	}
	for _, d := range dec.Components[comp] {
		id, _ := d["id"].(string)
		delete(classes, id)
	}
	delete(dec.Components, comp)
	kept := dec.Pending[:0]
	for _, pd := range dec.Pending {
		if pd.Component != comp {
			kept = append(kept, pd)
		}
	}
	dec.Pending, dec.Done = kept, false
	if err := writeJSON(r.path(stateDecomposed), dec); err != nil {
		return nil, err
	}
	if err := writeJSON(r.path(stateClasses), classes); err != nil {
		return nil, err
	}
	st, err := loadEnriched(r)
	if err != nil {
		return nil, err
	}
	delete(st.Components, comp)
	pend := st.Pending[:0]
	for _, pe := range st.Pending {
		if pe.Component != comp {
			pend = append(pend, pe)
		}
	}
	st.Pending, st.Done = pend, false
	if err := st.save(r); err != nil {
		return nil, err
	}
	removed := []string{stateDecomposed + " (component " + comp + ")", stateEnriched + " (component " + comp + ")"}
	for _, f := range []string{fileCoverage, fileApproval} {
		if exists(r.path(f)) && os.Remove(r.path(f)) == nil {
			removed = append(removed, f)
		}
	}
	return removed, nil
}

// resetEnrich forgets the Enrich output of one component (its function groups
// and its own groups), or of the root when comp is "", and removes the
// coverage and the approval. The Decompose drafts and the classes stay.
func resetEnrich(r *run, comp string) ([]string, error) {
	st, err := loadEnriched(r)
	if err != nil {
		return nil, err
	}
	var removed []string
	if comp == "" {
		st.Root = nil
		removed = append(removed, stateEnriched+" (root)")
	} else {
		dec, err := loadDecomposed(r)
		if err != nil {
			return nil, err
		}
		for _, d := range dec.Components[comp] {
			for _, g := range enrichFieldsAll {
				delete(d, g)
			}
		}
		if err := writeJSON(r.path(stateDecomposed), dec); err != nil {
			return nil, err
		}
		delete(st.Components, comp)
		pend := st.Pending[:0]
		for _, pe := range st.Pending {
			if pe.Component != comp {
				pend = append(pend, pe)
			}
		}
		st.Pending = pend
		removed = append(removed, stateEnriched+" (component "+comp+")")
	}
	st.Done = false
	if err := st.save(r); err != nil {
		return nil, err
	}
	for _, f := range []string{fileCoverage, fileApproval} {
		if exists(r.path(f)) && os.Remove(r.path(f)) == nil {
			removed = append(removed, f)
		}
	}
	return removed, nil
}
