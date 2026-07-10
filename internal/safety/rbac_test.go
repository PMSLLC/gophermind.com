package safety

import "testing"

func TestRoleGateRestrictsTools(t *testing.T) {
	// reviewer may only read/search; everything else denied even if fallback allows.
	allow := []string{"read_file", "search"}
	gate := RoleGate(allow, Auto) // Auto approves everything

	if !gate("read_file", "{}") {
		t.Error("read_file should be allowed for the role")
	}
	if gate("run_shell", "{}") {
		t.Error("run_shell is not in the role's allowlist and must be denied")
	}
	if gate("write_file", "{}") {
		t.Error("write_file must be denied for a read-only role")
	}
}

func TestRoleGateEmptyMeansUnrestricted(t *testing.T) {
	// An empty allowlist imposes no role restriction; fallback decides.
	gate := RoleGate(nil, func(string, string) bool { return false })
	if gate("run_shell", "{}") {
		t.Error("with no role restriction, the fallback (deny) should decide")
	}
	gate2 := RoleGate(nil, Auto)
	if !gate2("run_shell", "{}") {
		t.Error("with no role restriction + Auto fallback, tool should be allowed")
	}
}

func TestRoleTools(t *testing.T) {
	roles := map[string][]string{"reviewer": {"read_file", "search"}, "operator": {"*"}}
	if got := RoleTools(roles, "reviewer"); len(got) != 2 {
		t.Errorf("reviewer tools = %v", got)
	}
	if got := RoleTools(roles, "unknown"); got != nil {
		t.Errorf("unknown role should return nil, got %v", got)
	}
	// "*" means unrestricted -> nil (no restriction).
	if got := RoleTools(roles, "operator"); got != nil {
		t.Errorf("wildcard role should be unrestricted (nil), got %v", got)
	}
}
