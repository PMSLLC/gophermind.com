// Package orchestrate bridges the phaseflow executor core (internal/phaseflow,
// which stays free of agent/llm/tools dependencies) to a real, fresh
// per-task agent with verify-and-correct. It implements
// phaseflow.TaskRunner.
package orchestrate

import "gophermind/internal/phaseflow"

// resolveModel maps a task's model tier to a concrete model name: "speed" and
// "strong" resolve to the configured speed/strong models; any other value
// (a concrete model name, or empty) passes through, except empty which falls
// back to strongModel as the default.
func resolveModel(tier, speedModel, strongModel string) string {
	switch tier {
	case phaseflow.ModelSpeed:
		return speedModel
	case phaseflow.ModelStrong:
		return strongModel
	case "":
		return strongModel
	default:
		return tier
	}
}
