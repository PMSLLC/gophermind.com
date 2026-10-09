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

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runfs"
)

const fileRevisionDecisions = "_state/revisions/decisions.json"

var decisionClassRE = regexp.MustCompile(`^[a-z_]{1,40}$`)

// decisionMu serializes the read-modify-write of the decision store across
// every Reviser in the process.
var decisionMu sync.Mutex

// RevisionDecision is a person's answer about a leaf the revise stage touched.
type RevisionDecision struct {
	RunDir, NodeID string
	Revision       int
	Class, Action  string
	Note           string
	AnsweredBy     string
	Round          int
}

type revisionDecisions struct {
	Decisions []map[string]any `json:"decisions"`
}

// decisionNote puts a note on one line, drops control and format characters
// (escape sequences, bidi marks, zero-width characters) and bounds it.
func decisionNote(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	return oneLine(s, 600)
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
	if note := decisionNote(d.Note); note != "" {
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
			return id, nil
		}
	}
	id := fmt.Sprintf("rv-%s-%d", d.NodeID, count+1)
	if !decisionIDRE.MatchString(id) {
		return "", errors.New("planner: the decision id is not a plain name")
	}
	q := qrec{ID: id, Text: question, Kind: "decision", RaisedBy: "revise:" + d.NodeID, Round: d.Round, Status: qSettled,
		Answer: answer, AnsweredBy: by, SettledAt: stamp(v.p.d.Now())}
	st.Decisions = append(st.Decisions, recordOf(q))
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	if err := runfs.WriteFileAtomic(path, append(raw, '\n')); err != nil {
		return "", err
	}
	if err := runfs.WriteFileAtomic(r.path(dirDecisions+"/"+id+".md"), []byte(decisionMarkdown(q, []string{d.NodeID}))); err != nil {
		return "", err
	}
	return id, nil
}
