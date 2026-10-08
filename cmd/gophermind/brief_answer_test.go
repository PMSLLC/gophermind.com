package main

import (
	"bytes"
	"os"
	"path/filepath"
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

// The success path: a run waiting for the confirmation has its Clarify answer
// changed, and the CLI says the plan restarts from the Contract stage.
func TestBriefAnswerChangesASettledAnswer(t *testing.T) {
	repo, briefPath := planEnv(t)
	runDir := filepath.Join(repo, ".gophermind", greeterRunID)
	if code, out, errs := runBriefCmd(t, "", "plan", briefPath, "--fake", greeterFixture, "--gate", "file"); code != 3 {
		t.Fatalf("plan: code=%d out=%q err=%q", code, out, errs)
	}
	fill(t, filepath.Join(runDir, "QUESTIONS.md"), "answer", "accept")
	if code, out, errs := runBriefCmd(t, "", "resume", greeterRunID, "--fake", greeterFixture, "--gate", "file"); code != 3 {
		t.Fatalf("resume: code=%d out=%q err=%q", code, out, errs)
	}
	code, out, errs := runBriefCmd(t, "", "answer", greeterRunID, "q1", "No,", "use", "it", "exactly", "as", "given.")
	if code != 0 || !strings.Contains(out, "Answer changed. The plan restarts from the Contract stage") || !strings.Contains(out, "gophermind brief resume "+greeterRunID) {
		t.Fatalf("answer: code=%d out=%q err=%q", code, out, errs)
	}
	md, err := os.ReadFile(filepath.Join(runDir, "decisions", "q1.md"))
	if err != nil || !strings.Contains(string(md), "No, use it exactly as given.") || !strings.Contains(string(md), "Earlier answers") {
		t.Errorf("decisions/q1.md = %q, %v", md, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "_state", "understanding.json")); err == nil {
		t.Error("the confirmation survived the changed answer")
	}
}
