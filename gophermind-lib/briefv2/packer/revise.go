package packer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
)

const (
	maxReviseNotes    = 5
	maxReviseNoteSize = 200
)

// PackRevise builds the revise prompt: the contract entry and slice, the test
// file, and one line per attempt (class and names only, never reply text or
// command output). It applies the same secret scan and token budget as Pack
// (in.Budget, in.Secrets; in.Failure and in.Notes are not used): history lines
// with a secret are dropped, a secret in any other input is an error that does
// not quote it, and over budget the history is cut to its last 3 lines
// ("history:last3"), then the test comments are stripped ("test_comments"),
// then ErrFloorOverBudget.
func PackRevise(n NodeView, c *contract.Contracts, history []string, in Inputs) (Packed, error) {
	if in.Budget <= 0 {
		return Packed{}, errors.New("packer: budget must be positive")
	}
	if c == nil {
		return Packed{}, errors.New("packer: contracts are required")
	}
	slice, err := c.Slice([]string{n.FuncID}, "")
	if err != nil {
		return Packed{}, errors.New("packer: the function is not in the contracts")
	}
	sc := newScan(in.Secrets)
	hist := sc.scrub(history)
	src := n.TestSource
	render := func() (string, error) {
		h := "(none)"
		if len(hist) > 0 {
			h = "- " + strings.Join(hist, "\n- ")
		}
		t, err := renderTemplate("revise", escAll(map[string]string{
			"File": n.File, "Package": n.Package, "Signature": n.Signature,
			"Contract": strings.Join(slice, "\n\n"), "TestFile": n.TestFile,
			"TestSource": strings.TrimRight(src, "\n"), "History": h,
		}))
		if err != nil {
			return "", errors.New("packer: the prompt template failed")
		}
		return t, nil
	}
	text, err := render()
	if err != nil {
		return Packed{}, err
	}
	if sc.has(text) {
		return Packed{}, errors.New("packer: a secret value is present in the prompt inputs")
	}
	var dropped []string
	fits := func() bool { return EstimateTokens(len(text)) <= in.Budget }
	if !fits() && len(hist) > 3 {
		hist = hist[len(hist)-3:]
		dropped = append(dropped, "history:last3")
		if text, err = render(); err != nil {
			return Packed{}, err
		}
	}
	if !fits() {
		if out, _ := stripTestComments(src); len(out) < len(src) {
			src = out
			dropped = append(dropped, "test_comments")
			if text, err = render(); err != nil {
				return Packed{}, err
			}
		}
	}
	if !fits() {
		return Packed{}, &ErrFloorOverBudget{Tokens: EstimateTokens(len(text)), Budget: in.Budget}
	}
	return newPacked(text, dropped), nil
}

func reviseErr(kind string) error { return errors.New("packer: malformed revise reply: " + kind) }

// ParseRevise reads the revise reply: JSON {"notes":[...]} with 1 to 5 notes of
// at most 200 bytes (fenced or not), or a CONTRACT_PROBLEM line. Errors name a
// kind, never the text.
func ParseRevise(text string) (notes []string, contractProblem string, err error) {
	t := strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(t, "CONTRACT_PROBLEM:"); ok {
		line, _, _ := strings.Cut(strings.TrimSpace(rest), "\n")
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, "", reviseErr("empty contract problem")
		}
		return nil, line, nil
	}
	if strings.HasPrefix(t, "```") {
		_, body, _ := strings.Cut(t, "\n")
		t = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
	}
	var doc struct {
		Notes []string `json:"notes"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(t)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, "", reviseErr("not the expected JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, "", reviseErr("text after the JSON")
	}
	if len(doc.Notes) == 0 {
		return nil, "", reviseErr("no notes")
	}
	if len(doc.Notes) > maxReviseNotes {
		return nil, "", reviseErr("too many notes")
	}
	for _, n := range doc.Notes {
		if strings.TrimSpace(n) == "" {
			return nil, "", reviseErr("blank note")
		}
		if len(n) > maxReviseNoteSize {
			return nil, "", reviseErr("note too long")
		}
	}
	return doc.Notes, "", nil
}
