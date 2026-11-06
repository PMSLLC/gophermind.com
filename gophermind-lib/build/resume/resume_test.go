package resume

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixed = time.Date(2026, 11, 5, 22, 0, 0, 0, time.UTC)

func put(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestParseStep(t *testing.T) {
	if s, err := ParseStep("contract"); err != nil || s != (Step{Stage: "contract"}) {
		t.Fatalf("contract: %+v %v", s, err)
	}
	if s, err := ParseStep("node:fn-run"); err != nil || s != (Step{Node: "fn-run"}) {
		t.Fatalf("node: %+v %v", s, err)
	}
	for _, bad := range []string{"build", "node:", ""} {
		_, err := ParseStep(bad)
		want := `resume: unknown step "` + bad + `" (a planner stage or node:<id>)`
		if err == nil || err.Error() != want {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestStageFilesAreTheDocumentedTable(t *testing.T) {
	want := map[string][]string{
		"clarify":    {"_state/clarify/questions.json", "_state/clarify/facts.json", "_state/clarify/rounds.jsonl", "_state/clarify.json", "_state/question.json", "answers.json", "QUESTIONS.md", "decisions"},
		"confirm":    {"_state/understanding.json", "UNDERSTANDING.md"},
		"contract":   {"contracts.json", "_state/contract.json", "_state/sources.json", "dependencies.json"},
		"decompose":  {"_state/decomposed.json", "_state/classes.json"},
		"enrich":     {"_state/enriched.json"},
		"coverage":   {"coverage.json"},
		"approve":    {"approval.json", "APPROVAL.md"},
		"testwriter": {"_state/testwriter.json", "_state/test_files.json", "_state/leaf_tests.json", "_state/acceptance_tests.json", "_state/obligations.json"},
	}
	for _, s := range Stages {
		if !reflect.DeepEqual(stageFiles(s), want[s]) {
			t.Errorf("%s: %v", s, stageFiles(s))
		}
	}
	if len(want) != len(Stages) {
		t.Error("table and Stages differ")
	}
}

func fullRun(t *testing.T) string {
	dir := t.TempDir()
	for _, s := range Stages {
		for _, f := range stageFiles(s) {
			put(t, dir, f+"/x", "") // directories get a child, files get replaced below
			os.RemoveAll(filepath.Join(dir, f))
			if f != "decisions" {
				put(t, dir, f, "")
			} else {
				put(t, dir, "decisions/q1.md", "")
			}
		}
	}
	put(t, dir, "_state/status.json", `{"allow_public":true,"planned_at":"x","waiting":"w"}`)
	put(t, dir, "_state/planner_error.json", "{}")
	for _, f := range executorFiles {
		if f == "_state/revisions" {
			put(t, dir, f+"/fn-a/1.json", "")
		} else {
			put(t, dir, f, "")
		}
	}
	put(t, dir, "comp/fn-a.runtime.json", "")
	put(t, dir, "comp/fn-a.runtime.json.lock", "")
	put(t, dir, "ESCALATION-fn-a.md", "")
	put(t, dir, "root.json", "")
	put(t, dir, "comp/component.json", "")
	put(t, dir, "comp/fn-a.json", "")
	put(t, dir, "_state/llm/1.json", "keep")
	put(t, dir, "brief.md", "keep")
	put(t, dir, "run.log", "keep")
	return dir
}

func TestFromStageMovesTheStageAndLater(t *testing.T) {
	dir := fullRun(t)
	res, err := FromStage(dir, "coverage", fixed)
	if err != nil {
		t.Fatal(err)
	}
	dd := "_state/discarded/2026-11-05T22-00-00Z"
	if res.DiscardDir != dd {
		t.Errorf("DiscardDir %q", res.DiscardDir)
	}
	for _, rel := range []string{"coverage.json", "approval.json", "APPROVAL.md", "_state/testwriter.json", "_state/test_files.json", "_state/leaf_tests.json", "_state/acceptance_tests.json", "_state/obligations.json", "_state/executor.json", "_state/leaf-results.json", "_state/notes.json", "_state/escalations.json", "_state/revisions", "report.json", "_state/project.json", "comp/fn-a.runtime.json", "comp/fn-a.runtime.json.lock", "ESCALATION-fn-a.md", "_state/planner_error.json"} {
		if !has(res.Discarded, rel) {
			t.Errorf("not discarded: %s", rel)
		}
		if _, err := os.Stat(filepath.Join(dir, dd, filepath.FromSlash(rel))); err != nil {
			t.Errorf("not under discard dir: %s", rel)
		}
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			t.Errorf("still in place: %s", rel)
		}
	}
	for _, rel := range []string{"contracts.json", "_state/enriched.json", "root.json", "comp/component.json", "comp/fn-a.json", "dependencies.json"} {
		if has(res.Discarded, rel) {
			t.Errorf("wrongly discarded: %s", rel)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("gone: %s", rel)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "_state/status.json"))
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if st["planned_at"] != "" || st["waiting"] != "" || st["allow_public"] != true {
		t.Errorf("status %v", st)
	}
	for i := 1; i < len(res.Discarded); i++ {
		if res.Discarded[i-1] > res.Discarded[i] {
			t.Fatal("Discarded not sorted")
		}
	}
}

func TestFromStageDecomposeMovesNodePlans(t *testing.T) {
	dir := fullRun(t)
	res, err := FromStage(dir, "decompose", fixed)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"root.json", "comp", "_state/decomposed.json", "_state/classes.json"} {
		if !has(res.Discarded, rel) {
			t.Errorf("not discarded: %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, res.DiscardDir, "comp/fn-a.json")); err != nil {
		t.Error("leaf node file did not go with its folder")
	}
}

func TestFromStageRefusesAnUnreachedStage(t *testing.T) {
	dir := t.TempDir()
	for _, s := range Stages[:3] {
		put(t, dir, stageFiles(s)[0], "")
	}
	_, err := FromStage(dir, "coverage", fixed)
	if err == nil || !strings.Contains(err.Error(), "never reached") || !strings.Contains(err.Error(), `"contract"`) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_state/discarded")); err == nil {
		t.Error("something was moved")
	}
	empty := t.TempDir()
	if _, err := FromStage(empty, "clarify", fixed); err == nil || !strings.Contains(err.Error(), "nothing to resume") {
		t.Errorf("empty: %v", err)
	}
}

func TestFromStageRefusesAnUnknownStage(t *testing.T) {
	if _, err := FromStage(t.TempDir(), "build", fixed); err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("err %v", err)
	}
}

func TestFromStageKeepsTranscripts(t *testing.T) {
	dir := fullRun(t)
	if _, err := FromStage(dir, "clarify", fixed); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"_state/llm/1.json", "brief.md", "run.log", "_state/status.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func TestFromStageSecondResumeSameSecondRefuses(t *testing.T) {
	dir := fullRun(t)
	if _, err := FromStage(dir, "approve", fixed); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "approval.json", "again")
	if _, err := FromStage(dir, "approve", fixed); err == nil {
		t.Fatal("overwrote a discarded file")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "approval.json")); string(b) != "again" {
		t.Error("source lost")
	}
}

func TestInsideRefusesEscape(t *testing.T) {
	if _, err := inside("/r", "../x"); err == nil {
		t.Error("escape allowed")
	}
	if _, err := inside("/r", "/etc"); err == nil {
		t.Error("absolute allowed")
	}
}
