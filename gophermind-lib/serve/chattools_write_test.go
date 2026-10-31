package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/tools"
)

// writeToolsEnv returns the brief env plus the chat tools the server's own mux
// hands to a chat turn, so the tools act on the same manager as the HTTP side.
func writeToolsEnv(t *testing.T) (*briefEnv, []tools.Tool) {
	t.Helper()
	var got []tools.Tool
	e := newBriefEnv(t, func(o *Options, d *Deps) {
		d.SessionTurn = func(ctx context.Context, id, task string, emit func(event, data string) error) error {
			got = ChatToolsFrom(ctx)
			return nil
		}
	})
	w := e.post("/session/abc/stream", "hi", map[string]string{"Authorization": "Bearer " + testBearer})
	if w.Code != http.StatusOK || len(got) == 0 {
		t.Fatalf("no chat tools: status %d", w.Code)
	}
	return e, got
}

func pick(t *testing.T, ts []tools.Tool, name string) tools.Tool {
	t.Helper()
	for _, tl := range ts {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.Tool{}
}

func callTool(t *testing.T, ts []tools.Tool, name string, args map[string]any) (string, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return pick(t, ts, name).Run(context.Background(), b)
}

func runDirs(t *testing.T, dir string) []string {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteToolsRegistered(t *testing.T) {
	_, ts := writeToolsEnv(t)
	for _, n := range []string{"brief_create", "brief_answer", "brief_stop", "brief_resubmit"} {
		pick(t, ts, n)
	}
}

func TestBriefCreateStartsRun(t *testing.T) {
	e, ts := writeToolsEnv(t)
	out, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText("gm-2026-10-30-001")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gm-2026-10-30-001") {
		t.Errorf("out %q lacks run id", out)
	}
	e.waitPhase("gm-2026-10-30-001", phaseDone, phaseFailed, phaseRunning, phaseWaiting)
	if _, err := os.Stat(filepath.Join(e.dir, "gm-2026-10-30-001", "run.json")); err != nil {
		t.Errorf("no record: %v", err)
	}
	e.waitNotActive("gm-2026-10-30-001")
}

func TestBriefCreateAskDefaultsTrue(t *testing.T) {
	e, ts := writeToolsEnv(t)
	e.askScript(gateFixture)
	if _, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText("gm-2026-10-30-002")}); err != nil {
		t.Fatal(err)
	}
	if rec := e.waitPhase("gm-2026-10-30-002", phaseWaiting, phaseFailed, phaseDone); rec.Phase != phaseWaiting {
		t.Fatalf("phase %s, want waiting (ask default true)", rec.Phase)
	}
}

func TestBriefCreateInvalid(t *testing.T) {
	e, ts := writeToolsEnv(t)
	out, err := callTool(t, ts, "brief_create", map[string]any{"text": "not a brief"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "invalid brief") {
		t.Errorf("out %q", out)
	}
	if n := runDirs(t, e.dir); len(n) != 0 {
		t.Errorf("run dirs %v", n)
	}
}

func TestBriefCreateBusy(t *testing.T) {
	e, ts := writeToolsEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	if _, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText("gm-2026-10-30-003"), "ask": false}); err != nil {
		t.Fatal(err)
	}
	e.waitPhase("gm-2026-10-30-003", phasePlanning, phaseRunning)
	out, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText("gm-2026-10-30-004"), "ask": false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gm-2026-10-30-003") {
		t.Errorf("out %q lacks active id", out)
	}
	if n := runDirs(t, e.dir); len(n) != 1 {
		t.Errorf("run dirs %v, want one", n)
	}
	e.stopAndDrain("gm-2026-10-30-003")
}

func TestBriefAnswerUnknownQuestion(t *testing.T) {
	e, ts := writeToolsEnv(t)
	e.startAsking("gm-2026-10-30-005")
	qf := filepath.Join(e.root, ".gophermind", "gm-2026-10-30-005", "QUESTIONS.md")
	before, _ := os.ReadFile(qf)
	out, err := callTool(t, ts, "brief_answer", map[string]any{"id": "gm-2026-10-30-005", "question_id": "clarify-q9", "text": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no open question") {
		t.Errorf("out %q", out)
	}
	if after, _ := os.ReadFile(qf); string(after) != string(before) {
		t.Error("QUESTIONS.md changed")
	}
}

func TestBriefAnswerAlreadyFilled(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-006"
	e.startAsking(id)
	if out, err := callTool(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "sqlite"}); err != nil || !strings.Contains(out, "1 question") {
		t.Fatalf("first answer: %q %v", out, err)
	}
	qf := filepath.Join(e.root, ".gophermind", id, "QUESTIONS.md")
	before, _ := os.ReadFile(qf)
	if !strings.Contains(string(before), "```answer clarify-q1\nsqlite") {
		t.Fatalf("first answer not stored:\n%s", before)
	}
	out, err := callTool(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already answered") {
		t.Errorf("out %q", out)
	}
	if after, _ := os.ReadFile(qf); string(after) != string(before) {
		t.Errorf("QUESTIONS.md changed by a refused second answer:\n%s", after)
	}
}

// The chat tool refuses a filled question on the real write path, while the
// HTTP answer handler still overwrites exactly as before.
func TestAnswerHandlerStillOverwritesFilled(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-012"
	e.startAsking(id)
	if w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", "sqlite"), nil); w.Code != http.StatusOK {
		t.Fatalf("answer = %d %s", w.Code, w.Body)
	}
	qf := filepath.Join(e.root, ".gophermind", id, "QUESTIONS.md")
	if out, _ := callTool(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "x"}); !strings.Contains(out, "already answered") {
		t.Errorf("tool out %q", out)
	}
	if w := e.post("/app/briefs/"+id+"/answer", answerBody("clarify-q1", "postgres"), nil); w.Code != http.StatusOK {
		t.Fatalf("overwrite = %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(qf); !strings.Contains(string(b), "```answer clarify-q1\npostgres") {
		t.Error("HTTP answer no longer overwrites")
	}
}

// With a run folder the server cannot write, the launcher does the write and
// the refusal comes from it (exit 5); the tool passes --no-overwrite.
func TestBriefAnswerLauncherFallbackRefusesFilled(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-014"
	e.startAsking(id)
	dir := filepath.Join(e.root, ".gophermind", id)
	inner := e.script
	e.script = func(args []string) string {
		if len(args) > 1 && args[1] == "fill-answer" {
			return "exit 5"
		}
		return inner(args)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	out, err := callTool(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "x"})
	if err != nil || !strings.Contains(out, "already answered") {
		t.Fatalf("out %q err %v", out, err)
	}
	e.mu.Lock()
	last := strings.Join(e.calls[len(e.calls)-1], " ")
	e.mu.Unlock()
	if !strings.HasSuffix(last, "--no-overwrite") {
		t.Errorf("launcher call %q lacks --no-overwrite", last)
	}
}

// An older installed CLI rejects --no-overwrite with its usage text and exit 1;
// the chat tool says to reinstall rather than "exit status 1".
func TestBriefAnswerOldCLIMessage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-015"
	e.startAsking(id)
	dir := filepath.Join(e.root, ".gophermind", id)
	inner := e.script
	e.script = func(args []string) string {
		if len(args) > 1 && args[1] == "fill-answer" {
			return "echo 'usage: gophermind brief fill-answer <run-id> <question-id> --repo <root>' >&2; exit 1"
		}
		return inner(args)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	out, err := callTool(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "x"})
	if err != nil || !strings.Contains(out, "too old for chat answers; reinstall it together with gophermind-server") {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestBriefAnswerConcurrentOneWrite(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-013"
	e.startAsking(id)
	answer := pick(t, ts, "brief_answer")
	var wg sync.WaitGroup
	res := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]any{"id": id, "question_id": "clarify-q1", "text": fmt.Sprintf("answer-%d", i)})
			out, _ := answer.Run(context.Background(), raw)
			res <- out
		}(i)
	}
	wg.Wait()
	close(res)
	var outs []string
	for o := range res {
		outs = append(outs, o)
	}
	saved, refused := 0, 0
	for _, o := range outs {
		switch {
		case strings.Contains(o, "saved the answer"):
			saved++
		case strings.Contains(o, "already answered"):
			refused++
		default:
			t.Errorf("unexpected outcome %q", o)
		}
	}
	if saved != 1 || refused != len(outs)-1 {
		t.Errorf("saved %d refused %d", saved, refused)
	}
	b, _ := os.ReadFile(filepath.Join(e.root, ".gophermind", id, "QUESTIONS.md"))
	if n := strings.Count(string(b), "answer-"); n != 1 {
		t.Errorf("%d answers in file, want 1:\n%s", n, b)
	}
}

func TestBriefAnswerNotWaitingAndBadInput(t *testing.T) {
	_, ts := writeToolsEnv(t)
	for _, a := range []map[string]any{
		{"id": "../x", "question_id": "q1", "text": "a"},
		{"id": "gm-2026-10-30-099", "question_id": "bad id", "text": "a"},
		{"id": "gm-2026-10-30-099", "question_id": "q1", "text": "  "},
		{"id": "gm-2026-10-30-099", "question_id": "q1", "text": "```\nx"},
	} {
		if out, err := callTool(t, ts, "brief_answer", a); err == nil {
			t.Errorf("%v: want an error, got %q", a, out)
		}
	}
	out, err := callTool(t, ts, "brief_answer", map[string]any{"id": "gm-2026-10-30-099", "question_id": "q1", "text": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no such run") && !strings.Contains(out, "not waiting") {
		t.Errorf("unknown run outcome %q", out)
	}
}

func TestBriefStopNotActive(t *testing.T) {
	_, ts := writeToolsEnv(t)
	out, err := callTool(t, ts, "brief_stop", map[string]any{"id": "gm-2026-10-30-001"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not active") {
		t.Errorf("out %q", out)
	}
}

func TestBriefStopActive(t *testing.T) {
	e, ts := writeToolsEnv(t)
	e.script = func(args []string) string { return "sleep 30" }
	id := "gm-2026-10-30-007"
	if _, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText(id), "ask": false}); err != nil {
		t.Fatal(err)
	}
	e.waitPhase(id, phasePlanning, phaseRunning)
	out, err := callTool(t, ts, "brief_stop", map[string]any{"id": id})
	if err != nil || !strings.Contains(out, "stop") {
		t.Fatalf("out %q err %v", out, err)
	}
	e.waitNotActive(id)
}

func TestBriefResubmitBadID(t *testing.T) {
	_, ts := writeToolsEnv(t)
	if _, err := callTool(t, ts, "brief_resubmit", map[string]any{"id": "../etc"}); err == nil {
		t.Error("want error for a path id")
	}
	out, err := callTool(t, ts, "brief_resubmit", map[string]any{"id": "gm-2026-10-30-098"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no stored brief") {
		t.Errorf("out %q", out)
	}
}

func TestBriefResubmitStartsNewRun(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-010"
	if _, err := callTool(t, ts, "brief_create", map[string]any{"text": e.briefText(id), "ask": false}); err != nil {
		t.Fatal(err)
	}
	e.waitPhase(id, phaseDone, phaseFailed)
	e.waitNotActive(id)
	out, err := callTool(t, ts, "brief_resubmit", map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	newID := regexp.MustCompile(`started run (gm-[0-9-]+)`).FindStringSubmatch(out)
	if newID == nil || newID[1] == id {
		t.Errorf("out %q does not name a new id different from %s", out, id)
	}
	if n := runDirs(t, e.dir); len(n) != 2 {
		t.Errorf("run dirs %v, want two", n)
	}
}

// chatCallBody is an SSE completion asking for one tool call.
func chatCallBody(name string, args map[string]any) string {
	ab, _ := json.Marshal(args)
	call := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 0, "id": "call_1", "type": "function", "function": map[string]any{"name": name, "arguments": string(ab)}}}}}}}
	cb, _ := json.Marshal(call)
	return "data: " + string(cb) + "\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
}

const chatDoneBody = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: [DONE]\n\n"

// sendThroughAgent drives a real agent.Agent over a fake model that asks for
// one tool call, with approve as the approval func. It returns the tool result
// the model saw on its second request.
func sendThroughAgent(t *testing.T, ts []tools.Tool, name string, args map[string]any, approve func(tool, args string) bool) (approvals int32, toolMsg string) {
	t.Helper()
	var n atomic.Int32
	bodies := []string{chatCallBody(name, args), chatDoneBody}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		raw := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			k, err := r.Body.Read(buf)
			raw.Write(buf[:k])
			if err != nil {
				break
			}
		}
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		_ = json.Unmarshal([]byte(raw.String()), &req)
		for _, m := range req.Messages {
			if m.Role == "tool" {
				toolMsg = m.Content
			}
		}
		i := int(n.Add(1)) - 1
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(bodies[i]))
	}))
	defer srv.Close()
	var asked atomic.Int32
	gate := func(tool, a string) bool {
		asked.Add(1)
		return approve(tool, a)
	}
	ag := agent.New(llm.New(srv.URL, "", "m", 10*time.Second, false), tools.NewRegistry(ts...), 4, gate, nil)
	if _, err := ag.Send(context.Background(), "do it"); err != nil {
		t.Fatalf("send: %v", err)
	}
	return asked.Load(), toolMsg
}

func TestWriteToolDeniedDoesNothing(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-020"
	asked, toolMsg := sendThroughAgent(t, ts, "brief_create", map[string]any{"text": e.briefText(id)},
		func(string, string) bool { return false })
	if asked != 1 {
		t.Fatalf("approval asked %d times, want 1", asked)
	}
	if !strings.Contains(toolMsg, "denied") {
		t.Errorf("tool result %q, want denied", toolMsg)
	}
	if n := runDirs(t, e.dir); len(n) != 0 {
		t.Errorf("run dirs created despite denial: %v", n)
	}
	e.mu.Lock()
	calls := len(e.calls)
	e.mu.Unlock()
	if calls != 0 {
		t.Errorf("BriefCommand ran %d times despite denial", calls)
	}
}

func TestWriteToolDeniedAnswerAndStop(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-021"
	e.startAsking(id)
	qf := filepath.Join(e.root, ".gophermind", id, "QUESTIONS.md")
	before, _ := os.ReadFile(qf)
	deny := func(string, string) bool { return false }
	sendThroughAgent(t, ts, "brief_answer", map[string]any{"id": id, "question_id": "clarify-q1", "text": "sqlite"}, deny)
	if after, _ := os.ReadFile(qf); string(after) != string(before) {
		t.Error("answer file changed despite denial")
	}
	sendThroughAgent(t, ts, "brief_stop", map[string]any{"id": id}, deny)
	sendThroughAgent(t, ts, "brief_resubmit", map[string]any{"id": id}, deny)
	if n := runDirs(t, e.dir); len(n) != 1 {
		t.Errorf("run dirs %v, want only the original", n)
	}
	if rec := e.waitPhase(id, phaseWaiting); rec.Phase != phaseWaiting {
		t.Errorf("phase %s", rec.Phase)
	}
}

func TestWriteToolApprovedStartsRun(t *testing.T) {
	e, ts := writeToolsEnv(t)
	id := "gm-2026-10-30-022"
	asked, toolMsg := sendThroughAgent(t, ts, "brief_create", map[string]any{"text": e.briefText(id), "ask": false},
		func(string, string) bool { return true })
	if asked != 1 {
		t.Fatalf("approval asked %d times, want 1", asked)
	}
	if !strings.Contains(toolMsg, id) {
		t.Errorf("tool result %q lacks %s", toolMsg, id)
	}
	e.waitPhase(id, phaseDone, phaseFailed)
	e.waitNotActive(id)
	if n := runDirs(t, e.dir); len(n) != 1 {
		t.Errorf("run dirs %v", n)
	}
}

// Arguments with an unknown key, or two keys that differ only by case, are
// refused by all four write tools before anything runs.
func TestWriteToolsRejectAmbiguousArgs(t *testing.T) {
	_, ts := writeToolsEnv(t)
	cases := map[string][]string{
		"brief_create":   {`{"text":"a","TEXT":"b"}`, `{"text":"a","extra":1}`},
		"brief_answer":   {`{"id":"gm-2026-10-30-001","question_id":"q1","text":"a","Text":"b"}`, `{"id":"gm-2026-10-30-001","question_id":"q1","text":"a","x":1}`},
		"brief_stop":     {`{"id":"gm-2026-10-30-001","ID":"gm-2026-10-30-002"}`, `{"id":"gm-2026-10-30-001","x":1}`},
		"brief_resubmit": {`{"id":"gm-2026-10-30-001","Id":"gm-2026-10-30-002"}`, `{"id":"gm-2026-10-30-001","x":1}`},
	}
	for name, args := range cases {
		for _, a := range args {
			out, err := pick(t, ts, name).Run(context.Background(), json.RawMessage(a))
			if err == nil || !strings.Contains(err.Error(), "ambiguous") {
				t.Errorf("%s %s: out %q err %v", name, a, out, err)
			}
		}
	}
}
