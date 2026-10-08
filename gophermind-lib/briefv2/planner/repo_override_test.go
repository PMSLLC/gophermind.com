package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"errors"
	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/planner"
)

func TestRepoOverrideUsedForRunFolder(t *testing.T) {
	g := newRig(t, approving(), chainDir(t))
	g.briefPath = writeBrief(t, g.repo, func(s string) string {
		return strings.Replace(s, g.repo, filepath.Join(t.TempDir(), "missing"), 1)
	})
	if _, err := g.plan(planner.Options{StopAfter: "load"}); err == nil {
		t.Fatal("a missing brief repo was accepted without an override")
	}
	g.mustPlan(planner.Options{Repo: g.repo, StopAfter: "load"})
	if _, err := os.Stat(filepath.Join(g.repo, ".gophermind", greeterID)); err != nil {
		t.Fatalf("run folder: %v", err)
	}
	rec, err := planner.LookupRun(greeterID)
	if err != nil || rec.Repo != g.repo {
		t.Fatalf("record = %+v, %v; want repo %s", rec, err, g.repo)
	}
}

func TestRepoOverrideMustMatchOnResume(t *testing.T) {
	g := newRig(t, approving(), chainDir(t))
	g.mustPlan(planner.Options{Repo: g.repo, StopAfter: "load"})
	_, err := g.plan(planner.Options{RunID: greeterID, Repo: t.TempDir(), StopAfter: "load"})
	if err == nil || !strings.Contains(err.Error(), "was planned in") {
		t.Fatalf("err = %v", err)
	}
	g.mustPlan(planner.Options{RunID: greeterID, Repo: g.repo, StopAfter: "load"})
}

func TestReadAnswersApprovalUnderstandingAndCounts(t *testing.T) {
	g := newRig(t, approving(), chainDir(t))
	g.mustPlan(planner.Options{StopAfter: "load"})
	if as, err := planner.ReadAnswers(g.runDir); err != nil || as == nil || len(as) != 0 {
		t.Fatalf("ReadAnswers before = %v, %v", as, err)
	}
	if _, ok, err := planner.ReadApproval(g.runDir); err != nil || ok {
		t.Fatalf("ReadApproval before = %v, %v", ok, err)
	}
	if _, ok, err := planner.ReadUnderstanding(g.runDir); err != nil || ok {
		t.Fatalf("ReadUnderstanding before = %v, %v", ok, err)
	}
	if c, err := planner.ReadQuestionCounts(g.runDir); err != nil || c.Total != 0 || c.Calls != 0 {
		t.Fatalf("counts before = %+v, %v", c, err)
	}
	g.mustPlan(planner.Options{RunID: greeterID, Unattended: true, StopAfter: "approve"})
	as, err := planner.ReadAnswers(g.runDir)
	if err != nil || len(as) != 3 || !as[0].Assumed || as[0].Answer == "" || as[0].Stage == "" {
		t.Fatalf("ReadAnswers = %+v, %v", as, err)
	}
	ap, ok, err := planner.ReadApproval(g.runDir)
	if err != nil || !ok || ap.ApprovedBy != "test" || ap.PlanHash == "" || ap.UnderstandingHash == "" || ap.ApprovedAt == "" {
		t.Fatalf("ReadApproval = %+v, %v, %v", ap, ok, err)
	}
	c, err := planner.ReadQuestionCounts(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 3 || c.Settled != 3 || c.Open != 0 || c.ByAnsweredBy["unattended-default"] != 3 || c.Rounds < 1 || c.Calls != 2 || c.MidStageAssumed != 0 {
		t.Errorf("counts = %+v", c)
	}
}

func TestReadApprovalFlag(t *testing.T) {
	g := newRig(t, approving(), chainDir(t))
	g.mustPlan(planner.Options{Yes: true, StopAfter: "approve"})
	ap, ok, err := planner.ReadApproval(g.runDir)
	if err != nil || !ok || ap.ApprovedBy != "flag" {
		t.Fatalf("ReadApproval = %+v, %v, %v", ap, ok, err)
	}
}

func TestReadQuestionCountsMidStage(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "_state", "clarify"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := `{"calls":1,"round":1,"complete":true,"questions":[
{"id":"q1","question":"a","kind":"decision","raised_by":"clarify","status":"settled","answer":"x","answered_by":"unattended-default"},
{"id":"decompose-greeting-q1","question":"b","kind":"decision","raised_by":"decompose:greeting","status":"settled","answer":"y","answered_by":"unattended-default"},
{"id":"decompose-greeting-q2","question":"c","kind":"decision","raised_by":"decompose:greeting","status":"settled","answer":"y","answered_by":"human"}]}`
	if err := os.WriteFile(filepath.Join(dir, "_state", "clarify", "questions.json"), []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := planner.ReadQuestionCounts(dir)
	if err != nil || c.MidStageAssumed != 1 || c.ByAnsweredBy["human"] != 1 || c.ByAnsweredBy["unattended-default"] != 2 || c.Total != 3 {
		t.Fatalf("counts = %+v, %v", c, err)
	}
}

func TestResolveRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "r"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := planner.ResolveRepo("~/r")
	if err != nil || got != filepath.Join(home, "r") {
		t.Fatalf("ResolveRepo(~/r) = %q, %v", got, err)
	}
	var inv *brief.InvalidError
	if _, err := planner.ResolveRepo("https://example.com/x.git"); !errors.As(err, &inv) {
		t.Errorf("URL err = %v", err)
	}
	if _, err := planner.ResolveRepo(filepath.Join(home, "nope")); !errors.As(err, &inv) {
		t.Errorf("missing dir err = %v", err)
	}
}
