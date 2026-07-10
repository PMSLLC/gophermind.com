package safety

// RoleGate returns an ApprovalFunc that enforces role-based access control: a
// tool NOT in the allowed set is denied outright (least privilege), and a tool
// in the set defers to the fallback approval decision. An empty/nil allowed set
// imposes no restriction (fallback decides everything).
func RoleGate(allowed []string, fallback ApprovalFunc) ApprovalFunc {
	if len(allowed) == 0 {
		return fallback
	}
	set := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		set[t] = true
	}
	return func(tool, args string) bool {
		if !set[tool] {
			return false
		}
		return fallback(tool, args)
	}
}

// RoleTools resolves a role name to its allowed tool list from a roles map.
// The second return value reports whether the role is KNOWN — callers must fail
// closed (deny) on an unknown role rather than treating it as unrestricted. A
// known role containing "*" returns (nil, true), meaning "known + unrestricted".
func RoleTools(roles map[string][]string, role string) (allowed []string, known bool) {
	tools, ok := roles[role]
	if !ok {
		return nil, false // unknown role: caller must fail closed
	}
	for _, t := range tools {
		if t == "*" {
			return nil, true // known wildcard: unrestricted
		}
	}
	return tools, true
}
