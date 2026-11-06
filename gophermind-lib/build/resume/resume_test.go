package resume

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/build/runfs"
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

// nodeBoard builds a flat-layout board in runDir with the given statuses.
func nodeBoard(t *testing.T, runDir string, status map[string]blackboard.Status) *blackboard.FS {
	t.Helper()
	ctx := context.Background()
	b := blackboard.NewFS(runfs.Fixed(runDir), blackboard.WithFlatLayout())
	var ids []string
	waves := map[string]int{}
	for id := range status {
		ids = append(ids, id)
		waves[id] = 0
	}
	if err := b.InitRun(ctx, "r", ids, waves); err != nil {
		t.Fatal(err)
	}
	for id, st := range status {
		if st == blackboard.StatusPending {
			continue
		}
		if err := b.SetStatus(ctx, "r", id, blackboard.StatusPending, blackboard.StatusReady); err != nil {
			t.Fatal(err)
		}
		switch st {
		case blackboard.StatusReady:
		case blackboard.StatusClaimed, blackboard.StatusInProgress:
			if ok, err := b.Claim(ctx, "r", id, "w"); err != nil || !ok {
				t.Fatalf("claim %s: %v %v", id, ok, err)
			}
			if st == blackboard.StatusInProgress {
				if err := b.SetStatus(ctx, "r", id, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
					t.Fatal(err)
				}
			}
		case blackboard.StatusVerified:
			if ok, err := b.Claim(ctx, "r", id, "w"); err != nil || !ok {
				t.Fatalf("claim %s: %v %v", id, ok, err)
			}
			for _, step := range [][2]blackboard.Status{{blackboard.StatusClaimed, blackboard.StatusInProgress}, {blackboard.StatusInProgress, blackboard.StatusVerified}} {
				if err := b.SetStatus(ctx, "r", id, step[0], step[1]); err != nil {
					t.Fatal(err)
				}
			}
		default:
			if ok, err := b.Claim(ctx, "r", id, "w"); err != nil || !ok {
				t.Fatalf("claim %s: %v %v", id, ok, err)
			}
			if err := b.SetStatus(ctx, "r", id, blackboard.StatusClaimed, blackboard.StatusInProgress); err != nil {
				t.Fatal(err)
			}
			if err := b.SetStatus(ctx, "r", id, blackboard.StatusInProgress, blackboard.StatusNeedsRevision); err != nil {
				t.Fatal(err)
			}
			switch st {
			case blackboard.StatusFailed:
				// needs_revision cannot go to failed; use escalated then failed.
				if err := b.SetStatus(ctx, "r", id, blackboard.StatusNeedsRevision, blackboard.StatusEscalated); err != nil {
					t.Fatal(err)
				}
				if err := b.SetStatus(ctx, "r", id, blackboard.StatusEscalated, blackboard.StatusFailed); err != nil {
					t.Fatal(err)
				}
			case blackboard.StatusEscalated:
				if err := b.SetStatus(ctx, "r", id, blackboard.StatusNeedsRevision, blackboard.StatusEscalated); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return b
}

func TestFromNodeResetsAFailedNode(t *testing.T) {
	dir := t.TempDir()
	b := nodeBoard(t, dir, map[string]blackboard.Status{"fn-a": blackboard.StatusFailed, "fn-b": blackboard.StatusReady})
	ctx := context.Background()
	if err := b.AppendAttempt(ctx, "r", "fn-a", blackboard.Attempt{Model: "m", Verdict: blackboard.VerdictFail}); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "_state/executor.json", `{}`)
	put(t, dir, "_state/leaf-results.json", `{"fn-a":{"status":"failed","reason":"ladder_exhausted"},"fn-b":{"status":"verified"}}`)
	put(t, dir, "ESCALATION-fn-a.md", "x")
	put(t, dir, "report.json", "{}")
	put(t, dir, "_state/project.json", "{}")
	res, err := FromNode(ctx, dir, b, "r", "fn-a", fixed)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := b.Get(ctx, "r", "fn-a")
	if row.Status != blackboard.StatusReady || len(row.Attempts) != 0 {
		t.Errorf("fn-a: %+v", row)
	}
	other, _ := b.Get(ctx, "r", "fn-b")
	if other.Status != blackboard.StatusReady {
		t.Errorf("fn-b touched: %+v", other)
	}
	var lr map[string]json.RawMessage
	data, err := os.ReadFile(filepath.Join(dir, "_state/leaf-results.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &lr); err != nil || len(lr) != 1 || lr["fn-b"] == nil {
		t.Errorf("leaf-results: %s %v", data, err)
	}
	for _, rel := range []string{"report.json", "_state/project.json", "ESCALATION-fn-a.md"} {
		if !has(res.Discarded, rel) {
			t.Errorf("%s not listed in Discarded %v", rel, res.Discarded)
		}
		if present(dir, rel) {
			t.Errorf("%s still in place", rel)
		}
	}
	var snap struct {
		Status   string            `json:"status"`
		Attempts []json.RawMessage `json:"attempts"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, res.DiscardDir, "attempts-fn-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snap); err != nil || snap.Status != "failed" || len(snap.Attempts) != 1 {
		t.Errorf("attempts copy: %s %v", raw, err)
	}
	lraw, err := os.ReadFile(filepath.Join(dir, res.DiscardDir, "leaf-result-fn-a.json"))
	if err != nil || !strings.Contains(string(lraw), "ladder_exhausted") {
		t.Errorf("leaf-result copy: %s %v", lraw, err)
	}
	if !has(res.Discarded, res.DiscardDir+"/leaf-result-fn-a.json") || !has(res.Discarded, res.DiscardDir+"/attempts-fn-a.json") || !sort.StringsAreSorted(res.Discarded) {
		t.Errorf("Discarded %v", res.Discarded)
	}
	if !present(dir, res.DiscardDir+"/attempts-fn-a.json") || !present(dir, res.DiscardDir+"/report.json") {
		t.Errorf("discard dir %s lacks files", res.DiscardDir)
	}
	if !present(dir, "_state/executor.json") {
		t.Error("executor.json must stay")
	}
}

func TestFromNodeRefusesVerifiedAndUnrun(t *testing.T) {
	dir := t.TempDir()
	b := nodeBoard(t, dir, map[string]blackboard.Status{
		"fn-r": blackboard.StatusReady, "fn-v": blackboard.StatusVerified, "fn-p": blackboard.StatusPending,
	})
	ctx := context.Background()
	put(t, dir, "_state/executor.json", `{}`)
	cases := map[string]string{
		"fn-v": "resume: fn-v is verified; a verified node cannot be rerun in this round",
		"fn-r": "resume: fn-r has not run yet; resume the run without --from",
		"fn-p": "resume: fn-p has not run yet; resume the run without --from",
	}
	for node, want := range cases {
		if _, err := FromNode(ctx, dir, b, "r", node, fixed); err == nil || err.Error() != want {
			t.Errorf("%s: %v", node, err)
		}
	}
	if present(dir, "_state/discarded") {
		t.Error("a refusal must move nothing")
	}
}

func TestFromNodeResetsAnEscalatedNode(t *testing.T) {
	dir := t.TempDir()
	b := nodeBoard(t, dir, map[string]blackboard.Status{"fn-c": blackboard.StatusEscalated})
	put(t, dir, "_state/executor.json", `{}`)
	res, err := FromNode(context.Background(), dir, b, "r", "fn-c", fixed)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := b.Get(context.Background(), "r", "fn-c")
	if row.Status != blackboard.StatusReady {
		t.Errorf("status %s", row.Status)
	}
	raw, err := os.ReadFile(filepath.Join(dir, res.DiscardDir, "attempts-fn-c.json"))
	if err != nil || !strings.Contains(string(raw), `"escalated"`) {
		t.Errorf("copy: %s %v", raw, err)
	}
}

func TestFromNodeRefusesWithoutExecutorState(t *testing.T) {
	dir := t.TempDir()
	b := nodeBoard(t, dir, map[string]blackboard.Status{"fn-a": blackboard.StatusFailed})
	_, err := FromNode(context.Background(), dir, b, "r", "fn-a", fixed)
	if err == nil || err.Error() != "resume: the run never reached the executor" {
		t.Errorf("got %v", err)
	}
}

func TestFromNodeRefusesWhileClaimed(t *testing.T) {
	dir := t.TempDir()
	b := nodeBoard(t, dir, map[string]blackboard.Status{"fn-a": blackboard.StatusFailed, "fn-b": blackboard.StatusClaimed})
	put(t, dir, "_state/executor.json", `{}`)
	_, err := FromNode(context.Background(), dir, b, "r", "fn-a", fixed)
	if err == nil || err.Error() != "resume: the run is active (node fn-b is claimed)" {
		t.Errorf("got %v", err)
	}
	row, _ := b.Get(context.Background(), "r", "fn-a")
	if row.Status != blackboard.StatusFailed {
		t.Errorf("fn-a changed: %s", row.Status)
	}
}

func TestPointsListsStagesThenNodes(t *testing.T) {
	dir := t.TempDir()
	for _, s := range Stages[:7] {
		put(t, dir, stageFiles(s)[0], "{}")
	}
	put(t, dir, "_state/executor.json", `{}`)
	b := nodeBoard(t, dir, map[string]blackboard.Status{
		"fn-a": blackboard.StatusFailed, "fn-b": blackboard.StatusVerified, "fn-c": blackboard.StatusEscalated,
	})
	ctx := context.Background()
	pts, err := Points(ctx, dir, b, "r")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pts {
		got = append(got, p.Step)
	}
	want := []string{"clarify", "confirm", "contract", "decompose", "enrich", "coverage", "approve", "node:fn-a", "node:fn-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("steps %v, want %v", got, want)
	}
	if pts[0].Label != "clarify" || pts[7].Label != "fn-a (failed)" || pts[8].Label != "fn-c (escalated)" {
		t.Errorf("labels %+v", pts)
	}
	dir2 := t.TempDir()
	put(t, dir2, "_state/executor.json", `{}`)
	b2 := nodeBoard(t, dir2, map[string]blackboard.Status{"fn-a": blackboard.StatusFailed, "fn-b": blackboard.StatusClaimed})
	none, err := Points(ctx, dir2, b2, "r")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("claimed: %v %v", none, err)
	}
	empty, err := Points(ctx, t.TempDir(), nil, "r")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty: %v %v", empty, err)
	}
}
