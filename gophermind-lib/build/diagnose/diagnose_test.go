package diagnose

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const runID = "gm-2026-10-24-009"

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

func call(id int, at, stage, task, node, outcome string, ms, pt, ct int) string {
	b, _ := json.Marshal(map[string]any{"kind": "call", "id": id, "run_id": runID, "at": at, "stage": stage, "task_type": task,
		"node_id": node, "provider": "mini", "model_requested": "m1", "model_served": "m1", "prompt_tokens": pt,
		"completion_tokens": ct, "duration_ms": ms, "outcome": outcome})
	return string(b) + "\n"
}

// fixture is a failed run: fn-a failed on identical replies, fn-b is blocked
// by fn-a, planning had one refusal. The files hold canary text a diagnosis
// must never print.
func fixture(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), ".gophermind", runID)
	calls := call(1, "2026-10-24T10:00:10Z", "clarify", "clarify", "", "ok", 10000, 100, 50) +
		call(2, "2026-10-24T10:01:00Z", "implement:fn-a", "implement", "fn-a", "ok", 30000, 200, 80) +
		call(3, "2026-10-24T10:02:00Z", "implement:fn-a", "implement", "fn-a", "ok", 20000, 200, 80) +
		call(4, "2026-10-24T10:03:00Z", "testwrite:fn-b", "testwrite", "fn-b", "error", 90000, 0, 0) +
		call(5, "2026-10-24T10:05:00Z", "testwrite:fn-b", "testwrite", "fn-b", "error", 95000, 0, 0) +
		call(6, "2026-10-24T10:07:00Z", "testwrite:fn-b", "testwrite", "fn-b", "malformed", 1000, 5, 5)
	put(t, dir, "_state/calls.jsonl", calls)
	put(t, dir, "_state/events.jsonl", `{"run_id":"x","node_id":"fn-a","kind":"attempt","at":"2026-10-24T10:01:00Z"}`+"\n"+`{"kind":"result"}`+"\n")
	put(t, dir, "_state/status.json", `{"waiting":""}`)
	put(t, dir, "report.json", `{"schema_version":3,"run_id":"`+runID+`","started_at":"2026-10-24T10:00:00Z","finished_at":"2026-10-24T10:08:00Z",
"status":"failed","stop_reason":"blocked","exit_code":1,"nodes":{"total":2,"verified":0,"failed":1,"escalated":0,"blocked":1},
"failures":["fn-a: identical_reply","fn-b: blocked by fn-a"],"revised_leaves":[{"node_id":"fn-a","revisions":1,"refusals":["revise_invalid"]}]}`)
	put(t, dir, "_state/leaf-results.json", `{"fn-a":{"status":"failed","reason":"identical_reply"}}`)
	for n, mr := range []string{"", "gate:identical_reply"} {
		body := `{"provider":"mini","model":"m1","revision":0,"order":1,"verdict":"fail","class":"identical_reply","reason":"identical_reply","duration_ms":5}`
		if mr != "" {
			body = `{"provider":"mini","model":"m1","revision":1,"order":1,"verdict":"fail","class":"identical_reply","malformed_reason":"` + mr + `","duration_ms":6}`
		}
		put(t, dir, "attempts/fn-a/"+string(rune('1'+n))+"/attempt.json", body)
		put(t, dir, "attempts/fn-a/"+string(rune('1'+n))+"/reply.txt", "CANARY-REPLY secret=hunter2")
	}
	put(t, dir, "_state/refusals.jsonl",
		`{"at":"t","stage":"testwrite:fn-b","node_id":"fn-b","round":1,"text":"first"}`+"\n"+
			`{"at":"t","stage":"testwrite:fn-b","node_id":"fn-b","round":2,"text":"test file does not compile at line 7"}`+"\n")
	put(t, dir, "_state/planner_error.json", `{"at":"t","stage":"decompose","text":"planner: decompose: reply unusable"}`)
	put(t, dir, "brief.md", "CANARY-BRIEF")
	return dir
}

func TestDiagnoseFixtureRun(t *testing.T) {
	dir := fixture(t)
	d, err := Run(context.Background(), runID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "failed" || d.StopReason != "blocked" {
		t.Errorf("status = %q %q", d.Status, d.StopReason)
	}
	byName := map[string]Phase{}
	for _, p := range d.Phases {
		byName[p.Name] = p
	}
	if p := byName["implement"]; p.Calls != 2 || p.OK != 2 || p.Retries != 1 || p.ModelSecs != 50 {
		t.Errorf("implement phase = %+v", p)
	}
	if p := byName["testwrite"]; p.Calls != 3 || p.Malformed != 1 || p.Failed != 2 {
		t.Errorf("testwrite phase = %+v", p)
	}
	if len(d.Slowest) != 5 || d.Slowest[0].ID != 5 || d.Slowest[1].ID != 4 {
		t.Errorf("slowest = %+v", d.Slowest)
	}
	if len(d.Tokens) != 1 || d.Tokens[0].Calls != 6 || d.Tokens[0].PromptTok != 505 {
		t.Errorf("tokens = %+v", d.Tokens)
	}
	if d.Events["attempt"] != 1 || d.Events["result"] != 1 {
		t.Errorf("events = %v", d.Events)
	}
	probs := map[string]Problem{}
	for _, p := range d.Problems {
		probs[p.NodeID] = p
	}
	a, b := probs["fn-a"], probs["fn-b"]
	if a.Status != "failed" || len(a.Attempts) != 2 || a.Attempts[1].MalformedReason != "gate:identical_reply" || len(a.ReviseRefusals) != 1 {
		t.Errorf("fn-a = %+v", a)
	}
	if b.Status != "blocked" || b.Reason != "blocked by fn-a" || b.LastRefusal != "test file does not compile at line 7" || b.RefusalRound != 2 {
		t.Errorf("fn-b = %+v", b)
	}
	if d.PlannerErr == nil || d.PlannerErr.Stage != "decompose" {
		t.Errorf("planner error = %+v", d.PlannerErr)
	}
	causes := strings.Join(d.Causes, "\n")
	for _, want := range []string{"fn-a is where the blocking starts", "all 2 attempts were identical_reply", "2 model calls ended in error"} {
		if !strings.Contains(causes, want) {
			t.Errorf("causes lack %q:\n%s", want, causes)
		}
	}

	text := d.Text()
	raw, _ := json.Marshal(d)
	for _, out := range []string{text, string(raw)} {
		if strings.Contains(out, "CANARY") || strings.Contains(out, "hunter2") {
			t.Errorf("output carries file content:\n%s", out)
		}
	}
	for _, want := range []string{"PHASES", "SLOWEST MODEL CALLS", "TOKENS BY PROVIDER AND MODEL", "PLANNER ERROR", "PROBLEMS", "LIKELY CAUSE", "last planner refusal (testwrite:fn-b, round 2)"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestDiagnoseMissingFolder(t *testing.T) {
	if _, err := Run(context.Background(), runID, filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("want an error for a missing folder")
	}
}

func TestDiagnoseRunWithoutReportIsPlanning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), runID)
	put(t, dir, "_state/status.json", `{"waiting":"clarify"}`)
	d, err := Run(context.Background(), runID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "waiting" || len(d.Notes) == 0 || !strings.Contains(strings.Join(d.Causes, ";"), "no model call") {
		t.Errorf("diagnosis = %+v", d)
	}
}
