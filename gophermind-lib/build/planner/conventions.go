package planner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// conventionLists and conventionTexts are the conventions fields the schema
// wants as a list of text lines and as one text.
var (
	conventionLists = []string{"layout", "naming"}
	conventionTexts = []string{"errors", "logging", "testing"}
)

// normalizeConventions reshapes the benign deviations of a conventions block in
// place: a list field written as one string or an object becomes lines
// ("package: pkg"), a text field written as a list or an object or a scalar
// becomes one text. A model wrote naming as an object and the schema refused it
// three times. It reports whether it changed anything.
func normalizeConventions(c map[string]any) bool {
	changed := false
	for _, k := range conventionLists {
		switch v := c[k].(type) {
		case string:
			c[k] = []any{v}
			changed = true
		case map[string]any:
			c[k] = anyLines(lines(v))
			changed = true
		case []any:
			for i, x := range v {
				if _, ok := x.(string); !ok {
					v[i] = jsonText(x)
					changed = true
				}
			}
		}
	}
	for _, k := range conventionTexts {
		switch v := c[k].(type) {
		case string, nil:
		case []any:
			parts := make([]string, 0, len(v))
			for _, x := range v {
				parts = append(parts, jsonText(x))
			}
			c[k] = strings.Join(parts, "; ")
			changed = true
		case map[string]any:
			c[k] = strings.Join(lines(v), "; ")
			changed = true
		default:
			c[k] = jsonText(v)
			changed = true
		}
	}
	return changed
}

// lines is the entries of an object as "key: value", sorted by key.
func lines(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+": "+jsonText(m[k]))
	}
	return out
}

func anyLines(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// jsonText is the text of a decoded JSON value.
func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
