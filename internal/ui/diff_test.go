package ui

import (
	"strings"
	"testing"
)

func TestColorizeDiff(t *testing.T) {
	diff := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n context\n-old line\n+new line\n"
	out := ColorizeDiff(diff)

	if !strings.Contains(out, ansiGreen+"+new line") {
		t.Errorf("added line should be green:\n%q", out)
	}
	if !strings.Contains(out, ansiRed+"-old line") {
		t.Errorf("removed line should be red:\n%q", out)
	}
	if !strings.Contains(out, ansiCyan+"@@ -1,2 +1,2 @@") {
		t.Errorf("hunk header should be cyan:\n%q", out)
	}
	// Context line stays unstyled.
	if !strings.Contains(out, "\n context\n") {
		t.Errorf("context line should be unstyled:\n%q", out)
	}
	if ColorizeDiff("") != "" {
		t.Error("empty diff should stay empty")
	}
}
