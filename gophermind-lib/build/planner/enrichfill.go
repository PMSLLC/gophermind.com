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
