package planner

import (
	"bytes"
	"encoding/json"
	"strings"
)

// unwrapStringifiedItems rewrites, in the JSON object text, each element of the
// named arrays that is a string holding exactly one JSON object into that
// object: ["{\"a\":1}"] becomes [{"a":1}]. A model that double-encodes an item
// (the first one right, the rest as text) makes the reply refuse for its type.
// Only a string that is valid JSON starting with "{" is unwrapped, and only in
// arrays the caller names, so a text field that holds JSON is never touched.
// Text that is not a JSON object, or has nothing to unwrap, is returned as is.
func unwrapStringifiedItems(text string, arrays ...string) string {
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &top) != nil {
		return text
	}
	changed := false
	for _, name := range arrays {
		raw, ok := top[name]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		for i, it := range items {
			var s string
			if json.Unmarshal(it, &s) != nil {
				continue
			}
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "{") && json.Valid([]byte(s)) {
				items[i] = json.RawMessage(s)
				changed = true
			}
		}
		if changed {
			if b, err := json.Marshal(items); err == nil {
				top[name] = b
			}
		}
	}
	if !changed {
		return text
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(top) != nil {
		return text
	}
	return strings.TrimSpace(out.String())
}
