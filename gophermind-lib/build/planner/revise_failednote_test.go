package planner

import (
	"strings"
	"testing"
)

func TestFailedTestsNote(t *testing.T) {
	if got := failedTestsNote(nil); got != "" {
		t.Fatalf("no names gave %q", got)
	}
	got := failedTestsNote([]string{"TestA/x", "TestB"})
	if !strings.Contains(got, "TestA/x, TestB") || !strings.Contains(got, "do not repeat") {
		t.Fatalf("note = %q", got)
	}
}
