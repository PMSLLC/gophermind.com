package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"gophermind/gophermind-lib/build/human"
	"gophermind/gophermind-lib/build/revlog"
	"gophermind/gophermind-lib/build/runfs"
)

const fileRevisionDecisions = "_state/revisions/decisions.json"

var decisionClassRE = regexp.MustCompile(`^[a-z_]{1,40}$`)

// decisionMu serializes the read-modify-write of the decision store across
// every Reviser in the process. A run is single-process (one executor owns the
// run folder), so a second process writing the same store would lose an
// update; the lock does not try to cover that.
var decisionMu sync.Mutex

// RevisionDecision is a person's answer about a leaf the revise stage touched.
type RevisionDecision struct {
	RunDir, NodeID string
	Revision       int
	Class, Action  string
	Note           string
	AnsweredBy     string
	Round          int
	// Scrub removes declared secret values from the note. It runs on the note
	// after control and format characters are stripped, so a value split by an
	// invisible character is seen whole. Nil means the caller has scrubbed.
	Scrub func(string) string
}

type revisionDecisions struct {
	Decisions []map[string]any `json:"decisions"`
}

// decisionNote puts a note on one line, drops control and format characters
// (escape sequences, bidi marks, zero-width characters), applies scrub to the
// cleaned text and then bounds it.
func decisionNote(s string, scrub func(string) string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if scrub != nil {
		s = scrub(s)
	}
	return oneLine(s, 600)
}

// qrecOfRecord rebuilds the settled question a stored decision record came
// from, so its markdown can be rendered again byte for byte.
func qrecOfRecord(m map[string]any) qrec {
	str := func(k string) string { v, _ := m[k].(string); return v }
	round, _ := m["round"].(float64)
	return qrec{ID: str("id"), Text: str("question"), Kind: str("kind"), RaisedBy: str("raised_by"), Round: int(round), Status: qSettled,
		Answer: str("answer"), AnsweredBy: str("answered_by"), SettledAt: str("settled_at")}
}

func writeDecisionMarkdown(r *run, q qrec, nodeID string) error {
	return runfs.WriteFileAtomic(r.path(dirDecisions+"/"+q.ID+".md"), []byte(decisionMarkdown(q, []string{nodeID})))
}

// RecordDecision appends the answer to _state/revisions/decisions.json, in the
// shape of recordOf, and writes decisions/rv-<node>-<m>.md. The question store,
// answers.json (inside the plan hash), the root's decisions and every node's
// embedded decisions are not touched, so the B consistency check keeps passing.
// A repeat of the same answer (same question, round and answer) returns the id
// it already has; a different answer to the same question gets the next id, so
// a stop followed by a retry after a resume keeps both. The caller has scrubbed
// Note.
func (v *Reviser) RecordDecision(d RevisionDecision) (string, error) {
	if !reviseIDRE.MatchString(d.NodeID) || !filepath.IsAbs(d.RunDir) {
		return "", errors.New("planner: a revision decision needs a plain node id and an absolute run folder")
	}
	switch human.Action(d.Action) {
	case human.ActionRetry, human.ActionSkip, human.ActionStop:
	default:
		return "", errors.New("planner: a revision decision needs retry, skip or stop")
	}
	if _, err := revlog.NodeDir(d.RunDir, d.NodeID); err != nil {
		return "", err
	}
	if fi, err := os.Lstat(filepath.Join(d.RunDir, dirDecisions)); err == nil && (fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir()) {
		return "", errors.New("planner: the decisions folder is not a plain directory")
	}
	class := d.Class
	if !decisionClassRE.MatchString(class) {
		class = "other"
	}
	by := d.AnsweredBy
	if !human.ValidAnsweredBy(by) {
		by = human.AnsweredByProgrammatic
	}
	answer := d.Action
	if note := decisionNote(d.Note, d.Scrub); note != "" {
		answer += ": " + note
	}
	decisionMu.Lock()
	defer decisionMu.Unlock()
	r := &run{dir: d.RunDir}
	path := r.path(fileRevisionDecisions)
	var st revisionDecisions
	if _, err := readJSON(path, &st); err != nil {
		return "", err
	}
	question := fmt.Sprintf("Leaf %s ended with %s after %d revision(s). What should the build do?", d.NodeID, class, d.Revision)
	idRE := regexp.MustCompile(`^rv-` + regexp.QuoteMeta(d.NodeID) + `-[0-9]+$`)
	count := 0
	for _, m := range st.Decisions {
		id, _ := m["id"].(string)
		if !idRE.MatchString(id) {
			continue
		}
		count++
		if m["question"] == question && m["round"] == float64(d.Round) && m["answer"] == answer {
			// A crash between the two writes can leave the record without its
			// file; the file is derived from the record, so write it again.
			if _, err := os.Stat(r.path(dirDecisions + "/" + id + ".md")); errors.Is(err, os.ErrNotExist) {
				if err := writeDecisionMarkdown(r, qrecOfRecord(m), d.NodeID); err != nil {
					return "", err
				}
			}
			return id, nil
		}
	}
	id := fmt.Sprintf("rv-%s-%d", d.NodeID, count+1)
	if !decisionIDRE.MatchString(id) {
		return "", errors.New("planner: the decision id is not a plain name")
	}
	q := qrec{ID: id, Text: question, Kind: "decision", RaisedBy: "revise:" + d.NodeID, Round: d.Round, Status: qSettled,
		Answer: answer, AnsweredBy: by, SettledAt: stamp(v.p.d.Now())}
	// The file goes first: a crash leaves at most an orphan file that the next
	// identical call overwrites, never a record without its file.
	if err := writeDecisionMarkdown(r, q, d.NodeID); err != nil {
		return "", err
	}
	st.Decisions = append(st.Decisions, recordOf(q))
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	if err := runfs.WriteFileAtomic(path, append(raw, '\n')); err != nil {
		return "", err
	}
	return id, nil
}
