package planner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/tree"
)

// Triggers of a revision, as the executor reports them.
const (
	TriggerLadderExhausted = "ladder_exhausted"
	TriggerContractProblem = "contract_problem"
)

// Classes of the revise stage: every refusal, flag and escalation reason uses one.
const (
	ClassReviseInvalid   = "revise_invalid"
	ClassReviseNoop      = "revise_noop"
	ClassSecretInReply   = "secret_in_reply"
	ClassContractFlagged = "contract_flagged"
	ClassSplitRequired   = "split_required"
	ClassTestsLocked     = "tests_locked"
	ClassVacuousTest     = "revise_vacuous_test"
	ClassRedBuild        = "revise_red_build"
)

// reviseErr is a refusal or a flag with its class. Its text names a rule and at
// most a count, never the reply (the B convention).
type reviseErr struct{ class, detail string }

func (e *reviseErr) Error() string { return e.class + ": " + e.detail }

func invalid(format string, a ...any) error {
	return &reviseErr{ClassReviseInvalid, fmt.Sprintf(format, a...)}
}

const (
	maxDescription   = 2000
	maxParagraph     = 600
	maxAssumptions   = 5
	maxAssumptionLen = 300
	maxReplyBytes    = 256 << 10 // the whole reply, checked before any decoding
	maxEntryLen      = 500       // one string of notes, constraints, side_effects or an item field
	maxListItems     = 20        // entries of one list in the patch (assumptions have their own cap)
	maxJSONDepth     = 32        // nesting of the reply, checked before decoding
)

// revisePatch is the JSON reply of the revise model: optional keys, none else.
type revisePatch struct {
	Description  *string                    `json:"description"`
	Contract     *reviseContract            `json:"contract"`
	Context      *reviseContext             `json:"context"`
	Groups       map[string]json.RawMessage `json:"groups"`
	RewriteTests bool                       `json:"rewrite_tests"`
	Assumptions  []string                   `json:"assumptions"`
}

type reviseContract struct {
	Inputs      *[]map[string]any `json:"inputs"`
	Outputs     *[]map[string]any `json:"outputs"`
	Errors      *[]map[string]any `json:"errors"`
	SideEffects *[]string         `json:"side_effects"`
}

type reviseContext struct {
	Constraints *[]string `json:"constraints"`
	Notes       *[]string `json:"notes"`
}

type reviseKind int

const (
	kindPatch reviseKind = iota
	kindContractChange
	kindSplit
)

type reviseReply struct {
	kind      reviseKind
	patch     revisePatch
	paragraph string
}

var reviseGroupKeys = []string{"construction", "security", "observability", "performance", "portability", "profile_hooks"}

// cleanParagraph keeps a one-paragraph answer printable: control characters
// (terminal escapes included) become spaces, runs of white space collapse.
func cleanParagraph(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func paragraphReply(kind reviseKind, rest string) (reviseReply, error) {
	p := cleanParagraph(rest)
	if p == "" || len(p) > maxParagraph {
		return reviseReply{}, invalid("the paragraph must be 1 to %d bytes", maxParagraph)
	}
	return reviseReply{kind: kind, paragraph: p}, nil
}

// parseReviseReply reads the reply of spec section 6: a JSON patch, or one of
// two keyword lines with a paragraph. CONTRACT_PROBLEM is the implement
// model's answer and is malformed here.
func parseReviseReply(text string) (reviseReply, error) {
	if len(text) > maxReplyBytes {
		return reviseReply{}, invalid("the reply is longer than %d bytes", maxReplyBytes)
	}
	t := strings.TrimSpace(text)
	first, rest, _ := strings.Cut(t, "\n")
	switch strings.TrimSpace(first) {
	case "CONTRACT_CHANGE_REQUIRED":
		return paragraphReply(kindContractChange, rest)
	case "SPLIT_REQUIRED":
		return paragraphReply(kindSplit, rest)
	}
	if strings.HasPrefix(t, "CONTRACT_PROBLEM") {
		return reviseReply{}, invalid("the reply is a contract problem line, which the revise stage does not accept")
	}
	body := StripReply(t)
	if err := scanReplyJSON(body); err != nil {
		return reviseReply{}, err
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var p revisePatch
	if err := dec.Decode(&p); err != nil {
		return reviseReply{}, invalid("the reply is not a JSON patch (%s)", jsonErr(err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return reviseReply{}, invalid("the reply has text after the JSON")
	}
	if err := checkPatchShape(p); err != nil {
		return reviseReply{}, err
	}
	return reviseReply{kind: kindPatch, patch: p}, nil
}

// parseReviseReplyScanned is parseReviseReply behind the secret scan of the RAW
// reply: the keyword paragraphs and every free text of a patch are covered. Task
// 4 must call this (or scan the raw reply itself) BEFORE the reply is saved to
// a record, a decision, a report or a prompt, never after.
func parseReviseReplyScanned(text string, secrets []string) (reviseReply, error) {
	if len(text) > maxReplyBytes {
		return reviseReply{}, invalid("the reply is longer than %d bytes", maxReplyBytes)
	}
	if packer.HasSecret(text, secrets) {
		return reviseReply{}, &reviseErr{ClassSecretInReply, "the reply holds a secret value"}
	}
	return parseReviseReply(text)
}

// replyKeys are the keys a reply may name; only these are ever quoted in a message.
var replyKeys = append([]string{"description", "contract", "context", "groups", "rewrite_tests", "assumptions",
	"inputs", "outputs", "errors", "side_effects", "constraints", "notes",
	"name", "type", "when", "returns", "kind"}, reviseGroupKeys...)

// scanReplyJSON walks the reply's tokens before it is decoded: nesting is
// bounded, a key may not repeat in one object (Go would keep the last), and a
// key of the patch, contract or context may not be null.
func scanReplyJSON(body string) error {
	dec := json.NewDecoder(strings.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
	}
	return scanTok(dec, tok, 0, "top")
}

func scanTok(dec *json.Decoder, tok json.Token, depth int, ctx string) error {
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= maxJSONDepth {
		return invalid("the reply is nested deeper than %d levels", maxJSONDepth)
	}
	if d == '[' {
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
			}
			if err := scanTok(dec, t, depth+1, "other"); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		if err != nil {
			return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
		}
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
		}
		key, _ := kt.(string)
		if seen[key] {
			if contains(replyKeys, key) {
				return invalid("duplicate key %s at depth %d", key, depth+1)
			}
			return invalid("duplicate key at depth %d", depth+1)
		}
		seen[key] = true
		vt, err := dec.Token()
		if err != nil {
			return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
		}
		if vt == nil && ctx != "other" {
			if contains(replyKeys, key) {
				return invalid("%s may not be null", key)
			}
			return invalid("a key at depth %d may not be null", depth+1)
		}
		child := "other"
		if ctx == "top" && (key == "contract" || key == "context") {
			child = key
		}
		if err := scanTok(dec, vt, depth+1, child); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return invalid("the reply is not a JSON patch (%s)", jsonErr(err))
	}
	return nil
}

func checkStrings(field string, ss []string) error {
	if len(ss) > maxListItems {
		return invalid("%s has more than %d entries", field, maxListItems)
	}
	for _, x := range ss {
		if len(x) > maxEntryLen {
			return invalid("an entry of %s is longer than %d bytes", field, maxEntryLen)
		}
	}
	return nil
}

func checkPatchShape(p revisePatch) error {
	if p.Description != nil && (len(*p.Description) < 1 || len(*p.Description) > maxDescription) {
		return invalid("description must be 1 to %d bytes", maxDescription)
	}
	if len(p.Assumptions) > maxAssumptions {
		return invalid("at most %d assumptions", maxAssumptions)
	}
	seenA := map[string]bool{}
	for _, a := range p.Assumptions {
		if strings.TrimSpace(a) == "" || len(a) > maxAssumptionLen {
			return invalid("each assumption must be 1 to %d bytes", maxAssumptionLen)
		}
		if seenA[strings.TrimSpace(a)] {
			return invalid("assumptions may not repeat")
		}
		seenA[strings.TrimSpace(a)] = true
	}
	if p.Context != nil {
		if p.Context.Notes != nil {
			if err := checkStrings("context.notes", *p.Context.Notes); err != nil {
				return err
			}
		}
		if p.Context.Constraints != nil {
			if err := checkStrings("context.constraints", *p.Context.Constraints); err != nil {
				return err
			}
		}
	}
	if p.Contract != nil && p.Contract.SideEffects != nil {
		if err := checkStrings("contract.side_effects", *p.Contract.SideEffects); err != nil {
			return err
		}
	}
	for k, raw := range p.Groups {
		if !contains(reviseGroupKeys, k) {
			return invalid("groups may name construction, security, observability, performance, portability or profile_hooks")
		}
		if string(bytes.TrimSpace(raw)) == "null" {
			return invalid("a group may not be null")
		}
	}
	if c := p.Contract; c != nil {
		if err := checkItems(c.Inputs, "inputs", "name", "type", "description", "constraints"); err != nil {
			return err
		}
		if err := checkItems(c.Outputs, "outputs", "name", "type", "description", "constraints"); err != nil {
			return err
		}
		if err := checkItems(c.Errors, "errors", "when", "returns", "kind"); err != nil {
			return err
		}
		if c.Errors != nil {
			for _, e := range *c.Errors {
				if !contains([]string{"sentinel", "wrapped", "typed", "panic", "http_status", "exit_code", "other"}, strField(e, "kind")) {
					return invalid("each error needs a kind from the node schema")
				}
			}
		}
	}
	if p.Description == nil && p.Contract == nil && p.Context == nil && len(p.Groups) == 0 && !p.RewriteTests {
		return invalid("the patch changes nothing: it needs at least one change key")
	}
	return nil
}

// checkItems allows only the listed keys in each item: go_type and test links
// are written by code, never by the model.
func checkItems(items *[]map[string]any, field string, allowed ...string) error {
	if items == nil {
		return nil
	}
	if len(*items) > maxListItems {
		return invalid("contract.%s has more than %d items", field, maxListItems)
	}
	for _, it := range *items {
		for k, v := range it {
			if !contains(allowed, k) {
				return invalid("an item of contract.%s has a key that is not allowed", field)
			}
			switch x := v.(type) {
			case string:
				if len(x) > maxEntryLen {
					return invalid("a field of an item of contract.%s is longer than %d bytes", field, maxEntryLen)
				}
			case []any:
				var ss []string
				for _, e := range x {
					str, ok := e.(string)
					if !ok {
						return invalid("a field of an item of contract.%s holds a value that is not text", field)
					}
					ss = append(ss, str)
				}
				if err := checkStrings("a field of an item of contract."+field, ss); err != nil {
					return err
				}
			default:
				return invalid("a field of an item of contract.%s holds a value that is not text", field)
			}
		}
	}
	return nil
}

func anyObjects(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

func anyStrings(in []string) []any { return toAnySlice(in) }

// mergePatch applies p to a copy of orig. go_type of inputs and outputs is
// derived again from the signature, as Enrich does; nothing else is added.
func mergePatch(orig map[string]any, p revisePatch, c *contract.Contracts) (map[string]any, error) {
	m, err := copyDoc(orig)
	if err != nil {
		return nil, invalid("the node cannot be copied")
	}
	if p.Description != nil {
		m["description"] = *p.Description
	}
	ct, _ := m["contract"].(map[string]any)
	if ct == nil {
		return nil, invalid("the node has no contract")
	}
	if pc := p.Contract; pc != nil {
		if pc.Inputs != nil {
			ct["inputs"] = anyObjects(*pc.Inputs)
		}
		if pc.Outputs != nil {
			ct["outputs"] = anyObjects(*pc.Outputs)
		}
		if pc.Errors != nil {
			ct["errors"] = anyObjects(*pc.Errors)
		}
		if pc.SideEffects != nil {
			ct["side_effects"] = anyStrings(*pc.SideEffects)
		}
	}
	if pc := p.Context; pc != nil {
		cx, _ := m["context"].(map[string]any)
		if cx == nil {
			cx = map[string]any{}
			m["context"] = cx
		}
		if pc.Constraints != nil {
			cx["constraints"] = anyStrings(*pc.Constraints)
		}
		if pc.Notes != nil {
			cx["notes"] = anyStrings(*pc.Notes)
		}
	}
	for k, raw := range p.Groups {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, invalid("a group is not valid JSON (%s)", jsonErr(err))
		}
		m[k] = v
	}
	sig, _ := ct["signature"].(string)
	if gt, gerr := deriveGoTypes(sig, c); gerr == nil {
		for _, in := range objects(ct["inputs"]) {
			if name, _ := in["name"].(string); name != "" {
				if t, ok := gt.ParamByName[name]; ok {
					in["go_type"] = t
				}
			}
		}
		for i, o := range objects(ct["outputs"]) {
			if i < len(gt.Results) {
				o["go_type"] = gt.Results[i]
			}
		}
	}
	return m, nil
}

// mergeEnv is what the checks need besides the documents.
type mergeEnv struct {
	Class        string
	Env          groupEnv
	Secrets      []string // secret values of the run
	N            int      // the revision this rewrite would become
	TestsRewrite bool     // the Test-writer will run: a body-equal patch is not a no-op yet
}

// revisedFields are the paths compared to say what a patch changed.
var revisedFields = []string{"description", "contract.inputs", "contract.outputs", "contract.errors", "contract.side_effects",
	"context.constraints", "context.notes", "construction", "security", "observability", "performance", "portability", "profile_hooks"}

func fieldAt(m map[string]any, path string) any {
	head, tail, nested := strings.Cut(path, ".")
	if !nested {
		return m[head]
	}
	sub, _ := m[head].(map[string]any)
	return sub[tail]
}

// changedFields names the revisable fields whose value differs.
func changedFields(orig, merged map[string]any) []string {
	var out []string
	for _, f := range revisedFields {
		if !sameJSON(fieldAt(orig, f), fieldAt(merged, f)) {
			out = append(out, f)
		}
	}
	return out
}

// checkMerged runs the checks of spec section 6 in order and stops at the first
// failure. It raises merged to the final document: assumptions gain the
// reply's items prefixed "revision n: " and revision becomes n. The returned
// list names the revisable fields that changed.
func checkMerged(orig, merged map[string]any, p revisePatch, env mergeEnv) ([]string, error) {
	// 1. Frozen fields.
	ob, _ := json.Marshal(orig)
	mb, _ := json.Marshal(merged)
	if err := revlog.CheckFrozen(ob, mb); err != nil {
		var fe *revlog.FrozenError
		if errors.As(err, &fe) {
			return nil, invalid("a frozen field changed (%s)", fe.Field)
		}
		return nil, invalid("a frozen field changed")
	}
	// 2. The input names are the signature's parameters. Planning only requires
	// an input per named parameter, so this runs only when the patch sets the inputs.
	ct, _ := merged["contract"].(map[string]any)
	sig, _ := ct["signature"].(string)
	want := paramNames(sig)
	if p.Contract != nil && p.Contract.Inputs != nil {
		got := map[string]bool{}
		dup := false
		for _, in := range objects(ct["inputs"]) {
			name, _ := in["name"].(string)
			dup = dup || got[name]
			got[name] = true
		}
		if dup || len(got) != len(want) {
			return nil, &reviseErr{ClassContractFlagged, "the inputs no longer match the signature"}
		}
		for n := range want {
			if !got[n] {
				return nil, &reviseErr{ClassContractFlagged, "the inputs no longer match the signature"}
			}
		}
	}
	// 3. The group checks of planning.
	ge := env.Env
	ge.Params = want
	if ds := groupDefects(merged, env.Class, ge); len(ds) > 0 {
		return nil, invalid("the group checks fail (%s)", strings.Join(kindsOfGroup(ds), ", "))
	}
	// 4. No open question; assumptions grow, revision moves.
	if len(strList(merged["open_questions"])) > 0 {
		return nil, invalid("open_questions must stay empty")
	}
	// 5. No secret value anywhere in the patch.
	pb, _ := json.Marshal(p)
	if packer.HasSecret(string(pb), env.Secrets) {
		return nil, &reviseErr{ClassSecretInReply, "the reply holds a secret value"}
	}
	changed := changedFields(orig, merged)
	// Assumptions are append-only: the original list is a prefix of the merged one.
	oa := toAnySliceKeep(orig["assumptions"])
	as := toAnySliceKeep(merged["assumptions"])
	if len(as) < len(oa) || !sameJSON(as[:len(oa)], oa) {
		return nil, invalid("assumptions are append-only")
	}
	for _, a := range p.Assumptions {
		as = append(as, fmt.Sprintf("revision %d: %s", env.N, a))
	}
	merged["assumptions"] = as
	merged["revision"] = env.N
	// 6. The merged document is a valid node.
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, invalid("the revised node cannot be encoded")
	}
	if _, err := tree.ParseNode(raw); err != nil {
		return nil, invalid("the revised node does not match the node schema")
	}
	// 7. Not a no-op.
	if len(changed) == 0 && !env.TestsRewrite {
		return nil, &reviseErr{ClassReviseNoop, "the rewrite equals the current node"}
	}
	return changed, nil
}

// toAnySliceKeep returns the elements of a JSON array as a new []any.
func toAnySliceKeep(v any) []any {
	arr, _ := v.([]any)
	return append([]any{}, arr...)
}
