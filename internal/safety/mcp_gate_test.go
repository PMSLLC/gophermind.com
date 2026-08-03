package safety

import "testing"

// MCP tools are namespaced "server__tool" and their names are not known at
// compile time, so the hardcoded switch in Gated would let every one of them
// through ungated. They must fail closed instead: a server gophermind does not
// control can change its tool set between runs.
func TestGatedCoversMCPTools(t *testing.T) {
	for _, name := range []string{
		"pelagosnow__search",
		"filesystem__write_file",
		"anything__at_all",
	} {
		if !Gated(name) {
			t.Errorf("MCP tool %q must be gated by default", name)
		}
	}
}

// The namespace rule must not accidentally gate or un-gate builtins.
func TestGatedBuiltinsUnchanged(t *testing.T) {
	for _, name := range []string{"write_file", "run_shell", "edit_file", "fetch_url"} {
		if !Gated(name) {
			t.Errorf("builtin %q should still be gated", name)
		}
	}
	for _, name := range []string{"read_file", "search", "list_dir"} {
		if Gated(name) {
			t.Errorf("read-only builtin %q should still be ungated", name)
		}
	}
}
