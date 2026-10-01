package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"gophermind/gophermind-lib/briefv2/contract"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/router"
)

// A node of a Decompose reply that fails a check no longer sinks the batch. The
// nodes that passed are stored at once; the others are asked for again, alone,
// with their defects named in a fixed vocabulary, and only the defective fields
// are taken from the answer. Structural fields the contract's signature
// determines are derived by code once the bound is spent.

const (
	decomposeFixStage       = "decompose:_fix"
	maxDecomposeRepairs     = 2
	maxDecomposeRepairBatch = 8
)

// pendingDraft is a node waiting for its repair, kept in _state/decomposed.json
// so a resume neither asks again for what passed nor forgets the passes spent.
type pendingDraft struct {
	Component string         `json:"component"`
	ID        string         `json:"id"`
	Raw       map[string]any `json:"raw,omitempty"` // the node as the model sent it; nil when it sent none
	Defects   []string       `json:"defects"`
	Tries     int            `json:"tries,omitempty"`
	msg       string         // the defect messages, for tests; never stored or printed
}

// defectFields are the fields to ask for again for a defect: top-level names, or
// "contract.<field>" for a field of the node's contract.
func defectFields(kinds []string) []string {
	var out []string
	add := func(fs ...string) {
		for _, f := range fs {
			if !contains(out, f) {
				out = append(out, f)
			}
		}
	}
	for _, k := range kinds {
		switch k {
		case defMissingNode:
			add("title", "description", "model_tier", "node_class", "depends_on",
				"contract.inputs", "contract.outputs", "contract.errors", "contract.side_effects")
		case defNodeClass:
			add("node_class")
		case defTitle:
			add("title")
		case defDescription:
			add("description")
		case defContract:
			add("contract.inputs", "contract.outputs", "contract.errors", "contract.side_effects")
		case defInputsMissing, defInputType:
			add("contract.inputs")
		case defOutputsCount, defOutputType:
			add("contract.outputs")
		case defErrorsMissing, defErrorsPartial:
			add("contract.errors")
		case defDepends:
			add("depends_on")
		case defSchema:
			add("title", "description", "model_tier", "contract.side_effects")
		}
	}
	return out
}

// structural defects can be repaired by code from the contract's signature.
var structural = map[string]bool{defInputsMissing: true, defInputType: true, defOutputsCount: true,
	defOutputType: true, defErrorsMissing: true, defErrorsPartial: true}

func allStructural(kinds []string) bool {
	for _, k := range kinds {
		if !structural[k] {
			return false
		}
	}
	return len(kinds) > 0
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp, err := copyDoc(m)
	if err != nil {
		return m
	}
	return cp
}

// repairDrafts repairs the pending nodes: up to maxDecomposeRepairs passes over
// them, at most maxDecomposeRepairBatch per call, then the structural fallback.
// A node that cannot be repaired ends the stage with an error naming it.
func (p *Planner) repairDrafts(ctx context.Context, r *run, c *contract.Contracts, dec *decomposed, classes map[string]string) error {
	if len(dec.Pending) == 0 {
		return nil
	}
	fn := map[string]contract.Function{}
	for _, f := range c.Functions {
		fn[f.ID] = f
	}
	save := func() error {
		if err := writeJSON(r.path(stateClasses), classes); err != nil {
			return err
		}
		return writeJSON(r.path(stateDecomposed), dec)
	}
	// settle tries a pending node again after a change: true when it is now a draft.
	settle := func(pd *pendingDraft) (bool, error) {
		f, ok := fn[pd.ID]
		if !ok {
			return false, fmt.Errorf("stage decompose: function %s is not in the contract", boundedID(pd.ID))
		}
		if pd.Raw == nil {
			pd.Defects = []string{defMissingNode}
			return false, nil
		}
		d := cloneMap(pd.Raw)
		class, kinds, _, err := newNodeEnv(pd.Component, c, r).normalizeNode(f, d)
		if err != nil {
			return false, err
		}
		if len(kinds) > 0 {
			pd.Defects = kinds
			return false, nil
		}
		dec.Components[pd.Component] = append(dec.Components[pd.Component], d)
		classes[pd.ID] = class
		return true, nil
	}

	pos := func(id string) int {
		for i, pd := range dec.Pending {
			if pd.ID == id {
				return i
			}
		}
		return -1
	}
	for {
		var ids []string
		for _, pd := range dec.Pending {
			if pd.Tries < maxDecomposeRepairs {
				ids = append(ids, pd.ID)
			}
		}
		if len(ids) == 0 {
			break
		}
		for lo := 0; lo < len(ids); lo += maxDecomposeRepairBatch {
			chunk := ids[lo:min(lo+maxDecomposeRepairBatch, len(ids))]
			var replies map[string]map[string]any
			build := func(level int) (string, error) {
				var b strings.Builder
				for _, id := range chunk {
					pd := dec.Pending[pos(id)]
					b.WriteString(fixNodeText(pd, fn[id], level))
				}
				return render("decompose_fix", map[string]string{"Nodes": strings.TrimSpace(b.String()), "Count": fmt.Sprint(len(chunk))})
			}
			cs := callSpec{stage: decomposeFixStage, taskType: "decompose", scope: router.ScopeComponent, maxTokens: maxTokensDecompose}
			err := p.callSized(ctx, r, cs, build, func(text string) error {
				m, err := parseFixReply(StripReply(text))
				if err != nil {
					return err
				}
				replies = m
				return nil
			})
			if err != nil {
				if failedAttempt(ctx, err) {
					for _, id := range chunk {
						dec.Pending[pos(id)].Tries++
					}
					if werr := save(); werr != nil {
						return werr
					}
				}
				return err
			}
			done := map[string]bool{}
			for _, id := range chunk {
				pd := &dec.Pending[pos(id)]
				mergeNodeFix(pd, replies[id])
				pd.Tries++
				ok, err := settle(pd)
				if err != nil {
					return err
				}
				done[id] = ok
			}
			var rest []pendingDraft
			for _, pd := range dec.Pending {
				if !done[pd.ID] {
					rest = append(rest, pd)
				}
			}
			dec.Pending = rest
			if err := save(); err != nil {
				return err
			}
		}
	}

	// The bound is spent: structural fields come from the signature, anything
	// else ends the stage.
	var failed []string
	var defaulted []string
	n := 0
	var rest []pendingDraft
	for _, pd := range dec.Pending {
		f := fn[pd.ID]
		if pd.Raw != nil && allStructural(pd.Defects) {
			if err := defaultLeaf(pd.Raw, f); err == nil {
				ok, err := settle(&pd)
				if err != nil {
					return err
				}
				if ok {
					n++
					if len(defaulted) < maxDefaultedIDsShown {
						defaulted = append(defaulted, boundedID(pd.ID))
					}
					continue
				}
			}
		}
		rest = append(rest, pd)
		failed = append(failed, fmt.Sprintf("%s (%s)", boundedID(pd.ID), strings.Join(pd.Defects, ", ")))
	}
	dec.Pending = rest
	if n > 0 {
		p.emit(events.KindWarning, "decompose", "", fmt.Sprintf(
			"leaf_defaulted: %d nodes still failed the leaf checks after %d repair passes and got their inputs, outputs or errors from the contract's signature (%s)",
			n, maxDecomposeRepairs, strings.Join(defaulted, ", ")))
	}
	if err := save(); err != nil {
		return err
	}
	if len(failed) > 0 {
		shown := failed
		if len(shown) > maxUnresolvedInError {
			shown = append(shown[:maxUnresolvedInError:maxUnresolvedInError], fmt.Sprintf("and %d more", len(failed)-maxUnresolvedInError))
		}
		return fmt.Errorf("stage decompose: %d nodes still fail the leaf checks after %d repair passes: %s",
			len(failed), maxDecomposeRepairs, strings.Join(shown, "; "))
	}
	return nil
}

// parseFixReply reads a repair reply: a JSON array of objects, keyed by id.
func parseFixReply(text string) (map[string]map[string]any, error) {
	var got []map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return nil, fmt.Errorf("decompose repair reply is not a JSON array of nodes (%s)", jsonErr(err))
	}
	out := map[string]map[string]any{}
	for _, d := range got {
		if id, _ := d["id"].(string); id != "" && out[id] == nil {
			out[id] = d
		}
	}
	return out, nil
}

// mergeNodeFix takes from a reply, for one node, only the fields its defects name
// and only a value of the right JSON type; everything else stays as it was.
func mergeNodeFix(pd *pendingDraft, reply map[string]any) {
	if reply == nil {
		return
	}
	if pd.Raw == nil {
		pd.Raw = map[string]any{"id": pd.ID}
	}
	for _, f := range defectFields(pd.Defects) {
		if sub, ok := strings.CutPrefix(f, "contract."); ok {
			rc, _ := reply["contract"].(map[string]any)
			v, ok := rc[sub].([]any)
			if !ok {
				continue
			}
			ct, _ := pd.Raw["contract"].(map[string]any)
			if ct == nil {
				ct = map[string]any{}
				pd.Raw["contract"] = ct
			}
			ct[sub] = v
			continue
		}
		switch v := reply[f].(type) {
		case string:
			if f != "depends_on" && strings.TrimSpace(v) != "" {
				pd.Raw[f] = v
			}
		case []any:
			if f == "depends_on" {
				pd.Raw[f] = v
			}
		}
	}
}

// fixNodeText is one node of a repair prompt: its id, the defects as words, the
// contract entry of the function and the node as it stands. From level 2 only
// the defective fields of the node are shown; at level 3 the contract entry
// loses its doc.
func fixNodeText(pd pendingDraft, f contract.Function, level int) string {
	entry := map[string]any{"id": f.ID, "package": f.Package, "file": f.File, "signature": f.Signature, "uses": f.Uses}
	if level < maxPromptLevel {
		entry["doc"] = f.Doc
	}
	cur := pd.Raw
	if cur == nil {
		cur = map[string]any{}
	} else if level >= 2 {
		cur = map[string]any{"id": pd.ID}
		for _, field := range defectFields(pd.Defects) {
			if sub, ok := strings.CutPrefix(field, "contract."); ok {
				if ct, _ := pd.Raw["contract"].(map[string]any); ct != nil && ct[sub] != nil {
					cc, _ := cur["contract"].(map[string]any)
					if cc == nil {
						cc = map[string]any{}
						cur["contract"] = cc
					}
					cc[sub] = ct[sub]
				}
			} else if v, ok := pd.Raw[field]; ok {
				cur[field] = v
			}
		}
	}
	return fmt.Sprintf("function %q\ndefects: %s\nfields to write: %s\ncontract entry:\n%s\nthe node as it stands:\n%s\n\n",
		pd.ID, strings.Join(pd.Defects, ", "), strings.Join(defectFields(pd.Defects), ", "), mustJSON(entry), mustJSON(cur))
}

// defaultLeaf derives the structural fields of a node from its function's
// signature: an input per parameter, an output per result, an errors entry when
// the function returns error. Titles, descriptions and everything else that
// describes behaviour are never touched.
func defaultLeaf(raw map[string]any, f contract.Function) error {
	fd, err := parseSignature(f.Signature)
	if err != nil {
		return err
	}
	ct, _ := raw["contract"].(map[string]any)
	if ct == nil {
		ct = map[string]any{}
		raw["contract"] = ct
	}
	have := objects(ct["inputs"])
	inputs := []any{}
	if fd.Type.Params != nil {
		k := 0
		for _, field := range fd.Type.Params.List {
			typ := types.ExprString(field.Type)
			names := field.Names
			if len(names) == 0 {
				names = []*ast.Ident{{Name: ""}}
			}
			for _, n := range names {
				k++
				name := n.Name
				if name == "" || name == "_" {
					name = fmt.Sprintf("arg%d", k)
				}
				entry := map[string]any{"name": name, "type": typ, "description": "provided argument"}
				for _, h := range have {
					if h["name"] == name {
						entry = h
						if t, _ := h["type"].(string); strings.TrimSpace(t) == "" {
							h["type"] = typ
						}
					}
				}
				inputs = append(inputs, entry)
			}
		}
	}
	ct["inputs"] = inputs

	outputs := []any{}
	returnsError := false
	if fd.Type.Results != nil {
		k := 0
		for _, field := range fd.Type.Results.List {
			typ := types.ExprString(field.Type)
			isErr := typ == "error"
			names := field.Names
			if len(names) == 0 {
				names = []*ast.Ident{{Name: ""}}
			}
			for _, n := range names {
				k++
				name := n.Name
				switch {
				case name != "" && name != "_":
				case isErr:
					name = "err"
				case k == 1:
					name = "result"
				default:
					name = fmt.Sprintf("result%d", k)
				}
				if isErr {
					returnsError = true
				}
				outputs = append(outputs, map[string]any{"name": name, "type": typ, "description": "returned value"})
			}
		}
	}
	ct["outputs"] = outputs

	errs := []any{}
	for _, e := range objects(ct["errors"]) {
		if w, _ := e["when"].(string); strings.TrimSpace(w) == "" {
			e["when"] = "the function fails"
		}
		if rt, _ := e["returns"].(string); strings.TrimSpace(rt) == "" {
			e["returns"] = "returned error"
		}
		errs = append(errs, e)
	}
	if returnsError && len(errs) == 0 {
		errs = append(errs, map[string]any{"when": "the function fails", "returns": "returned error"})
	}
	ct["errors"] = errs
	return nil
}
