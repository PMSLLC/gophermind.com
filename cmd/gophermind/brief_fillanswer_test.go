package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fillFixture = "# Questions\n\nRound 1.\n\n## Question 1 (id: clarify-q1)\n\nWhich database?\n\n```answer clarify-q1\n\n```\n" +
	"\n## Question 2 (id: clarify-q2)\n\nPrint JSON?\n\n```answer clarify-q2\n\n```\n"

func TestBriefFillAnswerWritesTheBlockAndCountsTheRest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".gophermind", "gm-2026-10-24-001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "QUESTIONS.md"), []byte(fillFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if rc := briefFillAnswer([]string{"gm-2026-10-24-001", "clarify-q1", "--repo", root}, strings.NewReader("sqlite"), &out, &errw); rc != 0 || out.String() != "remaining 1\n" {
		t.Fatalf("rc %d out %q err %q", rc, out.String(), errw.String())
	}
	b, _ := os.ReadFile(filepath.Join(dir, "QUESTIONS.md"))
	if !strings.Contains(string(b), "```answer clarify-q1\nsqlite\n```") {
		t.Fatalf("file:\n%s", b)
	}
	out.Reset()
	if rc := briefFillAnswer([]string{"gm-2026-10-24-001", "clarify-q9", "--repo", root}, strings.NewReader("x"), &out, &errw); rc != 3 {
		t.Fatalf("unknown question rc %d", rc)
	}
	if rc := briefFillAnswer([]string{"gm-2026-10-24-009", "clarify-q1", "--repo", root}, strings.NewReader("x"), &out, &errw); rc != 4 {
		t.Fatalf("unknown run rc %d", rc)
	}
}
