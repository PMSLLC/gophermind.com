package project

import (
	"fmt"
	"regexp"
	"strings"
)

// lintLongBytes is the size past which injected instructions are flagged as
// overly long (they crowd out the task and dilute attention).
const lintLongBytes = 6000

// alwaysNeverRe captures an "always|never" directive plus the next two words,
// so opposing directives about the same object can be detected.
var alwaysNeverRe = regexp.MustCompile(`(?i)\b(always|never)\s+([a-z]+\s+[a-z]+)`)

// LintInstructions returns advisory warnings about the composed instruction
// text: it flags overly long instructions (against the fixed lintLongBytes
// fallback) and simple always/never contradictions about the same object.
// Warnings are non-fatal — they surface prompt bloat or conflicts a user may
// want to fix. Callers that know the model's actual byte budget should use
// LintInstructionsBudget instead, so the warning reflects real truncation
// rather than a threshold unaware of the model in use.
func LintInstructions(text string) []string {
	return lintInstructions(text, lintLongBytes)
}

// LintInstructionsBudget is like LintInstructions but flags length against
// budgetBytes — the model's actual injected-context budget — instead of the
// fixed fallback. Pass origLen, the pre-truncation byte length of the
// composed instructions, so the warning reports real truncation (content that
// did not fit) rather than firing on content that fits comfortably in a large
// model's window. A non-positive budgetBytes falls back to lintLongBytes.
func LintInstructionsBudget(text string, origLen, budgetBytes int) []string {
	threshold := budgetBytes
	if threshold <= 0 {
		threshold = lintLongBytes
	}
	var warns []string
	if strings.TrimSpace(text) == "" {
		return warns
	}
	if origLen > threshold {
		warns = append(warns, fmt.Sprintf("instructions were truncated to fit the model's context budget (%d bytes composed, %d byte budget); consider trimming", origLen, threshold))
	}
	warns = append(warns, lintContradictions(text)...)
	return warns
}

func lintInstructions(text string, threshold int) []string {
	var warns []string
	if strings.TrimSpace(text) == "" {
		return warns
	}
	if len(text) > threshold {
		warns = append(warns, fmt.Sprintf("instructions are long (%d bytes); consider trimming to leave room for the task", len(text)))
	}
	warns = append(warns, lintContradictions(text)...)
	return warns
}

// lintContradictions collects the object phrase after "always" vs "never"; a
// phrase appearing under both is a likely contradiction.
func lintContradictions(text string) []string {
	var warns []string
	always, never := map[string]bool{}, map[string]bool{}
	for _, m := range alwaysNeverRe.FindAllStringSubmatch(text, -1) {
		verb := strings.ToLower(m[1])
		obj := strings.ToLower(strings.Join(strings.Fields(m[2]), " "))
		if verb == "always" {
			always[obj] = true
		} else {
			never[obj] = true
		}
	}
	for obj := range always {
		if never[obj] {
			warns = append(warns, fmt.Sprintf("conflicting instructions: both 'always %s' and 'never %s'", obj, obj))
		}
	}
	return warns
}
