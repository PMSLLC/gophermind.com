package project

import (
	"strings"
	"testing"
)

func TestLintFlagsLongInstructions(t *testing.T) {
	long := strings.Repeat("x", lintLongBytes+1)
	warns := LintInstructions(long)
	if len(warns) == 0 || !containsSubstr(warns, "long") {
		t.Errorf("expected a length warning, got %v", warns)
	}
}

func TestLintFlagsAlwaysNeverConflict(t *testing.T) {
	text := "always use tabs here.\nnever use tabs in this repo.\n"
	warns := LintInstructions(text)
	if !containsSubstr(warns, "conflict") {
		t.Errorf("expected a conflict warning, got %v", warns)
	}
}

func TestLintCleanInstructionsNoWarnings(t *testing.T) {
	if warns := LintInstructions("Run go test before finishing. Prefer small diffs."); len(warns) != 0 {
		t.Errorf("clean instructions should not warn: %v", warns)
	}
}

func TestLintEmpty(t *testing.T) {
	if warns := LintInstructions(""); len(warns) != 0 {
		t.Errorf("empty should not warn: %v", warns)
	}
}

func containsSubstr(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}
