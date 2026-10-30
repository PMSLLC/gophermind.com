package planner

// alwaysNAGroups are the groups that may be not applicable for every node
// class (naAllowed decides the others by class).
var alwaysNAGroups = []string{"alternatives", "portability", "refactor_notes", "profile_hooks"}

// emptyGroupsNotApplicable writes the not-applicable object for each of those
// groups the reply gave as an empty list, which the schema refuses. The prompt
// asks for the object; a model wrote [] for "nothing to say" and two repairs
// did not change it. It returns the groups it rewrote.
func emptyGroupsNotApplicable(m map[string]any) []string {
	var changed []string
	for _, g := range alwaysNAGroups {
		if l, ok := m[g].([]any); ok && len(l) == 0 {
			m[g] = map[string]any{"not_applicable": "The reply listed nothing for this group, so it does not apply here."}
			changed = append(changed, g)
		}
	}
	return changed
}

// errorKindValues are the kinds a contract error may have.
var errorKindValues = []string{"sentinel", "wrapped", "typed", "panic", "http_status", "exit_code", "other"}

// errorKindList reads a reply's error_kinds. A kind written as the only key of
// an object, {"exit_code": 1}, is taken as that kind; a list holding anything
// else that is not a string reads as no list, so the count check names it.
func errorKindList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		switch e := x.(type) {
		case string:
			out = append(out, e)
		case map[string]any:
			if len(e) != 1 {
				return nil
			}
			for k := range e {
				if !contains(errorKindValues, k) {
					return nil
				}
				out = append(out, k)
			}
		default:
			return nil
		}
	}
	return out
}

// fitErrorKinds gives each contract error one kind: the reply's kind at its
// position, else the error's own valid kind, else "other". Kinds beyond the
// errors are dropped.
func fitErrorKinds(kinds []string, errs []map[string]any) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		switch k, _ := e["kind"].(string); {
		case i < len(kinds):
			out[i] = kinds[i]
		case contains(errorKindValues, k):
			out[i] = k
		default:
			out[i] = "other"
		}
	}
	return out
}

// securityNoBoundary turns a not-applicable security group into the explicit
// answer "no trust boundary" for a class that may not dismiss the group, as
// observabilityNoLogs does for logs: Gemma kept writing not applicable for a
// handler that only writes its output (run gm-2026-10-30-049). It reports
// whether it changed the group.
func securityNoBoundary(m map[string]any, class string) bool {
	if _, na := notApplicable(m["security"]); !na || naAllowed("security", class) {
		return false
	}
	m["security"] = map[string]any{"trust_boundary": "none"}
	return true
}

// groupListFields are the fields inside a group that the node schema types as
// lists.
var groupListFields = map[string][]string{
	"security":      {"untrusted_inputs", "secret_use", "threats"},
	"observability": {"log_events", "metrics"},
	"portability":   {"os", "arch", "build_tags", "deps"},
}

// notApplicableListFields turns a not-applicable object written for one list
// field inside a group ("metrics": {"not_applicable": "..."}) into an empty
// list: not applicable is a whole-group answer, and a model kept writing it for
// one field through every repair. It returns the fields it changed.
func notApplicableListFields(m map[string]any) []string {
	var changed []string
	for _, g := range []string{"security", "observability", "portability"} {
		grp, ok := m[g].(map[string]any)
		if !ok {
			continue
		}
		for _, f := range groupListFields[g] {
			if v, ok := grp[f].(map[string]any); ok {
				if _, na := v["not_applicable"]; na && len(v) == 1 {
					grp[f] = []any{}
					changed = append(changed, g+"."+f)
				}
			}
		}
	}
	return changed
}

// observabilityNoLogs turns a not-applicable observability group into the
// explicit answer "no log events, no metrics, no span" for a class that may not
// dismiss the group. That answer passes the checks and says the same thing;
// a model kept writing not applicable for a wiring function through every
// repair. It reports whether it changed the group.
func observabilityNoLogs(m map[string]any, class string) bool {
	if _, na := notApplicable(m["observability"]); !na || naAllowed("observability", class) {
		return false
	}
	m["observability"] = map[string]any{"log_events": []any{}, "metrics": []any{}, "trace_span": nil}
	return true
}
