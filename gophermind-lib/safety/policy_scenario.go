package safety

import (
	"encoding/json"
	"fmt"
	"os"
)

// PolicyScenario is a single policy-as-code test case: given a policy, a command
// or tool should produce an expected decision. This lets a team prove its
// .gophermind/policy guardrails behave as intended.
type PolicyScenario struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`              // "command" or "tool"
	Command string `json:"command,omitempty"` // for kind=command
	Tool    string `json:"tool,omitempty"`    // for kind=tool
	// Expect is the asserted decision: for commands "allow"/"deny"; for tools the
	// approval policy "always"/"ask"/"never".
	Expect string `json:"expect"`
}

// PolicyScenarioResult is the outcome of evaluating one scenario.
type PolicyScenarioResult struct {
	Name   string
	Expect string
	Got    string
	Pass   bool
}

// LoadPolicyScenarios reads a JSON file of the form {"scenarios":[...]}.
func LoadPolicyScenarios(path string) ([]PolicyScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenarios: %w", err)
	}
	var doc struct {
		Scenarios []PolicyScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse scenarios: %w", err)
	}
	return doc.Scenarios, nil
}

// RunPolicyScenarios evaluates each scenario against p and returns the results.
func RunPolicyScenarios(p *Policy, scenarios []PolicyScenario) []PolicyScenarioResult {
	results := make([]PolicyScenarioResult, 0, len(scenarios))
	for _, s := range scenarios {
		got := evaluateScenario(p, s)
		results = append(results, PolicyScenarioResult{
			Name:   s.Name,
			Expect: s.Expect,
			Got:    got,
			Pass:   got == s.Expect,
		})
	}
	return results
}

// evaluateScenario returns the decision the policy makes for one scenario.
func evaluateScenario(p *Policy, s PolicyScenario) string {
	switch s.Kind {
	case "command":
		if CheckPolicy(p, s.Command) == nil {
			return "allow"
		}
		return "deny"
	case "tool":
		return p.ToolApprovalPolicy(s.Tool)
	default:
		return "unknown-kind:" + s.Kind
	}
}
