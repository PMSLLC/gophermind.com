package planner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A model asked for a patch often answers with the whole node, the way the
// node is shown to it. parseReviseReplyFor reads such a reply leniently: what
// the reply repeats unchanged is dropped, the keys of an item that are code's
// (go_type, test links, ...) are dropped, and what is left is the patch. A
// change to a field a rewrite may not touch is not dropped: the strict parser
// refuses it and names the key.

// patchItemKeys are the keys a model may write in an item of each contract list.
var patchItemKeys = map[string][]string{
	"inputs":  {"name", "type", "description", "constraints"},
	"outputs": {"name", "type", "description", "constraints"},
	"errors":  {"when", "returns", "kind"},
}

// nodeFieldKeys are the fixed names of node fields and item fields that a
// refusal may quote; any other key of a reply is never quoted.
var nodeFieldKeys = []string{"id", "title", "node_class", "model_tier", "depends_on", "wave", "tests", "parent",
	"decision_ids", "rationale", "open_questions", "revision", "signature", "file", "package", "dependency_signatures",
	"go_type", "nilable", "units", "empty_behavior", "test"}

// parseReviseReplyFor is parseReviseReplyScanned after the whole-document
// leniency. The warnings name what was dropped (field names only, never reply
// text).
func parseReviseReplyFor(text string, secrets []string, orig map[string]any) (reviseReply, []string, error) {
	norm, warns := normalizeWholeDoc(text, orig)
	rep, err := parseReviseReplyScanned(norm, secrets)
	if err != nil && len(warns) > 0 && classOf(err, "") == ClassReviseInvalid && strings.Contains(err.Error(), "changes nothing") {
		// Every key the reply wrote repeats the current node.
		return rep, warns, &reviseErr{ClassReviseNoop, "the rewrite equals the current node"}
	}
	return rep, warns, err
}

func normalizeWholeDoc(text string, orig map[string]any) (string, []string) {
	t := strings.TrimSpace(text)
	if first, _, _ := strings.Cut(t, "\n"); strings.HasPrefix(strings.TrimSpace(first), "CONTRACT_") || strings.TrimSpace(first) == "SPLIT_REQUIRED" {
		return text, nil
	}
	body := StripReply(t)
	if len(body) > maxReplyBytes || scanReplyJSON(body) != nil {
		return text, nil
	}
	var doc map[string]any
	if json.Unmarshal([]byte(body), &doc) != nil || doc == nil {
		return text, nil
	}
	var warns []string
	drop := func(path string) {
		warns = append(warns, "left out "+path+": unchanged or not a field a rewrite may write")
	}

	for k, v := range doc {
		switch k {
		case "description":
			if sameJSON(v, orig[k]) {
				delete(doc, k)
				drop(k)
			}
		case "contract":
			if m, ok := v.(map[string]any); ok {
				origC, _ := orig["contract"].(map[string]any)
				warns = append(warns, pruneContract(m, origC)...)
				if len(m) == 0 {
					delete(doc, k)
				}
			}
		case "context":
			if m, ok := v.(map[string]any); ok {
				origC, _ := orig["context"].(map[string]any)
				for ck, cv := range m {
					if sameJSON(cv, origC[ck]) {
						delete(m, ck)
						drop("context." + ck)
					}
				}
				if len(m) == 0 {
					delete(doc, k)
				}
			}
		case "groups":
			if m, ok := v.(map[string]any); ok {
				for gk, gv := range m {
					if sameJSON(gv, orig[gk]) {
						delete(m, gk)
						drop("groups." + gk)
					}
				}
				if len(m) == 0 {
					delete(doc, k)
				}
			}
		case "rewrite_tests", "assumptions":
		default:
			if ov, ok := orig[k]; ok && sameJSON(v, ov) {
				delete(doc, k)
				drop(k)
			}
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return text, nil
	}
	sort.Strings(warns)
	return string(out), warns
}

// pruneContract drops the contract keys a reply repeats unchanged and the item
// keys that are code's, and returns what it dropped.
func pruneContract(m, orig map[string]any) []string {
	var warns []string
	for k, v := range m {
		keys, isList := patchItemKeys[k]
		if !isList {
			if sameJSON(v, orig[k]) {
				delete(m, k)
				warns = append(warns, "left out contract."+k+": unchanged")
			}
			continue
		}
		items, ok := v.([]any)
		if !ok {
			continue
		}
		stripped, cut := stripItems(items, keys)
		if cut {
			warns = append(warns, fmt.Sprintf("dropped the keys of contract.%s items that code writes", k))
		}
		origItems, _ := orig[k].([]any)
		if o, _ := stripItems(origItems, keys); sameJSON(stripped, o) {
			delete(m, k)
			warns = append(warns, "left out contract."+k+": unchanged")
			continue
		}
		m[k] = stripped
	}
	return warns
}

// stripItems keeps only the allowed keys of each object of items. Anything
// that is not an object is returned as it is, for the strict parser to refuse.
func stripItems(items []any, allowed []string) ([]any, bool) {
	out := make([]any, len(items))
	cut := false
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			out[i] = it
			continue
		}
		nm := map[string]any{}
		for k, v := range m {
			if contains(allowed, k) {
				nm[k] = v
			} else {
				cut = true
			}
		}
		out[i] = nm
	}
	return out, cut
}

var unknownFieldRE = regexp.MustCompile(`^json: unknown field "([^"]*)"`)

// unknownFieldText is the refusal for a key the patch does not hold. The key is
// quoted only when it is one of the fixed node field names.
func unknownFieldText(err error) (string, bool) {
	m := unknownFieldRE.FindStringSubmatch(err.Error())
	if m == nil {
		return "", false
	}
	what := "a key"
	if contains(nodeFieldKeys, m[1]) || contains(replyKeys, m[1]) {
		what = "the key " + m[1]
	}
	return what + " that a patch may not hold: write only what you change, and leave out unchanged fields and the signature, file, package and tests", true
}
