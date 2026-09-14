package safety

import "testing"

func TestRunPolicyScenariosCommand(t *testing.T) {
	p := &Policy{AllowList: []string{"ls", "git status"}}
	scenarios := []PolicyScenario{
		{Name: "ls allowed", Kind: "command", Command: "ls -la", Expect: "allow"},
		{Name: "rm denied", Kind: "command", Command: "rm -rf /", Expect: "deny"},
		{Name: "git allowed", Kind: "command", Command: "git status", Expect: "allow"},
	}
	results := RunPolicyScenarios(p, scenarios)
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Pass {
			t.Errorf("scenario %q failed: got %q, want %q", r.Name, r.Got, r.Expect)
		}
	}
}

func TestRunPolicyScenariosTool(t *testing.T) {
	p := &Policy{GatedTools: map[string]string{"write_file": "always", "run_shell": "never"}}
	scenarios := []PolicyScenario{
		{Name: "write auto", Kind: "tool", Tool: "write_file", Expect: "always"},
		{Name: "shell denied", Kind: "tool", Tool: "run_shell", Expect: "never"},
		{Name: "unknown asks", Kind: "tool", Tool: "edit_file", Expect: "ask"},
	}
	for _, r := range RunPolicyScenarios(p, scenarios) {
		if !r.Pass {
			t.Errorf("scenario %q failed: got %q, want %q", r.Name, r.Got, r.Expect)
		}
	}
}

func TestRunPolicyScenariosDetectsMismatch(t *testing.T) {
	p := &Policy{AllowList: []string{"ls"}}
	// Assert an intentionally wrong expectation is reported as a failure.
	results := RunPolicyScenarios(p, []PolicyScenario{
		{Name: "bad", Kind: "command", Command: "rm -rf /", Expect: "allow"},
	})
	if results[0].Pass {
		t.Error("a mismatched scenario should be reported as failing")
	}
}

func TestRunPolicyScenariosUnknownKind(t *testing.T) {
	results := RunPolicyScenarios(&Policy{}, []PolicyScenario{{Name: "x", Kind: "bogus", Expect: "allow"}})
	if results[0].Pass {
		t.Error("an unknown scenario kind should not pass")
	}
}
