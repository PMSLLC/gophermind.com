package planner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"gophermind/gophermind-lib/build/schema"
)

// A model's node often differs from the schema only in shape: side_effects as
// objects where strings are wanted, a string where an array is wanted, null for
// an empty array, an enum in the wrong case, a property the schema forbids. Such
// deviations are normalised by code before the checks run, each one noted so a
// run shows how much was reshaped; what cannot be normalised is reported to the
// repair by field and keyword.

var (
	schemaNamesOnce sync.Once
	nodeTopProps    = map[string]bool{}
	nodeContractKey = map[string]bool{}
	nodeAllNames    = map[string]bool{}
	nodeTierEnum    []string
)

func loadSchemaNames() {
	schemaNamesOnce.Do(func() {
		raw, err := schema.Raw(schema.KindNode)
		if err != nil {
			return
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			return
		}
		var walk func(v any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if props, ok := x["properties"].(map[string]any); ok {
					for k := range props {
						nodeAllNames[k] = true
					}
				}
				for _, c := range x {
					walk(c)
				}
			case []any:
				for _, c := range x {
					walk(c)
				}
			}
		}
		walk(doc)
		props, _ := doc["properties"].(map[string]any)
		for k := range props {
			nodeTopProps[k] = true
		}
		if ct, ok := props["contract"].(map[string]any); ok {
			if cp, ok := ct["properties"].(map[string]any); ok {
				for k := range cp {
					nodeContractKey[k] = true
				}
			}
		}
		if mt, ok := props["model_tier"].(map[string]any); ok {
			for _, e := range mt["enum"].([]any) {
				nodeTierEnum = append(nodeTierEnum, fmt.Sprint(e))
			}
		}
	})
}

// sideEffectText is a side effect that arrived as an object, as one sentence.
func sideEffectText(m map[string]any) string {
	var parts []string
	typ, _ := m["type"].(string)
	target, _ := m["target"].(string)
	if s := strings.TrimSpace(typ + " " + target); s != "" {
		parts = append(parts, s)
	}
	text := strings.Join(parts, "")
	if d, _ := m["description"].(string); strings.TrimSpace(d) != "" {
		if text != "" {
			text += ": "
		}
		text += strings.TrimSpace(d)
	}
	if text == "" {
		text = "side effect"
	}
	return text
}

// asArray returns v as an array for a field that wants one: null is empty, a
// single value is wrapped. changed says whether the shape was altered.
func asArray(v any, present bool) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, false
	case nil:
		return []any{}, present
	}
	return []any{v}, true
}

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// titleFromName is "Validate task assignee" from ValidateTaskAssignee.
func titleFromName(name string) string {
	s := strings.ToLower(camelBoundary.ReplaceAllString(name, "$1 $2"))
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// normalizeShape reshapes the benign deviations of one node in place. note
// records each change by field name and kind, never by content.
func normalizeShape(d map[string]any, fnName string, note func(field, what string)) {
	loadSchemaNames()
	for k := range d {
		if !nodeTopProps[k] {
			delete(d, k)
			note("(extra property)", "dropped")
		}
	}
	if v, present := d["model_tier"]; present {
		s, _ := v.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		if contains(nodeTierEnum, s) {
			if s != v {
				d["model_tier"] = s
				note("model_tier", "case")
			}
		} else {
			d["model_tier"] = "standard"
			note("model_tier", "defaulted")
		}
	}
	if arr, changed := asArray(d["depends_on"], d["depends_on"] == nil && hasKey(d, "depends_on")); hasKey(d, "depends_on") && changed {
		d["depends_on"] = arr
		note("depends_on", "array")
	}
	if arr, ok := d["depends_on"].([]any); ok {
		kept := make([]any, 0, len(arr))
		for _, x := range arr {
			if s, isStr := x.(string); isStr && strings.Contains(s, ".") {
				continue // a Go identifier (os.File), not a contract id: ids have no dots
			}
			kept = append(kept, x)
		}
		if len(kept) != len(arr) {
			d["depends_on"] = kept
			note("depends_on", "dropped")
		}
	}
	if t, present := d["title"]; (!present || strings.TrimSpace(fmt.Sprint(orEmpty(t))) == "") && fnName != "" {
		if _, isString := t.(string); isString || !present || t == nil {
			d["title"] = titleFromName(fnName)
			note("title", "derived")
		}
	}
	if d["context"] == nil && hasKey(d, "context") {
		d["context"] = map[string]any{}
		note("context", "object")
	}
	ct, ok := d["contract"].(map[string]any)
	if !ok {
		return
	}
	for k := range ct {
		if !nodeContractKey[k] {
			delete(ct, k)
			note("contract.(extra property)", "dropped")
		}
	}
	for _, field := range []string{"inputs", "outputs", "errors", "side_effects"} {
		v, present := ct[field]
		if !present {
			continue
		}
		if m, isObj := v.(map[string]any); isObj && field == "side_effects" {
			ct[field] = []any{sideEffectText(m)}
			note("contract.side_effects", "string")
			continue
		}
		arr, changed := asArray(v, true)
		if changed {
			ct[field] = arr
			note("contract."+field, "array")
		}
		if field == "errors" {
			for _, e := range arr {
				if m, isObj := e.(map[string]any); isObj && scalarsToText(m, "when", "returns") {
					note("contract.errors", "text")
				}
			}
		}
		if field == "inputs" || field == "outputs" {
			if nameParams(arr, field == "outputs") {
				note("contract."+field, "named")
			}
		}
		if field == "side_effects" {
			for i, e := range arr {
				if m, isObj := e.(map[string]any); isObj {
					arr[i] = sideEffectText(m)
					note("contract.side_effects", "string")
				}
			}
		}
	}
}

// nameParams gives a name to each input or output object that has none: the
// schema needs one, and Go results are often unnamed. A lone output is "result",
// several are "result1", "result2"; inputs are "arg1", "arg2" by position. It
// reports whether it named any.
func nameParams(arr []any, output bool) bool {
	named := false
	for i, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); strings.TrimSpace(n) != "" {
			continue
		}
		switch {
		case output && len(arr) == 1:
			m["name"] = "result"
		case output:
			m["name"] = fmt.Sprintf("result%d", i+1)
		default:
			m["name"] = fmt.Sprintf("arg%d", i+1)
		}
		named = true
	}
	return named
}

// scalarsToText turns a number or a boolean under the given keys into its text,
// so {"returns": 2} reads as "2". It reports whether it changed any.
func scalarsToText(m map[string]any, keys ...string) bool {
	changed := false
	for _, k := range keys {
		switch v := m[k].(type) {
		case json.Number:
			m[k] = v.String()
			changed = true
		case float64:
			m[k] = strconv.FormatFloat(v, 'f', -1, 64)
			changed = true
		case int:
			m[k] = strconv.Itoa(v)
			changed = true
		case bool:
			m[k] = strconv.FormatBool(v)
			changed = true
		}
	}
	return changed
}

// coerceToText makes each of the given fields of m text: a number or boolean is
// its text, and a list is its elements joined with ", " (["a", "b"] is "a, b").
// A string, a missing field and any other shape are left as they are. It
// reports whether it changed any.
func coerceToText(m map[string]any, keys ...string) bool {
	changed := scalarsToText(m, keys...)
	for _, k := range keys {
		arr, ok := m[k].([]any)
		if !ok {
			continue
		}
		parts := make([]string, 0, len(arr))
		for _, x := range arr {
			switch v := x.(type) {
			case string:
				parts = append(parts, v)
			case json.Number:
				parts = append(parts, v.String())
			case float64:
				parts = append(parts, strconv.FormatFloat(v, 'f', -1, 64))
			case bool:
				parts = append(parts, strconv.FormatBool(v))
			default:
				parts = append(parts, fmt.Sprint(v))
			}
		}
		m[k] = strings.Join(parts, ", ")
		changed = true
	}
	return changed
}

func hasKey(m map[string]any, k string) bool { _, ok := m[k]; return ok }

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// schemaDetails names, in a fixed vocabulary, where a node fails its schema:
// "field:<path> keyword:<schema keyword>". A path uses [] for an index and only
// property names the schema itself knows; at most 5 are returned.
func schemaDetails(ve *jsonschema.ValidationError) []string {
	loadSchemaNames()
	var out []string
	seen := map[string]bool{}
	add := func(path []string, kw string) {
		var b strings.Builder
		for _, seg := range path {
			switch {
			case seg != "" && seg[0] >= '0' && seg[0] <= '9':
				b.WriteString("[]")
			case nodeAllNames[seg]:
				if b.Len() > 0 {
					b.WriteByte('.')
				}
				b.WriteString(seg)
			default:
				if b.Len() > 0 {
					b.WriteByte('.')
				}
				b.WriteByte('?')
			}
		}
		line := "field:" + b.String() + " keyword:" + kw
		if b.Len() == 0 {
			line = "field:(node) keyword:" + kw
		}
		if !seen[line] && len(out) < 5 {
			seen[line] = true
			out = append(out, line)
		}
	}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		name := strings.TrimPrefix(fmt.Sprintf("%T", e.ErrorKind), "*kind.")
		kw := strings.ToLower(name[:1]) + name[1:]
		if kw == "" || len(kw) > 30 {
			kw = "other"
		}
		add(e.InstanceLocation, kw)
	}
	walk(ve)
	return out
}
