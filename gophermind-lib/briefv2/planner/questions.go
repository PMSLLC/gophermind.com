package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// Question status and who answered. answered_by is a closed set: human (typed
// or chosen), accepted (the person typed accept to take the recommendation),
// unattended-default (nobody was asked), probe (a fact answered by code).
const (
	qOpen    = "open"
	qSettled = "settled"

	byHuman      = "human"
	byAccepted   = "accepted"
	byUnattended = "unattended-default"
	byProbe      = "probe"
)

// factKeys is the closed list of facts the probe can answer. A question of
// kind fact must name one of them.
var factKeys = []string{"go_module", "go_version", "repo_is_git", "os", "arch", "packages", "secret_names", "hosts"}

const (
	maxQuestionBytes  = 1000
	maxRecommendBytes = 1000
	maxOptionBytes    = 200
	maxHistory        = 5
)

var (
	qidRE        = regexp.MustCompile(`^q([0-9]+)$`)
	decisionIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// qhistory is an earlier answer, kept when an answer is changed.
type qhistory struct {
	At     string `json:"at"`
	Answer string `json:"answer"`
}

// qrec is one question and, once settled, its answer. The planner keeps them
// in _state/clarify/questions.json and copies them into the node tree.
type qrec struct {
	ID             string     `json:"id"`
	Text           string     `json:"question"`
	Why            string     `json:"why_it_matters,omitempty"`
	Kind           string     `json:"kind"` // decision | fact
	FactKey        string     `json:"fact_key,omitempty"`
	DependsOn      []string   `json:"depends_on,omitempty"`
	Options        []string   `json:"options,omitempty"`
	Recommended    string     `json:"recommended,omitempty"`
	RecommendedWhy string     `json:"recommended_why,omitempty"`
	RaisedBy       string     `json:"raised_by"` // clarify, or the stage that asked
	Round          int        `json:"round"`
	Status         string     `json:"status"` // open | settled
	Answer         string     `json:"answer,omitempty"`
	AnsweredBy     string     `json:"answered_by,omitempty"`
	SettledAt      string     `json:"settled_at,omitempty"`
	History        []qhistory `json:"history,omitempty"`
}

// qstore is _state/clarify/questions.json.
type qstore struct {
	Calls     int    `json:"calls"`    // Clarify model calls made
	Round     int    `json:"round"`    // rounds put to the gate
	Complete  bool   `json:"complete"` // the Clarify loop has finished
	Questions []qrec `json:"questions"`
}

// loadQStore reads the question store. A run planned before the store
// existed has answers.json and no questions.json: the store is then built
// from the answers, complete, so nothing is asked again.
func loadQStore(r *run) (qstore, error) {
	s := qstore{Questions: []qrec{}}
	found, err := readJSON(r.path(fileQuestions), &s)
	if err != nil {
		return qstore{}, err
	}
	if s.Questions == nil {
		s.Questions = []qrec{}
	}
	if found {
		return s, nil
	}
	var as answersFile
	legacy, err := readJSON(r.path(fileAnswers), &as)
	if err != nil {
		return qstore{}, err
	}
	if !legacy {
		return s, nil
	}
	s.Complete = true
	for _, a := range as.Answers {
		by := byHuman
		if a.Assumed {
			by = byUnattended
		}
		s.Questions = append(s.Questions, qrec{
			ID: a.ID, Text: a.Question, Kind: "decision", RaisedBy: a.Stage,
			Status: qSettled, Answer: a.Answer, AnsweredBy: by,
		})
	}
	return s, nil
}

func (s qstore) save(r *run) error {
	if s.Questions == nil {
		s.Questions = []qrec{}
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return runfs.WriteFileAtomic(r.path(fileQuestions), append(raw, '\n'))
}

// stamp is the time format of the planner's state files.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func (s *qstore) get(id string) *qrec {
	for i := range s.Questions {
		if s.Questions[i].ID == id {
			return &s.Questions[i]
		}
	}
	return nil
}

// settle records the answer of an open question. It reports false when there
// is no such open question.
func (s *qstore) settle(id, answer, by string, at time.Time) bool {
	q := s.get(id)
	if q == nil || q.Status == qSettled {
		return false
	}
	q.Status, q.Answer, q.AnsweredBy, q.SettledAt = qSettled, answer, by, stamp(at)
	return true
}

// unsettled lists the open questions in order.
func (s qstore) unsettled() []qrec {
	var out []qrec
	for _, q := range s.Questions {
		if q.Status == qOpen {
			out = append(out, q)
		}
	}
	return out
}

// frontier lists the open questions whose prerequisites are all settled: the
// ones that can be asked now without guessing at an answer not yet heard.
func (s qstore) frontier() []qrec {
	settled := map[string]bool{}
	for _, q := range s.Questions {
		if q.Status == qSettled {
			settled[q.ID] = true
		}
	}
	var out []qrec
	for _, q := range s.Questions {
		if q.Status != qOpen {
			continue
		}
		ready := true
		for _, d := range q.DependsOn {
			if !settled[d] {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, q)
		}
	}
	return out
}

// nextNumber is one more than the highest q<N> id in the store.
func (s qstore) nextNumber() int {
	hi := 0
	for _, q := range s.Questions {
		if m := qidRE.FindStringSubmatch(q.ID); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > hi {
				hi = n
			}
		}
	}
	return hi + 1
}

// qparseOpts says how strictly a Clarify reply is read.
type qparseOpts struct {
	MaxTotal           int  // cap on all questions of the run, the ones already stored included; 0 means none
	RequireRecommended bool // a decision or a fact must carry a recommended answer (the run takes recommendations)
	Round              int
	RaisedBy           string
}

// parseQuestionList reads a Clarify reply. A violation is an error whose text
// names the rule and the question's position, never its words, so it can be
// sent back to the model on a retry.
func parseQuestionList(text string, have qstore, o qparseOpts) ([]qrec, error) {
	var raw []struct {
		ID             string   `json:"id"`
		Kind           string   `json:"kind"`
		FactKey        string   `json:"fact_key"`
		Question       string   `json:"question"`
		Why            string   `json:"why_it_matters"`
		DependsOn      []string `json:"depends_on"`
		Options        []string `json:"options"`
		Recommended    string   `json:"recommended"`
		RecommendedWhy string   `json:"recommended_why"`
		Default        string   `json:"default_if_unanswered"` // the field of the one-round Clarify; read as the recommendation
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("clarify reply is not a JSON array of questions (%s)", jsonErr(err))
	}
	if raw == nil {
		return nil, errors.New("clarify reply is null, want a JSON array of questions")
	}
	if o.MaxTotal > 0 && len(have.Questions)+len(raw) > o.MaxTotal {
		return nil, fmt.Errorf("clarify reply would bring the run to %d questions; ask only the most important %d in total", len(have.Questions)+len(raw), o.MaxTotal)
	}
	taken := map[string]bool{}
	earlier := map[string]bool{}
	for _, q := range have.Questions {
		taken[q.ID], earlier[q.ID] = true, true
	}
	next := have.nextNumber()
	out := make([]qrec, 0, len(raw))
	for i, x := range raw {
		n := i + 1
		id := strings.TrimSpace(x.ID)
		if id == "" {
			id = fmt.Sprintf("q%d", next+i)
		}
		if !qidRE.MatchString(id) {
			return nil, fmt.Errorf("clarify reply: question %d needs an id like q%d", n, next+i)
		}
		if taken[id] {
			return nil, fmt.Errorf("clarify reply: question %d repeats an id", n)
		}
		taken[id] = true
		text := strings.TrimSpace(x.Question)
		if text == "" {
			return nil, fmt.Errorf("clarify reply: question %d has no text", n)
		}
		if len(text) > maxQuestionBytes || len(x.Why) > maxQuestionBytes || len(x.RecommendedWhy) > maxQuestionBytes/2 {
			return nil, fmt.Errorf("clarify reply: question %d is too long", n)
		}
		kind := x.Kind
		switch kind {
		case "":
			kind = "decision"
		case "decision", "fact":
		default:
			return nil, fmt.Errorf("clarify reply: question %d has a kind that is neither decision nor fact", n)
		}
		key := x.FactKey
		if kind == "fact" && !contains(factKeys, key) {
			kind, key = "decision", "" // a fact the harness cannot name is a decision for the owner
		}
		if kind == "decision" {
			key = ""
		}
		for _, d := range x.DependsOn {
			if d == id {
				return nil, fmt.Errorf("clarify reply: question %d depends on itself", n)
			}
			if !earlier[d] {
				return nil, fmt.Errorf("clarify reply: question %d depends on a question that is not earlier in the list", n)
			}
		}
		earlier[id] = true
		opts := x.Options
		if len(opts) != 0 && (len(opts) < 2 || len(opts) > 8) {
			return nil, fmt.Errorf("clarify reply: question %d has %d options; give 2 to 8 or none", n, len(opts))
		}
		seen := map[string]bool{}
		for _, op := range opts {
			if strings.TrimSpace(op) == "" || len(op) > maxOptionBytes || seen[op] {
				return nil, fmt.Errorf("clarify reply: question %d needs distinct, non-empty options", n)
			}
			seen[op] = true
		}
		rec := strings.TrimSpace(x.Recommended)
		if rec == "" {
			rec = strings.TrimSpace(x.Default)
		}
		if len(rec) > maxRecommendBytes {
			return nil, fmt.Errorf("clarify reply: question %d has a recommendation that is too long", n)
		}
		if len(opts) > 0 && rec != "" && !contains(opts, rec) {
			return nil, fmt.Errorf("clarify reply: the recommended answer of question %d must be one of its options", n)
		}
		if o.RequireRecommended && rec == "" {
			return nil, fmt.Errorf("clarify reply: question %d needs a recommended answer", n)
		}
		out = append(out, qrec{
			ID: id, Text: text, Why: strings.TrimSpace(x.Why), Kind: kind, FactKey: key,
			DependsOn: append([]string(nil), x.DependsOn...), Options: append([]string(nil), opts...),
			Recommended: rec, RecommendedWhy: strings.TrimSpace(x.RecommendedWhy),
			RaisedBy: o.RaisedBy, Round: o.Round, Status: qOpen,
		})
	}
	return out, nil
}

// answersView is answers.json as the store derives it: every settled question
// in order. Prompts and the approval hash read that file, so it stays the
// compatible view of the store.
func answersView(s qstore) answersFile {
	as := answersFile{Answers: []answer{}}
	for _, q := range s.Questions {
		if q.Status == qSettled {
			as.Answers = append(as.Answers, answer{ID: q.ID, Stage: q.RaisedBy, Question: q.Text, Answer: q.Answer, Assumed: q.AnsweredBy == byUnattended})
		}
	}
	return as
}

// writeAnswersView rewrites answers.json from the store. A run planned before
// the store existed has answers and an empty store: its file is kept.
func writeAnswersView(r *run, s qstore) error {
	if len(s.Questions) == 0 && exists(r.path(fileAnswers)) {
		return nil
	}
	return writeJSON(r.path(fileAnswers), answersView(s))
}

// decisionMarkdown is the browsable record of one settled question.
func decisionMarkdown(q qrec, citedBy []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Decision %s\n\nQuestion: %s\n\n", q.ID, q.Text)
	if q.Why != "" {
		fmt.Fprintf(&b, "Why it matters: %s\n\n", q.Why)
	}
	if len(q.Options) > 0 {
		b.WriteString("Options:\n")
		for _, o := range q.Options {
			fmt.Fprintf(&b, "- %s\n", o)
		}
		b.WriteString("\n")
	}
	if q.Recommended != "" {
		fmt.Fprintf(&b, "Recommended: %s\n", q.Recommended)
		if q.RecommendedWhy != "" {
			fmt.Fprintf(&b, "Why recommended: %s\n", q.RecommendedWhy)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Answer:\n\n%s\n\n", q.Answer)
	fmt.Fprintf(&b, "Answered by: %s\nRound: %d\nRaised by: %s\nSettled at: %s\n", q.AnsweredBy, q.Round, q.RaisedBy, q.SettledAt)
	if len(q.DependsOn) > 0 {
		fmt.Fprintf(&b, "Depends on: %s\n", strings.Join(q.DependsOn, ", "))
	}
	if len(q.History) > 0 {
		b.WriteString("\nEarlier answers:\n")
		for _, h := range q.History {
			fmt.Fprintf(&b, "- %s: %s\n", h.At, oneLine(h.Answer, 400))
		}
	}
	cited := append([]string(nil), citedBy...)
	sort.Strings(cited)
	if len(cited) == 0 {
		b.WriteString("\nShaped these nodes: none recorded.\n")
	} else {
		fmt.Fprintf(&b, "\nShaped these nodes: %s\n", strings.Join(cited, ", "))
	}
	return b.String()
}

// writeDecision writes decisions/<id>.md.
func writeDecision(r *run, q qrec, citedBy []string) error {
	if !decisionIDRE.MatchString(q.ID) {
		return errors.New("a question id must be a plain name to be written as a decision record")
	}
	return runfs.WriteFileAtomic(r.path(dirDecisions+"/"+q.ID+".md"), []byte(decisionMarkdown(q, citedBy)))
}

// recordOf is a settled question as it is written into a node: every field a
// reader needs, none that is empty.
func recordOf(q qrec) map[string]any {
	m := map[string]any{
		"id": q.ID, "question": q.Text, "kind": q.Kind, "answer": q.Answer, "answered_by": q.AnsweredBy,
		"round": q.Round, "raised_by": q.RaisedBy,
	}
	set := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	set("why_it_matters", q.Why)
	set("fact_key", q.FactKey)
	set("recommended", q.Recommended)
	set("recommended_why", q.RecommendedWhy)
	set("settled_at", q.SettledAt)
	toAny := func(in []string) []any {
		out := make([]any, len(in))
		for i, s := range in {
			out[i] = s
		}
		return out
	}
	if len(q.DependsOn) > 0 {
		m["depends_on"] = toAny(q.DependsOn)
	}
	if len(q.Options) > 0 {
		m["options"] = toAny(q.Options)
	}
	if len(q.History) > 0 {
		h := make([]any, len(q.History))
		for i, e := range q.History {
			h[i] = map[string]any{"at": e.At, "answer": e.Answer}
		}
		m["history"] = h
	}
	return m
}

// embedDecisions records the questions and answers in the tree: the root gets
// every settled record, in order; a component or function gets copies of the
// records its decision_ids name, in store order (an empty list when it cites
// none). Code writes them; a model never does.
func embedDecisions(s qstore, root map[string]any, others []map[string]any) {
	all := make([]any, 0, len(s.Questions))
	byID := map[string]qrec{}
	var order []string
	for _, q := range s.Questions {
		if q.Status != qSettled {
			continue
		}
		all = append(all, recordOf(q))
		byID[q.ID] = q
		order = append(order, q.ID)
	}
	if root != nil {
		root["decisions"] = all
	}
	for _, doc := range others {
		cited := map[string]bool{}
		for _, id := range strList(doc["decision_ids"]) {
			cited[id] = true
		}
		mine := make([]any, 0, len(cited))
		for _, id := range order {
			if cited[id] {
				mine = append(mine, recordOf(byID[id])) // a copy, so editing one node leaves the others alone
			}
		}
		doc["decisions"] = mine
	}
}

// checkEmbedded compares the decisions embedded in the tree with what
// embedDecisions would write now. It names the nodes that differ and nothing
// else.
func checkEmbedded(s qstore, root map[string]any, others []map[string]any) []string {
	wantRoot := map[string]any{}
	wantOthers := make([]map[string]any, len(others))
	for i, o := range others {
		wantOthers[i] = map[string]any{"decision_ids": o["decision_ids"]}
	}
	embedDecisions(s, wantRoot, wantOthers)
	var problems []string
	if root != nil && !sameJSON(root["decisions"], wantRoot["decisions"]) {
		problems = append(problems, "the root's decisions differ from the question store")
	}
	for i, o := range others {
		if !sameJSON(o["decisions"], wantOthers[i]["decisions"]) {
			id, _ := o["id"].(string)
			problems = append(problems, fmt.Sprintf("the decisions of node %s differ from its decision_ids", boundedID(id)))
		}
	}
	return problems
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return reflect.DeepEqual(x, y)
}
