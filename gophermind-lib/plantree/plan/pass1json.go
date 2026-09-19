package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on what one skeleton pass may return. They keep a runaway reply from
// flooding the tree and keep every value small enough to sit in a prompt.
const (
	maxPhasesPerPass = 50
	maxTasksPerPhase = 100
	maxStepsPerTask  = 100
	maxTitleRunes    = 200
	maxDigestRunes   = 500
	maxObjective     = 1000
)

// Pass1Output is what one skeleton pass returns for one chunk of the brief.
type Pass1Output struct {
	Phases   []PhaseOut `json:"phases"`
	Overview string     `json:"overview"`
}

// PhaseOut is a phase proposed by a pass.
type PhaseOut struct {
	Title     string    `json:"title"`
	Digest    string    `json:"digest"`
	Objective string    `json:"objective"`
	Tasks     []TaskOut `json:"tasks"`
}

// TaskOut is a task proposed by a pass.
type TaskOut struct {
	Title     string    `json:"title"`
	Digest    string    `json:"digest"`
	Objective string    `json:"objective"`
	Steps     []StepOut `json:"steps"`
}

// StepOut is a step proposed by a pass.
type StepOut struct {
	Title  string `json:"title"`
	Digest string `json:"digest"`
}

// ExtractJSON returns the first complete top-level JSON object in reply,
// ignoring prose and code fences around it. Braces inside strings do not count.
func ExtractJSON(reply string) (string, error) {
	start := strings.IndexByte(reply, '{')
	if start < 0 {
		return "", errors.New("the reply contains no JSON object")
	}
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(reply); i++ {
		c := reply[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return reply[start : i+1], nil
			}
		}
	}
	return "", errors.New("the JSON object in the reply is not closed")
}

// ParsePass1 extracts, strictly decodes and validates a skeleton pass reply.
// Its errors are specific enough to send back to the model as a correction.
func ParsePass1(reply string) (Pass1Output, error) {
	raw, err := ExtractJSON(reply)
	if err != nil {
		return Pass1Output{}, err
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var out Pass1Output
	if err := dec.Decode(&out); err != nil {
		return Pass1Output{}, fmt.Errorf("the JSON does not match the schema: %w", err)
	}
	if err := validatePass1(out); err != nil {
		return Pass1Output{}, err
	}
	return out, nil
}

// NormalizeTitle is the key used to match a proposed node to an existing one.
func NormalizeTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// oneLine collapses all whitespace, including newlines, to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func validatePass1(o Pass1Output) error {
	if strings.TrimSpace(o.Overview) == "" {
		return errors.New(`"overview" must be a non-empty string`)
	}
	if len(o.Phases) > maxPhasesPerPass {
		return fmt.Errorf("too many phases (%d, at most %d)", len(o.Phases), maxPhasesPerPass)
	}
	seenPhase := map[string]bool{}
	for _, p := range o.Phases {
		where := fmt.Sprintf("phase %q", p.Title)
		if err := checkNode(where, p.Title, p.Digest, p.Objective, seenPhase); err != nil {
			return err
		}
		if len(p.Tasks) > maxTasksPerPhase {
			return fmt.Errorf("%s has too many tasks (%d, at most %d)", where, len(p.Tasks), maxTasksPerPhase)
		}
		seenTask := map[string]bool{}
		for _, t := range p.Tasks {
			twhere := fmt.Sprintf("task %q in %s", t.Title, where)
			if err := checkNode(twhere, t.Title, t.Digest, t.Objective, seenTask); err != nil {
				return err
			}
			if len(t.Steps) > maxStepsPerTask {
				return fmt.Errorf("%s has too many steps (%d, at most %d)", twhere, len(t.Steps), maxStepsPerTask)
			}
			seenStep := map[string]bool{}
			for _, s := range t.Steps {
				swhere := fmt.Sprintf("step %q in %s", s.Title, twhere)
				if err := checkNode(swhere, s.Title, s.Digest, "", seenStep); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkNode validates one proposed node and records its title among siblings.
func checkNode(where, title, digest, objective string, siblings map[string]bool) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("%s: title is empty", where)
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return fmt.Errorf("%s: title is longer than %d characters", where, maxTitleRunes)
	}
	if strings.TrimSpace(digest) == "" {
		return fmt.Errorf("%s: digest is empty (one or two sentences on why it exists)", where)
	}
	if utf8.RuneCountInString(digest) > maxDigestRunes {
		return fmt.Errorf("%s: digest is longer than %d characters", where, maxDigestRunes)
	}
	if utf8.RuneCountInString(objective) > maxObjective {
		return fmt.Errorf("%s: objective is longer than %d characters", where, maxObjective)
	}
	key := NormalizeTitle(title)
	if siblings[key] {
		return fmt.Errorf("%s: duplicate sibling title", where)
	}
	siblings[key] = true
	return nil
}
