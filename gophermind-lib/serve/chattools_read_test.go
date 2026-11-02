package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/runview/runviewtest"
	"gophermind/gophermind-lib/tools"
)

func chatToolsEnv(t *testing.T) (*chatTools, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := newBriefManager(Options{BriefsDir: dir}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &chatTools{bm: m}, dir
}

func writeRunRecord(t *testing.T, dir string, rec briefRecord, log string) {
	t.Helper()
	d := filepath.Join(dir, rec.ID)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(d, "run.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if log != "" {
		if err := os.WriteFile(filepath.Join(d, "run.log"), []byte(log), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func findTool(t *testing.T, c *chatTools, name string) tools.Tool {
	t.Helper()
	for _, tl := range c.all() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.Tool{}
}

func TestBriefsListNewestFirst(t *testing.T) {
	c, dir := chatToolsEnv(t)
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-23-001", Title: "Older", Phase: phaseDone, StartedAt: "2026-10-23T10:00:00Z"}, "")
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-24-001", Title: "Newer", Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"}, "")
	out, err := findTool(t, c, "briefs_list").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	ni, oi := strings.Index(out, "gm-2026-10-24-001"), strings.Index(out, "gm-2026-10-23-001")
	if ni < 0 || oi < 0 || ni > oi {
		t.Errorf("order wrong:\n%s", out)
	}
	if !strings.Contains(out, "Newer") || !strings.Contains(out, "running") || !strings.Contains(out, "2026-10-24T10:00:00Z") {
		t.Errorf("missing fields:\n%s", out)
	}
}

func TestBriefsListEmpty(t *testing.T) {
	c, _ := chatToolsEnv(t)
	out, err := findTool(t, c, "briefs_list").Run(context.Background(), nil)
	if err != nil || out != "no briefs" {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestBriefStateRejectsBadID(t *testing.T) {
	c, _ := chatToolsEnv(t)
	for _, id := range []string{"../x", "", "a/b"} {
		raw, _ := json.Marshal(map[string]string{"id": id})
		if _, err := findTool(t, c, "brief_state").Run(context.Background(), raw); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestBriefStateCapsLog(t *testing.T) {
	c, dir := chatToolsEnv(t)
	id := "gm-2026-10-24-002"
	log := strings.Repeat("line of log output that is fairly long to fill space quickly....\n", 3200)
	if len(log) < 200<<10 {
		t.Fatalf("log only %d", len(log))
	}
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "Big", Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"}, log+"LAST-LINE\n")
	raw, _ := json.Marshal(map[string]string{"id": id})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 8<<10 {
		t.Errorf("output %d bytes, over cap", len(out))
	}
	if !strings.Contains(out, "[tail of ") || !strings.Contains(out, "full text at") || !strings.Contains(out, "LAST-LINE") {
		t.Errorf("no note or tail:\n%s", out[len(out)-300:])
	}
}

func TestOutputRedactsSecrets(t *testing.T) {
	c, dir := chatToolsEnv(t)
	id := "gm-2026-10-24-003"
	writeRunRecord(t, dir, briefRecord{ID: id, Phase: phaseRunning, StartedAt: "2026-10-24T10:00:00Z"},
		"calling api with Authorization: Bearer abcdef1234567890abcdef1234567890\nowner jane.doe@example.com\n")
	raw, _ := json.Marshal(map[string]string{"id": id})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "abcdef1234567890abcdef1234567890") {
		t.Errorf("token leaked:\n%s", out)
	}
}

func writeGate(t *testing.T, c *chatTools, id, body string) {
	t.Helper()
	d := filepath.Join(c.bm.repoFor(id), ".gophermind", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "QUESTIONS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestQuestionsPendingAcrossBriefs(t *testing.T) {
	c, dir := chatToolsEnv(t)
	answered := strings.Replace(gateFixture, "```answer clarify-q1\n\n```", "```answer clarify-q1\nsqlite\n```", 1)
	for i, id := range []string{"gm-2026-10-23-001", "gm-2026-10-24-001"} {
		writeRunRecord(t, dir, briefRecord{ID: id, Title: "T", Phase: phaseWaiting, StartedAt: "2026-10-2" + string(rune('3'+i)) + "T10:00:00Z"}, "")
		writeGate(t, c, id, answered)
	}
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "clarify-q2"); n != 2 {
		t.Errorf("want 2 open q2 blocks, got %d:\n%s", n, out)
	}
	if strings.Contains(out, "clarify-q1") {
		t.Errorf("answered question listed:\n%s", out)
	}
	for _, want := range []string{"gm-2026-10-23-001", "gm-2026-10-24-001", "source: brief", "Should the CLI print JSON?", "answerable in chat: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestQuestionsPendingNone(t *testing.T) {
	c, dir := chatToolsEnv(t)
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-23-001", Title: "T", Phase: phaseDone}, "")
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing is waiting") {
		t.Errorf("got %q", out)
	}
}

func TestQuestionsPendingHarnessSource(t *testing.T) {
	c, dir := chatToolsEnv(t)
	ex := t.TempDir()
	if err := os.WriteFile(filepath.Join(ex, "a.md"), []byte("title: A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.h = newHarness(c.bm, ex)
	const id = "gm-2026-10-24-001"
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "T", Phase: phaseWaiting}, "")
	writeGate(t, c, id, gateFixture)
	c.h.mu.Lock()
	c.h.st.Briefs[0].LastRun, c.h.st.Briefs[0].LastPhase = id, phaseWaiting
	c.h.mu.Unlock()
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "source: harness") || strings.Contains(out, "source: brief") {
		t.Errorf("want harness source:\n%s", out)
	}
}

func TestHarnessStateOff(t *testing.T) {
	c, _ := chatToolsEnv(t)
	out, err := findTool(t, c, "harness_state").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "harness is off") {
		t.Errorf("got %q", out)
	}
}

func TestHarnessStateOn(t *testing.T) {
	c, _ := chatToolsEnv(t)
	ex := t.TempDir()
	if err := os.WriteFile(filepath.Join(ex, "a.md"), []byte("title: A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.h = newHarness(c.bm, ex)
	c.h.mu.Lock()
	c.h.st.Running, c.h.st.Current = true, "gm-2026-10-24-001"
	c.h.mu.Unlock()
	out, err := findTool(t, c, "harness_state").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"running: true", "gm-2026-10-24-001", "a.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestQuestionsPendingDefaults(t *testing.T) {
	c, dir := chatToolsEnv(t)
	const id = "gm-2026-10-24-001"
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "T", Phase: phaseWaiting}, "")
	writeGate(t, c, id, gateFixture)
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	// gate-only with a Recommended line, and one without
	q1 := out[strings.Index(out, "question: clarify-q1"):strings.Index(out, "question: clarify-q2")]
	q2 := out[strings.Index(out, "question: clarify-q2"):]
	if !strings.Contains(q1, "recommended: sqlite") || !strings.Contains(q1, "sqlite | postgres") {
		t.Errorf("q1 should show default and options:\n%s", q1)
	}
	if !strings.Contains(q2, "default: none") {
		t.Errorf("q2 should show no default:\n%s", q2)
	}
}

func TestQuestionsPendingStoreRecommended(t *testing.T) {
	c, dir := chatToolsEnv(t)
	const id = "gm-2026-10-24-002"
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "T", Phase: phaseRunning}, "")
	d := filepath.Join(c.bm.repoFor(id), ".gophermind", id, "_state", "clarify")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	js := `{"questions":[{"id":"s1","text":"Pick one?","status":"open","options":["a","b"],"recommended":"a"}]}`
	if err := os.WriteFile(filepath.Join(d, "questions.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "question: s1") || !strings.Contains(out, "recommended: a") || !strings.Contains(out, "a | b") {
		t.Errorf("store question:\n%s", out)
	}
}

func runTool(t *testing.T, c *chatTools, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return findTool(t, c, name).Run(context.Background(), raw)
}

func TestBriefArtifactListsThenReads(t *testing.T) {
	c, _ := chatToolsEnv(t)
	f := runviewtest.Build(t, c.bm.root, runviewtest.Verified)
	out, err := runTool(t, c, "brief_artifact", map[string]any{"id": f.RunID})
	if err != nil || !strings.Contains(out, "fn-greet") {
		t.Fatalf("node list: %q, %v", out, err)
	}
	out, err = runTool(t, c, "brief_artifact", map[string]any{"id": f.RunID, "node": "fn-greet"})
	if err != nil || !strings.Contains(out, "1") || !strings.Contains(out, "check-output.txt") {
		t.Fatalf("artifact list: %q, %v", out, err)
	}
	out, err = runTool(t, c, "brief_artifact", map[string]any{"id": f.RunID, "node": "fn-greet", "attempt": 1, "file": "check-output.txt"})
	if err != nil || !strings.Contains(out, "undefined: name") {
		t.Fatalf("content: %q, %v", out, err)
	}
}

func TestBriefArtifactRejectsTraversal(t *testing.T) {
	c, _ := chatToolsEnv(t)
	f := runviewtest.Build(t, c.bm.root, runviewtest.Verified)
	if _, err := runTool(t, c, "brief_artifact", map[string]any{"id": f.RunID, "node": "fn-greet", "attempt": 1, "file": "../../run.json"}); err == nil {
		t.Error("file traversal accepted")
	}
	if _, err := runTool(t, c, "brief_artifact", map[string]any{"id": f.RunID, "node": "../x", "attempt": 1, "file": "reply.txt"}); err == nil {
		t.Error("node traversal accepted")
	}
	if _, err := runTool(t, c, "brief_artifact", map[string]any{"id": "../x"}); err == nil {
		t.Error("id traversal accepted")
	}
}

func TestRunsDigestGroupsSameReason(t *testing.T) {
	c, _ := chatToolsEnv(t)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-001", runviewtest.Failed)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-002", runviewtest.Failed)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-003", runviewtest.Verified)
	out, err := runTool(t, c, "runs_digest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "TestBye failed") != 1 {
		t.Errorf("want one group for the shared reason:\n%s", out)
	}
	for _, want := range []string{"count 2", "gm-2026-10-08-001", "gm-2026-10-08-002", "fn-bye"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestRunsDigestLimit(t *testing.T) {
	c, _ := chatToolsEnv(t)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-001", runviewtest.Failed)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-002", runviewtest.Failed)
	runviewtest.BuildID(t, c.bm.root, "gm-2026-10-08-003", runviewtest.Failed)
	out, err := runTool(t, c, "runs_digest", map[string]any{"limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "count 2") || strings.Contains(out, "gm-2026-10-08-001") {
		t.Errorf("limit 2 should cover only the two newest:\n%s", out)
	}
}

func TestRunsDigestNormalizesReasons(t *testing.T) {
	c, _ := chatToolsEnv(t)
	for i, id := range []string{"gm-2026-10-08-001", "gm-2026-10-08-002"} {
		f := runviewtest.BuildID(t, c.bm.root, id, runviewtest.Failed)
		err := f.BB.AppendAttempt(context.Background(), id, "fn-bye", blackboard.Attempt{
			Order: 2, StartedAt: runviewtest.T0, Verdict: blackboard.VerdictFail,
			FailureReason: fmt.Sprintf("compile: bye.go:%d: undefined: x in %s took %dms\nsecond line", 10+i, id, 900+i),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := runTool(t, c, "runs_digest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "undefined: x") != 1 || !strings.Contains(out, "count 2") ||
		!strings.Contains(out, "gm-2026-10-08-001") || !strings.Contains(out, "gm-2026-10-08-002") || strings.Contains(out, "second line") {
		t.Errorf("want one group of 2:\n%s", out)
	}
}

func TestRunsDigestManyClassesStaysUnderCap(t *testing.T) {
	c, _ := chatToolsEnv(t)
	long := strings.Repeat("w", 400)
	// The first two runs share one reason (the biggest group); the rest are all distinct.
	for i := 1; i <= 24; i++ {
		id := fmt.Sprintf("gm-2026-10-08-%03d", i)
		f := runviewtest.BuildID(t, c.bm.root, id, runviewtest.Failed)
		reason := fmt.Sprintf("class%s-%s", strings.Repeat(string(rune('a'+i%26)), i), long)
		if i <= 2 {
			reason = "BIGGEST shared failure"
		}
		if err := f.BB.AppendAttempt(context.Background(), id, "fn-bye", blackboard.Attempt{
			Order: 2, StartedAt: runviewtest.T0, Verdict: blackboard.VerdictFail, FailureReason: reason,
		}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runTool(t, c, "runs_digest", map[string]any{"limit": 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 8<<10 || strings.Contains(out, "[tail of") {
		t.Errorf("output %d bytes or cut by cap", len(out))
	}
	if !strings.Contains(out, "BIGGEST shared failure") || !strings.Contains(out, "more classes omitted") {
		t.Errorf("biggest group or omitted line missing:\n%s", out)
	}
}

func appendFailure(t *testing.T, f runviewtest.Fixture, reason string) {
	t.Helper()
	if err := f.BB.AppendAttempt(context.Background(), f.RunID, "fn-bye", blackboard.Attempt{
		Order: 2, StartedAt: runviewtest.T0, Verdict: blackboard.VerdictFail, FailureReason: reason,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailureGroupKeyKeepsMalformedSuffix(t *testing.T) {
	key := func(reason, mal string) string {
		return normalizeReason(reason) + malformedSuffix(mal)
	}
	multi := func(n int) string { return fmt.Sprintf("malformed\nat line %d\nmore", n) }
	if key(multi(1), "fence:no_block") == key(multi(1), "shape:bad_json") {
		t.Error("different malformed classes merged")
	}
	if key(multi(1), "fence:no_block") != key(multi(7), "fence:no_block") {
		t.Error("same malformed class with different line numbers split")
	}
	if !strings.HasSuffix(key(multi(1), "fence:no_block"), "[fence:no_block]") {
		t.Errorf("suffix lost: %q", key(multi(1), "fence:no_block"))
	}
}

func TestRunsDigestMultiByteStaysUnderCap(t *testing.T) {
	c, _ := chatToolsEnv(t)
	for i := 1; i <= 24; i++ {
		id := fmt.Sprintf("gm-2026-10-08-%03d", i)
		f := runviewtest.BuildID(t, c.bm.root, id, runviewtest.Failed)
		appendFailure(t, f, strings.Repeat(string(rune(0x1F600+i)), 300))
	}
	out, err := runTool(t, c, "runs_digest", map[string]any{"limit": 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 8<<10 || strings.Contains(out, "[tail of") || !strings.Contains(out, "more classes omitted") {
		t.Errorf("output %d bytes, cut or no omitted line", len(out))
	}
}

func TestRunsDigestNoRuns(t *testing.T) {
	c, _ := chatToolsEnv(t)
	out, err := runTool(t, c, "runs_digest", nil)
	if err != nil || out != "no runs" {
		t.Errorf("got %q, %v", out, err)
	}
}

func TestQuestionsPendingPhaseAndOrder(t *testing.T) {
	c, dir := chatToolsEnv(t)
	const waiting, failed = "gm-2026-10-24-001", "gm-2026-10-24-002"
	// the failed run is newer, so it lists first without the sort
	writeRunRecord(t, dir, briefRecord{ID: waiting, Title: "W", Phase: phaseWaiting, StartedAt: "2026-10-24T10:00:00Z"}, "")
	writeRunRecord(t, dir, briefRecord{ID: failed, Title: "F", Phase: phaseFailed, StartedAt: "2026-10-24T11:00:00Z"}, "")
	writeGate(t, c, waiting, gateFixture)
	d := filepath.Join(c.bm.repoFor(failed), ".gophermind", failed, "_state", "clarify")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	js := `{"questions":[{"id":"s1","text":"Stale?","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(d, "questions.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "run phase: waiting") || !strings.Contains(out, "run phase: failed") {
		t.Errorf("phase lines missing:\n%s", out)
	}
	if strings.Index(out, "brief: "+waiting) > strings.Index(out, "brief: "+failed) {
		t.Errorf("waiting run should come first:\n%s", out)
	}
}

func TestRunsDigestGroupsEndedRuns(t *testing.T) {
	c, dir := chatToolsEnv(t)
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("gm-2026-10-08-%03d", i)
		writeRunRecord(t, dir, briefRecord{ID: id, Title: "T", Phase: phaseFailed, Error: "brief plan exited with code 1", StartedAt: fmt.Sprintf("2026-10-08T10:0%d:00Z", i)}, "")
	}
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-08-007", Title: "T", Phase: phaseInterrupted, Error: "server restarted", StartedAt: "2026-10-08T10:07:00Z"}, "")
	out, err := runTool(t, c, "runs_digest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "brief plan exited with code") != 1 || !strings.Contains(out, "count 6") || !strings.Contains(out, "(+2 more)") || !strings.Contains(out, "server restarted") {
		t.Errorf("want one group of 6 and one of 1:\n%s", out)
	}
}

func TestBriefsListFlattensTitle(t *testing.T) {
	c, dir := chatToolsEnv(t)
	writeRunRecord(t, dir, briefRecord{ID: "gm-2026-10-24-001", Title: "two\nlines\tand tab", Phase: phaseFailed}, "")
	out, err := runTool(t, c, "briefs_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 0 || !strings.Contains(out, "two lines and tab") {
		t.Errorf("title not flattened: %q", out)
	}
}

func TestRunIDInReasonRE_NewShape(t *testing.T) {
	for _, id := range []string{"csvstat-v1-20261029-r001", "csvstat-v1.1.2-20261029-r004", "ledger-sync-v1-20261029-r001", "gm-2026-10-29-004"} {
		got := normalizeReason("failed in " + id + " after")
		if got != "failed in <run> after" {
			t.Errorf("normalizeReason with %q = %q", id, got)
		}
	}
}

// TestChatToolsAcceptLongLibraryRunID: a library run id can be up to 120
// characters, longer than a question id. brief_state, questions_pending and
// runs_digest must all see a 100-character one instead of skipping it.
func TestChatToolsAcceptLongLibraryRunID(t *testing.T) {
	fam := "inventory-reconciliation-service-for-the-north-region-warehouses-and-their-own-store"
	id := fam[:83] + "-v1-20261101-r001"
	if len(id) != 100 {
		t.Fatalf("id is %d chars", len(id))
	}
	c, dir := chatToolsEnv(t)
	answered := strings.Replace(gateFixture, "```answer clarify-q1\n\n```", "```answer clarify-q1\nsqlite\n```", 1)
	writeRunRecord(t, dir, briefRecord{ID: id, Title: "Long", Phase: phaseWaiting, StartedAt: "2026-11-01T10:00:00Z"}, "LONG-LOG\n")
	writeGate(t, c, id, answered)

	raw, _ := json.Marshal(map[string]string{"id": id})
	out, err := findTool(t, c, "brief_state").Run(context.Background(), raw)
	if err != nil || !strings.Contains(out, "LONG-LOG") {
		t.Errorf("brief_state: %v\n%s", err, out)
	}
	out, err = findTool(t, c, "questions_pending").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, id) || !strings.Contains(out, "clarify-q2") {
		t.Errorf("questions_pending skips the long id: %v\n%s", err, out)
	}

	c2, _ := chatToolsEnv(t)
	runviewtest.BuildID(t, c2.bm.root, id, runviewtest.Failed)
	out, err = runTool(t, c2, "runs_digest", nil)
	if err != nil || !strings.Contains(out, id) {
		t.Errorf("runs_digest skips the long id: %v\n%s", err, out)
	}
}
