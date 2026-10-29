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

// contractErrorKinds returns the kind of each error when the contract already
// gives every one a valid kind, so a reply that leaves error_kinds out keeps
// them instead of failing the group check.
func contractErrorKinds(errs []map[string]any) ([]string, bool) {
	kinds := make([]string, 0, len(errs))
	for _, e := range errs {
		k, _ := e["kind"].(string)
		if !contains(errorKindValues, k) {
			return nil, false
		}
		kinds = append(kinds, k)
	}
	return kinds, true
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
