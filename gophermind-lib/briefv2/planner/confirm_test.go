package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/planner"
)

func readUnderstandingRec(t *testing.T, runDir string) (by, hash string, ok bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runDir, "_state", "understanding.json"))
	if err != nil {
		return "", "", false
	}
	var u struct {
		ConfirmedBy string `json:"confirmed_by"`
		Hash        string `json:"understanding_hash"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	return u.ConfirmedBy, u.Hash, true
}

func TestConfirmBlocksContractUntilTheOwnerConfirms(t *testing.T) {
	gate := approving()
	no := human.Decision{Approved: false, By: "test", Note: "not yet"}
	gate.confirm = &no
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	if _, err := g.plan(planner.Options{StopAfter: "contract"}); err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err = %v, want 'not confirmed'", err)
	}
	if _, err := os.Stat(filepath.Join(g.runDir, "contracts.json")); err == nil {
		t.Fatal("Contract ran without a confirmed understanding")
	}
	gate.confirm = nil
	if _, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "confirm"}); err != nil {
		t.Fatal(err)
	}
	by, hash, ok := readUnderstandingRec(t, g.runDir)
	if !ok || by != "test" || hash == "" {
		t.Fatalf("understanding.json: by=%q hash=%q ok=%v", by, hash, ok)
	}
	md, err := os.ReadFile(filepath.Join(g.runDir, "UNDERSTANDING.md"))
	if err != nil || !strings.Contains(string(md), "Frontier empty: every branch visited") || !strings.Contains(string(md), "Which store?") {
		t.Errorf("UNDERSTANDING.md = %s, %v", md, err)
	}
	if len(gate.understandings) != 2 || gate.understandings[0].Hash != hash {
		t.Errorf("the gate saw %d understandings", len(gate.understandings))
	}
}

func TestConfirmIsRedoneWhenAnAnswerChanges(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	if _, err := g.plan(planner.Options{StopAfter: "confirm"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(g.runDir, "_state", "clarify", "questions.json")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), `"answer": "yes"`, `"answer": "no"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "confirm"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.understandings) != 2 || gate.understandings[0].Hash == gate.understandings[1].Hash {
		t.Errorf("an edited answer must put a new understanding to the gate: %d, %v", len(gate.understandings), gate.understandings)
	}
}

func TestConfirmUnattendedNeedsNoGate(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	assumeBrief(g)
	if _, err := g.plan(planner.Options{StopAfter: "confirm"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.understandings) != 0 {
		t.Fatalf("an unattended run asked the gate to confirm")
	}
	if by, _, ok := readUnderstandingRec(t, g.runDir); !ok || by != "unattended" {
		t.Errorf("confirmed_by = %q, want unattended", by)
	}
}

func TestConfirmFileGateWaitsForTheOwner(t *testing.T) {
	dir := t.TempDir()
	g := newRig(t, human.NewFile(dir), fixtureDir(t, map[string]string{"clarify.txt": `[]`, "clarify.more.txt": "[]"}))
	out, err := g.plan(planner.Options{StopAfter: "confirm"})
	if err != nil || out != planner.Waiting {
		t.Fatalf("plan = %v, %v; want Waiting for UNDERSTANDING.md", out, err)
	}
	p := filepath.Join(dir, "UNDERSTANDING.md")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), "```decision\n\n```", "```decision\napprove\n```", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := g.plan(planner.Options{RunID: greeterID, StopAfter: "confirm"}); err != nil || out != planner.Done {
		t.Fatalf("after answering: %v, %v", out, err)
	}
	if by, _, ok := readUnderstandingRec(t, g.runDir); !ok || by != "file" {
		t.Errorf("confirmed_by = %q, want file", by)
	}
}

func TestConfirmIsRedoneWhenAnswersJSONIsEdited(t *testing.T) {
	gate := approving()
	g := newRig(t, gate, fixtureDir(t, map[string]string{"clarify.txt": chainQuestions, "clarify.more.txt": "[]"}))
	g.mustPlan(planner.Options{StopAfter: "confirm"})
	p := filepath.Join(g.runDir, "answers.json")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), `"answer": "yes"`, `"answer": "no"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "confirm"})
	if len(gate.understandings) != 2 {
		t.Errorf("an edited answers.json must put the understanding to the gate again: %d", len(gate.understandings))
	}
	// Confirming again leaves a consistent run: a third pass asks nothing.
	g.mustPlan(planner.Options{RunID: greeterID, StopAfter: "confirm"})
	if len(gate.understandings) != 2 {
		t.Errorf("the confirmation did not hold: %d", len(gate.understandings))
	}
}
