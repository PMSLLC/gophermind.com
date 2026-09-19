package plan

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on the questions one pass may ask.
const (
	maxQuestionsPerPass = 5
	maxQuestionRunes    = 500
	maxWhyRunes         = 500
	maxOptions          = 8
	maxLabelRunes       = 100
	maxOptionDescRunes  = 300
	maxRationaleRunes   = 300
	maxAffects          = 10
	maxAffectRunes      = 200
)

// OptionOut is a choice a pass offers for a question.
type OptionOut struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionOut is a question a pass asks. Recommended lists option labels.
// Affects means phase or task titles in pass 1 and step ids in pass 2.
type QuestionOut struct {
	Question    string      `json:"question"`
	Why         string      `json:"why"`
	Options     []OptionOut `json:"options"`
	MultiSelect bool        `json:"multi_select"`
	Recommended []string    `json:"recommended"`
	Rationale   string      `json:"rationale"`
	Affects     []string    `json:"affects"`
}

// validateQuestionOuts checks the questions a pass returned. checkAffect, if
// not nil, is called for every affects entry so each pass can hold its own
// meaning of it.
func validateQuestionOuts(qs []QuestionOut, checkAffect func(where, affect string) error) error {
	if len(qs) > maxQuestionsPerPass {
		return fmt.Errorf("too many questions (%d, at most %d)", len(qs), maxQuestionsPerPass)
	}
	seen := map[string]bool{}
	for _, q := range qs {
		where := fmt.Sprintf("question %q", clip(q.Question))
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("a question is empty")
		}
		if utf8.RuneCountInString(q.Question) > maxQuestionRunes {
			return fmt.Errorf("%s: longer than %d characters", where, maxQuestionRunes)
		}
		if utf8.RuneCountInString(q.Why) > maxWhyRunes {
			return fmt.Errorf("%s: why is longer than %d characters", where, maxWhyRunes)
		}
		key := NormalizeTitle(q.Question)
		if seen[key] {
			return fmt.Errorf("%s: asked twice", where)
		}
		seen[key] = true
		labels, err := checkOptions(where, q)
		if err != nil {
			return err
		}
		if err := checkRecommended(where, q, labels); err != nil {
			return err
		}
		if len(q.Affects) > maxAffects {
			return fmt.Errorf("%s: too many affects (%d, at most %d)", where, len(q.Affects), maxAffects)
		}
		for _, a := range q.Affects {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("%s: an affects entry is empty", where)
			}
			if utf8.RuneCountInString(a) > maxAffectRunes {
				return fmt.Errorf("%s: an affects entry is longer than %d characters", where, maxAffectRunes)
			}
			if checkAffect != nil {
				if err := checkAffect(where, a); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkOptions requires no options (a free-text question) or 2 to 8 distinct
// ones, and returns the set of normalized labels.
func checkOptions(where string, q QuestionOut) (map[string]bool, error) {
	labels := map[string]bool{}
	if len(q.Options) == 1 {
		return nil, fmt.Errorf("%s: give at least two options, or none for a free-text question", where)
	}
	if len(q.Options) > maxOptions {
		return nil, fmt.Errorf("%s: too many options (%d, at most %d)", where, len(q.Options), maxOptions)
	}
	for _, o := range q.Options {
		if strings.TrimSpace(o.Label) == "" {
			return nil, fmt.Errorf("%s: an option label is empty", where)
		}
		if utf8.RuneCountInString(o.Label) > maxLabelRunes {
			return nil, fmt.Errorf("%s: an option label is longer than %d characters", where, maxLabelRunes)
		}
		if utf8.RuneCountInString(o.Description) > maxOptionDescRunes {
			return nil, fmt.Errorf("%s: an option description is longer than %d characters", where, maxOptionDescRunes)
		}
		key := NormalizeTitle(o.Label)
		if labels[key] {
			return nil, fmt.Errorf("%s: option %q appears twice", where, clip(o.Label))
		}
		labels[key] = true
	}
	return labels, nil
}

func checkRecommended(where string, q QuestionOut, labels map[string]bool) error {
	if utf8.RuneCountInString(q.Rationale) > maxRationaleRunes {
		return fmt.Errorf("%s: rationale is longer than %d characters", where, maxRationaleRunes)
	}
	if len(q.Recommended) > 1 && !q.MultiSelect {
		return fmt.Errorf("%s: recommends %d options but is not multi_select", where, len(q.Recommended))
	}
	seen := map[string]bool{}
	for _, l := range q.Recommended {
		key := NormalizeTitle(l)
		if !labels[key] {
			return fmt.Errorf("%s: recommends %q, which is not one of its options", where, clip(l))
		}
		if seen[key] {
			return fmt.Errorf("%s: recommends %q twice", where, clip(l))
		}
		seen[key] = true
	}
	return nil
}

// newQuestion converts a validated QuestionOut, with its affects already
// resolved to node ids, into a question to add.
func newQuestion(o QuestionOut, affects []string) NewQuestion {
	nq := NewQuestion{
		Question:          o.Question,
		Why:               o.Why,
		MultiSelect:       o.MultiSelect,
		RecommendedLabels: o.Recommended,
		Rationale:         o.Rationale,
		Affects:           affects,
	}
	for _, opt := range o.Options {
		nq.Options = append(nq.Options, NewOption{Label: opt.Label, Description: opt.Description})
	}
	return nq
}
