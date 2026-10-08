package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBriefAnswerNeedsARunAQuestionAndAnAnswer(t *testing.T) {
	var out, errw bytes.Buffer
	if code := briefAnswer([]string{"gm-2026-10-08-001", "q1"}, &out, &errw); code != 1 || !strings.Contains(errw.String(), "gophermind brief answer") {
		t.Fatalf("code = %d, stderr = %q", code, errw.String())
	}
}

func TestBriefAnswerOfAnUnknownRunFails(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	var out, errw bytes.Buffer
	if code := briefAnswer([]string{"gm-2026-10-08-001", "q1", "yes"}, &out, &errw); code != 1 || !strings.Contains(errw.String(), "no run") {
		t.Fatalf("code = %d, stderr = %q", code, errw.String())
	}
}
