package safety

import "testing"

func TestPolicyApprovalPerTool(t *testing.T) {
	p := &Policy{GatedTools: map[string]string{
		"read_file":  "always",
		"run_shell":  "never",
		"write_file": "ask",
	}}
	fallbackCalls := 0
	fallback := func(tool, args string) bool { fallbackCalls++; return true }

	approve := PolicyApproval(p, fallback)

	// "always" auto-approves without consulting the fallback.
	if !approve("read_file", "{}") {
		t.Error("always-policy tool should be approved")
	}
	// "never" denies without consulting the fallback.
	if approve("run_shell", "{}") {
		t.Error("never-policy tool should be denied")
	}
	if fallbackCalls != 0 {
		t.Errorf("fallback consulted for always/never; calls=%d", fallbackCalls)
	}
	// "ask" defers to the fallback.
	if !approve("write_file", "{}") {
		t.Error("ask-policy tool should defer to fallback (which approves)")
	}
	if fallbackCalls != 1 {
		t.Errorf("ask should consult fallback exactly once; calls=%d", fallbackCalls)
	}
}

func TestPolicyApprovalDefaultsToAsk(t *testing.T) {
	p := &Policy{GatedTools: map[string]string{}}
	called := false
	approve := PolicyApproval(p, func(tool, args string) bool { called = true; return false })
	if approve("edit_file", "{}") {
		t.Error("unlisted tool should defer to fallback (which denies)")
	}
	if !called {
		t.Error("unlisted tool should consult the fallback")
	}
}

func TestPolicyApprovalNilPolicyIsFallback(t *testing.T) {
	approve := PolicyApproval(nil, func(tool, args string) bool { return true })
	if !approve("write_file", "{}") {
		t.Error("nil policy should pass through to the fallback")
	}
}
