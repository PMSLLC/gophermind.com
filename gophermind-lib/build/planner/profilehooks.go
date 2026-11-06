package planner

import "strings"

// benchWords are what a requirement says when it asks for performance or a
// benchmark, matched case-insensitively.
var benchWords = []string{"benchmark", "performance", "latency", "throughput", "faster than", "per second"}

// benchWanted reports whether any requirement, of any kind, asks for
// performance or a benchmark. Without one a bench profile hook has no reason:
// the hello run's fn-greeting got bench from the enrich model with no
// performance requirement in the brief.
func benchWanted(reqs []Requirement) bool {
	for _, r := range reqs {
		text := strings.ToLower(r.Name + " " + r.Text)
		for _, w := range benchWords {
			if strings.Contains(text, w) {
				return true
			}
		}
	}
	return false
}

// dropBench removes bench from a profile_hooks list; with nothing left the
// group is the not_applicable object. It reports whether it removed bench.
func dropBench(d map[string]any) bool {
	list, ok := d["profile_hooks"].([]any)
	if !ok {
		return false
	}
	kept := make([]any, 0, len(list))
	for _, e := range list {
		if s, _ := e.(string); s != "bench" {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(list) {
		return false
	}
	if len(kept) == 0 {
		d["profile_hooks"] = map[string]any{"not_applicable": profileHooksNotApplicable}
	} else {
		d["profile_hooks"] = kept
	}
	return true
}

// profileHookNames are the values the node schema allows in a profile_hooks list.
var profileHookNames = map[string]bool{"bench": true, "pprof": true, "trace": true}

const profileHooksNotApplicable = "No profiling hook applies: this function is not a hot path worth measuring on its own."

// normalizeProfileHooks puts a profile_hooks group in the form the schema allows:
// a not_applicable object with a reason, or a non-empty list of bench, pprof and
// trace without repeats. A model meaning "none" wrote [] or ["not_applicable"],
// which the schema refuses, and it did not change that in two repairs. Names that
// are not hooks are dropped; with none left, the group is the not_applicable
// object. A group that is absent or already valid is left alone. It reports
// whether it changed anything.
func normalizeProfileHooks(d map[string]any) bool {
	v, present := d["profile_hooks"]
	if !present {
		return false
	}
	switch x := v.(type) {
	case map[string]any:
		return false
	case []any:
		kept := make([]any, 0, len(x))
		seen := map[string]bool{}
		for _, e := range x {
			if s, _ := e.(string); profileHookNames[s] && !seen[s] {
				seen[s] = true
				kept = append(kept, s)
			}
		}
		if len(x) > 0 && len(kept) == len(x) {
			return false
		}
		if len(kept) == 0 {
			d["profile_hooks"] = map[string]any{"not_applicable": profileHooksNotApplicable}
		} else {
			d["profile_hooks"] = kept
		}
	case string:
		if profileHookNames[x] {
			d["profile_hooks"] = []any{x}
		} else {
			d["profile_hooks"] = map[string]any{"not_applicable": profileHooksNotApplicable}
		}
	default:
		d["profile_hooks"] = map[string]any{"not_applicable": profileHooksNotApplicable}
	}
	return true
}
