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

// RoleTools resolves a role name to its allowed tool list from a roles map. A
// role containing "*" (or an unknown role) returns nil, meaning "no
// restriction" for the wildcard and "no access defined" for unknown — callers
// treat nil as unrestricted, so only define restrictive roles.
func RoleTools(roles map[string][]string, role string) []string {
	tools, ok := roles[role]
	if !ok {
		return nil
	}
	for _, t := range tools {
		if t == "*" {
			return nil // wildcard: unrestricted
		}
	}
	return tools
}
