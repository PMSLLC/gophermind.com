package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

const (
	// maxStoredAffects caps the nodes one question can name. A model that keeps
	// re-asking a question with new ids cannot grow its record without bound;
	// ids beyond the cap are dropped.
	maxStoredAffects   = 200
	questionsFile      = "questions.json"
	questionsSchema    = 1
	maxAnswerTextRunes = 2000
	QuestionOpen       = "open"
	QuestionAnswered   = "answered"
)

var (
	// ErrNoSuchQuestion is returned when a question id does not exist.
	ErrNoSuchQuestion = errors.New("plan: no such question")
	// ErrAlreadyAnswered is returned when answering a question that is answered.
	ErrAlreadyAnswered = errors.New("plan: the question is already answered")
	// ErrInvalidAnswer is returned when an answer does not fit its question.
	ErrInvalidAnswer = errors.New("plan: invalid answer")
)

// Option is one choice offered for a question.
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Recommendation is the option or options a pass recommends, with its reason.
// A recommendation is never an answer.
type Recommendation struct {
	OptionIDs []string `json:"option_ids"`
	Rationale string   `json:"rationale"`
}

// Answer is a person's answer: the options they chose and/or free text. A
// question always accepts free text.
type Answer struct {
	OptionIDs []string `json:"option_ids"`
	Text      string   `json:"text"`
}

// Question is a decision the plan needs from a person.
type Question struct {
	ID            string          `json:"id"`
	Question      string          `json:"question"`
	Why           string          `json:"why"`
	Options       []Option        `json:"options"`
	MultiSelect   bool            `json:"multi_select"`
	AllowFreeText bool            `json:"allow_free_text"`
	Recommended   *Recommendation `json:"recommended"`
	Affects       []string        `json:"affects"` // node ids the answer changes
	Source        string          `json:"source"`  // where it was asked, for a person to read
	Status        string          `json:"status"`
	Answer        *Answer         `json:"answer"`
	AnsweredAt    string          `json:"answered_at"`
}

// NewOption and NewQuestion describe a question before it has an id.
type NewOption struct {
	Label       string
	Description string
}

// NewQuestion is a question to add. Option ids are assigned by position
// (opt-1, opt-2, ...); RecommendedLabels must name options by label.
type NewQuestion struct {
	Question          string
	Why               string
	Options           []NewOption
	MultiSelect       bool
	RecommendedLabels []string
	Rationale         string
	Affects           []string
	Source            string // where it was asked, for a person to read
}

type questionFile struct {
	SchemaVersion int        `json:"schema_version"`
	Revision      int        `json:"revision"`
	Questions     []Question `json:"questions"`
}

func questionsPath(repo *plantree.Repo) string { return filepath.Join(repo.Dir(), questionsFile) }

func lockQuestions(repo *plantree.Repo) (func(), error) {
	dir := filepath.Join(repo.Dir(), "_state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return lockfile.Acquire(filepath.Join(dir, "questions.lock"))
}

func loadQuestionFile(repo *plantree.Repo) (questionFile, error) {
	b, err := os.ReadFile(questionsPath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return questionFile{SchemaVersion: questionsSchema}, nil
	}
	if err != nil {
		return questionFile{}, err
	}
	var f questionFile
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return questionFile{}, fmt.Errorf("plan: reading %s: %w", questionsPath(repo), err)
	}
	if f.SchemaVersion != questionsSchema {
		return questionFile{}, fmt.Errorf("plan: %s has unsupported schema_version %d (want %d)", questionsPath(repo), f.SchemaVersion, questionsSchema)
	}
	return f, nil
}

func saveQuestionFile(repo *plantree.Repo, f questionFile) error {
	f.SchemaVersion = questionsSchema
	f.Revision++
	if f.Questions == nil {
		f.Questions = []Question{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(questionsPath(repo), append(b, '\n'), 0o644)
}

// LoadQuestions returns every question, open and answered, in the order asked.
func LoadQuestions(repo *plantree.Repo) ([]Question, error) {
	f, err := loadQuestionFile(repo)
	return f.Questions, err
}

// OpenQuestions returns the questions still waiting for an answer.
func OpenQuestions(repo *plantree.Repo) ([]Question, error) {
	all, err := LoadQuestions(repo)
	var open []Question
	for _, q := range all {
		if q.Status == QuestionOpen {
			open = append(open, q)
		}
	}
	return open, err
}

// AddQuestions adds new questions and returns the resulting Question for each,
// in order. A question whose text (ignoring case and spacing) matches one
// already asked, open or answered, is not added again: its existing record is
// returned, so replaying a pass after a crash asks nothing twice.
func AddQuestions(repo *plantree.Repo, in []NewQuestion) ([]Question, error) {
	if len(in) == 0 {
		return nil, nil
	}
	unlock, err := lockQuestions(repo)
	if err != nil {
		return nil, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(in))
	changed := false
	for _, nq := range in {
		key := NormalizeTitle(nq.Question)
		found := -1
		for i, q := range f.Questions {
			if NormalizeTitle(q.Question) == key {
				found = i
				break
			}
		}
		if found >= 0 {
			// The same question asked again: keep the record, but fold in
			// the nodes this asking names, so they are held (if open) or
			// shown its decision (if answered).
			if merged, grew := unionIDs(f.Questions[found].Affects, nq.Affects); grew {
				f.Questions[found].Affects = merged
				changed = true
			}
			out = append(out, f.Questions[found])
			continue
		}
		q, err := buildQuestion(nq, nextQuestionID(f.Questions))
		if err != nil {
			return nil, err
		}
		f.Questions = append(f.Questions, q)
		out = append(out, q)
		changed = true
	}
	if changed {
		if err := saveQuestionFile(repo, f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// unionIDs returns have followed by the ids of add that it lacks, in order,
// and whether anything was added. The result never exceeds maxAffects ids
// (a have that is already longer is returned unchanged).
func unionIDs(have, add []string) ([]string, bool) {
	seen := setOf(have)
	out := append([]string{}, have...)
	grew := false
	for _, id := range add {
		if len(out) >= maxStoredAffects {
			break
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
			grew = true
		}
	}
	return out, grew
}

func nextQuestionID(qs []Question) string {
	highest := 0
	for _, q := range qs {
		if len(q.ID) > 2 {
			n := 0
			fmt.Sscanf(q.ID[2:], "%d", &n)
			if n > highest {
				highest = n
			}
		}
	}
	return fmt.Sprintf("q-%03d", highest+1)
}

func buildQuestion(nq NewQuestion, id string) (Question, error) {
	q := Question{
		ID:            id,
		Question:      strings.TrimSpace(nq.Question),
		Why:           strings.TrimSpace(nq.Why),
		MultiSelect:   nq.MultiSelect,
		AllowFreeText: true,
		Affects:       append([]string{}, nq.Affects...),
		Source:        strings.TrimSpace(nq.Source),
		Status:        QuestionOpen,
		Options:       make([]Option, len(nq.Options)),
	}
	byLabel := map[string]string{}
	for i, o := range nq.Options {
		oid := fmt.Sprintf("opt-%d", i+1)
		q.Options[i] = Option{ID: oid, Label: strings.TrimSpace(o.Label), Description: strings.TrimSpace(o.Description)}
		byLabel[NormalizeTitle(o.Label)] = oid
	}
	if len(nq.RecommendedLabels) > 0 {
		rec := &Recommendation{Rationale: strings.TrimSpace(nq.Rationale), OptionIDs: []string{}}
		for _, l := range nq.RecommendedLabels {
			oid, ok := byLabel[NormalizeTitle(l)]
			if !ok {
				return Question{}, fmt.Errorf("plan: question %q recommends %q, which is not one of its options", cutBytes(nq.Question, 80), cutBytes(l, 80))
			}
			rec.OptionIDs = append(rec.OptionIDs, oid)
		}
		q.Recommended = rec
	}
	return q, nil
}

// AnswerQuestion records a person's answer to an open question. The answer
// must select at least one option or give text, select only options the
// question offers (each once), and select at most one option unless the
// question is multi-select. Free text is always allowed.
func AnswerQuestion(repo *plantree.Repo, id string, a Answer) (Question, error) {
	unlock, err := lockQuestions(repo)
	if err != nil {
		return Question{}, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return Question{}, err
	}
	for i := range f.Questions {
		q := &f.Questions[i]
		if q.ID != id {
			continue
		}
		if q.Status != QuestionOpen {
			return Question{}, fmt.Errorf("%w: %s", ErrAlreadyAnswered, id)
		}
		if err := checkAnswer(*q, a); err != nil {
			return Question{}, err
		}
		q.Status = QuestionAnswered
		q.Answer = &Answer{OptionIDs: append([]string{}, a.OptionIDs...), Text: strings.TrimSpace(a.Text)}
		q.AnsweredAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveQuestionFile(repo, f); err != nil {
			return Question{}, err
		}
		return *q, nil
	}
	return Question{}, fmt.Errorf("%w: %s", ErrNoSuchQuestion, id)
}

func checkAnswer(q Question, a Answer) error {
	text := strings.TrimSpace(a.Text)
	if len(a.OptionIDs) == 0 && text == "" {
		return fmt.Errorf("%w: choose an option or write an answer", ErrInvalidAnswer)
	}
	if utf8.RuneCountInString(text) > maxAnswerTextRunes {
		return fmt.Errorf("%w: the text is longer than %d characters", ErrInvalidAnswer, maxAnswerTextRunes)
	}
	if len(a.OptionIDs) > 1 && !q.MultiSelect {
		return fmt.Errorf("%w: this question accepts one option", ErrInvalidAnswer)
	}
	valid := map[string]bool{}
	for _, o := range q.Options {
		valid[o.ID] = true
	}
	seen := map[string]bool{}
	for _, oid := range a.OptionIDs {
		if !valid[oid] {
			return fmt.Errorf("%w: %q is not an option of %s", ErrInvalidAnswer, cutBytes(oid, 40), q.ID)
		}
		if seen[oid] {
			return fmt.Errorf("%w: option %s chosen twice", ErrInvalidAnswer, oid)
		}
		seen[oid] = true
	}
	return nil
}
