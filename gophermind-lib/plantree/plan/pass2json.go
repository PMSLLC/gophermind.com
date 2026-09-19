package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on what one specification pass may return.
const (
	maxDescRunes      = 2000
	maxCriteria       = 10
	maxCriterionRunes = 300
	maxTargetPaths    = 20
	maxPathRunes      = 300
	maxCommandArgs    = 20
	maxArgRunes       = 200
)

// StepSpecOut is the specification a pass proposes for one step.
type StepSpecOut struct {
	ID                 string   `json:"id"`
	Description        string   `json:"description"`
	TargetPaths        []string `json:"target_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestCommand        []string `json:"test_command"`
	DependsOn          []string `json:"depends_on"`
}

// Pass2Output is what one specification pass returns for a batch of steps.
type Pass2Output struct {
	Steps     []StepSpecOut `json:"steps"`
	Questions []QuestionOut `json:"questions"` // optional; affects are step ids from the batch
}

// ParsePass2 finds, strictly decodes and validates a specification pass reply.
// batch is the ids of the steps that were asked for; the reply must cover
// exactly those, once each. siblings is the ids of every step of the task: a
// step may depend only on an earlier-numbered sibling, which makes a dependency
// cycle impossible by construction. Its errors are specific enough to send back
// to the model as a correction.
func ParsePass2(reply string, batch, siblings []string) (Pass2Output, error) {
	return parseFirst(reply, func(raw string) (Pass2Output, error) {
		return decodePass2(raw, batch, siblings)
	})
}

func decodePass2(raw string, batch, siblings []string) (Pass2Output, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var out Pass2Output
	if err := dec.Decode(&out); err != nil {
		return Pass2Output{}, fmt.Errorf("the JSON does not match the schema: %s", cutBytes(err.Error(), 400))
	}
	if err := validatePass2(out, batch, siblings); err != nil {
		return Pass2Output{}, err
	}
	return out, nil
}

func validatePass2(out Pass2Output, batch, siblings []string) error {
	want := setOf(batch)
	sibling := setOf(siblings)
	asked := map[string]bool{}
	err := validateQuestionOuts(out.Questions, func(where, affect string) error {
		if !want[affect] {
			return fmt.Errorf("%s: affects %q, which is not one of the steps asked for", where, clip(affect))
		}
		asked[affect] = true
		return nil
	})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range out.Steps {
		id := clip(s.ID)
		if !want[s.ID] {
			return fmt.Errorf("step %q is not one of the steps asked for", id)
		}
		if seen[s.ID] {
			return fmt.Errorf("step %q appears more than once", id)
		}
		if asked[s.ID] {
			return fmt.Errorf("step %q is both specified and named in a question; leave it out of steps until the question is answered, or drop the question", id)
		}
		seen[s.ID] = true
		where := fmt.Sprintf("step %q", id)
		if err := checkSpec(where, s, sibling); err != nil {
			return err
		}
	}
	for _, id := range batch {
		if !seen[id] && !asked[id] {
			return fmt.Errorf("step %q was asked for but is missing from the reply (specify it, or ask a question that names it)", clip(id))
		}
	}
	return nil
}

func checkSpec(where string, s StepSpecOut, sibling map[string]bool) error {
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("%s: description is empty", where)
	}
	if utf8.RuneCountInString(s.Description) > maxDescRunes {
		return fmt.Errorf("%s: description is longer than %d characters", where, maxDescRunes)
	}
	if len(s.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%s: acceptance_criteria needs at least one check a reviewer can verify", where)
	}
	if len(s.AcceptanceCriteria) > maxCriteria {
		return fmt.Errorf("%s: too many acceptance criteria (%d, at most %d)", where, len(s.AcceptanceCriteria), maxCriteria)
	}
	for _, c := range s.AcceptanceCriteria {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("%s: an acceptance criterion is empty", where)
		}
		if utf8.RuneCountInString(c) > maxCriterionRunes {
			return fmt.Errorf("%s: an acceptance criterion is longer than %d characters", where, maxCriterionRunes)
		}
	}
	if len(s.TargetPaths) > maxTargetPaths {
		return fmt.Errorf("%s: too many target_paths (%d, at most %d)", where, len(s.TargetPaths), maxTargetPaths)
	}
	for _, p := range s.TargetPaths {
		if utf8.RuneCountInString(p) > maxPathRunes {
			return fmt.Errorf("%s: a target path is longer than %d characters", where, maxPathRunes)
		}
		if err := safeRelPath(p); err != nil {
			return fmt.Errorf("%s: target path %q %w", where, clip(p), err)
		}
	}
	if len(s.TestCommand) > maxCommandArgs {
		return fmt.Errorf("%s: test_command has too many arguments (%d, at most %d)", where, len(s.TestCommand), maxCommandArgs)
	}
	for _, a := range s.TestCommand {
		if strings.TrimSpace(a) == "" {
			return fmt.Errorf("%s: test_command has an empty argument", where)
		}
		if utf8.RuneCountInString(a) > maxArgRunes {
			return fmt.Errorf("%s: a test_command argument is longer than %d characters", where, maxArgRunes)
		}
	}
	return checkDeps(where, s, sibling)
}

// checkDeps requires every dependency to be an earlier-numbered sibling step.
func checkDeps(where string, s StepSpecOut, sibling map[string]bool) error {
	seen := map[string]bool{}
	for _, d := range s.DependsOn {
		if !sibling[d] {
			return fmt.Errorf("%s: depends_on %q is not a step of this task", where, clip(d))
		}
		if segmentNumber(d) >= segmentNumber(s.ID) {
			return fmt.Errorf("%s: depends_on %q must be an earlier step than this one", where, clip(d))
		}
		if seen[d] {
			return fmt.Errorf("%s: depends_on lists %q twice", where, clip(d))
		}
		seen[d] = true
	}
	return nil
}

// safeRelPath rejects a target path that is empty, absolute, drive-qualified,
// contains a control character, or climbs out with "..". Target paths only
// declare scope; this keeps the declaration honest.
func safeRelPath(p string) error {
	switch {
	case strings.TrimSpace(p) == "":
		return fmt.Errorf("is empty")
	case strings.ContainsAny(p, "\x00\r\n"):
		return fmt.Errorf("contains a control character")
	case strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`):
		return fmt.Errorf("must be relative to the repository, not absolute")
	case len(p) >= 2 && p[1] == ':':
		return fmt.Errorf("must be relative to the repository, not drive-qualified")
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("must not contain %q", "..")
		}
	}
	return nil
}

func setOf(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
