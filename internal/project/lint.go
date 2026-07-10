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
// text: it flags overly long instructions and simple always/never contradictions
// about the same object. Warnings are non-fatal — they surface prompt bloat or
// conflicts a user may want to fix.
func LintInstructions(text string) []string {
	var warns []string
	if strings.TrimSpace(text) == "" {
		return warns
	}
	if len(text) > lintLongBytes {
		warns = append(warns, fmt.Sprintf("instructions are long (%d bytes); consider trimming to leave room for the task", len(text)))
	}

	// Collect the object phrase after "always" vs "never"; a phrase appearing
	// under both is a likely contradiction.
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
