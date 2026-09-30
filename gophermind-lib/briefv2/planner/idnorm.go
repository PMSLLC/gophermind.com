package planner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/events"
)

// The schema wants type and component ids like intake-session and function ids
// like fn-validate-email. A model often writes IntakeSession or
// intake_session instead. The fix is deterministic and local: rewrite the id to
// the syntax and every reference to it in the same reply, before validation.
// It is a pure function of the reply and the contract written so far, so a
// resumed run (whose stored contract already holds normalised ids) rewrites
// identically.

var (
	typeIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	fnIDRE   = regexp.MustCompile(`^fn-[a-z0-9-]+$`)
)

// maxIDExamples bounds the examples named in the warning.
const maxIDExamples = 10

type replyKind int

const (
	replyOutline replyKind = iota
	replyComponent
	replyRepair
)

// idNotes is what normalising one reply did: how many ids and references were
// rewritten, a few bounded examples, and the objects dropped because two
// different raw ids became the same id (the first emission wins).
type idNotes struct {
	Count    int
	Examples []string
	Ignored  []string
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// normalizeID returns raw in the id syntax: case boundaries split, lower case,
// every run of other characters one dash, no leading or trailing dash. For a
// function id it also ensures the fn- prefix. An id that is already valid is
// returned as is. The result is empty when nothing is left (for a function id,
// nothing after the prefix).
func normalizeID(raw string, function bool) string {
	if function && fnIDRE.MatchString(raw) || !function && typeIDRE.MatchString(raw) {
		return raw
	}
	var b strings.Builder
	dash := func() {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case isUpper(c):
			if i > 0 {
				prev := raw[i-1]
				if isLower(prev) || isDigit(prev) || (isUpper(prev) && i+1 < len(raw) && isLower(raw[i+1])) {
					dash()
				}
			}
			b.WriteByte(c + 'a' - 'A')
		case isLower(c) || isDigit(c):
			b.WriteByte(c)
		default:
			dash()
		}
	}
	n := strings.Trim(b.String(), "-")
	if !function {
		return n
	}
	switch {
	case n == "":
		return ""
	case n == "fn":
		return ""
	case strings.HasPrefix(n, "fn-"):
		return n
	}
	return "fn-" + n
}

// exampleOf is one "old -> new" example for the warning. The old id is named
// only when it is at most 64 bytes of printable ASCII, otherwise by length.
func exampleOf(old, n string) string {
	shown := fmt.Sprintf("<%d bytes>", len(old))
	if len(old) <= maxBoundedID {
		ok := true
		for i := 0; i < len(old); i++ {
			if old[i] < 0x20 || old[i] > 0x7e {
				ok = false
			}
		}
		if ok {
			shown = fmt.Sprintf("%q", old)
		}
	}
	return shown + " -> " + n
}

// normalizeReply rewrites the ids of a contract reply. doc is the contract
// written so far (nil for the first outline pass). A reply that is not a JSON
// object, or whose lists are not lists of objects, is returned unchanged for
// the merge to reject in its usual words. An id with nothing left is an error
// naming the list and index, never the id.
func normalizeReply(doc map[string]any, text string, kind replyKind) (string, idNotes, error) {
	var notes idNotes
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var reply map[string]any
	if err := dec.Decode(&reply); err != nil || reply == nil {
		return text, notes, nil
	}
	keys := []string{"types", "functions"}
	if kind == replyOutline {
		keys = []string{"components", "types"}
	}
	lists := map[string][]map[string]any{}
	for _, k := range keys {
		raw, present := reply[k]
		if !present {
			continue
		}
		arr, ok := raw.([]any)
		if !ok {
			return text, notes, nil
		}
		for _, x := range arr {
			o, ok := x.(map[string]any)
			if !ok {
				return text, notes, nil
			}
			lists[k] = append(lists[k], o)
		}
	}

	rewrite := func(old, n string) {
		notes.Count++
		if len(notes.Examples) < maxIDExamples {
			notes.Examples = append(notes.Examples, exampleOf(old, n))
		}
	}
	// Ids first: the declared set is what references resolve against.
	declared := map[string]bool{}
	for _, k := range []string{"components", "types", "functions"} {
		for _, o := range objects(doc[k]) {
			if id, _ := o["id"].(string); id != "" {
				declared[id] = true
			}
		}
	}
	changed := false
	for _, k := range keys {
		owner := map[string]string{} // normalised id -> the raw id that first produced it
		var kept []any
		what := strings.TrimSuffix(k, "s")
		for i, o := range lists[k] {
			raw, isStr := o["id"].(string)
			if !isStr {
				kept = append(kept, o)
				continue
			}
			n := normalizeID(raw, k == "functions")
			if n == "" {
				return "", notes, fmt.Errorf("contract reply: %s[%d] id has no letters or digits", k, i)
			}
			if first, seen := owner[n]; seen && first != raw {
				notes.Ignored = append(notes.Ignored, what+" "+boundedID(n))
				changed = true
				continue
			}
			owner[n] = raw
			if n != raw {
				o["id"] = n
				rewrite(raw, n)
				changed = true
			}
			declared[n] = true
			kept = append(kept, o)
		}
		if _, present := reply[k]; present {
			reply[k] = kept
		}
		lists[k] = nil
		for _, x := range kept {
			if o, ok := x.(map[string]any); ok {
				lists[k] = append(lists[k], o)
			}
		}
	}

	// References: a uses entry may name a type or a function, so it resolves to
	// whichever normalised form is declared, else by its own shape (an id-like
	// forward reference is rewritten too: a later pass declares it in that form).
	resolve := func(raw string) string {
		if declared[raw] {
			return raw
		}
		t, f := normalizeID(raw, false), normalizeID(raw, true)
		switch {
		case t != "" && declared[t]:
			return t
		case f != "" && declared[f]:
			return f
		case !idSyntaxRE.MatchString(raw):
			// An unknown reference that is not id-like (spaces, punctuation) is
			// left as it is, so it is reported by length and never turned into
			// something that reads like an id.
			return raw
		case strings.HasPrefix(t, "fn-") && f != "":
			return f
		case t != "":
			return t
		}
		return raw
	}
	for _, k := range keys {
		for _, o := range lists[k] {
			if uses, ok := o["uses"].([]any); ok {
				for j, u := range uses {
					if s, ok := u.(string); ok {
						if n := resolve(s); n != s {
							uses[j] = n
							rewrite(s, n)
							changed = true
						}
					}
				}
			}
			if k == "functions" {
				if c, ok := o["component"].(string); ok && c != "" {
					if n := normalizeID(c, false); n != "" && n != c {
						o["component"] = n
						rewrite(c, n)
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return text, notes, nil
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reply); err != nil {
		return "", notes, err
	}
	return strings.TrimSpace(out.String()), notes, nil
}

// noteNormalized records the rewritten ids in the state and reports them as
// one warning: a count and at most 10 bounded examples, never other reply text.
func (p *Planner) noteNormalized(st *contractState, stage string, n idNotes) {
	if n.Count == 0 {
		return
	}
	st.IDsNormalized += n.Count
	p.emit(events.KindWarning, stage, "", fmt.Sprintf(
		"outline_id_normalized: %d ids and references were rewritten to the id syntax (%s); %d in all so far",
		n.Count, strings.Join(n.Examples, ", "), st.IDsNormalized))
}
