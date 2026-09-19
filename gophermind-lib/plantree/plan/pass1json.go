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

// ExtractJSON returns the first top-level JSON object in reply that parses,
// ignoring prose and code fences around it. Braces inside strings do not count.
// A balanced object that is not valid JSON is skipped in favor of a later valid
// one, but is returned if no valid one exists so the caller can report why it
// failed to decode.
func ExtractJSON(reply string) (string, error) {
	invalid := ""
	seen := false
	from := 0
	for {
		i := strings.IndexByte(reply[from:], '{')
		if i < 0 {
			break
		}
		start := from + i
		seen = true
		end, ok := balancedEnd(reply, start)
		if !ok {
			from = start + 1
			continue
		}
		cand := reply[start : end+1]
		if json.Valid([]byte(cand)) {
			return cand, nil
		}
		if invalid == "" {
			invalid = cand
		}
		from = end + 1
	}
	if invalid != "" {
		return invalid, nil
	}
	if !seen {
		return "", errors.New("the reply contains no JSON object")
	}
	return "", errors.New("the JSON object in the reply is not closed")
}

// balancedEnd returns the index of the brace that closes the one at start.
func balancedEnd(s string, start int) (int, bool) {
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
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
				return i, true
			}
		}
	}
	return 0, false
}

// candidates lists every balanced top-level object in reply, in order. Objects
// nested inside a balanced one are not listed. seen reports whether any "{" was
// found at all.
func candidates(reply string) (list []string, seen bool) {
	from := 0
	for {
		i := strings.IndexByte(reply[from:], '{')
		if i < 0 {
			return list, seen
		}
		start := from + i
		seen = true
		end, ok := balancedEnd(reply, start)
		if !ok {
			from = start + 1
			continue
		}
		list = append(list, reply[start:end+1])
		from = end + 1
	}
}

// parseFirst tries decode on each candidate object in reply and returns the
// first that succeeds, so prose or a stray "{}" before the real object does not
// hide it. If none succeeds it returns the error for the first candidate, which
// is the one the model most likely meant.
func parseFirst[T any](reply string, decode func(raw string) (T, error)) (T, error) {
	var zero T
	list, seen := candidates(reply)
	var firstErr error
	for _, raw := range list {
		v, err := decode(raw)
		if err == nil {
			return v, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return zero, firstErr
	}
	if !seen {
		return zero, errors.New("the reply contains no JSON object")
	}
	return zero, errors.New("the JSON object in the reply is not closed")
}

// ParsePass1 finds, strictly decodes and validates a skeleton pass reply. Its
// errors are specific enough to send back to the model as a correction.
func ParsePass1(reply string) (Pass1Output, error) {
	return parseFirst(reply, decodePass1)
}

func decodePass1(raw string) (Pass1Output, error) {
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

// maxClipRunes bounds a title quoted in an error message.
const maxClipRunes = 80

// clip makes a model-supplied title safe to quote in an error that goes back
// into a prompt: one line, at most maxClipRunes runes.
func clip(s string) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= maxClipRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == maxClipRunes {
			return s[:i] + "..."
		}
		n++
	}
	return s
}

func validatePass1(o Pass1Output) error {
	if strings.TrimSpace(o.Overview) == "" {
		return errors.New(`"overview" must be a non-empty string`)
	}
	if len(o.Phases) > maxPhasesPerPass {
		return fmt.Errorf("too many phases (%d, at most %d)", len(o.Phases), maxPhasesPerPass)
	}
	seenPhase := map[string]bool{}
	for _, p := range o.Phases {
		where := fmt.Sprintf("phase %q", clip(p.Title))
		if err := checkNode(where, p.Title, p.Digest, p.Objective, seenPhase); err != nil {
			return err
		}
		if len(p.Tasks) > maxTasksPerPhase {
			return fmt.Errorf("%s has too many tasks (%d, at most %d)", where, len(p.Tasks), maxTasksPerPhase)
		}
		seenTask := map[string]bool{}
		for _, t := range p.Tasks {
			twhere := fmt.Sprintf("task %q in %s", clip(t.Title), where)
			if err := checkNode(twhere, t.Title, t.Digest, t.Objective, seenTask); err != nil {
				return err
			}
			if len(t.Steps) > maxStepsPerTask {
				return fmt.Errorf("%s has too many steps (%d, at most %d)", twhere, len(t.Steps), maxStepsPerTask)
			}
			seenStep := map[string]bool{}
			for _, s := range t.Steps {
				swhere := fmt.Sprintf("step %q in %s", clip(s.Title), twhere)
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
